//go:build binderinvestigation && kperf && darwin && arm64 && cgo

package kperf

/*
#include <stdint.h>
*/
import "C"

import (
	"runtime"
	"runtime/cgo"
)

type investigationThreadCall struct {
	fn         func() error
	err        error
	panicked   bool
	panicValue any
}

//export kperfInvestigationThreadCallback
func kperfInvestigationThreadCallback(handle C.uintptr_t) {
	call := cgo.Handle(handle).Value().(*investigationThreadCall)
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	completed := false
	defer func() {
		if !completed {
			call.panicked = true
			call.panicValue = recover()
		}
	}()
	call.err = call.fn()
	completed = true
}
