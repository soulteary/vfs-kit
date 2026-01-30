package vfs

import (
	"os"
	"testing"
)

func TestDirEntryInfoSizeModTime(t *testing.T) {
	mem := Memory()
	if err := MkdirAll(mem, "a/b", 0755); err != nil {
		t.Fatal(err)
	}
	info, err := mem.Stat("a")
	if err != nil {
		t.Fatal(err)
	}
	if !info.IsDir() {
		t.Fatal("a should be dir")
	}
	if info.Size() != 0 {
		t.Errorf("Dir Size = %d, want 0", info.Size())
	}
	_ = info.ModTime()
	mode := info.Mode()
	if mode&os.ModeDir == 0 {
		t.Error("Mode should have ModeDir")
	}
}

func TestDirAddMiddleAndDuplicate(t *testing.T) {
	mem := Memory()
	if err := MkdirAll(mem, "d", 0755); err != nil {
		t.Fatal(err)
	}
	// Get root dir via internal entry to test Dir.Add
	ms, ok := mem.(*memoryFileSystem)
	if !ok {
		t.Fatal("Memory() should return *memoryFileSystem")
	}
	ms.mu.RLock()
	root, err := ms.dirEntry("")
	ms.mu.RUnlock()
	if err != nil {
		t.Fatal(err)
	}
	// Add so that we insert in the middle (v > name)
	if err := root.Add("c", &File{}); err != nil {
		t.Fatal(err)
	}
	if err := root.Add("a", &File{}); err != nil {
		t.Fatal(err)
	}
	if err := root.Add("b", &File{}); err != nil {
		t.Fatal(err)
	}
	// Duplicate name
	err = root.Add("a", &File{})
	if err == nil || err != os.ErrExist {
		t.Errorf("Add duplicate = %v, want ErrExist", err)
	}
}

func TestMemOpenFileOnDir(t *testing.T) {
	mem := Memory()
	if err := MkdirAll(mem, "d", 0755); err != nil {
		t.Fatal(err)
	}
	_, err := mem.OpenFile("d", os.O_CREATE|os.O_WRONLY, 0644)
	if err == nil {
		t.Error("OpenFile on directory should fail")
	}
}
