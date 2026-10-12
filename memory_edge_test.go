package vfs

import (
	"bytes"
	"compress/zlib"
	"errors"
	"fmt"
	"os"
	"testing"
)

func TestMemoryOpenFileDirectoryReturnsError(t *testing.T) {
	mem := Memory()
	if err := mem.Mkdir("dir", 0755); err != nil {
		t.Fatal(err)
	}
	for _, flag := range []int{os.O_RDONLY, os.O_RDONLY | os.O_CREATE, os.O_WRONLY, os.O_RDWR} {
		t.Run(fmt.Sprint(flag), func(t *testing.T) {
			f, err := mem.OpenFile("dir", flag, 0644)
			if f != nil {
				_ = f.Close()
			}
			if err == nil || f != nil {
				t.Fatalf("OpenFile(directory, %d) = %v, %v; want nil file and error", flag, f, err)
			}
		})
	}
}

func TestMemoryReadDirRejectsFilesAndMissingPaths(t *testing.T) {
	mem := Memory()
	if err := WriteFile(mem, "file", []byte("contents"), 0644); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"file", "missing", "file/child"} {
		t.Run(name, func(t *testing.T) {
			if _, err := mem.ReadDir(name); err == nil {
				t.Fatal("ReadDir accepted a non-directory path")
			}
		})
	}
}

func TestMapRejectsNamesThatCanonicalizeToTheSameEntry(t *testing.T) {
	_, err := Map(map[string]*File{"a": {}, "./a": {}})
	if !errors.Is(err, os.ErrExist) {
		t.Fatalf("Map duplicate normalized names: %v", err)
	}
}

func TestCompressedFileRejectsCorruptChecksum(t *testing.T) {
	var compressed bytes.Buffer
	w := zlib.NewWriter(&compressed)
	if _, err := w.Write(bytes.Repeat([]byte("payload"), 1024)); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	data := bytes.Clone(compressed.Bytes())
	data[len(data)-1] ^= 0xff
	for _, open := range []struct {
		name string
		fn   func(*File) (RFile, error)
	}{
		{"read", NewRFile},
		{"write", func(f *File) (RFile, error) { return NewWFile(f, true, true) }},
	} {
		t.Run(open.name, func(t *testing.T) {
			f, err := open.fn(&File{Mode: ModeCompress, Data: data})
			if f != nil || !errors.Is(err, zlib.ErrChecksum) {
				t.Fatalf("open corrupt compressed data = %v, %v", f, err)
			}
		})
	}
}
