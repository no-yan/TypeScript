package storeharness

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"runtime/metrics"
	"runtime/pprof"
	"strconv"
	"strings"
	"testing"
)

// Retained heap and GC (store-checker-7d2c-implementation-instructions-
// 20261002.md 5, the review contracts' 3): each side in a process of its own,
// the sides alternating, each process with the baseline of the fixture read
// alone. Two stages: after parse and bind, and after the check (the work of
// measure_test.go). Each stage keeps its roots alive, collects twice (a
// sync.Pool's victim cache survives one collection) and reads HeapAlloc and a
// live heap profile. The GC run is another process of the same steps with
// gctrace on and the collector at its default: the last forced collection's
// line is the stage's mark CPU, and runtime/metrics the scannable heap.
//
//	STORE_CHECKER_RETAINED_OUT=dir  the results and the profiles (required)
//	STORE_CHECKER_RETAINED_RUNS=n   the runs of each side (3)
//	STORE_CHECKER_RETAINED_WORK=print  the work is printing's (print_test.go):
//	                                the compiler project's requests printed once
//	                                on a checker that has checked, in place of the check

const (
	retainedSideEnv = "STORE_CHECKER_RETAINED_SIDE" // set in a child: pointer or store
	retainedOutEnv  = "STORE_CHECKER_RETAINED_OUT"
	retainedFileEnv = "STORE_CHECKER_RETAINED_FILE" // a child's result
	retainedWorkEnv = "STORE_CHECKER_RETAINED_WORK" // check (the default) or print
)

// retainedStage is a stage of a child.
type retainedStage struct {
	Name      string
	HeapAlloc uint64
	ScanHeap  uint64 // /gc/scan/heap:bytes
	Profile   string
}

type retainedResult struct {
	Side   string
	Run    int
	GC     bool // the GC run (gctrace on), not the retained one
	Stages []retainedStage
	// The forced collections' gctrace lines, in order, one per collection:
	// two for each of the three stages (baseline, parse+bind, check).
	Trace []string
}

func collectTwice() {
	runtime.GC()
	runtime.GC()
}

func scanHeapBytes() uint64 {
	s := []metrics.Sample{{Name: "/gc/scan/heap:bytes"}}
	metrics.Read(s)
	if s[0].Value.Kind() != metrics.KindUint64 {
		return 0
	}
	return s[0].Value.Uint64()
}

// TestRetainedChild is a child's work; it runs only in a child.
func TestRetainedChild(t *testing.T) {
	side := os.Getenv(retainedSideEnv)
	if side == "" {
		t.Skip("a child of TestRetained")
	}
	out := os.Getenv(retainedFileEnv)
	r := retainedResult{Side: side}
	stage := func(name string) {
		collectTwice()
		var m runtime.MemStats
		runtime.ReadMemStats(&m)
		profile := strings.TrimSuffix(out, ".json") + "." + name + ".heap.pb.gz"
		f, err := os.Create(profile)
		if err != nil {
			t.Fatal(err)
		}
		if err := pprof.Lookup("heap").WriteTo(f, 0); err != nil {
			t.Fatal(err)
		}
		f.Close()
		r.Stages = append(r.Stages, retainedStage{Name: name, HeapAlloc: m.HeapAlloc, ScanHeap: scanHeapBytes(), Profile: filepath.Base(profile)})
	}

	fixture := readFixture(t)
	var target *printTarget
	if os.Getenv(retainedWorkEnv) == "print" {
		_, m := readPrintManifest(t)
		target = &m.Targets[0]
	}
	stage("baseline")
	switch side {
	case "pointer":
		program := newCompilerProjectProgram(t)
		program.BindSourceFiles()
		diagFiles, walkFiles := projectTargets(program)
		stage("bound")
		if target != nil {
			p := newPointerPrint(t, program, diagFiles, walkFiles, target.Requests)
			p.print()
			checkOutputs(t, target, p.out)
			stage("printed")
			runtime.KeepAlive(p)
		} else {
			c := checkPointer(t, program, diagFiles)
			stage("checked")
			runtime.KeepAlive(c)
		}
		runtime.KeepAlive(program)
	case "store":
		program := NewFixtureProgram(fixture)
		program.BindSourceFiles()
		diagFiles := fixtureDiagFiles(fixture, program)
		stage("bound")
		if target != nil {
			p := newStorePrint(t, program, target)
			p.print()
			checkOutputs(t, target, p.out)
			stage("printed")
			runtime.KeepAlive(p)
		} else {
			c := checkStore(t, program, diagFiles)
			stage("checked")
			runtime.KeepAlive(c)
		}
		runtime.KeepAlive(program)
	default:
		t.Fatalf("side %q", side)
	}
	runtime.KeepAlive(fixture)
	data, err := json.Marshal(r)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(out, data, 0o644); err != nil {
		t.Fatal(err)
	}
}

var gctraceForced = regexp.MustCompile(`^gc \d+ .*\(forced\)$`)

// TestRetained runs the children: for each run, the Pointer then the Store,
// the retained process then the GC process of each.
func TestRetained(t *testing.T) {
	dir := os.Getenv(retainedOutEnv)
	if dir == "" || os.Getenv(retainedSideEnv) != "" {
		t.Skip(retainedOutEnv + " is not set")
	}
	readFixture(t) // the fixture is there
	runs := envInt(t, "STORE_CHECKER_RETAINED_RUNS", 3)
	var results []retainedResult
	for run := range runs {
		for _, side := range []string{"pointer", "store"} {
			for _, gc := range []bool{false, true} {
				kind := "retained"
				if gc {
					kind = "gc"
				}
				file := filepath.Join(dir, fmt.Sprintf("%s-%s-%d.json", side, kind, run))
				cmd := exec.Command(os.Args[0], "-test.run=^TestRetainedChild$", "-test.count=1", "-test.timeout=30m")
				cmd.Env = append(os.Environ(), retainedSideEnv+"="+side, retainedFileEnv+"="+file)
				if gc {
					cmd.Env = append(cmd.Env, "GODEBUG=gctrace=1")
				}
				var stderr bytes.Buffer
				cmd.Stdout, cmd.Stderr = os.Stdout, &stderr
				if err := cmd.Run(); err != nil {
					t.Fatalf("%s: %v\n%s", file, err, stderr.String())
				}
				if err := os.WriteFile(strings.TrimSuffix(file, ".json")+".stderr", stderr.Bytes(), 0o644); err != nil {
					t.Fatal(err)
				}
				data, err := os.ReadFile(file)
				if err != nil {
					t.Fatal(err)
				}
				var r retainedResult
				if err := json.Unmarshal(data, &r); err != nil {
					t.Fatal(err)
				}
				r.Run, r.GC = run, gc
				scanner := bufio.NewScanner(&stderr)
				for scanner.Scan() {
					if line := scanner.Text(); gctraceForced.MatchString(line) {
						r.Trace = append(r.Trace, line)
					}
				}
				if gc && len(r.Trace) != 6 {
					t.Fatalf("%s: %d forced collections, not 6", file, len(r.Trace))
				}
				results = append(results, r)
			}
		}
	}
	var summary strings.Builder
	fmt.Fprintf(&summary, "side\trun\tkind\tstage\theapAlloc MB\tscanHeap MB\tmark CPU ms (last forced)\n")
	for _, r := range results {
		kind := "retained"
		if r.GC {
			kind = "gc"
		}
		for i, s := range r.Stages {
			mark := ""
			if r.GC {
				mark = strconv.FormatFloat(markCPU(r.Trace[2*i+1]), 'f', 3, 64)
			}
			fmt.Fprintf(&summary, "%s\t%d\t%s\t%s\t%.2f\t%.2f\t%s\n", r.Side, r.Run, kind, s.Name, mb(s.HeapAlloc), mb(s.ScanHeap), mark)
		}
	}
	t.Log("\n" + summary.String())
	data, _ := json.MarshalIndent(results, "", "  ")
	if err := os.WriteFile(filepath.Join(dir, "results.json"), data, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "summary.tsv"), []byte(summary.String()), 0o644); err != nil {
		t.Fatal(err)
	}
}

func mb(n uint64) float64 { return float64(n) / (1 << 20) }

var gctraceCPU = regexp.MustCompile(`, ([\d.]+)\+([\d.]+)/([\d.]+)/([\d.]+)\+([\d.]+) ms cpu,`)

// markCPU is a gctrace line's mark CPU: the assist, background and idle mark
// times.
func markCPU(line string) float64 {
	m := gctraceCPU.FindStringSubmatch(line)
	if m == nil {
		panic("gctrace: " + line)
	}
	total := 0.0
	for _, s := range m[2:5] {
		v, _ := strconv.ParseFloat(s, 64)
		total += v
	}
	return total
}
