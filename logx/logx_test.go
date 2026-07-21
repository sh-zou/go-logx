package logx

import (
	"os"
	"path/filepath"
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
