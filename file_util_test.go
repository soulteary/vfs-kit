package vfs

import (
	"bytes"
	"io"
	"os"
	"runtime"
	"testing"
)

func TestCompressedFileRead(t *testing.T) {
	mem := Memory()
	plain := []byte("hello world")
	if err := MkdirAll(mem, "x", 0755); err != nil {
		t.Fatal(err)
	}
	w, err := mem.OpenFile("x/c", os.O_CREATE|os.O_RDWR, 0644)
	if err != nil {
		t.Fatal(err)
	}
	_, _ = w.Write(plain)
	if c, ok := w.(Compressor); ok {
		c.SetCompressed(true)
	}
	_ = w.Close()
	data, err := ReadFile(mem, "x/c")
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(data, plain) {
		t.Errorf("ReadFile after compressed = %q, want %q", data, plain)
	}
	r, err := mem.Open("x/c")
	if err != nil {
		t.Fatal(err)
	}
	if c, ok := r.(Compressor); ok {
		_ = c.IsCompressed()
		c.SetCompressed(false)
		_ = c.IsCompressed()
	}
	_ = r.Close()
}

func TestCompressedFileLarge(t *testing.T) {
	mem := Memory()
	if err := MkdirAll(mem, "x", 0755); err != nil {
		t.Fatal(err)
	}
	plain := bytes.Repeat([]byte("x"), 2000)
	w, err := mem.OpenFile("x/large", os.O_CREATE|os.O_RDWR, 0644)
	if err != nil {
		t.Fatal(err)
	}
	_, _ = w.Write(plain)
	if c, ok := w.(Compressor); ok {
		c.SetCompressed(true)
	}
	_ = w.Close()
	data, err := ReadFile(mem, "x/large")
	if err != nil {
		t.Fatal(err)
	}
	if len(data) != 2000 || !bytes.Equal(data, plain) {
		t.Errorf("ReadFile large compressed: len=%d, want 2000", len(data))
	}
}

func TestFileSeekWhence(t *testing.T) {
	mem := Memory()
	if err := WriteFile(mem, "f", []byte("abcdef"), 0644); err != nil {
		t.Fatal(err)
	}
	w, err := mem.OpenFile("f", os.O_RDWR, 0644)
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close()
	// SeekCurrent
	pos, err := w.Seek(2, io.SeekCurrent)
	if err != nil || pos != 2 {
		t.Fatalf("Seek(2, SeekCurrent) = %d, %v", pos, err)
	}
	pos, err = w.Seek(-1, io.SeekCurrent)
	if err != nil || pos != 1 {
		t.Fatalf("Seek(-1, SeekCurrent) = %d, %v", pos, err)
	}
	// SeekEnd
	pos, err = w.Seek(0, io.SeekEnd)
	if err != nil || pos != 6 {
		t.Fatalf("Seek(0, SeekEnd) = %d, %v", pos, err)
	}
	pos, err = w.Seek(-2, io.SeekEnd)
	if err != nil || pos != 4 {
		t.Fatalf("Seek(-2, SeekEnd) = %d, %v", pos, err)
	}
}

func TestFileSeekInvalidWhence(t *testing.T) {
	mem := Memory()
	if err := WriteFile(mem, "f", []byte("x"), 0644); err != nil {
		t.Fatal(err)
	}
	w, err := mem.OpenFile("f", os.O_RDWR, 0644)
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close()
	var panicked interface{}
	func() {
		defer func() { panicked = recover() }()
		_, _ = w.Seek(0, 99)
	}()
	if panicked == nil {
		t.Error("Seek(0, 99) should panic")
	}
}

func TestWFileCloseCompressedNoShrink(t *testing.T) {
	// Close with ModeCompress when compressed size >= original (else branch)
	mem := Memory()
	if err := MkdirAll(mem, "x", 0755); err != nil {
		t.Fatal(err)
	}
	// Use data that doesn't compress well so buf.Len() >= len(f.data) on close
	plain := bytes.Repeat([]byte{0xFF}, 200)
	w, err := mem.OpenFile("x/noshrink", os.O_CREATE|os.O_RDWR, 0644)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := w.Write(plain); err != nil {
		t.Fatal(err)
	}
	if c, ok := w.(Compressor); ok {
		c.SetCompressed(true)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	data, err := ReadFile(mem, "x/noshrink")
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(data, plain) {
		t.Errorf("after close compressed no-shrink: got %d bytes", len(data))
	}
}

func TestWFileFinalizer(t *testing.T) {
	// Trigger finalizer on WFile (closeFile) by not closing and running GC
	mem := Memory()
	if err := WriteFile(mem, "f", []byte("x"), 0644); err != nil {
		t.Fatal(err)
	}
	w, err := mem.OpenFile("f", os.O_RDWR, 0644)
	if err != nil {
		t.Fatal(err)
	}
	_ = w   // hold reference until we drop it
	w = nil // drop reference so finalizer can run
	runtime.GC()
	runtime.GC()
}

func TestNewRFileInvalidCompressedData(t *testing.T) {
	// NewRFile returns error when fileData fails (invalid zlib in ModeCompress)
	f := &File{Data: []byte("not valid zlib"), Mode: ModeCompress}
	_, err := NewRFile(f)
	if err == nil {
		t.Error("NewRFile with invalid compressed data should fail")
	}
}

func TestNewWFileInvalidCompressedData(t *testing.T) {
	f := &File{Data: []byte("not valid zlib"), Mode: ModeCompress}
	_, err := NewWFile(f, true, true)
	if err == nil {
		t.Error("NewWFile with invalid compressed data should fail")
	}
}

func TestRFileReadAfterClose(t *testing.T) {
	mem := Memory()
	if err := WriteFile(mem, "f", []byte("abc"), 0644); err != nil {
		t.Fatal(err)
	}
	r, err := mem.Open("f")
	if err != nil {
		t.Fatal(err)
	}
	if err := r.Close(); err != nil {
		t.Fatal(err)
	}
	_, err = r.Read(make([]byte, 1))
	if err == nil {
		t.Error("Read after Close should return error")
	}
}

func TestWFileWriteAfterClose(t *testing.T) {
	mem := Memory()
	if err := WriteFile(mem, "f", []byte("x"), 0644); err != nil {
		t.Fatal(err)
	}
	w, err := mem.OpenFile("f", os.O_RDWR, 0644)
	if err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	_, err = w.Write([]byte("y"))
	if err == nil {
		t.Error("Write after Close should return error")
	}
}

func TestWFileWriteExtendsData(t *testing.T) {
	// Write more bytes than space after offset to hit append branch (n < count)
	mem := Memory()
	if err := WriteFile(mem, "f", []byte("a"), 0644); err != nil {
		t.Fatal(err)
	}
	w, err := mem.OpenFile("f", os.O_RDWR, 0644)
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close()
	// seek to start, then write 10 bytes (file len 1, so copy extends then append)
	n, err := w.Write([]byte("0123456789"))
	if err != nil || n != 10 {
		t.Fatalf("Write = %d, %v", n, err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	data, err := ReadFile(mem, "f")
	if err != nil || string(data) != "0123456789" {
		t.Fatalf("after extend Write: %q, %v", data, err)
	}
}

func TestFileSeekAfterClose(t *testing.T) {
	mem := Memory()
	if err := WriteFile(mem, "f", []byte("ab"), 0644); err != nil {
		t.Fatal(err)
	}
	w, err := mem.OpenFile("f", os.O_RDWR, 0644)
	if err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	_, err = w.Seek(0, io.SeekStart)
	if err == nil {
		t.Error("Seek after Close should return error")
	}
}

func TestFileSeekOffsetClamp(t *testing.T) {
	mem := Memory()
	if err := WriteFile(mem, "f", []byte("abc"), 0644); err != nil {
		t.Fatal(err)
	}
	w, err := mem.OpenFile("f", os.O_RDWR, 0644)
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close()
	// Seek past end: offset clamped to len(data)
	pos, err := w.Seek(100, io.SeekStart)
	if err != nil {
		t.Fatal(err)
	}
	if pos != 3 {
		t.Errorf("Seek(100, Start) = %d, want 3 (clamped)", pos)
	}
	// Seek from end with negative overflow: offset clamped to 0
	pos, err = w.Seek(-100, io.SeekEnd)
	if err != nil {
		t.Fatal(err)
	}
	if pos != 0 {
		t.Errorf("Seek(-100, End) = %d, want 0 (clamped)", pos)
	}
}

func TestWFileWriteExtends(t *testing.T) {
	mem := Memory()
	if err := WriteFile(mem, "f", []byte("ab"), 0644); err != nil {
		t.Fatal(err)
	}
	w, err := mem.OpenFile("f", os.O_RDWR, 0644)
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close()
	// Seek to end then write: copy doesn't fit, append path
	_, _ = w.Seek(0, io.SeekEnd)
	n, err := w.Write([]byte("cdef"))
	if err != nil {
		t.Fatal(err)
	}
	if n != 4 {
		t.Errorf("Write = %d, want 4", n)
	}
	_ = w.Close()
	data, err := ReadFile(mem, "f")
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "abcdef" {
		t.Errorf("after write extend = %q, want \"abcdef\"", data)
	}
}
