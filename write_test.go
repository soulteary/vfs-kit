package vfs

import (
	"bytes"
	"errors"
	"io"
	"os"
	"path/filepath"
	"testing"
)

type writeTester struct {
	name   string
	writer func(io.Writer, VFS) error
	reader func(io.Reader) (VFS, error)
}

func TestWrite(t *testing.T) {
	var (
		writeTests = []writeTester{
			{"zip", WriteZip, func(r io.Reader) (VFS, error) { return Zip(r, 0) }},
			{"tar", WriteTar, Tar},
			{"tar.gz", WriteTarGzip, TarGzip},
		}
	)
	p := filepath.Join("testdata", "fs.zip")
	fs, err := Open(p)
	if err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	for _, v := range writeTests {
		buf.Reset()
		if err := v.writer(&buf, fs); err != nil {
			t.Fatalf("error writing %s: %s", v.name, err)
		}
		newFs, err := v.reader(&buf)
		if err != nil {
			t.Fatalf("error reading %s: %s", v.name, err)
		}
		testOpenedVFS(t, newFs)
	}
}

func TestWriteZipWalkFails(t *testing.T) {
	// copyVFS / WriteZip fail when Walk fails (e.g. Open fails for a path)
	mem := Memory()
	if err := MkdirAll(mem, "a", 0755); err != nil {
		t.Fatal(err)
	}
	if err := WriteFile(mem, "a/f", []byte("x"), 0644); err != nil {
		t.Fatal(err)
	}
	wrapped := &errOpenVFS{VFS: mem, path: "/a/f", err: errWriteZipOpenFail}
	var buf bytes.Buffer
	err := WriteZip(&buf, wrapped)
	if err != errWriteZipOpenFail {
		t.Errorf("WriteZip when Open fails = %v, want errWriteZipOpenFail", err)
	}
}

func TestWriteTarWalkFails(t *testing.T) {
	mem := Memory()
	if err := MkdirAll(mem, "a", 0755); err != nil {
		t.Fatal(err)
	}
	if err := WriteFile(mem, "a/f", []byte("x"), 0644); err != nil {
		t.Fatal(err)
	}
	wrapped := &errOpenVFS{VFS: mem, path: "/a/f", err: errWriteZipOpenFail}
	var buf bytes.Buffer
	err := WriteTar(&buf, wrapped)
	if err != errWriteZipOpenFail {
		t.Errorf("WriteTar when Open fails = %v, want errWriteZipOpenFail", err)
	}
}

func TestWriteTarGzipWalkFails(t *testing.T) {
	mem := Memory()
	if err := MkdirAll(mem, "a", 0755); err != nil {
		t.Fatal(err)
	}
	if err := WriteFile(mem, "a/f", []byte("x"), 0644); err != nil {
		t.Fatal(err)
	}
	wrapped := &errOpenVFS{VFS: mem, path: "/a/f", err: errWriteZipOpenFail}
	var buf bytes.Buffer
	err := WriteTarGzip(&buf, wrapped)
	if err != errWriteZipOpenFail {
		t.Errorf("WriteTarGzip when Open fails = %v, want errWriteZipOpenFail", err)
	}
}

var errWriteZipOpenFail = errors.New("open failed")

// TestWriteZipWhenOpenFails covers copyVFS and WriteZip error path when Open fails.
func TestWriteZipWhenOpenFails(t *testing.T) {
	mem := Memory()
	if err := MkdirAll(mem, "a", 0755); err != nil {
		t.Fatal(err)
	}
	if err := WriteFile(mem, "a/f", []byte("x"), 0644); err != nil {
		t.Fatal(err)
	}
	openErr := os.ErrPermission
	wrapped := &errOpenVFS{VFS: mem, path: "/a/f", err: openErr}
	var buf bytes.Buffer
	err := WriteZip(&buf, wrapped)
	if err != openErr {
		t.Errorf("WriteZip when Open fails = %v, want %v", err, openErr)
	}
}

// TestWriteTarWhenOpenFails covers copyVFS and WriteTar error path when Open fails.
func TestWriteTarWhenOpenFails(t *testing.T) {
	mem := Memory()
	if err := MkdirAll(mem, "a", 0755); err != nil {
		t.Fatal(err)
	}
	if err := WriteFile(mem, "a/f", []byte("x"), 0644); err != nil {
		t.Fatal(err)
	}
	openErr := os.ErrPermission
	wrapped := &errOpenVFS{VFS: mem, path: "/a/f", err: openErr}
	var buf bytes.Buffer
	err := WriteTar(&buf, wrapped)
	if err != openErr {
		t.Errorf("WriteTar when Open fails = %v, want %v", err, openErr)
	}
}

// TestWriteTarGzipWhenOpenFails covers WriteTarGzip error path when WriteTar fails.
func TestWriteTarGzipWhenOpenFails(t *testing.T) {
	mem := Memory()
	if err := WriteFile(mem, "f", []byte("x"), 0644); err != nil {
		t.Fatal(err)
	}
	wrapped := &errOpenVFS{VFS: mem, path: "/f", err: os.ErrPermission}
	var buf bytes.Buffer
	err := WriteTarGzip(&buf, wrapped)
	if err == nil {
		t.Fatal("WriteTarGzip when Open fails should return error")
	}
}
