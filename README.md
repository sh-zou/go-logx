# go-logx

`go-logx` 是基于 `zap` 和 `lumberjack` 的 Go 日志公共库，用于统一项目内的结构化日志、按级别文件输出、日志滚动和独立 sink 通道。

## 安装

模块路径：

```text
github.com/sh-zou/go-logx
```

安装：

```powershell
go get github.com/sh-zou/go-logx@v1.0.5
```

`v1.0.5` 需要 Go 1.24 或更高版本，包含 `OpenFileLogger`、`Flush`、`Shutdown` 和 `ErrNotInitialized`。

仓库地址：

```text
https://github.com/sh-zou/go-logx
```

## 配置

推荐配置：

```yaml
log:
  level: info
  dir: logs
  consoleEnabled: true
  maxSize: 1000
  maxBackups: 10
  maxAge: 10
  compress: true

  sinks:
    access:
      fileName: access.log
      consoleEnabled: false
    script:
      dir: scripts
      consoleEnabled: false
```

字段说明：

| 字段 | 说明 |
| --- | --- |
| `level` | 主日志级别，支持 `debug/info/warn/error` |
| `dir` | 日志根目录，默认 `logs` |
| `consoleEnabled` | 主日志是否输出控制台 |
| `maxSize` | 单个日志文件最大大小，单位 MB；大于 `0` 时同时在本地午夜主动轮转，`0` 表示普通追加文件 |
| `maxBackups` | 滚动日志最大保留数量 |
| `maxAge` | 滚动日志最大保留天数 |
| `compress` | 是否压缩滚动历史日志 |
| `sinks.<name>.dir` | sink 子目录，可选；例如 `scripts` 对应 `logs/<app>/scripts/` |
| `sinks.<name>.fileName` | sink 文件名，可选；默认 `<name>.log` |
| `sinks.<name>.consoleEnabled` | 当前 sink 是否输出控制台 |

## 使用

```go
package main

import (
    "github.com/sh-zou/go-logx/logx"
    "go.uber.org/zap"
)

func main() {
    if err := logx.Init("ssems-api", logx.Config{
        Level: "info",
        Dir:   "logs",
        Sinks: map[string]logx.SinkConfig{
            "access": {FileName: "access.log"},
        },
    }); err != nil {
        return
    }
    defer logx.Close()

    logx.Info("service started", zap.String("component", "api"))
    logx.Named("internal.app.bootstrap").Info("bootstrap completed")
    logx.SinkNamed("access", "http.access").Info("request completed")
}
```

## API

主日志：

```go
logx.Init(appName, cfg)
logx.Sync()
logx.Close()
logx.Flush()
logx.Shutdown()
logx.L()
logx.Sugar()
logx.Named("module")
logx.Package()
logx.Module("module")
```

快捷方法：

```go
logx.Debug("message")
logx.Info("message")
logx.Warn("message")
logx.Error("message")
logx.Debugf("message %s", value)
logx.Infof("message %s", value)
```

独立 sink：

```go
logx.Sink("access")
logx.SinkNamed("access", "http.access")
logx.FileLogger("script.demo", "scripts/demo.log")
logx.OpenFileLogger("script.demo", "scripts/demo.log")
```

`go-logx` 不内置 `access`、`script` 等业务 sink 名称。sink 名称完全由使用方配置决定，调用时显式传入同一个名称。

动态文件日志：

```go
scriptLog, err := logx.OpenFileLogger("script.demo", "scripts/demo.log")
if err != nil {
    return
}
scriptLog.Info("script started")
```

`OpenFileLogger` 会在返回前校验路径边界和目标文件可写性，并返回路径校验和文件创建错误，推荐新代码使用。`FileLogger` 保留兼容行为，失败时返回 no-op logger。

动态文件路径必须是应用日志目录下的相对路径，不能使用绝对路径或 `..` 跳出日志目录。`appName` 和 `Config.FileName` 只能是单个目录名称；sink 的 `dir` 可以是应用日志目录内的相对子目录。

动态文件 logger 只能在 `Init` 成功后创建。`Shutdown` 或 `Close` 之后，`OpenFileLogger` 返回 `ErrNotInitialized`，`FileLogger` 返回 no-op logger，直到下一次成功调用 `Init`。

路径校验会拒绝调用时已经存在的越界符号链接，但它不是文件系统沙箱。日志根目录及其父目录必须由应用信任，并禁止不受信任的本机用户或进程修改目录结构；如果无法满足这个条件，应由部署环境提供隔离后的专用日志目录。

## 错误处理

`Sync` 和 `Close` 保持原有无返回值 API。需要处理磁盘同步或文件关闭错误时，使用：

```go
if err := logx.Flush(); err != nil {
    // handle sync error
}
if err := logx.Shutdown(); err != nil {
    // handle sync or close error
}
```

`Shutdown` 与 `Close` 一样会释放全部受管理资源。二者只应选择一个作为应用退出流程。

重复调用 `Init` 时，库会先切换到新配置，再返回上一代 logger 的同步或关闭错误。因此 `Init` 返回清理错误时，新配置已经生效；调用方应记录或处理该错误，不要据此重复初始化。

`Flush` 和 `Shutdown` 会同步文件 writer。控制台输出直接写入 stdout，不执行不受终端或管道支持的 `fsync`，因此不会产生伪同步错误。

## 输出结构

主日志默认输出：

```text
logs/
└─ ssems-api/
   ├─ debug.log
   ├─ info.log
   ├─ warn.log
   └─ error.log
```

如果配置 access sink：

```yaml
sinks:
  access:
    fileName: access.log
```

则输出：

```text
logs/
└─ ssems-api/
   └─ access.log
```

如果配置 script sink：

```yaml
sinks:
  script:
    dir: scripts
    fileName: script.log
```

则输出：

```text
logs/
└─ ssems-api/
   └─ scripts/
      └─ script.log
```

## 开发验证

```powershell
go test ./...
go vet ./...
go test -race ./...
```
