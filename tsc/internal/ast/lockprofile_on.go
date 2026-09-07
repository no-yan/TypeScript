//go:build !nolock

package ast

import "sync"

func lockprofileRLock(mu *sync.RWMutex) func() {
	mu.RLock()
	return mu.RUnlock
}
