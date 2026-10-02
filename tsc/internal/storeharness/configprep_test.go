package storeharness

// Stage 2 (store-checker-7d2b3-implementation-instructions-20261001.md): every
// configuration of every compiler and conformance test, as the program the
// testrunner builds for it. The enumeration and the preparation are
// testrunner's and harnessutil's: the exported functions are called, and the
// unexported ones are written out here, each with the name of its original,
// from revision 0e32aa196a of testrunner/compiler_runner.go and
// testutil/harnessutil/harnessutil.go and 5f647a841a of
// testrunner/test_case_parser.go.

import (
	"cmp"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io/fs"
	"maps"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"slices"
	"strings"
	"sync"
	"testing"
	"testing/fstest"

	"github.com/microsoft/TypeScript/tsc/internal/ast"
	"github.com/microsoft/TypeScript/tsc/internal/bundled"
	"github.com/microsoft/TypeScript/tsc/internal/compiler"
	"github.com/microsoft/TypeScript/tsc/internal/contentmapper"
	"github.com/microsoft/TypeScript/tsc/internal/core"
	"github.com/microsoft/TypeScript/tsc/internal/locale"
	"github.com/microsoft/TypeScript/tsc/internal/parser"
	"github.com/microsoft/TypeScript/tsc/internal/repo"
	"github.com/microsoft/TypeScript/tsc/internal/testrunner"
	"github.com/microsoft/TypeScript/tsc/internal/testutil/contentmappertest"
	"github.com/microsoft/TypeScript/tsc/internal/testutil/harnessutil"
	"github.com/microsoft/TypeScript/tsc/internal/tsoptions"
	"github.com/microsoft/TypeScript/tsc/internal/tsoptions/tsoptionstest"
	"github.com/microsoft/TypeScript/tsc/internal/tspath"
	"github.com/microsoft/TypeScript/tsc/internal/vfs/osvfs"
	"github.com/microsoft/TypeScript/tsc/internal/vfs/vfstest"
)

// configSuite is a directory of tests: a runner's.
type configSuite struct {
	name string
	dir  string // as harnessutil.EnumerateFiles takes it: under testdata, or absolute
}

// runnerSuites are testrunner's compiler runners in TestLocal's order
// (compiler_runner_test.go; NewCompilerBaselineRunner's basePath).
var runnerSuites = []configSuite{
	{"compiler", "tests/cases/compiler"},
	{"conformance", "tests/cases/conformance"},
}

// compilerTestFile is compiler_runner.go's compilerBaselineRegex.
var compilerTestFile = regexp.MustCompile(`\.tsx?$`)

// runnerSkippedTests is compiler_runner.go's skippedTests: RunTests skips
// these files by base name before it reads them.
var runnerSkippedTests = []string{
	// Tests that depended on typescript.d.ts in built.
	"APILibCheck.ts",
	"APISample_Watch.ts",
	"APISample_WatchWithDefaults.ts",
	"APISample_WatchWithOwnWatchHost.ts",
	"APISample_compile.ts",
	"APISample_jsdoc.ts",
	"APISample_linter.ts",
	"APISample_parseConfig.ts",
	"APISample_transform.ts",
	"APISample_watcher.ts",

	// These tests contain options that have been completely removed, so fail to parse.
	"preserveUnusedImports.ts",
	"noCrashWithVerbatimModuleSyntaxAndImportsNotUsedAsValues.ts",
	"verbatimModuleSyntaxCompat.ts",
	"verbatimModuleSyntaxCompat2.ts",
	"verbatimModuleSyntaxCompat3.ts",
	"verbatimModuleSyntaxCompat4.ts",
	"preserveValueImports.ts",
	"preserveValueImports_importsNotUsedAsValues.ts",
	"preserveValueImports_errors.ts",
	"preserveValueImports_mixedImports.ts",
	"preserveValueImports_module.ts",
	"importsNotUsedAsValues_error.ts",
	"alwaysStrictNoImplicitUseStrict.ts",
	"nonPrimitiveIndexingWithForInSupressError.ts",
	"parameterInitializerBeforeDestructuringEmit.ts",
	"mappedTypeUnionConstraintInferences.ts",
	"lateBoundConstraintTypeChecksCorrectly.ts",
	"keyofDoesntContainSymbols.ts",
	"noStrictGenericChecks.ts",
	"noImplicitUseStrict_umd.ts",
	"noImplicitUseStrict_system.ts",
	"noImplicitUseStrict_es6.ts",
	"noImplicitUseStrict_commonjs.ts",
	"noImplicitAnyIndexingSuppressed.ts",
	"excessPropertyErrorsSuppressed.ts",
	"moduleNoneDynamicImport.ts",
	"moduleNoneErrors.ts",
	"noErrorUsingImportExportModuleAugmentationInDeclarationFile1.ts",
	"noErrorUsingImportExportModuleAugmentationInDeclarationFile2.ts",
	"noErrorUsingImportExportModuleAugmentationInDeclarationFile3.ts",
	"requireOfJsonFileWithModuleEmitNone.ts",
	"requireOfJsonFileWithModuleNodeResolutionEmitNone.ts",
}

// compilerVaryBy is compiler_runner.go's compilerVaryBy (getCompilerVaryByMap).
var compilerVaryBy = sync.OnceValue(func() map[string]struct{} {
	varyByOptions := append(
		core.Map(core.Filter(tsoptions.OptionsDeclarations, func(option *tsoptions.CommandLineOption) bool {
			return !option.IsCommandLineOnly &&
				(option.Kind == tsoptions.CommandLineOptionTypeBoolean || option.Kind == tsoptions.CommandLineOptionTypeEnum) &&
				(option.AffectsProgramStructure ||
					option.AffectsEmit ||
					option.AffectsModuleResolution ||
					option.AffectsBindDiagnostics ||
					option.AffectsSemanticDiagnostics ||
					option.AffectsSourceFile ||
					option.AffectsDeclarationPath ||
					option.AffectsBuildInfo)
		}), func(option *tsoptions.CommandLineOption) string {
			return option.Name
		}),
		// explicit variations that do not match above conditions
		"noEmit",
		"isolatedModules",
	)
	varyByMap := make(map[string]struct{})
	for _, option := range varyByOptions {
		varyByMap[strings.ToLower(option)] = struct{}{}
	}
	return varyByMap
})

// testOptionRegex is test_case_parser.go's optionRegex.
var testOptionRegex = regexp.MustCompile(`(?m)^\/{2}\s*@(\w+)\s*:\s*([^\r\n]*)`)

// extractCompilerSettings is test_case_parser.go's.
func extractCompilerSettings(content string) map[string]string {
	opts := make(map[string]string)
	for _, match := range testOptionRegex.FindAllStringSubmatch(content, -1) {
		opts[strings.ToLower(match[1])] = strings.TrimSuffix(strings.TrimSpace(match[2]), ";")
	}
	return opts
}

// compiler_runner.go's requireStr, referencesRegex and srcFolder, and
// harnessutil.go's testLibFolder.
const (
	requireStr    = "require("
	srcFolder     = "/.src"
	testLibFolder = "/.lib"
)

var referencesRegex = regexp.MustCompile(`reference\spath`)

// configCase is a configuration of a test, the unit of stage 2. Its index is
// its place in the list of every configuration of every suite (the suites'
// order, the runner's order of the files, then the ids'), and its id is the
// suite, the file and the values of the options the configurations of the
// file vary by, with "#n" for the n-th of an id that remains the same.
type configCase struct {
	Index       int
	ID          string
	Suite       string
	File        string // under the suite's directory
	ContentHash string
	Config      map[string]string // nil: the test sets no option
	Skipped     bool              // runnerSkippedTests names the file
	path        string            // as the runner reads it
	content     string
}

// inputError is a test whose configurations are not enumerated: the
// runner's GetFileBasedTestConfigurations ends the test (it is given the
// runner's top test, RunTests's) or panics.
type inputError struct {
	Suite       string
	File        string
	ContentHash string
	Skipped     bool // the runner skips the file before it reads it
	Reason      string
}

func contentHash(s string) string {
	h := sha256.Sum256([]byte(s))
	return hex.EncodeToString(h[:])
}

// enumerateConfigurations is RunTests's and getCompilerFileBasedTest's
// enumeration (compiler_runner.go) of the suites, every file included: a file
// of runnerSkippedTests is enumerated too, and its cases are skipped.
func enumerateConfigurations(suites []configSuite) (cases []*configCase, inputErrors []*inputError, files int, err error) {
	for _, suite := range suites {
		paths, err := harnessutil.EnumerateFiles(suite.dir, compilerTestFile, true /*recursive*/)
		if err != nil {
			return nil, nil, 0, fmt.Errorf("%s: %w", suite.name, err)
		}
		base := tspath.EnsureTrailingDirectorySeparator(tspath.GetNormalizedAbsolutePath(suite.dir, repo.TestDataPath()))
		for _, path := range paths {
			files++
			file := strings.TrimPrefix(path, base)
			// getCompilerFileBasedTest: osvfs reads the file (a BOM is decoded).
			content, ok := osvfs.FS().ReadFile(path)
			if !ok {
				return nil, nil, 0, fmt.Errorf("cannot read %s", path)
			}
			skipped := slices.Contains(runnerSkippedTests, tspath.GetBaseFileName(path))
			settings := extractCompilerSettings(content)
			var configurations []*harnessutil.NamedTestConfiguration
			if o := callWithT(func(t *testing.T) {
				configurations = harnessutil.GetFileBasedTestConfigurations(t, settings, compilerVaryBy())
			}); o.Kind != "" {
				inputErrors = append(inputErrors, &inputError{suite.name, file, contentHash(content), skipped, o.String()})
				continue
			}
			cases = append(cases, fileCases(suite.name, file, path, content, skipped, configurations)...)
		}
	}
	for i, c := range cases {
		c.Index = i
	}
	return cases, inputErrors, files, nil
}

// fileCases are the cases of a file's configurations (none: one case with
// no configuration, as runTest), in the order of their ids.
func fileCases(suite, file, path, content string, skipped bool, configurations []*harnessutil.NamedTestConfiguration) []*configCase {
	hash := contentHash(content)
	prefix := suite + "/" + file
	if len(configurations) == 0 {
		return []*configCase{{ID: prefix, Suite: suite, File: file, ContentHash: hash, Skipped: skipped, path: path, content: content}}
	}
	// The options the configurations vary by: those with more than one value
	// among them. A configuration's values come from the test's text
	// (splitOptionValues), and are compared as GetFileBasedTestConfigurations
	// names them, in lower case.
	values := map[string]map[string]bool{}
	for _, configuration := range configurations {
		for k, v := range configuration.Config {
			if values[k] == nil {
				values[k] = map[string]bool{}
			}
			values[k][strings.ToLower(v)] = true
		}
	}
	var varying []string
	for k, vs := range values {
		if len(vs) > 1 {
			varying = append(varying, k)
		}
	}
	slices.Sort(varying)
	cases := make([]*configCase, len(configurations))
	for i, configuration := range configurations {
		id := prefix
		if len(varying) > 0 {
			parts := make([]string, len(varying))
			for j, k := range varying {
				parts[j] = k + "=" + strings.ToLower(configuration.Config[k])
			}
			id += "(" + strings.Join(parts, ",") + ")"
		}
		cases[i] = &configCase{ID: id, Suite: suite, File: file, ContentHash: hash, Config: maps.Clone(configuration.Config), Skipped: skipped, path: path, content: content}
	}
	// GetFileBasedTestConfigurations's order follows a map's: the ids order
	// the cases, and the whole configuration orders those of the same id.
	slices.SortFunc(cases, func(a, b *configCase) int {
		return cmp.Or(strings.Compare(a.ID, b.ID), strings.Compare(configKey(a.Config), configKey(b.Config)))
	})
	seen := map[string]int{}
	for _, c := range cases {
		seen[c.ID]++
		if n := seen[c.ID]; n > 1 {
			c.ID += fmt.Sprintf("#%d", n)
		}
	}
	return cases
}

func configKey(config map[string]string) string {
	b, _ := json.Marshal(config) // keys sorted
	return string(b)
}

// tOutcome is how a call of callWithT ended: Kind is "" when it returned, or
// "fatal", "skip" or "panic"; Where is the function that called testing's
// FailNow or SkipNow (through Fatal, Skip and their kin), or that panicked,
// with its file and line; Panic is the panic's value.
type tOutcome struct {
	Kind  string
	Where string
	Panic string
}

func (o tOutcome) String() string {
	if o.Kind == "" {
		return ""
	}
	s := "t." + o.Kind + " in " + o.Where
	if o.Kind == "panic" {
		s = "panic in " + o.Where + ": " + o.Panic
	}
	return s
}

// callWithT runs f with a testing.T of its own, on a goroutine of its own,
// so that a t.Fatal, t.Skip or panic of f ends f and not the caller's test:
// the runner's exported preparation takes a *testing.T and ends the runner's
// test with it (GetFileBasedTestConfigurations, SetOptionsFromTestConfig,
// SkipUnsupportedCompilerOptions). The T has no output, and testing drops
// what it logs (outputWriter.write without a writer): the message is lost,
// and where the call ended is kept.
func callWithT(f func(t *testing.T)) (o tOutcome) {
	done := make(chan struct{})
	go func() {
		defer close(done)
		returned := false
		defer func() {
			v := recover()
			if !returned {
				o = endedAt(v)
			}
		}()
		f(new(testing.T))
		returned = true
	}()
	<-done
	return o
}

// endedAt reads, in a deferred call, the frames below it: those of the
// panic, or of testing's FailNow or SkipNow and its callers.
func endedAt(v any) tOutcome {
	pcs := make([]uintptr, 64)
	frames := runtime.CallersFrames(pcs[:runtime.Callers(3, pcs)])
	var o tOutcome
	if v != nil {
		o.Kind, o.Panic = "panic", fmt.Sprint(v)
	}
	inTesting := false
	for {
		frame, more := frames.Next()
		switch {
		case frame.Function == "testing.(*common).FailNow" && o.Kind == "":
			o.Kind, inTesting = "fatal", true
		case frame.Function == "testing.(*common).SkipNow" && o.Kind == "":
			o.Kind, inTesting = "skip", true
		case strings.HasPrefix(frame.Function, "runtime."):
		case inTesting && strings.HasPrefix(frame.Function, "testing."):
		case o.Kind != "":
			o.Where = fmt.Sprintf("%s (%s:%d)", frame.Function[strings.LastIndex(frame.Function, "/")+1:], filepath.Base(frame.File), frame.Line)
			return o
		}
		if !more {
			break
		}
	}
	if o.Kind == "" {
		o.Kind = "goexit"
	}
	return o
}

// configDescriptor is what a case builds its programs from (3): the runner's
// preparation of the case, as values, and build makes a new program of it
// each time.
type configDescriptor struct {
	currentDirectory string
	toBeCompiled     []*harnessutil.TestFile
	otherFiles       []*harnessutil.TestFile
	tsConfigFile     *harnessutil.TestFile // nil when the test has no tsconfig.json or jsconfig.json
	tsConfig         *tsoptions.ParsedCommandLine
	symlinks         map[string]string
	options          *core.CompilerOptions // as CompileFilesEx gives them to the program
	harnessOptions   harnessutil.HarnessOptions
	rootNames        []string // CompileFilesEx's programFileNames
	includeLibDir    bool
	contentMappers   []*contentmapper.Mapper
}

type testUnit struct {
	content string
	name    string
}

// prepareCase is the runner's preparation of a case: makeUnitsFromTest
// (test_case_parser.go), newCompilerTest up to CompileFiles
// (compiler_runner.go), and CompileFiles and CompileFilesEx up to the
// program (harnessutil.go). A t.Fatal of SetOptionsFromTestConfig is the
// runner's rejection of the case, and ends the preparation.
func prepareCase(c *configCase) (*configDescriptor, tOutcome) {
	// makeUnitsFromTest.
	units, symlinks, currentDirectory, globalOptions, _ := testrunner.ParseTestFilesAndSymlinks(
		c.content,
		c.path,
		func(filename string, content string, fileOptions map[string]string) (*testUnit, error) {
			return &testUnit{content: content, name: filename}, nil
		},
	)
	if currentDirectory == "" {
		currentDirectory = srcFolder
	}
	allFiles := make(map[string]string)
	for _, data := range units {
		allFiles[tspath.GetNormalizedAbsolutePath(data.name, currentDirectory)] = data.content
	}
	parseConfigHost := tsoptionstest.NewVFSParseConfigHostWithSymlinks(allFiles, symlinks, currentDirectory, true /*useCaseSensitiveFileNames*/)
	var existingOptions *core.CompilerOptions
	if globalOptions["runexternalcode"] == "true" {
		existingOptions = &core.CompilerOptions{RunExternalCode: core.TSTrue}
	}
	var tsConfig *tsoptions.ParsedCommandLine
	var tsConfigFileUnitData *testUnit
	for i, data := range units {
		if harnessutil.GetConfigNameFromFileName(data.name) != "" {
			configFileName := tspath.GetNormalizedAbsolutePath(data.name, currentDirectory)
			path := tspath.ToPath(data.name, parseConfigHost.GetCurrentDirectory(), parseConfigHost.Vfs.UseCaseSensitiveFileNames())
			configJson := parser.ParseSourceFile(ast.SourceFileParseOptions{
				FileName: configFileName,
				Path:     path,
			}, data.content, core.ScriptKindJSON)
			tsConfigSourceFile := &tsoptions.TsConfigSourceFile{
				SourceFile: configJson,
			}
			configDir := tspath.GetDirectoryPath(configFileName)
			tsConfig = tsoptions.ParseJsonSourceFileConfigFileContent(
				tsConfigSourceFile,
				parseConfigHost,
				configDir,
				existingOptions,
				nil, /*existingOptionsRaw*/
				configFileName,
				nil, /*resolutionStack*/
				nil, /*extendedConfigCache*/
			)
			tsConfigFileUnitData = data
			units = slices.Delete(units, i, i+1)
			break
		}
	}

	// newCompilerTest. The runner's configuration map is its own and it
	// rewrites baseurl in it: here, a copy.
	harnessConfig := maps.Clone(c.Config)
	d := &configDescriptor{
		currentDirectory: tspath.GetNormalizedAbsolutePath(harnessConfig["currentdirectory"], srcFolder),
		tsConfig:         tsConfig,
		symlinks:         symlinks,
	}
	createHarnessTestFile := func(unit *testUnit) *harnessutil.TestFile {
		return &harnessutil.TestFile{UnitName: tspath.GetNormalizedAbsolutePath(unit.name, d.currentDirectory), Content: unit.content}
	}
	if tsConfig != nil {
		d.tsConfigFile = createHarnessTestFile(tsConfigFileUnitData)
		for _, unit := range units {
			if slices.Contains(tsConfig.ParsedConfig.FileNames, tspath.GetNormalizedAbsolutePath(unit.name, d.currentDirectory)) {
				d.toBeCompiled = append(d.toBeCompiled, createHarnessTestFile(unit))
			} else {
				d.otherFiles = append(d.otherFiles, createHarnessTestFile(unit))
			}
		}
	} else {
		if baseUrl, ok := harnessConfig["baseurl"]; ok && !tspath.IsRootedDiskPath(baseUrl) {
			harnessConfig["baseurl"] = tspath.GetNormalizedAbsolutePath(baseUrl, d.currentDirectory)
		}
		lastUnit := units[len(units)-1]
		if harnessConfig["noimplicitreferences"] != "" ||
			strings.Contains(lastUnit.content, requireStr) ||
			referencesRegex.MatchString(lastUnit.content) {
			d.toBeCompiled = append(d.toBeCompiled, createHarnessTestFile(lastUnit))
			for _, unit := range units[:len(units)-1] {
				d.otherFiles = append(d.otherFiles, createHarnessTestFile(unit))
			}
		} else {
			d.toBeCompiled = core.Map(units, createHarnessTestFile)
		}
	}

	// CompileFiles.
	var compilerOptions *core.CompilerOptions
	if tsConfig != nil {
		compilerOptions = tsConfig.ParsedConfig.CompilerOptions.Clone()
	}
	if compilerOptions == nil {
		compilerOptions = &core.CompilerOptions{}
	}
	if compilerOptions.NewLine == core.NewLineKindNone {
		compilerOptions.NewLine = core.NewLineKindCRLF
	}
	if compilerOptions.SkipDefaultLibCheck == core.TSUnknown {
		compilerOptions.SkipDefaultLibCheck = core.TSTrue
	}
	compilerOptions.NoErrorTruncation = core.TSTrue
	d.harnessOptions = harnessutil.HarnessOptions{UseCaseSensitiveFileNames: true, CurrentDirectory: d.currentDirectory}
	if harnessConfig != nil {
		if o := callWithT(func(t *testing.T) {
			harnessutil.SetOptionsFromTestConfig(t, harnessConfig, compilerOptions, &d.harnessOptions, d.currentDirectory, false /*allowUnknownOptions*/)
		}); o.Kind != "" {
			return nil, o
		}
	}

	// CompileFilesEx, up to the file system.
	for _, file := range d.toBeCompiled {
		fileName := tspath.GetNormalizedAbsolutePath(file.UnitName, d.currentDirectory)
		if !tspath.FileExtensionIs(fileName, tspath.ExtensionJson) &&
			!tspath.FileExtensionIs(fileName, tspath.ExtensionTsBuildInfo) {
			d.rootNames = append(d.rootNames, fileName)
		}
	}
	d.includeLibDir = core.Some(d.toBeCompiled, func(file *harnessutil.TestFile) bool { return strings.Contains(file.Content, testLibFolder+"/") })
	for _, libFile := range d.harnessOptions.LibFiles {
		if libFile == "lib.d.ts" && compilerOptions.NoLib != core.TSTrue {
			continue
		}
		d.rootNames = append(d.rootNames, tspath.CombinePaths(testLibFolder, libFile))
		d.includeLibDir = true
	}
	if compilerOptions.OutDir != "" {
		compilerOptions.OutDir = tspath.GetNormalizedAbsolutePath(compilerOptions.OutDir, d.currentDirectory)
	}
	if compilerOptions.Project != "" {
		compilerOptions.Project = tspath.GetNormalizedAbsolutePath(compilerOptions.Project, d.currentDirectory)
	}
	if compilerOptions.RootDir != "" {
		compilerOptions.RootDir = tspath.GetNormalizedAbsolutePath(compilerOptions.RootDir, d.currentDirectory)
	}
	if compilerOptions.TsBuildInfoFile != "" {
		compilerOptions.TsBuildInfoFile = tspath.GetNormalizedAbsolutePath(compilerOptions.TsBuildInfoFile, d.currentDirectory)
	}
	if compilerOptions.BaseUrl != "" {
		compilerOptions.BaseUrl = tspath.GetNormalizedAbsolutePath(compilerOptions.BaseUrl, d.currentDirectory)
	}
	if compilerOptions.DeclarationDir != "" {
		compilerOptions.DeclarationDir = tspath.GetNormalizedAbsolutePath(compilerOptions.DeclarationDir, d.currentDirectory)
	}
	for i, rootDir := range compilerOptions.RootDirs {
		compilerOptions.RootDirs[i] = tspath.GetNormalizedAbsolutePath(rootDir, d.currentDirectory)
	}
	for i, typeRoot := range compilerOptions.TypeRoots {
		compilerOptions.TypeRoots[i] = tspath.GetNormalizedAbsolutePath(typeRoot, d.currentDirectory)
	}
	if tsConfig != nil && tsConfig.ParsedConfig != nil {
		d.contentMappers = tsConfig.ParsedConfig.ContentMappers
	}
	d.options = compilerOptions
	return d, tOutcome{}
}

// build is the rest of CompileFilesEx and compileFilesWithHost's createProgram
// (harnessutil.go): a new file system, host, content mapper host and project
// and program of the descriptor. The second value closes the content mapper
// project and host: CompileFilesEx closes them when it returns, and here they
// live until the program is no longer run.
//
// What differs from harnessutil, and why it does not change the program:
//   - the program is compiler.NewProgram's; createProgram wraps it in an
//     incremental program when the options say incremental, which adds the
//     build info to the program's diagnostics and emit, and the checker is made
//     on the compiler.Program;
//   - the host traces nothing: its tracer records the module resolution for
//     the runner's baseline;
//   - SingleThreaded is true, as testutil.TestProgramIsSingleThreaded is
//     outside the race detector;
//   - the files are parsed for each program, but the bundled libs
//     (configHost).
func (d *configDescriptor) build() (program *compiler.Program, release func()) {
	testfs := map[string]any{}
	for _, file := range core.Concatenate(d.toBeCompiled, d.otherFiles) {
		testfs[tspath.GetNormalizedAbsolutePath(file.UnitName, d.currentDirectory)] = &fstest.MapFile{
			Data: []byte(file.Content),
		}
	}
	for src, target := range d.symlinks {
		srcFileName := tspath.GetNormalizedAbsolutePath(src, d.currentDirectory)
		targetFileName := tspath.GetNormalizedAbsolutePath(target, d.currentDirectory)
		testfs[srcFileName] = vfstest.Symlink(targetFileName)
	}
	if d.includeLibDir {
		maps.Copy(testfs, testLibFolderMap())
	}
	fs := vfstest.FromMap(testfs, d.harnessOptions.UseCaseSensitiveFileNames)
	fs = bundled.WrapFS(fs)
	fs = harnessutil.NewOutputRecorderFS(fs)

	options := d.options.Clone()
	var closers []func()
	release = func() {
		for i := len(closers) - 1; i >= 0; i-- {
			closers[i]()
		}
	}
	defer func() {
		if v := recover(); v != nil {
			release()
			panic(v)
		}
	}()
	var contentMapperHost contentmapper.Host
	if options.RunExternalCode.IsTrue() && len(d.contentMappers) > 0 {
		contentMapperHost = contentmapper.NewHost(context.Background(), contentmappertest.NewSpawner(), locale.Default)
		closers = append(closers, func() { contentMapperHost.Close() })
	}
	var configFile *tsoptions.TsConfigSourceFile
	var errors []*ast.Diagnostic
	if d.tsConfig != nil {
		configFile = d.tsConfig.ConfigFile
		errors = d.tsConfig.Errors
	}
	config := &tsoptions.ParsedCommandLine{
		ParsedConfig: &tsoptions.ParsedOptions{
			CompilerOptions: options,
			FileNames:       slices.Clone(d.rootNames),
			ContentMappers:  d.contentMappers,
		},
		ConfigFile: configFile,
		Errors:     errors,
	}
	var contentMapperProject contentmapper.Project
	if contentMapperHost != nil {
		contentMapperProject = contentMapperHost.Project(contentmapper.ProjectSpec{
			ConfigFileName:  config.ConfigName(),
			Mappers:         config.ContentMappers(),
			CompilerOptions: config.CompilerOptions(),
		})
		closers = append(closers, func() { contentMapperProject.Close() })
	}
	host := &configHost{CompilerHost: compiler.NewCompilerHost(d.currentDirectory, fs, bundled.LibPath(), nil, nil, contentMapperProject)}
	program = compiler.NewProgram(compiler.ProgramOptions{Config: config, Host: host, SingleThreaded: core.TSTrue})
	return program, release
}

// targets are the program's diagnostic targets and walk targets.
//
// The diagnostic targets are its source files in its order, the root files
// and the files they bring in, but the default libs when the options skip
// their check: that condition of compiler.Program.SkipTypeChecking, and none
// of its others (2026-10-01, the user). The checker checks a file it is given
// whatever the options, and the default libs, checked on both sides for every
// case, were most of the time.
//
// The walk targets are verifyTypesAndSymbols's files (compiler_runner.go):
// those of toBeCompiled and otherFiles that are source files of the program,
// in that order.
func (d *configDescriptor) targets(program *compiler.Program) (diagFiles, walkFiles []*ast.SourceFile) {
	skipDefaultLibs := program.Options().SkipDefaultLibCheck.IsTrue()
	for _, f := range program.SourceFiles() {
		if !(skipDefaultLibs && program.IsSourceFileDefaultLibrary(f.Path())) {
			diagFiles = append(diagFiles, f)
		}
	}
	for _, file := range core.Concatenate(d.toBeCompiled, d.otherFiles) {
		if sf := program.GetSourceFile(file.UnitName); sf != nil {
			walkFiles = append(walkFiles, sf)
		}
	}
	return diagFiles, walkFiles
}

// testLibFolderMap is harnessutil.go's: the files of testdata/tests/lib at
// /.lib.
var testLibFolderMap = sync.OnceValue(func() map[string]any {
	testfs := make(map[string]any)
	libfs := os.DirFS(filepath.Join(repo.TestDataPath(), "tests", "lib"))
	err := fs.WalkDir(libfs, ".", func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			return nil
		}
		content, err := fs.ReadFile(libfs, path)
		if err != nil {
			return err
		}
		testfs[testLibFolder+"/"+path] = &fstest.MapFile{
			Data: content,
		}
		return nil
	})
	if err != nil {
		panic(fmt.Sprintf("Failed to read lib dir: %v", err))
	}
	return testfs
})

// configHost is harnessutil.go's cachedCompilerHost: a file's script kind is
// GetScriptKindFromFileName's, and an unknown one panics. It keeps the
// bundled libs only (cachedLib); the other files are parsed for each program.
type configHost struct {
	compiler.CompilerHost
}

func (h *configHost) GetSourceFile(opts ast.SourceFileParseOptions) *ast.SourceFile {
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
	return parser.ParseSourceFile(opts, text, scriptKind)
}
