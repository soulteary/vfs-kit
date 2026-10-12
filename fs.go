package vfs

import (
	"errors"
	"fmt"
	iofs "io/fs"
	"os"
	"path/filepath"
	"strings"
)

// IMPORTANT: Note about wrapping os. functions: os.Open, os.OpenFile etc... will return a non-nil
// interface pointing to a nil instance in case of error (whoever decided this disctintion in Go
// was a good idea deservers to be hung by his thumbs). This is highly undesirable, since users
// can't rely on checking f != nil to know if a correct handle was returned. That's why the
// methods in fileSystem do the error checking themselves and return a true nil in case of error.

type fileSystem struct {
	root      string
	temporary bool
}

// securePath maps a VFS path to a host path that is guaranteed to live inside
// fs.root. It is the single containment check for every entry point of
// fileSystem: Open, OpenFile, Lstat, Stat, Mkdir and Remove all go through it,
// so a path can never be accepted by one entry point and rejected by another.
//
// A path that tries to climb above the VFS root is rejected with ErrInvalidPath
// rather than silently rewritten, so callers get a diagnosable error.
//
// Note: containment is lexical. A symlink stored *inside* the VFS that points
// outside of it is still followed by the operating system, exactly as it would
// be inside a real chroot. Do not rely on a fileSystem as a security boundary
// for a tree that untrusted code can create symlinks in.
func (fs *fileSystem) securePath(name string) (string, error) {
	cleaned := filepath.Clean(filepath.FromSlash(name))
	if containsDotDot(cleaned) {
		return "", ErrInvalidPath
	}
	full := filepath.Join(fs.root, cleaned)
	if !isUnderRoot(fs.root, full) {
		return "", ErrInvalidPath
	}
	return full, nil
}

// Root returns the root directory of the fileSystem, as an
// absolute path native to the current operating system.
func (fs *fileSystem) Root() string {
	return fs.root
}

// IsTemporary returns whether the fileSystem is temporary.
func (fs *fileSystem) IsTemporary() bool {
	return fs.temporary
}

func (fs *fileSystem) Open(path string) (RFile, error) {
	full, err := fs.securePath(path)
	if err != nil {
		return nil, err
	}
	f, err := os.Open(full)
	if err != nil {
		return nil, fs.hideRoot(err)
	}
	return f, nil
}

var ErrInvalidPath = errors.New("invalid path: attempt to access parent directory")

// containsDotDot reports whether path contains ".." as a whole path segment.
// It deliberately does not use strings.Contains: names such as "v1..2.json" or
// "..hidden" are legal file names and must not be rejected.
func containsDotDot(path string) bool {
	for _, seg := range strings.Split(filepath.ToSlash(path), "/") {
		if seg == ".." {
			return true
		}
	}
	return false
}

// hideRoot rewrites the host path in a *fs.PathError back to the path the
// caller asked for, so error strings do not leak the filesystem's real root.
// The wrapped errno is preserved, so errors.Is(err, fs.ErrNotExist) and
// os.IsNotExist keep working.
func (fs *fileSystem) hideRoot(err error) error {
	var pathErr *iofs.PathError
	if !errors.As(err, &pathErr) {
		return err
	}
	rel, relErr := filepath.Rel(fs.root, pathErr.Path)
	if relErr != nil {
		return err
	}
	return &iofs.PathError{Op: pathErr.Op, Path: filepath.ToSlash(filepath.Join("/", rel)), Err: pathErr.Err}
}

func isUnderRoot(root, path string) bool {
	rel, err := filepath.Rel(root, path)
	if err != nil {
		return false
	}
	return !strings.HasPrefix(rel, ".."+string(filepath.Separator)) && rel != ".."
}

func (fs *fileSystem) OpenFile(path string, flag int, mode os.FileMode) (WFile, error) {
	full, err := fs.securePath(path)
	if err != nil {
		return nil, err
	}

	f, err := os.OpenFile(full, flag, mode)
	if err != nil {
		return nil, fs.hideRoot(err)
	}

	return f, nil
}

func (fs *fileSystem) Lstat(path string) (os.FileInfo, error) {
	full, err := fs.securePath(path)
	if err != nil {
		return nil, err
	}
	info, err := os.Lstat(full)
	if err != nil {
		return nil, fs.hideRoot(err)
	}
	return info, nil
}

func (fs *fileSystem) Stat(path string) (os.FileInfo, error) {
	full, err := fs.securePath(path)
	if err != nil {
		return nil, err
	}
	info, err := os.Stat(full)
	if err != nil {
		return nil, fs.hideRoot(err)
	}
	return info, nil
}

func (fs *fileSystem) ReadDir(path string) ([]os.FileInfo, error) {
	// Use io/fs.ReadDir for read-only path: same API, standard implementation (Go 1.16+).
	relName := filepath.ToSlash(filepath.Clean(path))
	if relName == "" || relName == "." || relName == "/" {
		relName = "."
	} else if strings.HasPrefix(relName, "/") {
		relName = relName[1:]
	}
	entries, err := iofs.ReadDir(os.DirFS(fs.root), relName)
	if err != nil {
		return nil, err
	}
	files := make([]os.FileInfo, 0, len(entries))
	for _, entry := range entries {
		info, err := entry.Info()
		if err != nil {
			return nil, err
		}
		files = append(files, info)
	}
	return files, nil
}

func (fs *fileSystem) Mkdir(path string, perm os.FileMode) error {
	full, err := fs.securePath(path)
	if err != nil {
		return err
	}
	return fs.hideRoot(os.Mkdir(full, perm))
}

func (fs *fileSystem) Remove(path string) error {
	full, err := fs.securePath(path)
	if err != nil {
		return err
	}
	return fs.hideRoot(os.Remove(full))
}

func (fs *fileSystem) String() string {
	return fmt.Sprintf("fileSystem: %s", fs.root)
}

// Close is a no-op on non-temporary filesystems. On temporary
// ones (as returned by TmpFS), it removes all the temporary files.
func (f *fileSystem) Close() error {
	if f.temporary {
		return os.RemoveAll(f.root)
	}
	return nil
}

func newFS(root string) (*fileSystem, error) {
	abs, err := filepath.Abs(root)
	if err != nil {
		return nil, err
	}
	return &fileSystem{root: abs}, nil
}

// FS returns a VFS at the given path, which must be provided
// as native path of the current operating system. The path might be
// either absolute or relative, but the fileSystem will be anchored
// at the absolute path represented by root at the time of the function
// call.
func FS(root string) (VFS, error) {
	return newFS(root)
}

// TmpFS returns a temporary file system with the given prefix and its root
// directory name, which might be empty. The temporary file system is created
// in the default temporary directory for the operating system. Once you're
// done with the temporary filesystem, you might can all its files by calling
// its Close method.
func TmpFS(prefix string) (TemporaryVFS, error) {
	dir, err := os.MkdirTemp("", prefix)
	if err != nil {
		return nil, err
	}
	fs, err := newFS(dir)
	if err != nil {
		return nil, err
	}
	fs.temporary = true
	return fs, nil
}
