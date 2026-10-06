package storeclitest

import (
	"encoding/json"
	"fmt"
	"os"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/microsoft/TypeScript/tsc/internal/core"
	"github.com/microsoft/TypeScript/tsc/internal/diagnosticwriter"
	"github.com/microsoft/TypeScript/tsc/internal/execute"
	"github.com/microsoft/TypeScript/tsc/internal/locale"
	"github.com/microsoft/TypeScript/tsc/internal/storeharness"
)

// corpusRecord is a line of the manifest of TestNativeCheckerConfigurations
// (store-cli-design-20261004.md 8.2): every enumerated input gets one, with
// its state.
type corpusRecord struct {
	Suite       string            `json:"suite"`
	ID          string            `json:"id"`
	File        string            `json:"file"`
	ContentHash string            `json:"contentHash"`
	Config      map[string]string `json:"config,omitempty"`
	// State: input-error, legacy-skip, prepare-rejected, native-unsupported,
	// equal, different, panic.
	State string `json:"state"`
	// Reason: the input error, the preparation's outcome, the native
	// reasons, or the panics.
	Reason string `json:"reason,omitempty"`
	// Derived: the options the native check adds to the case's
	// (store-cli-design 8.2): the case's values before.
	Derived       map[string]string `json:"derived,omitempty"`
	CaseSensitive bool              `json:"caseSensitive"`
	Cwd           string            `json:"cwd,omitempty"`
	Pointer       *corpusSide       `json:"pointer,omitempty"`
	Store         *corpusSide       `json:"store,omitempty"`
	FirstDiff     string            `json:"firstDiff,omitempty"`
}

type corpusSide struct {
	Status      int    `json:"status"`
	Lines       int    `json:"lines"`
	Files       int    `json:"files"` // listed files
	Diagnostics int    `json:"diagnostics"`
	Writes      int    `json:"writes"`
	Panic       string `json:"panic,omitempty"`
}

var diagnosticLine = regexp.MustCompile(`(?m)^(\S.*\(\d+,\d+\): |)(error|warning|message) TS\d+: `)

func runSide(files map[string]any, caseSensitive bool, cwd string, d *storeharness.Descriptor, backend core.ASTBackend, options *core.CompilerOptions) (result CLIResult, side *corpusSide) {
	side = &corpusSide{}
	defer func() {
		if v := recover(); v != nil {
			side.Panic = strings.SplitN(fmt.Sprint(v), "\n", 2)[0]
		}
	}()
	result = RunCheck(files, caseSensitive, cwd, d.CommandLine(options.Clone()), backend)
	side.Status = int(result.Status)
	side.Lines = strings.Count(result.Output, "\n")
	side.Diagnostics = len(diagnosticLine.FindAllString(result.Output, -1))
	for _, line := range strings.Split(result.Output, "\n") {
		if strings.HasPrefix(line, "/") || strings.HasPrefix(line, "bundled:") {
			side.Files++
		}
	}
	side.Writes = len(result.Writes)
	return result, side
}

// TestNativeCheckerConfigurations runs every configuration of the compiler
// and conformance tests as the native check of each backend, each from the
// case's input values on its own file system, and compares the output (the
// diagnostics and the listed files) and the exit status. The configurations
// the check mode does not take are recorded with their reasons; none is
// left out of the manifest.
//
// STORE_CLI_CORPUS_OUT: the manifest (JSONL); STORE_CLI_CORPUS_FILTER: a
// regexp of the ids to run (the others are recorded as not selected);
// STORE_CLI_CORPUS_WORKERS: parallel cases (default 4);
// STORE_CLI_CORPUS_MODE: single (the default: singleThreaded) or parallel
// (not singleThreaded, 4 checkers; store-cli-parallel-instructions-20261007.md
// 4.2).
func TestNativeCheckerConfigurations(t *testing.T) {
	if testing.Short() {
		t.Skip("the whole corpus")
	}
	parallel := false
	switch mode := os.Getenv("STORE_CLI_CORPUS_MODE"); mode {
	case "", "single":
	case "parallel":
		parallel = true
	default:
		t.Fatalf("STORE_CLI_CORPUS_MODE %q", mode)
	}
	cases, inputErrors, files, err := storeharness.EnumerateConfigurations()
	if err != nil {
		t.Fatal(err)
	}
	var filter *regexp.Regexp
	if f := os.Getenv("STORE_CLI_CORPUS_FILTER"); f != "" {
		filter = regexp.MustCompile(f)
	}
	workers := 4
	if w, err := strconv.Atoi(os.Getenv("STORE_CLI_CORPUS_WORKERS")); err == nil && w > 0 {
		workers = w
	}
	records := make([]*corpusRecord, len(cases))
	var wg sync.WaitGroup
	next := make(chan int)
	for range workers {
		wg.Go(func() {
			for i := range next {
				records[i] = runCorpusCase(cases[i], filter, parallel)
			}
		})
	}
	for i := range cases {
		next <- i
	}
	close(next)
	wg.Wait()
	for _, e := range inputErrors {
		records = append(records, &corpusRecord{Suite: e.Suite, File: e.File, ContentHash: e.ContentHash, State: "input-error", Reason: e.Reason})
	}

	counts := map[string]int{}
	var out *os.File
	if path := os.Getenv("STORE_CLI_CORPUS_OUT"); path != "" {
		if out, err = os.Create(path); err != nil {
			t.Fatal(err)
		}
		defer out.Close()
	}
	for _, r := range records {
		counts[r.State]++
		if out != nil {
			b, _ := json.Marshal(r)
			fmt.Fprintln(out, string(b))
		}
		if r.State == "different" || r.State == "panic" {
			t.Errorf("%s: %s %s %s", r.ID, r.State, r.Reason, r.FirstDiff)
		}
	}
	total := 0
	for _, n := range counts {
		total += n
	}
	t.Logf("%d test files, %d configurations, %d input errors: %v", files, len(cases), len(inputErrors), counts)
	if total != len(cases)+len(inputErrors) {
		t.Errorf("%d records for %d inputs", total, len(cases)+len(inputErrors))
	}
	if out != nil {
		b, _ := json.Marshal(map[string]any{"summary": counts, "files": files, "configurations": len(cases), "inputErrors": len(inputErrors), "parallel": parallel, "go": runtime.Version()})
		fmt.Fprintln(out, string(b))
	}
}

// checkersOfParallel is the checker count of the parallel mode, the CLI's
// default.
const checkersOfParallel = 4

func runCorpusCase(c *storeharness.Case, filter *regexp.Regexp, parallel bool) *corpusRecord {
	r := &corpusRecord{Suite: c.Suite, ID: c.ID, File: c.File, ContentHash: c.ContentHash, Config: c.Config}
	if filter != nil && !filter.MatchString(c.ID) {
		r.State = "not-selected"
		return r
	}
	if c.Skipped {
		r.State = "legacy-skip"
		return r
	}
	d, outcome := storeharness.PrepareCase(c)
	if outcome.Kind != "" {
		r.State, r.Reason = "prepare-rejected", outcome.String()
		return r
	}
	options := d.Options()
	r.Derived = map[string]string{
		"noEmit":         fmt.Sprint(options.NoEmit),
		"singleThreaded": fmt.Sprint(options.SingleThreaded),
		"listFiles":      fmt.Sprint(options.ListFiles),
	}
	options.NoEmit, options.SingleThreaded, options.ListFiles = core.TSTrue, core.TSTrue, core.TSTrue
	if parallel {
		// The case's singleThreaded and checkers are not kept.
		r.Derived["checkers"] = "<nil>"
		if options.Checkers != nil {
			r.Derived["checkers"] = strconv.Itoa(*options.Checkers)
		}
		checkers := checkersOfParallel
		options.SingleThreaded, options.Checkers = core.TSFalse, &checkers
	}
	r.CaseSensitive, r.Cwd = d.UseCaseSensitiveFileNames(), d.CurrentDirectory()
	if reasons := execute.CheckModeUnsupported(d.CommandLine(options.Clone())); len(reasons) != 0 {
		var texts []string
		for _, reason := range reasons {
			texts = append(texts, diagnosticwriter.WrapASTDiagnostic(reason).Localize(locale.Default))
		}
		r.State, r.Reason = "native-unsupported", strings.Join(texts, " ")
		return r
	}
	pointer, pside := runSide(d.FileMap(), r.CaseSensitive, r.Cwd, d, core.ASTBackendPointer, options)
	store, sside := runSide(d.FileMap(), r.CaseSensitive, r.Cwd, d, core.ASTBackendStore, options)
	r.Pointer, r.Store = pside, sside
	switch {
	case pside.Panic != "" || sside.Panic != "":
		r.State, r.Reason = "panic", fmt.Sprintf("pointer %q store %q", pside.Panic, sside.Panic)
	case pointer.Output == store.Output && pointer.Status == store.Status && len(pointer.Writes) == 0 && len(store.Writes) == 0:
		r.State = "equal"
	default:
		r.State = "different"
		r.FirstDiff = firstDifference(strings.Split(pointer.Output, "\n"), strings.Split(store.Output, "\n"))
	}
	return r
}
