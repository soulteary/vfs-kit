# vfs-kit

[![Go Reference](https://pkg.go.dev/badge/github.com/soulteary/vfs-kit.svg)](https://pkg.go.dev/github.com/soulteary/vfs-kit)
[![Go Report Card](https://goreportcard.com/badge/github.com/soulteary/vfs-kit)](https://goreportcard.com/report/github.com/soulteary/vfs-kit)
[![License](https://img.shields.io/badge/license-MPL%202.0-blue.svg)](LICENSE)
[![codecov](https://codecov.io/gh/soulteary/vfs-kit/graph/badge.svg)](https://codecov.io/gh/soulteary/vfs-kit)

Go 虚拟文件系统，支持读写。无第三方依赖。

本包源自 [rainycape/vfs](https://github.com/rainycape/vfs)，以 [github.com/soulteary/vfs-kit](https://github.com/soulteary/vfs-kit) 维护，供 soulteary 系列项目使用（如 [apt-proxy](https://github.com/soulteary/apt-proxy)）。

## 安装

```bash
go get github.com/soulteary/vfs-kit
```

指定版本（打 tag 后）：

```bash
go get github.com/soulteary/vfs-kit@v0.1.0
```

## 使用

详见 [包文档](https://pkg.go.dev/github.com/soulteary/vfs-kit)。示例：

```go
import "github.com/soulteary/vfs-kit"

fs := vfs.Memory()
vfs.WriteFile(fs, "hello.txt", []byte("hello"), 0644)
data, _ := vfs.ReadFile(fs, "hello.txt")
```

可选：设置 `vfs.LogCloseError` 以记录文件关闭错误（如 finalizer 中）：

```go
vfs.LogCloseError = func(err error) { log.Printf("vfs close error: %v", err) }
```

## API 概览

| 函数 / 类型 | 说明 |
|-------------|------|
| `Memory()` | 内存 VFS |
| `FS(root)` | 以 `root` 为根的磁盘 VFS |
| `TmpFS(prefix)` | 临时磁盘 VFS |
| `Chroot(root, fs)` | 以不同根目录包装的 VFS |
| `ReadOnly(fs)` | 只读包装 |
| `AsReadOnlyFS(v)` | 只读场景下的 `io/fs.FS` 适配 |
| `MkdirAll`, `ReadFile`, `WriteFile`, `Walk`, `IsNotExist` | 工具函数 |

## 协议

Mozilla Public License Version 2.0。见 [LICENSE](LICENSE)。
