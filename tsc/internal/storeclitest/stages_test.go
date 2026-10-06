package storeclitest

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"runtime/debug"
	"runtime/metrics"
	"runtime/pprof"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/microsoft/TypeScript/tsc/internal/ast"
	"github.com/microsoft/TypeScript/tsc/internal/ast/store"
	"github.com/microsoft/TypeScript/tsc/internal/checker"
	"github.com/microsoft/TypeScript/tsc/internal/compiler"
	"github.com/microsoft/TypeScript/tsc/internal/core"
	"github.com/microsoft/TypeScript/tsc/internal/diagnosticwriter"
	"github.com/microsoft/TypeScript/tsc/internal/execute"
	"github.com/microsoft/TypeScript/tsc/internal/execute/tsc"
	"github.com/microsoft/TypeScript/tsc/internal/storechecker"
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
//
// The runs of store-cli-parallel-instructions-20261007.md 5.4 to 5.6:
//   - STORE_CLI_STAGES_CHECKERS: --checkers, over the config's.
//   - STORE_CLI_STAGES_COUNTERS=0: the boundaries read no rusage counters and
//     no footprint (the timing runs; the counters are separate runs).
//   - STORE_CLI_STAGES_GC=gc100|gc-off: the common preparation of 5.5 after
//     the bind: GC off, two GCs, the boundary "prepared"; gc100 turns the GC
//     back on (GOGC 100), gc-off leaves it off until the run ends.
//   - STORE_CLI_STAGES_RETAIN=bound|checked: the run stops after the bind or
//     the second global stage, with two GCs there: retain-start after the
//     first, the second between retain-timed and retain-end (5.6), with the
//     live and scan bytes after each, and with STORE_CLI_STAGES_PROFILES the
//     heap profile retain-<point>.pb.gz. The program, the binder's output,
//     the pool and the diagnostics so far stay reachable.
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
	commandLine := &core.CompilerOptions{}
	if c := os.Getenv("STORE_CLI_STAGES_CHECKERS"); c != "" {
		n, err := strconv.Atoi(c)
		if err != nil {
			t.Fatal(err)
		}
		commandLine.Checkers = &n
	}
	gc, retain := os.Getenv("STORE_CLI_STAGES_GC"), os.Getenv("STORE_CLI_STAGES_RETAIN")
	if gc != "" && gc != "gc100" && gc != "gc-off" || retain != "" && retain != "bound" && retain != "checked" {
		t.Fatalf("STORE_CLI_STAGES_GC %q, STORE_CLI_STAGES_RETAIN %q", gc, retain)
	}
	gcPercent := debug.SetGCPercent(-1)
	debug.SetGCPercent(gcPercent)
	defer debug.SetGCPercent(gcPercent)

	var m stageMeter
	m.profiles = profiles
	m.counters = os.Getenv("STORE_CLI_STAGES_COUNTERS") != "0"
	m.gc, m.retain, m.backend = gc, retain, backend
	m.init()

	cwd := tspath.GetDirectoryPath(tspath.NormalizePath(configFile))
	sys := newOSSystem(cwd)
	ctx := context.Background()
	times := &tsc.CompileTimes{}
	var config *tsoptions.ParsedCommandLine
	var run tsc.CheckRun
	var status tsc.ExitStatus
	var diagnostics, files int
	var checkers []checkerCounts
	var stoppedAt string
	if backend == core.ASTBackendStore {
		host := storecompiler.NewCachedFSCompilerHost(cwd, sys.FS(), sys.DefaultLibraryPath(), nil, nil)
		config = parseConfigWith(t, host, configFile, commandLine)
		m.mark("start")
		program := storecompiler.NewProgram(storecompiler.ProgramOptions{Config: config, Host: host})
		m.mark("build")
		var ds []*store.Diagnostic
		stoppedAt = m.untilStop(func() {
			ds, run = tsc.CollectCheckDiagnostics(ctx, sys, times, markStages(execute.StoreCheckStages(program), &m))
		})
		if stoppedAt == "" {
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
			checkers = storeCheckerCounts(program)
		}
		runtime.KeepAlive(program)
	} else {
		host := compiler.NewCachedFSCompilerHost(cwd, sys.FS(), sys.DefaultLibraryPath(), nil, nil, nil)
		config = parseConfigWith(t, host, configFile, commandLine)
		m.mark("start")
		program := compiler.NewProgram(compiler.ProgramOptions{Config: config, Host: host})
		m.mark("build")
		var ds []*ast.Diagnostic
		stoppedAt = m.untilStop(func() {
			ds, run = tsc.CollectCheckDiagnostics(ctx, sys, times, markStages(execute.PointerCheckStages(program), &m))
		})
		if stoppedAt == "" {
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
			checkers = pointerCheckerCounts(program)
		}
		runtime.KeepAlive(program)
	}
	var poolRecord json.RawMessage
	if poolRecordOf != nil && stoppedAt == "" {
		poolRecord = poolRecordOf(backend)
	}
	if reasons := execute.CheckModeUnsupported(config); len(reasons) != 0 {
		t.Fatalf("%s is outside the check mode", configFile)
	}
	if stoppedAt == "" && !run.CheckExecuted {
		t.Fatalf("%s: the semantic stage did not run (%v)", configFile, run.Stages)
	}

	options := config.CompilerOptions()
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
		// 7f
		"singleThreaded":      options.SingleThreaded.IsTrue(),
		"checkersOption":      options.Checkers,
		"checkers":            checkers,
		"counters":            m.counters,
		"gc":                  gc,
		"retain":              retain,
		"stoppedAt":           stoppedAt,
		"gcPercent":           gcPercent,
		"memoryLimit":         debug.SetMemoryLimit(-1),
		"monotonicStartS":     m.monotonicStart,
		"poolCheckersAtBound": m.poolCheckersAtBound,
		"poolRecord":          poolRecord,
		"env": map[string]string{
			"GOGC":       os.Getenv("GOGC"),
			"GOMEMLIMIT": os.Getenv("GOMEMLIMIT"),
			"GODEBUG":    os.Getenv("GODEBUG"),
			"GOMAXPROCS": os.Getenv("GOMAXPROCS"),
		},
	}
	b, _ := json.MarshalIndent(result, "", " ")
	if err := os.WriteFile(out, b, 0o644); err != nil {
		t.Fatal(err)
	}
}

func parseConfigWith(tb testing.TB, host tsoptions.ParseConfigHost, configFile string, commandLine *core.CompilerOptions) *tsoptions.ParsedCommandLine {
	config, diags := tsoptions.GetParsedCommandLineOfConfigFile(configFile, commandLine, nil, host, &tsc.ExtendedConfigCache{})
	if len(diags) != 0 {
		tb.Fatalf("%s: %d config diagnostics", configFile, len(diags))
	}
	return config
}

// checkerCounts are a checker's counts after the run, by the pool's index;
// with the poolrecord overlay, Stats (Checker.PoolRecordStats) too.
type checkerCounts struct {
	Symbols        uint32         `json:"symbols"`
	Types          uint32         `json:"types"`
	Instantiations uint32         `json:"instantiations"`
	Stats          map[string]int `json:"stats,omitempty"`
}

// The hooks of the poolrecord overlay (stages_poolrecord_test.go): nil
// without it.
var (
	pointerCheckerStats func(*checker.Checker) map[string]int
	storeCheckerStats   func(*storechecker.Checker) map[string]int
	poolRecordOf        func(core.ASTBackend) json.RawMessage
)

func pointerCheckerCounts(program *compiler.Program) []checkerCounts {
	var mu sync.Mutex
	var counts []checkerCounts
	program.ForEachCheckerParallel(func(idx int, c *checker.Checker) {
		mu.Lock()
		defer mu.Unlock()
		counts = growCounts(counts, idx)
		counts[idx] = checkerCounts{c.SymbolCount, c.TypeCount, c.TotalInstantiationCount, nil}
		if pointerCheckerStats != nil {
			counts[idx].Stats = pointerCheckerStats(c)
		}
	})
	return counts
}

func storeCheckerCounts(program *storecompiler.Program) []checkerCounts {
	var mu sync.Mutex
	var counts []checkerCounts
	program.ForEachCheckerParallel(func(idx int, c *storechecker.Checker) {
		mu.Lock()
		defer mu.Unlock()
		counts = growCounts(counts, idx)
		counts[idx] = checkerCounts{c.SymbolCount, c.TypeCount, c.TotalInstantiationCount, nil}
		if storeCheckerStats != nil {
			counts[idx].Stats = storeCheckerStats(c)
		}
	})
	return counts
}

func growCounts(counts []checkerCounts, idx int) []checkerCounts {
	for len(counts) <= idx {
		counts = append(counts, checkerCounts{})
	}
	return counts
}

// markStages are stages with a boundary after each stage the collector runs
// and after the sort; the second global stage is global2. After the bind and
// the second global stage, the meter may prepare the GC or stop the run.
func markStages[D any](s tsc.CheckStages[D], m *stageMeter) tsc.CheckStages[D] {
	global := "global"
	return tsc.CheckStages[D]{
		Config:    func() []D { ds := s.Config(); m.mark("config"); return ds },
		Syntactic: func(ctx context.Context) []D { ds := s.Syntactic(ctx); m.mark("syntactic"); return ds },
		Program:   func() []D { ds := s.Program(); m.mark("program"); return ds },
		Bind: func(ctx context.Context) {
			s.Bind(ctx)
			m.mark("bind")
			m.afterBind()
		},
		Global: func(ctx context.Context) []D {
			ds := s.Global(ctx)
			m.mark(global)
			if global == "global2" {
				m.atPoint("checked")
			}
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
	HeapGoalBytes        uint64  `json:"heapGoalBytes"`
	Footprint            uint64  `json:"footprint"`
	MaxFootprint         uint64  `json:"maxFootprint"`
	Instructions         uint64  `json:"instructions"` // proc_pid_rusage, all threads
	Cycles               uint64  `json:"cycles"`
	LiveBytes            uint64  `json:"liveBytes,omitempty"`            // after forced GCs: the profiles, the preparation, the retained point
	ScanHeapBytes        uint64  `json:"scanHeapBytes,omitempty"`        // likewise
	HeapAlloc            uint64  `json:"heapAlloc,omitempty"`            // likewise
	HeapAllocsBytesAfter uint64  `json:"heapAllocsBytesAfter,omitempty"` // with profiles: after the profile is written
}

// stageMeter reads the boundaries into readings made beforehand.
type stageMeter struct {
	profiles   string
	counters   bool
	gc, retain string
	backend    core.ASTBackend
	// poolCheckersAtBound are the checkers of the pool after the bind, by
	// the poolrecord overlay (-1 without it): the bound point makes none.
	poolCheckersAtBound int
	t0                  time.Time
	monotonicStart      float64 // the monotonic clock of t0 (time's m=), the origin of gctrace's @
	samples             []metrics.Sample
	readings            [24]stageReading
	n                   int
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
	"/gc/heap/goal:bytes",
	"/gc/heap/live:bytes",
	"/gc/scan/heap:bytes",
}

func (m *stageMeter) init() {
	m.samples = make([]metrics.Sample, len(stageMetrics))
	for i, name := range stageMetrics {
		m.samples[i].Name = name
	}
	m.t0 = time.Now()
	if _, mono, ok := strings.Cut(m.t0.String(), " m=+"); ok {
		m.monotonicStart, _ = strconv.ParseFloat(mono, 64)
	}
}

// stopStages stops the collection at a retained point (untilStop).
type stopStages struct{ point string }

// untilStop runs collect and returns the point at which the meter stopped
// it, or "".
func (m *stageMeter) untilStop(collect func()) (point string) {
	defer func() {
		if v := recover(); v != nil {
			stop, ok := v.(stopStages)
			if !ok {
				panic(v)
			}
			point = stop.point
		}
	}()
	collect()
	return ""
}

// afterBind is the preparation of the GC control, then the bound point.
func (m *stageMeter) afterBind() {
	m.poolCheckersAtBound = -1
	if poolRecordOf != nil {
		var record struct {
			Checkers int `json:"checkers"`
		}
		_ = json.Unmarshal(poolRecordOf(m.backend), &record)
		m.poolCheckersAtBound = record.Checkers
	}
	if m.gc != "" {
		debug.SetGCPercent(-1)
		runtime.GC()
		runtime.GC()
		m.mark("prepared")
		m.state(&m.readings[m.n-1])
		if m.gc == "gc100" {
			debug.SetGCPercent(100)
		}
	}
	m.atPoint("bound")
}

// atPoint is the retained point: two GCs, the second timed, and the run
// stops.
func (m *stageMeter) atPoint(point string) {
	if m.retain != point {
		return
	}
	runtime.GC()
	m.mark("retain-start")
	m.state(&m.readings[m.n-1])
	m.mark("retain-timed")
	runtime.GC()
	m.mark("retain-end")
	m.state(&m.readings[m.n-1])
	if m.profiles != "" {
		m.writeProfile("retain-" + point)
	}
	panic(stopStages{point})
}

func (m *stageMeter) mark(stage string) {
	if m.profiles != "" && m.retain == "" {
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
	r.HeapGoalBytes = s[8].Value.Uint64()
	if m.counters {
		r.Footprint, r.MaxFootprint, r.Instructions, r.Cycles = procRusage()
	}
	if m.profiles == "" || m.retain != "" {
		return
	}
	m.state(r)
	m.writeProfile(strconv.Itoa(m.n-1) + "-" + stage)
	metrics.Read(m.samples[2:3])
	r.HeapAllocsBytesAfter = m.samples[2].Value.Uint64()
}

// state reads the heap after forced GCs into r: the live and scan bytes of
// the last GC, and HeapAlloc.
func (m *stageMeter) state(r *stageReading) {
	metrics.Read(m.samples)
	r.LiveBytes, r.ScanHeapBytes = m.samples[9].Value.Uint64(), m.samples[10].Value.Uint64()
	var ms runtime.MemStats
	runtime.ReadMemStats(&ms)
	r.HeapAlloc = ms.HeapAlloc
}

func (m *stageMeter) writeProfile(name string) {
	f, err := os.Create(filepath.Join(m.profiles, name+".pb.gz"))
	if err != nil {
		panic(err)
	}
	if err := pprof.Lookup("allocs").WriteTo(f, 0); err != nil {
		panic(err)
	}
	f.Close()
}

// BenchmarkStageMark is the cost of a boundary of the natural runs
// (store-cli-parallel-instructions-20261007.md 5.4), with and without the
// rusage counters.
func BenchmarkStageMark(b *testing.B) {
	for _, counters := range []bool{false, true} {
		name := "counters-off"
		if counters {
			name = "counters-on"
		}
		b.Run(name, func(b *testing.B) {
			var m stageMeter
			m.counters = counters
			m.init()
			for b.Loop() {
				m.n = 0
				m.mark("bench")
			}
		})
	}
}
