package vfs

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func testOpenedVFS(t *testing.T, fs VFS) {
	data1, err := ReadFile(fs, "a/b/c/d")
	if err != nil {
		t.Fatal(err)
	}
	if string(data1) != "go" {
		t.Errorf("expecting a/b/c/d to contain \"go\", it contains %q instead", string(data1))
	}
	data2, err := ReadFile(fs, "empty")
	if err != nil {
		t.Fatal(err)
	}
	if len(data2) > 0 {
		t.Error("non-empty empty file")
	}
}

func testOpenFilename(t *testing.T, filename string) {
	p := filepath.Join("testdata", filename)
	fs, err := Open(p)
	if err != nil {
		t.Fatal(err)
	}
	testOpenedVFS(t, fs)
}

func TestOpenZip(t *testing.T) {
	testOpenFilename(t, "fs.zip")
}

func TestOpenTar(t *testing.T) {
	testOpenFilename(t, "fs.tar")
}

func TestOpenTarGzip(t *testing.T) {
	testOpenFilename(t, "fs.tar.gz")
}

func TestOpenTarBzip2(t *testing.T) {
	testOpenFilename(t, "fs.tar.bz2")
}

func TestZipReaderAt(t *testing.T) {
	f := mustOpen(t, filepath.Join("testdata", "fs.zip"))
	defer f.Close()
	data, err := io.ReadAll(f)
	if err != nil {
		t.Fatal(err)
	}
	fs, err := Zip(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatal(err)
	}
	testOpenedVFS(t, fs)
}

func mustOpen(t *testing.T, name string) io.ReadCloser {
	f, err := os.Open(name)
	if err != nil {
		t.Skipf("testdata not found: %v", err)
	}
	return f
}

func TestOpenUnsupportedExtension(t *testing.T) {
	// Open with unsupported extension returns error "can't open a VFS from a ... file"
	dir := t.TempDir()
	f, err := os.CreateTemp(dir, "vfs.*.txt")
	if err != nil {
		t.Fatal(err)
	}
	_ = f.Close()
	_, err = Open(f.Name())
	if err == nil {
		t.Fatal("Open(.txt) should fail")
	}
	if !strings.Contains(err.Error(), "can't open a VFS from a") {
		t.Errorf("error = %v, want 'can't open a VFS from a ...'", err)
	}
}

func TestTarInvalidData(t *testing.T) {
	// Tar when tr.Next() returns error (not EOF) - e.g. corrupt/invalid tar
	_, err := Tar(bytes.NewReader([]byte("not a valid tar")))
	if err == nil {
		t.Fatal("Tar with invalid data should fail")
	}
	if err == io.EOF {
		t.Errorf("Tar should not return EOF for invalid data, got %v", err)
	}
}

func TestZipWithoutReaderAt(t *testing.T) {
	// Zip with reader that does not implement io.ReaderAt and size <= 0 uses io.ReadAll path
	p := filepath.Join("testdata", "fs.zip")
	data, err := os.ReadFile(p)
	if err != nil {
		t.Skipf("testdata not found: %v", err)
	}
	// noReaderAt wraps bytes.Reader to hide ReaderAt
	type noReaderAt struct{ io.Reader }
	fs, err := Zip(noReaderAt{bytes.NewReader(data)}, 0)
	if err != nil {
		t.Fatal(err)
	}
	testOpenedVFS(t, fs)
}
