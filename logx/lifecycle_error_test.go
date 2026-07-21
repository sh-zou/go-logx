package logx

import (
	"errors"
	"path/filepath"
	"testing"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

type errorWriteSyncer struct {
	syncErr error
}

func (w *errorWriteSyncer) Write(p []byte) (int, error) {
	return len(p), nil
}

func (w *errorWriteSyncer) Sync() error {
	return w.syncErr
}

type errorCloser struct {
	err error
}

func (c *errorCloser) Close() error {
	return c.err
}

func TestFlushReturnsManagedSyncErrors(t *testing.T) {
	if err := Init("api", Config{Dir: t.TempDir()}); err != nil {
		t.Fatalf("Init() error = %v", err)
	}
	t.Cleanup(Close)

	wantErr := errors.New("sync failed")
	mu.Lock()
	fileBaseLoggers["failing"] = zap.New(zapcore.NewCore(
		zapcore.NewConsoleEncoder(newFileEncoderConfig()),
		&errorWriteSyncer{syncErr: wantErr},
		zap.DebugLevel,
	))
	mu.Unlock()
	t.Cleanup(func() {
		mu.Lock()
		delete(fileBaseLoggers, "failing")
		mu.Unlock()
	})

	if err := Flush(); !errors.Is(err, wantErr) {
		t.Fatalf("Flush() error = %v, want %v", err, wantErr)
	}
}

func TestShutdownAggregatesSyncAndCloseErrors(t *testing.T) {
	if err := Init("api", Config{Dir: t.TempDir()}); err != nil {
		t.Fatalf("Init() error = %v", err)
	}
	t.Cleanup(Close)

	syncErr := errors.New("sync failed")
	closeErr := errors.New("close failed")
	mu.Lock()
	fileBaseLoggers["failing"] = zap.New(zapcore.NewCore(
		zapcore.NewConsoleEncoder(newFileEncoderConfig()),
		&errorWriteSyncer{syncErr: syncErr},
		zap.DebugLevel,
	))
	writeClosers = append(writeClosers, &errorCloser{err: closeErr})
	mu.Unlock()

	err := Shutdown()
	if !errors.Is(err, syncErr) || !errors.Is(err, closeErr) {
		t.Fatalf("Shutdown() error = %v, want sync and close errors", err)
	}
}

func TestInitReturnsPreviousGenerationCleanupErrors(t *testing.T) {
	if err := Init("previous", Config{Dir: t.TempDir()}); err != nil {
		t.Fatalf("initial Init() error = %v", err)
	}

	syncErr := errors.New("previous sync failed")
	closeErr := errors.New("previous close failed")
	mu.Lock()
	mainLogger = zap.New(zapcore.NewCore(
		zapcore.NewConsoleEncoder(newFileEncoderConfig()),
		&errorWriteSyncer{syncErr: syncErr},
		zap.DebugLevel,
	))
	writeClosers = append(writeClosers, &errorCloser{err: closeErr})
	mu.Unlock()

	newDir := t.TempDir()
	err := Init("next", Config{Dir: newDir})
	t.Cleanup(Close)
	if !errors.Is(err, syncErr) || !errors.Is(err, closeErr) {
		t.Fatalf("Init() error = %v, want previous sync and close errors", err)
	}
	Info("new-generation-message")
	Sync()
	assertFileContains(t, filepath.Join(newDir, "next", "info.log"), "new-generation-message")
}
