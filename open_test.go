package vfs

import (
	"archive/zip"
	"bytes"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
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
	defer func() { _ = f.Close() }()
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

// TestTarGzipInvalidData 覆盖 TarGzip 当 gzip.NewReader 失败时返回错误
func TestTarGzipInvalidData(t *testing.T) {
	_, err := TarGzip(bytes.NewReader([]byte("not gzip data")))
	if err == nil {
		t.Fatal("TarGzip with invalid gzip data should fail")
	}
}

// failingReaderAt 嵌入 *bytes.Reader 并在 offset >= failAfter 时使 ReadAt 返回错误，用于覆盖 Zip 中 io.ReadAll 失败路径
type failingReaderAt struct {
	*bytes.Reader
	failAfter int64
	err       error
}

func (f *failingReaderAt) ReadAt(p []byte, off int64) (n int, err error) {
	if off >= f.failAfter {
		return 0, f.err
	}
	return f.Reader.ReadAt(p, off)
}

func TestZipEntryReadFails(t *testing.T) {
	p := filepath.Join("testdata", "fs.zip")
	data, err := os.ReadFile(p)
	if err != nil {
		t.Skipf("testdata not found: %v", err)
	}
	// 在 offset >= 100 时失败，以便第二个或后续 zip 条目读取时触发错误
	failErr := errors.New("read failed")
	wrapped := &failingReaderAt{Reader: bytes.NewReader(data), failAfter: 100, err: failErr}
	_, err = Zip(wrapped, int64(len(data)))
	if err == nil {
		t.Fatal("Zip when entry read fails should return error")
	}
	if err != failErr && !errors.Is(err, failErr) && !strings.Contains(err.Error(), "read failed") {
		t.Errorf("Zip error = %v, want read failure", err)
	}
}

// TestZipEntryOpenFails 覆盖 Zip 当某条目的 file.Open() 失败时返回错误。
// 先用 Store 创建合法 zip，再在中央目录中把 compression method 改为 99，使读取时 Open() 报错。
func TestZipEntryOpenFails(t *testing.T) {
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	h := &zip.FileHeader{Name: "x", Method: zip.Store, Modified: time.Now()}
	w, err := zw.CreateHeader(h)
	if err != nil {
		t.Fatal(err)
	}
	_, _ = w.Write([]byte("data"))
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	data := buf.Bytes()
	// 中央目录文件头签名 0x02014b50，其后 +10 为 compression method (2 bytes)
	cdSig := []byte{0x50, 0x4b, 0x01, 0x02}
	i := bytes.Index(data, cdSig)
	if i < 0 || i+12 > len(data) {
		t.Fatal("central directory header not found")
	}
	data[i+10] = 99
	data[i+11] = 0
	_, err = Zip(bytes.NewReader(data), int64(len(data)))
	if err == nil {
		t.Fatal("Zip when entry Open() fails (unsupported method) should return error")
	}
}
