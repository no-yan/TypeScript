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

// storetranslate:pointer-hash 8fe83a19a62843fd host.go:compilerHost
type compilerHost struct {
	fs                  vfs.FS
	defaultLibraryPath  tspath.RootedDirectoryPath
	extendedConfigCache tsoptions.ExtendedConfigCache
	trace               func(msg *diagnostics.Message, args ...any)
}

// storetranslate:pointer-hash 9dcc8996bdd81e3b host.go:NewCachedFSCompilerHost
func NewCachedFSCompilerHost(
	fs vfs.FS,
	defaultLibraryPath tspath.RootedDirectoryPath,
	extendedConfigCache tsoptions.ExtendedConfigCache,
	trace func(msg *diagnostics.Message, args ...any),
) CompilerHost {
	return NewCompilerHost(cachedvfs.From(fs), defaultLibraryPath, extendedConfigCache, trace)
}

// storetranslate:pointer-hash 834295d5d9f24fa8 host.go:NewCompilerHost
func NewCompilerHost(
	fs vfs.FS,
	defaultLibraryPath tspath.RootedDirectoryPath,
	extendedConfigCache tsoptions.ExtendedConfigCache,
	trace func(msg *diagnostics.Message, args ...any),
) CompilerHost {
	if trace == nil {
		trace = func(msg *diagnostics.Message, args ...any) {}
	}
	return &compilerHost{
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

// storetranslate:pointer-hash 1a9788c784488dc6 host.go:compilerHost.DefaultLibraryPath
func (h *compilerHost) DefaultLibraryPath() tspath.RootedDirectoryPath {
	return h.defaultLibraryPath
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

// storetranslate:pointer-hash 2eec6b6695dbe9eb host.go:compilerHost.GetResolvedProjectReference
func (h *compilerHost) GetResolvedProjectReference(fileName tspath.RootedFilePath, path tspath.PathKey) *tsoptions.ParsedCommandLine {
	commandLine, _ := tsoptions.GetParsedCommandLineOfConfigFilePath(fileName, path, nil, nil /*optionsRaw*/, h.fs, h.extendedConfigCache)
	return commandLine
}
