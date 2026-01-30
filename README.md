# vfs-kit

Virtual File Systems with read-write support in Go. Zero external dependencies.

This package is extracted from [rainycape/vfs](https://github.com/rainycape/vfs) and maintained as [github.com/soulteary/vfs-kit](https://github.com/soulteary/vfs-kit) for use across soulteary projects (e.g. [apt-proxy](https://github.com/soulteary/apt-proxy)).

## Install

```bash
go get github.com/soulteary/vfs-kit
```

For a specific version (after tagging):

```bash
go get github.com/soulteary/vfs-kit@v0.1.0
```

## Usage

See [package documentation](https://pkg.go.dev/github.com/soulteary/vfs-kit) for API details. Example:

```go
import "github.com/soulteary/vfs-kit"

fs := vfs.Memory()
vfs.WriteFile(fs, "hello.txt", []byte("hello"), 0644)
data, _ := vfs.ReadFile(fs, "hello.txt")
```

Optional: set `vfs.LogCloseError` to log file-close errors (e.g. from a finalizer):

```go
vfs.LogCloseError = func(err error) { log.Printf("vfs close error: %v", err) }
```

## API overview

| Function / Type | Description |
|-----------------|-------------|
| `Memory()` | In-memory VFS |
| `FS(root)` | On-disk VFS at `root` |
| `TmpFS(prefix)` | Temporary on-disk VFS |
| `Chroot(root, fs)` | VFS with a different root |
| `ReadOnly(fs)` | Read-only wrapper |
| `AsReadOnlyFS(v)` | `io/fs.FS` adapter for read-only use |
| `MkdirAll`, `ReadFile`, `WriteFile`, `Walk`, `IsNotExist` | Shorthand utilities |

## License

Mozilla Public License Version 2.0. See [LICENSE](LICENSE).
