//go:build kperf && darwin && arm64 && cgo

package kperf

/*
#cgo LDFLAGS: -ldl
#include <dlfcn.h>
#include <stdint.h>
#include <stdlib.h>

typedef int (*f_i_i)(int);
typedef int (*f_i_u)(uint32_t);
typedef uint32_t (*f_u_u)(uint32_t);
typedef int (*f_read)(uint32_t, uint32_t, uint64_t*);

static void *lib;
static f_i_i  p_force;
static f_i_u  p_set_counting, p_set_thread_counting;
static f_u_u  p_get_counter_count;
static f_read p_get_thread_counters;
static uint32_t n_counters;

// stage counts the kpc_open steps that must be undone: 1 = counters forced,
// 2 = counting enabled, 3 = thread counting enabled (fully open).
static int stage;

static void kpc_close(void) {
    if (stage >= 3) p_set_thread_counting(0);
    if (stage >= 2) p_set_counting(0);
    if (stage >= 1) p_force(0);
    stage = 0;
    p_force = 0;
    p_set_counting = 0;
    p_set_thread_counting = 0;
    p_get_counter_count = 0;
    p_get_thread_counters = 0;
    if (lib) {
        dlclose(lib);
        lib = 0;
    }
}

static const char *kpc_open(void) {
    const char *err;
    if (stage) return "already open";
    lib = dlopen("/System/Library/PrivateFrameworks/kperf.framework/kperf", RTLD_LAZY);
    if (!lib) return "dlopen kperf failed";
    p_force = (f_i_i)dlsym(lib, "kpc_force_all_ctrs_set");
    p_set_counting = (f_i_u)dlsym(lib, "kpc_set_counting");
    p_set_thread_counting = (f_i_u)dlsym(lib, "kpc_set_thread_counting");
    p_get_counter_count = (f_u_u)dlsym(lib, "kpc_get_counter_count");
    p_get_thread_counters = (f_read)dlsym(lib, "kpc_get_thread_counters");
    err = "missing kpc_* symbols";
    if (!p_force || !p_set_counting || !p_set_thread_counting || !p_get_thread_counters) goto fail;
    err = "kpc_force_all_ctrs_set failed (need root, EPERM)";
    if (p_force(1) != 0) goto fail;
    stage = 1;
    err = "kpc_set_counting failed (PMU busy? do not run xctrace)";
    if (p_set_counting(1u) != 0) goto fail;
    stage = 2;
    err = "kpc_set_thread_counting failed";
    if (p_set_thread_counting(1u) != 0) goto fail;
    stage = 3;
    // Resolve the fixed-counter count once so kpc_read adds no extra call to
    // measured intervals.
    n_counters = 32;
    if (p_get_counter_count) {
        n_counters = p_get_counter_count(1u);
        err = "fixed counter count is outside [2, 32]";
        if (n_counters < 2 || n_counters > 32) goto fail;
    }
    return 0;
fail:
    kpc_close();
    return err;
}

static int kpc_read(uint64_t *buf) {
    if (!p_get_thread_counters) return -1;
    return p_get_thread_counters(0, n_counters, buf);
}
*/
import "C"

import (
	"fmt"
	"unsafe"
)

const available = true

// Counters is a snapshot of the calling thread's fixed PMCs.
// On Apple Silicon, buf[0] is cycles and buf[1] is instructions.
type Counters struct {
	Cycles       uint64
	Instructions uint64
}

// Open starts fixed-counter counting for this process. Requires root.
func Open() error {
	if msg := C.kpc_open(); msg != nil {
		return fmt.Errorf("kpc_open: %s", C.GoString(msg))
	}
	return nil
}

// Close releases the PMU so Instruments can use it again.
func Close() { C.kpc_close() }

// readBuf is package-level because a local passed to cgo moves to the heap,
// which would add an allocation inside every measured interval.
var readBuf [32]C.uint64_t

// Read returns the calling OS thread's accumulated fixed counters.
// The caller must hold runtime.LockOSThread. Read is not safe for concurrent use.
func Read() (Counters, error) {
	if rc := C.kpc_read((*C.uint64_t)(unsafe.Pointer(&readBuf[0]))); rc != 0 {
		return Counters{}, fmt.Errorf("kpc_read failed: %d", int(rc))
	}
	return Counters{Cycles: uint64(readBuf[0]), Instructions: uint64(readBuf[1])}, nil
}
