package storeclitest

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"runtime/metrics"
	"runtime/pprof"
	"strconv"
	"syscall"
	"testing"
	"time"

	"github.com/microsoft/TypeScript/tsc/internal/ast"
	"github.com/microsoft/TypeScript/tsc/internal/ast/store"
	"github.com/microsoft/TypeScript/tsc/internal/compiler"
	"github.com/microsoft/TypeScript/tsc/internal/core"
	"github.com/microsoft/TypeScript/tsc/internal/diagnosticwriter"
	"github.com/microsoft/TypeScript/tsc/internal/execute"
	"github.com/microsoft/TypeScript/tsc/internal/execute/tsc"
	"github.com/microsoft/TypeScript/tsc/internal/storecompiler"
	"github.com/microsoft/TypeScript/tsc/internal/tsoptions"
	"github.com/microsoft/TypeScript/tsc/internal/tspath"
)

// TestNativeStages is the stage runner of
// store-checker-perf-analysis-instructions-20261005.md 3 and 6.2: one
// backend's check mode of a config in one process, from the build to the
// report as performPointerCheck and performStoreCheck run it, with the values
// of 3.2 read at the end of each stage. STORE_CLI_STAGES_CONFIG (tsconfig),
// STORE_CLI_STAGES_BACKEND (pointer|store) and STORE_CLI_STAGES_OUT (the
// JSON).
//
// With STORE_CLI_STAGES_PROFILES (a directory), each boundary first runs two
// GCs, then reads the live and scan bytes and writes the heap profile
// <n>-<stage>.pb.gz (6.2); STORE_CLI_STAGES_MEMPROFILERATE sets
// runtime.MemProfileRate. The GCs change the times: such a run is apart from
// the timing runs.
func TestNativeStages(t *testing.T) {
	configFile, out := os.Getenv("STORE_CLI_STAGES_CONFIG"), os.Getenv("STORE_CLI_STAGES_OUT")
	if configFile == "" || out == "" {
		t.Skip("STORE_CLI_STAGES_CONFIG and STORE_CLI_STAGES_OUT are not set")
	}
	backend := core.ASTBackendPointer
	if os.Getenv("STORE_CLI_STAGES_BACKEND") == "store" {
		backend = core.ASTBackendStore
	}
	profiles := os.Getenv("STORE_CLI_STAGES_PROFILES")
	if rate := os.Getenv("STORE_CLI_STAGES_MEMPROFILERATE"); rate != "" {
		n, err := strconv.Atoi(rate)
		if err != nil {
			t.Fatal(err)
		}
		runtime.MemProfileRate = n
	}

	var m stageMeter
	m.profiles = profiles
	m.init()

	cwd := tspath.GetDirectoryPath(tspath.NormalizePath(configFile))
	sys := newOSSystem(cwd)
	ctx := context.Background()
	times := &tsc.CompileTimes{}
	var config *tsoptions.ParsedCommandLine
	var run tsc.CheckRun
	var status tsc.ExitStatus
	var diagnostics, files int
	if backend == core.ASTBackendStore {
		host := storecompiler.NewCachedFSCompilerHost(cwd, sys.FS(), sys.DefaultLibraryPath(), nil, nil)
		config = parseConfig(t, host, configFile)
		m.mark("start")
		program := storecompiler.NewProgram(storecompiler.ProgramOptions{Config: config, Host: host})
		m.mark("build")
		var ds []*store.Diagnostic
		ds, run = tsc.CollectCheckDiagnostics(ctx, sys, times, markStages(execute.StoreCheckStages(program), &m))
		reporters := tsc.NewCheckReporters(sys, config.CompilerOptions(), config.CompilerOptions(), config.Locale())
		emitSkipped := tsc.CheckEmit(sys, times, func() bool { return false })
		fileNames := make([]string, 0, len(program.SourceFiles()))
		for _, file := range program.SourceFiles() {
			fileNames = append(fileNames, file.FileName())
		}
		status = tsc.ReportCompilation(sys, config.CompilerOptions(), reporters, tsc.CompilationReport{
			Diagnostics:   storecompiler.WrapDiagnostics(ds),
			FileNames:     fileNames,
			Times:         times,
			EmitSkipped:   emitSkipped,
			CheckExecuted: run.CheckExecuted,
		}, nil)
		m.mark("report")
		diagnostics, files = len(ds), len(fileNames)
		runtime.KeepAlive(program)
	} else {
		host := compiler.NewCachedFSCompilerHost(cwd, sys.FS(), sys.DefaultLibraryPath(), nil, nil, nil)
		config = parseConfig(t, host, configFile)
		m.mark("start")
		program := compiler.NewProgram(compiler.ProgramOptions{Config: config, Host: host})
		m.mark("build")
		var ds []*ast.Diagnostic
		ds, run = tsc.CollectCheckDiagnostics(ctx, sys, times, markStages(execute.PointerCheckStages(program), &m))
		reporters := tsc.NewCheckReporters(sys, config.CompilerOptions(), config.CompilerOptions(), config.Locale())
		emitSkipped := tsc.CheckEmit(sys, times, func() bool {
			result := program.Emit(ctx, compiler.EmitOptions{})
			if len(result.Diagnostics) != 0 {
				panic("the emit of a --noEmit check has diagnostics")
			}
			return result.EmitSkipped
		})
		fileNames := make([]string, 0, len(program.GetSourceFiles()))
		for _, file := range program.GetSourceFiles() {
			fileNames = append(fileNames, file.FileName())
		}
		status = tsc.ReportCompilation(sys, config.CompilerOptions(), reporters, tsc.CompilationReport{
			Diagnostics:   diagnosticwriter.FromASTDiagnostics(ds),
			FileNames:     fileNames,
			Times:         times,
			EmitSkipped:   emitSkipped,
			CheckExecuted: run.CheckExecuted,
		}, nil)
		m.mark("report")
		diagnostics, files = len(ds), len(fileNames)
		runtime.KeepAlive(program)
	}
	if reasons := execute.CheckModeUnsupported(config); len(reasons) != 0 {
		t.Fatalf("%s is outside the check mode", configFile)
	}
	if !run.CheckExecuted {
		t.Fatalf("%s: the semantic stage did not run (%v)", configFile, run.Stages)
	}

	result := map[string]any{
		"config":         configFile,
		"backend":        backendName(backend),
		"profiles":       profiles != "",
		"memProfileRate": runtime.MemProfileRate,
		"stages":         run.Stages,
		"status":         status,
		"diagnostics":    diagnostics,
		"files":          files,
		"readings":       m.readings[:m.n],
		"gomaxprocs":     runtime.GOMAXPROCS(0),
		"gogc":           os.Getenv("GOGC"),
		"go":             runtime.Version(),
	}
	b, _ := json.MarshalIndent(result, "", " ")
	if err := os.WriteFile(out, b, 0o644); err != nil {
		t.Fatal(err)
	}
}

// markStages are stages with a boundary after each stage the collector runs
// and after the sort; the second global stage is global2.
func markStages[D any](s tsc.CheckStages[D], m *stageMeter) tsc.CheckStages[D] {
	global := "global"
	return tsc.CheckStages[D]{
		Config:    func() []D { ds := s.Config(); m.mark("config"); return ds },
		Syntactic: func(ctx context.Context) []D { ds := s.Syntactic(ctx); m.mark("syntactic"); return ds },
		Program:   func() []D { ds := s.Program(); m.mark("program"); return ds },
		Bind:      func(ctx context.Context) { s.Bind(ctx); m.mark("bind") },
		Global: func(ctx context.Context) []D {
			ds := s.Global(ctx)
			m.mark(global)
			global = "global2"
			return ds
		},
		Semantic:           func(ctx context.Context) []D { ds := s.Semantic(ctx); m.mark("semantic"); return ds },
		SortAndDeduplicate: func(ds []D) []D { ds = s.SortAndDeduplicate(ds); m.mark("sort"); return ds },
	}
}

// stageReading is the values at a boundary, cumulative since the process
// started. The runtime's GC CPU is as of the last GC that finished.
type stageReading struct {
	Stage                string  `json:"stage"`
	WallNS               int64   `json:"wallNS"` // since the meter's init, monotonic
	UtimeNS              int64   `json:"utimeNS"`
	StimeNS              int64   `json:"stimeNS"`
	MinFlt               int64   `json:"minflt"`
	MajFlt               int64   `json:"majflt"`
	NVCSW                int64   `json:"nvcsw"`
	NIVCSW               int64   `json:"nivcsw"`
	GCCycles             uint64  `json:"gcCycles"`
	GCForced             uint64  `json:"gcForced"`
	HeapAllocsBytes      uint64  `json:"heapAllocsBytes"`
	HeapAllocsObjects    uint64  `json:"heapAllocsObjects"`
	MarkAssist           float64 `json:"markAssist"`
	MarkDedicated        float64 `json:"markDedicated"`
	MarkIdle             float64 `json:"markIdle"`
	Pause                float64 `json:"pause"`
	Footprint            uint64  `json:"footprint"`
	MaxFootprint         uint64  `json:"maxFootprint"`
	Instructions         uint64  `json:"instructions"` // proc_pid_rusage, all threads
	Cycles               uint64  `json:"cycles"`
	LiveBytes            uint64  `json:"liveBytes,omitempty"`            // with profiles: after the two GCs
	ScanHeapBytes        uint64  `json:"scanHeapBytes,omitempty"`        // with profiles: after the two GCs
	HeapAlloc            uint64  `json:"heapAlloc,omitempty"`            // with profiles: after the two GCs
	HeapAllocsBytesAfter uint64  `json:"heapAllocsBytesAfter,omitempty"` // with profiles: after the profile is written
}

// stageMeter reads the boundaries into readings made beforehand.
type stageMeter struct {
	profiles string
	t0       time.Time
	samples  []metrics.Sample
	readings [16]stageReading
	n        int
}

var stageMetrics = []string{
	"/gc/cycles/total:gc-cycles",
	"/gc/cycles/forced:gc-cycles",
	"/gc/heap/allocs:bytes",
	"/gc/heap/allocs:objects",
	"/cpu/classes/gc/mark/assist:cpu-seconds",
	"/cpu/classes/gc/mark/dedicated:cpu-seconds",
	"/cpu/classes/gc/mark/idle:cpu-seconds",
	"/cpu/classes/gc/pause:cpu-seconds",
	"/gc/heap/live:bytes",
	"/gc/scan/heap:bytes",
}

func (m *stageMeter) init() {
	m.samples = make([]metrics.Sample, len(stageMetrics))
	for i, name := range stageMetrics {
		m.samples[i].Name = name
	}
	m.t0 = time.Now()
}

func (m *stageMeter) mark(stage string) {
	if m.profiles != "" {
		runtime.GC()
		runtime.GC()
	}
	r := &m.readings[m.n]
	m.n++
	r.Stage = stage
	r.WallNS = time.Since(m.t0).Nanoseconds()
	var ru syscall.Rusage
	_ = syscall.Getrusage(syscall.RUSAGE_SELF, &ru)
	r.UtimeNS, r.StimeNS = ru.Utime.Nano(), ru.Stime.Nano()
	r.MinFlt, r.MajFlt, r.NVCSW, r.NIVCSW = ru.Minflt, ru.Majflt, ru.Nvcsw, ru.Nivcsw
	metrics.Read(m.samples)
	s := m.samples
	r.GCCycles, r.GCForced = s[0].Value.Uint64(), s[1].Value.Uint64()
	r.HeapAllocsBytes, r.HeapAllocsObjects = s[2].Value.Uint64(), s[3].Value.Uint64()
	r.MarkAssist, r.MarkDedicated, r.MarkIdle, r.Pause = s[4].Value.Float64(), s[5].Value.Float64(), s[6].Value.Float64(), s[7].Value.Float64()
	r.Footprint, r.MaxFootprint, r.Instructions, r.Cycles = procRusage()
	if m.profiles == "" {
		return
	}
	r.LiveBytes, r.ScanHeapBytes = s[8].Value.Uint64(), s[9].Value.Uint64()
	var ms runtime.MemStats
	runtime.ReadMemStats(&ms)
	r.HeapAlloc = ms.HeapAlloc
	f, err := os.Create(filepath.Join(m.profiles, strconv.Itoa(m.n-1)+"-"+stage+".pb.gz"))
	if err != nil {
		panic(err)
	}
	if err := pprof.Lookup("allocs").WriteTo(f, 0); err != nil {
		panic(err)
	}
	f.Close()
	metrics.Read(m.samples[2:3])
	r.HeapAllocsBytesAfter = m.samples[2].Value.Uint64()
}
