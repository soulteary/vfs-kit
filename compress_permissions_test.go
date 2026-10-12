package vfs

import (
	"bytes"
	"errors"
	"os"
	"testing"
)

type compressionProbeVFS struct {
	VFS
	writeErr, closeErr error
	closes, writes     int
}

type compressionProbeFile struct {
	RFile
	owner *compressionProbeVFS
}

func (f *compressionProbeFile) Close() error {
	f.owner.closes++
	return errors.Join(f.RFile.Close(), f.owner.closeErr)
}

type compressionProbeCompressor struct {
	*compressionProbeFile
	Compressor
}

func (fs *compressionProbeVFS) Open(path string) (RFile, error) {
	f, err := fs.VFS.Open(path)
	if err != nil {
		return nil, err
	}
	probe := &compressionProbeFile{RFile: f, owner: fs}
	if c, ok := f.(Compressor); ok {
		return &compressionProbeCompressor{compressionProbeFile: probe, Compressor: c}, nil
	}
	return probe, nil
}

func (fs *compressionProbeVFS) OpenFile(path string, flag int, mode os.FileMode) (WFile, error) {
	fs.writes++
	if fs.writeErr != nil {
		return nil, fs.writeErr
	}
	return fs.VFS.OpenFile(path, flag, mode)
}

func TestCompressWriteAdmissionFailureClosesProbe(t *testing.T) {
	for _, failure := range []string{"write-open", "probe-close"} {
		t.Run(failure, func(t *testing.T) {
			mem := Memory()
			if err := WriteFile(mem, "file", []byte("original"), 0640); err != nil {
				t.Fatal(err)
			}
			injected := errors.New("compression storage unavailable")
			fs := &compressionProbeVFS{VFS: mem}
			wantWrites := 0
			if failure == "write-open" {
				fs.writeErr = injected
				wantWrites = 1
			} else {
				fs.closeErr = injected
			}
			if err := Compress(fs); !errors.Is(err, injected) {
				t.Fatalf("Compress = %v, want injected failure", err)
			}
			if fs.closes != 1 || fs.writes != wantWrites {
				t.Fatalf("probe closes=%d write attempts=%d; want 1/%d", fs.closes, fs.writes, wantWrites)
			}
			got, err := ReadFile(mem, "file")
			if err != nil || string(got) != "original" {
				t.Fatalf("failure changed source: %q, %v", got, err)
			}
		})
	}
}

func TestCompressReadOnlyMemoryRejectsWrite(t *testing.T) {
	mem := Memory()
	if err := WriteFile(mem, "file", []byte("original"), 0640); err != nil {
		t.Fatal(err)
	}
	if err := Compress(ReadOnly(mem)); !errors.Is(err, ErrReadOnlyFileSystem) {
		t.Fatalf("Compress read-only memory = %v, want read-only filesystem", err)
	}
	got, err := ReadFile(mem, "file")
	if err != nil || string(got) != "original" {
		t.Fatalf("read-only compression changed source: %q, %v", got, err)
	}
}

func TestCompressUnsupportedFileSystemDoesNotOpenForWrite(t *testing.T) {
	disk, err := FS(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	body := bytes.Repeat([]byte("payload"), 256)
	if err := WriteFile(disk, "file", body, 0640); err != nil {
		t.Fatal(err)
	}
	fs := &compressionProbeVFS{VFS: ReadOnly(disk), writeErr: os.ErrPermission}
	if err := Compress(fs); err != nil {
		t.Fatal(err)
	}
	if fs.closes != 1 || fs.writes != 0 {
		t.Fatalf("unsupported compression: probe closes=%d writes=%d; want 1/0", fs.closes, fs.writes)
	}
	got, err := ReadFile(disk, "file")
	if err != nil || !bytes.Equal(got, body) {
		t.Fatalf("unsupported compression changed source: %q, %v", got, err)
	}
}

func TestCompressMemoryUsesWritableHandle(t *testing.T) {
	mem := Memory()
	body := bytes.Repeat([]byte("payload"), 256)
	if err := WriteFile(mem, "file", body, 0640); err != nil {
		t.Fatal(err)
	}
	if err := Compress(mem); err != nil {
		t.Fatal(err)
	}
	info, err := mem.Stat("file")
	if err != nil || info.Mode()&ModeCompress == 0 {
		t.Fatalf("Compress did not retain compression: info=%v err=%v", info, err)
	}
	got, err := ReadFile(mem, "file")
	if err != nil || !bytes.Equal(got, body) {
		t.Fatalf("Compress changed body: %q, %v", got, err)
	}
}
