package storeharness

import (
	"bufio"
	"cmp"
	"context"
	"encoding/json"
	"fmt"
	"hash/fnv"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/microsoft/TypeScript/tsc/internal/ast"
	"github.com/microsoft/TypeScript/tsc/internal/ast/store"
	"github.com/microsoft/TypeScript/tsc/internal/checker"
	"github.com/microsoft/TypeScript/tsc/internal/compiler"
	"github.com/microsoft/TypeScript/tsc/internal/nodebuilder"
	"github.com/microsoft/TypeScript/tsc/internal/printer"
	"github.com/microsoft/TypeScript/tsc/internal/repo"
	"github.com/microsoft/TypeScript/tsc/internal/storechecker"
	"github.com/microsoft/TypeScript/tsc/internal/storeprinter"
	"github.com/microsoft/TypeScript/tsc/internal/testutil/harnessutil"
	"github.com/microsoft/TypeScript/tsc/internal/testutil/kperf"
)

// Printing, end to end (store-checker-7d2c-implementation-instructions-
// 20261002.md 6, the review contracts' 4): the node builder, the printer and
// the string, on the Store (storeprinter) and on the Pointer (printer), on the
// same requests. The requests are a manifest, written once from the Pointer:
// the walk's requests of a target (the type walk's types that go through the
// node builder, then the symbol walk's symbols, SymbolToStringEx), each with
// the hash of its string, and the diagnostics of the target, their messages
// with their hash.
//
// The targets are the compiler project and 200 configurations of stage 2
// (9's 3): equal in 2b-3's strict run, the most diagnostics first, the index
// breaking ties. The Store side of each is a fixture (fixture.go), recorded
// with the manifest: its process has no Pointer program.
//
// The operations, each its own interval:
//
//   - miss: a new checker, which has checked the diagnostic targets and
//     resolved each request's type or symbol outside the interval, prints
//     every request once;
//   - hit: the same checker prints them again;
//   - build: miss with the node builder only (types only, no printer, no
//     string; symbols as in miss), so that miss less build is the printer's;
//   - diagnostics: a new checker, GetDiagnostics of the diagnostic targets,
//     GetGlobalDiagnostics and every message's text, the chains flattened. Its
//     interval holds the check: it is not printing alone.
//
// A string that is not the manifest's fails the run after the interval.
//
//	STORE_CHECKER_PRINT_DIR=dir  the manifest and the fixtures of the configurations
//	STORE_CHECKER_PRINT_STRIDE=n every n-th request of each target, from its first (1)

const printDirEnv = "STORE_CHECKER_PRINT_DIR"

const printConfigurations = 200

type printOp string

const (
	printType   printOp = "type"
	printSymbol printOp = "symbol"
)

// printRequest is a request: the node at Ordinal in the preorder of
// forEachASTNode (forEachStoreNode) of the walk target WalkFile, and what it
// prints. Pos and Kind are for the reader, and checked.
type printRequest struct {
	WalkFile int
	Ordinal  int
	Pos      int
	Kind     ast.Kind
	Op       printOp
	Hash     uint64
}

type printTarget struct {
	ID        string // "compiler" or a case id
	Fixture   string // the file of the fixture in the print dir; the compiler's is STORE_CHECKER_FIXTURE
	DiagPaths []string
	WalkPaths []string
	Requests  []printRequest
	// The diagnostics' count and the hash of their messages.
	Diagnostics int
	DiagHash    uint64
}

type printManifest struct {
	Selection string // how the configurations were chosen
	Targets   []printTarget
}

func hashString(s string) uint64 {
	h := fnv.New64a()
	h.Write([]byte(s))
	return h.Sum64()
}

func hashStrings(strings []string) uint64 {
	h := fnv.New64a()
	for _, s := range strings {
		h.Write([]byte(s))
		h.Write([]byte{0})
	}
	return h.Sum64()
}

// Pointer side.

type pointerPrint struct {
	checker  *checker.Checker
	files    []*ast.SourceFile // the walk targets
	nodes    [][]*ast.Node     // forEachASTNode of each
	requests []printRequest
	types    []*checker.Type // of each request, or nil
	symbols  []*ast.Symbol   // of each request, or nil
	at       []*ast.Node
	out      []string
}

func pointerHadErrors(c *checker.Checker, diagFiles []*ast.SourceFile, diags int) bool {
	hadErrors := diags > 0
	for _, f := range diagFiles {
		hadErrors = hadErrors || len(f.Diagnostics()) > 0 || len(f.JSDiagnostics()) > 0 || len(f.BindDiagnostics()) > 0
	}
	return hadErrors
}

func pointerCheckAll(c *checker.Checker, diagFiles []*ast.SourceFile) []*ast.Diagnostic {
	ctx := context.Background()
	var all []*ast.Diagnostic
	for _, f := range diagFiles {
		all = append(all, c.GetDiagnostics(ctx, f)...)
	}
	return append(all, c.GetGlobalDiagnostics()...)
}

func pointerMessages(ds []*ast.Diagnostic, into []string) []string {
	for _, d := range ds {
		into = append(into, d.MessageText())
		into = pointerMessages(d.MessageChain(), into)
		into = pointerMessages(d.RelatedInformation(), into)
	}
	return into
}

// newPointerPrint makes a checker on program, checks the diagnostic
// targets, and, with requests nil, derives the requests of the walk targets
// (the manifest's); with requests, resolves each of them.
func newPointerPrint(tb testing.TB, program *compiler.Program, diagFiles, walkFiles []*ast.SourceFile, requests []printRequest) *pointerPrint {
	c, _ := checker.NewChecker(program, nil)
	diags := pointerCheckAll(c, diagFiles)
	w := &pointerWalker{checker: c, hadErrors: pointerHadErrors(c, diagFiles, len(diags))}
	p := &pointerPrint{checker: c, files: walkFiles}
	for _, f := range walkFiles {
		p.nodes = append(p.nodes, forEachASTNode(f.AsNode()))
	}
	add := func(r printRequest, t *checker.Type, s *ast.Symbol, node *ast.Node) {
		p.requests = append(p.requests, r)
		p.types = append(p.types, t)
		p.symbols = append(p.symbols, s)
		p.at = append(p.at, node)
	}
	if requests == nil {
		for _, op := range []printOp{printType, printSymbol} {
			for i, f := range walkFiles {
				for ordinal, n := range p.nodes[i] {
					if !pointerWalked(n, op == printSymbol) {
						continue
					}
					r := printRequest{WalkFile: i, Ordinal: ordinal, Pos: n.Pos(), Kind: n.Kind, Op: op}
					if op == printType {
						if t, _, ok := w.typeOf(f, n); ok && t != nil {
							add(r, t, nil, n)
						}
					} else if s := c.GetSymbolAtLocation(n); s != nil {
						add(r, nil, s, n)
					}
				}
			}
		}
		return p
	}
	for _, r := range requests {
		n := p.nodes[r.WalkFile][r.Ordinal]
		if n.Pos() != r.Pos || n.Kind != r.Kind {
			tb.Fatalf("request %+v: the node is %v at %d", r, n.Kind, n.Pos())
		}
		if r.Op == printType {
			t, _, ok := w.typeOf(walkFiles[r.WalkFile], n)
			if !ok || t == nil {
				tb.Fatalf("request %+v: no type to print", r)
			}
			add(r, t, nil, n)
		} else {
			add(r, nil, c.GetSymbolAtLocation(n), n)
		}
	}
	return p
}

// print is the interval of miss and hit: every request printed, its string
// kept.
func (p *pointerPrint) print() {
	p.out = p.out[:0]
	for i, r := range p.requests {
		if r.Op == printType {
			p.out = append(p.out, pointerPrintType(p.checker, p.types[i], p.files[r.WalkFile], p.at[i]))
		} else {
			p.out = append(p.out, p.checker.SymbolToStringEx(p.symbols[i], p.at[i].Parent, ast.SymbolFlagsNone, checker.SymbolFormatFlagsAllowAnyNodeKind))
		}
	}
}

// build is the interval of build: the node builder of each type request.
func (p *pointerPrint) build() {
	for i, r := range p.requests {
		if r.Op == printType {
			ctx := printer.NewEmitContext()
			builder := checker.NewNodeBuilder(p.checker, ctx)
			typeFormatFlags := checker.TypeFormatFlagsNoTruncation | checker.TypeFormatFlagsAllowUniqueESSymbolType | checker.TypeFormatFlagsGenerateNamesForShadowedTypeParams
			builder.TypeToTypeNode(p.types[i], p.at[i].Parent, nodebuilder.Flags(typeFormatFlags&checker.TypeFormatFlagsNodeBuilderFlagsMask)|nodebuilder.FlagsIgnoreErrors, nodebuilder.InternalFlagsAllowUnresolvedNames, nil)
		} else {
			p.checker.SymbolToStringEx(p.symbols[i], p.at[i].Parent, ast.SymbolFlagsNone, checker.SymbolFormatFlagsAllowAnyNodeKind)
		}
	}
}

// Store side.

type storePrint struct {
	checker  *storechecker.Checker
	ctx      *storeprinter.EmitContext
	files    []*store.File
	requests []printRequest
	types    []*storechecker.Type
	symbols  []*store.Symbol
	at       []store.Node
	out      []string
}

func storeCheckAll(c *storechecker.Checker, diagFiles []*store.File) []*store.Diagnostic {
	ctx := context.Background()
	var all []*store.Diagnostic
	for _, f := range diagFiles {
		all = append(all, c.GetDiagnostics(ctx, f)...)
	}
	return append(all, c.GetGlobalDiagnostics()...)
}

func storeMessages(ds []*store.Diagnostic, into []string) []string {
	for _, d := range ds {
		into = append(into, d.MessageText())
		into = storeMessages(d.MessageChain(), into)
		into = storeMessages(d.RelatedInformation(), into)
	}
	return into
}

func storeFiles(tb testing.TB, p *FixtureProgram, paths []string) []*store.File {
	out := make([]*store.File, len(paths))
	for i, path := range paths {
		if out[i] = p.byPath[path]; out[i] == nil {
			tb.Fatalf("no file %s in the fixture", path)
		}
	}
	return out
}

func newStorePrint(tb testing.TB, program *FixtureProgram, target *printTarget) *storePrint {
	diagFiles, walkFiles := storeFiles(tb, program, target.DiagPaths), storeFiles(tb, program, target.WalkPaths)
	c, _ := storechecker.NewChecker(program, nil)
	diags := storeCheckAll(c, diagFiles)
	hadErrors := len(diags) > 0
	for _, f := range diagFiles {
		hadErrors = hadErrors || len(f.Diagnostics()) > 0 || len(f.JSDiagnostics()) > 0 || len(f.Bound.BindDiagnostics()) > 0
	}
	w := newStoreWalker(c, program.IDSpace(), hadErrors)
	p := &storePrint{checker: c, ctx: w.ctx, files: walkFiles, requests: target.Requests}
	nodes := make([][]store.Node, len(walkFiles))
	for i, f := range walkFiles {
		nodes[i] = forEachStoreNode(f.Root())
	}
	for _, r := range target.Requests {
		n := nodes[r.WalkFile][r.Ordinal]
		if int(n.Pos()) != r.Pos || n.Kind() != r.Kind {
			tb.Fatalf("request %+v: the node is %v at %d", r, n.Kind(), n.Pos())
		}
		var t *storechecker.Type
		var s *store.Symbol
		if r.Op == printType {
			var ok bool
			if t, _, ok = w.typeOf(walkFiles[r.WalkFile], n); !ok || t == nil {
				tb.Fatalf("request %+v: no type to print", r)
			}
		} else {
			s = c.GetSymbolAtLocation(n)
		}
		p.types = append(p.types, t)
		p.symbols = append(p.symbols, s)
		p.at = append(p.at, n)
	}
	return p
}

func (p *storePrint) print() {
	p.out = p.out[:0]
	for i, r := range p.requests {
		if r.Op == printType {
			p.out = append(p.out, storePrintType(p.checker, p.ctx, p.types[i], p.files[r.WalkFile], p.at[i]))
		} else {
			p.out = append(p.out, p.checker.SymbolToStringEx(p.symbols[i], p.at[i].Parent(), ast.SymbolFlagsNone, storechecker.SymbolFormatFlagsAllowAnyNodeKind))
		}
	}
}

func (p *storePrint) build() {
	for i, r := range p.requests {
		if r.Op == printType {
			resetEmitContext(p.ctx)
			builder := storechecker.NewNodeBuilder(p.checker, p.ctx)
			typeFormatFlags := storechecker.TypeFormatFlagsNoTruncation | storechecker.TypeFormatFlagsAllowUniqueESSymbolType | storechecker.TypeFormatFlagsGenerateNamesForShadowedTypeParams
			builder.TypeToTypeNode(p.types[i], p.at[i].Parent(), nodebuilder.Flags(typeFormatFlags&storechecker.TypeFormatFlagsNodeBuilderFlagsMask)|nodebuilder.FlagsIgnoreErrors, nodebuilder.InternalFlagsAllowUnresolvedNames, nil)
		} else {
			p.checker.SymbolToStringEx(p.symbols[i], p.at[i].Parent(), ast.SymbolFlagsNone, storechecker.SymbolFormatFlagsAllowAnyNodeKind)
		}
	}
}

func checkOutputs(tb testing.TB, target *printTarget, out []string) {
	tb.Helper()
	if len(out) != len(target.Requests) {
		tb.Fatalf("%s: %d strings for %d requests", target.ID, len(out), len(target.Requests))
	}
	for i, r := range target.Requests {
		if hashString(out[i]) != r.Hash {
			tb.Fatalf("%s: request %+v printed %q, not the manifest's", target.ID, r, out[i])
		}
	}
}

// The manifest and the fixtures.

func readPrintManifest(tb testing.TB) (string, *printManifest) {
	tb.Helper()
	dir := os.Getenv(printDirEnv)
	if dir == "" {
		tb.Skip(printDirEnv + " is not set")
	}
	data, err := os.ReadFile(filepath.Join(dir, "manifest.json"))
	if err != nil {
		tb.Fatal(err)
	}
	m := &printManifest{}
	if err := json.Unmarshal(data, m); err != nil {
		tb.Fatal(err)
	}
	if stride := envInt(tb, "STORE_CHECKER_PRINT_STRIDE", 1); stride > 1 {
		for i := range m.Targets {
			var kept []printRequest
			for j, r := range m.Targets[i].Requests {
				if j%stride == 0 {
					kept = append(kept, r)
				}
			}
			m.Targets[i].Requests = kept
		}
	}
	return dir, m
}

// printCaseIDs are the configurations of the manifest: 2b-3's strict run's
// equal cases, the most diagnostics (and global ones) first.
func printCaseIDs(tb testing.TB) []string {
	name := filepath.Join(repo.RootPath(), "internal/ast/docs/_store-checker-7d2b3-results/strict.jsonl")
	f, err := os.Open(name)
	if err != nil {
		tb.Fatal(err)
	}
	defer f.Close()
	type line struct {
		Index  int
		ID     string
		Status string
		Counts []RunCounts
	}
	var equal []line
	scanner := bufio.NewScanner(f)
	scanner.Buffer(nil, 1<<24)
	for scanner.Scan() {
		var l line
		if err := json.Unmarshal(scanner.Bytes(), &l); err != nil {
			tb.Fatal(err)
		}
		if l.Status == statusEqual {
			equal = append(equal, l)
		}
	}
	diags := func(l line) int { return l.Counts[0].Diagnostics + l.Counts[0].Global }
	slices.SortFunc(equal, func(a, b line) int {
		return cmp.Or(cmp.Compare(diags(b), diags(a)), cmp.Compare(a.Index, b.Index))
	})
	var ids []string
	for _, l := range equal[:printConfigurations] {
		ids = append(ids, l.ID)
	}
	return ids
}

// TestWritePrintManifest writes the manifest of the compiler project and of
// the configurations, and the fixtures of the configurations, into
// STORE_CHECKER_PRINT_DIR. The compiler project's fixture is
// TestWriteCompilerProjectFixture's.
func TestWritePrintManifest(t *testing.T) {
	dir := os.Getenv(printDirEnv)
	if dir == "" {
		t.Skip(printDirEnv + " is not set")
	}
	m := &printManifest{Selection: fmt.Sprintf("%d configurations: status equal in _store-checker-7d2b3-results/strict.jsonl, by diagnostics+global of the Pointer descending, then by index", printConfigurations)}

	target := func(id, fixture string, program *compiler.Program, diagFiles, walkFiles []*ast.SourceFile) printTarget {
		p := newPointerPrint(t, program, diagFiles, walkFiles, nil)
		p.print()
		pt := printTarget{ID: id, Fixture: fixture, Requests: p.requests}
		for i := range pt.Requests {
			pt.Requests[i].Hash = hashString(p.out[i])
		}
		for _, f := range diagFiles {
			pt.DiagPaths = append(pt.DiagPaths, string(f.PathKey()))
		}
		for _, f := range walkFiles {
			pt.WalkPaths = append(pt.WalkPaths, string(f.PathKey()))
		}
		c, _ := checker.NewChecker(program, nil)
		messages := pointerMessages(pointerCheckAll(c, diagFiles), nil)
		pt.Diagnostics, pt.DiagHash = len(messages), hashStrings(messages)
		return pt
	}

	pointer := newCompilerProjectProgram(t)
	diagFiles, walkFiles := projectTargets(pointer)
	m.Targets = append(m.Targets, target("compiler", "", pointer, diagFiles, walkFiles))

	cases, _, _, err := enumerateConfigurations(runnerSuites)
	if err != nil {
		t.Fatal(err)
	}
	byID := map[string]*configCase{}
	for _, c := range cases {
		byID[c.ID] = c
	}
	for i, id := range printCaseIDs(t) {
		c := byID[id]
		if c == nil {
			t.Fatalf("no case %s", id)
		}
		d, _ := prepareCase(c)
		if d == nil {
			t.Fatalf("%s: not prepared", id)
		}
		if o := callWithT(func(t *testing.T) { harnessutil.SkipUnsupportedCompilerOptions(t, d.options) }); o.Kind != "" {
			t.Fatalf("%s: %s", id, o.String())
		}
		program, release := d.build()
		diagFiles, walkFiles := d.targets(program)
		class, detail, fixture, err := fixtureIdentity(program, diagFiles, walkFiles)
		if err != nil || class != ClassEqual {
			t.Fatalf("%s: the fixture: %v %q %+v", id, err, class, detail)
		}
		name := fmt.Sprintf("%03d.fixture.json", i)
		if err := WriteFixture(fixture, filepath.Join(dir, name)); err != nil {
			t.Fatal(err)
		}
		m.Targets = append(m.Targets, target(id, name, program, diagFiles, walkFiles))
		release()
	}
	data, err := json.MarshalIndent(m, "", " ")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "manifest.json"), data, 0o644); err != nil {
		t.Fatal(err)
	}
	requests, diags := 0, 0
	for _, pt := range m.Targets {
		requests += len(pt.Requests)
		diags += pt.Diagnostics
	}
	t.Logf("%d targets, %d requests (compiler %d), %d messages", len(m.Targets), requests, len(m.Targets[0].Requests), diags)
}

// The measurements. A set is the compiler project or the configurations; an
// op of the set runs over each of its targets, the setup of each outside the
// interval.

type printSet string

const (
	setCompiler       printSet = "compiler"
	setConfigurations printSet = "configurations"
)

func (s printSet) targets(m *printManifest) []printTarget {
	if s == setCompiler {
		return m.Targets[:1]
	}
	return m.Targets[1:]
}

// pointerProgramOf builds the Pointer program of a target.
func pointerProgramOf(tb testing.TB, target *printTarget, cases map[string]*configCase) (*compiler.Program, []*ast.SourceFile, []*ast.SourceFile, func()) {
	if target.ID == "compiler" {
		program := newCompilerProjectProgram(tb)
		diagFiles, walkFiles := projectTargets(program)
		return program, diagFiles, walkFiles, func() {}
	}
	d, _ := prepareCase(cases[target.ID])
	program, release := d.build()
	diagFiles, walkFiles := d.targets(program)
	return program, diagFiles, walkFiles, release
}

func fixtureOf(tb testing.TB, dir string, target *printTarget) *Fixture {
	if target.ID == "compiler" {
		return readFixture(tb)
	}
	f, err := ReadFixture(filepath.Join(dir, target.Fixture))
	if err != nil {
		tb.Fatal(err)
	}
	return f
}

// interval runs fn as the measured interval: the benchmark's timer, or a KPC
// session's Measure.
type interval func(fn func())

func timerInterval(b *testing.B) interval {
	return func(fn func()) {
		b.StartTimer()
		fn()
		b.StopTimer()
	}
}

func kpcInterval(b *testing.B, session *kperf.Session) interval {
	return func(fn func()) {
		b.StartTimer()
		session.Measure(fn)
		b.StopTimer()
	}
}

var printOps = []string{"miss", "hit", "build", "diagnostics"}

func runPointerPrint(b *testing.B, set printSet, op string, measure interval) {
	_, m := readPrintManifest(b)
	var cases map[string]*configCase
	if set == setConfigurations {
		all, _, _, err := enumerateConfigurations(runnerSuites)
		if err != nil {
			b.Fatal(err)
		}
		cases = map[string]*configCase{}
		for _, c := range all {
			cases[c.ID] = c
		}
	}
	targets := set.targets(m)
	b.ReportAllocs()
	b.StopTimer()
	b.ResetTimer()
	for range b.N {
		for i := range targets {
			target := &targets[i]
			program, diagFiles, walkFiles, release := pointerProgramOf(b, target, cases)
			program.BindSourceFiles()
			if op == "diagnostics" {
				var messages []string
				measure(func() {
					c, _ := checker.NewChecker(program, nil)
					messages = pointerMessages(pointerCheckAll(c, diagFiles), nil)
				})
				if hashStrings(messages) != target.DiagHash {
					b.Fatalf("%s: the messages are not the manifest's", target.ID)
				}
				release()
				continue
			}
			p := newPointerPrint(b, program, diagFiles, walkFiles, target.Requests)
			switch op {
			case "miss":
				measure(p.print)
			case "hit":
				p.print()
				measure(p.print)
			case "build":
				measure(p.build)
			}
			if op != "build" {
				checkOutputs(b, target, p.out)
			}
			release()
		}
	}
}

func runStorePrint(b *testing.B, set printSet, op string, measure interval) {
	dir, m := readPrintManifest(b)
	targets := set.targets(m)
	fixtures := make([]*Fixture, len(targets))
	for i := range targets {
		fixtures[i] = fixtureOf(b, dir, &targets[i])
	}
	b.ReportAllocs()
	b.StopTimer()
	b.ResetTimer()
	for range b.N {
		for i := range targets {
			target := &targets[i]
			program := NewFixtureProgram(fixtures[i])
			program.BindSourceFiles()
			if op == "diagnostics" {
				diagFiles := storeFiles(b, program, target.DiagPaths)
				var messages []string
				measure(func() {
					c, _ := storechecker.NewChecker(program, nil)
					messages = storeMessages(storeCheckAll(c, diagFiles), nil)
				})
				if hashStrings(messages) != target.DiagHash {
					b.Fatalf("%s: the messages are not the manifest's", target.ID)
				}
				continue
			}
			p := newStorePrint(b, program, target)
			switch op {
			case "miss":
				measure(p.print)
			case "hit":
				p.print()
				measure(p.print)
			case "build":
				measure(p.build)
			}
			if op != "build" {
				checkOutputs(b, target, p.out)
			}
		}
	}
}

func BenchmarkPrint(b *testing.B) {
	for _, side := range []string{"pointer", "store"} {
		for _, set := range []printSet{setCompiler, setConfigurations} {
			for _, op := range printOps {
				b.Run(fmt.Sprintf("side=%s/set=%s/op=%s", side, set, op), func(b *testing.B) {
					if side == "pointer" {
						runPointerPrint(b, set, op, timerInterval(b))
					} else {
						runStorePrint(b, set, op, timerInterval(b))
					}
				})
			}
		}
	}
}

func BenchmarkPrintKPC(b *testing.B) {
	for _, side := range []string{"pointer", "store"} {
		for _, set := range []printSet{setCompiler, setConfigurations} {
			for _, op := range printOps {
				b.Run(fmt.Sprintf("side=%s/set=%s/op=%s", side, set, op), func(b *testing.B) {
					session := kperf.Start(b)
					defer session.Stop()
					if side == "pointer" {
						runPointerPrint(b, set, op, kpcInterval(b, session))
					} else {
						runStorePrint(b, set, op, kpcInterval(b, session))
					}
					session.Report()
				})
			}
		}
	}
}

// TestPrintOutputs is the outputs' identity (8): the Store's strings and
// messages of every target are the manifest's, which are the Pointer's.
func TestPrintOutputs(t *testing.T) {
	dir, m := readPrintManifest(t)
	for i := range m.Targets {
		target := &m.Targets[i]
		program := NewFixtureProgram(fixtureOf(t, dir, target))
		program.BindSourceFiles()
		p := newStorePrint(t, program, target)
		p.print()
		checkOutputs(t, target, p.out)
		p.print()
		checkOutputs(t, target, p.out)
		c, _ := storechecker.NewChecker(program, nil)
		if messages := storeMessages(storeCheckAll(c, storeFiles(t, program, target.DiagPaths)), nil); hashStrings(messages) != target.DiagHash {
			t.Fatalf("%s: the messages are not the manifest's", target.ID)
		}
	}
	t.Logf("%d targets", len(m.Targets))
}
