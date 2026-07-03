# go-logx

`go-logx` 是基于 `zap` 和 `lumberjack` 的 Go 日志公共库，用于统一项目内的结构化日志、按级别文件输出、日志滚动和独立 sink 通道。

## 安装

模块路径：

```text
github.com/sh-zou/go-logx
```

安装：

```powershell
go get github.com/sh-zou/go-logx@v1.0.0
```

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
| `maxSize` | 单个日志文件最大大小，单位 MB；`0` 表示普通追加文件 |
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
    _ = logx.Init("ssems-api", logx.Config{
        Level: "info",
        Dir:   "logs",
        Sinks: map[string]logx.SinkConfig{
            "access": {FileName: "access.log"},
        },
    })
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
```

`go-logx` 不内置 `access`、`script` 等业务 sink 名称。sink 名称完全由使用方配置决定，调用时显式传入同一个名称。

动态文件日志：

```go
scriptLog := logx.FileLogger("script.demo", "scripts/demo.log")
scriptLog.Info("script started")
```

`FileLogger` 的路径必须是应用日志目录下的相对路径，不能使用绝对路径或 `..` 跳出日志目录。

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
```
