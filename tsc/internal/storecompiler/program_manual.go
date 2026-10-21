package storecompiler

import (
	"strings"
	"sync"

	"github.com/microsoft/TypeScript/tsc/internal/ast"
	"github.com/microsoft/TypeScript/tsc/internal/ast/store"
	"github.com/microsoft/TypeScript/tsc/internal/collections"
	"github.com/microsoft/TypeScript/tsc/internal/core"
	"github.com/microsoft/TypeScript/tsc/internal/module"
	"github.com/microsoft/TypeScript/tsc/internal/symlinks"
	"github.com/microsoft/TypeScript/tsc/internal/tracing"
	"github.com/microsoft/TypeScript/tsc/internal/tspath"
)

// The P2 declarations of compiler/program.go: the Store program of a native
// check (store-cli-design-20261004.md 4.1). Its methods are translated from
// compiler.Program's; this type has the fields they read: the compiler's
// checker pool, and no project system pool, emit or incremental state.

// Program owns its IDSpace (opts.idSpace): every Store of the program, the
// source files included, takes its StoreID from it (4.3).
//
// storetranslate:pointer-hash 798860682ab0cdb2 program.go:Program
type Program struct {
	opts                  ProgramConfig
	hosts                 ProgramHosts
	resolutionData        *module.ResolutionData
	includeProcessor      includeProcessor
	moduleResolutionError error

	comparePathsOptions tspath.ComparePathsOptions

	processedFiles

	usesUriStyleNodeCoreModules core.Tristate

	commonSourceDirectory     string
	commonSourceDirectoryOnce sync.Once

	knownSymlinks lazyValue[symlinks.KnownSymlinks]

	// Cached map of package names to whether they bundle types
	packagesMapOnce sync.Once
	packagesMap     map[string]bool

	programDiagnostics []*store.Diagnostic

	// compilerCheckerPool is the compiler's pool (checkerpool.go): the CLI has
	// no project system pool (CheckerPool).
	compilerCheckerPool *checkerPool

	// The Store files of the Pointer config files, for the diagnostics that
	// point into them (configFile).
	configFilesMu sync.Mutex
	configFiles   map[*ast.SourceFile]*store.File
}

// NewProgram loads the program's files into a new IDSpace, makes the checker
// pool and verifies the options; the pool makes its checkers on the first ask.
//
// storetranslate:pointer-hash fb90e83929ba909a program.go:NewProgram
func NewProgram(opts ProgramOptions) *Program {
	opts.idSpace = store.NewIDSpace(0)
	p := &Program{opts: opts.ProgramConfig, hosts: opts.ProgramHosts}
	if opts.Tracing != nil {
		defer opts.Tracing.Push(tracing.PhaseProgram, "createProgram", map[string]any{"configFilePath": opts.Config.CompilerOptions().ConfigFilePath}, true)()
	}
	p.processedFiles, p.resolutionData, p.moduleResolutionError = processAllProgramFiles(opts, p.SingleThreaded())
	p.initCheckerPool()
	p.verifyCompilerOptions()
	return p
}

// GetSymlinkCache takes the resolutions of the Store files:
// symlinks.KnownSymlinks.SetSymlinksFromResolutions takes callbacks over
// *ast.SourceFile, so its two loops are written here.
//
// storetranslate:pointer-hash 363e4cdce60694ad program.go:Program.GetSymlinkCache
func (p *Program) GetSymlinkCache() *symlinks.KnownSymlinks {
	return p.knownSymlinks.getValue(func() *symlinks.KnownSymlinks {
		resolver := p.newResolver()
		knownSymlinks := symlinks.NewKnownSymlink(p.GetCurrentDirectory(), p.UseCaseSensitiveFileNames())

		// Resolved modules store realpath information when they're resolved inside node_modules
		if len(p.resolvedModules) > 0 || len(p.typeResolutionsInFile) > 0 {
			p.ForEachResolvedModule(func(resolution *module.ResolvedModule, moduleName string, mode core.ResolutionMode, filePath tspath.Path) {
				knownSymlinks.ProcessResolution(resolution.OriginalPath, resolution.ResolvedFileName)
			}, nil)
			p.ForEachResolvedTypeReferenceDirective(func(resolution *module.ResolvedTypeReferenceDirective, moduleName string, mode core.ResolutionMode, filePath tspath.Path) {
				knownSymlinks.ProcessResolution(resolution.OriginalPath, resolution.ResolvedFileName)
			}, nil)
		}

		// Check other dependencies for symlinks
		var seenPackageJsons collections.Set[tspath.Path]
		for filePath, meta := range p.sourceFileMetaDatas {
			if meta.PackageJsonDirectory == "" ||
				!p.SourceFileMayBeEmitted(p.GetSourceFileByPath(filePath), false) ||
				!seenPackageJsons.AddIfAbsent(p.toPath(meta.PackageJsonDirectory)) {
				continue
			}
			packageJsonName := tspath.CombinePaths(meta.PackageJsonDirectory, "package.json")
			info := p.GetPackageJsonInfo(packageJsonName)
			if info.GetContents() == nil {
				continue
			}

			for dep := range info.GetContents().GetRuntimeDependencyNames().Keys() {
				// Skip work in common case: we already saved a symlink for this package directory
				// in the node_modules adjacent to this package.json
				possibleDirectoryPath := p.toPath(tspath.CombinePaths(meta.PackageJsonDirectory, "node_modules", dep))
				if knownSymlinks.HasDirectory(possibleDirectoryPath) {
					continue
				}
				if !strings.HasPrefix(dep, "@types") {
					possibleTypesDirectoryPath := p.toPath(tspath.CombinePaths(meta.PackageJsonDirectory, "node_modules", module.GetTypesPackageName(dep)))
					if knownSymlinks.HasDirectory(possibleTypesDirectoryPath) {
						continue
					}
				}

				if packageResolution := resolver.ResolvePackageDirectory(dep, packageJsonName, core.ResolutionModeCommonJS, nil); packageResolution.IsResolved() && packageResolution.OriginalPath != "" {
					knownSymlinks.ProcessResolution(
						tspath.CombinePaths(packageResolution.OriginalPath, "package.json"),
						tspath.CombinePaths(packageResolution.ResolvedFileName, "package.json"),
					)
				}
			}
		}
		return knownSymlinks
	})
}
