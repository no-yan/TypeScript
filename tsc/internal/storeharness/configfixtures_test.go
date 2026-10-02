package storeharness

import (
	"encoding/json"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/microsoft/TypeScript/tsc/internal/ast"
	"github.com/microsoft/TypeScript/tsc/internal/ast/store"
	"github.com/microsoft/TypeScript/tsc/internal/compiler"
	"github.com/microsoft/TypeScript/tsc/internal/core"
	"github.com/microsoft/TypeScript/tsc/internal/testutil/harnessutil"
	"github.com/microsoft/TypeScript/tsc/internal/tspath"
)

// The fixtures of stage 2 (store-checker-7d2b3-implementation-instructions-20261001.md
// 6.2): testdata/configurations, tests in the runner's format, enumerated as a
// suite of their own. What each must give is written out from reading the
// runner, not from the harness's copy of it.

func fixtureCases(t *testing.T) (map[string]*configCase, []*configCase, []*inputError) {
	t.Helper()
	dir, err := filepath.Abs(filepath.Join("testdata", "configurations"))
	if err != nil {
		t.Fatal(err)
	}
	cases, inputErrors, files, err := enumerateConfigurations([]configSuite{{"fixture", tspath.NormalizeSlashes(dir)}})
	if err != nil {
		t.Fatal(err)
	}
	checkManifest(t, cases, inputErrors, files)
	byID := map[string]*configCase{}
	for _, c := range cases {
		byID[c.ID] = c
	}
	return byID, cases, inputErrors
}

// fixtureRun is what compareCase did with a case: its result, its descriptor,
// the first Pointer program, and the two runs.
type fixtureRun struct {
	caseResult
	d       *configDescriptor
	program *compiler.Program
	p, s    *run
}

func runFixture(c *configCase) fixtureRun {
	var f fixtureRun
	f.caseResult = compareCase(c, caseHooks{
		onBuild: func(d *configDescriptor, program *compiler.Program) {
			if f.program == nil {
				f.d, f.program = d, program
			}
		},
		afterRuns: func(p, s *run) { f.p, f.s = p, s },
	})
	return f
}

// diagsIn are the codes of the Pointer run's diagnostics of the diagnostic
// target of that name.
func (f fixtureRun) diagsIn(fileName string) []int32 {
	var codes []int32
	diag, _ := f.d.targets(f.program)
	for i, sf := range diag {
		if sf.FileName() == fileName {
			for _, d := range f.p.diags[i] {
				codes = append(codes, d.code)
			}
		}
	}
	return codes
}

func (f fixtureRun) walkNames() []string {
	_, walk := f.d.targets(f.program)
	return core.Map(walk, (*ast.SourceFile).FileName)
}

func (f fixtureRun) sourceFileNames() []string {
	return core.Map(f.program.SourceFiles(), (*ast.SourceFile).FileName)
}

func unitNames(files []*harnessutil.TestFile) []string {
	return core.Map(files, func(f *harnessutil.TestFile) string { return f.UnitName })
}

func TestConfigurationFixtures(t *testing.T) {
	byID, cases, inputErrors := fixtureCases(t)
	wantIDs := []string{
		"fixture/caseInsensitive.ts",
		"fixture/configPriority.ts",
		"fixture/defaultLibChecked.ts",
		"fixture/dependencyOnly.ts",
		"fixture/emptyRoots.ts",
		"fixture/importHelpers.ts",
		"fixture/importHelpersNodeNext.ts",
		"fixture/jsCheck.ts",
		"fixture/jsonImport.ts",
		"fixture/jsxDevRuntime.tsx",
		"fixture/jsxImportSourcePragma.tsx",
		"fixture/jsxRuntime.tsx",
		"fixture/jsxRuntimeMissing.tsx",
		"fixture/laterRoot.ts",
		"fixture/libFiles.ts",
		"fixture/nodeNext.ts",
		"fixture/rejectedAmd.ts",
		"fixture/rejectedUnknownOption.ts",
		"fixture/skippedTarget.ts",
		"fixture/symlinked.ts",
		"fixture/uncheckedTargets.ts",
		// @target: esnext, es2015, ES2015 (ES2015 is es2015), @strict: *, and
		// @module: commonjs, esnext, -commonjs (esnext alone, not varied).
		"fixture/variations.ts(strict=false,target=es2015)",
		"fixture/variations.ts(strict=false,target=esnext)",
		"fixture/variations.ts(strict=true,target=es2015)",
		"fixture/variations.ts(strict=true,target=esnext)",
	}
	if ids := core.Map(cases, func(c *configCase) string { return c.ID }); !slices.Equal(ids, wantIDs) {
		t.Fatalf("ids in index order:\n%s\nwant\n%s", strings.Join(ids, "\n"), strings.Join(wantIDs, "\n"))
	}
	// More than 25 variations end GetFileBasedTestConfigurations with t.Fatal,
	// and an empty set panics in splitOptionValues.
	wantErrors := map[string]string{
		"emptyVariation.ts":    "panic in harnessutil.splitOptionValues",
		"tooManyVariations.ts": "t.fatal in harnessutil.GetFileBasedTestConfigurations",
	}
	if len(inputErrors) != len(wantErrors) {
		t.Errorf("%d input errors, want %d", len(inputErrors), len(wantErrors))
	}
	for _, e := range inputErrors {
		if want, ok := wantErrors[e.File]; !ok || !strings.HasPrefix(e.Reason, want) {
			t.Errorf("input error %s: %s, want %q", e.File, e.Reason, want)
		}
	}

	run := func(id string) fixtureRun {
		t.Helper()
		c := byID[id]
		if c == nil {
			t.Fatalf("no case %s", id)
		}
		return runFixture(c)
	}
	wantStatus := func(t *testing.T, f fixtureRun, status string) {
		t.Helper()
		if f.status != status {
			where := ""
			if f.Detail != nil {
				where = f.Detail.Where + "\nPointer: " + clip(f.Detail.Pointer) + "\nStore: " + clip(f.Detail.Store)
			}
			t.Fatalf("status %s (%s: %s), want %s\n%s", f.status, f.Class, clip(f.reason), status, where)
		}
	}

	t.Run("dependency file", func(t *testing.T) {
		// noImplicitReferences: the last unit is the only root; dep.ts is an
		// other file, in the program through the import.
		f := run("fixture/dependencyOnly.ts")
		wantStatus(t, f, statusEqual)
		wantStrings(t, "roots", f.d.rootNames, []string{"/.src/main.ts"})
		wantStrings(t, "toBeCompiled", unitNames(f.d.toBeCompiled), []string{"/.src/main.ts"})
		wantStrings(t, "otherFiles", unitNames(f.d.otherFiles), []string{"/.src/dep.ts"})
		wantStrings(t, "walk targets", f.walkNames(), []string{"/.src/main.ts", "/.src/dep.ts"})
		if codes := f.diagsIn("/.src/dep.ts"); !slices.Contains(codes, 2322) {
			t.Errorf("dep.ts: diagnostics %v, want TS2322", codes)
		}
		if codes := f.diagsIn("/.src/main.ts"); len(codes) != 0 {
			t.Errorf("main.ts: diagnostics %v, want none", codes)
		}
		wantWalked(t, f, []string{"value", "fromDep"})
	})

	t.Run("later root", func(t *testing.T) {
		f := run("fixture/laterRoot.ts")
		wantStatus(t, f, statusEqual)
		wantStrings(t, "roots", f.d.rootNames, []string{"/.src/a.ts", "/.src/b.ts", "/.src/c.ts"})
		// skipDefaultLibCheck is the runner's default: the default libs are
		// not diagnostic targets.
		diag, _ := f.d.targets(f.program)
		wantStrings(t, "diagnostic targets", core.Map(diag, (*ast.SourceFile).FileName), []string{"/.src/a.ts", "/.src/b.ts", "/.src/c.ts"})
		for _, name := range []string{"/.src/a.ts", "/.src/b.ts"} {
			if codes := f.diagsIn(name); len(codes) != 0 {
				t.Errorf("%s: diagnostics %v, want none", name, codes)
			}
		}
		if codes := f.diagsIn("/.src/c.ts"); !slices.Equal(codes, []int32{2322}) {
			t.Errorf("c.ts: diagnostics %v, want [2322]", codes)
		}
		wantWalked(t, f, []string{"a", "b", "c"})
	})

	t.Run("config and directives", func(t *testing.T) {
		// The config's options, cloned, then the runner's defaults where the
		// config sets none, then the directives (CompileFiles).
		f := run("fixture/configPriority.ts")
		wantStatus(t, f, statusEqual)
		o := f.program.Options()
		for _, c := range []struct {
			name      string
			got, want any
		}{
			{"strict (directive over the config)", o.Strict, core.TSTrue},
			{"newLine (the config over the default)", o.NewLine, core.NewLineKindLF},
			{"allowJs (the config)", o.AllowJs, core.TSTrue},
			{"checkJs (directive)", o.CheckJs, core.TSTrue},
			{"module (the config)", o.Module, core.ModuleKindESNext},
			{"target (the config)", o.Target, core.ScriptTargetES2017},
			{"skipDefaultLibCheck (default)", o.SkipDefaultLibCheck, core.TSTrue},
			{"noErrorTruncation (default)", o.NoErrorTruncation, core.TSTrue},
		} {
			if c.got != c.want {
				t.Errorf("%s: %v, want %v", c.name, c.got, c.want)
			}
		}
		wantStrings(t, "toBeCompiled", unitNames(f.d.toBeCompiled), []string{"/proj/a.ts", "/proj/b.js"})
		wantStrings(t, "otherFiles", unitNames(f.d.otherFiles), []string{"/proj/other.ts"})
		wantStrings(t, "walk targets", f.walkNames(), []string{"/proj/a.ts", "/proj/b.js"})
		if codes := f.diagsIn("/proj/a.ts"); !slices.Contains(codes, 7006) {
			t.Errorf("a.ts: diagnostics %v, want TS7006 (strict)", codes)
		}
		if codes := f.diagsIn("/proj/b.js"); !slices.Contains(codes, 2322) {
			t.Errorf("b.js: diagnostics %v, want TS2322 (checkJs)", codes)
		}
	})

	t.Run("variations", func(t *testing.T) {
		for _, c := range cases {
			if !strings.HasPrefix(c.ID, "fixture/variations.ts(") {
				continue
			}
			f := runFixture(c)
			wantStatus(t, f, statusEqual)
			o := f.program.Options()
			wantStrict := core.TSFalse
			if strings.Contains(c.ID, "strict=true") {
				wantStrict = core.TSTrue
			}
			wantTarget := core.ScriptTargetES2015
			if strings.Contains(c.ID, "target=esnext") {
				wantTarget = core.ScriptTargetESNext
			}
			if o.Strict != wantStrict || o.Target != wantTarget || o.Module != core.ModuleKindESNext {
				t.Errorf("%s: strict %v target %v module %v", c.ID, o.Strict, o.Target, o.Module)
			}
		}
	})

	t.Run("case insensitive", func(t *testing.T) {
		f := run("fixture/caseInsensitive.ts")
		wantStatus(t, f, statusEqual)
		if f.program.Host().FS().UseCaseSensitiveFileNames() {
			t.Error("the file system is case sensitive")
		}
		// The name as written, found in the case-insensitive file system.
		if resolved := resolvedModule(f.program, "/src/main.ts", "./LIB"); resolved != "/src/LIB.ts" {
			t.Errorf(`"./LIB" resolves to %q, want /src/LIB.ts`, resolved)
		}
		if codes := f.diagsIn("/src/main.ts"); !slices.Contains(codes, 2322) {
			t.Errorf("main.ts: diagnostics %v, want TS2322 (the imported number)", codes)
		}
	})

	t.Run("symlinks", func(t *testing.T) {
		f := run("fixture/symlinked.ts")
		wantStatus(t, f, statusEqual)
		if len(f.d.symlinks) != 2 {
			t.Errorf("symlinks %v, want the @link and the @symlink", f.d.symlinks)
		}
		// The real paths (preserveSymlinks is not set).
		for name, want := range map[string]string{"linked": "/packages/real/index.ts", "other": "/packages/other/index.ts"} {
			if resolved := resolvedModule(f.program, "/app/main.ts", name); resolved != want {
				t.Errorf("%q resolves to %q, want %s", name, resolved, want)
			}
		}
		if codes := f.diagsIn("/app/main.ts"); !slices.Contains(codes, 2322) {
			t.Errorf("main.ts: diagnostics %v, want TS2322 (the types of the linked files)", codes)
		}
	})

	t.Run("json", func(t *testing.T) {
		// A JSON unit is compiled, and not a root (CompileFilesEx).
		f := run("fixture/jsonImport.ts")
		wantStatus(t, f, statusEqual)
		wantStrings(t, "toBeCompiled", unitNames(f.d.toBeCompiled), []string{"/src/data.json", "/src/main.ts"})
		wantStrings(t, "roots", f.d.rootNames, []string{"/src/main.ts"})
		wantStrings(t, "walk targets", f.walkNames(), []string{"/src/data.json", "/src/main.ts"})
	})

	t.Run("lib files", func(t *testing.T) {
		f := run("fixture/libFiles.ts")
		wantStatus(t, f, statusEqual)
		wantStrings(t, "roots", f.d.rootNames, []string{"/.src/main.tsx", "/.lib/react.d.ts"})
		if !f.d.includeLibDir {
			t.Error("the /.lib files are not in the file system")
		}
		// A /.lib file is not a default lib: it is a diagnostic target.
		diag, _ := f.d.targets(f.program)
		wantStrings(t, "diagnostic targets", core.Map(diag, (*ast.SourceFile).FileName), []string{"/.src/main.tsx", "/.lib/react.d.ts"})
		names := f.sourceFileNames()
		if !slices.Contains(names, "/.lib/react.d.ts") || !slices.ContainsFunc(names, func(n string) bool { return strings.HasSuffix(n, "lib.es2015.collection.d.ts") }) {
			t.Errorf("source files %v", names)
		}
		if slices.ContainsFunc(names, func(n string) bool { return strings.HasSuffix(n, "lib.dom.d.ts") }) {
			t.Errorf("@lib: es2015 has lib.dom.d.ts: %v", names)
		}
	})

	t.Run("default libs checked", func(t *testing.T) {
		// @skipDefaultLibCheck: false: the default libs are diagnostic
		// targets, in the program's order, before the root.
		f := run("fixture/defaultLibChecked.ts")
		wantStatus(t, f, statusEqual)
		diag, _ := f.d.targets(f.program)
		names := core.Map(diag, (*ast.SourceFile).FileName)
		if len(names) != len(f.program.SourceFiles()) || !slices.ContainsFunc(names, func(n string) bool { return strings.HasSuffix(n, "/lib.es5.d.ts") }) || names[len(names)-1] != "/.src/defaultLibChecked.ts" {
			t.Errorf("diagnostic targets %v, want every source file", names)
		}
	})

	t.Run("files the program does not check", func(t *testing.T) {
		// noCheck, skipLibCheck and a JS file without checkJs: the program
		// checks none of them, and they are diagnostic targets all the same.
		f := run("fixture/uncheckedTargets.ts")
		wantStatus(t, f, statusEqual)
		diag, _ := f.d.targets(f.program)
		wantStrings(t, "diagnostic targets", core.Map(diag, (*ast.SourceFile).FileName), []string{"/.src/decl.d.ts", "/.src/a.js", "/.src/main.ts"})
		if codes := f.diagsIn("/.src/main.ts"); !slices.Contains(codes, 2322) {
			t.Errorf("main.ts: diagnostics %v, want TS2322 (noCheck is the program's)", codes)
		}
	})

	t.Run("js", func(t *testing.T) {
		f := run("fixture/jsCheck.ts")
		wantStatus(t, f, statusEqual)
		if codes := f.diagsIn("/.src/a.js"); !slices.Contains(codes, 2345) {
			t.Errorf("a.js: diagnostics %v, want TS2345", codes)
		}
	})

	t.Run("nodenext", func(t *testing.T) {
		f := run("fixture/nodeNext.ts")
		wantStatus(t, f, statusEqual)
		// Two imports and an import call: the modes of all three compared.
		if f.Extra != 3 {
			t.Errorf("%d module specifier positions compared, want 3", f.Extra)
		}
		index := f.program.GetSourceFile("/src/index.ts")
		if mode := f.program.GetDefaultResolutionModeForFile(index); mode != core.ModuleKindESNext {
			t.Errorf("index.ts: mode %v, want ESNext (package.json type module)", mode)
		}
	})

	t.Run("empty roots", func(t *testing.T) {
		// "files": [] is a config error, and no unit is compiled: a program
		// of no root has no source file, the libs included. The comparison
		// is of the global diagnostics, and of empty targets.
		f := run("fixture/emptyRoots.ts")
		wantStatus(t, f, statusEqual)
		if len(f.d.rootNames) != 0 || len(f.d.toBeCompiled) != 0 || f.walkFiles != 0 || f.diagFiles != 0 || len(f.program.SourceFiles()) != 0 {
			t.Errorf("roots %v, toBeCompiled %d, walk targets %d, diagnostic targets %d, source files %v; want none",
				f.d.rootNames, len(f.d.toBeCompiled), f.walkFiles, f.diagFiles, f.sourceFileNames())
		}
		wantStrings(t, "otherFiles", unitNames(f.d.otherFiles), []string{"/p/a.ts"})
		if len(f.d.tsConfig.Errors) == 0 {
			t.Error("the config has no error")
		}
	})

	t.Run("runner", func(t *testing.T) {
		for _, c := range []struct{ id, status, reason string }{
			{"fixture/rejectedAmd.ts", statusRunnerRejected, "t.fatal in harnessutil.failOnUnsupportedCompilerOptions"},
			{"fixture/rejectedUnknownOption.ts", statusRunnerRejected, "t.fatal in harnessutil.SetOptionsFromTestConfig"},
			{"fixture/skippedTarget.ts", statusRunnerSkipped, "t.skip in harnessutil.SkipUnsupportedCompilerOptions"},
		} {
			f := run(c.id)
			if f.status != c.status || !strings.HasPrefix(f.reason, c.reason) {
				t.Errorf("%s: %s (%s), want %s (%s)", c.id, f.status, f.reason, c.status, c.reason)
			}
		}
	})

	t.Run("synthetic imports", func(t *testing.T) {
		// The Store's synthetic imports (store.NewSyntheticImport), as the
		// Pointer program has them: the checker takes them, the modes are
		// the Pointer's, and the case is equal.
		for _, c := range []struct {
			id      string
			files   map[string]string // the specifiers' text, by file
			counts  [2]int
			helpers bool
			modes   int
		}{
			{"fixture/jsxRuntime.tsx", map[string]string{"/index.tsx": "react/jsx-runtime"}, [2]int{1, 0}, false, 1},
			{"fixture/jsxDevRuntime.tsx", map[string]string{"/index.tsx": "react/jsx-dev-runtime"}, [2]int{1, 0}, false, 1},
			{"fixture/jsxImportSourcePragma.tsx", map[string]string{"/index.tsx": "preact/jsx-runtime"}, [2]int{1, 0}, false, 1},
			{"fixture/jsxRuntimeMissing.tsx", map[string]string{"/index.tsx": "react/jsx-runtime"}, [2]int{1, 0}, false, 1},
			{"fixture/importHelpers.ts", map[string]string{"/index.ts": "tslib"}, [2]int{0, 1}, true, 1},
			{"fixture/importHelpersNodeNext.ts", map[string]string{"/a.mts": "tslib", "/b.cts": "tslib"}, [2]int{0, 2}, true, 2},
		} {
			f := run(c.id)
			if f.status != statusEqual || f.SyntheticAsked == 0 || f.synthetic != c.counts || f.Extra != c.modes {
				t.Errorf("%s: %s %s, %d taken, %v specifiers, %d modes; want equal, some taken, %v, %d", c.id, f.status, f.Class, f.SyntheticAsked, f.synthetic, f.Extra, c.counts, c.modes)
				continue
			}
			program := f.s.store
			for name, text := range c.files {
				file := program.GetSourceFile(name)
				var specifier, other store.Node
				if c.helpers {
					specifier, other = program.GetImportHelpersImportSpecifier(file.Path()), program.jsxImports[file.Path()]
				} else {
					_, specifier = program.GetJSXRuntimeImportSpecifier(file.Path())
					other = program.helperImports[file.Path()]
				}
				decl := specifier.Parent()
				if specifier.Text() != text || decl.Kind() != ast.KindImportDeclaration || !store.SameNode(decl.Parent(), file.Root()) || specifier.File() != file || !other.IsNil() {
					t.Errorf("%s %s: %v %q, parent %v", c.id, name, specifier.Kind(), specifier.Text(), decl.Kind())
				}
				again := program.GetImportHelpersImportSpecifier(file.Path())
				if !c.helpers {
					_, again = program.GetJSXRuntimeImportSpecifier(file.Path())
				}
				if !store.SameNode(again, specifier) {
					t.Errorf("%s %s: another node the second time", c.id, name)
				}
			}
		}
		// No synthetic import where the Pointer program has none.
		f := run("fixture/laterRoot.ts")
		if f.synthetic != [2]int{} || f.SyntheticAsked != 0 || len(f.s.store.helperImports)+len(f.s.store.jsxImports) != 0 {
			t.Errorf("laterRoot.ts: %v specifiers, %d taken", f.synthetic, f.SyntheticAsked)
		}
		// The runtime that does not resolve: the error is at the tag.
		if codes := run("fixture/jsxRuntimeMissing.tsx").diagsIn("/index.tsx"); len(codes) == 0 {
			t.Error("jsxRuntimeMissing.tsx: no diagnostic")
		}
	})
}

// resolvedModule is the file a module name of the file resolves to, in any
// mode, or "".
func resolvedModule(program *compiler.Program, fileName, name string) string {
	file := program.GetSourceFile(fileName)
	if file == nil {
		return ""
	}
	for key, r := range program.GetResolvedModules()[file.Path()] {
		if key.Name == name && r.IsResolved() {
			return r.ResolvedFileName
		}
	}
	return ""
}

func wantStrings(t *testing.T, what string, got, want []string) {
	t.Helper()
	if !slices.Equal(got, want) {
		t.Errorf("%s: %v, want %v", what, got, want)
	}
}

// wantWalked: every walk target has records on both sides, and the symbol
// walk has the names.
func wantWalked(t *testing.T, f fixtureRun, names []string) {
	t.Helper()
	for side, r := range []*run{f.p, f.s} {
		if len(r.walked) != f.walkFiles || slices.Contains(r.walked, 0) {
			t.Errorf("side %d: records of each walk target %v", side, r.walked)
		}
		for _, name := range names {
			if !slices.ContainsFunc(r.symbols, func(w walkRecord) bool { return w.text == name && w.present }) {
				t.Errorf("side %d: no symbol record of %s", side, name)
			}
		}
	}
}

// TestConfigurationRebuild: the programs of a descriptor share no mutable
// state, and are the same program (6.3); a rerun of the Pointer builds the
// same program again.
func TestConfigurationRebuild(t *testing.T) {
	byID, _, _ := fixtureCases(t)
	for _, id := range []string{"fixture/configPriority.ts", "fixture/symlinked.ts", "fixture/dependencyOnly.ts", "fixture/libFiles.ts"} {
		t.Run(id, func(t *testing.T) {
			d, o := prepareCase(byID[id])
			if d == nil {
				t.Fatalf("not prepared: %s", o)
			}
			before := optionsJSON(t, d.options)
			var programs []*compiler.Program
			for range 3 {
				program, release := d.build()
				defer release()
				programs = append(programs, program)
			}
			first := programSummary(t, d, programs[0])
			for i, program := range programs[1:] {
				if summary := programSummary(t, d, program); summary != first {
					t.Errorf("build %d:\n%s\nthe first:\n%s", i+1, summary, first)
				}
				previous := programs[i]
				if program.Options() == previous.Options() || program.Options() == d.options || program.Host() == previous.Host() || program.Host().FS() == previous.Host().FS() {
					t.Errorf("build %d shares the options, the host or the file system of the one before", i+1)
				}
				for j, f := range program.SourceFiles() {
					if !isBundledLib(f.FileName()) && f == previous.SourceFiles()[j] {
						t.Errorf("build %d shares %s with the one before", i+1, f.FileName())
					}
				}
			}
			if after := optionsJSON(t, d.options); after != before {
				t.Errorf("the descriptor's options changed:\n%s\n%s", before, after)
			}
		})
	}

	t.Run("rerun", func(t *testing.T) {
		// A Store record changed: the records differ, and the Pointer runs
		// again pointerReruns times, each on a program built again from the
		// descriptor, the same as the first.
		var summaries []string
		r := compareCase(byID["fixture/configPriority.ts"], caseHooks{
			onBuild: func(d *configDescriptor, program *compiler.Program) {
				summaries = append(summaries, programSummary(t, d, program))
			},
			afterRuns: func(p, s *run) { s.types[0].value += " (changed)" },
		})
		if r.status != statusMismatch || r.Class != "walk: type" || r.Reruns != pointerReruns || r.Excluded != "" {
			t.Fatalf("%s %s, %d reruns, excluded %q; want mismatch, walk: type, %d reruns", r.status, r.Class, r.Reruns, r.Excluded, pointerReruns)
		}
		if len(summaries) != 1+pointerReruns {
			t.Fatalf("%d programs built, want %d", len(summaries), 1+pointerReruns)
		}
		for i, s := range summaries[1:] {
			if s != summaries[0] {
				t.Errorf("rerun %d:\n%s\nthe first:\n%s", i+1, s, summaries[0])
			}
		}
	})
}

func optionsJSON(t *testing.T, o *core.CompilerOptions) string {
	b, err := json.Marshal(o)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// programSummary is what a program is for the comparison: its options, roots
// and source files (name, text, script kind, parse options), and the
// descriptor's targets in it.
func programSummary(t *testing.T, d *configDescriptor, program *compiler.Program) string {
	var b strings.Builder
	b.WriteString(optionsJSON(t, program.Options()))
	b.WriteString("\nroots: " + strings.Join(program.CommandLine().FileNames(), " "))
	for _, f := range program.SourceFiles() {
		opts, _ := json.Marshal(f.ParseOptions())
		b.WriteString("\n" + f.FileName() + " " + contentHash(f.Text())[:16] + " " + f.ScriptKind.String() + " " + string(opts))
	}
	diag, walk := d.targets(program)
	b.WriteString("\ndiagnostic targets: " + strings.Join(core.Map(diag, (*ast.SourceFile).FileName), " "))
	b.WriteString("\nwalk targets: " + strings.Join(core.Map(walk, (*ast.SourceFile).FileName), " "))
	return b.String()
}

// TestConfigurationClasses: the status of a case follows what its runs give:
// a Store panic is a mismatch, and a token is found (on the walk's values and
// on a diagnostic's).
func TestConfigurationClasses(t *testing.T) {
	byID, _, _ := fixtureCases(t)
	plain := byID["fixture/laterRoot.ts"]
	for _, c := range []struct {
		name   string
		change func(p, s *run)
		status string
		class  string
	}{
		{"store panic", func(p, s *run) { s.panicked = &panicInfo{message: "some panic", frame: "storechecker.f"} }, statusMismatch, storePanicClassPrefix + "some panic @ storechecker.f"},
		{"token in a walk value", func(p, s *run) { s.symbols[0].value += " __@x@1" }, statusMismatch, ClassInternalIDTokenPresent},
		{"raw token in a diagnostic", func(p, s *run) {
			for i := range s.diags {
				if len(s.diags[i]) > 0 {
					s.diags[i][0].messageText += " \xFE@x@1"
					return
				}
			}
		}, statusMismatch, ClassInternalIDTokenPresent},
	} {
		t.Run(c.name, func(t *testing.T) {
			r := compareCase(plain, caseHooks{afterRuns: c.change})
			if r.status != c.status || r.Class != c.class {
				t.Errorf("%s %q (%s), want %s %q", r.status, r.Class, r.reason, c.status, c.class)
			}
		})
	}
}

// TestConfigurationContentMappers: the content mapper tests of the compiler
// suite build with the mapper's project, and run: none is unsupported.
func TestConfigurationContentMappers(t *testing.T) {
	if testing.Short() {
		t.Skip("the suites are not enumerated with -short")
	}
	cases, _, _, err := enumerateConfigurations(runnerSuites[:1])
	if err != nil {
		t.Skipf("the compiler suite: %v", err)
	}
	n := 0
	for _, c := range cases {
		if !strings.HasPrefix(c.File, "contentMapper") {
			continue
		}
		n++
		live, mapped := false, 0
		r := compareCase(c, caseHooks{onBuild: func(d *configDescriptor, program *compiler.Program) {
			live = live || program.Host().ContentMapperProject() != nil
			for _, f := range program.SourceFiles() {
				if f.ContentMapper() != "" {
					mapped++
				}
			}
		}})
		t.Logf("%s: %s %s, mapper project %v, %d mapped files", c.ID, r.status, r.Class, live, mapped)
		if r.status != statusEqual && r.status != statusMismatch {
			t.Errorf("%s: %s (%s)", c.ID, r.status, clip(r.reason))
		}
		if !live {
			t.Errorf("%s: built without the mapper's project", c.ID)
		}
	}
	if n == 0 {
		t.Error("no content mapper test")
	}
}
