//go:build kperf && darwin && arm64

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
static int opened;

static int kpc_open(void) {
    lib = dlopen("/System/Library/PrivateFrameworks/kperf.framework/kperf", RTLD_LAZY);
    if (!lib) return -1;
    p_force = (f_i_i)dlsym(lib, "kpc_force_all_ctrs_set");
    p_set_counting = (f_i_u)dlsym(lib, "kpc_set_counting");
    p_set_thread_counting = (f_i_u)dlsym(lib, "kpc_set_thread_counting");
    p_get_counter_count = (f_u_u)dlsym(lib, "kpc_get_counter_count");
    p_get_thread_counters = (f_read)dlsym(lib, "kpc_get_thread_counters");
    if (!p_force || !p_set_counting || !p_set_thread_counting || !p_get_thread_counters) return -2;
    if (p_force(1) != 0) return -3;
    if (p_set_counting(1u) != 0) return -4;
    if (p_set_thread_counting(1u) != 0) return -5;
    if (p_get_counter_count && p_get_counter_count(1u) < 2) return -6;
    opened = 1;
    return 0;
}

static int kpc_read(uint64_t *buf) {
    if (!p_get_thread_counters) return -1;
    uint32_t n = 32;
    if (p_get_counter_count) {
        uint32_t c = p_get_counter_count(1u);
        if (c > 0 && c < n) n = c;
        if (c > 32) return -2;
    }
    return p_get_thread_counters(0, n, buf);
}

static void kpc_close(void) {
    if (!opened) return;
    if (p_set_thread_counting) p_set_thread_counting(0);
    if (p_set_counting) p_set_counting(0);
    if (p_force) p_force(0);
    opened = 0;
}
*/
import "C"

import (
	"fmt"
	"unsafe"
)

// Counters is a snapshot of the calling thread's fixed PMCs.
// On Apple Silicon, buf[0] is cycles and buf[1] is instructions.
type Counters struct {
	Cycles       uint64
	Instructions uint64
}

// Open starts fixed-counter counting for this process. Requires root.
func Open() error {
	switch rc := C.kpc_open(); rc {
	case 0:
		return nil
	case -1:
		return fmt.Errorf("kpc_open: dlopen kperf failed")
	case -2:
		return fmt.Errorf("kpc_open: missing kpc_* symbols")
	case -3:
		return fmt.Errorf("kpc_open: kpc_force_all_ctrs_set failed (need root, EPERM)")
	case -4:
		return fmt.Errorf("kpc_open: kpc_set_counting failed (PMU busy? do not run xctrace)")
	case -5:
		return fmt.Errorf("kpc_open: kpc_set_thread_counting failed")
	case -6:
		return fmt.Errorf("kpc_open: fewer than 2 fixed counters")
	default:
		return fmt.Errorf("kpc_open: %d", int(rc))
	}
}

// Close releases the PMU so Instruments can use it again.
func Close() { C.kpc_close() }

// Read returns the calling OS thread's accumulated fixed counters.
// The caller must hold runtime.LockOSThread.
func Read() Counters {
	var buf [32]C.uint64_t
	if C.kpc_read((*C.uint64_t)(unsafe.Pointer(&buf[0]))) != 0 {
		return Counters{}
	}
	return Counters{Cycles: uint64(buf[0]), Instructions: uint64(buf[1])}
}
