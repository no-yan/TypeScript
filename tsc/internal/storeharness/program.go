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
	fs := bundled.WrapFS(vfstest.FromMap(files, tspath.CaseSensitive))
	host := &libCachingHost{CompilerHost: compiler.NewCompilerHost(fs, bundled.LibPath(), nil, nil, nil)}
	config := tsoptions.NewParsedCommandLine(options, rootedFileNames(slices.Sorted(maps.Keys(files))), nil, "/", tspath.CaseSensitive)
	return compiler.NewProgram(compiler.ProgramOptions{Config: config, Host: host, SingleThreaded: core.TSTrue})
}

// newProgram parses every source file of the Pointer program again into the
// Store, in the program's order, each with its StoreID in the program's
// IDSpace, and makes the synthetic imports the Pointer program has. The Store files are the program's own, the libs included: a Store
// belongs to one IDSpace, and a Factory refuses the nodes of another
// (idspace.go, factory.go), so they are not shared between programs.
func newProgram(pointer *compiler.Program) *Program {
	sources := pointer.SourceFiles()
	p := &Program{
		pointer:  pointer,
		space:    store.NewIDSpace(len(sources)),
		files:    make([]*store.File, len(sources)),
		byPath:   make(map[tspath.PathKey]*store.File, len(sources)),
		pointers: make(map[tspath.PathKey]*ast.SourceFile, len(sources)),
	}
	for i, pf := range sources {
		f := storeparser.ParseSourceFileIn(p.space, store.StoreID(i), pf.ParseOptions(), pf.Text(), pf.ScriptKind)
		f.SetContentMapper(pf.ContentMapper())
		p.files[i] = f
		p.byPath[f.PathKey()] = f
		p.pointers[f.PathKey()] = pf
	}
	// The synthetic imports of each file the Pointer program made them for,
	// the import helpers' and then the JSX runtime's, as its fileloader makes
	// them (resolveImportsAndModuleAugmentations).
	p.helperImports = map[tspath.PathKey]store.Node{}
	p.jsxImports = map[tspath.PathKey]store.Node{}
	for i, pf := range sources {
		if specifier := pointer.GetImportHelpersImportSpecifier(pf.PathKey()); specifier != nil {
			p.helperImports[pf.PathKey()] = store.NewSyntheticImport(p.files[i], specifier.Text())
		}
		if _, specifier := pointer.GetJSXRuntimeImportSpecifier(pf.PathKey()); specifier != nil {
			p.jsxImports[pf.PathKey()] = store.NewSyntheticImport(p.files[i], specifier.Text())
		}
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
	if !isBundledLib(opts.FileName) {
		return h.CompilerHost.GetSourceFile(opts)
	}
	text, ok := h.FS().ReadFile(opts.FileName)
	if !ok {
		return nil
	}
	return cachedLib(opts, text, core.EnsureScriptKindFromFileName(opts.FileName))
}

func isBundledLib(fileName tspath.RootedFilePath) bool {
	return strings.HasPrefix(fileName.AsString(), tspath.EnsureTrailingDirectorySeparator(bundled.LibPath().AsString()))
}

// rootedFileNames are the absolute file names of a program's files map.
func rootedFileNames(names []string) []tspath.RootedFilePath {
	out := make([]tspath.RootedFilePath, len(names))
	for i, name := range names {
		out[i] = tspath.RootedFilePathFromAbsolute(name)
	}
	return out
}

// cachedLib is the Pointer source file of a bundled lib, parsed once for the
// programs of every test.
func cachedLib(opts ast.SourceFileParseOptions, text string, scriptKind core.ScriptKind) *ast.SourceFile {
	key := libCacheKey{opts: opts, text: text, scriptKind: scriptKind}
	if cached, ok := libCache.Load(key); ok {
		return cached
	}
	sourceFile := parser.ParseSourceFile(opts, text, scriptKind)
	result, _ := libCache.LoadOrStore(key, sourceFile)
	return result
}
