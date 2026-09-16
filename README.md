# vfs-kit

[![Go Reference](https://pkg.go.dev/badge/github.com/soulteary/vfs-kit.svg)](https://pkg.go.dev/github.com/soulteary/vfs-kit)
[![Go Report Card](.github/goreportcard.svg)](.github/goreportcard-report.md)
[![License](https://img.shields.io/badge/license-MPL%202.0-blue.svg)](LICENSE)
[![codecov](https://codecov.io/gh/soulteary/vfs-kit/graph/badge.svg)](https://codecov.io/gh/soulteary/vfs-kit)

[中文文档](README_CN.md)

Virtual file systems with read-write support in Go. Zero external dependencies.

Extracted from [rainycape/vfs](https://github.com/rainycape/vfs) and maintained
as `github.com/soulteary/vfs-kit` for use across soulteary projects (for example
[apt-proxy](https://github.com/soulteary/apt-proxy) and
[httpcache-kit](https://github.com/soulteary/httpcache-kit)).

## Features

- **One interface**: `Memory`, on-disk, archive, mounted and rewritten file systems all satisfy `VFS`
- **Read-write**: not only a read-only overlay — `OpenFile`, `Mkdir`, `Remove` and friends
- **Contained**: an on-disk or chrooted VFS rejects paths that would escape its root
- **Archives**: read and write tar, tar.gz, tar.bz2 and zip
- **Composable**: mount file systems at arbitrary points, rewrite paths, wrap read-only
- **`io/fs` bridge**: `AsReadOnlyFS` adapts any VFS to `fs.FS`
- **Zero dependencies**: standard library only

## Requirements

- **Go 1.27+** (`go.mod` declares `go 1.27.0`)

## Install

```bash
go get github.com/soulteary/vfs-kit
```

## Quick Start

```go
package main

import (
    "fmt"
    "log"

    vfs "github.com/soulteary/vfs-kit"
)

func main() {
    fs := vfs.Memory()

    if err := vfs.WriteFile(fs, "hello.txt", []byte("hello"), 0644); err != nil {
        log.Fatal(err)
    }

    data, err := vfs.ReadFile(fs, "hello.txt")
    if err != nil {
        log.Fatal(err)
    }
    fmt.Println(string(data))
}
```

## Creating a File System

```go
fs := vfs.Memory()                      // in-memory, read-write

fs, err := vfs.FS("/var/lib/myapp")     // on-disk, rooted at that directory
fs, err = vfs.Chroot("sub/dir", fs)     // a sub-tree of another VFS
tmp, err := vfs.TmpFS("myapp-")         // temporary on-disk; tmp.Close() removes it
fs = vfs.ReadOnly(fs)                   // reject every write

fs, err = vfs.Map(map[string]*vfs.File{ // in-memory from a literal map
    "a/b.txt": {Data: []byte("content"), Mode: 0644},
})
```

`TmpFS` returns a `TemporaryVFS`, which adds `Root()` and `Close()`:

```go
tmp, err := vfs.TmpFS("myapp-")
if err != nil {
    log.Fatal(err)
}
defer tmp.Close() // removes the directory
log.Println("working in", tmp.Root())
```

## Path Containment

An on-disk VFS (`FS`) and a chrooted one (`Chroot`) **reject a path that would
escape the root**, at every entry point — `Open`, `OpenFile`, `Stat`, `Lstat`,
`Mkdir` and `Remove` all go through the same check, so a path cannot be accepted
by one and rejected by another.

```go
v, _ := vfs.FS("/var/lib/myapp")

_, err := v.Open("../../etc/passwd")
errors.Is(err, vfs.ErrInvalidPath) // true
```

A `..` is matched as a **whole path segment**, so ordinary names work:

```go
vfs.ReadFile(v, "v1..2.json")        // fine
vfs.ReadFile(v, "..hidden")          // fine
vfs.ReadFile(v, "sub/../inside.txt") // fine — resolves inside the root
```

Errors returned from an on-disk VFS name the **VFS path**, not the deployment's
real root, so an error string does not disclose where the VFS lives. The wrapped
errno is preserved, so classification keeps working:

```go
import iofs "io/fs"

_, err := vfs.ReadFile(v, "missing.txt")
errors.Is(err, iofs.ErrNotExist) // true
vfs.IsNotExist(err)              // true
```

> **Containment is lexical.** A symlink stored inside the VFS that points outside
> it is still followed, exactly as in a real chroot. Do not rely on a VFS root as
> a security boundary against content you did not put there yourself.

## Reading and Writing

```go
data, err := vfs.ReadFile(fs, "path/to/file")
err = vfs.WriteFile(fs, "path/to/file", data, 0644)

err = vfs.MkdirAll(fs, "a/b/c", 0755)
err = vfs.RemoveAll(fs, "a")

exists := !vfs.IsNotExist(err)
```

Lower level, through the `VFS` interface:

```go
f, err := fs.Open("file")                                  // vfs.RFile
w, err := fs.OpenFile("file", os.O_CREATE|os.O_WRONLY, 0644) // vfs.WFile
info, err := fs.Stat("file")
info, err = fs.Lstat("link")
entries, err := fs.ReadDir("dir")
err = fs.Mkdir("dir", 0755)
err = fs.Remove("file")
name := fs.String()
```

### Walking

```go
err := vfs.Walk(fs, "/", func(fs vfs.VFS, path string, info os.FileInfo, err error) error {
    if err != nil {
        return err
    }
    if info.IsDir() && filepath.Base(path) == "vendor" {
        return vfs.ErrSkipDir // skip this directory
    }
    fmt.Println(path)
    return nil
})
```

### Copying

```go
err := vfs.Clone(dst, src)
```

Files or directories whose permissions are all zero become `0755` for directories
and `0644` for files. Use `Walk` directly when you need finer control.

## Archives

```go
// Read — by extension: .zip, .tar, .tar.gz, .tar.bz2
fs, err := vfs.Open("assets.tar.gz")

// Or explicitly, from a reader
fs, err = vfs.Tar(r)
fs, err = vfs.TarGzip(r)
fs, err = vfs.TarBzip2(r)
fs, err = vfs.Zip(r, size) // zip needs the total size

// Write
err = vfs.WriteTar(w, fs)
err = vfs.WriteTarGzip(w, fs)
err = vfs.WriteZip(w, fs)
```

An archive is loaded into an in-memory VFS, so it is read-write once open — the
changes simply do not reach the original file unless you write it back out.

## Composing

### Mounting

`Mounter` implements `VFS` and mounts other file systems at arbitrary points,
much like a UNIX filesystem. **The first mount must be at `"/"`.**

```go
m := &vfs.Mounter{}
if err := m.Mount(vfs.Memory(), "/"); err != nil {
    log.Fatal(err)
}

assets, _ := vfs.Open("assets.zip")
if err := m.Mount(vfs.ReadOnly(assets), "/static"); err != nil {
    log.Fatal(err)
}

data, err := vfs.ReadFile(m, "/static/logo.png")
err = m.Umount("/static")
```

### Rewriting paths

```go
// Serve "/v2/..." from "/..."
fs = vfs.Rewriter(fs, func(oldPath string) string {
    return strings.TrimPrefix(oldPath, "/v2")
})
```

### Read-only

```go
ro := vfs.ReadOnly(fs)
err := vfs.WriteFile(ro, "x", nil, 0644)
errors.Is(err, vfs.ErrReadOnly) // true
```

### Using a VFS as an `io/fs.FS`

```go
import iofs "io/fs"

var fsys iofs.FS = vfs.AsReadOnlyFS(v)

data, err := iofs.ReadFile(fsys, "path/to/file") // io/fs paths: no leading slash
```

`vfs.VFSPathFromFSName(name)` converts an `io/fs` name back to a VFS path.

## Compression

Some file systems support transparent per-file compression through the
`Compressor` interface:

```go
if err := vfs.Compress(fs); err != nil {
    log.Fatal(err) // not every file system supports it
}

if c, ok := entry.(vfs.Compressor); ok {
    c.SetCompressed(true)
    _ = c.IsCompressed()
}
```

`vfs.ModeCompress` is the mode bit used to mark a compressed entry.

## Concurrency

**File contents** are safe to use from several goroutines. Each `*File` carries
its own `sync.RWMutex`, and everything that reads or writes an entry's contents
or mode takes it — including opening a file while another goroutine closes a
handle for the same path.

**Directory structure is not fully synchronized.** `Mkdir`, `Remove` and
creating an entry each take the parent directory's lock, but `Remove` checks
whether a directory is empty *before* taking that directory's own lock. Removing
a directory while another goroutine creates an entry inside it is therefore a
data race, and the creation can land in a directory that is being detached.
**Serialize directory removal yourself, or keep the tree shape fixed while
several goroutines use it.** Everything else measured clean under `-race`:
reading a file while another writes it, two goroutines creating different files
in one directory, removing a *file* while it is open, `Mkdir` against `Remove`,
and `Stat` against a write.

Two more things the file lock does not cover:

- **A single open handle is not shared state.** `Read`, `Seek` and `Write` move
  one offset. Give each goroutine its own handle rather than passing one around.
- **Containment is lexical, not a lock.** Two goroutines writing the same path
  race for the final contents, the same as two processes writing one file.

The on-disk VFS inherits the operating system's semantics; it holds no locks of
its own.

## Errors

| Sentinel | Meaning |
|----------|---------|
| `ErrInvalidPath` | The path is malformed, or would escape the VFS root |
| `ErrReadOnly` | A write was attempted on a read-only VFS |
| `ErrReadOnlyFileSystem` | The underlying file system itself is read-only |
| `ErrWriteOnly` | A read was attempted on a write-only file |
| `ErrSkipDir` | Returned from a `WalkFunc` to skip a directory |

Match them with `errors.Is`. `vfs.IsExist` and `vfs.IsNotExist` classify
existence errors from any backend.

## Diagnostics

Closing a file can fail in a place with nowhere to return the error — a
finalizer, for instance. `LogCloseError` is called when that happens, and is a
no-op by default:

```go
vfs.LogCloseError = func(err error) { log.Printf("vfs close error: %v", err) }
```

## API Overview

| Function / Type | Description |
|-----------------|-------------|
| `Memory()` | In-memory read-write VFS |
| `FS(root)` | On-disk VFS rooted at `root` |
| `TmpFS(prefix)` | Temporary on-disk VFS (`TemporaryVFS`: `Root`, `Close`) |
| `Chroot(root, fs)` | A sub-tree of another VFS as its own root |
| `Map(files)` | In-memory VFS from a `map[string]*File` |
| `ReadOnly(fs)` | Read-only wrapper |
| `Rewriter(fs, fn)` | Rewrite paths on the way through |
| `Mounter` | Mount several VFSs at different points |
| `AsReadOnlyFS(v)` / `VFSPathFromFSName(n)` | `io/fs` bridge |
| `Open(filename)` | Load a `.zip`/`.tar`/`.tar.gz`/`.tar.bz2` into memory |
| `Tar`, `TarGzip`, `TarBzip2`, `Zip` | Load an archive from a reader |
| `WriteTar`, `WriteTarGzip`, `WriteZip` | Write a VFS out as an archive |
| `ReadFile`, `WriteFile`, `MkdirAll`, `RemoveAll` | Path-level helpers |
| `Walk`, `WalkFunc`, `ErrSkipDir` | Tree traversal |
| `Clone(dst, src)` | Copy every file between two VFSs |
| `Compress(fs)`, `Compressor`, `ModeCompress` | Transparent compression |
| `IsExist`, `IsNotExist` | Error classification |
| `File`, `Dir`, `Entry`, `EntryInfo` | In-memory entry types |
| `RFile`, `WFile`, `Opener`, `Container` | File and capability interfaces |
| `LogCloseError` | Hook for close failures with nowhere to return |

See the [package documentation](https://pkg.go.dev/github.com/soulteary/vfs-kit)
for full signatures.

## Upgrade Notes (v1.4.1)

No API was added, removed or changed. One data race is gone.

- **Reading an entry's contents or mode now takes the file's lock.** `*File`
  already carried an `RWMutex` and the writers took it — `(*file).Close` sets
  `Data`, and clears `ModeCompress` from `Mode`, under the write lock — but the
  readers did not, so the lock protected nothing. Opening a file while another
  goroutine closed a handle for the same path was a race, reported by `-race`
  between `fileData` and `Close`. `fileData`, `(*File).FileMode` and
  `(*file).IsCompressed` now take the read side. **If you run the in-memory VFS
  under `-race` in your own tests, this is the report that goes away.**
- **`fileData` decompresses outside the lock.** It snapshots `Data` and `Mode`
  under the read lock and releases before inflating, so a large compressed entry
  does not hold other openers off the file.
- **`(*file).Close` is safe to call from several goroutines.** It pre-checked
  the handle's `closed` flag outside the lock, while writing it under the lock,
  so concurrent closes of one handle raced on it. The check now happens under
  the lock; `Close` stays idempotent.

## Upgrade Notes (v1.4.0)

No API was added or removed. Some paths that used to resolve are now rejected,
and some that used to be rejected now work.

- **`Open` on an on-disk VFS no longer escapes the root.** It joined the root
  with an *uncleaned* relative path — `filepath.Clean("../x")` is still `"../x"`,
  and `Join` then resolved the `..` away — so `Open("../secret.txt")` read files
  outside the VFS root. Every other entry point was already anchored, which made
  the gap easy to miss: the one unguarded method was the main read entry point.
- **`Chroot` contains `..` too.** Its path handling was plain string
  concatenation with no `..` handling, so `"../.."` climbed straight back out into
  the parent VFS.
- **Every entry point shares one containment check.** `Open`, `OpenFile`,
  `Lstat`, `Stat`, `Mkdir` and `Remove` all route through it, so a path can no
  longer be accepted by one and rejected by another.
- **`..` is matched as a whole path segment.** It was a substring match, so
  legal names like `v1..2.json` and `..hidden` were rejected. **If you worked
  around that by renaming files, you can stop.**
- **Error strings name the VFS path, not the host path.** `*fs.PathError` values
  had the deployment's real root in them. The wrapped errno is preserved, so
  `errors.Is(err, fs.ErrNotExist)` (from `io/fs`) and `os.IsNotExist` keep working.
- **Documented, not changed**: containment is **lexical**. A symlink stored
  inside the VFS that points outside is still followed, as in a real chroot.

## Testing

```bash
go test ./...

# With coverage
go test ./... -coverprofile=coverage.out -covermode=atomic
go tool cover -func=coverage.out
```

## License

Mozilla Public License 2.0 — see [LICENSE](LICENSE). Derived from
[rainycape/vfs](https://github.com/rainycape/vfs).
