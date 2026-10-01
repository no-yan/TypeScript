// Package storeharness runs the Store checker beside the Pointer checker on
// the same program and compares what they report: the H1 harness of
// internal/ast/docs/store-checker-design-20260923.md 2.1, as
// store-checker-7d2b1-implementation-instructions-20261001.md 5 and 6 build it.
// The Pointer program gives the files, the options and the module
// resolution; the Store parses the same texts again into its own files, and
// Program (adapter.go) is the storechecker.Program over both.
package storeharness

import (
	"maps"
	"slices"
	"strings"

	"github.com/microsoft/TypeScript/tsc/internal/ast"
	"github.com/microsoft/TypeScript/tsc/internal/ast/store"
	"github.com/microsoft/TypeScript/tsc/internal/bundled"
	"github.com/microsoft/TypeScript/tsc/internal/collections"
	"github.com/microsoft/TypeScript/tsc/internal/compiler"
	"github.com/microsoft/TypeScript/tsc/internal/core"
	"github.com/microsoft/TypeScript/tsc/internal/parser"
	"github.com/microsoft/TypeScript/tsc/internal/storeparser"
	"github.com/microsoft/TypeScript/tsc/internal/tsoptions"
	"github.com/microsoft/TypeScript/tsc/internal/tspath"
	"github.com/microsoft/TypeScript/tsc/internal/vfs/vfstest"
)

// NewPrograms makes the Pointer program of files (normalized absolute path ->
// text, every file a root) under options, single-threaded, and the Store
// program of the same source files (5.1).
func NewPrograms(files map[string]string, options *core.CompilerOptions) (*compiler.Program, *Program) {
	pointer := newPointerProgram(files, options)
	return pointer, newProgram(pointer)
}

func newPointerProgram(files map[string]string, options *core.CompilerOptions) *compiler.Program {
	fs := bundled.WrapFS(vfstest.FromMap(files, true /*useCaseSensitiveFileNames*/))
	host := &libCachingHost{CompilerHost: compiler.NewCompilerHost("/", fs, bundled.LibPath(), nil, nil, nil)}
	config := &tsoptions.ParsedCommandLine{
		ParsedConfig: &tsoptions.ParsedOptions{
			CompilerOptions: options,
			FileNames:       slices.Sorted(maps.Keys(files)),
		},
	}
	return compiler.NewProgram(compiler.ProgramOptions{Config: config, Host: host, SingleThreaded: core.TSTrue})
}

// newProgram parses every source file of the Pointer program again into the
// Store, in the program's order, each with its StoreID in the program's
// IDSpace. The Store files are the program's own, the libs included: a Store
// belongs to one IDSpace, and a Factory refuses the nodes of another
// (idspace.go, factory.go), so they are not shared between programs.
func newProgram(pointer *compiler.Program) *Program {
	sources := pointer.SourceFiles()
	p := &Program{
		pointer:  pointer,
		space:    store.NewIDSpace(len(sources)),
		files:    make([]*store.File, len(sources)),
		byPath:   make(map[tspath.Path]*store.File, len(sources)),
		pointers: make(map[tspath.Path]*ast.SourceFile, len(sources)),
	}
	for i, pf := range sources {
		f := storeparser.ParseSourceFileIn(p.space, store.StoreID(i), pf.ParseOptions(), pf.Text(), pf.ScriptKind)
		p.files[i] = f
		p.byPath[f.Path()] = f
		p.pointers[f.Path()] = pf
	}
	return p
}

// libCachingHost shares the Pointer source files of the bundled libs between
// the programs, in the form of harnessutil's cachedCompilerHost
// (testutil/harnessutil/harnessutil.go): the parse options, the text and the
// script kind are the key. The other files are parsed for each program.
type libCachingHost struct {
	compiler.CompilerHost
}

type libCacheKey struct {
	opts       ast.SourceFileParseOptions
	text       string
	scriptKind core.ScriptKind
}

var libCache collections.SyncMap[libCacheKey, *ast.SourceFile]

func (h *libCachingHost) GetSourceFile(opts ast.SourceFileParseOptions) *ast.SourceFile {
	if !strings.HasPrefix(opts.FileName, tspath.EnsureTrailingDirectorySeparator(bundled.LibPath())) {
		return h.CompilerHost.GetSourceFile(opts)
	}
	text, ok := h.FS().ReadFile(opts.FileName)
	if !ok {
		return nil
	}
	scriptKind := core.EnsureScriptKindFromFileName(opts.FileName)
	key := libCacheKey{opts: opts, text: text, scriptKind: scriptKind}
	if cached, ok := libCache.Load(key); ok {
		return cached
	}
	sourceFile := parser.ParseSourceFile(opts, text, scriptKind)
	result, _ := libCache.LoadOrStore(key, sourceFile)
	return result
}
