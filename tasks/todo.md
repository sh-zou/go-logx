# go-logx 优化任务清单

- [x] 任务 1：封装轮转生命周期，消除停止期间的全局解锁
- [x] 任务 2：支持动态 FileLogger 注册到当前轮转管理器
- [x] 检查点 A：全量测试、race 和并发生命周期压力检查
- [x] 任务 3：统一路径边界校验并新增 OpenFileLogger
- [x] 任务 4：保证同一物理路径只有一个 writer
- [x] 检查点 B：路径与 writer 所有权回归验证
- [x] 任务 5：修正快捷方法和 ModuleLogger caller
- [x] 任务 6：保存并恢复标准库 log 重定向
- [x] 任务 7：新增 Flush/Shutdown 并聚合资源错误
- [x] 任务 8：清理跨 generation 缓存并更新 README
- [x] 最终检查：test、vet、fmt、Windows 路径测试
- [x] CI 检查：Linux symlink 路径测试、race 和 govulncheck 工作流
- [x] 修复控制台输出产生的伪同步错误
- [x] Close/Shutdown 后禁止动态 logger 重新创建文件
- [x] 发布准备：Go 1.24/1.26 CI、固定 govulncheck、更新 v1.0.5 文档
- [x] 修复 rolling writer 的真实文件同步
- [x] 修复夏令时切换日的午夜轮转时间
- [x] 固化 ErrNotInitialized 的错误优先级与 errors.Is 契约
