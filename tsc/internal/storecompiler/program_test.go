package storecompiler_test

import (
	"maps"
	"slices"
	"testing"

	"github.com/microsoft/TypeScript/tsc/internal/ast"
	"github.com/microsoft/TypeScript/tsc/internal/bundled"
	"github.com/microsoft/TypeScript/tsc/internal/checker"
	"github.com/microsoft/TypeScript/tsc/internal/compiler"
	"github.com/microsoft/TypeScript/tsc/internal/core"
	"github.com/microsoft/TypeScript/tsc/internal/storechecker"
	"github.com/microsoft/TypeScript/tsc/internal/storecompiler"
	"github.com/microsoft/TypeScript/tsc/internal/tsoptions"
	"github.com/microsoft/TypeScript/tsc/internal/tspath"
	"github.com/microsoft/TypeScript/tsc/internal/vfs/vfstest"
)

// The two programs of the same input values, each built from its own FS and
// host: nothing of the Pointer program reaches the Store program.
func newPrograms(t *testing.T, files map[string]string, roots []string, options *core.CompilerOptions) (*compiler.Program, *storecompiler.Program) {
	t.Helper()
	rootFiles := core.Map(roots, func(root string) tspath.RootedFilePath { return tspath.ToRootedFilePath(root, "/") })
	config := func() *tsoptions.ParsedCommandLine {
		return tsoptions.NewParsedCommandLine(options, rootFiles, nil, "/", tspath.CaseSensitive)
	}
	pfs := bundled.WrapFS(vfstest.FromMap(files, tspath.CaseSensitive /*caseSensitivity*/))
	pointer := compiler.NewProgram(compiler.ProgramOptions{
		Config:         config(),
		Host:           compiler.NewCompilerHost(pfs, bundled.LibPath(), nil, nil, nil),
		SingleThreaded: core.TSTrue,
	})
	sfs := bundled.WrapFS(vfstest.FromMap(files, tspath.CaseSensitive /*caseSensitivity*/))
	s := storecompiler.NewProgram(storecompiler.ProgramOptions{
		Config:         config(),
		Host:           storecompiler.NewCompilerHost(sfs, bundled.LibPath(), nil, nil),
		SingleThreaded: core.TSTrue,
	})
	return pointer, s
}

func pointerNames(p *compiler.Program) []string {
	var out []string
	for _, f := range p.SourceFiles() {
		out = append(out, f.FileName().AsString())
	}
	return out
}

func storeNames(p *storecompiler.Program) []string {
	var out []string
	for _, f := range p.SourceFiles() {
		out = append(out, f.FileName().AsString())
	}
	return out
}

// One root with one import: the Store program finds the imported file, the
// default lib and the same order as the Pointer program, and resolves the
// import to the same file.
func TestNativeGraphOneImport(t *testing.T) {
	files := map[string]string{
		"/src/a.ts": "import { b } from \"./b\";\nexport const a: string = b;\n",
		"/src/b.ts": "export const b = 1;\n",
	}
	options := &core.CompilerOptions{Module: core.ModuleKindNodeNext, Target: core.ScriptTargetES2020, Strict: core.TSTrue, NoEmit: core.TSTrue}
	pointer, s := newPrograms(t, files, []string{"/src/a.ts"}, options)

	if got, want := storeNames(s), pointerNames(pointer); !slices.Equal(got, want) {
		t.Fatalf("SourceFiles:\nstore   %v\npointer %v", got, want)
	}
	if !slices.Contains(storeNames(s), "/src/b.ts") {
		t.Fatalf("the import is not in the program: %v", storeNames(s))
	}
	a := s.GetSourceFile("/src/a.ts")
	mode := s.GetModeForUsageLocation(a, a.Imports()[0])
	if mode != pointer.GetModeForUsageLocation(pointer.GetSourceFile("/src/a.ts"), pointer.GetSourceFile("/src/a.ts").Imports()[0]) {
		t.Errorf("the mode of the import differs: %v", mode)
	}
	resolved := s.GetResolvedModule(a, "./b", mode)
	if resolved == nil || resolved.ResolvedFileName != "/src/b.ts" {
		t.Fatalf("./b resolves to %+v", resolved)
	}
	for _, f := range s.SourceFiles() {
		if got, want := s.IsSourceFileDefaultLibrary(f.PathKey()), pointer.IsSourceFileDefaultLibrary(f.PathKey()); got != want {
			t.Errorf("%s: default library %v, Pointer %v", f.FileName(), got, want)
		}
		if got, want := s.GetSourceFileMetaData(f.PathKey()), pointer.GetSourceFileMetaData(f.PathKey()); got != want {
			t.Errorf("%s: metadata %+v, Pointer %+v", f.FileName(), got, want)
		}
	}
	if got, want := slices.Sorted(maps.Keys(s.GetResolvedModules())), slices.Sorted(maps.Keys(pointer.GetResolvedModules())); !slices.Equal(got, want) {
		t.Errorf("resolved modules of %v, Pointer %v", got, want)
	}

	// The checker of each program reports the same diagnostic of a.ts.
	sc, _ := storechecker.NewChecker(s, nil)
	pc, _ := checker.NewChecker(pointer, nil)
	got := sc.GetDiagnostics(t.Context(), a)
	want := pc.GetDiagnostics(t.Context(), pointer.GetSourceFile("/src/a.ts"))
	if len(got) != 1 || len(want) != 1 || got[0].Code() != want[0].Code() || got[0].Pos() != want[0].Pos() {
		t.Errorf("diagnostics of a.ts: store %v, pointer %v", codes(got), codesPointer(want))
	}
}

func codes[D interface{ Code() int32 }](ds []D) []int32 {
	var out []int32
	for _, d := range ds {
		out = append(out, d.Code())
	}
	return out
}

func codesPointer(ds []*ast.Diagnostic) []int32 { return codes(ds) }
