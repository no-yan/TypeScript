package storeharness

import (
	"github.com/microsoft/TypeScript/tsc/internal/ast"
	"github.com/microsoft/TypeScript/tsc/internal/ast/store"
	"github.com/microsoft/TypeScript/tsc/internal/compiler"
	"github.com/microsoft/TypeScript/tsc/internal/core"
	"github.com/microsoft/TypeScript/tsc/internal/module"
	"github.com/microsoft/TypeScript/tsc/internal/packagejson"
	"github.com/microsoft/TypeScript/tsc/internal/storebinder"
	"github.com/microsoft/TypeScript/tsc/internal/storechecker"
	"github.com/microsoft/TypeScript/tsc/internal/symlinks"
	"github.com/microsoft/TypeScript/tsc/internal/tsoptions"
	"github.com/microsoft/TypeScript/tsc/internal/tspath"
)

// Program is storechecker.Program on a Pointer program (5.2): the methods
// that take or give no node are the Pointer program's (A), the files are the
// Store files of the same paths (B), the three that read a module specifier
// are written by hand in mode.go (C), and the synthetic import specifiers are
// the Store's own, made as the Pointer program has them.
type Program struct {
	pointer        *compiler.Program
	space          *store.IDSpace
	files          []*store.File // in the order of the Pointer program's SourceFiles
	byPath         map[tspath.Path]*store.File
	pointers       map[tspath.Path]*ast.SourceFile
	helperImports  map[tspath.Path]store.Node // the synthetic import specifiers, by file
	jsxImports     map[tspath.Path]store.Node
	syntheticAsked int
}

var _ storechecker.Program = (*Program)(nil)

// Store is the Store file of a Pointer source file of the program, or nil.
func (p *Program) Store(file *ast.SourceFile) *store.File {
	if file == nil {
		return nil
	}
	return p.byPath[file.Path()]
}

// B: the files, mapped by path.

func (p *Program) SourceFiles() []*store.File { return p.files }

func (p *Program) GetSourceFile(fileName string) *store.File {
	return p.Store(p.pointer.GetSourceFile(fileName))
}

// GetSourceFileForResolvedModule is the Pointer program's, the redirect
// included, mapped by path.
func (p *Program) GetSourceFileForResolvedModule(fileName string) *store.File {
	return p.Store(p.pointer.GetSourceFileForResolvedModule(fileName))
}

func (p *Program) SourceFileMayBeEmitted(sourceFile *store.File, forceDtsEmit bool) bool {
	return p.pointer.SourceFileMayBeEmitted(p.pointers[sourceFile.Path()], forceDtsEmit)
}

// The synthetic import specifiers: newProgram makes them, as the Pointer
// program has them (store.NewSyntheticImport). syntheticAsked counts the
// specifiers given to the checker.

func (p *Program) GetJSXRuntimeImportSpecifier(path tspath.Path) (moduleReference string, specifier store.Node) {
	moduleReference, _ = p.pointer.GetJSXRuntimeImportSpecifier(path)
	specifier = p.jsxImports[path]
	if !specifier.IsNil() {
		p.syntheticAsked++
	}
	return moduleReference, specifier
}

func (p *Program) GetImportHelpersImportSpecifier(path tspath.Path) store.Node {
	specifier := p.helperImports[path]
	if !specifier.IsNil() {
		p.syntheticAsked++
	}
	return specifier
}

// BindSourceFiles binds every Store file; a file binds once.
func (p *Program) BindSourceFiles() {
	for _, f := range p.files {
		storebinder.BindSourceFile(f)
	}
}

func (p *Program) IDSpace() *store.IDSpace { return p.space }

// A: the Pointer program's. A *store.File is an ast.HasFileName; the Pointer
// implementations read its FileName and Path only.

func (p *Program) Options() *core.CompilerOptions  { return p.pointer.Options() }
func (p *Program) FileExists(fileName string) bool { return p.pointer.FileExists(fileName) }
func (p *Program) GetEmitModuleFormatOfFile(sourceFile ast.HasFileName) core.ModuleKind {
	return p.pointer.GetEmitModuleFormatOfFile(sourceFile)
}

func (p *Program) GetImpliedNodeFormatForEmit(sourceFile ast.HasFileName) core.ModuleKind {
	return p.pointer.GetImpliedNodeFormatForEmit(sourceFile)
}

func (p *Program) GetResolvedModule(currentSourceFile ast.HasFileName, moduleReference string, mode core.ResolutionMode) *module.ResolvedModule {
	return p.pointer.GetResolvedModule(currentSourceFile, moduleReference, mode)
}

func (p *Program) GetResolvedModules() map[tspath.Path]module.ModeAwareCache[*module.ResolvedModule] {
	return p.pointer.GetResolvedModules()
}
func (p *Program) GetPackagesMap() map[string]bool { return p.pointer.GetPackagesMap() }
func (p *Program) GetSourceFileMetaData(path tspath.Path) ast.SourceFileMetaData {
	return p.pointer.GetSourceFileMetaData(path)
}

func (p *Program) IsSourceFileDefaultLibrary(path tspath.Path) bool {
	return p.pointer.IsSourceFileDefaultLibrary(path)
}

func (p *Program) GetProjectReferenceFromOutputDts(path tspath.Path) *tsoptions.SourceOutputAndProjectReference {
	return p.pointer.GetProjectReferenceFromOutputDts(path)
}

func (p *Program) GetRedirectForResolution(file ast.HasFileName) *tsoptions.ParsedCommandLine {
	return p.pointer.GetRedirectForResolution(file)
}
func (p *Program) CommonSourceDirectory() string { return p.pointer.CommonSourceDirectory() }

// A: the module specifier generation host.

func (p *Program) GetSymlinkCache() *symlinks.KnownSymlinks { return p.pointer.GetSymlinkCache() }
func (p *Program) ContentMapperExtensions() []string        { return p.pointer.ContentMapperExtensions() }
func (p *Program) GetGlobalTypingsCacheLocation() string {
	return p.pointer.GetGlobalTypingsCacheLocation()
}
func (p *Program) UseCaseSensitiveFileNames() bool { return p.pointer.UseCaseSensitiveFileNames() }
func (p *Program) GetCurrentDirectory() string     { return p.pointer.GetCurrentDirectory() }
func (p *Program) GetProjectReferenceFromSource(path tspath.Path) *tsoptions.SourceOutputAndProjectReference {
	return p.pointer.GetProjectReferenceFromSource(path)
}
func (p *Program) GetRedirectTargets(path tspath.Path) []string {
	return p.pointer.GetRedirectTargets(path)
}
func (p *Program) GetSourceOfProjectReferenceIfOutputIncluded(file ast.HasFileName) string {
	return p.pointer.GetSourceOfProjectReferenceIfOutputIncluded(file)
}

func (p *Program) GetNearestAncestorDirectoryWithPackageJson(dirname string) string {
	return p.pointer.GetNearestAncestorDirectoryWithPackageJson(dirname)
}

func (p *Program) GetPackageJsonInfo(pkgJsonPath string) *packagejson.InfoCacheEntry {
	return p.pointer.GetPackageJsonInfo(pkgJsonPath)
}

func (p *Program) GetDefaultResolutionModeForFile(file ast.HasFileName) core.ResolutionMode {
	return p.pointer.GetDefaultResolutionModeForFile(file)
}
