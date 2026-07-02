package logx

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPlainRollingWriterCreatesParentDirectory(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested", "app.log")
	writer, err := newRollingWriter(path, Config{})
	if err != nil {
		t.Fatalf("newRollingWriter() error = %v", err)
	}
	if writer == nil {
		t.Fatal("newRollingWriter() returned nil")
	}
	if _, err := writer.Write([]byte("hello")); err != nil {
		t.Fatalf("Write() error = %v", err)
	}
	if err := writer.Sync(); err != nil {
		t.Fatalf("Sync() error = %v", err)
	}
	if closer, ok := writer.(interface{ Close() error }); ok {
		if err := closer.Close(); err != nil {
			t.Fatalf("Close() error = %v", err)
		}
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	if got := string(data); !strings.Contains(got, "hello") {
		t.Fatalf("file content = %q, want to contain hello", got)
	}
}

func TestRollingWriterEmptyPathReturnsNil(t *testing.T) {
	writer, err := newRollingWriter(" ", Config{})
	if err != nil {
		t.Fatalf("newRollingWriter() error = %v", err)
	}
	if writer != nil {
		t.Fatalf("newRollingWriter() = %v, want nil", writer)
	}
}
