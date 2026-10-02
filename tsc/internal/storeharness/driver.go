package storeharness

import (
	"context"
	"fmt"
	"runtime"
	"slices"
	"strings"

	"github.com/microsoft/TypeScript/tsc/internal/ast"
	"github.com/microsoft/TypeScript/tsc/internal/ast/store"
	"github.com/microsoft/TypeScript/tsc/internal/checker"
	"github.com/microsoft/TypeScript/tsc/internal/compiler"
	"github.com/microsoft/TypeScript/tsc/internal/core"
	"github.com/microsoft/TypeScript/tsc/internal/diagnostics"
	"github.com/microsoft/TypeScript/tsc/internal/storechecker"
)

// The driver (6.1): on each side, one checker made on the program, not the
// program's checkers (H1), then in the same order
//
//  1. GetDiagnostics and GetSuggestionDiagnostics of each file of the
//     diagnostic targets, in the program's order;
//  2. GetGlobalDiagnostics;
//  3. the type walk of each file of the walk targets, then the symbol walk of
//     each (the order of tsbaseline: its type baseline, then its symbol
//     baseline).
//
// Stage 1 (a program of files, Compare) has its root files as both targets.
// Stage 2 (a configuration of a test, configurations_test.go) has the source
// files of the program but the default libs whose check the options skip as
// the diagnostic targets, and the runner's walked files as the walk targets
// (store-checker-7d2b3-implementation-instructions-20261001.md 3.3).
//
// The diagnostics are the checker's, before the program's comment directives
// and sorting. A panic ends the side's run: the checker is not used after it.
// After everything is recorded, each side checks CompareSymbols on the
// symbols of its symbol walk (symbolorder.go).

// run is what the driver records of one side.
type run struct {
	diags       [][]diag // of each file of the diagnostic targets
	suggestions [][]diag
	global      []diag
	types       []walkRecord
	symbols     []walkRecord
	walked      []int // the type and symbol records of each walk target
	order       symbolOrder
	panicked    *panicInfo
	store       *Program // the Store side's program, once built
}

// diag is a diagnostic of either side, with the fields 6.2 compares, in its
// order.
type diag struct {
	file               string // the path, "" for none
	pos, end           int
	code               int32
	category           diagnostics.Category
	messageKey         diagnostics.Key
	messageArgs        []string
	messageText        string
	source             string
	reportsUnnecessary bool
	reportsDeprecated  bool
	skippedOnNoEmit    bool
	repopulate         *repopulateInfo
	messageChain       []diag
	relatedInformation []diag
}

type repopulateInfo struct {
	kind            int
	moduleReference string
	mode            core.ResolutionMode
	packageName     string
}

// panicInfo is a recovered panic: its message and the first frame of a
// Store package (internal/store*, not storeharness), which the class of 7.2
// names.
type panicInfo struct {
	message string
	frame   string
	stack   string
}

// Roots are the root files of the Pointer program that it has as source
// files, in its order, and the paths of the files it lacks.
func Roots(pointer *compiler.Program, files map[string]string) (roots []*ast.SourceFile, missing []string) {
	for name := range files {
		if pointer.GetSourceFile(name) == nil {
			missing = append(missing, name)
		}
	}
	for _, f := range pointer.SourceFiles() {
		if _, ok := files[f.FileName()]; ok {
			roots = append(roots, f)
		}
	}
	return roots, missing
}

// runPointer runs the driver on the Pointer program. hadErrorBaseline, when
// not nil, is the walker's hadErrorBaseline in place of the driver's rule
// (the diagnostics of the run, before the comment directives: H1).
func runPointer(program *compiler.Program, diagFiles, walkFiles []*ast.SourceFile, hadErrorBaseline *bool) (r *run) {
	r = &run{}
	defer func() {
		if v := recover(); v != nil {
			r.panicked = recovered(v)
		}
	}()
	ctx := context.Background()
	c, _ := checker.NewChecker(program, nil)
	for _, f := range diagFiles {
		r.diags = append(r.diags, pointerDiags(c.GetDiagnostics(ctx, f)))
		r.suggestions = append(r.suggestions, pointerDiags(c.GetSuggestionDiagnostics(ctx, f)))
	}
	r.global = pointerDiags(c.GetGlobalDiagnostics())
	hadErrors := len(r.global) > 0
	for i, f := range diagFiles {
		hadErrors = hadErrors || len(r.diags[i]) > 0 || len(f.Diagnostics()) > 0 || len(f.JSDiagnostics()) > 0 || len(f.BindDiagnostics()) > 0
	}
	if hadErrorBaseline != nil {
		hadErrors = *hadErrorBaseline
	}
	w := &pointerWalker{checker: c, hadErrors: hadErrors}
	for _, f := range walkFiles {
		records := w.walk(f, false /*isSymbolWalk*/)
		r.types = append(r.types, records...)
		r.walked = append(r.walked, len(records))
	}
	for i, f := range walkFiles {
		records := w.walk(f, true /*isSymbolWalk*/)
		r.symbols = append(r.symbols, records...)
		r.walked[i] += len(records)
	}
	r.order = checkSymbolOrder(r.symbols,
		func(a, b any) int { return c.CompareSymbols(a.(*ast.Symbol), b.(*ast.Symbol)) },
		func(s any) string { return s.(*ast.Symbol).Name })
	return r
}

// runStore builds the Store program (its parse is the Store's too) and runs
// the driver on it, with the targets of the Pointer program.
func runStore(build func() *Program, diagFiles, walkFiles []*ast.SourceFile) (r *run) {
	r = &run{}
	defer func() {
		if v := recover(); v != nil {
			r.panicked = recovered(v)
		}
	}()
	program := build()
	r.store = program
	stores := func(files []*ast.SourceFile) []*store.File {
		out := make([]*store.File, len(files))
		for i, f := range files {
			out[i] = program.Store(f)
		}
		return out
	}
	diagStores, walkStores := stores(diagFiles), stores(walkFiles)
	ctx := context.Background()
	c, _ := storechecker.NewChecker(program, nil)
	for _, f := range diagStores {
		r.diags = append(r.diags, storeDiags(c.GetDiagnostics(ctx, f)))
		r.suggestions = append(r.suggestions, storeDiags(c.GetSuggestionDiagnostics(ctx, f)))
	}
	r.global = storeDiags(c.GetGlobalDiagnostics())
	hadErrors := len(r.global) > 0
	for i, f := range diagStores {
		hadErrors = hadErrors || len(r.diags[i]) > 0 || len(f.Diagnostics()) > 0 || len(f.JSDiagnostics()) > 0 || len(f.Bound.BindDiagnostics()) > 0
	}
	w := newStoreWalker(c, program.IDSpace(), hadErrors)
	for _, f := range walkStores {
		records := w.walk(f, false /*isSymbolWalk*/)
		r.types = append(r.types, records...)
		r.walked = append(r.walked, len(records))
	}
	for i, f := range walkStores {
		records := w.walk(f, true /*isSymbolWalk*/)
		r.symbols = append(r.symbols, records...)
		r.walked[i] += len(records)
	}
	r.order = checkSymbolOrder(r.symbols,
		func(a, b any) int { return c.CompareSymbols(a.(*store.Symbol), b.(*store.Symbol)) },
		func(s any) string { return s.(*store.Symbol).Name })
	return r
}

func recovered(v any) *panicInfo {
	info := &panicInfo{message: fmt.Sprint(v)}
	pcs := make([]uintptr, 64)
	frames := runtime.CallersFrames(pcs[:runtime.Callers(3, pcs)])
	var stack strings.Builder
	for {
		frame, more := frames.Next()
		fmt.Fprintf(&stack, "%s\n\t%s:%d\n", frame.Function, frame.File, frame.Line)
		if info.frame == "" {
			if _, rest, ok := strings.Cut(frame.Function, "/tsc/internal/"); ok && strings.HasPrefix(rest, "store") && !strings.HasPrefix(rest, "storeharness") {
				info.frame = rest
			}
		}
		if !more {
			break
		}
	}
	info.stack = stack.String()
	return info
}

func pointerDiags(ds []*ast.Diagnostic) []diag {
	out := make([]diag, len(ds))
	for i, d := range ds {
		var file string
		if d.File() != nil {
			file = string(d.File().Path())
		}
		out[i] = diag{
			file:               file,
			pos:                d.Pos(),
			end:                d.End(),
			code:               d.Code(),
			category:           d.Category(),
			messageKey:         d.MessageKey(),
			messageArgs:        d.MessageArgs(),
			messageText:        d.MessageText(),
			source:             d.Source(),
			reportsUnnecessary: d.ReportsUnnecessary(),
			reportsDeprecated:  d.ReportsDeprecated(),
			skippedOnNoEmit:    d.SkippedOnNoEmit(),
			messageChain:       pointerDiags(d.MessageChain()),
			relatedInformation: pointerDiags(d.RelatedInformation()),
		}
		if info := d.RepopulateInfo(); info != nil {
			out[i].repopulate = &repopulateInfo{int(info.Kind), info.ModuleReference, info.Mode, info.PackageName}
		}
	}
	return out
}

func storeDiags(ds []*store.Diagnostic) []diag {
	out := make([]diag, len(ds))
	for i, d := range ds {
		var file string
		if d.File() != nil {
			file = string(d.File().Path())
		}
		out[i] = diag{
			file:               file,
			pos:                d.Pos(),
			end:                d.End(),
			code:               d.Code(),
			category:           d.Category(),
			messageKey:         d.MessageKey(),
			messageArgs:        d.MessageArgs(),
			messageText:        d.MessageText(),
			source:             d.Source(),
			reportsUnnecessary: d.ReportsUnnecessary(),
			reportsDeprecated:  d.ReportsDeprecated(),
			skippedOnNoEmit:    d.SkippedOnNoEmit(),
			messageChain:       storeDiags(d.MessageChain()),
			relatedInformation: storeDiags(d.RelatedInformation()),
		}
		if info := d.RepopulateInfo(); info != nil {
			out[i].repopulate = &repopulateInfo{int(info.Kind), info.ModuleReference, info.Mode, info.PackageName}
		}
	}
	return out
}

// Result is the comparison of one program: its class (7.2), the first
// difference that decided it, and the reason when it is excluded (7.1).
// Tokens counts the internal id tokens of both sides (tokens.go); Pairs and
// Fallback the pairs of CompareSymbols checked on each side, the Pointer's
// first, and those that reach the id fallback (symbolorder.go). A program
// excluded as the Pointer's nondeterminism keeps its class, and Rerun is the
// first difference of the Pointer's records in its run Reruns from those of
// its first run (Rerun.Pointer is the first run's, Rerun.Store the rerun's);
// RerunStore is the class of the Store's records against that rerun's, equal
// when the Store's are the rerun's. Counts are what each side recorded, the
// Pointer's first; SyntheticAsked the synthetic import specifiers the Store
// checker took from the adapter, and Extra the positions compareOptions.extra
// compared.
type Result struct {
	Class          string
	Excluded       string
	Detail         *Detail
	Tokens         int
	Pairs          [2]int
	Fallback       [2]int
	Rerun          *Detail
	Reruns         int
	RerunStore     string
	Counts         [2]RunCounts
	SyntheticAsked int
	Extra          int
}

// RunCounts is what a side's run recorded: its diagnostics, its walk records
// and the distinct symbols of its symbol walk, and the type and symbol records
// of each walk target.
type RunCounts struct {
	Diagnostics     int   `json:"diagnostics"`
	Suggestions     int   `json:"suggestions"`
	Global          int   `json:"global"`
	Types           int   `json:"types"`
	Symbols         int   `json:"symbols"`
	DistinctSymbols int   `json:"distinctSymbols"`
	Walked          []int `json:"walked,omitempty"`
}

func (r *run) counts() RunCounts {
	c := RunCounts{Global: len(r.global), Types: len(r.types), Symbols: len(r.symbols), Walked: r.walked}
	for i := range r.diags {
		c.Diagnostics += len(r.diags[i])
		c.Suggestions += len(r.suggestions[i])
	}
	symbols := map[any]bool{}
	for _, record := range r.symbols {
		if record.symbol != nil {
			symbols[record.symbol] = true
		}
	}
	c.DistinctSymbols = len(symbols)
	return c
}

// Detail is a difference: where it is and the two values.
type Detail struct {
	Where   string `json:"where"`
	Pointer string `json:"pointer"`
	Store   string `json:"store"`
}

// Exclusion reasons (store-checker-7d2b2-implementation-instructions-20261001.md
// 5.2).
const (
	ExcludedMissingRoot             = "a root is not a source file of the Pointer program"
	ExcludedPointerPanics           = "Pointer panics"
	ExcludedPointerNondeterministic = "Pointer nondeterministic"
)

// ExclusionReasons are the reasons, in the order the corpus logs them.
var ExclusionReasons = []string{ExcludedMissingRoot, ExcludedPointerPanics, ExcludedPointerNondeterministic}

// pointerReruns is how many times a program that is not equal runs the
// Pointer side again, to tell the Pointer's nondeterminism (5.2).
const pointerReruns = 10

// Compare builds the two programs of files, every file a root, and compares
// them (comparePrograms) with the root files as both targets. A rerun builds
// the Pointer program of files again.
func Compare(files map[string]string, options *core.CompilerOptions) Result {
	pointer, panicked := tryPointerProgram(files, options)
	if panicked != nil {
		return Result{Excluded: ExcludedPointerPanics, Detail: &Detail{Where: "the Pointer program", Pointer: panicked.message + "\n" + panicked.stack}}
	}
	if _, missing := Roots(pointer, files); len(missing) > 0 {
		slices.Sort(missing)
		return Result{Excluded: ExcludedMissingRoot, Detail: &Detail{Where: strings.Join(missing, ", ")}}
	}
	roots := func(program *compiler.Program) (diagFiles, walkFiles []*ast.SourceFile) {
		roots, _ := Roots(program, files)
		return roots, roots
	}
	rebuild := func() (*compiler.Program, func()) { return newPointerProgram(files, options), func() {} }
	return comparePrograms(pointer, roots, rebuild, compareOptions{})
}

// compareOptions are the optional parts of comparePrograms: extra compares
// more of two programs whose records are equal (stage 2's modes), and
// afterRuns, for the harness's own tests, sees the two runs, and may change
// the Store's, before they are compared.
type compareOptions struct {
	extra     func(pointer *compiler.Program, program *Program) (class string, detail *Detail, positions int)
	afterRuns func(p, s *run)
}

// comparePrograms runs the driver on the Pointer program, then on the Store
// program parsed again from its files, the targets of the Pointer program on
// both sides, and compares what they record, then, when they record the same,
// the properties of CompareSymbols on each side, then options.extra. A program
// whose records differ (not by a Store panic, and with no internal id token)
// runs the Pointer side again, on a new program of rebuild, up to
// pointerReruns times: a run whose records are not the first run's, by the
// same comparison, excludes the program as the Pointer's nondeterminism.
// rebuild's second value releases what its program holds, after the run.
func comparePrograms(pointer *compiler.Program, targets func(*compiler.Program) (diagFiles, walkFiles []*ast.SourceFile), rebuild func() (*compiler.Program, func()), options compareOptions) Result {
	diagFiles, walkFiles := targets(pointer)
	p := runPointer(pointer, diagFiles, walkFiles, nil)
	if p.panicked != nil {
		return Result{Excluded: ExcludedPointerPanics, Detail: &Detail{Where: p.panicked.frame, Pointer: p.panicked.message + "\n" + p.panicked.stack}}
	}
	s := runStore(func() *Program { return newProgram(pointer) }, diagFiles, walkFiles)
	if options.afterRuns != nil {
		options.afterRuns(p, s)
	}
	r := Result{Pairs: [2]int{p.order.pairs, s.order.pairs}, Fallback: [2]int{p.order.fallback, s.order.fallback}, Counts: [2]RunCounts{p.counts(), s.counts()}}
	if s.store != nil {
		r.SyntheticAsked = s.store.syntheticAsked
	}
	pointerTokens, pointerWhere, pointerValue := idTokens(p)
	storeTokens, storeWhere, storeValue := idTokens(s)
	r.Tokens = pointerTokens + storeTokens
	r.Class, r.Detail = compareRecords(p, s)
	switch {
	case pointerTokens > 0:
		r.Class, r.Detail = ClassInternalIDTokenPresent, &Detail{Where: "Pointer " + pointerWhere, Pointer: pointerValue}
	case storeTokens > 0:
		r.Class, r.Detail = ClassInternalIDTokenPresent, &Detail{Where: "Store " + storeWhere, Store: storeValue}
	case r.Class == ClassEqual:
		if class, detail := symbolOrderViolation(p, s); class != "" {
			r.Class, r.Detail = class, detail
		} else if options.extra != nil {
			var positions int
			class, detail, positions = options.extra(pointer, s.store)
			r.Extra = positions
			if class != "" {
				r.Class, r.Detail = class, detail
			}
		}
	case s.panicked == nil:
		for r.Reruns < pointerReruns {
			r.Reruns++
			again := rerunPointer(rebuild, targets)
			if class, detail := compareRecords(p, again); class != ClassEqual {
				r.Excluded, r.Rerun = ExcludedPointerNondeterministic, &Detail{Where: class + ": " + detail.Where, Pointer: detail.Pointer, Store: detail.Store}
				r.RerunStore, _ = compareRecords(again, s)
				break
			}
		}
	}
	return r
}

// rerunPointer runs the driver on a new Pointer program of rebuild, and
// releases it.
func rerunPointer(rebuild func() (*compiler.Program, func()), targets func(*compiler.Program) (diagFiles, walkFiles []*ast.SourceFile)) *run {
	program, release := rebuild()
	defer release()
	diagFiles, walkFiles := targets(program)
	return runPointer(program, diagFiles, walkFiles, nil)
}

// tryPointerProgram is newPointerProgram with its panic recovered: the
// Pointer side's, as a panic of its checker is.
func tryPointerProgram(files map[string]string, options *core.CompilerOptions) (pointer *compiler.Program, panicked *panicInfo) {
	defer func() {
		if v := recover(); v != nil {
			panicked = recovered(v)
		}
	}()
	return newPointerProgram(files, options), nil
}
