package logx

import "go.uber.org/zap"

func callerAdjustedLogger() *zap.Logger {
	mu.RLock()
	defer mu.RUnlock()
	return callerLogger("main", "", mainLogger)
}

// Debug 输出到主日志。
func Debug(msg string, fields ...zap.Field) {
	callerAdjustedLogger().Debug(msg, fields...)
}

// Info 输出到主日志。
func Info(msg string, fields ...zap.Field) {
	callerAdjustedLogger().Info(msg, fields...)
}

// Warn 输出到主日志。
func Warn(msg string, fields ...zap.Field) {
	callerAdjustedLogger().Warn(msg, fields...)
}

// Error 输出到主日志。
func Error(msg string, fields ...zap.Field) {
	callerAdjustedLogger().Error(msg, fields...)
}

// Fatal 输出到主日志并退出进程。
func Fatal(msg string, fields ...zap.Field) {
	callerAdjustedLogger().Fatal(msg, fields...)
}

// Debugf 输出格式化 debug 日志到主日志。
func Debugf(template string, args ...any) {
	callerAdjustedLogger().Sugar().Debugf(template, args...)
}

// Infof 输出格式化 info 日志到主日志。
func Infof(template string, args ...any) {
	callerAdjustedLogger().Sugar().Infof(template, args...)
}

// Warnf 输出格式化 warn 日志到主日志。
func Warnf(template string, args ...any) {
	callerAdjustedLogger().Sugar().Warnf(template, args...)
}

// Errorf 输出格式化 error 日志到主日志。
func Errorf(template string, args ...any) {
	callerAdjustedLogger().Sugar().Errorf(template, args...)
}

// Fatalf 输出格式化 fatal 日志到主日志并退出进程。
func Fatalf(template string, args ...any) {
	callerAdjustedLogger().Sugar().Fatalf(template, args...)
}
