package storecompiler

import (
	"github.com/microsoft/TypeScript/tsc/internal/ast"
	"github.com/microsoft/TypeScript/tsc/internal/ast/store"
	"github.com/microsoft/TypeScript/tsc/internal/core"
	"github.com/microsoft/TypeScript/tsc/internal/diagnostics"
	"github.com/microsoft/TypeScript/tsc/internal/storeparser"
	"github.com/microsoft/TypeScript/tsc/internal/tsoptions"
	"github.com/microsoft/TypeScript/tsc/internal/tspath"
	"github.com/microsoft/TypeScript/tsc/internal/vfs"
	"github.com/microsoft/TypeScript/tsc/internal/vfs/cachedvfs"
)

// The P2 declarations of compiler/host.go: the Store host parses with
// storeparser into the program's IDSpace and has no content mapper
// (store-cli-design-20261004.md 4.1).

var _ CompilerHost = (*compilerHost)(nil)

// storetranslate:pointer-hash c3d3e7850c25337f host.go:compilerHost
type compilerHost struct {
	currentDirectory    string
	fs                  vfs.FS
	defaultLibraryPath  string
	extendedConfigCache tsoptions.ExtendedConfigCache
	trace               func(msg *diagnostics.Message, args ...any)
}

// storetranslate:pointer-hash 92d0f26997dafef1 host.go:NewCachedFSCompilerHost
func NewCachedFSCompilerHost(
	currentDirectory string,
	fs vfs.FS,
	defaultLibraryPath string,
	extendedConfigCache tsoptions.ExtendedConfigCache,
	trace func(msg *diagnostics.Message, args ...any),
) CompilerHost {
	return NewCompilerHost(currentDirectory, cachedvfs.From(fs), defaultLibraryPath, extendedConfigCache, trace)
}

// storetranslate:pointer-hash d8d329e70232a485 host.go:NewCompilerHost
func NewCompilerHost(
	currentDirectory string,
	fs vfs.FS,
	defaultLibraryPath string,
	extendedConfigCache tsoptions.ExtendedConfigCache,
	trace func(msg *diagnostics.Message, args ...any),
) CompilerHost {
	if trace == nil {
		trace = func(msg *diagnostics.Message, args ...any) {}
	}
	return &compilerHost{
		currentDirectory:    currentDirectory,
		fs:                  fs,
		defaultLibraryPath:  defaultLibraryPath,
		extendedConfigCache: extendedConfigCache,
		trace:               trace,
	}
}

// storetranslate:pointer-hash 86be46a5858fb87a host.go:compilerHost.FS
func (h *compilerHost) FS() vfs.FS {
	return h.fs
}

// storetranslate:pointer-hash 0a10dd355da8aadf host.go:compilerHost.DefaultLibraryPath
func (h *compilerHost) DefaultLibraryPath() string {
	return h.defaultLibraryPath
}

// storetranslate:pointer-hash 1b3f47f9a78d6f85 host.go:compilerHost.GetCurrentDirectory
func (h *compilerHost) GetCurrentDirectory() string {
	return h.currentDirectory
}

// storetranslate:pointer-hash 3fe6651e527f96c4 host.go:compilerHost.Trace
func (h *compilerHost) Trace(msg *diagnostics.Message, args ...any) {
	h.trace(msg, args...)
}

// GetSourceFile numbers the file in space after the text is read, just
// before the parse: a file that cannot be read takes no StoreID (4.3).
//
// storetranslate:pointer-hash d6162476bf3ec2d7 host.go:compilerHost.GetSourceFile
func (h *compilerHost) GetSourceFile(opts ast.SourceFileParseOptions, space *store.IDSpace) *store.File {
	text, ok := h.FS().ReadFile(opts.FileName)
	if !ok {
		return nil
	}
	return storeparser.ParseSourceFileIn(space, space.Next(), opts, text, core.EnsureScriptKindFromFileName(opts.FileName))
}

// storetranslate:pointer-hash ba8df77f502ddae7 host.go:compilerHost.GetResolvedProjectReference
func (h *compilerHost) GetResolvedProjectReference(fileName string, path tspath.Path) *tsoptions.ParsedCommandLine {
	commandLine, _ := tsoptions.GetParsedCommandLineOfConfigFilePath(fileName, path, nil, nil /*optionsRaw*/, h, h.extendedConfigCache)
	return commandLine
}
