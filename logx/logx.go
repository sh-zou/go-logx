package logx

import (
	"errors"
	"fmt"
	"io"
	stdlog "log"
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
	mu              sync.RWMutex
	mainLogger      = zap.NewNop()
	noOpLogger      = zap.NewNop()
	sinkLoggers     = map[string]*zap.Logger{}
	fileLoggers     = map[string]*zap.Logger{}
	fileBaseLoggers = map[string]*zap.Logger{}
	managedLogPaths = map[string]string{}
	writeClosers    []io.Closer
	rotateClosers   []dailyRotator
	currentAppName  string
	currentConfig   Config
	namedCache      sync.Map
	sinkCache       sync.Map
	callerCache     sync.Map
	loggerGen       atomic.Uint64
	rotationMgr     *rotationManager
	restoreStdLog   func()
	initialized     bool
)

// ErrNotInitialized 表示日志系统尚未初始化或已经关闭。
var ErrNotInitialized = errors.New("logx is not initialized")

type levelEnablerFunc func(level zapcore.Level) bool

type writeOnly struct {
	io.Writer
}

func newConsoleWriteSyncer(writer io.Writer) zapcore.WriteSyncer {
	return zapcore.Lock(zapcore.AddSync(writeOnly{Writer: writer}))
}

func (fn levelEnablerFunc) Enabled(level zapcore.Level) bool {
	return fn(level)
}

// Init 初始化日志系统。
// 主日志默认写入 logs/<appName>/{debug,info,warn,error}.log。
func Init(appName string, cfg Config) error {
	mu.Lock()
	defer mu.Unlock()
	if err := validateInitPaths(appName, cfg); err != nil {
		return err
	}

	atomicLevel := parseLevel(cfg.Level)
	configuredPaths, err := collectManagedLogPaths(appName, cfg, atomicLevel)
	if err != nil {
		return err
	}
	logger, closers, rotators, err := buildLogger(appName, cfg, atomicLevel)
	if err != nil {
		return err
	}
	sinks, sinkClosers, sinkRotators, err := buildSinkLoggers(appName, cfg)
	if err != nil {
		_ = closeClosers(closers)
		return err
	}

	redirectStdLogLocked(logger)
	cleanupErr := syncLoggersLocked()
	cleanupErr = errors.Join(cleanupErr, closeWriteClosersLocked())

	mainLogger = logger
	sinkLoggers = sinks
	fileLoggers = map[string]*zap.Logger{}
	fileBaseLoggers = map[string]*zap.Logger{}
	managedLogPaths = configuredPaths
	currentAppName = appName
	currentConfig = cfg
	initialized = true
	writeClosers = append(closers, sinkClosers...)
	rotateClosers = append(append(make([]dailyRotator, 0, len(rotators)+len(sinkRotators)), rotators...), sinkRotators...)
	loggerGen.Add(1)
	clearLoggerCachesLocked()
	startRotateSchedulerLocked()

	zap.ReplaceGlobals(mainLogger)
	return cleanupErr
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
	mu.RLock()
	defer mu.RUnlock()
	return namedLoggerLocked(name)
}

func namedLoggerLocked(name string) *zap.Logger {
	if name == "" {
		return mainLogger
	}
	key := currentLoggerCacheKey("main", name)
	if cached, ok := namedCache.Load(key); ok {
		logger, _ := cached.(*zap.Logger)
		return logger
	}
	logger := mainLogger.Named(name)
	actual, _ := namedCache.LoadOrStore(key, logger)
	cached, _ := actual.(*zap.Logger)
	return cached
}

// Sink 返回独立 sink 日志实例。
// 未配置的 sink 会返回 no-op logger，而不是回退到主日志，避免通道配置错误时污染业务日志。
func Sink(name string) *zap.Logger {
	name = strings.TrimSpace(name)
	mu.RLock()
	defer mu.RUnlock()
	return sinkLoggerLocked(name)
}

func sinkLoggerLocked(name string) *zap.Logger {
	if name == "" {
		return noOpLogger
	}
	if logger, ok := sinkLoggers[name]; ok && logger != nil {
		return logger
	}
	return noOpLogger
}

// SinkNamed 返回指定 sink 下带名称的日志实例。
func SinkNamed(sinkName, loggerName string) *zap.Logger {
	sinkName = strings.TrimSpace(sinkName)
	loggerName = strings.TrimSpace(loggerName)
	mu.RLock()
	defer mu.RUnlock()
	if sinkName == "" {
		return namedLoggerLocked(loggerName)
	}
	if loggerName == "" {
		return sinkLoggerLocked(sinkName)
	}
	key := currentLoggerCacheKey("sink:"+sinkName, loggerName)
	if cached, ok := sinkCache.Load(key); ok {
		logger, _ := cached.(*zap.Logger)
		return logger
	}
	logger := sinkLoggerLocked(sinkName).Named(loggerName)
	actual, _ := sinkCache.LoadOrStore(key, logger)
	cached, _ := actual.(*zap.Logger)
	return cached
}

// FileLogger 返回写入应用日志目录下指定相对路径的动态文件日志实例。
// relativePath 必须是相对路径，且不能包含跳出日志目录的 ".."。
func FileLogger(name, relativePath string) *zap.Logger {
	logger, err := OpenFileLogger(name, relativePath)
	if err != nil {
		return noOpLogger
	}
	return logger
}

// OpenFileLogger 返回写入应用日志目录下指定相对路径的动态文件日志实例。
// 与 FileLogger 不同，路径校验或文件创建失败时会返回错误。
func OpenFileLogger(name, relativePath string) (*zap.Logger, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return nil, fmt.Errorf("logger name is empty")
	}
	cleanPath, err := cleanRelativePath(relativePath, false)
	if err != nil {
		return nil, fmt.Errorf("invalid log path %q: %w", relativePath, err)
	}

	mu.Lock()
	defer mu.Unlock()
	if !initialized {
		return nil, ErrNotInitialized
	}

	path := filepath.Join(resolveProgramLogDir(currentAppName, currentConfig), cleanPath)
	canonicalPath, err := canonicalLogPathWithin(resolveProgramLogDir(currentAppName, currentConfig), path)
	if err != nil {
		return nil, fmt.Errorf("resolve log path %q: %w", relativePath, err)
	}
	key := name + "\x00" + canonicalPath
	if logger, ok := fileLoggers[key]; ok && logger != nil {
		return logger, nil
	}
	if baseLogger, ok := fileBaseLoggers[canonicalPath]; ok && baseLogger != nil {
		fileLoggers[key] = baseLogger.Named(name)
		return fileLoggers[key], nil
	}
	if owner, ok := managedLogPaths[canonicalPath]; ok {
		return nil, fmt.Errorf("log path %q is already managed by %s", relativePath, owner)
	}

	baseLogger, closers, rotators, err := buildSingleFileLogger(path, currentConfig, false)
	if err != nil {
		return nil, fmt.Errorf("open log file %q: %w", relativePath, err)
	}
	fileBaseLoggers[canonicalPath] = baseLogger
	managedLogPaths[canonicalPath] = "dynamic:" + name
	fileLoggers[key] = baseLogger.Named(name)
	writeClosers = append(writeClosers, closers...)
	rotateClosers = append(rotateClosers, rotators...)
	if rotationMgr == nil {
		startRotateSchedulerLocked()
	} else {
		rotationMgr.Add(rotators...)
	}
	return fileLoggers[key], nil
}

// Sync 刷盘日志缓冲。
func Sync() {
	_ = Flush()
}

// Flush 刷盘所有受管理的日志，并返回聚合后的同步错误。
func Flush() error {
	mu.RLock()
	defer mu.RUnlock()
	return syncLoggersLocked()
}

// Close 刷盘并关闭所有受管理的日志文件。
// Close 后，logx 会回退为 no-op logger，直到再次调用 Init。
func Close() {
	_ = Shutdown()
}

// Shutdown 刷盘并关闭所有受管理的日志文件，并返回聚合后的错误。
func Shutdown() error {
	mu.Lock()
	defer mu.Unlock()
	restoreStdLogLocked()
	err := syncLoggersLocked()
	err = errors.Join(err, closeWriteClosersLocked())
	mainLogger = zap.NewNop()
	sinkLoggers = map[string]*zap.Logger{}
	fileLoggers = map[string]*zap.Logger{}
	fileBaseLoggers = map[string]*zap.Logger{}
	managedLogPaths = map[string]string{}
	currentAppName = ""
	currentConfig = Config{}
	initialized = false
	loggerGen.Add(1)
	clearLoggerCachesLocked()
	zap.ReplaceGlobals(mainLogger)
	return err
}

func syncLoggersLocked() error {
	var errs []error
	if err := mainLogger.Sync(); err != nil {
		errs = append(errs, fmt.Errorf("sync main logger: %w", err))
	}
	for name, logger := range sinkLoggers {
		if err := logger.Sync(); err != nil {
			errs = append(errs, fmt.Errorf("sync sink %q: %w", name, err))
		}
	}
	for path, logger := range fileBaseLoggers {
		if err := logger.Sync(); err != nil {
			errs = append(errs, fmt.Errorf("sync file %q: %w", path, err))
		}
	}
	return errors.Join(errs...)
}

func collectManagedLogPaths(appName string, cfg Config, level zap.AtomicLevel) (map[string]string, error) {
	paths := make(map[string]string, 4+len(cfg.Sinks))
	baseDir := resolveProgramLogDir(appName, cfg)
	add := func(logPath, owner string) error {
		canonical, err := canonicalLogPathWithin(baseDir, logPath)
		if err != nil {
			return fmt.Errorf("resolve %s path: %w", owner, err)
		}
		if existing, ok := paths[canonical]; ok {
			return fmt.Errorf("log path conflict: %s and %s both target %q", existing, owner, logPath)
		}
		paths[canonical] = owner
		return nil
	}
	programPaths := resolveProgramLogPaths(appName, cfg)
	mainPaths := []struct {
		enabled bool
		path    string
		owner   string
	}{
		{level.Enabled(zap.DebugLevel), programPaths.debug, "main:debug"},
		{level.Enabled(zap.InfoLevel), programPaths.info, "main:info"},
		{level.Enabled(zap.WarnLevel), programPaths.warn, "main:warn"},
		{level.Enabled(zap.ErrorLevel), programPaths.error, "main:error"},
	}
	for _, item := range mainPaths {
		if item.enabled {
			if err := add(item.path, item.owner); err != nil {
				return nil, err
			}
		}
	}
	for rawName, sinkCfg := range cfg.Sinks {
		name := strings.TrimSpace(rawName)
		if name == "" {
			continue
		}
		if err := add(resolveSinkLogPath(appName, cfg, name, sinkCfg), "sink:"+name); err != nil {
			return nil, err
		}
	}
	return paths, nil
}

func redirectStdLogLocked(logger *zap.Logger) {
	if restoreStdLog == nil {
		originalWriter := stdlog.Writer()
		undo := zap.RedirectStdLog(logger)
		restoreStdLog = func() {
			undo()
			stdlog.SetOutput(originalWriter)
		}
		return
	}
	zap.RedirectStdLog(logger)
}

func restoreStdLogLocked() {
	if restoreStdLog == nil {
		return
	}
	restoreStdLog()
	restoreStdLog = nil
}

func buildLogger(appName string, cfg Config, atomicLevel zap.AtomicLevel) (*zap.Logger, []io.Closer, []dailyRotator, error) {
	cores := make([]zapcore.Core, 0, 5)
	closers := make([]io.Closer, 0, 4)
	rotators := make([]dailyRotator, 0, 4)
	if cfg.ConsoleEnabled {
		cores = append(cores, zapcore.NewCore(
			zapcore.NewConsoleEncoder(newConsoleEncoderConfig()),
			newConsoleWriteSyncer(os.Stdout),
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
			newConsoleWriteSyncer(os.Stdout),
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
			_ = closeClosers(closers)
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
			newConsoleWriteSyncer(os.Stdout),
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
			newConsoleWriteSyncer(os.Stdout),
			sinkLevel,
		))
	}
	return zap.New(zapcore.NewTee(cores...), zap.AddCaller(), zap.AddStacktrace(zapcore.ErrorLevel)), closers, rotators, nil
}

func buildSingleFileLogger(path string, cfg Config, consoleEnabled bool) (*zap.Logger, []io.Closer, []dailyRotator, error) {
	cores := make([]zapcore.Core, 0, 2)
	closers := make([]io.Closer, 0, 1)
	rotators := make([]dailyRotator, 0, 1)
	level := zap.NewAtomicLevelAt(zap.DebugLevel)
	if consoleEnabled {
		cores = append(cores, zapcore.NewCore(
			zapcore.NewConsoleEncoder(newConsoleEncoderConfig()),
			newConsoleWriteSyncer(os.Stdout),
			level,
		))
	}
	writer, err := newRollingWriter(path, cfg)
	if err != nil {
		return nil, nil, nil, err
	}
	if writer != nil {
		cores = append(cores, zapcore.NewCore(
			zapcore.NewConsoleEncoder(newFileEncoderConfig()),
			writer,
			level,
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
			newConsoleWriteSyncer(os.Stdout),
			level,
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
			_ = closeClosers(closers)
			return nil, nil, nil, err
		}
	}
	if atomicLevel.Enabled(zap.InfoLevel) {
		if err := build(paths.info, exactLevelEnabler(atomicLevel, zap.InfoLevel)); err != nil {
			_ = closeClosers(closers)
			return nil, nil, nil, err
		}
	}
	if atomicLevel.Enabled(zap.WarnLevel) {
		if err := build(paths.warn, exactLevelEnabler(atomicLevel, zap.WarnLevel)); err != nil {
			_ = closeClosers(closers)
			return nil, nil, nil, err
		}
	}
	if atomicLevel.Enabled(zap.ErrorLevel) {
		if err := build(paths.error, errorLevelEnabler(atomicLevel)); err != nil {
			_ = closeClosers(closers)
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
		cleanDir, _ := cleanRelativePath(dir, true)
		baseDir = filepath.Join(baseDir, cleanDir)
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

func closeWriteClosersLocked() error {
	stopRotateSchedulerLocked()
	err := closeClosers(writeClosers)
	writeClosers = nil
	rotateClosers = nil
	return err
}

func closeClosers(closers []io.Closer) error {
	var errs []error
	for index, closer := range closers {
		if closer != nil {
			if err := closer.Close(); err != nil {
				errs = append(errs, fmt.Errorf("close writer %d: %w", index, err))
			}
		}
	}
	return errors.Join(errs...)
}

func clearLoggerCachesLocked() {
	namedCache.Clear()
	sinkCache.Clear()
	callerCache.Clear()
}

func callerLogger(scope, name string, logger *zap.Logger) *zap.Logger {
	key := currentLoggerCacheKey("caller:"+scope, name)
	if cached, ok := callerCache.Load(key); ok {
		cachedLogger, _ := cached.(*zap.Logger)
		return cachedLogger
	}
	adjusted := logger.WithOptions(zap.AddCallerSkip(1))
	actual, _ := callerCache.LoadOrStore(key, adjusted)
	cachedLogger, _ := actual.(*zap.Logger)
	return cachedLogger
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
	rotationMgr = newRotationManager(rotateClosers)
}

func stopRotateSchedulerLocked() {
	if rotationMgr == nil {
		return
	}
	rotationMgr.Stop()
	rotationMgr = nil
}

func durationUntilNextMidnight(now time.Time) time.Duration {
	year, month, day := now.Date()
	next := time.Date(year, month, day+1, 0, 0, 0, 0, now.Location())
	wait := next.Sub(now)
	if wait <= 0 {
		return time.Second
	}
	return wait
}
