# vfs-kit

[![Go Reference](https://pkg.go.dev/badge/github.com/soulteary/vfs-kit.svg)](https://pkg.go.dev/github.com/soulteary/vfs-kit)
[![Go Report Card](.github/goreportcard.svg)](.github/goreportcard-report.md)
[![License](https://img.shields.io/badge/license-MPL%202.0-blue.svg)](LICENSE)
[![codecov](https://codecov.io/gh/soulteary/vfs-kit/graph/badge.svg)](https://codecov.io/gh/soulteary/vfs-kit)

[English](README.md)

Go 的虚拟文件系统，支持读写。零外部依赖。

源自 [rainycape/vfs](https://github.com/rainycape/vfs)，以
`github.com/soulteary/vfs-kit` 维护，供 soulteary 的项目使用（例如
[apt-proxy](https://github.com/soulteary/apt-proxy) 和
[httpcache-kit](https://github.com/soulteary/httpcache-kit)）。

## 特性

- **统一接口**：`Memory`、磁盘、归档、挂载、路径重写的文件系统都满足 `VFS`
- **可读写**：不只是只读覆盖层——有 `OpenFile`、`Mkdir`、`Remove` 等
- **有边界**：磁盘和 chroot 的 VFS 会拒绝逃出根目录的路径
- **归档**：读写 tar、tar.gz、tar.bz2 和 zip
- **可组合**：把文件系统挂到任意挂载点、重写路径、包成只读
- **`io/fs` 桥接**：`AsReadOnlyFS` 把任意 VFS 适配成 `fs.FS`
- **零依赖**：仅使用标准库

## 环境要求

- **Go 1.27+**（`go.mod` 声明 `go 1.27.0`）

## 安装

```bash
go get github.com/soulteary/vfs-kit
```

## 快速开始

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

## 创建文件系统

```go
fs := vfs.Memory()                      // 内存，可读写

fs, err := vfs.FS("/var/lib/myapp")     // 磁盘，以该目录为根
fs, err = vfs.Chroot("sub/dir", fs)     // 另一个 VFS 的子树
tmp, err := vfs.TmpFS("myapp-")         // 临时磁盘目录；tmp.Close() 会删除它
fs = vfs.ReadOnly(fs)                   // 拒绝一切写入

fs, err = vfs.Map(map[string]*vfs.File{ // 由字面量 map 构造的内存 VFS
    "a/b.txt": {Data: []byte("content"), Mode: 0644},
})
```

`TmpFS` 返回 `TemporaryVFS`，它额外提供 `Root()` 和 `Close()`：

```go
tmp, err := vfs.TmpFS("myapp-")
if err != nil {
    log.Fatal(err)
}
defer tmp.Close() // 删除该目录
log.Println("工作目录:", tmp.Root())
```

## 路径边界

磁盘 VFS（`FS`）和 chroot 后的 VFS（`Chroot`）会在**每一个入口**上**拒绝会逃出根目录的
路径**——`Open`、`OpenFile`、`Stat`、`Lstat`、`Mkdir` 和 `Remove` 都走同一个检查，因此
一条路径不可能被其中一个接受、又被另一个拒绝。

```go
v, _ := vfs.FS("/var/lib/myapp")

_, err := v.Open("../../etc/passwd")
errors.Is(err, vfs.ErrInvalidPath) // true
```

`..` 是按**完整路径段**匹配的，因此正常名字都能用：

```go
vfs.ReadFile(v, "v1..2.json")        // 可以
vfs.ReadFile(v, "..hidden")          // 可以
vfs.ReadFile(v, "sub/../inside.txt") // 可以 —— 解析后仍在根目录内
```

磁盘 VFS 返回的错误里写的是 **VFS 路径**，不是部署环境真实的根目录，因此错误信息不会
泄露 VFS 所在的位置。被包装的 errno 会保留，所以分类判断照常可用：

```go
import iofs "io/fs"

_, err := vfs.ReadFile(v, "missing.txt")
errors.Is(err, iofs.ErrNotExist) // true
vfs.IsNotExist(err)              // true
```

> **边界是词法层面的。** 存放在 VFS 内部、但指向外部的符号链接仍然会被跟随，这与真实的
> chroot 一致。不要把 VFS 根目录当成针对"并非你自己放进去的内容"的安全边界。

## 读与写

```go
data, err := vfs.ReadFile(fs, "path/to/file")
err = vfs.WriteFile(fs, "path/to/file", data, 0644)

err = vfs.MkdirAll(fs, "a/b/c", 0755)
err = vfs.RemoveAll(fs, "a")
```

更底层一些，通过 `VFS` 接口：

```go
f, err := fs.Open("file")                                    // vfs.RFile
w, err := fs.OpenFile("file", os.O_CREATE|os.O_WRONLY, 0644) // vfs.WFile
info, err := fs.Stat("file")
info, err = fs.Lstat("link")
entries, err := fs.ReadDir("dir")
err = fs.Mkdir("dir", 0755)
err = fs.Remove("file")
name := fs.String()
```

### 遍历

```go
err := vfs.Walk(fs, "/", func(fs vfs.VFS, path string, info os.FileInfo, err error) error {
    if err != nil {
        return err
    }
    if info.IsDir() && filepath.Base(path) == "vendor" {
        return vfs.ErrSkipDir // 跳过该目录
    }
    fmt.Println(path)
    return nil
})
```

### 复制

```go
err := vfs.Clone(dst, src)
```

权限位全为零的文件或目录会被设为目录 `0755`、文件 `0644`。需要更细的控制请直接用 `Walk`。

## 归档

```go
// 读取 —— 按扩展名识别：.zip、.tar、.tar.gz、.tar.bz2
fs, err := vfs.Open("assets.tar.gz")

// 或者显式地从 reader 读取
fs, err = vfs.Tar(r)
fs, err = vfs.TarGzip(r)
fs, err = vfs.TarBzip2(r)
fs, err = vfs.Zip(r, size) // zip 需要总大小

// 写出
err = vfs.WriteTar(w, fs)
err = vfs.WriteTarGzip(w, fs)
err = vfs.WriteZip(w, fs)
```

归档会被加载进一个内存 VFS，因此打开之后它是可读写的——只是这些改动不会回写到原文件，
除非你自己再写出去。

## 组合

### 挂载

`Mounter` 实现了 `VFS`，可以把其他文件系统挂到任意挂载点，用法类似 UNIX 文件系统。
**第一个挂载点必须是 `"/"`。**

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

### 重写路径

```go
// 用 "/..." 的内容来服务 "/v2/..."
fs = vfs.Rewriter(fs, func(oldPath string) string {
    return strings.TrimPrefix(oldPath, "/v2")
})
```

### 只读

```go
ro := vfs.ReadOnly(fs)
err := vfs.WriteFile(ro, "x", nil, 0644)
errors.Is(err, vfs.ErrReadOnly) // true
```

### 作为 `io/fs.FS` 使用

```go
import iofs "io/fs"

var fsys iofs.FS = vfs.AsReadOnlyFS(v)

data, err := iofs.ReadFile(fsys, "path/to/file") // io/fs 的路径不带前导斜杠
```

`vfs.VFSPathFromFSName(name)` 可以把 `io/fs` 的名字转回 VFS 路径。

## 压缩

部分文件系统通过 `Compressor` 接口支持按文件的透明压缩：

```go
if err := vfs.Compress(fs); err != nil {
    log.Fatal(err) // 并非所有文件系统都支持
}

if c, ok := entry.(vfs.Compressor); ok {
    c.SetCompressed(true)
    _ = c.IsCompressed()
}
```

`vfs.ModeCompress` 是用来标记"已压缩"条目的 mode 位。

## 并发

**文件内容**可以在多个 goroutine 中安全使用。每个 `*File` 自带 `sync.RWMutex`，凡是读写条目内容或 mode 的地方都会持有它 —— 包括在另一个 goroutine 正关闭同路径句柄时打开该文件。

**目录结构同样是同步的。** `Mkdir`、`Remove` 和创建条目都会持有父目录的锁，而 `Remove` 会在目标目录自身的写锁内完成「判空」与「摘除」这两步。在另一个 goroutine 往某目录中创建条目的同时删除该目录，只会有两种结果之一，不会两者兼有：要么创建先落地、删除返回 `directory ... not empty`，要么删除先落地、创建返回 `os.ErrNotExist`。创建绝不会被悄悄丢进一个已被摘除的目录里。**如果你在其他 goroutine 写入的同时删除目录，请处理 `OpenFile` 和 `Mkdir` 返回的 `os.ErrNotExist`。**

以下组合在 `-race` 下均干净：一边读文件一边写、两个 goroutine 在同一目录下创建不同文件、删除一个正被打开的**文件**、`Mkdir` 与 `Remove`、`Stat` 与写入，以及在往某目录创建条目的同时删除该目录。

文件锁另外**不**覆盖两件事：

- **单个已打开的句柄不是共享状态。** `Read`、`Seek`、`Write` 移动的是同一个偏移量。请给每个 goroutine 各自的句柄，而不是传递同一个。
- **路径包含是词法的，不是锁。** 两个 goroutine 写同一路径会争抢最终内容，与两个进程写同一个文件无异。

磁盘 VFS 沿用操作系统的语义，自身不持有任何锁。

## 错误

| 哨兵错误 | 含义 |
|----------|------|
| `ErrInvalidPath` | 路径格式非法，或会逃出 VFS 根目录 |
| `ErrReadOnly` | 对只读 VFS 执行了写操作 |
| `ErrReadOnlyFileSystem` | 底层文件系统本身是只读的 |
| `ErrWriteOnly` | 对只写文件执行了读操作 |
| `ErrSkipDir` | 在 `WalkFunc` 中返回它以跳过某个目录 |
| `ErrRemoveRoot` | 对文件系统根目录调用了 `Remove`，而根目录没有父目录 |

请用 `errors.Is` 判断。`vfs.IsExist` 和 `vfs.IsNotExist` 可对任意后端的存在性错误做分类。

## 诊断

关闭文件可能在"无处返回错误"的地方失败——比如 finalizer 里。发生这种情况时会调用
`LogCloseError`，它默认是空操作：

```go
vfs.LogCloseError = func(err error) { log.Printf("vfs close error: %v", err) }
```

## API 概览

| 函数 / 类型 | 说明 |
|-------------|------|
| `Memory()` | 内存可读写 VFS |
| `FS(root)` | 以 `root` 为根的磁盘 VFS |
| `TmpFS(prefix)` | 临时磁盘 VFS（`TemporaryVFS`：`Root`、`Close`） |
| `Chroot(root, fs)` | 把另一个 VFS 的子树作为自己的根 |
| `Map(files)` | 由 `map[string]*File` 构造的内存 VFS |
| `ReadOnly(fs)` | 只读包装 |
| `Rewriter(fs, fn)` | 在访问过程中重写路径 |
| `Mounter` | 把多个 VFS 挂到不同挂载点 |
| `AsReadOnlyFS(v)` / `VFSPathFromFSName(n)` | `io/fs` 桥接 |
| `Open(filename)` | 把 `.zip`/`.tar`/`.tar.gz`/`.tar.bz2` 加载进内存 |
| `Tar`、`TarGzip`、`TarBzip2`、`Zip` | 从 reader 加载归档 |
| `WriteTar`、`WriteTarGzip`、`WriteZip` | 把 VFS 写成归档 |
| `ReadFile`、`WriteFile`、`MkdirAll`、`RemoveAll` | 路径级辅助函数 |
| `Walk`、`WalkFunc`、`ErrSkipDir` | 树遍历 |
| `Clone(dst, src)` | 在两个 VFS 之间复制全部文件 |
| `Compress(fs)`、`Compressor`、`ModeCompress` | 透明压缩 |
| `IsExist`、`IsNotExist` | 错误分类 |
| `File`、`Dir`、`Entry`、`EntryInfo` | 内存条目类型 |
| `RFile`、`WFile`、`Opener`、`Container` | 文件与能力接口 |
| `LogCloseError` | 无处返回的关闭失败钩子 |

完整签名见[包文档](https://pkg.go.dev/github.com/soulteary/vfs-kit)。

## 升级说明（v1.4.1）

没有移除或改变任何导出 API，新增了一个哨兵错误。修掉了两个数据竞态和一个 panic。

- **读取条目的内容或 mode 现在会持有该文件的锁。** `*File` 本来就带着 `RWMutex`，写方也确实拿了 —— `(*file).Close` 在写锁内设置 `Data`、并从 `Mode` 中清除 `ModeCompress` —— 但读方没拿，所以这把锁什么都没保护到。在另一个 goroutine 关闭同路径句柄时打开文件会触发竞态，`-race` 会在 `fileData` 与 `Close` 之间报出。现在 `fileData`、`(*File).FileMode` 和 `(*file).IsCompressed` 都会取读侧。**如果你在自己的测试里用 `-race` 跑内存 VFS，消失的就是这条报告。**
- **`fileData` 在锁外解压。** 它在读锁内快照 `Data` 与 `Mode`，释放之后再解压，因此一个较大的压缩条目不会把其他打开者挡在外面。
- **`(*file).Close` 可以从多个 goroutine 调用。** 它此前在锁外预检句柄的 `closed` 标志，而该标志是在锁内写入的，因此同一句柄上的并发 `Close` 会在它上面竞态。现在检查移到了锁内；`Close` 仍然是幂等的。
- **删除目录与创建条目之间现在是原子的。** `Remove` 在取得目标目录自身的锁**之前**就读取它的条目数，而 `Dir.Add` 是在该锁内修改同一个切片的 —— `-race` 会把它报成 `Dir.Add` 与 `Remove` 之间的一对写/读。同一个间隙还允许一个目录在另一个 goroutine 刚往里创建完条目之后被摘除：两次调用都返回成功，而新条目已经不可达，在双 goroutine 循环中约每 2000 次丢 3 次。现在 `Remove` 会在目标目录的写锁内完成判空与摘除，并把该目录标记为已摘除。**在删除发生前就已解析到该目录的创建操作，现在会返回 `os.ErrNotExist`，而不是成功写进一个不存在的地方。**
- **`Remove` 不会再摘除一个它没有检查过的条目。** 它在锁定父目录之后是按名字重新查一遍的，所以这期间被重新绑定的名字会被照单摘除，不论它此刻指向什么 —— 判空判的是旧的那个占用者，而重新绑定还可能改变类型：一个解析到**文件**的 `Remove` 可以就此摘掉一整个目录，且从未检查它是否为空。现在 `Remove` 会确认自己摘除的正是它解析到的那个条目，否则返回 `os.ErrNotExist`。
- **对文件系统根目录调用 `Remove` 现在返回错误，而不是 panic。** 根目录没有父目录可供摘除，而那个空的父目录指针此前会被解引用。`RemoveAll(fs, "/")` 通过本包自己的辅助函数就会走到这里。现在它会清空整棵树并返回新增的 `ErrRemoveRoot` 哨兵错误。**如果你此前用 `RemoveAll(fs, "/")` 清空内存 VFS，它不再 panic，而是开始返回这个错误；可以用 `errors.Is` 忽略它。**
- **`Dir` 新增了一个未导出字段**，用于记录它是否已被摘除。带字段名的结构体字面量不受影响；你自己代码里不带字段名的 `vfs.Dir{...}` 字面量会编译不过 —— 对来自其他包的结构体，`go vet` 本来就会对这种写法告警。

## 升级说明（v1.4.0）

没有新增或删除任何 API。一些此前能解析成功的路径现在会被拒绝，一些此前被拒绝的现在可用了。

- **磁盘 VFS 上的 `Open` 不再逃出根目录。** 它此前把根目录和一个*未清理*的相对路径拼在
  一起——`filepath.Clean("../x")` 仍然是 `"../x"`，而 `Join` 接着把 `..` 解析掉了——于是
  `Open("../secret.txt")` 能读到 VFS 根目录之外的文件。其他入口早就做了锚定，这让这个
  缺口很容易被忽略：唯一没设防的方法恰好是主要的读入口。
- **`Chroot` 同样会约束 `..`。** 它的路径处理此前是纯字符串拼接，完全没处理 `..`，于是
  `"../.."` 直接爬回了父 VFS。
- **所有入口共用同一个边界检查。** `Open`、`OpenFile`、`Lstat`、`Stat`、`Mkdir` 和
  `Remove` 都走它，因此一条路径不可能被其中一个接受、又被另一个拒绝。
- **`..` 按完整路径段匹配。** 它此前是子串匹配，于是 `v1..2.json`、`..hidden` 这类合法
  名字会被拒绝。**如果你是靠改名来绕开这一点的，现在可以不用了。**
- **错误信息里写的是 VFS 路径，而不是宿主路径。** `*fs.PathError` 里此前带着部署环境
  真实的根目录。被包装的 errno 会保留，所以 `errors.Is(err, fs.ErrNotExist)`
  （来自 `io/fs`）和 `os.IsNotExist` 照常可用。
- **只是补充文档、行为未变**：边界是**词法层面**的。存放在 VFS 内部、但指向外部的符号
  链接仍然会被跟随，这与真实的 chroot 一致。

## 测试

```bash
go test ./...

# 带覆盖率
go test ./... -coverprofile=coverage.out -covermode=atomic
go tool cover -func=coverage.out
```

## 许可证

Mozilla Public License 2.0 —— 详见 [LICENSE](LICENSE)。派生自
[rainycape/vfs](https://github.com/rainycape/vfs)。
