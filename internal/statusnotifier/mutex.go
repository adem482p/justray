//go:build !debug
// +build !debug

package statusnotifier

import "sync"

type registryMutex = sync.Mutex
