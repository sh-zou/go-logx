package logx

import (
	"strings"

	"go.uber.org/zap"
)

// ModuleLogger 是延迟解析的日志包装器。
// 即使在 Init 调用前声明为包级变量也是安全的。
type ModuleLogger struct {
	name string
}

// Module 返回模块日志包装器。
func Module(name string) ModuleLogger {
	return ModuleLogger{name: name}
}

// Named 返回当前模块名称。
func (l ModuleLogger) Named() string {
	return l.name
}

// Logger 返回底层 zap logger。
func (l ModuleLogger) Logger() *zap.Logger {
	name := strings.TrimSpace(l.name)
	if name == "" {
		return L()
	}
	return Named(name)
}

func (l ModuleLogger) callerAdjustedLogger() *zap.Logger {
	name := strings.TrimSpace(l.name)
	mu.RLock()
	defer mu.RUnlock()
	return callerLogger("module", name, namedLoggerLocked(name))
}

// Debug 输出 debug 日志。
func (l ModuleLogger) Debug(msg string, fields ...zap.Field) {
	l.callerAdjustedLogger().Debug(msg, fields...)
}

// Info 输出 info 日志。
func (l ModuleLogger) Info(msg string, fields ...zap.Field) {
	l.callerAdjustedLogger().Info(msg, fields...)
}

// Warn 输出 warn 日志。
func (l ModuleLogger) Warn(msg string, fields ...zap.Field) {
	l.callerAdjustedLogger().Warn(msg, fields...)
}

// Error 输出 error 日志。
func (l ModuleLogger) Error(msg string, fields ...zap.Field) {
	l.callerAdjustedLogger().Error(msg, fields...)
}

// Debugf 输出格式化 debug 日志。
func (l ModuleLogger) Debugf(template string, args ...any) {
	l.callerAdjustedLogger().Sugar().Debugf(template, args...)
}

// Infof 输出格式化 info 日志。
func (l ModuleLogger) Infof(template string, args ...any) {
	l.callerAdjustedLogger().Sugar().Infof(template, args...)
}

// Warnf 输出格式化 warn 日志。
func (l ModuleLogger) Warnf(template string, args ...any) {
	l.callerAdjustedLogger().Sugar().Warnf(template, args...)
}

// Errorf 输出格式化 error 日志。
func (l ModuleLogger) Errorf(template string, args ...any) {
	l.callerAdjustedLogger().Sugar().Errorf(template, args...)
}
