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

// ProgramOptions has no checker pool: the Store program owns its one checker.
// idSpace is the program's StoreID namespace (4.3), which NewProgram makes.
//
// storetranslate:pointer-hash 9beea9be69954d97 program.go:ProgramOptions
type ProgramOptions struct {
	Host                        CompilerHost
	Config                      *tsoptions.ParsedCommandLine
	UseSourceOfProjectReference bool
	SingleThreaded              core.Tristate
	TypingsLocation             string
	ProjectName                 string
	Tracing                     *tracing.Tracing
	// SkipModuleResolution avoids all module and type reference resolution while
	// still collecting import metadata needed for emit.
	SkipModuleResolution bool

	idSpace *store.IDSpace
}

// CompilerHost parses into the Store: GetSourceFile takes the program's
// IDSpace, from which it numbers the file just before the parse (4.3). It has
// no content mapper.
//
// storetranslate:pointer-hash 59bbef62b1636236 host.go:CompilerHost
type CompilerHost interface {
	FS() vfs.FS
	DefaultLibraryPath() string
	GetCurrentDirectory() string
	Trace(msg *diagnostics.Message, args ...any)
	GetSourceFile(opts ast.SourceFileParseOptions, space *store.IDSpace) *store.File
	GetResolvedProjectReference(fileName string, path tspath.Path) *tsoptions.ParsedCommandLine
}

// parseSourceFile has no content-mapped files.
//
// storetranslate:pointer-hash 50fefef9856a4d2a fileloader.go:fileLoader.parseSourceFile
func (p *fileLoader) parseSourceFile(t *parseTask) *store.File {
	if p.opts.Tracing != nil {
		defer p.opts.Tracing.Push(tracing.PhaseParse, "createSourceFile", map[string]any{"path": t.normalizedFilePath}, true)()
	}
	path := p.toPath(t.normalizedFilePath)
	options := p.projectReferenceFileMapper.getCompilerOptionsForFile(t)
	parseOptions := ast.SourceFileParseOptions{
		FileName:                       t.normalizedFilePath,
		Path:                           path,
		ExternalModuleIndicatorOptions: ast.GetExternalModuleIndicatorOptions(t.normalizedFilePath, options, t.metadata),
	}
	return p.opts.Host.GetSourceFile(parseOptions, p.opts.idSpace)
}

// addProjectReferenceTasks makes the mapper with no reference: the CLI
// rejects a non-empty references list for the Store program.
//
// storetranslate:pointer-hash d957643b2eb4542e fileloader.go:fileLoader.addProjectReferenceTasks
func (p *fileLoader) addProjectReferenceTasks(singleThreaded bool) {
	p.projectReferenceFileMapper = &projectReferenceFileMapper{
		opts: p.opts,
		host: p.opts.Host,
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
// storetranslate:pointer-hash 306708cc349f5aa6 filesparser.go:parseTask.load
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
	if loader.opts.Tracing != nil {
		defer loader.opts.Tracing.Push(tracing.PhaseProgram, "findSourceFile", map[string]any{"fileName": t.normalizedFilePath}, false)()
	}
	redirect := loader.projectReferenceFileMapper.getParseFileRedirect(t)
	if redirect != "" {
		t.redirect(loader, redirect)
		return
	}

	if tspath.HasExtension(t.normalizedFilePath) {
		compilerOptions := loader.opts.Config.CompilerOptions()
		allowNonTsExtensions := compilerOptions.AllowNonTsExtensions.IsTrue()
		if !allowNonTsExtensions {
			canonicalFileName := tspath.GetCanonicalFileName(t.normalizedFilePath, loader.opts.Host.FS().UseCaseSensitiveFileNames())
			if !loader.isSupportedExtension(canonicalFileName) {
				if tspath.HasJSFileExtension(canonicalFileName) {
					t.processingDiagnostics = append(t.processingDiagnostics, &processingDiagnostic{
						kind: processingDiagnosticKindExplainingFileInclude,
						data: &includeExplainingDiagnostic{
							diagnosticReason: t.includeReason,
							message:          diagnostics.File_0_is_a_JavaScript_file_Did_you_mean_to_enable_the_allowJs_option,
							args:             []any{t.normalizedFilePath},
						},
					})
				} else {
					t.processingDiagnostics = append(t.processingDiagnostics, &processingDiagnostic{
						kind: processingDiagnosticKindExplainingFileInclude,
						data: &includeExplainingDiagnostic{
							diagnosticReason: t.includeReason,
							message:          diagnostics.File_0_has_an_unsupported_extension_The_only_supported_extensions_are_1,
							args:             []any{t.normalizedFilePath, "'" + strings.Join(core.Flatten(loader.supportedExtensions), "', '") + "'"},
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
				data: &referencedFileData{
					file:  t.path,
					index: index,
				},
			}
			if name, ok := tsoptions.GetLibFileName(lib.FileName); ok {
				libFile := loader.pathForLibFile(name)
				t.addSubTask(resolvedRef{
					fileName:      libFile.path,
					includeReason: includeReason,
				}, libFile)
			} else {
				t.processingDiagnostics = append(t.processingDiagnostics, &processingDiagnostic{
					kind: processingDiagnosticKindUnknownReference,
					data: includeReason,
				})
			}
		}
	}

	loader.resolveImportsAndModuleAugmentations(t)
}

// getProcessedFiles keeps no duplicate source files: the Store program has
// no parse cache whose acquires they balance.
//
// storetranslate:pointer-hash a089961247b3eeee filesparser.go:filesParser.getProcessedFiles
func (w *filesParser) getProcessedFiles(loader *fileLoader) processedFiles {
	totalFileCount := int(loader.totalFileCount.Load())
	libFileCount := int(loader.libFileCount.Load())

	var missingFiles []string
	files := make([]*store.File, 0, totalFileCount-libFileCount)
	libFiles := make([]*store.File, 0, totalFileCount) // totalFileCount here since we append files to it later to construct the final list

	filesByPath := make(map[tspath.Path]*store.File, totalFileCount)
	// stores 'filename -> file association' ignoring case
	// used to track cases when two file names differ only in casing
	var tasksSeenByNameIgnoreCase map[string]*parseTask
	if loader.comparePathsOptions.UseCaseSensitiveFileNames {
		tasksSeenByNameIgnoreCase = make(map[string]*parseTask, totalFileCount)
	}

	includeProcessor := &includeProcessor{
		fileIncludeReasons: make(map[tspath.Path][]*FileIncludeReason, totalFileCount),
	}
	var outputFileToProjectReferenceSource map[tspath.Path]string
	if !loader.opts.canUseProjectReferenceSource() {
		outputFileToProjectReferenceSource = make(map[tspath.Path]string, totalFileCount)
	}
	resolvedModules := make(map[tspath.Path]module.ModeAwareCache[*module.ResolvedModule], totalFileCount+1)
	typeResolutionsInFile := make(map[tspath.Path]module.ModeAwareCache[*module.ResolvedTypeReferenceDirective], totalFileCount)
	sourceFileMetaDatas := make(map[tspath.Path]ast.SourceFileMetaData, totalFileCount)
	var jsxRuntimeImportSpecifiers map[tspath.Path]*jsxRuntimeImportSpecifier
	var importHelpersImportSpecifiers map[tspath.Path]store.Node
	var sourceFilesFoundSearchingNodeModules collections.Set[tspath.Path]
	libFilesMap := make(map[tspath.Path]*LibFile, libFileCount)

	var redirectTargetsMap map[tspath.Path][]string
	var redirectFilesByPath map[tspath.Path]*redirectsFile
	var packageIdToSourceFile map[module.PackageId]*store.File
	if !loader.opts.Config.CompilerOptions().DeduplicatePackages.IsFalse() {
		redirectTargetsMap = make(map[tspath.Path][]string)
		packageIdToSourceFile = make(map[module.PackageId]*store.File)
	}

	var collectFiles func(tasks []*parseTask, seen map[*parseTaskData]string)
	collectFiles = func(tasks []*parseTask, seen map[*parseTaskData]string) {
		for _, task := range tasks {
			includeReason := task.includeReason
			// Exclude automatic type directive tasks from include reason processing,
			// as these are internal implementation details and should not contribute
			// to the reasons for including files.
			if task.redirectedParseTask == nil && !task.isForAutomaticTypeDirective {
				if task.loadedTask != nil {
					task = task.loadedTask
				}
				w.addIncludeReason(includeProcessor, task, includeReason)
			}
			data, _ := w.taskDataByPath.Load(task.path)
			if !task.loaded {
				continue
			}

			// ensure we only walk each task once
			if checkedName, ok := seen[data]; ok {
				if !loader.opts.Config.CompilerOptions().ForceConsistentCasingInFileNames.IsFalse() {
					// Check if it differs only in drive letters its ok to ignore that error:
					checkedAbsolutePath := tspath.GetNormalizedAbsolutePathWithoutRoot(checkedName, loader.comparePathsOptions.CurrentDirectory)
					inputAbsolutePath := tspath.GetNormalizedAbsolutePathWithoutRoot(task.normalizedFilePath, loader.comparePathsOptions.CurrentDirectory)
					if checkedAbsolutePath != inputAbsolutePath {
						includeProcessor.addProcessingDiagnosticsForFileCasing(task.path, checkedName, task.normalizedFilePath, includeReason)
					}
				}
				continue
			} else {
				seen[data] = task.normalizedFilePath
			}

			if tasksSeenByNameIgnoreCase != nil {
				pathLowerCase := tspath.ToFileNameLowerCase(string(task.path))
				if taskByIgnoreCase, ok := tasksSeenByNameIgnoreCase[pathLowerCase]; ok {
					includeProcessor.addProcessingDiagnosticsForFileCasing(taskByIgnoreCase.path, taskByIgnoreCase.normalizedFilePath, task.normalizedFilePath, includeReason)
				} else {
					tasksSeenByNameIgnoreCase[pathLowerCase] = task
				}
			}

			for _, trace := range task.typeResolutionsTrace {
				loader.opts.Host.Trace(trace.Message, trace.Args...)
			}
			for _, trace := range task.resolutionsTrace {
				loader.opts.Host.Trace(trace.Message, trace.Args...)
			}

			file := task.file
			if packageIdToSourceFile != nil && data.packageId.Name != "" {
				if packageIdFile, exists := packageIdToSourceFile[data.packageId]; exists {
					redirectTargetsMap[packageIdFile.Path()] = append(redirectTargetsMap[packageIdFile.Path()], task.normalizedFilePath)
					if redirectFilesByPath == nil {
						redirectFilesByPath = make(map[tspath.Path]*redirectsFile, totalFileCount)
					}
					redirectFilesByPath[task.path] = &redirectsFile{
						index:    len(files) + len(redirectFilesByPath),
						fileName: task.normalizedFilePath,
						path:     task.path,
						target:   packageIdFile.Path(),
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
					includeProcessor.processingDiagnostics = append(includeProcessor.processingDiagnostics, task.processingDiagnostics...)
				}
				continue
			}

			path := task.path

			if len(task.processingDiagnostics) > 0 {
				includeProcessor.processingDiagnostics = append(includeProcessor.processingDiagnostics, task.processingDiagnostics...)
			}

			if file == nil {
				missingFiles = append(missingFiles, task.normalizedFilePath)
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
					jsxRuntimeImportSpecifiers = make(map[tspath.Path]*jsxRuntimeImportSpecifier, totalFileCount)
				}
				jsxRuntimeImportSpecifiers[path] = task.jsxRuntimeImportSpecifier
			}
			if !task.importHelpersImportSpecifier.IsNil() {
				if importHelpersImportSpecifiers == nil {
					importHelpersImportSpecifiers = make(map[tspath.Path]store.Node, totalFileCount)
				}
				importHelpersImportSpecifiers[path] = task.importHelpersImportSpecifier
			}
			if data.lowestDepth > 0 {
				sourceFilesFoundSearchingNodeModules.Add(path)
			}
		}
	}

	collectFiles(loader.rootTasks, make(map[*parseTaskData]string, totalFileCount))
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
			loader.opts.Host.Trace(trace.Message, trace.Args...)
		}
	}

	return processedFiles{
		finishedProcessing:                   true,
		resolver:                             loader.resolver,
		files:                                allFiles,
		filesByPath:                          filesByPath,
		projectReferenceFileMapper:           loader.projectReferenceFileMapper,
		resolvedModules:                      resolvedModules,
		typeResolutionsInFile:                typeResolutionsInFile,
		sourceFileMetaDatas:                  sourceFileMetaDatas,
		jsxRuntimeImportSpecifiers:           jsxRuntimeImportSpecifiers,
		importHelpersImportSpecifiers:        importHelpersImportSpecifiers,
		sourceFilesFoundSearchingNodeModules: sourceFilesFoundSearchingNodeModules,
		libFiles:                             libFilesMap,
		missingFiles:                         missingFiles,
		includeProcessor:                     includeProcessor,
		outputFileToProjectReferenceSource:   outputFileToProjectReferenceSource,
		redirectTargetsMap:                   redirectTargetsMap,
		redirectFilesByPath:                  redirectFilesByPath,
		contentMapperDiagnostics:             loader.contentMapperDiagnostics,
	}
}
