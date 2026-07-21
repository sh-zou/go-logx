package logx

import (
	"sync/atomic"
	"testing"
)

type countingRotator struct {
	count atomic.Int32
}

func (r *countingRotator) Rotate() error {
	r.count.Add(1)
	return nil
}

func TestRotationManagerIncludesRotatorsAddedAfterStart(t *testing.T) {
	manager := newRotationManager(nil)
	t.Cleanup(manager.Stop)

	rotator := &countingRotator{}
	if !manager.Add(rotator) {
		t.Fatal("Add() = false, want true for a running manager")
	}

	manager.rotateAll()
	if got := rotator.count.Load(); got != 1 {
		t.Fatalf("Rotate() calls = %d, want 1", got)
	}
}

func TestRotationManagerDeduplicatesRotators(t *testing.T) {
	rotator := &countingRotator{}
	manager := newRotationManager([]dailyRotator{rotator})
	t.Cleanup(manager.Stop)

	if !manager.Add(rotator) {
		t.Fatal("Add() = false, want true for an existing rotator")
	}
	manager.rotateAll()

	if got := rotator.count.Load(); got != 1 {
		t.Fatalf("Rotate() calls = %d, want 1", got)
	}
}

func TestRotationManagerRejectsAddAfterStop(t *testing.T) {
	manager := newRotationManager(nil)
	manager.Stop()
	manager.Stop()

	if manager.Add(&countingRotator{}) {
		t.Fatal("Add() = true after Stop(), want false")
	}
}
