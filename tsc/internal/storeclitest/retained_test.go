package storeclitest

import (
	"context"
	"encoding/json"
	"os"
	"runtime"
	"runtime/metrics"
	"syscall"
	"testing"

	"github.com/microsoft/TypeScript/tsc/internal/core"
	"github.com/microsoft/TypeScript/tsc/internal/execute"
)

// TestNativeRetained is the retained heap and GC measurement of
// store-cli-verification-instructions-20261004.md 9.4, one backend and one
// point in a process: STORE_CLI_RETAINED_CONFIG (tsconfig),
// STORE_CLI_RETAINED_BACKEND (pointer|store), STORE_CLI_RETAINED_POINT
// (parse|bound|checked) and STORE_CLI_RETAINED_OUT (the JSON). The program
// is the check mode's: built, then bound (the collector's bind stage), then
// checked (the program, global, semantic and global stages). Before the
// program and at the point, two forced GCs: HeapAlloc after the second, and
// the mark CPU of the second (the runtime's assist, dedicated and idle mark
// estimates). The normal GC's cumulative values up to the point are taken
// before the forced GCs. The program, its checker and the diagnostics live
// until the end (runtime.KeepAlive).
func TestNativeRetained(t *testing.T) {
	configFile, out := os.Getenv("STORE_CLI_RETAINED_CONFIG"), os.Getenv("STORE_CLI_RETAINED_OUT")
	if configFile == "" || out == "" {
		t.Skip("STORE_CLI_RETAINED_CONFIG and STORE_CLI_RETAINED_OUT are not set")
	}
	backend := core.ASTBackendPointer
	if os.Getenv("STORE_CLI_RETAINED_BACKEND") == "store" {
		backend = core.ASTBackendStore
	}
	point := os.Getenv("STORE_CLI_RETAINED_POINT")
	if point != "parse" && point != "bound" && point != "checked" {
		t.Fatalf("STORE_CLI_RETAINED_POINT %q", point)
	}

	samples := []metrics.Sample{
		{Name: "/cpu/classes/gc/mark/assist:cpu-seconds"},
		{Name: "/cpu/classes/gc/mark/dedicated:cpu-seconds"},
		{Name: "/cpu/classes/gc/mark/idle:cpu-seconds"},
		{Name: "/gc/cycles/total:gc-cycles"},
		{Name: "/gc/cycles/forced:gc-cycles"},
		{Name: "/gc/scan/heap:bytes"},
		{Name: "/gc/heap/allocs:bytes"},
		{Name: "/memory/classes/total:bytes"},
	}
	read := func() map[string]float64 {
		metrics.Read(samples)
		m := map[string]float64{}
		for _, s := range samples {
			switch s.Value.Kind() {
			case metrics.KindFloat64:
				m[s.Name] = s.Value.Float64()
			case metrics.KindUint64:
				m[s.Name] = float64(s.Value.Uint64())
			}
		}
		return m
	}
	mark := func(m map[string]float64) float64 {
		return m["/cpu/classes/gc/mark/assist:cpu-seconds"] + m["/cpu/classes/gc/mark/dedicated:cpu-seconds"] + m["/cpu/classes/gc/mark/idle:cpu-seconds"]
	}
	heapAfterTwoGCs := func() (heap uint64, markCPU float64, before, after map[string]float64) {
		runtime.GC()
		before = read()
		runtime.GC()
		after = read()
		var ms runtime.MemStats
		runtime.ReadMemStats(&ms)
		return ms.HeapAlloc, mark(after) - mark(before), before, after
	}

	baseHeap, _, _, _ := heapAfterTwoGCs()
	ctx := context.Background()
	p := buildProgram(t, backend, configFile)
	var keep []any
	keep = append(keep, p)
	if point != "parse" {
		if backend == core.ASTBackendStore {
			stages := execute.StoreCheckStages(p.store)
			stages.Bind(ctx)
			if point == "checked" {
				keep = append(keep, stages.Program(), stages.Global(ctx), stages.Semantic(ctx), stages.Global(ctx))
			}
		} else {
			stages := execute.PointerCheckStages(p.pointer)
			stages.Bind(ctx)
			if point == "checked" {
				keep = append(keep, stages.Program(), stages.Global(ctx), stages.Semantic(ctx), stages.Global(ctx))
			}
		}
	}
	normal := read()
	var rusage syscall.Rusage
	_ = syscall.Getrusage(syscall.RUSAGE_SELF, &rusage)
	heap, markCPU, before, after := heapAfterTwoGCs()

	result := map[string]any{
		"config":                configFile,
		"backend":               backendName(backend),
		"point":                 point,
		"heapAlloc":             heap,
		"heapAllocMinusBase":    int64(heap) - int64(baseHeap),
		"baseHeapAlloc":         baseHeap,
		"forcedMarkCPUSeconds":  markCPU,
		"forcedGCScanHeapBytes": after["/gc/scan/heap:bytes"],
		"forcedCyclesBefore":    before["/gc/cycles/forced:gc-cycles"],
		"forcedCyclesAfter":     after["/gc/cycles/forced:gc-cycles"],
		"normalMarkCPUSeconds":  mark(normal),
		"normalGCCycles":        normal["/gc/cycles/total:gc-cycles"] - normal["/gc/cycles/forced:gc-cycles"],
		"normalScanHeapBytes":   normal["/gc/scan/heap:bytes"],
		"normalHeapAllocsBytes": normal["/gc/heap/allocs:bytes"],
		"memoryTotalBytes":      normal["/memory/classes/total:bytes"],
		"maxRSSBytes":           rusage.Maxrss, // darwin: bytes
		"gomaxprocs":            runtime.GOMAXPROCS(0),
		"go":                    runtime.Version(),
	}
	b, _ := json.MarshalIndent(result, "", " ")
	if err := os.WriteFile(out, b, 0o644); err != nil {
		t.Fatal(err)
	}
	runtime.KeepAlive(keep)
}
