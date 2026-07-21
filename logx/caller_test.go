package logx

import (
	"path/filepath"
	"testing"
)

func TestTopLevelHelpersReportBusinessCaller(t *testing.T) {
	dir := t.TempDir()
	if err := Init("api", Config{Level: "info", Dir: dir}); err != nil {
		t.Fatalf("Init() error = %v", err)
	}
	t.Cleanup(Close)

	Info("structured-caller")
	Infof("formatted-%s", "caller")
	Sync()

	path := filepath.Join(dir, "api", "info.log")
	assertLineContains(t, path, "structured-caller", "caller_test.go")
	assertLineContains(t, path, "formatted-caller", "caller_test.go")
}

func TestModuleLoggerDirectAndWrappedCallsReportBusinessCaller(t *testing.T) {
	dir := t.TempDir()
	if err := Init("api", Config{Level: "info", Dir: dir}); err != nil {
		t.Fatalf("Init() error = %v", err)
	}
	t.Cleanup(Close)

	logger := Module("module")
	logger.Info("wrapped-module-caller")
	logger.Logger().Info("direct-module-caller")
	Sync()

	path := filepath.Join(dir, "api", "info.log")
	assertLineContains(t, path, "wrapped-module-caller", "caller_test.go")
	assertLineContains(t, path, "direct-module-caller", "caller_test.go")
}

func assertLineContains(t *testing.T, filePath, message, want string) {
	t.Helper()
	content := readFileEventually(t, filePath, message)
	for _, line := range splitLines(content) {
		if containsAll(line, message, want) {
			return
		}
	}
	t.Fatalf("line containing %q does not contain %q; content: %s", message, want, content)
}
