package storecompiler

import (
	"slices"
	"strings"

	"github.com/microsoft/TypeScript/tsc/internal/ast"
	"github.com/microsoft/TypeScript/tsc/internal/ast/store"
	"github.com/microsoft/TypeScript/tsc/internal/collections"
	"github.com/microsoft/TypeScript/tsc/internal/core"
	"github.com/microsoft/TypeScript/tsc/internal/diagnostics"
	"github.com/microsoft/TypeScript/tsc/internal/module"
	"github.com/microsoft/TypeScript/tsc/internal/tracing"
	"github.com/microsoft/TypeScript/tsc/internal/tsoptions"
	"github.com/microsoft/TypeScript/tsc/internal/tspath"
	"github.com/microsoft/TypeScript/tsc/internal/vfs"
)

// The P2 declarations of the file loader (store-cli-design-20261004.md 4.2,
// 6): what cuts the loader from the content mapper, the parse cache, the
// project reference parse and the checker pool, which the Store program does
// not have. The CLI rejects content mappers and non-empty project references
// before a Store program is made (D).

// ProgramOptions is the Pointer's, of the Store's ProgramConfig and
// ProgramFactories.
//
// storetranslate:pointer-hash 4333292bf59c7b6f program.go:ProgramOptions
type ProgramOptions struct {
	ProgramConfig
	ProgramHosts
	ProgramFactories
}

// ProgramConfig carries idSpace, the program's StoreID namespace (4.3), which
// NewProgram makes: the loader and the program read it from their opts.
//
// storetranslate:pointer-hash 6993ddc85227fc35 program.go:ProgramConfig
type ProgramConfig struct {
	Config                      *tsoptions.ParsedCommandLine
	UseSourceOfProjectReference bool
	SingleThreaded              core.Tristate
	TypingsLocation             tspath.RootedDirectoryPath
	ProjectName                 string
	// SkipModuleResolution avoids all module and type reference resolution while
	// still collecting import metadata needed for emit.
	SkipModuleResolution bool

	idSpace *store.IDSpace
}

// ProgramFactories has no checker pool factory: the Store program owns the
// compiler's pool (initCheckerPool).
//
// storetranslate:pointer-hash 09172c98ac10b4af program.go:ProgramFactories
type ProgramFactories struct {
	CreateModuleResolver func(module.ResolverOptions) module.Resolver
}

// CompilerHost parses into the Store: GetSourceFile takes the program's
// IDSpace, from which it numbers the file just before the parse (4.3). It has
// no content mapper.
//
// storetranslate:pointer-hash 5416249767b9042b host.go:CompilerHost
type CompilerHost interface {
	FS() vfs.FS
	DefaultLibraryPath() tspath.RootedDirectoryPath
	Trace(msg *diagnostics.Message, args ...any)
	GetSourceFile(opts ast.SourceFileParseOptions, space *store.IDSpace) *store.File
	GetResolvedProjectReference(fileName tspath.RootedFilePath, path tspath.PathKey) *tsoptions.ParsedCommandLine
}

// parseSourceFile has no content-mapped files.
//
// storetranslate:pointer-hash 2ef5606cd23d30bc fileloader.go:fileLoader.parseSourceFile
func (p *fileLoader) parseSourceFile(t *parseTask) *store.File {
	if p.tracing != nil {
		defer p.tracing.Push(tracing.PhaseParse, "createSourceFile", map[string]any{"path": t.normalizedFilePath.AsString()}, true)()
	}
	options := p.projectReferences.getCompilerOptionsForFile(t)
	parseOptions := ast.SourceFileParseOptions{
		FileName:                       t.normalizedFilePath,
		PathKey:                        t.path,
		ExternalModuleIndicatorOptions: ast.GetExternalModuleIndicatorOptions(t.normalizedFilePath, options, t.metadata),
	}
	return p.host.GetSourceFile(parseOptions, p.opts.idSpace)
}

// addProjectReferenceTasks makes the mapper with no reference: the CLI
// rejects a non-empty references list for the Store program.
//
// storetranslate:pointer-hash 2ef77ce180a60a1b fileloader.go:fileLoader.addProjectReferenceTasks
func (p *fileLoader) addProjectReferenceTasks(singleThreaded bool) {
	p.projectReferences = &projectReferenceFileMapperBuilder{
		projectReferenceFileMapper: &projectReferenceFileMapper{
			config:                      p.opts.Config,
			useSourceOfProjectReference: p.opts.canUseProjectReferenceSource(),
		},
		host: &compilerResolutionHost{host: p.host, baseDirectory: p.opts.Config.BaseDirectory()},
	}
	if len(p.opts.Config.ResolvedProjectReferencePaths()) != 0 {
		panic("storecompiler: project references are not supported; the CLI rejects them")
	}
}

// createSyntheticImport makes the specifier in a Store of its own
// (store.NewSyntheticImport); the loader makes one for a file and a role.
//
// storetranslate:pointer-hash 60cb368221790967 fileloader.go:fileLoader.createSyntheticImport
func (p *fileLoader) createSyntheticImport(text string, file *store.File) store.Node {
	p.factoryMu.Lock()
	defer p.factoryMu.Unlock()
	return store.NewSyntheticImport(file, text)
}

// load has no content-mapped file: no virtual file name and no supplemental
// files.
//
// storetranslate:pointer-hash 151934bef852badd filesparser.go:parseTask.load
func (t *parseTask) load(loader *fileLoader) {
	t.loaded = true
	if t.isForAutomaticTypeDirective {
		t.loadAutomaticTypeDirectives(loader)
		return
	}
	if t.failedLookup {
		// The root file name did not resolve to a supported extension; the task
		// exists only to carry its processing diagnostic, so nothing is parsed.
		return
	}
	if loader.tracing != nil {
		defer loader.tracing.Push(tracing.PhaseProgram, "findSourceFile", map[string]any{"fileName": t.normalizedFilePath.AsString()}, false)()
	}
	redirect, redirectPath := loader.projectReferences.getParseFileRedirect(t)
	if redirect != "" {
		t.redirect(loader, redirect, redirectPath)
		return
	}

	if t.normalizedFilePath.HasExtension() {
		compilerOptions := loader.opts.Config.CompilerOptions()
		allowNonTsExtensions := compilerOptions.AllowNonTsExtensions.IsTrue()
		if !allowNonTsExtensions {
			canonicalFileName := t.path
			if !loader.isSupportedExtension(canonicalFileName) {
				if canonicalFileName.HasJSFileExtension() {
					t.processingDiagnostics = append(t.processingDiagnostics, &processingDiagnostic{
						kind: processingDiagnosticKindExplainingFileInclude,
						explanation: &includeExplainingDiagnostic{
							diagnosticReason: t.includeReason,
							message:          diagnostics.File_0_is_a_JavaScript_file_Did_you_mean_to_enable_the_allowJs_option,
							args:             []string{t.normalizedFilePath.AsString()},
						},
					})
				} else {
					t.processingDiagnostics = append(t.processingDiagnostics, &processingDiagnostic{
						kind: processingDiagnosticKindExplainingFileInclude,
						explanation: &includeExplainingDiagnostic{
							diagnosticReason: t.includeReason,
							message:          diagnostics.File_0_has_an_unsupported_extension_The_only_supported_extensions_are_1,
							args:             []string{t.normalizedFilePath.AsString(), "'" + strings.Join(core.Flatten(loader.supportedExtensions), "', '") + "'"},
						},
					})
				}
				return
			}
		}
	}

	loader.totalFileCount.Add(1)
	if t.libFile != nil {
		loader.libFileCount.Add(1)
		// Default lib files are all scripts; we can safely skip looking up their package.json
		// to avoid adding spurious lookups to file watcher tracking.
		t.metadata = ast.SourceFileMetaData{ImpliedNodeFormat: core.ResolutionModeCommonJS}
	} else {
		t.metadata = loader.loadSourceFileMetaData(t.normalizedFilePath)
	}

	file := t.file
	if file == nil {
		file = loader.parseSourceFile(t)
	}
	if file == nil {
		return
	}

	t.file = file
	t.subTasks = make([]*parseTask, 0, len(file.ReferencedFiles)+len(file.Imports())+len(file.ModuleAugmentations))

	compilerOptions := loader.opts.Config.CompilerOptions()
	if !compilerOptions.NoResolve.IsTrue() && !loader.opts.SkipModuleResolution {
		for index, ref := range file.ReferencedFiles {
			resolvedRef, processingDiagnostic := loader.resolveTripleslashPathReference(ref.FileName, file.FileName(), index)
			if processingDiagnostic != nil {
				t.processingDiagnostics = append(t.processingDiagnostics, processingDiagnostic)
				continue
			}
			t.addSubTask(*resolvedRef, nil)
		}

		loader.resolveTypeReferenceDirectives(t)
	}

	if compilerOptions.NoLib != core.TSTrue && !loader.opts.SkipModuleResolution {
		for index, lib := range file.LibReferenceDirectives {
			includeReason := &FileIncludeReason{
				kind: fileIncludeKindLibReferenceDirective,
				referencedFile: &referencedFileData{
					file:  t.path,
					index: index,
				},
			}
			if name, ok := tsoptions.GetLibFileName(lib.FileName); ok {
				libFile := loader.pathForLibFile(name)
				t.addSubTask(resolvedRef{
					fileName:      libFile.path,
					path:          libFile.pathKey,
					includeReason: includeReason,
				}, libFile)
			} else {
				t.processingDiagnostics = append(t.processingDiagnostics, &processingDiagnostic{
					kind:   processingDiagnosticKindUnknownReference,
					reason: includeReason,
				})
			}
		}
	}

	loader.resolveImportsAndModuleAugmentations(t)
}

// getProcessedFiles keeps no duplicate source files: the Store program has
// no parse cache whose acquires they balance.
//
// storetranslate:pointer-hash 3d5428be7d8c915d filesparser.go:filesParser.getProcessedFiles
func (w *filesParser) getProcessedFiles(loader *fileLoader) processedFiles {
	totalFileCount := int(loader.totalFileCount.Load())
	libFileCount := int(loader.libFileCount.Load())

	var missingFiles collections.Set[tspath.PathKey]
	files := make([]*store.File, 0, totalFileCount-libFileCount)
	libFiles := make([]*store.File, 0, totalFileCount) // totalFileCount here since we append files to it later to construct the final list

	filesByPath := make(map[tspath.PathKey]*store.File, totalFileCount)
	// stores 'filename -> file association' ignoring case
	// used to track cases when two file names differ only in casing
	var tasksSeenByNameIgnoreCase map[tspath.PathKey]*parseTask
	if loader.caseSensitivity.IsCaseSensitive() {
		tasksSeenByNameIgnoreCase = make(map[tspath.PathKey]*parseTask, totalFileCount)
	}

	includeData := &fileIncludeData{
		fileIncludeReasons: make(map[tspath.PathKey][]*FileIncludeReason, totalFileCount),
	}
	var outputFileToProjectReferenceSource map[tspath.PathKey]tspath.RootedFilePath
	if !loader.opts.canUseProjectReferenceSource() {
		outputFileToProjectReferenceSource = make(map[tspath.PathKey]tspath.RootedFilePath, totalFileCount)
	}
	resolvedModules := make(map[tspath.PathKey]module.ModeAwareCache[*module.ResolvedModule], totalFileCount+1)
	typeResolutionsInFile := make(map[tspath.PathKey]module.ModeAwareCache[*module.ResolvedTypeReferenceDirective], totalFileCount)
	sourceFileMetaDatas := make(map[tspath.PathKey]ast.SourceFileMetaData, totalFileCount)
	var jsxRuntimeImportSpecifiers map[tspath.PathKey]*jsxRuntimeImportSpecifier
	var importHelpersImportSpecifiers map[tspath.PathKey]store.Node
	var sourceFilesFoundSearchingNodeModules collections.Set[tspath.PathKey]
	libFilesMap := make(map[tspath.PathKey]*LibFile, libFileCount)

	var redirectTargetsMap map[tspath.PathKey][]tspath.RootedFilePath
	var redirectFilesByPath map[tspath.PathKey]*redirectsFile
	var packageIdToSourceFile map[module.PackageId]*store.File
	if !loader.opts.Config.CompilerOptions().DeduplicatePackages.IsFalse() {
		redirectTargetsMap = make(map[tspath.PathKey][]tspath.RootedFilePath)
		packageIdToSourceFile = make(map[module.PackageId]*store.File)
	}

	var collectFiles func(tasks []*parseTask, seen map[*parseTaskData]tspath.RootedFilePath)
	collectFiles = func(tasks []*parseTask, seen map[*parseTaskData]tspath.RootedFilePath) {
		for _, task := range tasks {
			includeReason := task.includeReason
			// Exclude automatic type directive tasks from include reason processing,
			// as these are internal implementation details and should not contribute
			// to the reasons for including files.
			if task.redirectedParseTask == nil && !task.isForAutomaticTypeDirective {
				if task.loadedTask != nil {
					task = task.loadedTask
				}
				w.addIncludeReason(includeData, task, includeReason)
			}
			data, _ := w.taskDataByPath.Load(task.path)
			if !task.loaded {
				continue
			}

			// ensure we only walk each task once
			if checkedName, ok := seen[data]; ok {
				if !loader.opts.Config.CompilerOptions().ForceConsistentCasingInFileNames.IsFalse() {
					// Check if it differs only in drive letters its ok to ignore that error:
					checkedAbsolutePath := checkedName.WithoutRoot()
					inputAbsolutePath := task.normalizedFilePath.WithoutRoot()
					if checkedAbsolutePath != inputAbsolutePath {
						includeData.addProcessingDiagnosticsForFileCasing(task.path, checkedName.AsString(), task.normalizedFilePath.AsString(), includeReason)
					}
				}
				continue
			} else {
				seen[data] = task.normalizedFilePath
			}

			if tasksSeenByNameIgnoreCase != nil {
				pathLowerCase := task.path.CaseInsensitiveKey()
				if taskByIgnoreCase, ok := tasksSeenByNameIgnoreCase[pathLowerCase]; ok {
					includeData.addProcessingDiagnosticsForFileCasing(taskByIgnoreCase.path, taskByIgnoreCase.normalizedFilePath.AsString(), task.normalizedFilePath.AsString(), includeReason)
				} else {
					tasksSeenByNameIgnoreCase[pathLowerCase] = task
				}
			}

			for _, trace := range task.typeResolutionsTrace {
				loader.host.Trace(trace.Message, trace.Args...)
			}
			for _, trace := range task.resolutionsTrace {
				loader.host.Trace(trace.Message, trace.Args...)
			}

			file := task.file
			if packageIdToSourceFile != nil && data.packageId.Name != "" {
				if packageIdFile, exists := packageIdToSourceFile[data.packageId]; exists {
					redirectTargetsMap[packageIdFile.PathKey()] = append(redirectTargetsMap[packageIdFile.PathKey()], task.normalizedFilePath)
					if redirectFilesByPath == nil {
						redirectFilesByPath = make(map[tspath.PathKey]*redirectsFile, totalFileCount)
					}
					redirectFilesByPath[task.path] = &redirectsFile{
						index:    len(files) + len(redirectFilesByPath),
						fileName: task.normalizedFilePath,
						path:     task.path,
						target:   packageIdFile.PathKey(),
					}
					filesByPath[task.path] = packageIdFile
					if data.lowestDepth > 0 {
						sourceFilesFoundSearchingNodeModules.Add(task.path)
					}
					continue
				} else if file != nil {
					packageIdToSourceFile[data.packageId] = file
				}
			}

			if subTasks := task.subTasks; len(subTasks) > 0 {
				collectFiles(subTasks, seen)
			}

			// Exclude automatic type directive tasks from include reason processing,
			// as these are internal implementation details and should not contribute
			// to the reasons for including files.
			if task.redirectedParseTask != nil {
				if !loader.opts.canUseProjectReferenceSource() {
					outputFileToProjectReferenceSource[task.redirectedParseTask.path] = task.FileName()
				}
				continue
			}

			if task.isForAutomaticTypeDirective {
				typeResolutionsInFile[task.path] = task.typeResolutionsInFile
				if len(task.processingDiagnostics) > 0 {
					includeData.processingDiagnostics = append(includeData.processingDiagnostics, task.processingDiagnostics...)
				}
				continue
			}

			path := task.path

			if len(task.processingDiagnostics) > 0 {
				includeData.processingDiagnostics = append(includeData.processingDiagnostics, task.processingDiagnostics...)
			}

			if file == nil {
				missingFiles.Add(path)
				continue
			}

			if task.libFile != nil {
				libFiles = append(libFiles, file)
				libFilesMap[path] = task.libFile
			} else {
				files = append(files, file)
			}
			filesByPath[path] = file
			resolvedModules[path] = task.resolutionsInFile
			typeResolutionsInFile[path] = task.typeResolutionsInFile
			sourceFileMetaDatas[path] = task.metadata

			if task.jsxRuntimeImportSpecifier != nil {
				if jsxRuntimeImportSpecifiers == nil {
					jsxRuntimeImportSpecifiers = make(map[tspath.PathKey]*jsxRuntimeImportSpecifier, totalFileCount)
				}
				jsxRuntimeImportSpecifiers[path] = task.jsxRuntimeImportSpecifier
			}
			if !task.importHelpersImportSpecifier.IsNil() {
				if importHelpersImportSpecifiers == nil {
					importHelpersImportSpecifiers = make(map[tspath.PathKey]store.Node, totalFileCount)
				}
				importHelpersImportSpecifiers[path] = task.importHelpersImportSpecifier
			}
			if data.lowestDepth > 0 {
				sourceFilesFoundSearchingNodeModules.Add(path)
			}
		}
	}

	collectFiles(loader.rootTasks, make(map[*parseTaskData]tspath.RootedFilePath, totalFileCount))
	loader.sortLibs(libFiles)

	allFiles := append(libFiles, files...)
	for _, redirectFile := range redirectFilesByPath {
		redirectFile.index += len(libFiles)
	}

	keys := slices.Collect(loader.pathForLibFileResolutions.Keys())
	slices.Sort(keys)
	for _, key := range keys {
		value, _ := loader.pathForLibFileResolutions.Load(key)
		resolvedModules[key] = module.ModeAwareCache[*module.ResolvedModule]{
			module.ModeAwareCacheKey{Name: value.libraryName, Mode: core.ModuleKindCommonJS}: value.resolution,
		}
		for _, trace := range value.trace {
			loader.host.Trace(trace.Message, trace.Args...)
		}
	}

	return processedFiles{
		finishedProcessing:                   true,
		files:                                allFiles,
		filesByPath:                          filesByPath,
		projectReferenceFileMapper:           loader.projectReferences.projectReferenceFileMapper,
		resolvedModules:                      resolvedModules,
		typeResolutionsInFile:                typeResolutionsInFile,
		sourceFileMetaDatas:                  sourceFileMetaDatas,
		jsxRuntimeImportSpecifiers:           jsxRuntimeImportSpecifiers,
		importHelpersImportSpecifiers:        importHelpersImportSpecifiers,
		sourceFilesFoundSearchingNodeModules: sourceFilesFoundSearchingNodeModules,
		libFiles:                             libFilesMap,
		missingFiles:                         missingFiles,
		fileIncludeData:                      *includeData,
		outputFileToProjectReferenceSource:   outputFileToProjectReferenceSource,
		redirectTargetsMap:                   redirectTargetsMap,
		redirectFilesByPath:                  redirectFilesByPath,
		contentMapperDiagnostics:             loader.contentMapperDiagnostics,
	}
}
