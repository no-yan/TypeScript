package autoimport

import (
	"github.com/microsoft/TypeScript/tsc/internal/ast"
	"github.com/microsoft/TypeScript/tsc/internal/binder"
	"github.com/microsoft/TypeScript/tsc/internal/checker"
	"github.com/microsoft/TypeScript/tsc/internal/collections"
	"github.com/microsoft/TypeScript/tsc/internal/core"
	"github.com/microsoft/TypeScript/tsc/internal/module"
	"github.com/microsoft/TypeScript/tsc/internal/packagejson"
	"github.com/microsoft/TypeScript/tsc/internal/symlinks"
	"github.com/microsoft/TypeScript/tsc/internal/tsoptions"
	"github.com/microsoft/TypeScript/tsc/internal/tspath"
)

type pathAndFileName struct {
	path     tspath.Path
	fileName string
}
type aliasResolver struct {
	toPath         func(fileName string) tspath.Path
	host           RegistryCloneHost
	moduleResolver *module.Resolver
	rootFiles      []*// symlinks maps from realpath to symlinked path and file name
	// BindSourceFiles implements checker.Program.
	// We will bind as we parse
	// SourceFiles implements checker.Program.
	// Options implements checker.Program.
	// GetCurrentDirectory implements checker.Program.
	// UseCaseSensitiveFileNames implements checker.Program.
	// GetSourceFile implements checker.Program.
	// file may be nil due to symlink/realpath mismatch; see TestAutoImportBuilderFS
	// GetDefaultResolutionModeForFile implements checker.Program.
	// GetEmitModuleFormatOfFile implements checker.Program.
	// GetEmitSyntaxForUsageLocation implements checker.Program.
	// GetImpliedNodeFormatForEmit implements checker.Program.
	// GetModeForUsageLocation implements checker.Program.
	// GetResolvedModule implements checker.Program.
	// GetSourceFileForResolvedModule implements checker.Program.
	// GetResolvedModules implements checker.Program.
	// only used when producing diagnostics, which hopefully the checker won't do
	// ---
	// GetSymlinkCache implements checker.Program.
	// GetSourceFileMetaData implements checker.Program.
	// CommonSourceDirectory implements checker.Program.
	// ContentMapperExtensions implements checker.Program.
	// FileExists implements checker.Program.
	// GetGlobalTypingsCacheLocation implements checker.Program.
	// GetImportHelpersImportSpecifier implements checker.Program.
	// GetJSXRuntimeImportSpecifier implements checker.Program.
	// GetNearestAncestorDirectoryWithPackageJson implements checker.Program.
	// GetPackageJsonInfo implements checker.Program.
	// GetProjectReferenceFromOutputDts implements checker.Program.
	// GetProjectReferenceFromSource implements checker.Program.
	// GetRedirectForResolution implements checker.Program.
	// GetRedirectTargets implements checker.Program.
	// GetResolvedModuleFromModuleSpecifier implements checker.Program.
	// GetSourceOfProjectReferenceIfOutputIncluded implements checker.Program.
	// IsSourceFileDefaultLibrary implements checker.Program.
	// IsSourceFromProjectReference implements checker.Program.
	// SourceFileMayBeEmitted implements checker.Program.
	ast.SourceFile
	symlinks                    map[tspath.Path]pathAndFileName
	onFailedAmbientModuleLookup func(source ast.HasFileName, moduleName string)
	resolvedModules             collections.SyncMap[tspath.Path, *collections.SyncMap[module.ModeAwareCacheKey, *module.ResolvedModule]]
}

func newAliasResolver(rootFiles []*ast.SourceFile, symlinks map[tspath.Path]pathAndFileName, host RegistryCloneHost, moduleResolver *module.Resolver, toPath func(fileName string) tspath.Path, onFailedAmbientModuleLookup func(source ast.HasFileName, moduleName string)) *aliasResolver {
	r := &aliasResolver{toPath: toPath, host: host, moduleResolver: moduleResolver, rootFiles: rootFiles, symlinks: symlinks, onFailedAmbientModuleLookup: onFailedAmbientModuleLookup}
	return r
}

func (r *aliasResolver) BindSourceFiles() {
}

func (r *aliasResolver) SourceFiles() []*ast.SourceFile {
	return r.rootFiles
}

func (r *aliasResolver) Options() *core.CompilerOptions {
	return &core.CompilerOptions{NoCheck: core.TSTrue}
}

func (r *aliasResolver) GetCurrentDirectory() string {
	return r.host.GetCurrentDirectory()
}

func (r *aliasResolver) UseCaseSensitiveFileNames() bool {
	return r.host.FS().UseCaseSensitiveFileNames()
}

func (r *aliasResolver) GetSourceFile(fileName string) *ast.SourceFile {
	file := r.host.GetSourceFile(fileName, r.toPath(fileName))
	if file == nil {
		return nil
	}
	binder.BindSourceFile(file)
	return file
}

func (r *aliasResolver) GetDefaultResolutionModeForFile(file ast.HasFileName) core.ResolutionMode {
	return core.ModuleKindESNext
}

func (r *aliasResolver) GetEmitModuleFormatOfFile(sourceFile ast.HasFileName) core.ModuleKind {
	return core.ModuleKindESNext
}

func (r *aliasResolver) GetEmitSyntaxForUsageLocation(sourceFile ast.HasFileName, usageLocation ast.Handle) core.ResolutionMode {
	return core.ModuleKindESNext
}

func (r *aliasResolver) GetImpliedNodeFormatForEmit(sourceFile ast.HasFileName) core.ModuleKind {
	return core.ModuleKindESNext
}

func (r *aliasResolver) GetModeForUsageLocation(file ast.HasFileName, moduleSpecifier ast.Handle) core.ResolutionMode {
	return core.ModuleKindESNext
}

func (r *aliasResolver) GetResolvedModule(currentSourceFile ast.HasFileName, moduleReference string, mode core.ResolutionMode) *module.ResolvedModule {
	cache, _ := r.resolvedModules.LoadOrStore(currentSourceFile.Path(), &collections.SyncMap[module.ModeAwareCacheKey, *module.ResolvedModule]{})
	if resolved, ok := cache.Load(module.ModeAwareCacheKey{Name: moduleReference, Mode: mode}); ok {
		return resolved
	}
	resolved, _ := r.moduleResolver.ResolveModuleName(moduleReference, currentSourceFile.FileName(), mode, nil)
	resolved, _ = cache.LoadOrStore(module.ModeAwareCacheKey{Name: moduleReference, Mode: mode}, resolved)
	if !resolved.IsResolved() && !tspath.PathIsRelative(moduleReference) {
		r.onFailedAmbientModuleLookup(currentSourceFile, moduleReference)
	}
	return resolved
}

func (r *aliasResolver) GetSourceFileForResolvedModule(fileName string) *ast.SourceFile {
	return r.GetSourceFile(fileName)
}

func (r *aliasResolver) GetResolvedModules() map[tspath.Path]module.ModeAwareCache[*module.ResolvedModule] {
	return nil
}

func (r *aliasResolver) GetSymlinkCache() *symlinks.KnownSymlinks {
	panic("unimplemented")
}

func (r *aliasResolver) GetSourceFileMetaData(path tspath.Path) ast.SourceFileMetaData {
	panic("unimplemented")
}

func (r *aliasResolver) CommonSourceDirectory() string {
	panic("unimplemented")
}

func (r *aliasResolver) ContentMapperExtensions() []string {
	return nil
}

func (r *aliasResolver) FileExists(fileName string) bool {
	panic("unimplemented")
}

func (r *aliasResolver) GetGlobalTypingsCacheLocation() string {
	panic("unimplemented")
}

func (r *aliasResolver) GetImportHelpersImportSpecifier(path tspath.Path) ast.Handle {
	panic("unimplemented")
}

func (r *aliasResolver) GetJSXRuntimeImportSpecifier(path tspath.Path) (moduleReference string, specifier ast.Handle) {
	panic("unimplemented")
}

func (r *aliasResolver) GetNearestAncestorDirectoryWithPackageJson(dirname string) string {
	panic("unimplemented")
}

func (r *aliasResolver) GetPackageJsonInfo(pkgJsonPath string) *packagejson.InfoCacheEntry {
	panic("unimplemented")
}

func (r *aliasResolver) GetProjectReferenceFromOutputDts(path tspath.Path) *tsoptions.SourceOutputAndProjectReference {
	panic("unimplemented")
}

func (r *aliasResolver) GetProjectReferenceFromSource(path tspath.Path) *tsoptions.SourceOutputAndProjectReference {
	panic("unimplemented")
}

func (r *aliasResolver) GetRedirectForResolution(file ast.HasFileName) *tsoptions.ParsedCommandLine {
	panic("unimplemented")
}

func (r *aliasResolver) GetRedirectTargets(path tspath.Path) []string {
	panic("unimplemented")
}

func (r *aliasResolver) GetResolvedModuleFromModuleSpecifier(file ast.HasFileName, moduleSpecifier ast.Handle) *module.ResolvedModule {
	panic("unimplemented")
}

func (r *aliasResolver) GetSourceOfProjectReferenceIfOutputIncluded(file ast.HasFileName) string {
	panic("unimplemented")
}

func (r *aliasResolver) IsSourceFileDefaultLibrary(path tspath.Path) bool {
	return false
}

func (r *aliasResolver) IsSourceFromProjectReference(path tspath.Path) bool {
	panic("unimplemented")
}

func (r *aliasResolver) SourceFileMayBeEmitted(sourceFile *ast.SourceFile, forceDtsEmit bool) bool {
	panic("unimplemented")
}
func (r *aliasResolver) GetPackagesMap() map[string]bool {
	return nil
}

var _ checker.Program = (*aliasResolver)(nil)
