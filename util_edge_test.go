package vfs

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

type utilityFaultVFS struct {
	VFS
	op, path string
	err      error
}

func (v *utilityFaultVFS) ReadDir(path string) ([]os.FileInfo, error) {
	if v.op == "readdir" && path == v.path {
		return nil, v.err
	}
	return v.VFS.ReadDir(path)
}

func (v *utilityFaultVFS) Mkdir(path string, mode os.FileMode) error {
	if v.op == "mkdir" && path == v.path {
		return v.err
	}
	return v.VFS.Mkdir(path, mode)
}

func (v *utilityFaultVFS) Remove(path string) error {
	if v.op == "remove" && path == v.path {
		return v.err
	}
	return v.VFS.Remove(path)
}

func TestUtilityFailuresPreserveCauseAndContents(t *testing.T) {
	for _, tc := range []struct {
		name, op, path string
		call           func(VFS) error
	}{
		{"mkdir child", "mkdir", "/new", func(v VFS) error { return MkdirAll(v, "new/child", 0755) }},
		{"remove directory read", "readdir", "dir", func(v VFS) error { return RemoveAll(v, "dir") }},
		{"remove child", "remove", "dir/file", func(v VFS) error { return RemoveAll(v, "dir") }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			mem := Memory()
			seedUtilityFile(t, mem)
			cause := errors.New("injected backing failure")
			wrapped := &utilityFaultVFS{VFS: mem, op: tc.op, path: tc.path, err: cause}
			if err := tc.call(wrapped); !errors.Is(err, cause) {
				t.Fatalf("error=%v, want original cause", err)
			}
			if data, err := ReadFile(mem, "dir/file"); err != nil || string(data) != "contents" {
				t.Fatalf("operation changed original file: %q, %v", data, err)
			}
		})
	}
}

func TestMkdirAllRootCreationFailure(t *testing.T) {
	cause := errors.New("root unavailable")
	writable := &utilityFaultVFS{VFS: Memory(), op: "mkdir", path: "/", err: cause}
	v := &errLstatVFS{VFS: writable, path: "/", err: os.ErrNotExist}
	if err := MkdirAll(v, "child", 0755); !errors.Is(err, cause) {
		t.Fatalf("MkdirAll root error=%v", err)
	}
}

func TestWalkRootSkipDirDoesNotVisitChildren(t *testing.T) {
	mem := Memory()
	seedUtilityFile(t, mem)
	var visited []string
	err := Walk(mem, "/", func(_ VFS, path string, _ os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		visited = append(visited, path)
		return ErrSkipDir
	})
	if err != nil || !reflect.DeepEqual(visited, []string{"/"}) {
		t.Fatalf("Walk=%v, visited=%v", err, visited)
	}
}

func TestCloneAndCompressPropagateWalkFailure(t *testing.T) {
	cause := errors.New("cannot inspect root")
	v := &errLstatVFS{VFS: Memory(), path: "/", err: cause}
	for _, tc := range []struct {
		name string
		call func(VFS) error
	}{
		{"clone", func(src VFS) error { return Clone(Memory(), src) }},
		{"compress", Compress},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if err := tc.call(v); !errors.Is(err, cause) {
				t.Fatalf("walk error=%v", err)
			}
		})
	}
}

func TestCloneDirectoryCreationFailureStopsCopy(t *testing.T) {
	src := Memory()
	seedUtilityFile(t, src)
	cause := errors.New("directory creation failed")
	dst := &utilityFaultVFS{VFS: Memory(), op: "mkdir", path: "/dir", err: cause}
	if err := Clone(dst, src); !errors.Is(err, cause) {
		t.Fatalf("Clone=%v", err)
	}
	if _, err := dst.Stat("dir/file"); !IsNotExist(err) {
		t.Fatalf("Clone wrote file after directory failure: %v", err)
	}
}

func TestMounterSiblingNamesDoNotBlockUnmount(t *testing.T) {
	mem := Memory()
	for _, path := range []string{"a", "ab"} {
		if err := mem.Mkdir(path, 0755); err != nil {
			t.Fatal(err)
		}
	}
	m := &Mounter{}
	if err := m.Mount(mem, "/"); err != nil {
		t.Fatal(err)
	}
	for _, path := range []string{"/a", "/ab"} {
		if err := m.Mount(Memory(), path); err != nil {
			t.Fatal(err)
		}
	}
	if err := m.Umount("/a"); err != nil {
		t.Fatalf("sibling mount treated as a descendant: %v", err)
	}
	if err := m.Mount(Memory(), "/missing"); !IsNotExist(err) {
		t.Fatalf("mount on missing directory=%v", err)
	}
}

func TestTmpFSUnavailableTemporaryDirectory(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "missing")
	for _, key := range []string{"TMPDIR", "TMP", "TEMP"} {
		t.Setenv(key, missing)
	}
	fs, err := TmpFS("unavailable-")
	if fs != nil || !IsNotExist(err) {
		t.Fatalf("TmpFS=%v, %v", fs, err)
	}
}

func seedUtilityFile(t *testing.T, v VFS) {
	t.Helper()
	if err := MkdirAll(v, "dir", 0755); err != nil {
		t.Fatal(err)
	}
	if err := WriteFile(v, "dir/file", []byte("contents"), 0644); err != nil {
		t.Fatal(err)
	}
}
