package logx

import (
	"reflect"
	"sync"
	"time"
)

type rotationManager struct {
	mu       sync.Mutex
	rotators []dailyRotator
	stopCh   chan struct{}
	doneCh   chan struct{}
	stopped  bool
}

func newRotationManager(rotators []dailyRotator) *rotationManager {
	manager := &rotationManager{
		stopCh: make(chan struct{}),
		doneCh: make(chan struct{}),
	}
	manager.addLocked(rotators...)
	go manager.run()
	return manager
}

func (m *rotationManager) Add(rotators ...dailyRotator) bool {
	if m == nil {
		return false
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.stopped {
		return false
	}
	m.addLocked(rotators...)
	return true
}

func (m *rotationManager) Stop() {
	if m == nil {
		return
	}
	m.mu.Lock()
	doneCh := m.doneCh
	if !m.stopped {
		m.stopped = true
		close(m.stopCh)
	}
	m.mu.Unlock()
	<-doneCh
}

func (m *rotationManager) rotateAll() {
	m.mu.Lock()
	rotators := append([]dailyRotator(nil), m.rotators...)
	m.mu.Unlock()
	for _, rotator := range rotators {
		if rotator != nil {
			_ = rotator.Rotate()
		}
	}
}

func (m *rotationManager) run() {
	defer close(m.doneCh)
	for {
		timer := time.NewTimer(durationUntilNextMidnight(time.Now()))
		select {
		case <-m.stopCh:
			if !timer.Stop() {
				select {
				case <-timer.C:
				default:
				}
			}
			return
		case <-timer.C:
			m.rotateAll()
		}
	}
}

func (m *rotationManager) addLocked(rotators ...dailyRotator) {
	for _, rotator := range rotators {
		if rotator == nil || containsRotator(m.rotators, rotator) {
			continue
		}
		m.rotators = append(m.rotators, rotator)
	}
}

func containsRotator(rotators []dailyRotator, candidate dailyRotator) bool {
	candidateType := reflect.TypeOf(candidate)
	if candidateType == nil || !candidateType.Comparable() {
		return false
	}
	for _, rotator := range rotators {
		if reflect.TypeOf(rotator) == candidateType && rotator == candidate {
			return true
		}
	}
	return false
}
