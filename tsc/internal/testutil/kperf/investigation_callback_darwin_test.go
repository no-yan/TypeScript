//go:build binderinvestigation && kperf && darwin && arm64 && cgo

package kperf

import (
	"errors"
	"runtime"
	"testing"
)

// These tests exercise the foreign-thread bridge without opening the PMU.
func TestInvestigationFreshThreadResult(t *testing.T) {
	want := errors.New("callback error")
	if got := OnFreshThread(func() error {
		runtime.GC()
		return want
	}); got != want {
		t.Fatalf("callback error lost: %v", got)
	}
	called := false
	if err := OnFreshThread(func() error { called = true; return nil }); err != nil || !called {
		t.Fatalf("callback failed: called=%v err=%v", called, err)
	}
}

func TestInvestigationFreshThreadPanic(t *testing.T) {
	want := &struct{}{}
	defer func() {
		if got := recover(); got != want {
			t.Fatalf("panic did not return to calling goroutine: %v", got)
		}
	}()
	_ = OnFreshThread(func() error { panic(want) })
}
