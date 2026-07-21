package logx

import (
	"bytes"
	stdlog "log"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"go.uber.org/zap"
)

func TestInitWritesMainAndConfiguredSinks(t *testing.T) {
	dir := t.TempDir()
	err := Init("api", Config{
		Level: "debug",
		Dir:   dir,
		Sinks: map[string]SinkConfig{
			"access": {
				FileName: "access.log",
			},
			"script": {
				Dir:      "scripts",
				FileName: "script.log",
			},
		},
	})
	if err != nil {
		t.Fatalf("Init() error = %v", err)
	}
	t.Cleanup(Close)

	Named("main").Info("main-message", zap.String("k", "v"))
	SinkNamed("access", "http.access").Info("access-message")
	SinkNamed("script", "runner").Info("script-message")
	Sync()

	assertFileContains(t, filepath.Join(dir, "api", "info.log"), "main-message")
	assertFileContains(t, filepath.Join(dir, "api", "access.log"), "access-message")
	assertFileContains(t, filepath.Join(dir, "api", "scripts", "script.log"), "script-message")
}

func TestInitReplacesPreviousLoggersAndClosesFiles(t *testing.T) {
	firstDir := t.TempDir()
	if err := Init("first", Config{Level: "info", Dir: firstDir}); err != nil {
		t.Fatalf("first Init() error = %v", err)
	}
	t.Cleanup(Close)
	Info("first-message")
	Sync()

	secondDir := t.TempDir()
	if err := Init("second", Config{Level: "info", Dir: secondDir}); err != nil {
		t.Fatalf("second Init() error = %v", err)
	}
	Info("second-message")
	Sync()

	assertFileContains(t, filepath.Join(firstDir, "first", "info.log"), "first-message")
	assertFileContains(t, filepath.Join(secondDir, "second", "info.log"), "second-message")
}

func TestStandardLoggerFollowsReinitializationAndCloseRestoresState(t *testing.T) {
	originalWriter := stdlog.Writer()
	originalFlags := stdlog.Flags()
	originalPrefix := stdlog.Prefix()
	t.Cleanup(func() {
		stdlog.SetOutput(originalWriter)
		stdlog.SetFlags(originalFlags)
		stdlog.SetPrefix(originalPrefix)
		Close()
	})

	var restored bytes.Buffer
	stdlog.SetOutput(&restored)
	stdlog.SetFlags(0)
	stdlog.SetPrefix("original:")

	firstDir := t.TempDir()
	if err := Init("first", Config{Dir: firstDir}); err != nil {
		t.Fatalf("first Init() error = %v", err)
	}
	stdlog.Print("first-standard-message")
	Sync()
	assertFileContains(t, filepath.Join(firstDir, "first", "info.log"), "first-standard-message")

	secondDir := t.TempDir()
	if err := Init("second", Config{Dir: secondDir}); err != nil {
		t.Fatalf("second Init() error = %v", err)
	}
	stdlog.Print("second-standard-message")
	Sync()
	assertFileContains(t, filepath.Join(secondDir, "second", "info.log"), "second-standard-message")

	Close()
	stdlog.Print("after-close")
	if got := restored.String(); !strings.Contains(got, "original:after-close") {
		t.Fatalf("restored standard log output = %q, want original prefix and writer", got)
	}
}

func TestSinkMissingDoesNotPolluteMainLogger(t *testing.T) {
	dir := t.TempDir()
	if err := Init("api", Config{Level: "info", Dir: dir}); err != nil {
		t.Fatalf("Init() error = %v", err)
	}
	t.Cleanup(Close)
	SinkNamed("missing", "module").Info("fallback-message")
	Sync()

	assertFileNotContains(t, filepath.Join(dir, "api", "info.log"), "fallback-message")
}

func TestFileLoggerWritesDynamicRelativePath(t *testing.T) {
	dir := t.TempDir()
	if err := Init("api", Config{Level: "info", Dir: dir}); err != nil {
		t.Fatalf("Init() error = %v", err)
	}
	t.Cleanup(Close)

	first := FileLogger("script.alpha", "scripts/alpha.log")
	second := FileLogger("script.alpha", "scripts/alpha.log")
	if first != second {
		t.Fatalf("FileLogger should reuse logger for same name and path")
	}

	first.Info("dynamic-script-message")
	Sync()

	assertFileContains(t, filepath.Join(dir, "api", "scripts", "alpha.log"), "dynamic-script-message")
}

func TestFileLoggerJoinsActiveRotationManager(t *testing.T) {
	dir := t.TempDir()
	if err := Init("api", Config{Level: "info", Dir: dir, MaxSize: 1}); err != nil {
		t.Fatalf("Init() error = %v", err)
	}
	t.Cleanup(Close)

	logger := FileLogger("script.alpha", "scripts/alpha.log")
	logger.Info("before-rotation")
	Sync()

	mu.RLock()
	manager := rotationMgr
	mu.RUnlock()
	if manager == nil {
		t.Fatal("rotation manager is nil")
	}
	manager.rotateAll()

	logger.Info("after-rotation")
	Sync()
	backups, err := filepath.Glob(filepath.Join(dir, "api", "scripts", "alpha-*.log"))
	if err != nil {
		t.Fatalf("Glob() error = %v", err)
	}
	if len(backups) != 1 {
		t.Fatalf("backup files = %v, want one dynamic logger backup", backups)
	}
	assertFileContains(t, backups[0], "before-rotation")
	assertFileContains(t, filepath.Join(dir, "api", "scripts", "alpha.log"), "after-rotation")
}

func TestInitRejectsEscapingSinkDirectory(t *testing.T) {
	err := Init("api", Config{
		Dir: filepath.Join(t.TempDir(), "logs"),
		Sinks: map[string]SinkConfig{
			"access": {Dir: ".."},
		},
	})
	if err == nil {
		Close()
		t.Fatal("Init() error = nil, want escaping sink directory error")
	}
}

func TestInitRejectsAppNameWithPathSeparators(t *testing.T) {
	err := Init(filepath.Join("nested", "api"), Config{Dir: t.TempDir()})
	if err == nil {
		Close()
		t.Fatal("Init() error = nil, want appName validation error")
	}
}

func TestOpenFileLoggerRejectsEscapingPath(t *testing.T) {
	if err := Init("api", Config{Dir: t.TempDir()}); err != nil {
		t.Fatalf("Init() error = %v", err)
	}
	t.Cleanup(Close)

	logger, err := OpenFileLogger("escape", filepath.Join("..", "escape.log"))
	if err == nil {
		t.Fatalf("OpenFileLogger() = %v, nil; want path validation error", logger)
	}
}

func TestOpenFileLoggerNormalizesPortableSeparators(t *testing.T) {
	dir := t.TempDir()
	if err := Init("api", Config{Dir: dir}); err != nil {
		t.Fatalf("Init() error = %v", err)
	}
	t.Cleanup(Close)

	logger, err := OpenFileLogger("portable", `scripts\portable.log`)
	if err != nil {
		t.Fatalf("OpenFileLogger() error = %v", err)
	}
	logger.Info("portable-message")
	Sync()
	assertFileContains(t, filepath.Join(dir, "api", "scripts", "portable.log"), "portable-message")
}

func TestFileLoggerUsesOneWriterForSamePathWithDifferentNames(t *testing.T) {
	dir := t.TempDir()
	if err := Init("api", Config{Dir: dir, MaxSize: 1}); err != nil {
		t.Fatalf("Init() error = %v", err)
	}
	t.Cleanup(Close)

	mu.RLock()
	before := len(writeClosers)
	mu.RUnlock()
	first := FileLogger("script.first", "scripts/shared.log")
	second := FileLogger("script.second", "scripts/shared.log")

	mu.RLock()
	added := len(writeClosers) - before
	mu.RUnlock()
	if added != 1 {
		t.Fatalf("managed writers added = %d, want 1", added)
	}
	first.Info("first-message")
	second.Info("second-message")
	Sync()
	path := filepath.Join(dir, "api", "scripts", "shared.log")
	assertFileContains(t, path, "first-message")
	assertFileContains(t, path, "second-message")
}

func TestInitRejectsSinkPathConflictingWithMainLogger(t *testing.T) {
	err := Init("api", Config{
		Level: "info",
		Dir:   t.TempDir(),
		Sinks: map[string]SinkConfig{
			"access": {FileName: "info.log"},
		},
	})
	if err == nil {
		Close()
		t.Fatal("Init() error = nil, want sink and main path conflict")
	}
}

func TestInitRejectsTwoSinksTargetingSamePath(t *testing.T) {
	err := Init("api", Config{
		Dir: t.TempDir(),
		Sinks: map[string]SinkConfig{
			"access": {FileName: "shared.log"},
			"audit":  {FileName: "shared.log"},
		},
	})
	if err == nil {
		Close()
		t.Fatal("Init() error = nil, want duplicate sink path conflict")
	}
}

func TestOpenFileLoggerRejectsConfiguredPathConflicts(t *testing.T) {
	dir := t.TempDir()
	if err := Init("api", Config{
		Level: "info",
		Dir:   dir,
		Sinks: map[string]SinkConfig{
			"access": {FileName: "access.log"},
		},
	}); err != nil {
		t.Fatalf("Init() error = %v", err)
	}
	t.Cleanup(Close)

	if logger, err := OpenFileLogger("main-conflict", "info.log"); err == nil {
		t.Fatalf("OpenFileLogger(info.log) = %v, nil; want main path conflict", logger)
	}
	if logger, err := OpenFileLogger("sink-conflict", "access.log"); err == nil {
		t.Fatalf("OpenFileLogger(access.log) = %v, nil; want sink path conflict", logger)
	}
}

func TestOpenFileLoggerRejectsSymlinkEscape(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	logDir := filepath.Join(root, "logs")
	link := filepath.Join(logDir, "linked")
	if err := os.MkdirAll(logDir, 0o755); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
	if err := os.Symlink(outside, link); err != nil {
		if runtime.GOOS == "windows" {
			t.Skipf("symlink unavailable on Windows: %v", err)
		}
		t.Fatalf("Symlink() error = %v", err)
	}
	if err := Init("api", Config{Dir: logDir}); err != nil {
		t.Fatalf("Init() error = %v", err)
	}
	t.Cleanup(Close)

	if logger, err := OpenFileLogger("escape", "linked/escape.log"); err == nil {
		t.Fatalf("OpenFileLogger() = %v, nil; want symlink escape error", logger)
	}
}

func TestOpenFileLoggerPreflightsRollingFileCreation(t *testing.T) {
	dir := t.TempDir()
	if err := Init("api", Config{Dir: dir, MaxSize: 1}); err != nil {
		t.Fatalf("Init() error = %v", err)
	}
	t.Cleanup(Close)
	target := filepath.Join(dir, "api", "blocked.log")
	if err := os.Mkdir(target, 0o755); err != nil {
		t.Fatalf("Mkdir() error = %v", err)
	}

	if logger, err := OpenFileLogger("blocked", "blocked.log"); err == nil {
		t.Fatalf("OpenFileLogger() = %v, nil; want rolling file creation error", logger)
	}
}

func TestResolveSinkLogPathDefaults(t *testing.T) {
	path := resolveSinkLogPath("api", Config{Dir: "logs"}, "access", SinkConfig{})
	want := filepath.Join("logs", "api", "access.log")
	if path != want {
		t.Fatalf("resolveSinkLogPath() = %q, want %q", path, want)
	}
}

func assertFileContains(t *testing.T, path string, want string) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	var data []byte
	var err error
	for time.Now().Before(deadline) {
		data, err = os.ReadFile(path)
		if err == nil && strings.Contains(string(data), want) {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	t.Fatalf("%s does not contain %q; content: %s", path, want, string(data))
}

func assertFileNotContains(t *testing.T, path string, unwanted string) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	if strings.Contains(string(data), unwanted) {
		t.Fatalf("%s contains %q; content: %s", path, unwanted, string(data))
	}
}

func readFileEventually(t *testing.T, path string, want string) string {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		data, err := os.ReadFile(path)
		if err == nil && strings.Contains(string(data), want) {
			return string(data)
		}
		time.Sleep(10 * time.Millisecond)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	return string(data)
}

func splitLines(value string) []string {
	return strings.FieldsFunc(value, func(r rune) bool { return r == '\n' || r == '\r' })
}

func containsAll(value string, parts ...string) bool {
	for _, part := range parts {
		if !strings.Contains(value, part) {
			return false
		}
	}
	return true
}
