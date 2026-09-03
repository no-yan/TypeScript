package autoimport

import (
	"context"
	"github.com/microsoft/TypeScript/tsc/internal/ast"
	"github.com/microsoft/TypeScript/tsc/internal/checker"
	"github.com/microsoft/TypeScript/tsc/internal/collections"
	"github.com/microsoft/TypeScript/tsc/internal/compiler"
	"github.com/microsoft/TypeScript/tsc/internal/core"
	"github.com/microsoft/TypeScript/tsc/internal/module"
	"github.com/microsoft/TypeScript/tsc/internal/modulespecifiers"
	"github.com/microsoft/TypeScript/tsc/internal/packagejson"
	"github.com/microsoft/TypeScript/tsc/internal/tspath"
	"github.com/microsoft/TypeScript/tsc/internal/vfs"
	"github.com/microsoft/TypeScript/tsc/internal/vfs/wrapvfs"
	"runtime"
	"strings"
	"sync/atomic"
	"unicode"
	"unicode/utf8"
)

func tryGetModuleIDAndFileNameOfModuleSymbol(symbol *ast.Symbol) (ModuleID, string, bool) {
	if !symbol.IsExternalModule() {
		return "", "", false
	}
	decl := ast.GetNonAugmentationDeclaration(symbol)
	if decl.IsNil() {
		return "", "", false
	}
	if decl.Kind == ast.KindSourceFile {
		sf := ast.GetSourceFileOfNode(decl)
		return ModuleID(sf.Path()), sf.FileName(), true
	}
	if ast.IsModuleWithStringLiteralName(decl) {
		return ModuleID(decl.Name().Text()), "", true
	}
	return "", "", false
}
func getModuleIDAndFileNameOfModuleSymbol(symbol *ast.Symbol) (ModuleID, string) {
	if !symbol.IsExternalModule() {
		panic("symbol is not an external module")
	}
	decl := ast.GetNonAugmentationDeclaration(symbol)
	if decl.IsNil() {
		panic("module symbol has no non-augmentation declaration")
	}
	if decl.Kind == ast.KindSourceFile {
		sf := ast.GetSourceFileOfNode(decl)
		return ModuleID(sf.Path()), sf.FileName()
	}
	if ast.IsModuleWithStringLiteralName(decl) {
		return ModuleID(decl.Name().Text()), ""
	}
	panic("could not determine module ID of module symbol")
}

func wordIndices(s string) []int {
	var indices []int
	for byteIndex, runeValue := range s {
		if byteIndex == 0 {
			indices = append(indices, byteIndex)
			continue
		}
		if runeValue == '_' {
			if byteIndex+1 < len(s) && s[byteIndex+1] != '_' {
				indices = append(indices, byteIndex+1)
			}
			continue
		}
		if unicode.IsUpper(runeValue) && (unicode.IsLower(core.FirstResult(utf8.DecodeLastRuneInString(s[:byteIndex]))) || (byteIndex+1 < len(s) && unicode.IsLower(core.FirstResult(utf8.DecodeRuneInString(s[byteIndex+1:]))))) {
			indices = append(indices, byteIndex)
		}
	}
	return indices
}
func getPackageNamesInNodeModules(nodeModulesDir string, fs vfs.FS) *collections.Set[string] {
	packageNames := &collections.Set[string]{}
	if tspath.GetBaseFileName(nodeModulesDir) != "node_modules" {
		panic("nodeModulesDir is not a node_modules directory")
	}
	entries := fs.GetAccessibleEntries(nodeModulesDir)
	for _, baseName := range entries.Directories {
		if baseName[0] == '.' {
			continue
		}
		if baseName[0] == '@' {
			scopedDirPath := tspath.CombinePaths(nodeModulesDir, baseName)
			for _, scopedPackageDirName := range fs.GetAccessibleEntries(scopedDirPath).Directories {
				scopedBaseName := tspath.GetBaseFileName(scopedPackageDirName)
				if baseName == "@types" {
					packageNames.Add(module.GetPackageNameFromTypesPackageName(tspath.CombinePaths("@types", scopedBaseName)))
				} else {
					packageNames.Add(tspath.CombinePaths(baseName, scopedBaseName))
				}
			}
			continue
		}
		packageNames.Add(baseName)
	}
	return packageNames
}
func getDefaultLikeExportNameFromDeclaration(symbol *ast.Symbol) string {
	for _, d := range ast.DeclarationNodes(symbol) {
		if ast.IsExportAssignment(d) {
			if innerExpression := ast.SkipOuterExpressions(d.Expression(), ast.OEKAll); ast.IsIdentifier(innerExpression) {
				return innerExpression.Text()
			}
			continue
		}
		if ast.IsExportSpecifier(d) && d.Symbol().Flags == ast.SymbolFlagsAlias && !d.PropertyName().IsNil() {
			if d.PropertyName().Kind == ast.KindIdentifier {
				return d.PropertyName().Text()
			}
			continue
		}
		if name := ast.GetNameOfDeclaration(d); !name.IsNil() && name.Kind == ast.KindIdentifier {
			return name.Text()
		}
		if symbol.Parent != nil && !checker.IsExternalModuleSymbol(symbol.Parent) {
			return symbol.Parent.Name
		}
	}
	return ""
}
func getResolvedPackageNames(ctx context.Context, program *compiler.Program) *collections.Set[string] {
	rawNames := program.ResolvedPackageNames()
	unresolvedPackageNames := program.UnresolvedPackageNames()
	resolvedPackageNames := collections.NewSetWithSizeHint[string](rawNames.Len())
	for name := range rawNames.Keys() {
		resolvedPackageNames.Add(module.GetPackageNameFromTypesPackageName(name))
	}
	for _, name := range program.Options().Types {
		if name != "*" {
			resolvedPackageNames.Add(module.GetPackageNameFromTypesPackageName(name))
		}
	}
	if unresolvedPackageNames.Len() > 0 {
		checker, done := program.GetTypeChecker(ctx)
		defer done()
		for name := range unresolvedPackageNames.Keys() {
			if symbol := checker.TryFindAmbientModule(name); symbol != nil {
				declaringFile := ast.GetSourceFileOfModule(symbol)
				if packageName := modulespecifiers.GetPackageNameFromDirectory(declaringFile.FileName()); packageName != "" {
					resolvedPackageNames.Add(module.GetPackageNameFromTypesPackageName(packageName))
				}
			}
		}
	}
	return resolvedPackageNames
}

func addProjectReferenceOutputMappings(program *compiler.Program, result map[tspath.Path]string) {
	refs := program.GetResolvedProjectReferences()
	for _, ref := range refs {
		if ref == nil {
			continue
		}
		ref.ParseInputOutputNames()
		for outputDtsPath, mapping := range ref.OutputDtsToProjectReference() {
			if _, exists := result[outputDtsPath]; !exists {
				result[outputDtsPath] = mapping.Source
			}
		}
	}
}
func createCheckerPool(program checker.Program) (getChecker func() (*checker.Checker, func()), closePool func(), getCreatedCount func() int32) {
	maxSize := int32(runtime.GOMAXPROCS(0))
	pool := make(chan *checker.Checker, maxSize)
	var created atomic.Int32
	return func() (*checker.Checker, func()) {
			select {
			case ch := <-pool:
				return ch, func() {
					pool <- ch
				}
			default:
				break
			}
			for {
				current := created.Load()
				if current >= maxSize {
					ch := <-pool
					return ch, func() {
						pool <- ch
					}
				}
				if created.CompareAndSwap(current, current+1) {
					ch := core.FirstResult(checker.NewChecker(program, nil))
					return ch, func() {
						pool <- ch
					}
				}
			}
		}, func() {
			close(pool)
		}, func() int32 {
			return created.Load()
		}
}

func addPackageJsonDependencies(contents *packagejson.PackageJson, deps *collections.Set[string]) {
	contents.RangeDependencies(func(name, _, field string) bool {
		if name == "" || name == "@types/" || name[0] == '.' {
			return true
		}
		if field == "dependencies" || field == "peerDependencies" {
			deps.Add(module.GetPackageNameFromTypesPackageName(name))
		}
		return true
	})
}

func getPackageRealpathFuncs(fs vfs.FS, packageDir string) (toRealpath, toSymlink func(string) string) {
	realPackageDir := fs.Realpath(packageDir)
	isSymlinked := realPackageDir != packageDir
	dirCache := make(map[string]string)
	toRealpath = func(fileName string) string {
		if isSymlinked {
			if after, ok := strings.CutPrefix(fileName, packageDir); ok {
				return realPackageDir + after
			}
		}
		pkgDir := module.ParseNodeModuleFromPath(fileName, false)
		if pkgDir == "" {
			return fileName
		}
		if realDir, ok := dirCache[pkgDir]; ok {
			if realDir == pkgDir {
				return fileName
			}
			return realDir + fileName[len(pkgDir):]
		}
		realDir := fs.Realpath(pkgDir)
		dirCache[pkgDir] = realDir
		if realDir == pkgDir {
			return fileName
		}
		return realDir + fileName[len(pkgDir):]
	}
	if !isSymlinked {
		return toRealpath, core.Identity
	}
	toSymlink = func(fileName string) string {
		if after, ok := strings.CutPrefix(fileName, realPackageDir); ok {
			return packageDir + after
		}
		return fileName
	}
	return toRealpath, toSymlink
}

type resolutionHost struct {
	fs               vfs.FS
	currentDirectory string
}

var _ module.ResolutionHost = (*resolutionHost)(nil)

func (rh *resolutionHost) GetCurrentDirectory() string {
	return rh.currentDirectory
}
func (rh *resolutionHost) FS() vfs.FS {
	return rh.fs
}
func getModuleResolver(host RegistryCloneHost, realpath func(string) string, opts module.ResolverOptions) *module.Resolver {
	rh := &resolutionHost{fs: wrapvfs.Wrap(host.FS(), wrapvfs.Replacements{Realpath: realpath}), currentDirectory: host.GetCurrentDirectory()}
	return module.NewResolverWithOptions(rh, core.EmptyCompilerOptions, "", "", opts)
}
