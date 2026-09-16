package vfs

import (
	"io"
	"os"
	"sync"
	"testing"
	"time"
)

// Opening a file while another goroutine writes and closes one for the same
// path must be race-free. NewRFile/NewWFile read Data and Mode through
// fileData, and (*file).Close writes both, so both sides have to take the
// file's lock. Run with -race; before the fix this reported a write/read pair
// between fileData and Close.
func TestConcurrentOpenAndCloseIsRaceFree(t *testing.T) {
	fs := Memory()
	const path = "/dists/stable/InRelease"

	var wg sync.WaitGroup
	for i := 0; i < 24; i++ {
		wg.Add(2)
		go func() {
			defer wg.Done()
			w, err := fs.OpenFile(path, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0600)
			if err != nil {
				return
			}
			_, _ = w.Write([]byte("HTTP/1.1 200 OK\r\nContent-Length: 4\r\n\r\n"))
			_ = w.Close()
		}()
		go func() {
			defer wg.Done()
			r, err := fs.Open(path)
			if err != nil {
				return
			}
			_, _ = io.ReadAll(r)
			_ = r.Close()
		}()
	}
	wg.Wait()
}

// The same for a compressed file, which takes fileData's decompress path and
// makes Close rewrite Mode as well as Data.
func TestConcurrentOpenAndCloseCompressedIsRaceFree(t *testing.T) {
	fs := Memory()
	const path = "/payload"

	w, err := fs.OpenFile(path, os.O_CREATE|os.O_WRONLY, 0600)
	if err != nil {
		t.Fatalf("OpenFile: %v", err)
	}
	if c, ok := w.(interface{ SetCompressed(bool) }); ok {
		c.SetCompressed(true)
	}
	// Compressible payload, so Close keeps ModeCompress.
	payload := make([]byte, 4096)
	for i := range payload {
		payload[i] = 'A'
	}
	if _, err := w.Write(payload); err != nil {
		t.Fatalf("Write: %v", err)
	}
	if err := w.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	var wg sync.WaitGroup
	for i := 0; i < 24; i++ {
		wg.Add(2)
		go func() {
			defer wg.Done()
			r, err := fs.Open(path)
			if err != nil {
				return
			}
			_, _ = io.ReadAll(r)
			_ = r.Close()
		}()
		go func() {
			defer wg.Done()
			if fi, err := fs.Stat(path); err == nil {
				_ = fi.Mode()
				_ = fi.Size()
			}
		}()
	}
	wg.Wait()
}

// Closing the same handle from several goroutines must not race on the
// handle's closed flag, and must stay idempotent.
func TestConcurrentCloseOfSameHandleIsRaceFree(t *testing.T) {
	fs := Memory()

	w, err := fs.OpenFile("/f", os.O_CREATE|os.O_WRONLY, 0600)
	if err != nil {
		t.Fatalf("OpenFile: %v", err)
	}
	if _, err := w.Write([]byte("data")); err != nil {
		t.Fatalf("Write: %v", err)
	}

	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := w.Close(); err != nil {
				t.Errorf("Close: %v", err)
			}
		}()
	}
	wg.Wait()

	got, err := ReadFile(fs, "/f")
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if string(got) != "data" {
		t.Errorf("content = %q, want %q", got, "data")
	}
}

// Removing a directory while another goroutine creates an entry in it must be
// race-free. memoryFileSystem.Remove used to read len(entry.(*Dir).Entries)
// without holding that directory's lock, while Dir.Add mutates the same slice
// under it. Run with -race; before the fix this reported a write/read pair
// between Dir.Add and Remove.
func TestConcurrentRemoveDirAndCreateIsRaceFree(t *testing.T) {
	for round := 0; round < 200; round++ {
		fs := Memory()
		if err := fs.Mkdir("/d", 0755); err != nil {
			t.Fatalf("Mkdir: %v", err)
		}
		var wg sync.WaitGroup
		wg.Add(2)
		go func() {
			defer wg.Done()
			_ = fs.Remove("/d")
		}()
		go func() {
			defer wg.Done()
			w, err := fs.OpenFile("/d/f", os.O_CREATE|os.O_WRONLY, 0600)
			if err == nil {
				_ = w.Close()
			}
		}()
		wg.Wait()
	}
}

// Remove and OpenFile must never both report success while the created entry
// ends up unreachable. Remove checks emptiness and unlinks under one hold of
// the directory's write lock, and marks it removed, so a creation that
// resolved the directory first is rejected instead of landing in a detached
// one. Before the fix this lost roughly 3 creations in 2000 rounds.
func TestRemoveNeverDetachesACreatedEntry(t *testing.T) {
	const rounds = 5000
	for round := 0; round < rounds; round++ {
		fs := Memory()
		if err := fs.Mkdir("/d", 0755); err != nil {
			t.Fatalf("Mkdir: %v", err)
		}
		var removeErr, createErr error
		var start, wg sync.WaitGroup
		start.Add(1)
		wg.Add(2)
		go func() {
			defer wg.Done()
			start.Wait()
			removeErr = fs.Remove("/d")
		}()
		go func() {
			defer wg.Done()
			start.Wait()
			w, err := fs.OpenFile("/d/f", os.O_CREATE|os.O_WRONLY, 0600)
			createErr = err
			if err == nil {
				_ = w.Close()
			}
		}()
		start.Done()
		wg.Wait()

		if removeErr == nil && createErr == nil {
			if _, err := fs.Stat("/d/f"); err != nil {
				t.Fatalf("round %d: Remove and OpenFile both succeeded, but /d/f is gone: %v", round, err)
			}
		}
	}
}

// Remove unlinks the entry it resolved, not whatever holds the name by the
// time it takes the parent's lock. Rebinding a name in that window can change
// the entry's type too, so a stale Remove that resolved a *file* could
// otherwise unlink a directory without ever checking that it is empty and drop
// the whole subtree with it.
//
// Against the original code these fail within a couple of thousand rounds,
// because the unlocked emptiness check misses the same interleaving. Against
// code that has the locking fix but not the identity check they are weak: that
// window is a few instructions wide and loses a handful of entries per 200000
// rounds. At the 20000 rounds below, the file case passed 6 out of 6 runs
// against code missing the check and only failed 3 out of 3 at 200000. Treat a
// pass here as a regression net for the original defect, not as proof.
func TestRemoveDoesNotUnlinkARebindedDirectory(t *testing.T) {
	removeRebindLoop(t, func(fs VFS) error { return fs.Mkdir("/x", 0755) })
}

func TestRemoveDoesNotUnlinkADirectoryThatReplacedAFile(t *testing.T) {
	removeRebindLoop(t, func(fs VFS) error { return WriteFile(fs, "/x", []byte("v"), 0600) })
}

func removeRebindLoop(t *testing.T, seed func(VFS) error) {
	t.Helper()
	const rounds = 20000
	for round := 0; round < rounds; round++ {
		fs := Memory()
		if err := seed(fs); err != nil {
			t.Fatalf("seed: %v", err)
		}
		created := false
		var start, wg sync.WaitGroup
		start.Add(1)
		wg.Add(2)
		go func() {
			defer wg.Done()
			start.Wait()
			_ = fs.Remove("/x")
		}()
		go func() {
			defer wg.Done()
			start.Wait()
			// Drop whatever the other goroutine may have resolved and put a
			// fresh, non-empty directory at the same name.
			_ = fs.Remove("/x")
			if err := fs.Mkdir("/x", 0755); err != nil {
				return
			}
			w, err := fs.OpenFile("/x/f", os.O_CREATE|os.O_WRONLY, 0600)
			if err != nil {
				return
			}
			_ = w.Close()
			created = true
		}()
		start.Done()
		wg.Wait()

		if created {
			if _, err := fs.Stat("/x/f"); err != nil {
				t.Fatalf("round %d: /x/f was created but a stale Remove unlinked its directory: %v", round, err)
			}
		}
	}
}

// Remove holds the parent's lock while taking the entry's own. Every other
// Dir lock in the package is acquired outer directory first, so churning a
// nested tree from several goroutines must not deadlock.
func TestConcurrentNestedRemoveDoesNotDeadlock(t *testing.T) {
	fs := Memory()
	if err := MkdirAll(fs, "a/b/c", 0755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}

	done := make(chan struct{})
	go func() {
		defer close(done)
		var wg sync.WaitGroup
		for i := 0; i < 16; i++ {
			wg.Add(4)
			go func() { defer wg.Done(); _ = fs.Remove("/a/b/c") }()
			go func() { defer wg.Done(); _ = fs.Remove("/a/b") }()
			go func() { defer wg.Done(); _ = MkdirAll(fs, "a/b/c", 0755) }()
			go func() {
				defer wg.Done()
				if w, err := fs.OpenFile("/a/b/c/f", os.O_CREATE|os.O_WRONLY, 0600); err == nil {
					_ = w.Close()
				}
			}()
		}
		wg.Wait()
	}()

	select {
	case <-done:
	case <-time.After(30 * time.Second):
		t.Fatal("timed out: Remove's nested directory locks deadlocked")
	}
}
