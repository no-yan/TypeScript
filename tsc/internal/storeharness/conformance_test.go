package storeharness

import (
	"cmp"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/microsoft/TypeScript/tsc/internal/ast"
	"github.com/microsoft/TypeScript/tsc/internal/ast/store"
	"github.com/microsoft/TypeScript/tsc/internal/bundled"
	"github.com/microsoft/TypeScript/tsc/internal/compiler"
	"github.com/microsoft/TypeScript/tsc/internal/core"
	"github.com/microsoft/TypeScript/tsc/internal/diagnostics"
	"github.com/microsoft/TypeScript/tsc/internal/diagnosticwriter"
	"github.com/microsoft/TypeScript/tsc/internal/execute/incremental"
	"github.com/microsoft/TypeScript/tsc/internal/parser"
	"github.com/microsoft/TypeScript/tsc/internal/repo"
	"github.com/microsoft/TypeScript/tsc/internal/storecompiler"
	"github.com/microsoft/TypeScript/tsc/internal/testutil"
	"github.com/microsoft/TypeScript/tsc/internal/testutil/baseline"
	"github.com/microsoft/TypeScript/tsc/internal/testutil/harnessutil"
	"github.com/microsoft/TypeScript/tsc/internal/testutil/tsbaseline"
	"github.com/microsoft/TypeScript/tsc/internal/tsoptions"
	"github.com/microsoft/TypeScript/tsc/internal/tspath"
	"github.com/microsoft/TypeScript/tsc/internal/vfs/vfstest"
)

// The semantic baselines of every configuration the testrunner runs
// (store-conformance-semantic-instructions-20261008.md): for each, the
// reference's file (R), the text the Pointer makes without emit and
// declaration diagnostics (P), and the text of the native Store program (S).
// The runner's unexported functions are written out here, each with the name
// of its original, from revision 0e32aa196a of testrunner/compiler_runner.go
// and testutil/harnessutil/harnessutil.go.

// The states of a configuration (2).
const (
	semanticCompared         = "compared"
	semanticRunnerSkipped    = "runner-skipped"
	semanticRunnerRejected   = "runner-rejected"
	semanticInputError       = "input-error"
	semanticStoreUnsupported = "store-unsupported"
	semanticHarnessError     = "harness-error"
	semanticNotSelected      = "not-selected"
)

// The verdicts of a baseline (1).
const (
	verdictEqual              = "equal"
	verdictEqualPointerNoEmit = "equal-pointer-noemit"
	verdictStoreMismatch      = "store-mismatch"
)

// The semantic baselines: their names and their files' extensions
// (compiler_runner.go's verify functions).
const (
	kindErrors = iota
	kindTypes
	kindSymbols
	kindModuleResolution
	kindCount
)

var semanticKinds = [kindCount]struct{ name, extension string }{
	{"errors", ".errors.txt"},
	{"types", ".types"},
	{"symbols", ".symbols"},
	{"module resolution", ".trace.json"},
}

// semanticRecord is a line of the manifest: every configuration gets one,
// with its state.
type semanticRecord struct {
	Index int    `json:"index"`
	ID    string `json:"id"`
	Suite string `json:"suite"`
	File  string `json:"file"`
	// Name is the runner's test name (runTest): the file's base name, and the
	// configuration's name after a space.
	Name   string `json:"name"`
	State  string `json:"state"`
	Reason string `json:"reason,omitempty"`
	// SuppressOutputPathCheck: S has it set (4).
	SuppressOutputPathCheck bool `json:"suppressOutputPathCheck,omitempty"`
	// StorePanic is the head of the stack of a Store panic.
	StorePanic string              `json:"storePanic,omitempty"`
	Baselines  []*semanticBaseline `json:"baselines,omitempty"`
	ElapsedMs  int64               `json:"elapsedMs"`
	texts      []semanticText
}

// semanticBaseline is a baseline of a compared configuration.
type semanticBaseline struct {
	Kind    string `json:"kind"`
	File    string `json:"file"` // in the suite's folder of the reference
	Applies bool   `json:"applies"`
	Verdict string `json:"verdict,omitempty"`
	// Reason: why S is not compared as a text (store-mismatch).
	Reason string `json:"reason,omitempty"`
	R      string `json:"r,omitempty"`
	P      string `json:"p,omitempty"`
	S      string `json:"s,omitempty"`
	// PointerDiff: P against R, where they differ.
	PointerDiff *lineDiff `json:"pointerDiff,omitempty"`
	// StoreDiff: S against what it is held to (R where P = R, P otherwise),
	// in a store-mismatch.
	StoreDiff *lineDiff `json:"storeDiff,omitempty"`
}

// lineDiff is the first line where two texts differ: its number from 1, and
// the line of each ("(none)" past the end of a text).
type lineDiff struct {
	Line     int    `json:"line"`
	Expected string `json:"expected"`
	Actual   string `json:"actual"`
}

// TestStoreConformanceBaselines makes the three texts of each semantic
// baseline of each configuration and gives each a verdict (1). It fails on a
// harness error only: the verdicts are reported.
//
// STORE_CONFORMANCE_OUT: the manifest (JSONL); the texts of the baselines
// where P is not R or S is a store-mismatch go to <suite>/<file>.P and .S
// beside it. STORE_CONFORMANCE_FILTER: a regexp of the ids to run (the others
// are recorded as not selected). STORE_CONFORMANCE_WORKERS: parallel
// configurations (default 4).
func TestStoreConformanceBaselines(t *testing.T) {
	if testing.Short() {
		t.Skip("the whole corpus")
	}
	cases, inputErrors, files, err := enumerateConfigurations(runnerSuites)
	if err != nil {
		t.Fatal(err)
	}
	var filter *regexp.Regexp
	if f := os.Getenv("STORE_CONFORMANCE_FILTER"); f != "" {
		filter = regexp.MustCompile(f)
	}
	textDir := ""
	var out *os.File
	if path := os.Getenv("STORE_CONFORMANCE_OUT"); path != "" {
		if out, err = os.Create(path); err != nil {
			t.Fatal(err)
		}
		defer out.Close()
		textDir = filepath.Dir(path)
	}
	workers := envInt(t, "STORE_CONFORMANCE_WORKERS", 4)

	records := make([]*semanticRecord, len(cases))
	var wg sync.WaitGroup
	next := make(chan int)
	for range workers {
		wg.Go(func() {
			for i := range next {
				records[i] = runSemanticCase(cases[i], filter)
				if textDir != "" {
					if err := writeSemanticTexts(textDir, records[i]); err != nil {
						records[i].State, records[i].Reason = semanticHarnessError, err.Error()
					}
				}
				records[i].texts = nil
			}
		})
	}
	for i := range cases {
		next <- i
	}
	close(next)
	wg.Wait()
	for _, e := range inputErrors {
		records = append(records, &semanticRecord{Index: -1, Suite: e.Suite, File: e.File, State: semanticInputError, Reason: e.Reason})
	}

	states := map[string]int{}
	verdicts := map[string]int{} // suite, kind, verdict
	unsupported := map[string]int{}
	for _, r := range records {
		states[r.State]++
		switch r.State {
		case semanticHarnessError:
			t.Errorf("%s: %s", r.ID, r.Reason)
		case semanticStoreUnsupported:
			unsupported[r.Reason]++
		}
		for _, b := range r.Baselines {
			if b.Applies {
				verdicts[r.Suite+" "+b.Kind+" "+b.Verdict]++
			}
		}
		if out != nil {
			line, _ := json.Marshal(r)
			fmt.Fprintln(out, string(line))
		}
	}
	t.Logf("%d test files, %d configurations, %d input errors: %v", files, len(cases), len(inputErrors), states)
	for _, k := range slices.Sorted(maps.Keys(verdicts)) {
		t.Logf("%s: %d", k, verdicts[k])
	}
	for _, k := range slices.Sorted(maps.Keys(unsupported)) {
		t.Logf("store-unsupported %s: %d", k, unsupported[k])
	}
	if out != nil {
		line, _ := json.Marshal(map[string]any{
			"summary": states, "verdicts": verdicts, "storeUnsupported": unsupported,
			"files": files, "configurations": len(cases), "inputErrors": len(inputErrors),
			"singleThreaded": testutil.TestProgramIsSingleThreaded(), "go": runtime.Version(),
		})
		fmt.Fprintln(out, string(line))
	}
}

// runSemanticCase is a configuration's record: the runner's skip and
// rejection (runSingleConfigTest), the Store's unsupported configurations,
// and the three texts of each baseline.
func runSemanticCase(c *configCase, filter *regexp.Regexp) (r *semanticRecord) {
	r = &semanticRecord{Index: c.Index, ID: c.ID, Suite: c.Suite, File: c.File, Name: runnerTestName(c)}
	start := time.Now()
	defer func() {
		if v := recover(); v != nil {
			info := recovered(v)
			r.State, r.Reason, r.Baselines, r.texts = semanticHarnessError, "panic: "+info.message+"\n"+info.stack, nil, nil
		}
		r.ElapsedMs = time.Since(start).Milliseconds()
	}()
	if filter != nil && !filter.MatchString(c.ID) {
		r.State = semanticNotSelected
		return r
	}
	if c.Skipped {
		r.State, r.Reason = semanticRunnerSkipped, "skippedTests (compiler_runner.go)"
		return r
	}
	d, o := prepareCase(c)
	if d == nil {
		r.State, r.Reason = outcomeState(o), o.String()
		return r
	}
	if o := callWithT(func(t *testing.T) { harnessutil.SkipUnsupportedCompilerOptions(t, d.options) }); o.Kind != "" {
		r.State, r.Reason = outcomeState(o), o.String()
		return r
	}
	if reasons := storeUnsupported(d); len(reasons) > 0 {
		r.State, r.Reason = semanticStoreUnsupported, strings.Join(reasons, ", ")
		return r
	}

	header := tspath.GetPathFromPathComponents(tspath.GetPathComponentsRelativeTo(repo.TestDataPath(), c.path, tspath.ComparePathsOptions{}))
	p, applies, problem := pointerSemantic(d, header)
	if problem != "" {
		r.State, r.Reason = semanticHarnessError, "P: "+problem
		return r
	}
	s := storeSemantic(d, applies, header)
	r.State, r.SuppressOutputPathCheck, r.StorePanic = semanticCompared, s.suppressed, s.panicStack

	configuredName := runnerConfiguredName(c)
	for k, kind := range semanticKinds {
		b := &semanticBaseline{Kind: kind.name, File: tsExtension.ReplaceAllString(configuredName, kind.extension), Applies: applies[k]}
		r.Baselines = append(r.Baselines, b)
		if !b.Applies {
			continue
		}
		reference := baseline.NoContent
		if content, err := os.ReadFile(filepath.Join(repo.TestDataPath(), "baselines", "reference", c.Suite, b.File)); err == nil {
			reference = string(content)
		}
		pText, sText := p.texts[k], s.texts[k]
		b.R, b.P, b.S = textHash(reference), textHash(pText), textHash(sText)
		b.PointerDiff = firstLineDiff(reference, pText)
		switch {
		case s.failed[k] != "":
			b.Verdict, b.Reason = verdictStoreMismatch, s.failed[k]
		case pText == reference && sText == reference:
			b.Verdict = verdictEqual
		case pText != reference && sText == pText:
			b.Verdict = verdictEqualPointerNoEmit
		default:
			b.Verdict = verdictStoreMismatch
		}
		if b.Verdict == verdictStoreMismatch {
			// What S is held to: R where P = R, P otherwise.
			b.StoreDiff = firstLineDiff(pText, sText)
		}
		if b.PointerDiff != nil || b.Verdict == verdictStoreMismatch {
			r.texts = append(r.texts, semanticText{b.File, pText, sText})
		}
	}
	return r
}

// outcomeState is the state of a preparation that ended: a t.Fatal of the
// runner's preparation is its rejection, a t.Skip its skip, and a panic the
// harness's fault.
func outcomeState(o tOutcome) string {
	switch o.Kind {
	case "fatal":
		return semanticRunnerRejected
	case "skip":
		return semanticRunnerSkipped
	}
	return semanticHarnessError
}

// runnerTestName is runTest's testName (compiler_runner.go).
func runnerTestName(c *configCase) string {
	name := tspath.GetBaseFileName(c.path)
	if c.Name != "" {
		name += " " + c.Name
	}
	return name
}

// runnerConfiguredName is newCompilerTest's configuredName
// (compiler_runner.go).
func runnerConfiguredName(c *configCase) string {
	basename := tspath.GetBaseFileName(c.path)
	if c.Name == "" {
		return basename
	}
	extname := tspath.GetAnyExtensionFromPath(basename, nil, false)
	return fmt.Sprintf("%s(%s)%s", basename[:len(basename)-len(extname)], c.Name, extname)
}

// storeUnsupported are the reasons the Store program does not take a
// configuration (2), from its options and config alone.
func storeUnsupported(d *configDescriptor) []string {
	config := d.commandLine(d.options)
	var reasons []string
	if len(config.ContentMappers()) != 0 {
		reasons = append(reasons, "content mapper")
	}
	if d.harnessOptions.CaptureSuggestions {
		reasons = append(reasons, "captureSuggestions")
	}
	if len(config.ProjectReferences()) != 0 {
		reasons = append(reasons, "project references")
	}
	return reasons
}

func textHash(text string) string {
	if text == baseline.NoContent {
		return text
	}
	h := sha256.Sum256([]byte(text))
	return hex.EncodeToString(h[:])
}

func firstLineDiff(expected, actual string) *lineDiff {
	if expected == actual {
		return nil
	}
	e, a := strings.Split(expected, "\n"), strings.Split(actual, "\n")
	line := func(lines []string, i int) string {
		if i < len(lines) {
			return lines[i]
		}
		return "(none)"
	}
	i := 0
	for i < len(e) && i < len(a) && e[i] == a[i] {
		i++
	}
	return &lineDiff{Line: i + 1, Expected: line(e, i), Actual: line(a, i)}
}

// semanticText is the P and S of a baseline where P is not R or S is a
// store-mismatch, for writeSemanticTexts.
type semanticText struct {
	file string
	p, s string
}

func writeSemanticTexts(dir string, r *semanticRecord) error {
	for _, text := range r.texts {
		base := filepath.Join(dir, r.Suite, text.file)
		if err := os.MkdirAll(filepath.Dir(base), 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(base+".P", []byte(text.p), 0o644); err != nil {
			return err
		}
		if err := os.WriteFile(base+".S", []byte(text.s), 0o644); err != nil {
			return err
		}
	}
	return nil
}

// semanticSingleThreaded is createProgram's SingleThreaded (harnessutil.go).
func semanticSingleThreaded() core.Tristate {
	if testutil.TestProgramIsSingleThreaded() {
		return core.TSTrue
	}
	return core.TSUnknown
}

// preCommandLine is compileFilesWithHost's preConfig (harnessutil.go): the
// config without TraceResolution.
func preCommandLine(config *tsoptions.ParsedCommandLine) *tsoptions.ParsedCommandLine {
	preCompilerOptions := config.CompilerOptions().Clone()
	preCompilerOptions.TraceResolution = core.TSFalse
	return &tsoptions.ParsedCommandLine{
		ParsedConfig: &tsoptions.ParsedOptions{
			CompilerOptions: preCompilerOptions,
			FileNames:       config.FileNames(),
			ContentMappers:  config.ContentMappers(),
		},
		ConfigFile: config.ConfigFile,
		Errors:     config.Errors,
	}
}

// semanticSide is the texts of one side, by kind. failed is why a text of S
// is not compared as a text: the pre and post programs' counts, the error
// baseline's own check, a diagnostic of code -1, a panic.
type semanticSide struct {
	texts      [kindCount]string
	failed     [kindCount]string
	suppressed bool
	panicStack string
}

// errorBaselineFiles are verifyDiagnostics's files (compiler_runner.go):
// tsConfigFiles, toBeCompiled and otherFiles.
func errorBaselineFiles(d *configDescriptor) []*harnessutil.TestFile {
	var files []*harnessutil.TestFile
	if d.tsConfigFile != nil {
		files = append(files, d.tsConfigFile)
	}
	return core.Concatenate(files, core.Concatenate(d.toBeCompiled, d.otherFiles))
}

// errorBaselineText is DoErrorBaseline's text (tsbaseline): GetErrorBaseline
// with a T of its own, whose failure GetErrorBaseline records without ending
// it (assert.Check): problem is that failure, or how the call ended.
func errorBaselineText[T diagnosticwriter.Diagnostic](files []*harnessutil.TestFile, ds []T, compare func(a, b T) int, pretty bool) (text string, problem string) {
	if len(ds) == 0 {
		return baseline.NoContent, ""
	}
	failed := false
	if o := callWithT(func(t *testing.T) {
		text = tsbaseline.GetErrorBaseline(t, files, ds, compare, pretty)
		failed = t.Failed()
	}); o.Kind != "" {
		return "", o.String()
	}
	if failed {
		return text, "error-baseline-count"
	}
	return text, ""
}

// pointerSemantic is P (3): compileFilesWithHost (harnessutil.go) without
// postProgram.Emit and GetDeclarationDiagnostics, and the runner's
// verifyDiagnostics, verifyTypesAndSymbols and verifyModuleResolution
// (compiler_runner.go) on its result. applies are the baselines the runner
// makes for the configuration. problem is a Pointer panic or a failure of its
// error baseline.
func pointerSemantic(d *configDescriptor, header string) (side semanticSide, applies [kindCount]bool, problem string) {
	defer func() {
		if v := recover(); v != nil {
			info := recovered(v)
			problem = "panic: " + info.message + "\n" + info.stack
		}
	}()
	ctx := context.Background()

	// CompileFilesEx's file system and createCompilerHost.
	fs := vfstest.FromMap(d.fileMap(), d.harnessOptions.UseCaseSensitiveFileNames)
	fs = bundled.WrapFS(fs)
	fs = harnessutil.NewOutputRecorderFS(fs)
	config := d.commandLine(d.options.Clone())
	var trace strings.Builder
	tracer := harnessutil.NewTracerForBaselining(tspath.ComparePathsOptions{
		UseCaseSensitiveFileNames: fs.UseCaseSensitiveFileNames(),
		CurrentDirectory:          d.currentDirectory,
	}, &trace)
	host := &pointerSemanticHost{
		CompilerHost: compiler.NewCompilerHost(d.currentDirectory, fs, bundled.LibPath(), nil, tracer.Trace, nil),
		files:        map[harnessutil.SourceFileCacheKey]*ast.SourceFile{},
	}

	// compileFilesWithHost.
	collect := func(program compiler.ProgramLike) []*ast.Diagnostic {
		var errors []*ast.Diagnostic
		errors = append(errors, program.GetConfigFileParsingDiagnostics()...)
		errors = append(errors, program.GetProgramDiagnostics()...)
		errors = append(errors, program.GetSyntacticDiagnostics(ctx, nil)...)
		errors = append(errors, program.GetSemanticDiagnostics(ctx, nil)...)
		errors = append(errors, program.GetGlobalDiagnostics(ctx)...)
		if d.harnessOptions.CaptureSuggestions {
			errors = append(errors, program.GetSuggestionDiagnostics(ctx, nil)...)
		}
		return compiler.SortAndDeduplicateDiagnostics(errors)
	}
	preErrors := collect(pointerSemanticProgram(host, preCommandLine(config)))
	postProgram := pointerSemanticProgram(host, config)
	errors := pointerPrePostErrors(preErrors, collect(postProgram))
	// CompileFilesEx's result.Trace, read when the compilation returns.
	traceText := trace.String()
	options := postProgram.Options()

	applies = [kindCount]bool{true, !d.harnessOptions.NoTypesAndSymbols, !d.harnessOptions.NoTypesAndSymbols, options.TraceResolution.IsTrue()}

	// verifyDiagnostics and DoErrorBaseline.
	side.texts[kindErrors], problem = errorBaselineText(errorBaselineFiles(d), diagnosticwriter.WrapASTDiagnostics(errors), diagnosticwriter.CompareASTDiagnostics, options.Pretty.IsTrue())
	if problem != "" {
		return side, applies, "the error baseline: " + problem
	}
	if core.Some(errors, func(d *ast.Diagnostic) bool { return d.Code() == -1 }) {
		return side, applies, "a diagnostic of code -1"
	}

	// verifyTypesAndSymbols and DoTypeAndSymbolBaseline: the type walk of
	// every file, then the symbol walk.
	if applies[kindTypes] {
		var allFiles []*harnessutil.TestFile
		var sourceFiles []*ast.SourceFile
		for _, f := range core.Concatenate(d.toBeCompiled, d.otherFiles) {
			if sf := postProgram.GetSourceFile(f.UnitName); sf != nil {
				allFiles = append(allFiles, f)
				sourceFiles = append(sourceFiles, sf)
			}
		}
		hadErrorBaseline := len(errors) > 0
		walk := func(isSymbolWalk bool) [][]baselineResult {
			results := make([][]baselineResult, len(sourceFiles))
			for i, sf := range sourceFiles {
				// getTypeCheckerForCurrentFile.
				c, done := postProgram.Program().GetTypeCheckerForFile(ctx, sf)
				w := &pointerWalker{checker: c, hadErrors: hadErrorBaseline}
				results[i] = baselineResults(sf, w.walk(sf, isSymbolWalk))
				done()
			}
			return results
		}
		side.texts[kindTypes] = generateBaseline(allFiles, walk(false), header, false)
		side.texts[kindSymbols] = generateBaseline(allFiles, walk(true), header, true)
	}

	// verifyModuleResolution and DoModuleResolutionBaseline.
	if applies[kindModuleResolution] {
		side.texts[kindModuleResolution] = cmp.Or(traceText, baseline.NoContent)
	}
	return side, applies, ""
}

// pointerSemanticProgram is createProgram (harnessutil.go).
func pointerSemanticProgram(host compiler.CompilerHost, config *tsoptions.ParsedCommandLine) compiler.ProgramLike {
	program := compiler.NewProgram(compiler.ProgramOptions{
		Config:         config,
		Host:           host,
		SingleThreaded: semanticSingleThreaded(),
	})
	if config.CompilerOptions().Incremental.IsTrue() {
		oldProgram := incremental.ReadBuildInfoProgram(config, &semanticBuildInfoReader{inner: incremental.NewBuildInfoReader(host)}, host)
		return incremental.NewProgram(program, oldProgram, incremental.CreateHost(host), nil, false)
	}
	return program
}

// semanticBuildInfoReader is harnessutil.go's testBuildInfoReader.
type semanticBuildInfoReader struct {
	inner incremental.BuildInfoReader
}

func (r *semanticBuildInfoReader) ReadBuildInfo(config *tsoptions.ParsedCommandLine) *incremental.BuildInfo {
	buildInfo := r.inner.ReadBuildInfo(config)
	if buildInfo == nil {
		return nil
	}
	buildInfo.Version = core.Version()
	return buildInfo
}

// pointerSemanticHost is harnessutil.go's cachedCompilerHost, its cache the
// case's: the pre and post programs share their source files, as the
// runner's do. The bundled libs are cachedLib's, for every case.
type pointerSemanticHost struct {
	compiler.CompilerHost
	mu    sync.Mutex
	files map[harnessutil.SourceFileCacheKey]*ast.SourceFile
}

func (h *pointerSemanticHost) GetSourceFile(opts ast.SourceFileParseOptions) *ast.SourceFile {
	text, ok := h.FS().ReadFile(opts.FileName)
	if !ok {
		return nil
	}
	scriptKind := core.GetScriptKindFromFileName(opts.FileName)
	if scriptKind == core.ScriptKindUnknown {
		panic("Unknown script kind for file  " + opts.FileName)
	}
	if isBundledLib(opts.FileName) {
		return cachedLib(opts, text, scriptKind)
	}
	key := harnessutil.GetSourceFileCacheKey(opts, text, scriptKind)
	h.mu.Lock()
	defer h.mu.Unlock()
	if cached, ok := h.files[key]; ok {
		return cached
	}
	sourceFile := parser.ParseSourceFile(opts, text, scriptKind)
	h.files[key] = sourceFile
	return sourceFile
}

// pointerPrePostErrors is compileFilesWithHost's errors: the post program's,
// or, where the counts differ, the shorter list and a diagnostic of the
// excess.
func pointerPrePostErrors(preErrors, postErrors []*ast.Diagnostic) []*ast.Diagnostic {
	errors := postErrors
	if len(postErrors) != len(preErrors) {
		longerErrors := postErrors
		shorterErrors := preErrors
		if len(preErrors) > len(postErrors) {
			longerErrors, shorterErrors = preErrors, postErrors
		}
		diag := ast.NewCompilerDiagnostic(
			diagnostics.NewAdHocMessage(fmt.Sprintf("Pre-emit (%d) and post-emit (%d) diagnostic counts do not match! This can indicate that a semantic _error_ was added by the emit resolver - such an error may not be reflected on the command line or in the editor, but may be captured in a baseline here!", len(preErrors), len(postErrors))),
		)
		diag = diag.AddRelatedInfo(ast.NewCompilerDiagnostic(diagnostics.NewAdHocMessage("The excess diagnostics are:")))
		for _, d := range longerErrors {
			matched := false
			for _, d2 := range shorterErrors {
				comparison := ast.CompareDiagnostics(d, d2)
				if comparison == 0 {
					matched = true
					break
				}
			}
			if !matched {
				diag = diag.AddRelatedInfo(d)
			}
		}
		errors = shorterErrors
		errors = append(errors, diag)
	}
	return errors
}

// storeSemantic is S (4): the native Store program of the descriptor's input
// values, its pre and post programs and the texts P makes, of the baselines
// that apply. A panic fails the texts not made yet.
func storeSemantic(d *configDescriptor, applies [kindCount]bool, header string) (side semanticSide) {
	var made [kindCount]bool
	defer func() {
		if v := recover(); v != nil {
			info := recovered(v)
			for k := range kindCount {
				if applies[k] && !made[k] {
					side.failed[k] = "panic: " + strings.SplitN(info.message, "\n", 2)[0]
				}
			}
			side.panicStack = stackHead(info.stack, 12)
		}
	}()
	ctx := context.Background()

	fs := bundled.WrapFS(vfstest.FromMap(d.FileMap(), d.UseCaseSensitiveFileNames()))
	options := d.Options()
	if !options.NoEmit.IsTrue() {
		options.SuppressOutputPathCheck = core.TSTrue
		side.suppressed = true
	}
	config := d.CommandLine(options)
	var trace strings.Builder
	tracer := harnessutil.NewTracerForBaselining(tspath.ComparePathsOptions{
		UseCaseSensitiveFileNames: fs.UseCaseSensitiveFileNames(),
		CurrentDirectory:          d.CurrentDirectory(),
	}, &trace)
	// One host for both programs, as P's: the Store host keeps no file.
	host := storecompiler.NewCompilerHost(d.CurrentDirectory(), fs, bundled.LibPath(), nil, tracer.Trace)
	program := func(config *tsoptions.ParsedCommandLine) *storecompiler.Program {
		return storecompiler.NewProgram(storecompiler.ProgramOptions{Config: config, Host: host, SingleThreaded: semanticSingleThreaded()})
	}
	collect := func(program *storecompiler.Program) []*store.Diagnostic {
		var errors []*store.Diagnostic
		errors = append(errors, program.GetConfigFileParsingDiagnostics()...)
		errors = append(errors, program.GetProgramDiagnostics()...)
		errors = append(errors, program.GetSyntacticDiagnostics(ctx, nil)...)
		errors = append(errors, program.GetSemanticDiagnostics(ctx, nil)...)
		errors = append(errors, program.GetGlobalDiagnostics(ctx)...)
		return storecompiler.SortAndDeduplicateDiagnostics(errors)
	}
	preErrors := collect(program(preCommandLine(config)))
	postProgram := program(config)
	errors := collect(postProgram)
	traceText := trace.String()
	countsDiffer := len(preErrors) != len(errors)

	var problems []string
	if countsDiffer {
		problems = append(problems, "pre-post-count")
	}
	text, problem := errorBaselineText(errorBaselineFiles(d), storecompiler.WrapDiagnostics(errors), storecompiler.CompareWrappedDiagnostics, postProgram.Options().Pretty.IsTrue())
	if problem != "" {
		problems = append(problems, problem)
	}
	if core.Some(errors, func(d *store.Diagnostic) bool { return d.Code() == -1 }) {
		problems = append(problems, "code -1")
	}
	side.texts[kindErrors], side.failed[kindErrors] = text, strings.Join(problems, ", ")
	made[kindErrors] = true

	if applies[kindTypes] {
		var allFiles []*harnessutil.TestFile
		var sourceFiles []*store.File
		for _, f := range core.Concatenate(d.toBeCompiled, d.otherFiles) {
			if sf := postProgram.GetSourceFile(f.UnitName); sf != nil {
				allFiles = append(allFiles, f)
				sourceFiles = append(sourceFiles, sf)
			}
		}
		// The runner's errors hold a diagnostic of the counts where they
		// differ.
		hadErrorBaseline := len(errors) > 0 || countsDiffer
		walk := func(isSymbolWalk bool) [][]baselineResult {
			results := make([][]baselineResult, len(sourceFiles))
			for i, sf := range sourceFiles {
				c, done := postProgram.GetTypeCheckerForFileExclusive(ctx, sf)
				w := newStoreWalker(c, postProgram.IDSpace(), hadErrorBaseline)
				results[i] = baselineResults(sf, w.walk(sf, isSymbolWalk))
				done()
			}
			return results
		}
		side.texts[kindTypes] = generateBaseline(allFiles, walk(false), header, false)
		made[kindTypes] = true
		side.texts[kindSymbols] = generateBaseline(allFiles, walk(true), header, true)
		made[kindSymbols] = true
	}

	if applies[kindModuleResolution] {
		side.texts[kindModuleResolution] = cmp.Or(traceText, baseline.NoContent)
		made[kindModuleResolution] = true
	}
	return side
}

func stackHead(stack string, lines int) string {
	parts := strings.SplitN(stack, "\n", lines+1)
	return strings.Join(parts[:min(len(parts), lines)], "\n")
}
