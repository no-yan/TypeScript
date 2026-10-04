package storecompiler

import (
	"github.com/microsoft/TypeScript/tsc/internal/ast/store"
	"github.com/microsoft/TypeScript/tsc/internal/outputpaths"
	"github.com/microsoft/TypeScript/tsc/internal/tspath"
)

// sourceFileMayBeEmitted has no content-mapped file (store-cli-design-20261004.md 4.1).
//
// storetranslate:pointer-hash 9fe4fb0f9a3ca21b emitter.go:sourceFileMayBeEmitted
func sourceFileMayBeEmitted(sourceFile *store.File, host SourceFileMayBeEmittedHost, forceDtsEmit bool, forceJsEmit bool) bool {
	// TODO: move this to outputpaths?
	options := host.Options()
	// Js files are emitted only if option is enabled
	if !forceJsEmit && options.NoEmitForJsFiles.IsTrue() && store.IsSourceFileJS(sourceFile) {
		return false
	}

	// Declaration files are not emitted
	if sourceFile.IsDeclarationFile {
		return false
	}

	// Source file from node_modules are not emitted
	if host.IsSourceFileFromExternalLibrary(sourceFile) {
		return false
	}

	// forcing dts emit => file needs to be emitted
	if forceDtsEmit || forceJsEmit {
		return true
	}

	// Check other conditions for file emit
	// Source files from referenced projects are not emitted
	if host.GetProjectReferenceFromSource(sourceFile.Path()) != nil {
		return false
	}

	// Any non json file should be emitted
	if !store.IsJsonSourceFile(sourceFile) {
		return true
	}

	// Json file is not emitted if outDir is not specified
	if options.OutDir == "" {
		return false
	}

	// Otherwise, if rootDir is specified or a config file exists, we know the common source directory and can check if the file would be emitted in the same location
	if options.RootDir != "" || options.ConfigFilePath != "" {
		commonDir := tspath.GetNormalizedAbsolutePath(outputpaths.GetCommonSourceDirectory(options, func() []string { return nil }, host.GetCurrentDirectory(), host.UseCaseSensitiveFileNames(), nil), host.GetCurrentDirectory())
		outputPath := outputpaths.GetSourceFilePathInNewDirWorker(sourceFile.FileName(), options.OutDir, host.GetCurrentDirectory(), commonDir, host.UseCaseSensitiveFileNames())
		if tspath.ComparePaths(sourceFile.FileName(), outputPath, tspath.ComparePathsOptions{
			UseCaseSensitiveFileNames: host.UseCaseSensitiveFileNames(),
			CurrentDirectory:          host.GetCurrentDirectory(),
		}) == 0 {
			return false
		}
	}
	return true
}
