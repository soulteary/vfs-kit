package vfs

import (
	"errors"
	"fmt"
	"os"
	pathpkg "path"
	"strings"
	"time"
)

// ErrRemoveRoot is returned when removing the root of a file system, which
// has no parent to be unlinked from. RemoveAll reaches it after emptying the
// tree, so callers clearing a whole in-memory VFS can ignore it with
// errors.Is.
var ErrRemoveRoot = errors.New("can't remove the filesystem root")

var (
	errNoEmptyNameFile = errors.New("can't create file with empty name")
	errNoEmptyNameDir  = errors.New("can't create directory with empty name")
)

type memoryFileSystem struct {
	root *Dir
}

// entry walks path from the root and returns the entry, the directory
// holding it and its index in that directory. The returned directory is nil
// when path names the root itself.
//
// Each level is read under its own directory's read lock, released before
// descending, so the result is a snapshot: by the time entry returns, the
// name may already be bound to something else. Callers that mutate take the
// relevant directory's lock and re-check there.
func (fs *memoryFileSystem) entry(path string) (Entry, *Dir, int, error) {
	path = cleanPath(path)
	if path == "" || path == "/" || path == "." {
		return fs.root, nil, 0, nil
	}
	if path[0] == '/' {
		path = path[1:]
	}
	dir := fs.root
	for {
		p := strings.IndexByte(path, '/')
		name := path
		if p > 0 {
			name = path[:p]
			path = path[p+1:]
		} else {
			path = ""
		}
		dir.RLock()
		entry, pos, err := dir.Find(name)
		dir.RUnlock()
		if err != nil {
			return nil, nil, 0, err
		}
		if len(path) == 0 {
			return entry, dir, pos, nil
		}
		if entry.Type() != EntryTypeDir {
			break
		}
		dir = entry.(*Dir)
	}
	return nil, nil, 0, os.ErrNotExist
}

func (fs *memoryFileSystem) dirEntry(path string) (*Dir, error) {
	entry, _, _, err := fs.entry(path)
	if err != nil {
		return nil, err
	}
	if entry.Type() != EntryTypeDir {
		return nil, fmt.Errorf("%s it's not a directory", path)
	}
	return entry.(*Dir), nil
}

func (fs *memoryFileSystem) Open(path string) (RFile, error) {
	entry, _, _, err := fs.entry(path)
	if err != nil {
		return nil, err
	}
	if entry.Type() != EntryTypeFile {
		return nil, fmt.Errorf("%s is not a file", path)
	}
	return NewRFile(entry.(*File))
}

func (fs *memoryFileSystem) OpenFile(path string, flag int, mode os.FileMode) (WFile, error) {
	if mode&os.ModeType != 0 {
		return nil, fmt.Errorf("%T does not support special files", fs)
	}
	path = cleanPath(path)
	dir, base := pathpkg.Split(path)
	if base == "" {
		return nil, errNoEmptyNameFile
	}
	d, err := fs.dirEntry(dir)
	if err != nil {
		return nil, err
	}

	d.Lock()
	defer d.Unlock()
	if d.removed {
		// The directory was unlinked between resolving it above and taking
		// its lock, so nothing can live at this path any more.
		return nil, os.ErrNotExist
	}
	f, _, _ := d.Find(base)
	if f == nil && flag&os.O_CREATE == 0 {
		return nil, os.ErrNotExist
	}
	// Read only file?
	if flag&os.O_WRONLY == 0 && flag&os.O_RDWR == 0 {
		if f == nil {
			return nil, os.ErrNotExist
		}
		return NewWFile(f.(*File), true, false)
	}
	// Write file, either f != nil or flag&os.O_CREATE
	if f != nil {
		if f.Type() != EntryTypeFile {
			return nil, fmt.Errorf("%s is not a file", path)
		}
		if flag&os.O_EXCL != 0 {
			return nil, os.ErrExist
		}
		// Check if we should truncate
		if flag&os.O_TRUNC != 0 {
			file := f.(*File)
			file.Lock()
			file.ModTime = time.Now()
			file.Data = nil
			file.Unlock()
		}
	} else {
		f = &File{ModTime: time.Now()}
		err = d.Add(base, f)
		if err != nil {
			return nil, os.ErrExist
		}
	}
	return NewWFile(f.(*File), flag&os.O_RDWR != 0, true)
}

func (fs *memoryFileSystem) Lstat(path string) (os.FileInfo, error) {
	return fs.Stat(path)
}

func (fs *memoryFileSystem) Stat(path string) (os.FileInfo, error) {
	entry, _, _, err := fs.entry(path)
	if err != nil {
		return nil, err
	}
	return &EntryInfo{Path: path, Entry: entry}, nil
}

func (fs *memoryFileSystem) ReadDir(path string) ([]os.FileInfo, error) {
	entry, _, _, err := fs.entry(path)
	if err != nil {
		return nil, err
	}
	if entry.Type() != EntryTypeDir {
		return nil, fmt.Errorf("%s is not a directory", path)
	}
	dir := entry.(*Dir)
	dir.RLock()
	infos := make([]os.FileInfo, len(dir.Entries))
	for ii, v := range dir.EntryNames {
		infos[ii] = &EntryInfo{
			Path:  pathpkg.Join(path, v),
			Entry: dir.Entries[ii],
		}
	}
	dir.RUnlock()
	return infos, nil
}

func (fs *memoryFileSystem) Mkdir(path string, perm os.FileMode) error {
	path = cleanPath(path)
	dir, base := pathpkg.Split(path)
	if base == "" {
		if dir == "/" || dir == "" {
			return os.ErrExist
		}
		return errNoEmptyNameDir
	}
	d, err := fs.dirEntry(dir)
	if err != nil {
		return err
	}
	d.Lock()
	defer d.Unlock()
	if d.removed {
		return os.ErrNotExist
	}
	if _, p, _ := d.Find(base); p >= 0 {
		return os.ErrExist
	}
	err = d.Add(base, &Dir{
		Mode:    os.ModeDir | perm,
		ModTime: time.Now(),
	})
	if err != nil {
		return os.ErrExist
	}
	return nil
}

func (fs *memoryFileSystem) Remove(path string) error {
	entry, dir, _, err := fs.entry(path)
	if err != nil {
		return err
	}
	if dir == nil {
		// path resolved to the root, which has no parent to unlink it from.
		// Without this the nil parent below is dereferenced, which is what
		// RemoveAll(fs, "/") used to panic on.
		return ErrRemoveRoot
	}
	// Take the parent first and the entry itself second. This is the only
	// place that holds two Dir locks at once, and it always takes the outer
	// directory first, so the nesting can't produce a cycle.
	dir.Lock()
	defer dir.Unlock()

	d, isDir := entry.(*Dir)
	if isDir {
		// The emptiness check and the unlink have to happen under one
		// continuous hold of d's write lock. Dir.Add always runs under that
		// lock, so releasing it in between would let an entry land in a
		// directory this call is about to detach: both operations report
		// success and the new entry is unreachable.
		d.Lock()
		defer d.Unlock()
		if len(d.Entries) > 0 {
			return fmt.Errorf("directory %s not empty", path)
		}
	}
	// Look the position up again, it might have changed since fs.entry.
	found, pos, err := dir.Find(pathpkg.Base(path))
	if err != nil {
		return err
	}
	if found != entry {
		// The name was rebound between the lookup and the lock, so nothing
		// checked above applies to what is there now. This matters whatever
		// was resolved, not just for directories: a file can be replaced by
		// a directory, and unlinking that without the emptiness check would
		// drop a whole subtree.
		return os.ErrNotExist
	}
	dir.EntryNames = append(dir.EntryNames[:pos], dir.EntryNames[pos+1:]...)
	dir.Entries = append(dir.Entries[:pos], dir.Entries[pos+1:]...)
	if isDir {
		// Anyone already waiting on d's lock to add an entry resolved the
		// path before this unlink and must not create into a detached
		// directory.
		d.removed = true
	}
	return nil
}

func (fs *memoryFileSystem) String() string {
	return "MemoryFileSystem"
}

func newMemory() *memoryFileSystem {
	fs := &memoryFileSystem{
		root: &Dir{
			Mode:    os.ModeDir | 0755,
			ModTime: time.Now(),
		},
	}
	return fs
}

// Memory returns an empty in memory VFS.
func Memory() VFS {
	return newMemory()
}

func cleanPath(path string) string {
	return strings.Trim(pathpkg.Clean("/"+path), "/")
}
