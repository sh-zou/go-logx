# go-logx 优化实施方案

## 目标

修复审查中发现的并发生命周期、文件所有权、路径边界、caller 准确性和错误可观测性问题，同时保持现有 v1 调用方式可用。每个任务都包含对应回归测试，完成任一任务后仓库都应保持可构建、可测试。

## 兼容性决策

- 保留 `Init`、`FileLogger`、`Sync`、`Close` 的现有调用方式。
- 新增显式错误 API：`OpenFileLogger(name, relativePath) (*zap.Logger, error)`、`Flush() error`、`Shutdown() error`。旧 API 委托给新 API，以兼容现有调用方。
- 非法 `appName`、`SinkConfig.Dir` 和动态相对路径改为拒绝，而不是自动清洗到其他目录。这属于安全修复。
- 保留当前“按大小滚动并在本地午夜主动滚动”的行为，并让初始化后创建的动态 logger 同样参与。
- `Close` 后继续按现有文档将 zap 全局 logger 置为 no-op，但恢复标准库 `log` 在首次 `Init` 前的输出、flags 和 prefix。

## 依赖关系

```text
任务 1：轮转生命周期
  ├─> 任务 2：动态轮转注册
  └─> 任务 4：单路径单 writer

任务 3：路径边界与显式创建错误
  └─> 任务 4：规范化路径 writer 注册

任务 5、6、7 可在任务 1 后独立实施
任务 8 依赖所有行为变更完成
```

## 第一阶段：并发与资源所有权

### 任务 1：封装轮转生命周期，消除半关闭状态

**说明：** 用私有 `rotationManager` 管理 rotator、停止通道和完成信号。manager 不访问全局 `mu`，因此 `Init`/`Close` 可以在持有全局锁时安全停止并等待，不再通过临时解锁暴露中间状态。

**验收标准：**

- `stopRotateSchedulerLocked` 不再执行 `mu.Unlock()`/`mu.Lock()`。
- 并发 `Init`、`Close`、`FileLogger` 不会返回已关闭 writer，也不会关闭新状态资源。
- 多次停止和关闭幂等，不泄漏调度 goroutine。

**验证：**

- 新增阻塞 rotator 的生命周期测试，覆盖关闭期间的并发调用。
- 运行 `go test -count=100 ./logx`。
- 在具备 C 编译器的 Linux CI 运行 `go test -race ./...`。

**涉及文件：** `logx/logx.go`、`logx/rolling_writer_test.go` 或新增 `logx/rotation_manager_test.go`。

**规模：** M；**依赖：** 无。

### 任务 2：让动态 logger 加入正在运行的轮转管理器

**说明：** `rotationManager` 提供并发安全的 `Add` 和 `rotateAll`；午夜触发时获取当前 rotator 快照，而不是仅使用 `Init` 时复制的切片。

**验收标准：**

- `Init` 后创建的 `FileLogger` 在下一次触发时会执行 `Rotate()`。
- 同一个 rotator 只注册一次；普通追加文件不会产生有意义的轮转操作。
- 添加 rotator 与 `Close` 并发时行为确定，不向已停止 manager 注册资源。

**验证：**

- 使用 fake rotator 和可控触发信号测试动态注册，不依赖真实午夜时间。
- 运行 `go test -run 'Rotation|FileLogger' -count=50 ./logx`。

**涉及文件：** `logx/logx.go`、`logx/rolling_writer.go`、轮转测试文件。

**规模：** M；**依赖：** 任务 1。

### 检查点 A

- `go test ./...`、`go vet ./...`、`gofmt -l .` 全部通过。
- `go test -race ./...` 在 CI 通过。
- goroutine 泄漏测试通过。

## 第二阶段：路径和文件 writer 一致性

### 任务 3：统一路径边界校验并提供显式错误

**说明：** 增加 `joinWithinBase(base, relative)` 和目录名校验。`Init` 对非法 `appName`/sink 配置返回错误；新增 `OpenFileLogger` 返回创建和校验错误，现有 `FileLogger` 保持兼容并退化为 no-op。

**验收标准：**

- 拒绝绝对路径、空目录名、`..` 越界以及带路径分隔符的 `appName`。
- sink 和动态文件的最终词法路径始终位于应用日志目录内。
- 合法的嵌套相对路径和 Windows/Unix 分隔符场景有表驱动测试。

**验证：**

- 运行 `go test -run 'Path|Sink|OpenFileLogger' ./logx`。
- 测试 `..`、绝对路径、清洗后越界、合法嵌套目录四类输入。

**涉及文件：** `logx/logx.go`、`logx/logx_test.go`、`logx/config.go`。

**规模：** M；**依赖：** 无。

### 任务 4：同一物理路径只创建一个 writer

**说明：** 将动态 logger 拆成“按规范化绝对路径缓存的基础 logger/writer”和“按路径+名称缓存的 Named logger”。不同名称复用同一个 core，关闭和轮转只发生一次。

**验收标准：**

- 两个名称指向同一路径时只新增一个 writer、closer 和 rotator。
- 两个 logger 输出各自名称，内容均写入同一文件。
- 重复调用同一名称和路径仍返回稳定实例；不同路径不会错误复用。

**验证：**

- 新增双名称同路径测试，并断言单一资源所有权和日志内容。
- 运行 `go test -run FileLogger -count=50 ./logx`。

**涉及文件：** `logx/logx.go`、`logx/logx_test.go`。

**规模：** M；**依赖：** 任务 1、3。

### 检查点 B

- 路径越界测试全部失败关闭，合法路径保持兼容。
- 同路径 writer 在轮转、同步、关闭流程中只执行一次。
- 全量测试和竞态测试通过。

## 第三阶段：日志准确性和可观测性

### 任务 5：修正所有公开包装 API 的 caller

**说明：** `ModuleLogger.Logger()` 返回未增加 skip 的底层 logger；仅 `ModuleLogger` 和顶层快捷方法在调用 zap 时增加一层 `AddCallerSkip(1)`。

**验收标准：**

- `Info`、`Infof`、`ModuleLogger.Info` 和 `ModuleLogger.Logger().Info` 都记录业务测试文件行号。
- `L()`、`Named()`、`Sink()`、`FileLogger()` 的直接调用不受影响。
- `PackageSkip` 的包名和 caller 均指向跳过适配层后的调用方。

**验证：**

- 解析临时日志输出并断言 caller 文件名与行号范围。
- 运行 `go test -run 'Caller|Package' ./logx`。

**涉及文件：** `logx/sugar.go`、`logx/module_logger.go`、caller 测试文件。

**规模：** S；**依赖：** 无。

### 任务 6：正确管理标准库 log 重定向

**说明：** 保存第一次 `zap.RedirectStdLog` 返回的恢复函数；重新初始化前撤销旧重定向并绑定新 logger；最终 `Close`/`Shutdown` 恢复原输出、flags 和 prefix。

**验收标准：**

- `Init` 后标准库日志写入当前主 logger。
- 再次 `Init` 后不再引用旧 logger。
- `Close` 后标准库 logger 恢复到初始化前状态，而不是写入关闭文件。

**验证：**

- 用内存 buffer 保存并断言初始化前后的标准库 logger 状态。
- 运行 `go test -run StdLog -count=50 ./logx`。

**涉及文件：** `logx/logx.go`、`logx/logx_test.go`。

**规模：** S；**依赖：** 任务 1。

### 任务 7：暴露创建、刷盘和关闭错误

**说明：** 增加 `OpenFileLogger`、`Flush`、`Shutdown`；使用 `errors.Join` 聚合多个 logger/closer 错误。旧 `FileLogger`、`Sync`、`Close` 委托新 API，保留兼容行为。

**验收标准：**

- 新 API 能返回路径创建、写入同步和关闭错误。
- 即使其中一个资源失败，其他资源仍会继续同步和关闭。
- 旧 API 的现有调用和测试无需修改即可继续工作。

**验证：**

- 使用故障 writer 测试错误聚合和资源继续清理。
- 运行 `go test -run 'Error|Flush|Shutdown|OpenFileLogger' ./logx`。

**涉及文件：** `logx/logx.go`、`logx/rolling_writer.go`、相关测试。

**规模：** M；**依赖：** 任务 1、3。

### 任务 8：清理跨 generation 缓存并更新文档

**说明：** 在持有一致生命周期锁的条件下清理 `namedCache`、`sinkCache` 和 caller-adjusted logger 缓存，删除与 Named 缓存重复的 `moduleLoggerCache`，防止旧 logger 被重新插入；同步 README 的错误 API、路径约束、轮转语义和最新安装版本。

**验收标准：**

- 重复 `Init`/`Close` 后缓存大小不随 generation 无界增长。
- 并发获取 Named/Module logger 不会返回前一代实例。
- README 示例使用最新稳定版本并说明推荐的新错误 API。

**验证：**

- 新增 100 次重初始化缓存测试。
- 运行 `go test -count=20 ./...`、`go vet ./...`、`gofmt -l .`。

**涉及文件：** `logx/logx.go`、`logx/module_logger.go`、测试文件、`README.md`。

**规模：** M；**依赖：** 任务 1、4、5、6、7。

## 最终检查点

- 所有回归测试、全量测试、竞态测试和 vet 通过。
- 测试覆盖率不低于当前 69.7%，新增分支均有覆盖。
- `govulncheck ./...` 在可访问 `vuln.go.dev` 的 CI 环境通过。
- Windows 和 Linux 至少各执行一次路径测试。
- API 兼容性和行为变更完成一次人工审查后再发布新版本。

## 风险与缓解

| 风险 | 影响 | 缓解措施 |
| --- | --- | --- |
| 路径校验使历史上的嵌套 `appName` 配置失败 | 中 | 发布说明列出非法示例；合法嵌套仅通过 `Dir` 配置 |
| 轮转 manager 重构引入关闭死锁 | 高 | fake rotator 阻塞测试、竞态测试、goroutine 泄漏测试 |
| 同路径 writer 复用改变 logger 指针或名称 | 中 | 保留按路径+名称的派生缓存并测试指针稳定性 |
| 新错误 API 未被调用方采用 | 中 | README 首选新 API，旧 API 明确标记为兼容入口 |

## 待确认项

1. 是否确认保留“每天本地午夜主动轮转”行为？当前代码包含该行为，但 README 只描述按大小轮转。
2. 下一版本是否必须严格保持 v1 API 签名？本方案默认保持，并通过新增 API 暴露错误。
3. 是否允许 `appName` 包含多级目录？本方案默认禁止，目录层级统一由 `Config.Dir` 控制。
