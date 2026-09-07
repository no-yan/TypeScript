//go:build nolock

package ast

import "sync"

func lockprofileRLock(*sync.RWMutex) func() {
	return func() {}
}
