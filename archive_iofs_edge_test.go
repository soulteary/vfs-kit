package vfs

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

type archiveErrorReader struct{ err error }

func (r archiveErrorReader) Read([]byte) (int, error) { return 0, r.err }

type archiveErrorWriter struct{ err error }

func (w archiveErrorWriter) Write([]byte) (int, error) { return 0, w.err }

func TestArchiveEdgesZipBufferReadFailure(t *testing.T) {
	want := errors.New("archive source unavailable")
	v, err := Zip(archiveErrorReader{want}, 0)
	if v != nil || !errors.Is(err, want) {
		t.Fatalf("Zip = %v, %v; want nil VFS and source error", v, err)
	}
}

func TestArchiveEdgesZipChecksumFailure(t *testing.T) {
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	w, err := zw.CreateHeader(&zip.FileHeader{Name: "f", Method: zip.Store})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := io.WriteString(w, "checksum-protected data"); err != nil {
		t.Fatal(err)
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	data := buf.Bytes()
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatal(err)
	}
	offset, err := zr.File[0].DataOffset()
	if err != nil {
		t.Fatal(err)
	}
	data[offset] ^= 1
	v, err := Zip(bytes.NewReader(data), int64(len(data)))
	if v != nil || !errors.Is(err, zip.ErrChecksum) {
		t.Fatalf("Zip = %v, %v; want nil VFS and checksum error", v, err)
	}
}

func TestArchiveEdgesTarTruncatedBody(t *testing.T) {
	var buf bytes.Buffer
	tw := tar.NewWriter(&buf)
	if err := tw.WriteHeader(&tar.Header{Name: "f", Mode: 0644, Size: 4}); err != nil {
		t.Fatal(err)
	}
	// Supply one byte for an entry whose header promises four bytes.
	r := io.MultiReader(bytes.NewReader(buf.Bytes()), strings.NewReader("x"))
	v, err := Tar(r)
	if v != nil || !errors.Is(err, io.ErrUnexpectedEOF) {
		t.Fatalf("Tar = %v, %v; want nil VFS and unexpected EOF", v, err)
	}
}

func TestArchiveEdgesOpenMissingFile(t *testing.T) {
	v, err := Open(filepath.Join(t.TempDir(), "missing.zip"))
	if v != nil || !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("Open = %v, %v; want nil VFS and not-exist error", v, err)
	}
}

var archiveEdgeWriters = []struct {
	name  string
	write func(io.Writer, VFS) error
}{
	{"zip", WriteZip},
	{"tar", WriteTar},
	{"tar.gz", WriteTarGzip},
}

func archiveEdgeMemory(t *testing.T, names ...string) VFS {
	t.Helper()
	v := Memory()
	for _, name := range names {
		if err := WriteFile(v, name, []byte("data"), 0644); err != nil {
			t.Fatal(err)
		}
	}
	return v
}

func TestArchiveEdgesDestinationFailure(t *testing.T) {
	want := errors.New("archive destination unavailable")
	for _, writer := range archiveEdgeWriters {
		t.Run(writer.name, func(t *testing.T) {
			for _, names := range [][]string{nil, {"f"}} {
				err := writer.write(archiveErrorWriter{want}, archiveEdgeMemory(t, names...))
				if !errors.Is(err, want) {
					t.Fatalf("write with files %v = %v; want destination error", names, err)
				}
			}
		})
	}
}

type archiveReadDirVFS struct {
	VFS
	err   error
	calls int
}

func (v *archiveReadDirVFS) ReadDir(name string) ([]os.FileInfo, error) {
	v.calls++
	if v.err != nil {
		return nil, v.err
	}
	return v.VFS.ReadDir(name)
}

func TestArchiveEdgesWalkFailure(t *testing.T) {
	want := errors.New("directory listing unavailable")
	for _, writer := range archiveEdgeWriters {
		t.Run(writer.name, func(t *testing.T) {
			v := &archiveReadDirVFS{VFS: archiveEdgeMemory(t, "f"), err: want}
			if err := writer.write(io.Discard, v); !errors.Is(err, want) {
				t.Fatalf("write = %v; want directory-listing error", err)
			}
		})
	}
}

type archiveReadErrorVFS struct {
	VFS
	err    error
	closed bool
}

func (v *archiveReadErrorVFS) Open(name string) (RFile, error) {
	f, err := v.VFS.Open(name)
	if err != nil {
		return nil, err
	}
	return &archiveReadErrorFile{RFile: f, v: v}, nil
}

type archiveReadErrorFile struct {
	RFile
	v *archiveReadErrorVFS
}

func (f *archiveReadErrorFile) Read([]byte) (int, error) { return 0, f.v.err }

func (f *archiveReadErrorFile) Close() error {
	f.v.closed = true
	return f.RFile.Close()
}

func TestArchiveEdgesSourceReadFailureClosesFile(t *testing.T) {
	want := errors.New("file read unavailable")
	for _, writer := range archiveEdgeWriters {
		t.Run(writer.name, func(t *testing.T) {
			v := &archiveReadErrorVFS{VFS: archiveEdgeMemory(t, "f"), err: want}
			if err := writer.write(io.Discard, v); !errors.Is(err, want) {
				t.Fatalf("write = %v; want source read error", err)
			}
			if !v.closed {
				t.Fatal("source file was not closed after read failure")
			}
		})
	}
}

func TestArchiveEdgesZipRejectsLongName(t *testing.T) {
	v := archiveEdgeMemory(t, strings.Repeat("x", 1<<16))
	if err := WriteZip(io.Discard, v); err == nil {
		t.Fatal("WriteZip accepted an entry name exceeding ZIP's 16-bit length limit")
	}
}

type archiveMetadataVFS struct {
	VFS
	info os.FileInfo
}

func (v archiveMetadataVFS) Lstat(name string) (os.FileInfo, error) {
	if name == "/f" {
		return v.info, nil
	}
	return v.VFS.Lstat(name)
}

type archiveMetadataInfo struct {
	os.FileInfo
	mode os.FileMode
	size int64
}

func (i archiveMetadataInfo) Mode() os.FileMode { return i.mode }
func (i archiveMetadataInfo) Size() int64       { return i.size }

func TestArchiveEdgesTarRejectsInvalidMetadata(t *testing.T) {
	v := archiveEdgeMemory(t, "f")
	info, err := v.Stat("f")
	if err != nil {
		t.Fatal(err)
	}
	for _, tt := range []struct {
		name string
		mode os.FileMode
		size int64
	}{
		{"socket", os.ModeSocket, info.Size()},
		{"negative size", 0644, -1},
	} {
		t.Run(tt.name, func(t *testing.T) {
			wrapped := archiveMetadataVFS{VFS: v, info: archiveMetadataInfo{info, tt.mode, tt.size}}
			if err := WriteTar(io.Discard, wrapped); err == nil {
				t.Fatal("WriteTar accepted unsupported metadata")
			}
		})
	}
}

func TestReadOnlyFSEdgesOpenFailureAfterStat(t *testing.T) {
	want := errors.New("file disappeared after stat")
	v := &errOpenVFS{VFS: archiveEdgeMemory(t, "f"), path: "f", err: want}
	f, err := AsReadOnlyFS(v).Open("f")
	if f != nil || !errors.Is(err, want) {
		t.Fatalf("Open = %v, %v; want nil file and source error", f, err)
	}
}

func archiveEdgeDirFile(t *testing.T, v VFS) fs.ReadDirFile {
	t.Helper()
	f, err := AsReadOnlyFS(v).Open(".")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := f.Close(); err != nil {
			t.Error(err)
		}
	})
	df, ok := f.(fs.ReadDirFile)
	if !ok {
		t.Fatal("directory does not implement fs.ReadDirFile")
	}
	return df
}

func archiveEdgeEntryNames(entries []fs.DirEntry) []string {
	names := make([]string, len(entries))
	for i, entry := range entries {
		names[i] = entry.Name()
	}
	return names
}

func TestReadOnlyFSEdgesReadDirErrorRecovery(t *testing.T) {
	want := errors.New("directory listing unavailable")
	v := &archiveReadDirVFS{VFS: archiveEdgeMemory(t, "a"), err: want}
	df := archiveEdgeDirFile(t, v)
	if entries, err := df.ReadDir(1); entries != nil || !errors.Is(err, want) {
		t.Fatalf("ReadDir = %v, %v; want nil entries and listing error", entries, err)
	}
	v.err = nil
	entries, err := df.ReadDir(1)
	if err != nil || !reflect.DeepEqual(archiveEdgeEntryNames(entries), []string{"a"}) {
		t.Fatalf("ReadDir retry = %v, %v; want entry a", entries, err)
	}
	if v.calls != 2 {
		t.Fatalf("ReadDir upstream calls = %d; want a fresh call after failure", v.calls)
	}
}

func TestReadOnlyFSEdgesOversizedBatchUsesCache(t *testing.T) {
	v := &archiveReadDirVFS{VFS: archiveEdgeMemory(t, "a", "b")}
	df := archiveEdgeDirFile(t, v)
	entries, err := df.ReadDir(10)
	if err != nil || !reflect.DeepEqual(archiveEdgeEntryNames(entries), []string{"a", "b"}) {
		t.Fatalf("ReadDir(10) = %v, %v; want entries a, b", entries, err)
	}
	if err := WriteFile(v.VFS, "c", []byte("new"), 0644); err != nil {
		t.Fatal(err)
	}
	if entries, err := df.ReadDir(1); len(entries) != 0 || !errors.Is(err, io.EOF) {
		t.Fatalf("ReadDir after cached batch = %v, %v; want EOF", entries, err)
	}
	if v.calls != 1 {
		t.Fatalf("ReadDir upstream calls = %d; want one cached listing", v.calls)
	}
	// A newly opened handle sees the new directory entry.
	fresh, err := archiveEdgeDirFile(t, v).ReadDir(10)
	if err != nil || !reflect.DeepEqual(archiveEdgeEntryNames(fresh), []string{"a", "b", "c"}) {
		t.Fatalf("fresh ReadDir = %v, %v; want entries a, b, c", fresh, err)
	}
}
