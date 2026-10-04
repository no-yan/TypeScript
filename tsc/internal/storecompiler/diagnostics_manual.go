package storecompiler

import (
	"context"
	"fmt"
	"slices"

	"github.com/microsoft/TypeScript/tsc/internal/ast"
	"github.com/microsoft/TypeScript/tsc/internal/ast/store"
	"github.com/microsoft/TypeScript/tsc/internal/core"
	"github.com/microsoft/TypeScript/tsc/internal/diagnostics"
	"github.com/microsoft/TypeScript/tsc/internal/storebinder"
	"github.com/microsoft/TypeScript/tsc/internal/storechecker"
	"github.com/microsoft/TypeScript/tsc/internal/tsoptions"
	"github.com/microsoft/TypeScript/tsc/internal/tspath"
)

// The P2 declarations of the program's diagnostics
// (store-cli-design-20261004.md 5): the program has one checker, which it
// makes on the first ask; the parser's and the binder's diagnostics have no
// file and get the Store file here; the config's and the module resolver's
// are the Pointer's and become Store diagnostics (store.DiagnosticFromAST);
// there is no content mapper.

// storetranslate:pointer-hash 292be3f116d28cc7 program.go:Program.GetSyntacticDiagnostics
func (p *Program) GetSyntacticDiagnostics(ctx context.Context, sourceFile *store.File) []*store.Diagnostic {
	return p.collectDiagnostics(ctx, sourceFile, false /*concurrent*/, func(_ context.Context, file *store.File) []*store.Diagnostic {
		diags := store.DiagnosticsFromAST(core.Concatenate(file.Diagnostics(), file.JSDiagnostics()), store.InFile(file))
		// For JS files that won't be checked by the checker (no checkJs/ts-check), we need
		// program-level syntactic checks that require compiler options. This mirrors Strada's
		// getJSSyntacticDiagnosticsForFile in program.ts.
		if store.IsSourceFileJS(file) && !store.IsCheckJSEnabledForFile(file, p.Options()) {
			diags = append(diags, getAdditionalJSSyntacticDiagnostics(file, p.Options())...)
		}
		return diags
	})
}

// storetranslate:pointer-hash 98003fd9cf4d4b41 program.go:Program.GetBindDiagnostics
func (p *Program) GetBindDiagnostics(ctx context.Context, sourceFile *store.File) []*store.Diagnostic {
	if sourceFile != nil {
		storebinder.BindSourceFile(sourceFile)
	} else {
		p.BindSourceFiles()
	}
	return p.collectDiagnostics(ctx, sourceFile, false /*concurrent*/, func(_ context.Context, file *store.File) []*store.Diagnostic {
		return store.DiagnosticsFromAST(file.Bound.BindDiagnostics(), store.InFile(file))
	})
}

// collectCheckerDiagnostics asks the program's checker, file by file in the
// program's order, as the compiler pool of one checker does.
//
// storetranslate:pointer-hash 22206c37f727e753 program.go:Program.collectCheckerDiagnostics
func (p *Program) collectCheckerDiagnostics(ctx context.Context, sourceFile *store.File, collect func(context.Context, *storechecker.Checker, *store.File) []*store.Diagnostic) []*store.Diagnostic {
	c := p.getTypeChecker()
	if sourceFile != nil {
		if p.SkipTypeChecking(sourceFile, false) {
			return nil
		}
		return filterAndSortDiagnostics(collect(ctx, c, sourceFile))
	}
	diagnostics := make([][]*store.Diagnostic, len(p.files))
	for i, file := range p.files {
		diagnostics[i] = collect(ctx, c, file)
	}
	return filterAndSortDiagnostics(slices.Concat(diagnostics...))
}

// getBindAndCheckDiagnosticsWithChecker has no content mapper directives.
//
// storetranslate:pointer-hash b6b5bbecdef1827e program.go:Program.getBindAndCheckDiagnosticsWithChecker
func (p *Program) getBindAndCheckDiagnosticsWithChecker(ctx context.Context, fileChecker *storechecker.Checker, sourceFile *store.File) []*store.Diagnostic {
	compilerOptions := p.Options()
	if p.SkipTypeChecking(sourceFile, false) {
		return nil
	}

	// Checker creation forces binding, so bind diagnostics will be populated.
	diags := store.DiagnosticsFromAST(sourceFile.Bound.BindDiagnostics(), store.InFile(sourceFile))
	diags = append(diags, fileChecker.GetDiagnostics(ctx, sourceFile)...)

	isPlainJS := store.IsPlainJSFile(sourceFile, compilerOptions.CheckJs)
	if isPlainJS {
		return core.Filter(diags, func(d *store.Diagnostic) bool {
			return plainJSErrors.Has(d.Code())
		})
	}

	isJS := sourceFile.ScriptKind == core.ScriptKindJS || sourceFile.ScriptKind == core.ScriptKindJSX
	isCheckJS := isJS && store.IsCheckJSEnabledForFile(sourceFile, compilerOptions)
	if isCheckJS {
		diags = append(diags, store.DiagnosticsFromAST(sourceFile.JSDocDiagnostics(), store.InFile(sourceFile))...)
	}

	filtered, directivesByLine := p.getDiagnosticsWithPrecedingDirectives(sourceFile, diags)
	for _, directive := range directivesByLine {
		// Above we changed all used directive kinds to @ts-ignore, so any @ts-expect-error directives that
		// remain are unused and thus errors.
		if directive.Kind == ast.CommentDirectiveKindExpectError {
			filtered = append(filtered, store.NewDiagnostic(sourceFile, directive.Loc, diagnostics.Unused_ts_expect_error_directive))
		}
	}
	return filtered
}

// storetranslate:pointer-hash 0dbad2d1ee12ae0d program.go:Program.GetGlobalDiagnostics
func (p *Program) GetGlobalDiagnostics(ctx context.Context) []*store.Diagnostic {
	if len(p.files) == 0 {
		return nil
	}
	return SortAndDeduplicateDiagnostics(p.getTypeChecker().GetGlobalDiagnostics())
}

// GetProgramDiagnostics has no content mapper diagnostics.
//
// storetranslate:pointer-hash aa8a0c3a91061974 program.go:Program.GetProgramDiagnostics
func (p *Program) GetProgramDiagnostics() []*store.Diagnostic {
	return SortAndDeduplicateDiagnostics(slices.Concat(
		p.programDiagnostics,
		p.includeProcessor.getDiagnostics(p).GetGlobalDiagnostics(),
	))
}

// getDiagnostics takes the resolver's diagnostics, which are the Pointer's
// with no file, as Store diagnostics.
//
// storetranslate:pointer-hash f1d0010fe34b94c3 includeprocessor.go:includeProcessor.getDiagnostics
func (i *includeProcessor) getDiagnostics(p *Program) *store.DiagnosticsCollection {
	i.computedDiagnosticsOnce.Do(func() {
		i.computedDiagnostics = &store.DiagnosticsCollection{}
		for _, d := range i.processingDiagnostics {
			i.computedDiagnostics.Add(d.toDiagnostic(p))
		}
		for _, resolutions := range p.resolvedModules {
			for _, resolvedModule := range resolutions {
				for _, diag := range resolvedModule.ResolutionDiagnostics {
					i.computedDiagnostics.Add(store.DiagnosticFromAST(diag, p.configFile))
				}
			}
		}
		for _, typeResolutions := range p.typeResolutionsInFile {
			for _, resolvedTypeRef := range typeResolutions {
				for _, diag := range resolvedTypeRef.ResolutionDiagnostics {
					i.computedDiagnostics.Add(store.DiagnosticFromAST(diag, p.configFile))
				}
			}
		}
	})
	return i.computedDiagnostics
}

// toRelatedInfo points into the config file, the Pointer's, through its Store
// file (Program.configFile).
//
// storetranslate:pointer-hash 08d04b782f105724 fileInclude.go:FileIncludeReason.toRelatedInfo
func (r *FileIncludeReason) toRelatedInfo(program *Program) *store.Diagnostic {
	if r.isReferencedFile() {
		return r.computeReferenceFileRelatedInfo(program)
	}
	if program.opts.Config.ConfigFile == nil {
		return nil
	}
	config := program.opts.Config
	inConfig := func(d *ast.Diagnostic) *store.Diagnostic { return store.DiagnosticFromAST(d, program.configFile) }
	switch r.kind {
	case fileIncludeKindRootFile:
		fileName := tspath.GetNormalizedAbsolutePath(config.FileNames()[r.asIndex()], program.GetCurrentDirectory())
		if matchedFileSpec := config.GetMatchedFileSpec(fileName); matchedFileSpec != "" {
			if filesNode := tsoptions.GetTsConfigPropArrayElementValue(config.ConfigFile.SourceFile, "files", matchedFileSpec); filesNode != nil {
				return inConfig(tsoptions.CreateDiagnosticForNodeInSourceFile(config.ConfigFile.SourceFile, filesNode.AsNode(), diagnostics.File_is_matched_by_files_list_specified_here))
			}
		} else if matchedIncludeSpec, isDefaultIncludeSpec := config.GetMatchedIncludeSpec(fileName); matchedIncludeSpec != "" && !isDefaultIncludeSpec {
			if includeNode := tsoptions.GetTsConfigPropArrayElementValue(config.ConfigFile.SourceFile, "include", matchedIncludeSpec); includeNode != nil {
				return inConfig(tsoptions.CreateDiagnosticForNodeInSourceFile(config.ConfigFile.SourceFile, includeNode.AsNode(), diagnostics.File_is_matched_by_include_pattern_specified_here))
			}
		}
	case fileIncludeKindAutomaticTypeDirectiveFile:
		if !program.Options().UsesWildcardTypes() {
			data := r.asAutomaticTypeDirectiveFileData()
			if typesSyntax := tsoptions.GetOptionsSyntaxByArrayElementValue(program.includeProcessor.getCompilerOptionsObjectLiteralSyntax(program), "types", data.typeReference); typesSyntax != nil {
				return inConfig(tsoptions.CreateDiagnosticForNodeInSourceFile(config.ConfigFile.SourceFile, typesSyntax.AsNode(), diagnostics.File_is_entry_point_of_type_library_specified_here))
			}
		}
	case fileIncludeKindLibFile:
		if index, ok := r.asLibFileIndex(); ok {
			if libSyntax := tsoptions.GetOptionsSyntaxByArrayElementValue(program.includeProcessor.getCompilerOptionsObjectLiteralSyntax(program), "lib", program.Options().Lib[index]); libSyntax != nil {
				return inConfig(tsoptions.CreateDiagnosticForNodeInSourceFile(config.ConfigFile.SourceFile, libSyntax.AsNode(), diagnostics.File_is_library_specified_here))
			}
		} else if target := program.Options().GetEmitScriptTarget().String(); target != "" {
			if targetValueSyntax := tsoptions.ForEachPropertyAssignment(program.includeProcessor.getCompilerOptionsObjectLiteralSyntax(program), "target", tsoptions.GetCallbackForFindingPropertyAssignmentByValue(target)); targetValueSyntax != nil {
				return inConfig(tsoptions.CreateDiagnosticForNodeInSourceFile(config.ConfigFile.SourceFile, targetValueSyntax.AsNode(), diagnostics.File_is_default_library_for_target_specified_here))
			}
		}
	case fileIncludeKindContentMapperSupplemental:
		return nil
	default:
		panic(fmt.Sprintf("unknown reason: %v", r.kind))
	}
	return nil
}

// SymbolCount counts the bound files' symbols and then the checker's, which
// it makes if there is none yet, as the compiler pool does.
//
// storetranslate:pointer-hash 0fce1efcada135f5 program.go:Program.SymbolCount
func (p *Program) SymbolCount() int {
	var count int
	for _, file := range p.files {
		if file.Bound != nil {
			count += file.Bound.SymbolCount
		}
	}
	return count + int(p.getTypeChecker().SymbolCount)
}

// storetranslate:pointer-hash 9381bd6319b3f045 program.go:Program.TypeCount
func (p *Program) TypeCount() int {
	return int(p.getTypeChecker().TypeCount)
}

// storetranslate:pointer-hash 5469acf90ba94554 program.go:Program.InstantiationCount
func (p *Program) InstantiationCount() int {
	return int(p.getTypeChecker().TotalInstantiationCount)
}

// getAdditionalJSSyntacticDiagnostics walks the whole file: the Store has no
// SubtreeFacts, by which the Pointer skips a subtree without decorators. A
// subtree whose facts the Pointer erases for emit (a method without a body)
// is a syntax error in JS already; the native equivalence (7e E) compares
// the result.
//
// storetranslate:pointer-hash 3b2f92f29b895311 program.go:getAdditionalJSSyntacticDiagnostics
func getAdditionalJSSyntacticDiagnostics(file *store.File, options *core.CompilerOptions) []*store.Diagnostic {
	if options.ExperimentalDecorators.IsTrue() {
		return nil
	}
	var diags []*store.Diagnostic
	// Parameter decorators are only valid with experimentalDecorators. Without it,
	// the checker would report this, but the checker doesn't run on unchecked JS files.
	var walk store.Visitor
	walk = func(node store.Node) bool {
		if node.Kind() == ast.KindParameter && store.HasDecorators(node) {
			modifiers := node.ModifierNodes()
			for i := range modifiers.Len() {
				if decorator := modifiers.At(i); store.IsDecorator(decorator) {
					diags = append(diags, store.NewDiagnostic(file, decorator.Loc(), diagnostics.Decorators_are_not_valid_here))
					break
				}
			}
		}
		node.ForEachChild(walk)
		return false
	}
	file.Root().ForEachChild(walk)
	return diags
}

// GetConfigFileParsingDiagnostics are the config's, the Pointer's, as Store
// diagnostics.
//
// storetranslate:pointer-hash 409df864dd15ccde program.go:Program.GetConfigFileParsingDiagnostics
func (p *Program) GetConfigFileParsingDiagnostics() []*store.Diagnostic {
	return store.DiagnosticsFromAST(p.opts.Config.GetConfigFileParsingDiagnostics(), p.configFile)
}
