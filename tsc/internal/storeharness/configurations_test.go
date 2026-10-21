package storeharness

import (
	"bufio"
	"cmp"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"slices"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/microsoft/TypeScript/tsc/internal/ast"
	"github.com/microsoft/TypeScript/tsc/internal/ast/store"
	"github.com/microsoft/TypeScript/tsc/internal/ast/store/storetest"
	"github.com/microsoft/TypeScript/tsc/internal/compiler"
	"github.com/microsoft/TypeScript/tsc/internal/testutil/harnessutil"
	"github.com/microsoft/TypeScript/tsc/internal/tspath"
	"golang.org/x/sync/semaphore"
)

// The statuses of a case (store-checker-7d2b3-implementation-instructions-20261001.md
// 5): each case ends in one; the class of its comparison is apart.
const (
	statusEqual              = "equal"               // every comparison made, and equal
	statusMismatch           = "mismatch"            // a difference, a Store panic, a token, a violation
	statusRunnerSkipped      = "runner-skipped"      // the runner skips it
	statusRunnerRejected     = "runner-rejected"     // the runner ends it with t.Fatal
	statusAdapterUnsupported = "adapter-unsupported" // the adapter cannot give what the checker asks: none since the synthetic imports (2026-10-02)
	statusPointerExcluded    = "pointer-excluded"    // the Pointer panics, or is not deterministic
	statusHarnessError       = "harness-error"       // the harness's own fault
)

var statuses = []string{statusEqual, statusMismatch, statusRunnerSkipped, statusRunnerRejected, statusAdapterUnsupported, statusPointerExcluded, statusHarnessError}

// caseResult is what a case ended in: its status, the class of its
// comparison and the reason of the status, and what the comparison found.
type caseResult struct {
	Result
	status, reason string
	roots          int      // the program's root names (CompileFilesEx's programFileNames)
	missingRoots   []string // of them, those that are not source files of the program
	diagFiles      int
	walkFiles      int
	synthetic      [2]int // the Pointer program's files with a JSX runtime and an import helpers specifier
	elapsed        time.Duration
}

// caseHooks are views into compareCase: trace sees each rerun of the
// Pointer; for the harness's own tests, onBuild sees each Pointer program
// built for the case, the first and each rerun's, and afterRuns the two runs
// (compareOptions).
type caseHooks struct {
	trace     func(format string, args ...any)
	onBuild   func(d *configDescriptor, program *compiler.Program)
	afterRuns func(p, s *run)
}

// compareCase runs a case: the runner's skip and rejection, the Pointer
// program the runner builds for it, and the comparison of its two sides
// (comparePrograms) with every source file as a diagnostic target, the
// runner's walked files as the walk targets, and the modes of every module
// specifier (compareModes). A rerun builds the descriptor's program again.
func compareCase(c *configCase, hooks caseHooks) (r caseResult) {
	start := time.Now()
	defer func() {
		r.elapsed = time.Since(start)
		if v := recover(); v != nil {
			info := recovered(v)
			r.status, r.Class, r.reason = statusHarnessError, "panic", info.message+"\n"+info.stack
		}
	}()
	if c.Skipped {
		r.status, r.reason = statusRunnerSkipped, "skippedTests (compiler_runner.go)"
		return r
	}
	d, o := prepareCase(c)
	if d == nil {
		return outcomeResult(o)
	}
	if o := callWithT(func(t *testing.T) { harnessutil.SkipUnsupportedCompilerOptions(t, d.options) }); o.Kind != "" {
		return outcomeResult(o)
	}

	build := func() (*compiler.Program, func()) {
		program, release := d.build()
		if hooks.onBuild != nil {
			hooks.onBuild(d, program)
		}
		return program, release
	}
	pointer, release, panicked := tryBuild(build)
	if panicked != nil {
		r.status, r.Class, r.reason = statusPointerExcluded, ExcludedPointerPanics, "the Pointer program: "+panicked.message
		r.Detail = &Detail{Where: "the Pointer program", Pointer: panicked.message + "\n" + panicked.stack}
		return r
	}
	defer release()
	r.roots = len(d.rootNames)
	for _, name := range d.rootNames {
		if pointer.GetSourceFile(name) == nil {
			r.missingRoots = append(r.missingRoots, name.AsString())
		}
	}
	diagFiles, walkFiles := d.targets(pointer)
	r.diagFiles, r.walkFiles = len(diagFiles), len(walkFiles)
	r.synthetic = syntheticSpecifiers(pointer)

	reruns := 0
	rebuild := func() (*compiler.Program, func()) {
		reruns++
		if hooks.trace != nil {
			hooks.trace("rerun %d %s %s %d\n", c.Index, c.ID, c.ContentHash, reruns)
		}
		return build()
	}
	r.Result = comparePrograms(pointer, d.targets, rebuild, compareOptions{extra: compareModes, afterRuns: hooks.afterRuns})
	switch r.Excluded {
	case "":
		r.status = statusMismatch
		if r.Class == ClassEqual {
			r.status = statusEqual
		}
	case ExcludedPointerPanics:
		r.status, r.reason = statusPointerExcluded, ExcludedPointerPanics
		r.Class = ExcludedPointerPanics
	case ExcludedPointerNondeterministic:
		r.status, r.reason = statusPointerExcluded, ExcludedPointerNondeterministic
	default:
		r.status, r.reason = statusHarnessError, "unknown exclusion: "+r.Excluded
	}
	return r
}

// outcomeResult is the status of a preparation that ended: a t.Fatal of the
// runner's preparation is its rejection of the case, a t.Skip its skip, and a
// panic the harness's fault.
func outcomeResult(o tOutcome) caseResult {
	switch o.Kind {
	case "fatal":
		return caseResult{status: statusRunnerRejected, reason: o.String()}
	case "skip":
		return caseResult{status: statusRunnerSkipped, reason: o.String()}
	}
	return caseResult{status: statusHarnessError, reason: "preparation: " + o.String()}
}

// tryBuild is build with its panic recovered: the Pointer side's.
func tryBuild(build func() (*compiler.Program, func())) (program *compiler.Program, release func(), panicked *panicInfo) {
	defer func() {
		if v := recover(); v != nil {
			panicked = recovered(v)
		}
	}()
	program, release = build()
	return program, release, nil
}

// syntheticSpecifiers counts the source files of the Pointer program with a
// JSX runtime import specifier and with an import helpers specifier.
func syntheticSpecifiers(program *compiler.Program) (n [2]int) {
	for _, f := range program.SourceFiles() {
		if _, specifier := program.GetJSXRuntimeImportSpecifier(f.PathKey()); specifier != nil {
			n[0]++
		}
		if program.GetImportHelpersImportSpecifier(f.PathKey()) != nil {
			n[1]++
		}
	}
	return n
}

// compareModes is TestModeCorpus's comparison (mode_test.go) on a program of
// stage 2: at every module specifier of every source file but the default
// libs, and at its synthetic import specifiers, which are not in its tree, the
// Pointer program's and the adapter's mode, emit syntax and resolved module.
// The synthetic specifiers are those the adapter gives the checker: each is
// the Pointer's (one where the Pointer has one), the same node when asked
// again, and of the Pointer's shape (literal, import declaration, the file's
// root).
func compareModes(pointer *compiler.Program, program *Program) (class string, detail *Detail, positions int) {
	jsxImport := func(path tspath.PathKey) store.Node {
		_, specifier := program.GetJSXRuntimeImportSpecifier(path)
		return specifier
	}
	for _, pf := range pointer.SourceFiles() {
		if pointer.IsSourceFileDefaultLibrary(pf.PathKey()) {
			continue
		}
		sf := program.Store(pf)
		pairs, _, problems := pairNodes(pf, sf)
		if len(problems) > 0 {
			return "mode: the trees do not pair", &Detail{Where: pf.FileName().AsString(), Pointer: strings.Join(problems, "\n")}, positions
		}
		_, jsx := pointer.GetJSXRuntimeImportSpecifier(pf.PathKey())
		for _, synthetic := range []struct {
			role    string
			pointer *ast.Node
			get     func(tspath.PathKey) store.Node
		}{
			{"helpers", pointer.GetImportHelpersImportSpecifier(pf.PathKey()), program.GetImportHelpersImportSpecifier},
			{"jsx", jsx, jsxImport},
		} {
			specifier := synthetic.get(pf.PathKey())
			where := pf.FileName().AsString() + " (" + synthetic.role + ")"
			if (synthetic.pointer == nil) != specifier.IsNil() {
				return "mode: synthetic import", &Detail{Where: where, Pointer: fmt.Sprint(synthetic.pointer != nil), Store: fmt.Sprint(!specifier.IsNil())}, positions
			}
			if synthetic.pointer == nil {
				continue
			}
			if !store.SameNode(synthetic.get(pf.PathKey()), specifier) {
				return "mode: synthetic import identity", &Detail{Where: where, Store: "another node when asked again"}, positions
			}
			p, decl := synthetic.pointer, specifier.Parent()
			if p.Parent == nil || p.Parent.Kind != ast.KindImportDeclaration || p.Parent.Parent != pf.AsNode() ||
				decl.Kind() != ast.KindImportDeclaration || !store.SameNode(decl.Parent(), sf.Root()) || specifier.File() != sf ||
				specifier.Text() != p.Text() {
				return "mode: synthetic import shape", &Detail{Where: where, Pointer: p.Text(), Store: fmt.Sprintf("%q, parent %v", specifier.Text(), decl.Kind())}, positions
			}
			pairs = append(pairs, storetest.Pair{P: synthetic.pointer, N: specifier})
		}
		for _, pair := range pairs {
			place := specifierPlace(pair.P)
			if place == "" {
				continue
			}
			positions++
			if m := compareSpecifier(pointer, program, pf, sf, pair); len(m.mismatches) > 0 {
				first := m.mismatches[0]
				return "mode: " + first.method, &Detail{Where: fmt.Sprintf("%s at %d (%s %q)", pf.FileName(), pair.P.Pos(), place, pair.P.Text()), Pointer: first.values}, positions
			}
		}
	}
	return "", nil, positions
}

// configSelection is the cases a run takes, by STORE_CHECKER_CONFIG_FILTER,
// _SAMPLE and _SHARDS / _SHARD, in that order, from the indexes of every case;
// notSelected is why each other case is not taken.
type configSelection struct {
	selected    []*configCase
	notSelected map[int]string
}

func selectCases(tb testing.TB, cases []*configCase) configSelection {
	tb.Helper()
	s := configSelection{notSelected: map[int]string{}}
	selected := cases
	if filter := os.Getenv("STORE_CHECKER_CONFIG_FILTER"); filter != "" {
		var exact, substrings []string
		for item := range strings.SplitSeq(filter, ";") {
			if id, ok := strings.CutPrefix(item, "="); ok {
				exact = append(exact, id)
			} else if item != "" {
				substrings = append(substrings, item)
			}
		}
		for _, id := range exact {
			if !slices.ContainsFunc(cases, func(c *configCase) bool { return c.ID == id }) {
				tb.Fatalf("STORE_CHECKER_CONFIG_FILTER: no case %q", id)
			}
		}
		selected = slices.DeleteFunc(slices.Clone(selected), func(c *configCase) bool {
			if slices.Contains(exact, c.ID) || slices.ContainsFunc(substrings, func(sub string) bool { return strings.Contains(c.ID, sub) }) {
				return false
			}
			s.notSelected[c.Index] = "filter"
			return true
		})
	}
	if n := envInt(tb, "STORE_CHECKER_CONFIG_SAMPLE", 0); n > 0 && n < len(selected) {
		sampled := make([]*configCase, 0, n)
		taken := map[int]bool{}
		for i := range n {
			c := selected[i*len(selected)/n]
			sampled = append(sampled, c)
			taken[c.Index] = true
		}
		for _, c := range selected {
			if !taken[c.Index] {
				s.notSelected[c.Index] = "sample"
			}
		}
		selected = sampled
	}
	shards, shardSet := os.LookupEnv("STORE_CHECKER_CONFIG_SHARDS")
	shard, shardGiven := os.LookupEnv("STORE_CHECKER_CONFIG_SHARD")
	if shardSet || shardGiven {
		n, err := strconv.Atoi(shards)
		if err != nil || n <= 0 {
			tb.Fatalf("STORE_CHECKER_CONFIG_SHARDS=%q is not a positive integer", shards)
		}
		i, err := strconv.Atoi(shard)
		if err != nil || i < 0 || i >= n {
			tb.Fatalf("STORE_CHECKER_CONFIG_SHARD=%q is not in [0, %d)", shard, n)
		}
		selected = slices.DeleteFunc(slices.Clone(selected), func(c *configCase) bool {
			if c.Index%n == i {
				return false
			}
			s.notSelected[c.Index] = "shard"
			return true
		})
	}
	s.selected = selected
	return s
}

// TestCheckerConfigurations runs the driver on every configuration of every
// compiler and conformance test, as the program the testrunner builds for it
// (stage 2, store-checker-7d2b3-implementation-instructions-20261001.md), and
// ends each case in a status. Strict: a case that is mismatch,
// runner-rejected, adapter-unsupported or harness-error, and a test whose
// configurations are not enumerated and that the runner does not skip, fail
// it. Every run fails on the
// harness's own checks: the manifest's, a case not run, and the output read
// back.
//
//	STORE_CHECKER_CONFIG_REPORT=1   report: log every status, fail on the harness's checks only
//	STORE_CHECKER_CONFIG_OUT=path   a JSONL line for each case run, and the manifest at path less .jsonl, plus .manifest.jsonl
//	STORE_CHECKER_CONFIG_TRACE=path each case as it starts and ends, and each rerun of the Pointer
//	STORE_CHECKER_CONFIG_FILTER=s   the cases whose id contains s; =id for the case id; several separated by ;
//	STORE_CHECKER_CONFIG_SAMPLE=n   n of the cases, at a fixed interval
//	STORE_CHECKER_CONFIG_WORKERS=n  the workers (4)
//	STORE_CHECKER_CONFIG_SHARDS=n and STORE_CHECKER_CONFIG_SHARD=i  the cases whose index is i modulo n
func TestCheckerConfigurations(t *testing.T) {
	if testing.Short() {
		t.Skip("the configurations are not run with -short")
	}
	cases, inputErrors, files, err := enumerateConfigurations(runnerSuites)
	if err != nil {
		t.Fatal(err)
	}
	checkManifest(t, cases, inputErrors, files)
	selection := selectCases(t, cases)
	out := os.Getenv("STORE_CHECKER_CONFIG_OUT")
	if out != "" {
		if err := writeManifest(manifestPath(out), cases, inputErrors, selection); err != nil {
			t.Fatal(err)
		}
	}
	results := runCases(t, selection.selected, envInt(t, "STORE_CHECKER_CONFIG_WORKERS", 4))
	if out != "" {
		if err := writeCaseJSONL(out, selection.selected, results); err != nil {
			t.Error(err)
		} else if err := checkCaseJSONL(out, selection.selected, results); err != nil {
			t.Errorf("the output read back: %v", err)
		}
	}
	logConfigurations(t, cases, inputErrors, files, selection, results, os.Getenv("STORE_CHECKER_CONFIG_REPORT") == "")
}

// checkManifest is the enumeration's own check: every file enumerated or an
// input error, the ids unique and without ";" (FILTER's separator), the
// indexes in order.
func checkManifest(t *testing.T, cases []*configCase, inputErrors []*inputError, files int) {
	t.Helper()
	expanded := map[string]bool{}
	ids := map[string]bool{}
	for i, c := range cases {
		if c.Index != i {
			t.Fatalf("case %s has index %d at %d", c.ID, c.Index, i)
		}
		if ids[c.ID] {
			t.Fatalf("case id %s is not unique", c.ID)
		}
		if strings.Contains(c.ID, ";") {
			t.Fatalf("case id %s has a ;", c.ID)
		}
		ids[c.ID] = true
		expanded[c.Suite+"/"+c.File] = true
	}
	for _, e := range inputErrors {
		if expanded[e.Suite+"/"+e.File] {
			t.Fatalf("%s/%s is both enumerated and an input error", e.Suite, e.File)
		}
	}
	if len(expanded)+len(inputErrors) != files {
		t.Fatalf("%d files: %d enumerated and %d input errors", files, len(expanded), len(inputErrors))
	}
}

func runCases(t *testing.T, cases []*configCase, workers int) []caseResult {
	results := make([]caseResult, len(cases))
	var (
		mu      sync.Mutex
		running = map[int]time.Time{}
	)
	var trace *os.File
	if path := os.Getenv("STORE_CHECKER_CONFIG_TRACE"); path != "" {
		var err error
		if trace, err = os.Create(path); err != nil {
			t.Fatal(err)
		}
		defer trace.Close()
	}
	traceLine := func(format string, args ...any) {
		if trace != nil {
			mu.Lock()
			fmt.Fprintf(trace, format, args...)
			mu.Unlock()
		}
	}
	stopWatch := make(chan struct{})
	go func() {
		ticker := time.NewTicker(time.Minute)
		defer ticker.Stop()
		for {
			select {
			case <-stopWatch:
				return
			case now := <-ticker.C:
				mu.Lock()
				for i, start := range running {
					if d := now.Sub(start); d > time.Minute {
						t.Logf("running for %v: %s", d.Round(time.Second), cases[i].ID)
					}
				}
				mu.Unlock()
			}
		}
	}()
	// A test of 1MB or more runs with no other: as the corpus (corpus_test.go).
	budget := semaphore.NewWeighted(int64(workers))
	weight := func(c *configCase) int64 {
		if len(c.content) >= 1<<20 {
			return int64(workers)
		}
		return 1
	}
	start := time.Now()
	next := make(chan int)
	var wg sync.WaitGroup
	for range workers {
		wg.Go(func() {
			for i := range next {
				w := weight(cases[i])
				if err := budget.Acquire(context.Background(), w); err != nil {
					panic(err)
				}
				traceLine("start %d %s %s\n", cases[i].Index, cases[i].ID, cases[i].ContentHash)
				mu.Lock()
				running[i] = time.Now()
				mu.Unlock()
				results[i] = compareCase(cases[i], caseHooks{trace: traceLine})
				mu.Lock()
				delete(running, i)
				mu.Unlock()
				traceLine("end %d %s\n", cases[i].Index, results[i].status)
				budget.Release(w)
			}
		})
	}
	for i := range cases {
		next <- i
	}
	close(next)
	wg.Wait()
	close(stopWatch)
	wall := time.Since(start)
	var rusage syscall.Rusage
	_ = syscall.Getrusage(syscall.RUSAGE_SELF, &rusage)
	t.Logf("%d cases, %d workers: wall %v, %.1f ms a case of wall, max RSS %.2f GB",
		len(cases), workers, wall.Round(time.Second), float64(wall.Milliseconds())/float64(max(len(cases), 1)), float64(maxRSSBytes(rusage))/(1<<30))
	return results
}

func manifestPath(out string) string {
	return strings.TrimSuffix(out, ".jsonl") + ".manifest.jsonl"
}

// manifestLine is a line of the manifest: a case (its index, id, file,
// configuration, content hash, and whether the run takes it) or a test whose
// configurations are not enumerated (index -1).
type manifestLine struct {
	Kind         string            `json:"kind"` // "case" or "input-error"
	Index        int               `json:"index"`
	ID           string            `json:"id,omitempty"`
	Suite        string            `json:"suite"`
	File         string            `json:"file"`
	ContentHash  string            `json:"contentHash"`
	Config       map[string]string `json:"config"`
	SkippedTests bool              `json:"skippedTests,omitempty"`
	Reason       string            `json:"reason,omitempty"`
	Selected     bool              `json:"selected"`
	NotSelected  string            `json:"notSelected,omitempty"`
}

func manifestLines(cases []*configCase, inputErrors []*inputError, selection configSelection) []manifestLine {
	var lines []manifestLine
	for _, c := range cases {
		reason := selection.notSelected[c.Index]
		lines = append(lines, manifestLine{Kind: "case", Index: c.Index, ID: c.ID, Suite: c.Suite, File: c.File, ContentHash: c.ContentHash, Config: c.Config, SkippedTests: c.Skipped, Selected: reason == "", NotSelected: reason})
	}
	for _, e := range inputErrors {
		lines = append(lines, manifestLine{Kind: "input-error", Index: -1, Suite: e.Suite, File: e.File, ContentHash: e.ContentHash, SkippedTests: e.Skipped, Reason: e.Reason})
	}
	return lines
}

// writeManifest writes the manifest and reads it back.
func writeManifest(path string, cases []*configCase, inputErrors []*inputError, selection configSelection) error {
	lines := manifestLines(cases, inputErrors, selection)
	err := writeJSONL(path, func(enc *json.Encoder) error {
		for _, l := range lines {
			if err := enc.Encode(l); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return err
	}
	n := 0
	err = readJSONL(path, func(line []byte) error {
		var l manifestLine
		if err := json.Unmarshal(line, &l); err != nil {
			return err
		}
		if n >= len(lines) {
			return fmt.Errorf("more lines than the %d written", len(lines))
		}
		if w := lines[n]; l.Kind != w.Kind || l.Index != w.Index || l.ID != w.ID || l.Suite != w.Suite || l.File != w.File || l.ContentHash != w.ContentHash || l.Selected != w.Selected || configKey(l.Config) != configKey(w.Config) {
			return fmt.Errorf("%+v, written %+v", l, w)
		}
		n++
		return nil
	})
	if err == nil && n != len(lines) {
		err = fmt.Errorf("%d lines, %d written", n, len(lines))
	}
	if err != nil {
		return fmt.Errorf("the manifest read back: %w", err)
	}
	return nil
}

// readJSONL calls line with each line of the file, numbering an error with
// its line.
func readJSONL(path string, line func([]byte) error) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	scanner := bufio.NewScanner(f)
	scanner.Buffer(nil, 1<<30)
	n := 0
	for scanner.Scan() {
		n++
		if err := line(scanner.Bytes()); err != nil {
			return fmt.Errorf("line %d: %w", n, err)
		}
	}
	return scanner.Err()
}

func writeJSONL(path string, write func(*json.Encoder) error) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	w := bufio.NewWriterSize(f, 1<<20)
	if err := write(json.NewEncoder(w)); err != nil {
		f.Close()
		return err
	}
	if err := w.Flush(); err != nil {
		f.Close()
		return err
	}
	return f.Close()
}

// caseLine is the JSONL line of a case run.
type caseLine struct {
	Index        int           `json:"index"`
	ID           string        `json:"id"`
	ContentHash  string        `json:"contentHash"`
	Status       string        `json:"status"`
	Class        string        `json:"class,omitempty"`
	Reason       string        `json:"reason,omitempty"`
	Detail       *Detail       `json:"detail,omitempty"`
	Roots        int           `json:"roots"`
	MissingRoots []string      `json:"missingRoots,omitempty"`
	DiagFiles    int           `json:"diagFiles"`
	WalkFiles    int           `json:"walkFiles"`
	Counts       *[2]RunCounts `json:"counts,omitempty"` // Pointer and Store
	Tokens       int           `json:"tokens"`
	Pairs        [2]int        `json:"pairs"`    // CompareSymbols, Pointer and Store
	Fallback     [2]int        `json:"fallback"` // of them, those that reach the ids
	Modes        int           `json:"modePositions"`
	Synthetic    [2]int        `json:"syntheticSpecifiers"` // the Pointer's files with a JSX runtime and an import helpers specifier
	Asked        int           `json:"syntheticAsked"`      // the synthetic specifiers the Store checker took
	Reruns       int           `json:"reruns,omitempty"`
	Rerun        *Detail       `json:"rerun,omitempty"`      // the Pointer runs' first difference
	RerunStore   string        `json:"rerunStore,omitempty"` // the Store's records against that rerun's
	Millis       int64         `json:"ms"`
}

func (r caseResult) line(c *configCase) caseLine {
	l := caseLine{
		Index: c.Index, ID: c.ID, ContentHash: c.ContentHash, Status: r.status, Class: r.Class, Reason: r.reason, Detail: r.Detail,
		Roots: r.roots, MissingRoots: r.missingRoots, DiagFiles: r.diagFiles, WalkFiles: r.walkFiles,
		Tokens: r.Tokens, Pairs: r.Pairs, Fallback: r.Fallback, Modes: r.Extra, Synthetic: r.synthetic, Asked: r.SyntheticAsked,
		Reruns: r.Reruns, Rerun: r.Rerun, RerunStore: r.RerunStore, Millis: r.elapsed.Milliseconds(),
	}
	if r.status == statusEqual || r.status == statusMismatch || r.Excluded == ExcludedPointerNondeterministic {
		counts := r.Counts
		l.Counts = &counts
	}
	return l
}

func writeCaseJSONL(path string, cases []*configCase, results []caseResult) error {
	return writeJSONL(path, func(enc *json.Encoder) error {
		for i, r := range results {
			if err := enc.Encode(r.line(cases[i])); err != nil {
				return err
			}
		}
		return nil
	})
}

// checkCaseJSONL reads the JSONL back: a line for each case run, in order,
// with its status.
func checkCaseJSONL(path string, cases []*configCase, results []caseResult) error {
	n := 0
	err := readJSONL(path, func(line []byte) error {
		var l caseLine
		if err := json.Unmarshal(line, &l); err != nil {
			return err
		}
		if n >= len(cases) {
			return fmt.Errorf("more lines than the %d cases run", len(cases))
		}
		if l.Index != cases[n].Index || l.ID != cases[n].ID || l.Status != results[n].status || !slices.Contains(statuses, l.Status) {
			return fmt.Errorf("%d %s %q, want %d %s %q", l.Index, l.ID, l.Status, cases[n].Index, cases[n].ID, results[n].status)
		}
		n++
		return nil
	})
	if err != nil {
		return err
	}
	if n != len(cases) {
		return fmt.Errorf("%d lines for %d cases", n, len(cases))
	}
	return nil
}

// logConfigurations logs the enumeration, the selection, the statuses and
// their classes and reasons, and the counts; it checks that every case run
// ended in a status, and, strict, fails on the statuses that are not
// accepted.
func logConfigurations(t *testing.T, cases []*configCase, inputErrors []*inputError, files int, selection configSelection, results []caseResult, strict bool) {
	t.Helper()
	run := selection.selected // results[i] is run[i]'s
	t.Logf("%d test files: %d enumerated, %d not (input errors); %d cases", files, files-len(inputErrors), len(inputErrors), len(cases))
	for _, e := range inputErrors {
		t.Logf("  input error: %s/%s (runner skippedTests: %v): %s", e.Suite, e.File, e.Skipped, e.Reason)
	}
	byReason := map[string]int{}
	for _, reason := range selection.notSelected {
		byReason[reason]++
	}
	t.Logf("selected %d, not selected %d (filter %d, sample %d, shard %d)", len(selection.selected), len(selection.notSelected), byReason["filter"], byReason["sample"], byReason["shard"])
	if len(selection.selected)+len(selection.notSelected) != len(cases) {
		t.Errorf("%d selected and %d not of %d cases", len(selection.selected), len(selection.notSelected), len(cases))
	}

	type bucket struct {
		key      string
		count    int
		examples []int
	}
	count := func(m map[string]*bucket, key string, i int) {
		b := m[key]
		if b == nil {
			b = &bucket{key: key}
			m[key] = b
		}
		b.count++
		if len(b.examples) < 3 {
			b.examples = append(b.examples, i)
		}
	}
	sorted := func(m map[string]*bucket) []*bucket {
		out := slices.Collect(func(yield func(*bucket) bool) {
			for _, b := range m {
				if !yield(b) {
					return
				}
			}
		})
		slices.SortFunc(out, func(a, b *bucket) int { return cmp.Or(cmp.Compare(b.count, a.count), cmp.Compare(a.key, b.key)) })
		return out
	}
	byStatus := map[string]int{}
	reasons := map[string]map[string]*bucket{}
	classes := map[string]*bucket{}
	var tokens, modes, missingRoots, emptyWalks, withSynthetic, askedSynthetic int
	var pairs, fallback [2]int
	var failed []int
	for i, r := range results {
		if !slices.Contains(statuses, r.status) {
			t.Errorf("%s (index %d) ended in no status (%q): not run", run[i].ID, run[i].Index, r.status)
			continue
		}
		byStatus[r.status]++
		tokens += r.Tokens
		modes += r.Extra
		for side := range 2 {
			pairs[side] += r.Pairs[side]
			fallback[side] += r.Fallback[side]
		}
		if len(r.missingRoots) > 0 {
			missingRoots++
		}
		if r.status == statusEqual && r.walkFiles == 0 {
			emptyWalks++
		}
		if r.synthetic != [2]int{} {
			withSynthetic++
		}
		if r.SyntheticAsked > 0 {
			askedSynthetic++
		}
		switch r.status {
		case statusMismatch:
			count(classes, r.Class, i)
		case statusEqual:
		default:
			if reasons[r.status] == nil {
				reasons[r.status] = map[string]*bucket{}
			}
			count(reasons[r.status], reasonKey(r), i)
		}
		switch r.status {
		case statusMismatch, statusRunnerRejected, statusAdapterUnsupported, statusHarnessError:
			failed = append(failed, i)
		}
		if r.status == statusHarnessError {
			t.Errorf("harness error on %s (index %d): %s", run[i].ID, run[i].Index, clip(r.reason))
		}
	}
	sum := 0
	for _, status := range statuses {
		sum += byStatus[status]
		t.Logf("%-20s %6d", status, byStatus[status])
	}
	t.Logf("%-20s %6d", "total", sum)
	if sum != len(results) {
		t.Errorf("%d statuses for %d cases", sum, len(results))
	}
	for _, status := range statuses {
		if reasons[status] == nil {
			continue
		}
		t.Logf("%s by reason:", status)
		for _, b := range sorted(reasons[status]) {
			t.Logf("  %6d  %s (first %s)", b.count, clip(b.key), run[b.examples[0]].ID)
		}
	}
	t.Logf("mismatch by class: %d classes", len(classes))
	for i, b := range sorted(classes) {
		t.Logf("  %6d  %s", b.count, b.key)
		if i >= 30 {
			continue
		}
		for _, j := range b.examples {
			if d := results[j].Detail; d != nil {
				t.Logf("          %s (index %d): %s\n            Pointer: %s\n            Store:   %s", run[j].ID, run[j].Index, d.Where, clip(d.Pointer), clip(d.Store))
			}
		}
	}
	for i, r := range results {
		if r.Excluded == ExcludedPointerNondeterministic {
			t.Logf("  Pointer nondeterministic after %d reruns: %s (index %d), class %s, the Store against that rerun: %s\n    Pointer rerun: %s\n    Store: %s",
				r.Reruns, run[i].ID, run[i].Index, r.Class, r.RerunStore, r.Rerun.Where, r.Detail.Where)
		}
	}
	t.Logf("cases with a missing root: %d; equal cases with no walk target: %d; cases whose Pointer program has a synthetic import specifier: %d, whose Store checker took one: %d", missingRoots, emptyWalks, withSynthetic, askedSynthetic)
	t.Logf("internal id tokens: %d; module specifier positions compared: %d", tokens, modes)
	t.Logf("CompareSymbols: %d pairs checked on the Pointer side (%d reach the id fallback), %d on the Store side (%d)", pairs[0], fallback[0], pairs[1], fallback[1])
	if !strict {
		return
	}
	// A file the runner skips before it reads it (skippedTests) is not
	// enumerated by the runner either: its input error is the runner's skip.
	for _, e := range inputErrors {
		if !e.Skipped {
			t.Errorf("%s/%s: configurations not enumerated: %s", e.Suite, e.File, e.Reason)
		}
	}
	for n, i := range failed {
		if n == 50 {
			t.Errorf("… and %d more", len(failed)-n)
			break
		}
		where := results[i].reason
		if d := results[i].Detail; d != nil && results[i].status == statusMismatch {
			where = d.Where
		}
		t.Errorf("%s (index %d): %s: %s: %s", run[i].ID, run[i].Index, results[i].status, results[i].Class, clip(where))
	}
	if len(failed) > 0 {
		t.Errorf("%d cases not accepted", len(failed))
	}
}

// reasonKey groups the cases of a status other than equal and mismatch: the
// reason, with an adapter-unsupported case's file and module left out.
func reasonKey(r caseResult) string {
	if r.status == statusPointerExcluded {
		return r.reason
	}
	if i := strings.Index(r.reason, "\n"); i >= 0 {
		return r.reason[:i]
	}
	return r.reason
}
