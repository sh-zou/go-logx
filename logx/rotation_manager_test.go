package logx

import (
	"sync/atomic"
	"testing"
	"time"
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

func TestDurationUntilNextMidnightAcrossDaylightSavingTransitions(t *testing.T) {
	location, err := time.LoadLocation("America/New_York")
	if err != nil {
		t.Fatalf("LoadLocation() error = %v", err)
	}
	tests := []struct {
		name string
		now  time.Time
		next time.Time
	}{
		{
			name: "spring forward",
			now:  time.Date(2026, time.March, 8, 0, 30, 0, 0, location),
			next: time.Date(2026, time.March, 9, 0, 0, 0, 0, location),
		},
		{
			name: "fall back",
			now:  time.Date(2026, time.November, 1, 0, 30, 0, 0, location),
			next: time.Date(2026, time.November, 2, 0, 0, 0, 0, location),
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got, want := durationUntilNextMidnight(test.now), test.next.Sub(test.now); got != want {
				t.Fatalf("durationUntilNextMidnight() = %v, want %v", got, want)
			}
		})
	}
}
