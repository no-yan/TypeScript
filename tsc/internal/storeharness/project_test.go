package storeharness

import (
	"path/filepath"
	"testing"

	"github.com/microsoft/TypeScript/tsc/internal/ast"
	"github.com/microsoft/TypeScript/tsc/internal/bundled"
	"github.com/microsoft/TypeScript/tsc/internal/compiler"
	"github.com/microsoft/TypeScript/tsc/internal/core"
	"github.com/microsoft/TypeScript/tsc/internal/repo"
	"github.com/microsoft/TypeScript/tsc/internal/tsoptions"
	"github.com/microsoft/TypeScript/tsc/internal/tspath"
	"github.com/microsoft/TypeScript/tsc/internal/vfs/osvfs"
)

// The compiler project of the KPC benchmarks (compiler/ast_benchmark_test.go),
// read the same way, single-threaded (store-checker-7d2c-implementation-
// instructions-20261002.md 2).

func newCompilerProjectProgram(tb testing.TB) *compiler.Program {
	tb.Helper()
	configFileName := tspath.NormalizeSlashes(filepath.Join(repo.TestDataPath(), "fixtures/compiler/tsconfig.json"))
	root := filepath.Dir(configFileName)
	fs := bundled.WrapFS(osvfs.FS())
	host := compiler.NewCompilerHost(root, fs, bundled.LibPath(), nil, nil, nil)
	config, errors := tsoptions.GetParsedCommandLineOfConfigFile(configFileName, &core.CompilerOptions{}, nil, host, nil)
	if len(errors) != 0 || config == nil {
		tb.Fatalf("parse project config %s: %d errors", configFileName, len(errors))
	}
	return compiler.NewProgram(compiler.ProgramOptions{Config: config, Host: host, SingleThreaded: core.TSTrue})
}

// projectTargets are stage 2's diagnostic targets (the source files but the
// default libs when the options skip their check) and the root files as the
// walk targets.
func projectTargets(program *compiler.Program) (diagFiles, walkFiles []*ast.SourceFile) {
	skipDefaultLibs := program.Options().SkipDefaultLibCheck.IsTrue()
	roots := map[string]bool{}
	for _, name := range program.CommandLine().FileNames() {
		roots[name] = true
	}
	for _, f := range program.SourceFiles() {
		if !(skipDefaultLibs && program.IsSourceFileDefaultLibrary(f.Path())) {
			diagFiles = append(diagFiles, f)
		}
		if roots[f.FileName()] {
			walkFiles = append(walkFiles, f)
		}
	}
	return diagFiles, walkFiles
}

func TestCompilerProjectEquivalence(t *testing.T) {
	if testing.Short() {
		t.Skip("the compiler project")
	}
	pointer := newCompilerProjectProgram(t)
	rebuild := func() (*compiler.Program, func()) { return newCompilerProjectProgram(t), func() {} }
	r := comparePrograms(pointer, projectTargets, rebuild, compareOptions{extra: compareModes})
	diagFiles, walkFiles := projectTargets(pointer)
	t.Logf("diagnostic targets %d, walk targets %d, counts %+v, pairs %v, synthetic %d, mode positions %d",
		len(diagFiles), len(walkFiles), r.Counts, r.Pairs, r.SyntheticAsked, r.Extra)
	if r.Class != ClassEqual || r.Excluded != "" {
		t.Fatalf("class %q, excluded %q: %+v", r.Class, r.Excluded, r.Detail)
	}
}
