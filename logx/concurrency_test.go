package logx

import (
	"fmt"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

func TestConcurrentLifecycleOperationsCompleteWithoutDeadlock(t *testing.T) {
	root := t.TempDir()
	if err := Init("initial", Config{Dir: root, MaxSize: 1}); err != nil {
		t.Fatalf("Init() error = %v", err)
	}
	t.Cleanup(Close)

	var workers sync.WaitGroup
	workers.Add(4)
	go func() {
		defer workers.Done()
		for i := 0; i < 25; i++ {
			Named("main").Info("message")
		}
	}()
	go func() {
		defer workers.Done()
		for i := 0; i < 25; i++ {
			FileLogger("dynamic", filepath.Join("dynamic", "shared.log")).Info("message")
		}
	}()
	go func() {
		defer workers.Done()
		for i := 0; i < 10; i++ {
			_ = Init(fmt.Sprintf("app-%d", i), Config{Dir: root, MaxSize: 1})
		}
	}()
	go func() {
		defer workers.Done()
		for i := 0; i < 10; i++ {
			Close()
		}
	}()

	done := make(chan struct{})
	go func() {
		workers.Wait()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("concurrent lifecycle operations deadlocked")
	}
}
