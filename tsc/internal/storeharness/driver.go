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
//  1. GetDiagnostics and GetSuggestionDiagnostics of each root file, in the
//     program's order;
//  2. GetGlobalDiagnostics;
//  3. the type walk of each root file, then the symbol walk of each (the
//     order of tsbaseline: its type baseline, then its symbol baseline).
//
// The diagnostics are the checker's, before the program's comment directives
// and sorting. A panic ends the side's run: the checker is not used after it.

// run is what the driver records of one side.
type run struct {
	diags       [][]diag // of each root file
	suggestions [][]diag
	global      []diag
	types       []walkRecord
	symbols     []walkRecord
	panicked    *panicInfo
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

func runPointer(program *compiler.Program, roots []*ast.SourceFile) (r *run) {
	r = &run{}
	defer func() {
		if v := recover(); v != nil {
			r.panicked = recovered(v)
		}
	}()
	ctx := context.Background()
	c, _ := checker.NewChecker(program, nil)
	for _, f := range roots {
		r.diags = append(r.diags, pointerDiags(c.GetDiagnostics(ctx, f)))
		r.suggestions = append(r.suggestions, pointerDiags(c.GetSuggestionDiagnostics(ctx, f)))
	}
	r.global = pointerDiags(c.GetGlobalDiagnostics())
	hadErrors := len(r.global) > 0
	for i, f := range roots {
		hadErrors = hadErrors || len(r.diags[i]) > 0 || len(f.Diagnostics()) > 0 || len(f.JSDiagnostics()) > 0 || len(f.BindDiagnostics()) > 0
	}
	w := &pointerWalker{checker: c, hadErrors: hadErrors}
	for _, f := range roots {
		r.types = append(r.types, w.walk(f, false /*isSymbolWalk*/)...)
	}
	for _, f := range roots {
		r.symbols = append(r.symbols, w.walk(f, true /*isSymbolWalk*/)...)
	}
	return r
}

// runStore builds the Store program (its parse is the Store's too) and runs
// the driver on it, with the root files of the Pointer program.
func runStore(build func() *Program, pointerRoots []*ast.SourceFile) (r *run) {
	r = &run{}
	defer func() {
		if v := recover(); v != nil {
			r.panicked = recovered(v)
		}
	}()
	program := build()
	roots := make([]*store.File, len(pointerRoots))
	for i, f := range pointerRoots {
		roots[i] = program.Store(f)
	}
	ctx := context.Background()
	c, _ := storechecker.NewChecker(program, nil)
	for _, f := range roots {
		r.diags = append(r.diags, storeDiags(c.GetDiagnostics(ctx, f)))
		r.suggestions = append(r.suggestions, storeDiags(c.GetSuggestionDiagnostics(ctx, f)))
	}
	r.global = storeDiags(c.GetGlobalDiagnostics())
	hadErrors := len(r.global) > 0
	for i, f := range roots {
		hadErrors = hadErrors || len(r.diags[i]) > 0 || len(f.Diagnostics()) > 0 || len(f.JSDiagnostics()) > 0 || len(f.Bound.BindDiagnostics()) > 0
	}
	w := newStoreWalker(c, program.IDSpace(), hadErrors)
	for _, f := range roots {
		r.types = append(r.types, w.walk(f, false /*isSymbolWalk*/)...)
	}
	for _, f := range roots {
		r.symbols = append(r.symbols, w.walk(f, true /*isSymbolWalk*/)...)
	}
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
type Result struct {
	Class    string
	Excluded string
	Detail   *Detail
}

// Detail is a difference: where it is and the two values.
type Detail struct {
	Where   string `json:"where"`
	Pointer string `json:"pointer"`
	Store   string `json:"store"`
}

// Exclusion reasons (7.1).
const (
	ExcludedMissingRoot   = "a root is not a source file of the Pointer program"
	ExcludedPointerPanics = "Pointer panics"
)

// Compare builds the two programs of files, runs the driver on each side,
// the Pointer first, and compares what they record.
func Compare(files map[string]string, options *core.CompilerOptions) Result {
	pointer, panicked := tryPointerProgram(files, options)
	if panicked != nil {
		return Result{Excluded: ExcludedPointerPanics, Detail: &Detail{Where: "the Pointer program", Pointer: panicked.message + "\n" + panicked.stack}}
	}
	roots, missing := Roots(pointer, files)
	if len(missing) > 0 {
		slices.Sort(missing)
		return Result{Excluded: ExcludedMissingRoot, Detail: &Detail{Where: strings.Join(missing, ", ")}}
	}
	p := runPointer(pointer, roots)
	if p.panicked != nil {
		return Result{Excluded: ExcludedPointerPanics, Detail: &Detail{Where: p.panicked.frame, Pointer: p.panicked.message + "\n" + p.panicked.stack}}
	}
	s := runStore(func() *Program { return newProgram(pointer) }, roots)
	class, detail := compareRuns(p, s)
	return Result{Class: class, Detail: detail}
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
