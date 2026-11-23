//go:build debug
// +build debug

package statusnotifier

import (
	"sync"
	"testing"
	"time"
)

func TestRegistryMutex_NoPanicOnContention(t *testing.T) {
	var mu registryMutex
	var wg sync.WaitGroup
	const numGoroutines = 10
	const iterations = 100

	// Test concurrent lock/unlock from multiple goroutines
	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < iterations; j++ {
				mu.Lock()
				// Simulate some work
				time.Sleep(time.Microsecond)
				mu.Unlock()
			}
		}()
	}

	wg.Wait()
	// If we reach here without panic, the test passes
}

func TestRegistryMutex_RecursiveLockPanic(t *testing.T) {
	var mu registryMutex

	mu.Lock()
	// Attempting to lock again from same goroutine should panic
	defer func() {
		if r := recover(); r == nil {
			t.Error("expected panic on recursive lock, got none")
		}
	}()
	mu.Lock() // This should panic
}

func TestRegistryMutex_UnlockWithoutLockPanic(t *testing.T) {
	var mu registryMutex

	// Unlocking without locking should panic
	defer func() {
		if r := recover(); r == nil {
			t.Error("expected panic on unlock without lock, got none")
		}
	}()
	mu.Unlock() // This should panic
}

func TestRegistryMutex_UnlockFromDifferentGoroutinePanic(t *testing.T) {
	var mu registryMutex
	done := make(chan struct{})

	mu.Lock()
	go func() {
		defer func() {
			if r := recover(); r != nil {
				// Expected panic
				close(done)
			}
		}()
		mu.Unlock() // This should panic - different goroutine
	}()

	select {
	case <-done:
		// Test passed - panic occurred
	case <-time.After(5 * time.Second):
		t.Error("timeout waiting for panic from different goroutine unlock")
	}
	mu.Unlock()
}