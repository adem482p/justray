//go:build debug
// +build debug

package statusnotifier

import (
	"fmt"
	"runtime"
	"sync"
	"sync/atomic"
)

type registryMutex struct {
	mu    sync.Mutex
	owner uint64
	depth int32
}

func (m *registryMutex) Lock() {
	gid := curGoroutineID()
	// Fast path: check if we already own it (no lock held)
	old := atomic.LoadUint64(&m.owner)
	if old == gid {
		panic(fmt.Sprintf("MUTEX LOCK: goroutine %d recursive lock", gid))
	}
	// Acquire mutex
	m.mu.Lock()
	// Double-check after acquiring (in case another goroutine acquired in between)
	old = atomic.LoadUint64(&m.owner)
	if old != 0 && old != gid {
		m.mu.Unlock()
		panic(fmt.Sprintf("MUTEX LOCK: goroutine %d recursive lock", gid))
	}
	atomic.StoreUint64(&m.owner, gid)
	atomic.AddInt32(&m.depth, 1)
}

func (m *registryMutex) Unlock() {
	gid := curGoroutineID()
	owner := atomic.LoadUint64(&m.owner)
	if owner != gid {
		panic(fmt.Sprintf("MUTEX UNLOCK: goroutine %d unlocking mutex owned by %d", gid, owner))
	}
	newDepth := atomic.AddInt32(&m.depth, -1)
	if newDepth == 0 {
		atomic.StoreUint64(&m.owner, 0)
	}
	// Only unlock internal mutex if we actually hold it (depth >= 0 before decrement)
	if newDepth >= 0 {
		m.mu.Unlock()
	}
}

func curGoroutineID() uint64 {
	var buf [64]byte
	n := runtime.Stack(buf[:], false)
	var id uint64
	fmt.Sscanf(string(buf[:n]), "goroutine %d ", &id)
	return id
}
