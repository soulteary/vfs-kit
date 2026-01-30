package vfs

import (
	"os"
	"path/filepath"
	"testing"
)

func TestFS(t *testing.T) {
	dir := t.TempDir()
	fs, err := FS(dir)
	if err != nil {
		t.Fatal(err)
	}
	if err := WriteFile(fs, "f", []byte("x"), 0644); err != nil {
		t.Fatal(err)
	}
	data, err := ReadFile(fs, "f")
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "x" {
		t.Errorf("ReadFile = %q, want \"x\"", data)
	}
	// Root() and String() - fileSystem is unexported, so we need to get Root via type assert or use a wrapper that exposes it. FS() returns VFS, so we can't call Root() on it. Root is on *fileSystem. So we need to test via behavior: create subdir and check. Actually we can add a test that only uses public API: WriteFile, ReadFile, MkdirAll, ReadDir, RemoveAll. That will cover Stat, Lstat, OpenFile, Mkdir, Remove, ReadDir, String (indirectly). For Root() and IsTemporary() we need to use a type that exposes them - TemporaryVFS has Root(). So TmpFS returns TemporaryVFS which has Root(). So we can test Root and IsTemporary in TestTmpFS.
	// Cover Stat: ReadDir calls Stat internally for listing? No, ReadDir. Stat is used when we Stat a path. So do fs.Stat("f") and fs.Stat(".") or "/".
	info, err := fs.Stat("f")
	if err != nil {
		t.Fatal(err)
	}
	if info.IsDir() || info.Size() != 1 {
		t.Errorf("Stat(f) = dir=%v size=%d", info.IsDir(), info.Size())
	}
	infoRoot, err := fs.Stat("/")
	if err != nil {
		t.Fatal(err)
	}
	if !infoRoot.IsDir() {
		t.Error("Stat(/) should be dir")
	}
	_ = fs.String() // cover String()
}

func TestFSOpenFileInvalidPath(t *testing.T) {
	dir := t.TempDir()
	fs, err := FS(dir)
	if err != nil {
		t.Fatal(err)
	}
	_, err = fs.OpenFile("..", os.O_CREATE|os.O_WRONLY, 0644)
	if err != ErrInvalidPath {
		t.Errorf("OpenFile(\"..\") = %v, want ErrInvalidPath", err)
	}
	_, err = fs.OpenFile("sub/../../etc", os.O_CREATE|os.O_WRONLY, 0644)
	if err != ErrInvalidPath {
		t.Errorf("OpenFile path traversal = %v, want ErrInvalidPath", err)
	}
}

func TestTmpFSRootAndClose(t *testing.T) {
	fs, err := TmpFS("vfs-tmpfs-")
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = fs.Close() }()
	root := fs.Root()
	if root == "" {
		t.Error("Root() should be non-empty")
	}
	if !filepath.IsAbs(root) {
		t.Errorf("Root() = %q should be absolute", root)
	}
	// IsTemporary is on *fileSystem; we can't call it from interface. So we test Close() which removes the dir - that implies temporary.
	if err := WriteFile(fs, "x", []byte("y"), 0644); err != nil {
		t.Fatal(err)
	}
	data, err := ReadFile(fs, "x")
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "y" {
		t.Errorf("ReadFile = %q", data)
	}
	if tf, ok := fs.(*fileSystem); ok && !tf.IsTemporary() {
		t.Error("TmpFS should return temporary filesystem")
	}
	if err := fs.Close(); err != nil {
		t.Fatal(err)
	}
	// After Close, root dir should be gone
	if _, err := os.Stat(root); err == nil {
		t.Error("after Close(), root dir should be removed")
	}
}

func TestFSOpenNonexistent(t *testing.T) {
	dir := t.TempDir()
	fs, err := FS(dir)
	if err != nil {
		t.Fatal(err)
	}
	_, err = fs.Open("nonexistent")
	if err == nil {
		t.Error("Open(nonexistent) should fail")
	}
}

func TestFSCloseNonTemporary(t *testing.T) {
	dir := t.TempDir()
	fs, err := FS(dir)
	if err != nil {
		t.Fatal(err)
	}
	if err := fs.(*fileSystem).Close(); err != nil { // cover Close() when !temporary
		t.Errorf("Close() on non-temporary FS = %v, want nil", err)
	}
}

func TestFSStatNonexistent(t *testing.T) {
	dir := t.TempDir()
	fs, err := FS(dir)
	if err != nil {
		t.Fatal(err)
	}
	_, err = fs.Stat("nonexistent")
	if err == nil {
		t.Error("Stat(nonexistent) should fail")
	}
}

func TestFSReadDirSubdir(t *testing.T) {
	dir := t.TempDir()
	fs, err := FS(dir)
	if err != nil {
		t.Fatal(err)
	}
	if err := MkdirAll(fs, "a/b", 0755); err != nil {
		t.Fatal(err)
	}
	// ReadDir("/a") hits the branch where relName has leading slash and gets stripped
	infos, err := fs.ReadDir("/a")
	if err != nil {
		t.Fatal(err)
	}
	if len(infos) != 1 || infos[0].Name() != "b" {
		t.Errorf("ReadDir(\"/a\") = %v, want one entry \"b\"", infos)
	}
}
