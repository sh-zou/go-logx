package logx

import (
	"strings"
	"sync"

	"go.uber.org/zap"
)

var moduleLoggerCache sync.Map

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
		return L().WithOptions(zap.AddCallerSkip(1))
	}
	key := currentLoggerCacheKey("module", name)
	if cached, ok := moduleLoggerCache.Load(key); ok {
		logger, _ := cached.(*zap.Logger)
		return logger
	}
	logger := Named(name).WithOptions(zap.AddCallerSkip(1))
	actual, _ := moduleLoggerCache.LoadOrStore(key, logger)
	cached, _ := actual.(*zap.Logger)
	return cached
}

// Debug 输出 debug 日志。
func (l ModuleLogger) Debug(msg string, fields ...zap.Field) {
	l.Logger().Debug(msg, fields...)
}

// Info 输出 info 日志。
func (l ModuleLogger) Info(msg string, fields ...zap.Field) {
	l.Logger().Info(msg, fields...)
}

// Warn 输出 warn 日志。
func (l ModuleLogger) Warn(msg string, fields ...zap.Field) {
	l.Logger().Warn(msg, fields...)
}

// Error 输出 error 日志。
func (l ModuleLogger) Error(msg string, fields ...zap.Field) {
	l.Logger().Error(msg, fields...)
}

// Debugf 输出格式化 debug 日志。
func (l ModuleLogger) Debugf(template string, args ...any) {
	l.Logger().Sugar().Debugf(template, args...)
}

// Infof 输出格式化 info 日志。
func (l ModuleLogger) Infof(template string, args ...any) {
	l.Logger().Sugar().Infof(template, args...)
}

// Warnf 输出格式化 warn 日志。
func (l ModuleLogger) Warnf(template string, args ...any) {
	l.Logger().Sugar().Warnf(template, args...)
}

// Errorf 输出格式化 error 日志。
func (l ModuleLogger) Errorf(template string, args ...any) {
	l.Logger().Sugar().Errorf(template, args...)
}
