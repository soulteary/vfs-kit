package vfs

import (
	"crypto/sha1"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

const (
	goTestFile = "go1.3.src.tar.gz"
)

type errNoTestFile string

func (e errNoTestFile) Error() string {
	return fmt.Sprintf("%s test file not found, use testdata/download-data.sh to fetch it", filepath.Base(string(e)))
}

func openOptionalTestFile(t testing.TB, name string) *os.File {
	filename := filepath.Join("testdata", name)
	f, err := os.Open(filename)
	if err != nil {
		t.Skip(errNoTestFile(filename))
	}
	return f
}

func testVFS(t *testing.T, fs VFS) {
	if err := WriteFile(fs, "a", []byte("A"), 0644); err != nil {
		t.Fatal(err)
	}
	data, err := ReadFile(fs, "a")
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "A" {
		t.Errorf("expecting file a to contain \"A\" got %q instead", string(data))
	}
	if err := WriteFile(fs, "b", []byte("B"), 0755); err != nil {
		t.Fatal(err)
	}
	if _, err := fs.OpenFile("b", os.O_CREATE|os.O_TRUNC|os.O_EXCL|os.O_WRONLY, 0755); err == nil || !IsExist(err) {
		t.Errorf("error should be ErrExist, it's %v", err)
	}
	fb, err := fs.OpenFile("b", os.O_TRUNC|os.O_WRONLY, 0755)
	if err != nil {
		t.Fatalf("error opening b: %s", err)
	}
	if _, err := fb.Write([]byte("BB")); err != nil {
		t.Errorf("error writing to b: %s", err)
	}
	if _, err := fb.Seek(0, io.SeekStart); err != nil {
		t.Errorf("error seeking b: %s", err)
	}
	if _, err := fb.Read(make([]byte, 2)); err == nil {
		t.Error("allowed reading WRONLY file b")
	}
	if err := fb.Close(); err != nil {
		t.Errorf("error closing b: %s", err)
	}
	files, err := fs.ReadDir("/")
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 2 {
		t.Errorf("expecting 2 files, got %d", len(files))
	}
	if n := files[0].Name(); n != "a" {
		t.Errorf("expecting first file named \"a\", got %q", n)
	}
	if n := files[1].Name(); n != "b" {
		t.Errorf("expecting first file named \"b\", got %q", n)
	}
	for ii, v := range files {
		es := int64(ii + 1)
		if s := v.Size(); es != s {
			t.Errorf("expecting file %s to have size %d, has %d", v.Name(), es, s)
		}
	}
	if err := MkdirAll(fs, "a/b/c/d", 0); err == nil {
		t.Error("should not allow dir over file")
	}
	if err := MkdirAll(fs, "c/d", 0755); err != nil {
		t.Fatal(err)
	}
	// Idempotent
	if err := MkdirAll(fs, "c/d", 0755); err != nil {
		t.Fatal(err)
	}
	if err := fs.Mkdir("c", 0755); err == nil || !IsExist(err) {
		t.Errorf("err should be ErrExist, it's %v", err)
	}
	// Should fail to remove, c is not empty
	if err := fs.Remove("c"); err == nil {
		t.Fatalf("removed non-empty directory")
	}
	var walked []os.FileInfo
	var walkedNames []string
	err = Walk(fs, "c", func(fs VFS, path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		walked = append(walked, info)
		walkedNames = append(walkedNames, path)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if exp := []string{"c", "c/d"}; !reflect.DeepEqual(exp, walkedNames) {
		t.Error(fmt.Printf("expecting walked names %v, got %v", exp, walkedNames))
	}
	for _, v := range walked {
		if !v.IsDir() {
			t.Errorf("%s should be a dir", v.Name())
		}
	}
	if err := RemoveAll(fs, "c"); err != nil {
		t.Fatal(err)
	}
	err = Walk(fs, "c", func(fs VFS, path string, info os.FileInfo, err error) error {
		return err
	})
	if err == nil || !IsNotExist(err) {
		t.Errorf("error should be ErrNotExist, it's %v", err)
	}
}

func TestMapFS(t *testing.T) {
	fs, err := Map(nil)
	if err != nil {
		t.Fatal(err)
	}
	testVFS(t, fs)
}

func TestPopulatedMap(t *testing.T) {
	files := map[string]*File{
		"a/1": {},
		"a/2": {},
	}
	fs, err := Map(files)
	if err != nil {
		t.Fatal(err)
	}
	infos, err := fs.ReadDir("a")
	if err != nil {
		t.Fatal(err)
	}
	if c := len(infos); c != 2 {
		t.Fatalf("expecting 2 files in a, got %d", c)
	}
	if infos[0].Name() != "1" || infos[1].Name() != "2" {
		t.Errorf("expecting names 1, 2 got %q, %q", infos[0].Name(), infos[1].Name())
	}
}

func TestBadPopulatedMap(t *testing.T) {
	// 1 can't be file and directory
	files := map[string]*File{
		"a/1":   {},
		"a/1/2": {},
	}
	_, err := Map(files)
	if err == nil {
		t.Fatal("Map should not work with a path as both file and directory")
	}
}

func TestTmpFS(t *testing.T) {
	fs, err := TmpFS("vfs-test")
	if err != nil {
		t.Fatal(err)
	}
	defer fs.Close()
	testVFS(t, fs)
}

const (
	go13FileCount = 4157
	// +1 because of the root, the real count is 407
	go13DirCount = 407 + 1
)

func countFileSystem(fs VFS) (int, int, error) {
	files, dirs := 0, 0
	err := Walk(fs, "/", func(fs VFS, _ string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			dirs++
		} else {
			files++
		}
		return nil
	})
	return files, dirs, err
}

func testGoFileCount(t *testing.T, fs VFS) {
	files, dirs, err := countFileSystem(fs)
	if err != nil {
		t.Fatal(err)
	}
	if files != go13FileCount {
		t.Errorf("expecting %d files in go1.3, got %d instead", go13FileCount, files)
	}
	if dirs != go13DirCount {
		t.Errorf("expecting %d directories in go1.3, got %d instead", go13DirCount, dirs)
	}
}

func TestGo13Files(t *testing.T) {
	f := openOptionalTestFile(t, goTestFile)
	defer f.Close()
	fs, err := TarGzip(f)
	if err != nil {
		t.Fatal(err)
	}
	testGoFileCount(t, fs)
}

func TestMounter(t *testing.T) {
	m := &Mounter{}
	f := openOptionalTestFile(t, goTestFile)
	defer f.Close()
	fs, err := TarGzip(f)
	if err != nil {
		t.Fatal(err)
	}
	m.Mount(fs, "/")
	testGoFileCount(t, m)
}

func TestClone(t *testing.T) {
	fs, err := Open(filepath.Join("testdata", "fs.zip"))
	if err != nil {
		t.Fatal(err)
	}
	infos1, err := fs.ReadDir("/")
	if err != nil {
		t.Fatal(err)
	}
	mem1 := Memory()
	if err := Clone(mem1, fs); err != nil {
		t.Fatal(err)
	}
	infos2, err := mem1.ReadDir("/")
	if err != nil {
		t.Fatal(err)
	}
	if len(infos2) != len(infos1) {
		t.Fatalf("cloned fs has %d entries in / rather than %d", len(infos2), len(infos1))
	}
	mem2 := Memory()
	if err := Clone(mem2, mem1); err != nil {
		t.Fatal(err)
	}
	infos3, err := mem2.ReadDir("/")
	if err != nil {
		t.Fatal(err)
	}
	if len(infos3) != len(infos2) {
		t.Fatalf("cloned fs has %d entries in / rather than %d", len(infos3), len(infos2))
	}
}

func measureVFSMemorySize(t testing.TB, fs VFS) int {
	mem, ok := fs.(*memoryFileSystem)
	if !ok {
		t.Fatalf("%T is not a memory filesystem", fs)
	}
	var total int
	var f func(d *Dir)
	f = func(d *Dir) {
		for _, v := range d.Entries {
			total += int(v.Size())
			if sd, ok := v.(*Dir); ok {
				f(sd)
			}
		}
	}
	f(mem.root)
	return total
}

func hashVFS(t testing.TB, fs VFS) string {
	sha := sha1.New()
	err := Walk(fs, "/", func(fs VFS, p string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return err
		}
		f, err := fs.Open(p)
		if err != nil {
			return err
		}
		defer f.Close()
		if _, err := io.Copy(sha, f); err != nil {
			return err
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return hex.EncodeToString(sha.Sum(nil))
}

func TestCompress(t *testing.T) {
	f := openOptionalTestFile(t, goTestFile)
	defer f.Close()
	fs, err := TarGzip(f)
	if err != nil {
		t.Fatal(err)
	}
	size1 := measureVFSMemorySize(t, fs)
	hash1 := hashVFS(t, fs)
	if err := Compress(fs); err != nil {
		t.Fatalf("can't compress fs: %s", err)
	}
	testGoFileCount(t, fs)
	size2 := measureVFSMemorySize(t, fs)
	hash2 := hashVFS(t, fs)
	if size2 >= size1 {
		t.Fatalf("compressed fs takes more memory %d than bare fs %d", size2, size1)
	}
	if hash1 != hash2 {
		t.Fatalf("compressing fs changed hash from %s to %s", hash1, hash2)
	}
}

// --- Chroot ---

func TestChroot(t *testing.T) {
	mem := Memory()
	if err := MkdirAll(mem, "sub/dir", 0755); err != nil {
		t.Fatal(err)
	}
	if err := WriteFile(mem, "sub/dir/f", []byte("data"), 0644); err != nil {
		t.Fatal(err)
	}
	ch, err := Chroot("sub", mem)
	if err != nil {
		t.Fatal(err)
	}
	// Under chroot, path "/" is "sub", so "dir/f" is "sub/dir/f"
	data, err := ReadFile(ch, "dir/f")
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "data" {
		t.Errorf("ReadFile(chroot, \"dir/f\") = %q, want \"data\"", data)
	}
	infos, err := ch.ReadDir("/")
	if err != nil {
		t.Fatal(err)
	}
	if len(infos) != 1 || infos[0].Name() != "dir" {
		t.Errorf("ReadDir(\"/\") = %v, want one entry \"dir\"", infos)
	}
	// Cover Stat, Lstat, OpenFile, Mkdir, Remove
	info, err := ch.Stat("dir/f")
	if err != nil || info.Size() != 4 {
		t.Errorf("Stat(dir/f) = %v, size=%d", err, info.Size())
	}
	_, err = ch.Lstat("dir")
	if err != nil || info == nil {
		_ = err
	}
	if err := ch.Mkdir("newdir", 0755); err != nil {
		t.Fatal(err)
	}
	if err := WriteFile(ch, "newdir/g", []byte("g"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := ch.Remove("newdir/g"); err != nil {
		t.Fatal(err)
	}
	if err := ch.Remove("newdir"); err != nil {
		t.Fatal(err)
	}
	if c, ok := ch.(Container); !ok || c.VFS() != mem {
		t.Error("Chroot VFS should implement Container and return underlying VFS")
	}
	s := ch.String()
	if s == "" || !strings.Contains(s, "Chroot") || !strings.Contains(s, "sub") || !strings.Contains(s, mem.String()) {
		t.Errorf("String() = %q", s)
	}
}

func TestChrootNotDir(t *testing.T) {
	mem := Memory()
	if err := WriteFile(mem, "file", []byte("x"), 0644); err != nil {
		t.Fatal(err)
	}
	_, err := Chroot("file", mem)
	if err == nil {
		t.Fatal("Chroot on file should fail")
	}
}

func TestChrootNotExist(t *testing.T) {
	mem := Memory()
	_, err := Chroot("nonexistent", mem)
	if err == nil {
		t.Fatal("Chroot on nonexistent path should fail")
	}
}

// --- ReadOnly ---

func TestReadOnly(t *testing.T) {
	mem := Memory()
	if err := WriteFile(mem, "f", []byte("x"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := MkdirAll(mem, "d", 0755); err != nil {
		t.Fatal(err)
	}
	ro := ReadOnly(mem)
	data, err := ReadFile(ro, "f")
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "x" {
		t.Errorf("ReadFile(ro, \"f\") = %q, want \"x\"", data)
	}
	_, _ = ro.Stat("f")
	_, _ = ro.Lstat("f")
	_, _ = ro.ReadDir("/")
	if c, ok := ro.(Container); !ok || c.VFS() != mem {
		t.Error("ReadOnly should implement Container")
	}
	if s := ro.String(); s == "" || s != fmt.Sprintf("RO %s", mem.String()) {
		t.Errorf("String() = %q", s)
	}
}

func TestReadOnlyWriteForbidden(t *testing.T) {
	mem := Memory()
	if err := MkdirAll(mem, "d", 0755); err != nil {
		t.Fatal(err)
	}
	if err := WriteFile(mem, "f", []byte("x"), 0644); err != nil {
		t.Fatal(err)
	}
	ro := ReadOnly(mem)
	_, err := ro.OpenFile("new", os.O_CREATE|os.O_WRONLY, 0644)
	if err != ErrReadOnlyFileSystem {
		t.Errorf("OpenFile(O_CREATE) = %v, want ErrReadOnlyFileSystem", err)
	}
	if err := ro.Mkdir("x", 0755); err != ErrReadOnlyFileSystem {
		t.Errorf("Mkdir = %v, want ErrReadOnlyFileSystem", err)
	}
	if err := ro.Remove("d"); err != ErrReadOnlyFileSystem {
		t.Errorf("Remove = %v, want ErrReadOnlyFileSystem", err)
	}
	// OpenFile read-only (no write flags) succeeds
	r, err := ro.OpenFile("f", os.O_RDONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	_ = r.Close()
}

// --- Rewriter ---

func TestRewriterNil(t *testing.T) {
	mem := Memory()
	wrapped := Rewriter(mem, nil)
	if wrapped != mem {
		t.Error("Rewriter(fs, nil) should return fs unchanged")
	}
}

func TestRewriter(t *testing.T) {
	mem := Memory()
	if err := WriteFile(mem, "a", []byte("A"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := MkdirAll(mem, "d", 0755); err != nil {
		t.Fatal(err)
	}
	// Rewrite path "x" -> "a" so that Open("x") reads "a"
	rew := Rewriter(mem, func(p string) string {
		if p == "x" {
			return "a"
		}
		if p == "y" {
			return "d"
		}
		return p
	})
	data, err := ReadFile(rew, "x")
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "A" {
		t.Errorf("ReadFile(rew, \"x\") = %q, want \"A\"", data)
	}
	_, _ = rew.Stat("x")
	_, _ = rew.Lstat("y")
	_, _ = rew.ReadDir("y")
	if err := rew.Mkdir("z", 0755); err != nil {
		t.Fatal(err)
	}
	if err := WriteFile(rew, "z/f", []byte("f"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := rew.Remove("z/f"); err != nil {
		t.Fatal(err)
	}
	if err := rew.Remove("z"); err != nil {
		t.Fatal(err)
	}
	if c, ok := rew.(Container); !ok || c.VFS() != mem {
		t.Error("Rewriter should implement Container")
	}
	if s := rew.String(); s == "" || s != fmt.Sprintf("Rewriter %s", mem.String()) {
		t.Errorf("String() = %q", s)
	}
}

// --- Mounter (extended) ---

func TestMounterEmptyOpen(t *testing.T) {
	m := &Mounter{}
	_, err := m.Open("/")
	if err == nil || !IsNotExist(err) {
		t.Errorf("Open with no mounts = %v, want ErrNotExist", err)
	}
	_, err = m.OpenFile("/f", os.O_CREATE|os.O_WRONLY, 0644)
	if err == nil || !IsNotExist(err) {
		t.Errorf("OpenFile with no mounts = %v, want ErrNotExist", err)
	}
	_, err = m.Stat("/")
	if err == nil || !IsNotExist(err) {
		t.Errorf("Stat with no mounts = %v, want ErrNotExist", err)
	}
	_, err = m.Lstat("/")
	if err == nil || !IsNotExist(err) {
		t.Errorf("Lstat with no mounts = %v, want ErrNotExist", err)
	}
	_, err = m.ReadDir("/")
	if err == nil || !IsNotExist(err) {
		t.Errorf("ReadDir with no mounts = %v, want ErrNotExist", err)
	}
	err = m.Mkdir("/d", 0755)
	if err == nil || !IsNotExist(err) {
		t.Errorf("Mkdir with no mounts = %v, want ErrNotExist", err)
	}
	err = m.Remove("/x")
	if err == nil || !IsNotExist(err) {
		t.Errorf("Remove with no mounts = %v, want ErrNotExist", err)
	}
}

func TestMounterMountTwiceAtRoot(t *testing.T) {
	m := &Mounter{}
	mem := Memory()
	if err := m.Mount(mem, "/"); err != nil {
		t.Fatal(err)
	}
	mem2 := Memory()
	err := m.Mount(mem2, "/")
	if err == nil {
		t.Fatal("Mount second FS at / should fail")
	}
}

func TestMounterMountAtSubdir(t *testing.T) {
	m := &Mounter{}
	mem := Memory()
	if err := MkdirAll(mem, "base", 0755); err != nil {
		t.Fatal(err)
	}
	if err := m.Mount(mem, "/"); err != nil {
		t.Fatal(err)
	}
	sub := Memory()
	if err := WriteFile(sub, "f", []byte("sub"), 0644); err != nil {
		t.Fatal(err)
	}
	// Mount at "/base" so Stat("/base") resolves via root FS
	if err := m.Mount(sub, "/base"); err != nil {
		t.Fatal(err)
	}
	data, err := ReadFile(m, "/base/f")
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "sub" {
		t.Errorf("ReadFile(m, \"/base/f\") = %q, want \"sub\"", data)
	}
}

func TestMounterUmount(t *testing.T) {
	m := &Mounter{}
	mem := Memory()
	if err := m.Mount(mem, "/"); err != nil {
		t.Fatal(err)
	}
	if err := m.Umount("/"); err != nil {
		t.Fatal(err)
	}
	_, err := m.Open("any")
	if err == nil || !IsNotExist(err) {
		t.Errorf("Open after Umount = %v, want not exist", err)
	}
}

func TestMounterUmountFailBelow(t *testing.T) {
	m := &Mounter{}
	mem := Memory()
	if err := MkdirAll(mem, "a", 0755); err != nil {
		t.Fatal(err)
	}
	if err := m.Mount(mem, "/"); err != nil {
		t.Fatal(err)
	}
	sub := Memory()
	if err := m.Mount(sub, "/a"); err != nil {
		t.Fatal(err)
	}
	err := m.Umount("/")
	if err == nil {
		t.Fatal("Umount(/) when /a is mounted below should fail")
	}
	if !strings.Contains(err.Error(), "mounted below") {
		t.Errorf("error = %v, want 'mounted below'", err)
	}
}

func TestMounterMountNonDir(t *testing.T) {
	// Mount at a path that is a file -> "is not a directory"
	m := &Mounter{}
	mem := Memory()
	if err := m.Mount(mem, "/"); err != nil {
		t.Fatal(err)
	}
	if err := WriteFile(mem, "f", []byte("x"), 0644); err != nil {
		t.Fatal(err)
	}
	other := Memory()
	err := m.Mount(other, "/f")
	if err == nil {
		t.Fatal("Mount at /f (a file) should fail")
	}
	if !strings.Contains(err.Error(), "not a directory") {
		t.Errorf("error = %v, want 'not a directory'", err)
	}
}

func TestMounterUmountNotMounted(t *testing.T) {
	// Umount at a path where nothing is mounted -> "no filesystem mounted at ..."
	m := &Mounter{}
	mem := Memory()
	if err := m.Mount(mem, "/"); err != nil {
		t.Fatal(err)
	}
	err := m.Umount("/nonexistent")
	if err == nil {
		t.Fatal("Umount(/nonexistent) when nothing mounted there should fail")
	}
	if !strings.Contains(err.Error(), "no filesystem mounted") {
		t.Errorf("error = %v, want 'no filesystem mounted'", err)
	}
}

func TestMounterString(t *testing.T) {
	m := &Mounter{}
	mem := Memory()
	if err := m.Mount(mem, "/"); err != nil {
		t.Fatal(err)
	}
	s := m.String()
	if s == "" || len(s) < 2 {
		t.Errorf("String() = %q", s)
	}
}

func TestMounterCompileTimeCheck(t *testing.T) {
	_ = mounterCompileTimeCheck()
}

func TestMounterOpenFileReadDirMkdirRemove(t *testing.T) {
	m := &Mounter{}
	mem := Memory()
	if err := m.Mount(mem, "/"); err != nil {
		t.Fatal(err)
	}
	// Mounter with root at "/" expects paths like "/f"
	w, err := m.OpenFile("/f", os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := w.Write([]byte("hello")); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	_, _ = m.Lstat("/f")
	infos, err := m.ReadDir("/")
	if err != nil || len(infos) != 1 {
		t.Fatalf("ReadDir(/) = %v, len=%d", err, len(infos))
	}
	if err := m.Mkdir("/d", 0755); err != nil {
		t.Fatal(err)
	}
	if err := m.Remove("/f"); err != nil {
		t.Fatal(err)
	}
	infos2, _ := m.ReadDir("/")
	if len(infos2) != 1 || infos2[0].Name() != "d" {
		t.Errorf("after Remove(f) ReadDir(/) = %v", infos2)
	}
}

func TestMemoryOpenFileFileAsParent(t *testing.T) {
	// OpenFile with path "file/sub" where "file" is an existing file -> dirEntry returns "it's not a directory"
	mem := Memory()
	if err := WriteFile(mem, "file", []byte("x"), 0644); err != nil {
		t.Fatal(err)
	}
	_, err := mem.OpenFile("file/sub", os.O_CREATE|os.O_RDWR, 0644)
	if err == nil {
		t.Fatal("OpenFile(file/sub) with file as parent should fail")
	}
	if !strings.Contains(err.Error(), "it's not a directory") {
		t.Errorf("error = %v, want 'it's not a directory'", err)
	}
}

func TestMemoryOpenFileSpecialMode(t *testing.T) {
	// OpenFile with mode that has ModeType set (e.g. ModeSymlink) -> "does not support special files"
	mem := Memory()
	if err := MkdirAll(mem, "d", 0755); err != nil {
		t.Fatal(err)
	}
	_, err := mem.OpenFile("d/f", os.O_CREATE|os.O_WRONLY, os.ModeSymlink|0644)
	if err == nil {
		t.Fatal("OpenFile with ModeSymlink should fail")
	}
	if !strings.Contains(err.Error(), "does not support special files") {
		t.Errorf("error = %v, want 'does not support special files'", err)
	}
}

func TestMemoryOpenFileEmptyBase(t *testing.T) {
	// OpenFile("/", ...) gives base == "" -> errNoEmptyNameFile
	mem := Memory()
	_, err := mem.OpenFile("/", os.O_CREATE|os.O_WRONLY, 0644)
	if err == nil {
		t.Fatal("OpenFile(/) should fail")
	}
	if !strings.Contains(err.Error(), "empty") {
		t.Errorf("error = %v, want empty name error", err)
	}
}

func TestMemoryOpenDirAsFile(t *testing.T) {
	// Open(path) when path is a directory -> "is not a file"
	mem := Memory()
	if err := MkdirAll(mem, "dir", 0755); err != nil {
		t.Fatal(err)
	}
	_, err := mem.Open("dir")
	if err == nil {
		t.Fatal("Open(dir) when dir is directory should fail")
	}
	if !strings.Contains(err.Error(), "is not a file") {
		t.Errorf("error = %v, want 'is not a file'", err)
	}
}

func TestMemoryOpenFileExclWhenExists(t *testing.T) {
	// OpenFile with O_EXCL|O_CREATE when file exists -> ErrExist
	mem := Memory()
	if err := WriteFile(mem, "f", []byte("x"), 0644); err != nil {
		t.Fatal(err)
	}
	_, err := mem.OpenFile("f", os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0644)
	if err == nil {
		t.Fatal("OpenFile(O_EXCL|O_CREATE) when file exists should fail")
	}
	if !IsExist(err) {
		t.Errorf("error = %v, want ErrExist", err)
	}
}

func TestMemoryOpenFileTruncate(t *testing.T) {
	mem := Memory()
	if err := WriteFile(mem, "f", []byte("original"), 0644); err != nil {
		t.Fatal(err)
	}
	w, err := mem.OpenFile("f", os.O_RDWR|os.O_TRUNC, 0644)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := w.Write([]byte("new")); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	data, err := ReadFile(mem, "f")
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "new" {
		t.Errorf("after O_TRUNC write = %q, want \"new\"", data)
	}
}
