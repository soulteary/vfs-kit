package vfs

import (
	"errors"
	"io"
	"os"
	"sort"
	"strings"
	"testing"
)

var errWriteFail = errors.New("write failed")

// errWriteVFS wraps a VFS and returns a WFile that fails on Write for a given path (for testing).
type errWriteVFS struct {
	VFS
	path string
}

func (e *errWriteVFS) OpenFile(path string, flag int, perm os.FileMode) (WFile, error) {
	if path == e.path {
		return &errWriteFile{err: errWriteFail}, nil
	}
	return e.VFS.OpenFile(path, flag, perm)
}

// errWriteVFSWithCloseErr returns a VFS that yields a WFile that fails on Write and Close (for testing).
func errWriteVFSWithCloseErr(vfs VFS, path string, writeErr, closeErr error) VFS {
	return &errWriteVFSWithClose{vfs: vfs, path: path, writeErr: writeErr, closeErr: closeErr}
}

type errWriteVFSWithClose struct {
	vfs      VFS
	path     string
	writeErr error
	closeErr error
}

func (e *errWriteVFSWithClose) OpenFile(path string, flag int, perm os.FileMode) (WFile, error) {
	if path == e.path {
		return &errWriteFile{err: e.writeErr, closeErr: e.closeErr}, nil
	}
	return e.vfs.OpenFile(path, flag, perm)
}

func (e *errWriteVFSWithClose) Open(p string) (RFile, error)            { return e.vfs.Open(p) }
func (e *errWriteVFSWithClose) Lstat(p string) (os.FileInfo, error)     { return e.vfs.Lstat(p) }
func (e *errWriteVFSWithClose) Stat(p string) (os.FileInfo, error)      { return e.vfs.Stat(p) }
func (e *errWriteVFSWithClose) ReadDir(p string) ([]os.FileInfo, error) { return e.vfs.ReadDir(p) }
func (e *errWriteVFSWithClose) Mkdir(p string, perm os.FileMode) error  { return e.vfs.Mkdir(p, perm) }
func (e *errWriteVFSWithClose) Remove(p string) error                   { return e.vfs.Remove(p) }
func (e *errWriteVFSWithClose) String() string                          { return e.vfs.String() }

type errWriteFile struct {
	err      error
	closeErr error
}

func (e *errWriteFile) Read(p []byte) (int, error)     { return 0, io.EOF }
func (e *errWriteFile) Write(p []byte) (int, error)    { return 0, e.err }
func (e *errWriteFile) Seek(int64, int) (int64, error) { return 0, nil }
func (e *errWriteFile) Close() error {
	if e.closeErr != nil {
		return e.closeErr
	}
	return nil
}

// errLstatVFS wraps a VFS and returns an error from Lstat for a given path (for testing).
type errLstatVFS struct {
	VFS
	path string
	err  error
}

func (e *errLstatVFS) Lstat(path string) (os.FileInfo, error) {
	if path == e.path {
		return nil, e.err
	}
	return e.VFS.Lstat(path)
}

// errOpenVFS wraps a VFS and returns an error from Open for a given path (for testing).
type errOpenVFS struct {
	VFS
	path string
	err  error
}

func (e *errOpenVFS) Open(path string) (RFile, error) {
	if path == e.path {
		return nil, e.err
	}
	return e.VFS.Open(path)
}

// errStatVFS wraps a VFS and returns an error from Stat for a given path (for testing).
type errStatVFS struct {
	VFS
	path string
	err  error
}

func (e *errStatVFS) Stat(path string) (os.FileInfo, error) {
	if path == e.path {
		return nil, e.err
	}
	return e.VFS.Stat(path)
}

func TestCompressMemory(t *testing.T) {
	mem := Memory()
	if err := WriteFile(mem, "a", []byte("hello"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := Compress(mem); err != nil {
		t.Fatalf("Compress(Memory()) = %v", err)
	}
	data, err := ReadFile(mem, "a")
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "hello" {
		t.Errorf("after Compress ReadFile = %q, want \"hello\"", data)
	}
}

func TestIsExistIsNotExist(t *testing.T) {
	if !IsNotExist(os.ErrNotExist) {
		t.Error("IsNotExist(os.ErrNotExist) should be true")
	}
	if IsNotExist(os.ErrExist) {
		t.Error("IsNotExist(os.ErrExist) should be false")
	}
	if !IsExist(os.ErrExist) {
		t.Error("IsExist(os.ErrExist) should be true")
	}
	if IsExist(os.ErrNotExist) {
		t.Error("IsExist(os.ErrNotExist) should be false")
	}
}

func TestWalkErrSkipDir(t *testing.T) {
	mem := Memory()
	if err := MkdirAll(mem, "a/b/c", 0755); err != nil {
		t.Fatal(err)
	}
	if err := WriteFile(mem, "a/f", []byte("x"), 0644); err != nil {
		t.Fatal(err)
	}
	var visited []string
	err := Walk(mem, "/", func(fs VFS, path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		visited = append(visited, path)
		if path == "a" {
			return ErrSkipDir
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	// "a/b" and "a/b/c" and "a/f" should not be visited because we skipped "a"
	for _, p := range visited {
		if p == "a/b" || p == "a/b/c" || p == "a/f" {
			t.Errorf("visited %q but should have skipped under a", p)
		}
	}
}

func TestFileInfosSort(t *testing.T) {
	mem := Memory()
	if err := WriteFile(mem, "z", []byte("z"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := WriteFile(mem, "a", []byte("a"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := WriteFile(mem, "m", []byte("m"), 0644); err != nil {
		t.Fatal(err)
	}
	infos, err := mem.ReadDir("/")
	if err != nil {
		t.Fatal(err)
	}
	fis := FileInfos(infos)
	sort.Sort(fis)
	if fis.Len() != 3 {
		t.Fatalf("Len = %d, want 3", fis.Len())
	}
	if fis[0].Name() != "a" || fis[1].Name() != "m" || fis[2].Name() != "z" {
		t.Errorf("after sort: %q, %q, %q; want a, m, z", fis[0].Name(), fis[1].Name(), fis[2].Name())
	}
	// Cover Swap: reverse order so sort calls Swap
	fis2 := FileInfos([]os.FileInfo{infos[2], infos[0], infos[1]})
	sort.Sort(fis2)
	if fis2[0].Name() != "a" || fis2[1].Name() != "m" || fis2[2].Name() != "z" {
		t.Errorf("after sort reversed: %q, %q, %q", fis2[0].Name(), fis2[1].Name(), fis2[2].Name())
	}
}

func TestRemoveAllNested(t *testing.T) {
	mem := Memory()
	if err := MkdirAll(mem, "a/b/c", 0755); err != nil {
		t.Fatal(err)
	}
	if err := WriteFile(mem, "a/b/c/f", []byte("x"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := RemoveAll(mem, "a"); err != nil {
		t.Fatal(err)
	}
	_, err := mem.Stat("a")
	if err == nil || !IsNotExist(err) {
		t.Errorf("after RemoveAll(a) Stat(a) = %v", err)
	}
}

func TestWriteFileError(t *testing.T) {
	ro := ReadOnly(Memory())
	err := WriteFile(ro, "f", []byte("x"), 0644)
	if err != ErrReadOnlyFileSystem {
		t.Errorf("WriteFile on read-only = %v, want ErrReadOnlyFileSystem", err)
	}
}

func TestWriteFileWriteFails(t *testing.T) {
	mem := Memory()
	if err := MkdirAll(mem, "x", 0755); err != nil {
		t.Fatal(err)
	}
	wrapped := &errWriteVFS{VFS: mem, path: "x/f"}
	err := WriteFile(wrapped, "x/f", []byte("data"), 0644)
	if err != errWriteFail {
		t.Errorf("WriteFile when Write fails = %v, want errWriteFail", err)
	}
}

func TestWalkWithError(t *testing.T) {
	mem := Memory()
	_ = WriteFile(mem, "f", []byte("x"), 0644)
	myErr := os.ErrPermission
	err := Walk(mem, "/", func(fs VFS, path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info != nil && !info.IsDir() && info.Name() == "f" {
			return myErr
		}
		return nil
	})
	if err != myErr {
		t.Errorf("Walk = %v, want %v", err, myErr)
	}
}

func TestWalkSkipDirOnFile(t *testing.T) {
	mem := Memory()
	_ = WriteFile(mem, "f", []byte("x"), 0644)
	var visited []string
	err := Walk(mem, "/", func(fs VFS, path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		visited = append(visited, path)
		if info != nil && !info.IsDir() && info.Name() == "f" {
			return ErrSkipDir
		}
		return nil
	})
	if err != ErrSkipDir {
		t.Fatalf("Walk = %v, want ErrSkipDir (returning ErrSkipDir on file propagates)", err)
	}
	if len(visited) < 1 {
		t.Error("expected at least one visit")
	}
}

func TestCloneZeroPerm(t *testing.T) {
	src := Memory()
	if err := MkdirAll(src, "d", 0); err != nil {
		t.Fatal(err)
	}
	if err := WriteFile(src, "d/f", []byte("x"), 0); err != nil {
		t.Fatal(err)
	}
	dst := Memory()
	if err := Clone(dst, src); err != nil {
		t.Fatal(err)
	}
	data, err := ReadFile(dst, "d/f")
	if err != nil || string(data) != "x" {
		t.Fatalf("Clone zero perm: %v, %q", err, data)
	}
}

func TestRemoveAllSingleFile(t *testing.T) {
	mem := Memory()
	if err := WriteFile(mem, "f", []byte("x"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := RemoveAll(mem, "f"); err != nil {
		t.Fatal(err)
	}
	if _, err := mem.Stat("f"); err == nil || !IsNotExist(err) {
		t.Errorf("after RemoveAll(f) Stat(f) = %v", err)
	}
}

func TestRemoveAllNonExistent(t *testing.T) {
	mem := Memory()
	if err := RemoveAll(mem, "nonexistent"); err != nil {
		t.Fatal(err)
	}
}

func TestWalkLstatError(t *testing.T) {
	mem := Memory()
	if err := MkdirAll(mem, "a/b", 0755); err != nil {
		t.Fatal(err)
	}
	myErr := os.ErrPermission
	// Walk joins paths like "/a" + "b" -> "/a/b"; Lstat is called with that path
	wrapped := &errLstatVFS{VFS: mem, path: "/a/b", err: myErr}
	err := Walk(wrapped, "/", func(fs VFS, path string, info os.FileInfo, err error) error {
		if err != nil && path == "/a/b" {
			if err != myErr {
				t.Errorf("Walk Lstat error = %v, want %v", err, myErr)
			}
			return err
		}
		return nil
	})
	if err != myErr {
		t.Errorf("Walk = %v, want %v", err, myErr)
	}
}

func TestMkdirAllFileExists(t *testing.T) {
	mem := Memory()
	if err := WriteFile(mem, "file", []byte("x"), 0644); err != nil {
		t.Fatal(err)
	}
	err := MkdirAll(mem, "file", 0755)
	if err == nil {
		t.Fatal("MkdirAll(file) when file exists as file should fail")
	}
	if !strings.Contains(err.Error(), "exists") && !strings.Contains(err.Error(), "not a directory") {
		t.Errorf("error = %v, want 'exists' or 'not a directory'", err)
	}
}

func TestRemoveAllLstatError(t *testing.T) {
	mem := Memory()
	if err := MkdirAll(mem, "a", 0755); err != nil {
		t.Fatal(err)
	}
	myErr := os.ErrPermission
	wrapped := &errLstatVFS{VFS: mem, path: "a", err: myErr}
	err := RemoveAll(wrapped, "a")
	if err != myErr {
		t.Errorf("RemoveAll when Lstat fails = %v, want %v", err, myErr)
	}
}

func TestCloneReadFileFails(t *testing.T) {
	mem := Memory()
	if err := MkdirAll(mem, "a", 0755); err != nil {
		t.Fatal(err)
	}
	if err := WriteFile(mem, "a/f", []byte("x"), 0644); err != nil {
		t.Fatal(err)
	}
	openErr := os.ErrPermission
	// Clone walks from "/"; paths are like "/a", "/a/f"; Open is called with "/a/f"
	wrapped := &errOpenVFS{VFS: mem, path: "/a/f", err: openErr}
	dst := Memory()
	err := Clone(dst, wrapped)
	if err != openErr {
		t.Errorf("Clone when Open fails = %v, want %v", err, openErr)
	}
}

func TestWriteFileWriteFailsCloseAlsoFails(t *testing.T) {
	mem := Memory()
	if err := MkdirAll(mem, "x", 0755); err != nil {
		t.Fatal(err)
	}
	closeErr := errors.New("close failed")
	wrapped := errWriteVFSWithCloseErr(mem, "x/f", errWriteFail, closeErr)
	err := WriteFile(wrapped, "x/f", []byte("data"), 0644)
	if err != errWriteFail {
		t.Errorf("WriteFile when Write and Close fail = %v, want errWriteFail", err)
	}
}

func TestCloneWriteFileFails(t *testing.T) {
	mem := Memory()
	if err := MkdirAll(mem, "a", 0755); err != nil {
		t.Fatal(err)
	}
	if err := WriteFile(mem, "a/f", []byte("x"), 0644); err != nil {
		t.Fatal(err)
	}
	dst := Memory()
	if err := MkdirAll(dst, "a", 0755); err != nil {
		t.Fatal(err)
	}
	// Clone passes paths like "/a/f"; errWriteVFS makes OpenFile("/a/f") return a file that fails on Write
	wrappedDst := &errWriteVFS{VFS: dst, path: "/a/f"}
	err := Clone(wrappedDst, mem)
	if err != errWriteFail {
		t.Errorf("Clone when WriteFile fails = %v, want errWriteFail", err)
	}
}
