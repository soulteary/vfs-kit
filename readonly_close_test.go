package vfs

import (
	"bytes"
	"errors"
	"io"
	"os"
	"testing"
)

var readOnlyCloseModes = []struct {
	name                  string
	previous, replacement bool
	previousRepeats       int
}{
	{"plain-to-plain", false, false, 128},
	{"plain-to-compressed", false, true, 128},
	{"compressed-to-plain", true, false, 128},
	{"compressed-to-compressed", true, true, 128},
	{"empty-to-compressed", false, true, 0},
}

func TestReadOnlyFileClosePreservesReplacement(t *testing.T) {
	for _, opener := range []struct {
		name string
		open func(*File) (RFile, error)
	}{
		{"NewRFile", NewRFile},
		{"NewWFile-read-only", func(f *File) (RFile, error) { return NewWFile(f, true, false) }},
	} {
		for _, mode := range readOnlyCloseModes {
			t.Run(opener.name+"/"+mode.name, func(t *testing.T) {
				f := &File{Mode: 0640}
				writeReadOnlyCloseFixture(t, f, bytes.Repeat([]byte("previous"), mode.previousRepeats), mode.previous)
				reader, err := opener.open(f)
				if err != nil {
					t.Fatal(err)
				}
				t.Cleanup(func() { _ = reader.Close() })
				replacement := bytes.Repeat([]byte("replacement"), 256)
				writeReadOnlyCloseFixture(t, f, replacement, mode.replacement)
				assertReadOnlyClosePreservesReplacement(t, reader, f, replacement)
			})
		}
	}
}

func writeReadOnlyCloseFixture(t *testing.T, f *File, body []byte, compressed bool) {
	t.Helper()
	w, err := NewWFile(f, false, true)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = w.Close() })
	if n, err := w.Write(body); err != nil || n != len(body) {
		t.Fatalf("Write = %d, %v; want %d bytes", n, err, len(body))
	}
	w.(Compressor).SetCompressed(compressed)
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestMemoryReadOnlyClosePreservesReplacement(t *testing.T) {
	for _, opener := range []struct {
		name string
		open func(VFS) (RFile, error)
	}{
		{"Open", func(fs VFS) (RFile, error) { return fs.Open("file") }},
		{"OpenFile-read-only", func(fs VFS) (RFile, error) { return fs.OpenFile("file", os.O_RDONLY, 0) }},
	} {
		for _, mode := range readOnlyCloseModes {
			t.Run(opener.name+"/"+mode.name, func(t *testing.T) {
				fs := Memory()
				writeMemoryReadOnlyCloseFixture(t, fs, bytes.Repeat([]byte("previous"), mode.previousRepeats), mode.previous)
				reader, err := opener.open(fs)
				if err != nil {
					t.Fatal(err)
				}
				t.Cleanup(func() { _ = reader.Close() })
				replacement := bytes.Repeat([]byte("replacement"), 256)
				writeMemoryReadOnlyCloseFixture(t, fs, replacement, mode.replacement)
				info, err := fs.Stat("file")
				if err != nil {
					t.Fatal(err)
				}
				assertReadOnlyClosePreservesReplacement(t, reader, info.Sys().(*File), replacement)
				got, err := ReadFile(fs, "file")
				if err != nil || !bytes.Equal(got, replacement) {
					t.Fatalf("ReadFile after old reader.Close = %q, %v", got, err)
				}
			})
		}
	}
}

func writeMemoryReadOnlyCloseFixture(t *testing.T, fs VFS, body []byte, compressed bool) {
	t.Helper()
	w, err := fs.OpenFile("file", os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0640)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = w.Close() })
	if n, err := w.Write(body); err != nil || n != len(body) {
		t.Fatalf("Write = %d, %v; want %d bytes", n, err, len(body))
	}
	w.(Compressor).SetCompressed(compressed)
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
}

func assertReadOnlyClosePreservesReplacement(t *testing.T, reader RFile, f *File, replacement []byte) {
	t.Helper()
	wantData := bytes.Clone(f.Data)
	wantMode, wantModTime := f.Mode, f.ModTime
	for range 2 {
		if err := reader.Close(); err != nil {
			t.Fatal(err)
		}
	}
	if !bytes.Equal(f.Data, wantData) {
		t.Errorf("read-only Close changed the replacement's stored bytes")
	}
	if f.Mode != wantMode || f.ModTime != wantModTime {
		t.Errorf("read-only Close changed metadata: Mode=%v ModTime=%v; want %v %v", f.Mode, f.ModTime, wantMode, wantModTime)
	}
	if _, err := reader.Read(make([]byte, 1)); !errors.Is(err, errFileClosed) {
		t.Errorf("Read after Close = %v, want file closed", err)
	}
	if _, err := reader.Seek(0, io.SeekStart); !errors.Is(err, errFileClosed) {
		t.Errorf("Seek after Close = %v, want file closed", err)
	}
	got, err := fileData(f)
	if err != nil || !bytes.Equal(got, replacement) {
		t.Errorf("replacement body changed: got %d bytes, %v; want %d bytes", len(got), err, len(replacement))
	}
}

func TestReadOnlySetCompressedPreservesFile(t *testing.T) {
	for _, opener := range []struct {
		name string
		open func(*File) (RFile, error)
	}{
		{"NewRFile", NewRFile},
		{"NewWFile-read-only", func(f *File) (RFile, error) { return NewWFile(f, true, false) }},
	} {
		for _, compressed := range []bool{false, true} {
			name := "plain"
			if compressed {
				name = "compressed"
			}
			t.Run(opener.name+"/"+name, func(t *testing.T) {
				f := &File{Mode: 0640}
				body := bytes.Repeat([]byte("payload"), 256)
				writeReadOnlyCloseFixture(t, f, body, compressed)
				reader, err := opener.open(f)
				if err != nil {
					t.Fatal(err)
				}
				t.Cleanup(func() { _ = reader.Close() })
				wantData, wantMode := bytes.Clone(f.Data), f.Mode
				c := reader.(Compressor)
				c.SetCompressed(!compressed)
				if !bytes.Equal(f.Data, wantData) || f.Mode != wantMode || c.IsCompressed() != compressed {
					t.Error("read-only SetCompressed changed stored data or compression mode")
				}
				assertReadOnlyClosePreservesReplacement(t, reader, f, body)
			})
		}
	}
}
