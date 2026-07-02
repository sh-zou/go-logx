package logx

import (
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"go.uber.org/zap/zapcore"
	lumberjack "gopkg.in/natefinch/lumberjack.v2"
)

type dailyRotator interface {
	Rotate() error
}

// managedWriteSyncer 为 zap 提供支持关闭和滚动的线程安全 writer。
type managedWriteSyncer struct {
	mu      sync.Mutex
	writer  io.WriteCloser
	syncer  interface{ Sync() error }
	rotator dailyRotator
}

func newRollingWriter(path string, cfg Config) (zapcore.WriteSyncer, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return nil, nil
	}
	if cfg.MaxSize <= 0 {
		return newPlainFileWriteSyncer(path)
	}
	logger := &lumberjack.Logger{
		Filename:   path,
		MaxSize:    cfg.MaxSize,
		MaxBackups: cfg.MaxBackups,
		MaxAge:     cfg.MaxAge,
		LocalTime:  true,
		Compress:   cfg.Compress,
	}
	return &managedWriteSyncer{
		writer:  logger,
		rotator: logger,
	}, nil
}

func newPlainFileWriteSyncer(path string) (zapcore.WriteSyncer, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, err
	}
	file, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return nil, err
	}
	return &managedWriteSyncer{
		writer: file,
		syncer: file,
	}, nil
}

func (w *managedWriteSyncer) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w == nil || w.writer == nil {
		return 0, os.ErrClosed
	}
	return w.writer.Write(p)
}

func (w *managedWriteSyncer) Sync() error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w == nil || w.writer == nil || w.syncer == nil {
		return nil
	}
	return w.syncer.Sync()
}

func (w *managedWriteSyncer) Close() error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w == nil || w.writer == nil {
		return nil
	}
	err := w.writer.Close()
	w.writer = nil
	w.syncer = nil
	w.rotator = nil
	return err
}

func (w *managedWriteSyncer) Rotate() error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w == nil || w.rotator == nil {
		return nil
	}
	return w.rotator.Rotate()
}
