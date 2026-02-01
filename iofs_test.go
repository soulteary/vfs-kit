package vfs

import (
	"errors"
	"io"
	"io/fs"
	"path"
	"testing"
)

// TestAsReadOnlyFS verifies that a VFS can be used as io/fs.FS for read-only
// paths (fs.ReadFile, fs.WalkDir, etc.) without changing the VFS interface.
func TestAsReadOnlyFS(t *testing.T) {
	mem := Memory()
	if err := WriteFile(mem, "x", []byte("xxx"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := MkdirAll(mem, "a", 0755); err != nil {
		t.Fatal(err)
	}
	if err := WriteFile(mem, "a/b", []byte("ab"), 0644); err != nil {
		t.Fatal(err)
	}

	ro := AsReadOnlyFS(mem)

	// fs.ReadFile
	data, err := fs.ReadFile(ro, "x")
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "xxx" {
		t.Errorf("fs.ReadFile(ro, \"x\") = %q, want \"xxx\"", data)
	}
	data, err = fs.ReadFile(ro, "a/b")
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "ab" {
		t.Errorf("fs.ReadFile(ro, \"a/b\") = %q, want \"ab\"", data)
	}
	// root as ".": ReadFile on a directory must fail
	_, err = fs.ReadFile(ro, ".")
	if err == nil {
		t.Error("fs.ReadFile(ro, \".\") should fail for directory")
	}

	// fs.WalkDir (read-only path using io/fs)
	var names []string
	err = fs.WalkDir(ro, ".", func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		names = append(names, path.Clean(p))
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	// expect ".", "x", "a", "a/b" (order may vary)
	haveRoot, haveX, haveA, haveAB := false, false, false, false
	for _, n := range names {
		switch n {
		case ".", "/":
			haveRoot = true
		case "x":
			haveX = true
		case "a":
			haveA = true
		case "a/b":
			haveAB = true
		}
	}
	if !haveRoot {
		t.Errorf("WalkDir did not visit root, got names %v", names)
	}
	if !haveX {
		t.Errorf("WalkDir did not visit x, got names %v", names)
	}
	if !haveA {
		t.Errorf("WalkDir did not visit a, got names %v", names)
	}
	if !haveAB {
		t.Errorf("WalkDir did not visit a/b, got names %v", names)
	}
}

// TestAsReadOnlyFS_OpenDir verifies Open(".") returns a directory that supports ReadDir.
func TestAsReadOnlyFS_OpenDir(t *testing.T) {
	mem := Memory()
	_ = WriteFile(mem, "f", []byte("f"), 0644)

	ro := AsReadOnlyFS(mem)
	f, err := ro.Open(".")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = f.Close() }()

	df, ok := f.(fs.ReadDirFile)
	if !ok {
		t.Fatal("Open(\".\") should return fs.ReadDirFile for directory")
	}
	entries, err := df.ReadDir(-1)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 {
		t.Errorf("ReadDir(-1) got %d entries, want 1", len(entries))
	}
	if len(entries) > 0 && entries[0].Name() != "f" {
		t.Errorf("first entry Name() = %q, want \"f\"", entries[0].Name())
	}
}

func TestAsReadOnlyFS_FileReadClose(t *testing.T) {
	mem := Memory()
	_ = WriteFile(mem, "f", []byte("content"), 0644)
	ro := AsReadOnlyFS(mem)
	f, err := ro.Open("f")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = f.Close() }()
	buf := make([]byte, 20)
	n, err := f.Read(buf)
	if err != nil && err != io.EOF {
		t.Fatal(err)
	}
	if n != 7 || string(buf[:n]) != "content" {
		t.Errorf("Read = %d, %q", n, buf[:n])
	}
	_, _ = f.Stat()
	// Cover adapterDirFile.Read (returns 0, nil for dirs)
	dir, err := ro.Open(".")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = dir.Close() }()
	n, err = dir.Read(buf)
	if n != 0 || err != nil {
		t.Errorf("dir.Read = %d, %v (want 0, nil)", n, err)
	}
}

func TestAsReadOnlyFS_ReadDirN(t *testing.T) {
	mem := Memory()
	_ = WriteFile(mem, "a", []byte("a"), 0644)
	_ = WriteFile(mem, "b", []byte("b"), 0644)
	ro := AsReadOnlyFS(mem)
	f, err := ro.Open(".")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = f.Close() }()
	df := f.(fs.ReadDirFile)
	// ReadDir(1) multiple times to cover n>0 path
	ents, err := df.ReadDir(1)
	if err != nil || len(ents) != 1 {
		t.Fatalf("ReadDir(1) = %v, len=%d", err, len(ents))
	}
	ents2, err := df.ReadDir(1)
	if err != nil || len(ents2) != 1 {
		t.Fatalf("ReadDir(1) #2 = %v, len=%d", err, len(ents2))
	}
	ents3, err := df.ReadDir(1)
	if err != io.EOF || len(ents3) != 0 {
		t.Fatalf("ReadDir(1) #3 = %v, len=%d (expect EOF)", err, len(ents3))
	}
}

// TestAsReadOnlyFS_ReadDirZero 覆盖 ReadDir(n) 当 n <= 0 时返回全部剩余条目
func TestAsReadOnlyFS_ReadDirZero(t *testing.T) {
	mem := Memory()
	_ = WriteFile(mem, "a", []byte("a"), 0644)
	_ = WriteFile(mem, "b", []byte("b"), 0644)
	ro := AsReadOnlyFS(mem)
	f, err := ro.Open(".")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = f.Close() }()
	df := f.(fs.ReadDirFile)
	// ReadDir(0) 应返回所有条目（io/fs 约定）
	ents, err := df.ReadDir(0)
	if err != nil || len(ents) != 2 {
		t.Fatalf("ReadDir(0) = %v, len=%d; want 2 entries", err, len(ents))
	}
}

func TestAsReadOnlyFS_InvalidPath(t *testing.T) {
	mem := Memory()
	_ = WriteFile(mem, "f", []byte("x"), 0644)
	ro := AsReadOnlyFS(mem)
	_, err := fs.ReadFile(ro, "..")
	if err == nil {
		t.Error("ReadFile(\"..\") should fail")
	}
	var pe *fs.PathError
	if !errors.As(err, &pe) || pe.Err != ErrInvalidPath {
		t.Errorf("ReadFile(\"..\") = %v, want PathError with ErrInvalidPath", err)
	}
	_, err = ro.Open("../f")
	if err == nil {
		t.Error("Open(\"../f\") should fail")
	}
	if !errors.As(err, &pe) || pe.Err != ErrInvalidPath {
		t.Errorf("Open(\"../f\") = %v, want PathError with ErrInvalidPath", err)
	}
}

func TestVFSPathFromFSName(t *testing.T) {
	tests := []struct {
		name string
		want string
	}{
		{".", "/"},
		{"", "/"},
		{"/", "/"},
		{"a", "a"},
		{"a/b", "a/b"},
		{"/a/b", "/a/b"}, // path.Clean + FromSlash keeps leading slash on Unix
	}
	for _, tt := range tests {
		got := VFSPathFromFSName(tt.name)
		if got != tt.want {
			t.Errorf("VFSPathFromFSName(%q) = %q, want %q", tt.name, got, tt.want)
		}
	}
}

func TestAsReadOnlyFSOpenNonexistent(t *testing.T) {
	mem := Memory()
	_ = WriteFile(mem, "f", []byte("x"), 0644)
	ro := AsReadOnlyFS(mem)
	_, err := ro.Open("nonexistent")
	if err == nil {
		t.Error("Open(nonexistent) should fail")
	}
}

func TestAsReadOnlyFSOpenWhenStatFails(t *testing.T) {
	// Open calls Stat; when Stat fails we return the error
	mem := Memory()
	_ = WriteFile(mem, "f", []byte("x"), 0644)
	statErr := errors.New("stat failed")
	wrapped := &errStatVFS{VFS: mem, path: "f", err: statErr}
	ro := AsReadOnlyFS(wrapped)
	_, err := ro.Open("f")
	if err != statErr {
		t.Errorf("Open when Stat fails = %v, want %v", err, statErr)
	}
}
