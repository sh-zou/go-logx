package logx

import (
	"io"
	"os"
	"path/filepath"
	"runtime/debug"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

var (
	mu            sync.RWMutex
	mainLogger    = zap.NewNop()
	noOpLogger    = zap.NewNop()
	sinkLoggers   = map[string]*zap.Logger{}
	writeClosers  []io.Closer
	rotateClosers []dailyRotator
	namedCache    sync.Map
	sinkCache     sync.Map
	loggerGen     atomic.Uint64
	rotateStopCh  chan struct{}
	rotateWG      sync.WaitGroup
)

type levelEnablerFunc func(level zapcore.Level) bool

func (fn levelEnablerFunc) Enabled(level zapcore.Level) bool {
	return fn(level)
}

// Init 初始化日志系统。
// 主日志默认写入 logs/<appName>/{debug,info,warn,error}.log。
func Init(appName string, cfg Config) error {
	mu.Lock()
	defer mu.Unlock()

	atomicLevel := parseLevel(cfg.Level)
	logger, closers, rotators, err := buildLogger(appName, cfg, atomicLevel)
	if err != nil {
		return err
	}
	sinks, sinkClosers, sinkRotators, err := buildSinkLoggers(appName, cfg)
	if err != nil {
		closeClosers(closers)
		return err
	}

	_ = mainLogger.Sync()
	for _, logger := range sinkLoggers {
		_ = logger.Sync()
	}
	closeWriteClosersLocked()

	mainLogger = logger
	sinkLoggers = sinks
	writeClosers = append(closers, sinkClosers...)
	rotateClosers = append(append(make([]dailyRotator, 0, len(rotators)+len(sinkRotators)), rotators...), sinkRotators...)
	loggerGen.Add(1)
	startRotateSchedulerLocked()

	zap.ReplaceGlobals(mainLogger)
	_ = zap.RedirectStdLog(mainLogger)
	return nil
}

// L 返回主日志实例。
func L() *zap.Logger {
	mu.RLock()
	defer mu.RUnlock()
	return mainLogger
}

// Sugar 返回主日志的 SugaredLogger。
func Sugar() *zap.SugaredLogger {
	return L().Sugar()
}

// Named 返回带名称的主日志实例。
func Named(name string) *zap.Logger {
	name = strings.TrimSpace(name)
	if name == "" {
		return L()
	}
	key := currentLoggerCacheKey("main", name)
	if cached, ok := namedCache.Load(key); ok {
		logger, _ := cached.(*zap.Logger)
		return logger
	}
	logger := L().Named(name)
	actual, _ := namedCache.LoadOrStore(key, logger)
	cached, _ := actual.(*zap.Logger)
	return cached
}

// Sink 返回独立 sink 日志实例。
// 未配置的 sink 会返回 no-op logger，而不是回退到主日志，避免通道配置错误时污染业务日志。
func Sink(name string) *zap.Logger {
	name = strings.TrimSpace(name)
	if name == "" {
		return noOpLogger
	}
	mu.RLock()
	defer mu.RUnlock()
	if logger, ok := sinkLoggers[name]; ok && logger != nil {
		return logger
	}
	return noOpLogger
}

// SinkNamed 返回指定 sink 下带名称的日志实例。
func SinkNamed(sinkName, loggerName string) *zap.Logger {
	sinkName = strings.TrimSpace(sinkName)
	loggerName = strings.TrimSpace(loggerName)
	if sinkName == "" {
		return Named(loggerName)
	}
	if loggerName == "" {
		return Sink(sinkName)
	}
	key := currentLoggerCacheKey("sink:"+sinkName, loggerName)
	if cached, ok := sinkCache.Load(key); ok {
		logger, _ := cached.(*zap.Logger)
		return logger
	}
	logger := Sink(sinkName).Named(loggerName)
	actual, _ := sinkCache.LoadOrStore(key, logger)
	cached, _ := actual.(*zap.Logger)
	return cached
}

// Sync 刷盘日志缓冲。
func Sync() {
	mu.RLock()
	defer mu.RUnlock()
	_ = mainLogger.Sync()
	for _, logger := range sinkLoggers {
		_ = logger.Sync()
	}
}

// Close 刷盘并关闭所有受管理的日志文件。
// Close 后，logx 会回退为 no-op logger，直到再次调用 Init。
func Close() {
	mu.Lock()
	defer mu.Unlock()
	_ = mainLogger.Sync()
	for _, logger := range sinkLoggers {
		_ = logger.Sync()
	}
	closeWriteClosersLocked()
	mainLogger = zap.NewNop()
	sinkLoggers = map[string]*zap.Logger{}
	loggerGen.Add(1)
	zap.ReplaceGlobals(mainLogger)
}

func buildLogger(appName string, cfg Config, atomicLevel zap.AtomicLevel) (*zap.Logger, []io.Closer, []dailyRotator, error) {
	cores := make([]zapcore.Core, 0, 5)
	closers := make([]io.Closer, 0, 4)
	rotators := make([]dailyRotator, 0, 4)
	if cfg.ConsoleEnabled {
		cores = append(cores, zapcore.NewCore(
			zapcore.NewConsoleEncoder(newConsoleEncoderConfig()),
			zapcore.AddSync(os.Stdout),
			atomicLevel,
		))
	}
	fileCores, fileClosers, fileRotators, err := buildFileCores(appName, cfg, atomicLevel)
	if err != nil {
		return nil, nil, nil, err
	}
	cores = append(cores, fileCores...)
	closers = append(closers, fileClosers...)
	rotators = append(rotators, fileRotators...)
	if len(cores) == 0 {
		cores = append(cores, zapcore.NewCore(
			zapcore.NewConsoleEncoder(newConsoleEncoderConfig()),
			zapcore.AddSync(os.Stdout),
			atomicLevel,
		))
	}
	return zap.New(zapcore.NewTee(cores...), zap.AddCaller(), zap.AddStacktrace(zapcore.ErrorLevel)), closers, rotators, nil
}

func buildSinkLoggers(appName string, cfg Config) (map[string]*zap.Logger, []io.Closer, []dailyRotator, error) {
	result := make(map[string]*zap.Logger, len(cfg.Sinks))
	closers := make([]io.Closer, 0, len(cfg.Sinks))
	rotators := make([]dailyRotator, 0, len(cfg.Sinks))
	for rawName, sinkCfg := range cfg.Sinks {
		name := strings.TrimSpace(rawName)
		if name == "" {
			continue
		}
		logger, sinkClosers, sinkRotators, err := buildSinkLogger(appName, cfg, name, sinkCfg)
		if err != nil {
			closeClosers(closers)
			return nil, nil, nil, err
		}
		result[name] = logger
		closers = append(closers, sinkClosers...)
		rotators = append(rotators, sinkRotators...)
	}
	return result, closers, rotators, nil
}

func buildSinkLogger(appName string, cfg Config, name string, sinkCfg SinkConfig) (*zap.Logger, []io.Closer, []dailyRotator, error) {
	cores := make([]zapcore.Core, 0, 2)
	closers := make([]io.Closer, 0, 1)
	rotators := make([]dailyRotator, 0, 1)
	sinkLevel := zap.NewAtomicLevelAt(zap.DebugLevel)
	if sinkCfg.ConsoleEnabled {
		cores = append(cores, zapcore.NewCore(
			zapcore.NewConsoleEncoder(newConsoleEncoderConfig()),
			zapcore.AddSync(os.Stdout),
			sinkLevel,
		))
	}
	writer, err := newRollingWriter(resolveSinkLogPath(appName, cfg, name, sinkCfg), cfg)
	if err != nil {
		return nil, nil, nil, err
	}
	if writer != nil {
		cores = append(cores, zapcore.NewCore(
			zapcore.NewConsoleEncoder(newFileEncoderConfig()),
			writer,
			sinkLevel,
		))
		if closer, ok := writer.(io.Closer); ok {
			closers = append(closers, closer)
		}
		if rotator, ok := writer.(dailyRotator); ok {
			rotators = append(rotators, rotator)
		}
	}
	if len(cores) == 0 {
		cores = append(cores, zapcore.NewCore(
			zapcore.NewConsoleEncoder(newConsoleEncoderConfig()),
			zapcore.AddSync(os.Stdout),
			sinkLevel,
		))
	}
	return zap.New(zapcore.NewTee(cores...), zap.AddCaller(), zap.AddStacktrace(zapcore.ErrorLevel)), closers, rotators, nil
}

func newFileEncoderConfig() zapcore.EncoderConfig {
	return zapcore.EncoderConfig{
		TimeKey:        "time",
		LevelKey:       "level",
		NameKey:        "logger",
		CallerKey:      "caller",
		MessageKey:     "msg",
		StacktraceKey:  "stacktrace",
		LineEnding:     zapcore.DefaultLineEnding,
		EncodeLevel:    zapcore.CapitalLevelEncoder,
		EncodeTime:     zapcore.TimeEncoderOfLayout("2006-01-02 15:04:05.000"),
		EncodeDuration: zapcore.MillisDurationEncoder,
		EncodeCaller:   zapcore.ShortCallerEncoder,
	}
}

func newConsoleEncoderConfig() zapcore.EncoderConfig {
	cfg := newFileEncoderConfig()
	cfg.EncodeLevel = zapcore.CapitalColorLevelEncoder
	return cfg
}

func parseLevel(value string) zap.AtomicLevel {
	levelValue := zapcore.InfoLevel
	if err := levelValue.UnmarshalText([]byte(strings.ToLower(strings.TrimSpace(value)))); err != nil {
		levelValue = zapcore.InfoLevel
	}
	return zap.NewAtomicLevelAt(levelValue)
}

func buildFileCores(appName string, cfg Config, atomicLevel zap.AtomicLevel) ([]zapcore.Core, []io.Closer, []dailyRotator, error) {
	paths := resolveProgramLogPaths(appName, cfg)
	encoder := zapcore.NewConsoleEncoder(newFileEncoderConfig())
	cores := make([]zapcore.Core, 0, 4)
	closers := make([]io.Closer, 0, 4)
	rotators := make([]dailyRotator, 0, 4)

	build := func(path string, enabler levelEnablerFunc) error {
		writer, err := newRollingWriter(path, cfg)
		if err != nil {
			return err
		}
		if writer == nil {
			return nil
		}
		cores = append(cores, zapcore.NewCore(encoder, writer, enabler))
		if closer, ok := writer.(io.Closer); ok {
			closers = append(closers, closer)
		}
		if rotator, ok := writer.(dailyRotator); ok {
			rotators = append(rotators, rotator)
		}
		return nil
	}

	if atomicLevel.Enabled(zap.DebugLevel) {
		if err := build(paths.debug, exactLevelEnabler(atomicLevel, zap.DebugLevel)); err != nil {
			closeClosers(closers)
			return nil, nil, nil, err
		}
	}
	if atomicLevel.Enabled(zap.InfoLevel) {
		if err := build(paths.info, exactLevelEnabler(atomicLevel, zap.InfoLevel)); err != nil {
			closeClosers(closers)
			return nil, nil, nil, err
		}
	}
	if atomicLevel.Enabled(zap.WarnLevel) {
		if err := build(paths.warn, exactLevelEnabler(atomicLevel, zap.WarnLevel)); err != nil {
			closeClosers(closers)
			return nil, nil, nil, err
		}
	}
	if atomicLevel.Enabled(zap.ErrorLevel) {
		if err := build(paths.error, errorLevelEnabler(atomicLevel)); err != nil {
			closeClosers(closers)
			return nil, nil, nil, err
		}
	}

	return cores, closers, rotators, nil
}

func exactLevelEnabler(atomicLevel zap.AtomicLevel, expected zapcore.Level) levelEnablerFunc {
	return func(level zapcore.Level) bool {
		return level == expected && atomicLevel.Enabled(level)
	}
}

func errorLevelEnabler(atomicLevel zap.AtomicLevel) levelEnablerFunc {
	return func(level zapcore.Level) bool {
		return level >= zapcore.ErrorLevel && atomicLevel.Enabled(level)
	}
}

type programLogPaths struct {
	debug string
	info  string
	warn  string
	error string
}

func resolveProgramLogPaths(appName string, cfg Config) programLogPaths {
	baseDir := resolveProgramLogDir(appName, cfg)
	return programLogPaths{
		debug: filepath.Join(baseDir, "debug.log"),
		info:  filepath.Join(baseDir, "info.log"),
		warn:  filepath.Join(baseDir, "warn.log"),
		error: filepath.Join(baseDir, "error.log"),
	}
}

func resolveSinkLogPath(appName string, cfg Config, name string, sinkCfg SinkConfig) string {
	baseDir := resolveProgramLogDir(appName, cfg)
	if dir := strings.TrimSpace(sinkCfg.Dir); dir != "" {
		baseDir = filepath.Join(baseDir, filepath.Clean(dir))
	}
	fileName := strings.TrimSpace(sinkCfg.FileName)
	if fileName == "" {
		fileName = name + ".log"
	}
	return filepath.Join(baseDir, filepath.Base(fileName))
}

func resolveProgramLogDir(appName string, cfg Config) string {
	return filepath.Join(resolveProgramLogRootDir(cfg), resolveProgramLogDirName(appName, cfg))
}

func resolveProgramLogRootDir(cfg Config) string {
	dir := strings.TrimSpace(cfg.Dir)
	if dir == "" {
		dir = "logs"
	}
	return dir
}

func resolveProgramLogDirName(appName string, cfg Config) string {
	fileName := strings.TrimSpace(cfg.FileName)
	if fileName != "" {
		name := strings.TrimSuffix(filepath.Base(fileName), filepath.Ext(fileName))
		if name != "" {
			return name
		}
	}
	name := strings.TrimSpace(appName)
	if name == "" {
		name = detectProcessName()
	}
	if name == "" {
		name = "app"
	}
	return name
}

func detectProcessName() string {
	if info, ok := debug.ReadBuildInfo(); ok && info != nil {
		if name := processNameFromImportPath(info.Path); name != "" {
			return name
		}
	}
	executable, err := os.Executable()
	if err != nil {
		return ""
	}
	name := strings.TrimSuffix(filepath.Base(executable), filepath.Ext(executable))
	return strings.TrimSpace(name)
}

func closeWriteClosersLocked() {
	stopRotateSchedulerLocked()
	closeClosers(writeClosers)
	writeClosers = nil
	rotateClosers = nil
}

func closeClosers(closers []io.Closer) {
	for _, closer := range closers {
		if closer != nil {
			_ = closer.Close()
		}
	}
}

type loggerCacheKey struct {
	generation uint64
	scope      string
	name       string
}

func currentLoggerCacheKey(scope, name string) loggerCacheKey {
	return loggerCacheKey{
		generation: loggerGen.Load(),
		scope:      scope,
		name:       name,
	}
}

func processNameFromImportPath(importPath string) string {
	importPath = strings.TrimSpace(importPath)
	importPath = strings.Trim(importPath, "/")
	if importPath == "" || importPath == "command-line-arguments" {
		return ""
	}
	if index := strings.LastIndex(importPath, "/"); index >= 0 {
		return strings.TrimSpace(importPath[index+1:])
	}
	return importPath
}

func startRotateSchedulerLocked() {
	stopRotateSchedulerLocked()
	if len(rotateClosers) == 0 {
		return
	}
	rotateStopCh = make(chan struct{})
	rotateWG.Add(1)
	go func(rotators []dailyRotator, stopCh <-chan struct{}) {
		defer rotateWG.Done()
		for {
			wait := durationUntilNextMidnight(time.Now())
			timer := time.NewTimer(wait)
			select {
			case <-stopCh:
				if !timer.Stop() {
					<-timer.C
				}
				return
			case <-timer.C:
				for _, rotator := range rotators {
					if rotator == nil {
						continue
					}
					_ = rotator.Rotate()
				}
			}
		}
	}(append([]dailyRotator(nil), rotateClosers...), rotateStopCh)
}

func stopRotateSchedulerLocked() {
	if rotateStopCh == nil {
		return
	}
	close(rotateStopCh)
	rotateStopCh = nil
	mu.Unlock()
	rotateWG.Wait()
	mu.Lock()
}

func durationUntilNextMidnight(now time.Time) time.Duration {
	next := truncateDay(now).Add(24 * time.Hour)
	wait := next.Sub(now)
	if wait <= 0 {
		return time.Second
	}
	return wait
}

func truncateDay(value time.Time) time.Time {
	year, month, day := value.Date()
	return time.Date(year, month, day, 0, 0, 0, 0, value.Location())
}
