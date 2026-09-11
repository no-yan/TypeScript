//go:build binderinvestigation && kperf && darwin && arm64 && cgo

package kperf

/*
#include <dlfcn.h>
#include <stdint.h>
#include <string.h>
#include <stdio.h>
#include <pthread.h>

extern void kperfInvestigationThreadCallback(uintptr_t);
static void *inv_worker(void *handle) {
    kperfInvestigationThreadCallback((uintptr_t)handle);
    return NULL;
}
static int inv_fresh_thread(uintptr_t handle) {
    pthread_t thread;
    int rc = pthread_create(&thread, NULL, inv_worker, (void *)handle);
    if (rc) return rc;
    return pthread_join(thread, NULL);
}

static void *inv_lib;
static int inv_reserved;
static int inv_used;
static int (*inv_force)(int);
static int (*inv_count)(uint32_t);
static int (*inv_thread)(uint32_t);
static uint32_t (*inv_ncounter)(uint32_t);
static uint32_t (*inv_nconfig)(uint32_t);
static int (*inv_setconfig)(uint32_t, uint64_t *);
static int (*inv_getconfig)(uint32_t, uint64_t *);
static int (*inv_read)(uint32_t, uint32_t, uint64_t *);

static void inv_close(void) {
    if (inv_reserved) {
        inv_thread(0);
        inv_count(0);
        inv_force(0);
        inv_reserved = 0;
    }
    if (inv_lib) { dlclose(inv_lib); inv_lib = 0; }
}

static int inv_open(uint64_t *config) {
    // Thread cumulative counters can retain an earlier session's baseline.
    // Experiments use a fresh process for every sample; reject reconfiguration.
    if (inv_lib || inv_used) return -1;
    inv_lib = dlopen("/System/Library/PrivateFrameworks/kperf.framework/kperf", RTLD_LAZY);
    if (!inv_lib) return -2;
#define INV_LOAD(dst, name) do { *(void **)(&dst) = dlsym(inv_lib, name); if (!dst) { inv_close(); return -3; } } while (0)
    INV_LOAD(inv_force, "kpc_force_all_ctrs_set");
    INV_LOAD(inv_count, "kpc_set_counting");
    INV_LOAD(inv_thread, "kpc_set_thread_counting");
    INV_LOAD(inv_ncounter, "kpc_get_counter_count");
    INV_LOAD(inv_nconfig, "kpc_get_config_count");
    INV_LOAD(inv_setconfig, "kpc_set_config");
    INV_LOAD(inv_getconfig, "kpc_get_config");
    INV_LOAD(inv_read, "kpc_get_thread_counters");
#undef INV_LOAD
    // This experiment is deliberately restricted to the recorded M1 layout.
    if (inv_ncounter(1) != 2 || inv_ncounter(2) != 8 || inv_nconfig(2) != 8) {
        inv_close(); return -4;
    }
    if (inv_force(1)) { inv_close(); return -5; }
    inv_reserved = 1;
    inv_used = 1;
    if (inv_setconfig(2, config)) { inv_close(); return -6; }
    if (inv_count(3) || inv_thread(3)) { inv_close(); return -8; }
    uint64_t actual[8] = {0};
    int readback_rc = inv_getconfig(2, actual);
    if (readback_rc || memcmp(actual, config, sizeof(actual))) {
        fprintf(stderr, "kpc readback rc=%d\n", readback_rc);
        for (int i=0; i<8; i++) fprintf(stderr, "PMC%d requested=%#llx actual=%#llx\n", i+2, config[i], actual[i]);
        inv_close(); return -7;
    }
    return 0;
}

static int inv_snapshot(uint64_t *buf) {
    if (!inv_reserved) return -1;
    return inv_read(0, 10, buf);
}
*/
import "C"

import (
	"fmt"
	"runtime/cgo"
	"unsafe"
)

// OpenEvents reserves the PMU with fixed EL0+EL1 and eight configurable slots.
// Config words are generated from the host's recorded kpep event database.
// CFGWORD_EL0A64EN is 0x20000 in Apple's osfmk/arm64/kpc.c; EL1 is left clear.
// One session per process; after CloseEvents, start a fresh process rather than
// reopening. Calls must be serialized and must not overlap Open or Instruments.
func OpenEvents(config [8]uint64) error {
	for _, word := range config {
		if word != 0 && (word&0xf0000 != 0x20000 || word&^uint64(0x200ff) != 0) {
			return fmt.Errorf("kperf: invalid M1 EL0-only config %#x", word)
		}
	}
	if rc := C.inv_open((*C.uint64_t)(unsafe.Pointer(&config[0]))); rc != 0 {
		return fmt.Errorf("kperf configurable open failed (%d): -4 layout, -5 permission, -6 set config, -7 readback, -8 enable", int(rc))
	}
	return nil
}

func CloseEvents() { C.inv_close() }

// OnFreshThread runs after OpenEvents on a pthread created after PMU setup.
// Existing threads can have wrapped cumulative baselines from earlier PMU
// configurations; LockOSThread alone does not create a fresh native thread.
// The callback must return errors, not call testing.Fatal or runtime.Goexit.
func OnFreshThread(fn func() error) error {
	call := &investigationThreadCall{fn: fn}
	handle := cgo.NewHandle(call)
	defer handle.Delete()
	if rc := C.inv_fresh_thread(C.uintptr_t(handle)); rc != 0 {
		return fmt.Errorf("kperf fresh thread failed: %d", int(rc))
	}
	if call.panicked {
		panic(call.panicValue)
	}
	return call.err
}

// ReadEvents fails explicitly: a failed read must never look like zero work.
// The caller must hold runtime.LockOSThread throughout the measured interval.
func ReadEvents() ([10]uint64, error) {
	var counters [10]uint64
	if rc := C.inv_snapshot((*C.uint64_t)(unsafe.Pointer(&counters[0]))); rc != 0 {
		return counters, fmt.Errorf("kperf configurable read failed: %d", int(rc))
	}
	return counters, nil
}
