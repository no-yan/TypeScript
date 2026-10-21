package storecompiler

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/microsoft/TypeScript/tsc/internal/ast"
	"github.com/microsoft/TypeScript/tsc/internal/ast/store"
	"github.com/microsoft/TypeScript/tsc/internal/collections"
	"github.com/microsoft/TypeScript/tsc/internal/core"
	"github.com/microsoft/TypeScript/tsc/internal/diagnostics"
	"github.com/microsoft/TypeScript/tsc/internal/outputpaths"
	"github.com/microsoft/TypeScript/tsc/internal/scanner"
	"github.com/microsoft/TypeScript/tsc/internal/tsoptions"
	"github.com/microsoft/TypeScript/tsc/internal/tspath"
)

// verifyCompilerOptions makes the options diagnostics on the config, the
// Pointer's JSON AST, as the Pointer does, and keeps them as Store
// diagnostics (store-cli-design-20261004.md 6); it reads the Store files
// where the Pointer reads its files. The program has no project reference and
// no emit, and no content mapper: a file's root path is its own path, not its
// canonical source file's.
//
// storetranslate:pointer-hash a34c02ae1dd863f2 program.go:Program.verifyCompilerOptions
func (p *Program) verifyCompilerOptions() {
	options := p.Options()
	var programDiagnostics []*ast.Diagnostic
	defer func() { p.programDiagnostics = store.DiagnosticsFromAST(programDiagnostics, p.configFile) }()

	sourceFile := core.Memoize(func() *ast.SourceFile {
		configFile := p.opts.Config.ConfigFile
		if configFile == nil {
			return nil
		}
		return configFile.SourceFile
	})

	configFilePath := core.Memoize(func() tspath.RootedFilePath {
		file := sourceFile()
		if file != nil {
			return file.FileName()
		}
		return ""
	})

	getCompilerOptionsPropertySyntax := core.Memoize(func() *ast.PropertyAssignment {
		return tsoptions.ForEachTsConfigPropArray(sourceFile(), "compilerOptions", core.Identity)
	})

	getCompilerOptionsObjectLiteralSyntax := core.Memoize(func() *ast.ObjectLiteralExpression {
		compilerOptionsProperty := getCompilerOptionsPropertySyntax()
		if compilerOptionsProperty != nil &&
			compilerOptionsProperty.Initializer != nil &&
			ast.IsObjectLiteralExpression(compilerOptionsProperty.Initializer) {
			return compilerOptionsProperty.Initializer.AsObjectLiteralExpression()
		}
		return nil
	})

	createOptionDiagnosticInObjectLiteralSyntax := func(objectLiteral *ast.ObjectLiteralExpression, onKey bool, key1 string, key2 string, message *diagnostics.Message, args ...any) *ast.Diagnostic {
		diag := tsoptions.ForEachPropertyAssignment(objectLiteral, key1, func(property *ast.PropertyAssignment) *ast.Diagnostic {
			return tsoptions.CreateDiagnosticForNodeInSourceFile(sourceFile(), core.IfElse(onKey, property.Name(), property.Initializer), message, args...)
		}, key2)
		if diag != nil {
			programDiagnostics = append(programDiagnostics, diag)
		}
		return diag
	}

	createCompilerOptionsDiagnostic := func(message *diagnostics.Message, args ...any) *ast.Diagnostic {
		compilerOptionsProperty := getCompilerOptionsPropertySyntax()
		var diag *ast.Diagnostic
		if compilerOptionsProperty != nil {
			diag = tsoptions.CreateDiagnosticForNodeInSourceFile(sourceFile(), compilerOptionsProperty.Name(), message, args...)
		} else {
			diag = ast.NewCompilerDiagnostic(message, args...)
		}
		programDiagnostics = append(programDiagnostics, diag)
		return diag
	}

	createDiagnosticForOption := func(onKey bool, option1 string, option2 string, message *diagnostics.Message, args ...any) *ast.Diagnostic {
		diag := createOptionDiagnosticInObjectLiteralSyntax(getCompilerOptionsObjectLiteralSyntax(), onKey, option1, option2, message, args...)
		if diag == nil {
			diag = createCompilerOptionsDiagnostic(message, args...)
		}
		return diag
	}

	createDiagnosticForOptionName := func(message *diagnostics.Message, option1 string, option2 string, args ...any) {
		newArgs := make([]any, 0, len(args)+2)
		newArgs = append(newArgs, option1, option2)
		newArgs = append(newArgs, args...)
		createDiagnosticForOption(true /*onKey*/, option1, option2, message, newArgs...)
	}

	createOptionValueDiagnostic := func(option1 string, message *diagnostics.Message, args ...any) {
		createDiagnosticForOption(false /*onKey*/, option1, "", message, args...)
	}

	createRemovedOptionDiagnostic := func(name string, value string, useInstead string) {
		var message *diagnostics.Message
		var args []any
		if value == "" {
			message = diagnostics.Option_0_has_been_removed_Please_remove_it_from_your_configuration
			args = []any{name}
		} else {
			message = diagnostics.Option_0_1_has_been_removed_Please_remove_it_from_your_configuration
			args = []any{name, value}
		}

		diag := createDiagnosticForOption(value == "", name, "", message, args...)
		if useInstead != "" {
			diag.AddMessageChain(ast.NewCompilerDiagnostic(diagnostics.Use_0_instead, useInstead))
		}
	}

	// Removed in TS7

	if options.BaseUrl != "" {
		// BaseUrl will have been turned absolute by this point.
		var useInstead string
		if configFilePath() != "" {
			relative := tspath.GetRelativePathFromFile(configFilePath().AsString(), options.BaseUrl.AsString(), p.caseSensitivity)
			if !(strings.HasPrefix(relative, "./") || strings.HasPrefix(relative, "../")) {
				relative = "./" + relative
			}
			suggestion := tspath.CombinePaths(relative, "*")
			useInstead = fmt.Sprintf(`"paths": {"*": [%s]}`, core.Must(json.Marshal(suggestion)))
		}
		createRemovedOptionDiagnostic("baseUrl", "", useInstead)
	}

	if options.OutFile != "" {
		createRemovedOptionDiagnostic("outFile", "", "")
	}

	if options.Target == core.ScriptTargetES5 {
		createRemovedOptionDiagnostic("target", "ES5", "")
	}

	if options.Module == core.ModuleKindAMD {
		createRemovedOptionDiagnostic("module", "AMD", "")
	}
	if options.Module == core.ModuleKindSystem {
		createRemovedOptionDiagnostic("module", "System", "")
	}
	if options.Module == core.ModuleKindUMD {
		createRemovedOptionDiagnostic("module", "UMD", "")
	}

	if options.ModuleResolution == core.ModuleResolutionKindClassic {
		createRemovedOptionDiagnostic("moduleResolution", "Classic", "")
	}

	if options.AlwaysStrict.IsFalse() {
		createRemovedOptionDiagnostic("alwaysStrict", "false", "")
	}

	if options.ESModuleInterop.IsFalse() {
		createRemovedOptionDiagnostic("esModuleInterop", "false", "")
	}

	if options.AllowSyntheticDefaultImports.IsFalse() {
		createRemovedOptionDiagnostic("allowSyntheticDefaultImports", "false", "")
	}

	if options.ModuleResolution == core.ModuleResolutionKindNode10 {
		createRemovedOptionDiagnostic("moduleResolution", "node10", "")
	}

	if !options.DownlevelIteration.IsUnknown() {
		createRemovedOptionDiagnostic("downlevelIteration", "", "")
	}

	if options.StrictPropertyInitialization.IsTrue() && !options.GetStrictOptionValue(options.StrictNullChecks) {
		createDiagnosticForOptionName(diagnostics.Option_0_cannot_be_specified_without_specifying_option_1, "strictPropertyInitialization", "strictNullChecks")
	}
	if options.ExactOptionalPropertyTypes.IsTrue() && !options.GetStrictOptionValue(options.StrictNullChecks) {
		createDiagnosticForOptionName(diagnostics.Option_0_cannot_be_specified_without_specifying_option_1, "exactOptionalPropertyTypes", "strictNullChecks")
	}

	if options.IsolatedDeclarations.IsTrue() {
		if options.GetAllowJS() {
			createDiagnosticForOptionName(diagnostics.Option_0_cannot_be_specified_with_option_1, "allowJs", "isolatedDeclarations")
		}
		if !options.GetEmitDeclarations() {
			createDiagnosticForOptionName(diagnostics.Option_0_cannot_be_specified_without_specifying_option_1_or_option_2, "isolatedDeclarations", "declaration", "composite")
		}
	}

	if options.InlineSourceMap.IsTrue() {
		if options.SourceMap.IsTrue() {
			createDiagnosticForOptionName(diagnostics.Option_0_cannot_be_specified_with_option_1, "sourceMap", "inlineSourceMap")
		}
		if options.MapRoot != "" {
			createDiagnosticForOptionName(diagnostics.Option_0_cannot_be_specified_with_option_1, "mapRoot", "inlineSourceMap")
		}
	}

	if options.Composite.IsTrue() {
		if options.Declaration.IsFalse() {
			createDiagnosticForOptionName(diagnostics.Composite_projects_may_not_disable_declaration_emit, "declaration", "")
		}
		if options.Incremental.IsFalse() {
			createDiagnosticForOptionName(diagnostics.Composite_projects_may_not_disable_incremental_compilation, "declaration", "")
		}
	}

	if options.TsBuildInfoFile == "" && options.Incremental.IsTrue() && options.ConfigFilePath == "" {
		createCompilerOptionsDiagnostic(diagnostics.Option_incremental_is_only_valid_with_a_known_configuration_file_like_tsconfig_json_or_when_tsBuildInfoFile_is_explicitly_provided)
	}

	// verifyProjectReferences: the program has no project reference (addProjectReferenceTasks).

	if options.Composite.IsTrue() {
		var rootPaths collections.Set[tspath.PathKey]
		for _, fileName := range p.opts.Config.FileNames() {
			rootPaths.Add(p.caseSensitivity.PathKey(tspath.RootedPath(fileName)))
		}

		for _, file := range p.files {
			if sourceFileMayBeEmitted(file, p, false, false) && !rootPaths.Has(file.PathKey()) {
				p.addProcessingDiagnostic(&processingDiagnostic{
					kind: processingDiagnosticKindExplainingFileInclude,
					explanation: &includeExplainingDiagnostic{
						file:    file.PathKey(),
						message: diagnostics.File_0_is_not_listed_within_the_file_list_of_project_1_Projects_must_list_all_files_or_use_an_include_pattern,
						args:    []string{file.FileName().AsString(), configFilePath().AsString()},
					},
				})
			}
		}
	}

	forEachOptionPathsSyntax := func(callback func(*ast.PropertyAssignment) *ast.Diagnostic) *ast.Diagnostic {
		return tsoptions.ForEachPropertyAssignment(getCompilerOptionsObjectLiteralSyntax(), "paths", callback)
	}

	createDiagnosticForOptionPaths := func(onKey bool, key string, message *diagnostics.Message, args ...any) *ast.Diagnostic {
		diag := forEachOptionPathsSyntax(func(pathProp *ast.PropertyAssignment) *ast.Diagnostic {
			if ast.IsObjectLiteralExpression(pathProp.Initializer) {
				return createOptionDiagnosticInObjectLiteralSyntax(pathProp.Initializer.AsObjectLiteralExpression(), onKey, key, "", message, args...)
			}
			return nil
		})
		if diag == nil {
			diag = createCompilerOptionsDiagnostic(message, args...)
		}
		return diag
	}

	createDiagnosticForOptionPathKeyValue := func(key string, valueIndex int, message *diagnostics.Message, args ...any) *ast.Diagnostic {
		diag := forEachOptionPathsSyntax(func(pathProp *ast.PropertyAssignment) *ast.Diagnostic {
			if ast.IsObjectLiteralExpression(pathProp.Initializer) {
				return tsoptions.ForEachPropertyAssignment(pathProp.Initializer.AsObjectLiteralExpression(), key, func(keyProps *ast.PropertyAssignment) *ast.Diagnostic {
					initializer := keyProps.Initializer
					if ast.IsArrayLiteralExpression(initializer) {
						elements := initializer.ElementList()
						if elements != nil && len(elements.Nodes) > valueIndex {
							diag := tsoptions.CreateDiagnosticForNodeInSourceFile(sourceFile(), elements.Nodes[valueIndex], message, args...)
							programDiagnostics = append(programDiagnostics, diag)
							return diag
						}
					}
					return nil
				})
			}
			return nil
		})
		if diag == nil {
			diag = createCompilerOptionsDiagnostic(message, args...)
		}
		return diag
	}

	for key, value := range options.Paths.Entries() {
		// !!! This code does not handle cases where where the path mappings have the wrong types,
		// as that information is mostly lost during the parsing process.
		if !hasZeroOrOneAsteriskCharacter(key) {
			createDiagnosticForOptionPaths(true /*onKey*/, key, diagnostics.Pattern_0_can_have_at_most_one_Asterisk_character, key)
		}
		if value == nil {
			createDiagnosticForOptionPaths(false /*onKey*/, key, diagnostics.Substitutions_for_pattern_0_should_be_an_array, key)
		} else if len(value) == 0 {
			createDiagnosticForOptionPaths(false /*onKey*/, key, diagnostics.Substitutions_for_pattern_0_shouldn_t_be_an_empty_array, key)
		}
		for i, subst := range value {
			if !hasZeroOrOneAsteriskCharacter(subst) {
				createDiagnosticForOptionPathKeyValue(key, i, diagnostics.Substitution_0_in_pattern_1_can_have_at_most_one_Asterisk_character, subst, key)
			}
			if !tspath.PathIsRelative(subst) && !tspath.PathIsAbsolute(subst) {
				createDiagnosticForOptionPathKeyValue(key, i, diagnostics.Non_relative_paths_are_not_allowed_Did_you_forget_a_leading_Slash)
			}
		}
	}

	if options.SourceMap.IsFalseOrUnknown() && options.InlineSourceMap.IsFalseOrUnknown() {
		if options.InlineSources.IsTrue() {
			createDiagnosticForOptionName(diagnostics.Option_0_can_only_be_used_when_either_option_inlineSourceMap_or_option_sourceMap_is_provided, "inlineSources", "")
		}
		if options.SourceRoot != "" {
			createDiagnosticForOptionName(diagnostics.Option_0_can_only_be_used_when_either_option_inlineSourceMap_or_option_sourceMap_is_provided, "sourceRoot", "")
		}
	}

	if options.MapRoot != "" && !(options.SourceMap.IsTrue() || options.DeclarationMap.IsTrue()) {
		// Error to specify --mapRoot without --sourcemap
		createDiagnosticForOptionName(diagnostics.Option_0_cannot_be_specified_without_specifying_option_1_or_option_2, "mapRoot", "sourceMap", "declarationMap")
	}

	if options.DeclarationDir != "" {
		if !options.GetEmitDeclarations() {
			createDiagnosticForOptionName(diagnostics.Option_0_cannot_be_specified_without_specifying_option_1_or_option_2, "declarationDir", "declaration", "composite")
		}
	}

	if options.DeclarationMap.IsTrue() && !options.GetEmitDeclarations() {
		createDiagnosticForOptionName(diagnostics.Option_0_cannot_be_specified_without_specifying_option_1_or_option_2, "declarationMap", "declaration", "composite")
	}

	if options.Lib != nil && options.NoLib.IsTrue() {
		createDiagnosticForOptionName(diagnostics.Option_0_cannot_be_specified_with_option_1, "lib", "noLib")
	}

	if options.IsolatedModules.IsTrue() || options.VerbatimModuleSyntax.IsTrue() {
		if options.PreserveConstEnums.IsFalse() {
			createDiagnosticForOptionName(diagnostics.Option_preserveConstEnums_cannot_be_disabled_when_0_is_enabled, core.IfElse(options.VerbatimModuleSyntax.IsTrue(), "verbatimModuleSyntax", "isolatedModules"), "preserveConstEnums")
		}
	}

	if options.OutDir != "" ||
		options.RootDir != "" ||
		options.SourceRoot != "" ||
		options.MapRoot != "" ||
		(options.GetEmitDeclarations() && options.DeclarationDir != "") {
		// !!! sheetal checkSourceFilesBelongToPath - for root Dir and configFile - explaining why file is in the program
		dir := p.CommonSourceDirectory()
		if options.OutDir != "" && dir == "" && core.Some(p.files, func(f *store.File) bool { return f.FileName().RootLength() > 1 }) {
			createDiagnosticForOptionName(diagnostics.Cannot_find_the_common_subdirectory_path_for_the_input_files, "outDir", "")
		}
	}

	if !options.NoEmit.IsTrue() &&
		!options.Composite.IsTrue() &&
		options.RootDir == "" &&
		options.ConfigFilePath != "" &&
		(options.OutDir != "" ||
			(options.GetEmitDeclarations() && options.DeclarationDir != "") ||
			options.OutFile != "") {
		// Check if rootDir inferred changed and issue diagnostic
		dir := p.CommonSourceDirectory()
		var emittedFiles []tspath.RootedFilePath
		for _, file := range p.files {
			if !file.IsDeclarationFile && sourceFileMayBeEmitted(file, p, false, false) {
				emittedFiles = append(emittedFiles, file.FileName())
			}
		}
		dir59 := outputpaths.GetComputedCommonSourceDirectory(emittedFiles, p.BaseDirectory(), p.CaseSensitivity())
		if dir59 != "" && p.caseSensitivity.ComparePaths(dir.AsPath(), dir59.AsPath()) != 0 {
			// change in layout
			var option1 string
			if options.OutFile != "" {
				option1 = "outFile"
			} else if options.OutDir != "" {
				option1 = "outDir"
			} else {
				option1 = "declarationDir"
			}
			var option2 string
			if options.OutFile == "" && options.OutDir != "" {
				option2 = "declarationDir"
			}
			commonSourceDirectory := dir59.AsString()
			if relativePath, ok := p.caseSensitivity.RelativePathFromFileToPath(
				options.ConfigFilePath,
				dir59.AsPath(),
			); ok {
				commonSourceDirectory = relativePath.AsModuleSpecifier().AsString()
			}
			diag := createDiagnosticForOption(
				true, /*onKey*/
				option1,
				option2,
				diagnostics.The_common_source_directory_of_0_is_1_The_rootDir_setting_must_be_explicitly_set_to_this_or_another_path_to_adjust_your_output_s_file_layout,
				options.ConfigFilePath.BaseName(),
				commonSourceDirectory,
			)
			diag.AddMessageChain(ast.NewCompilerDiagnostic(diagnostics.Visit_https_Colon_Slash_Slashaka_ms_Slashts6_for_migration_information))
		}
	}

	if options.CheckJs.IsTrue() && !options.GetAllowJS() {
		createDiagnosticForOptionName(diagnostics.Option_0_cannot_be_specified_without_specifying_option_1, "checkJs", "allowJs")
	}

	if options.EmitDeclarationOnly.IsTrue() {
		if !options.GetEmitDeclarations() {
			createDiagnosticForOptionName(diagnostics.Option_0_cannot_be_specified_without_specifying_option_1_or_option_2, "emitDeclarationOnly", "declaration", "composite")
		}
	}

	if options.EmitDecoratorMetadata.IsTrue() && options.ExperimentalDecorators.IsFalseOrUnknown() {
		createDiagnosticForOptionName(diagnostics.Option_0_cannot_be_specified_without_specifying_option_1, "emitDecoratorMetadata", "experimentalDecorators")
	}

	if options.JsxFactory != "" {
		if options.ReactNamespace != "" {
			createDiagnosticForOptionName(diagnostics.Option_0_cannot_be_specified_with_option_1, "reactNamespace", "jsxFactory")
		}
		if options.Jsx == core.JsxEmitReactJSX || options.Jsx == core.JsxEmitReactJSXDev {
			createDiagnosticForOptionName(diagnostics.Option_0_cannot_be_specified_when_option_jsx_is_1, "jsxFactory", options.Jsx.String())
		}
		if isolatedEntityNameIsNil(options.JsxFactory) {
			createOptionValueDiagnostic("jsxFactory", diagnostics.Invalid_value_for_jsxFactory_0_is_not_a_valid_identifier_or_qualified_name, options.JsxFactory)
		}
	} else if options.ReactNamespace != "" && !scanner.IsIdentifierText(options.ReactNamespace, core.LanguageVariantStandard) {
		createOptionValueDiagnostic("reactNamespace", diagnostics.Invalid_value_for_reactNamespace_0_is_not_a_valid_identifier, options.ReactNamespace)
	}

	if options.JsxFragmentFactory != "" {
		if options.JsxFactory == "" {
			createDiagnosticForOptionName(diagnostics.Option_0_cannot_be_specified_without_specifying_option_1, "jsxFragmentFactory", "jsxFactory")
		}
		if options.Jsx == core.JsxEmitReactJSX || options.Jsx == core.JsxEmitReactJSXDev {
			createDiagnosticForOptionName(diagnostics.Option_0_cannot_be_specified_when_option_jsx_is_1, "jsxFragmentFactory", options.Jsx.String())
		}
		if isolatedEntityNameIsNil(options.JsxFragmentFactory) {
			createOptionValueDiagnostic("jsxFragmentFactory", diagnostics.Invalid_value_for_jsxFragmentFactory_0_is_not_a_valid_identifier_or_qualified_name, options.JsxFragmentFactory)
		}
	}

	if options.ReactNamespace != "" {
		if options.Jsx == core.JsxEmitReactJSX || options.Jsx == core.JsxEmitReactJSXDev {
			createDiagnosticForOptionName(diagnostics.Option_0_cannot_be_specified_when_option_jsx_is_1, "reactNamespace", options.Jsx.String())
		}
	}

	if options.JsxImportSource != "" {
		if options.Jsx == core.JsxEmitReact {
			createDiagnosticForOptionName(diagnostics.Option_0_cannot_be_specified_when_option_jsx_is_1, "jsxImportSource", options.Jsx.String())
		}
	}

	moduleKind := options.GetEmitModuleKind()

	if options.AllowImportingTsExtensions.IsTrue() && !(options.NoEmit.IsTrue() || options.EmitDeclarationOnly.IsTrue() || options.RewriteRelativeImportExtensions.IsTrue()) {
		createOptionValueDiagnostic("allowImportingTsExtensions", diagnostics.Option_allowImportingTsExtensions_can_only_be_used_when_one_of_noEmit_emitDeclarationOnly_or_rewriteRelativeImportExtensions_is_set)
	}

	moduleResolution := options.GetModuleResolutionKind()
	if options.ResolvePackageJsonExports.IsTrue() && !moduleResolutionSupportsPackageJsonExportsAndImports(moduleResolution) {
		createDiagnosticForOptionName(diagnostics.Option_0_can_only_be_used_when_moduleResolution_is_set_to_node16_nodenext_or_bundler, "resolvePackageJsonExports", "")
	}
	if options.ResolvePackageJsonImports.IsTrue() && !moduleResolutionSupportsPackageJsonExportsAndImports(moduleResolution) {
		createDiagnosticForOptionName(diagnostics.Option_0_can_only_be_used_when_moduleResolution_is_set_to_node16_nodenext_or_bundler, "resolvePackageJsonImports", "")
	}
	if options.CustomConditions != nil && !moduleResolutionSupportsPackageJsonExportsAndImports(moduleResolution) {
		createDiagnosticForOptionName(diagnostics.Option_0_can_only_be_used_when_moduleResolution_is_set_to_node16_nodenext_or_bundler, "customConditions", "")
	}

	if moduleResolution == core.ModuleResolutionKindBundler && !emitModuleKindIsNonNodeESM(moduleKind) && moduleKind != core.ModuleKindPreserve && moduleKind != core.ModuleKindCommonJS {
		createOptionValueDiagnostic("moduleResolution", diagnostics.Option_0_can_only_be_used_when_module_is_set_to_preserve_commonjs_or_es2015_or_later, "bundler")
	}

	if core.ModuleKindNode16 <= moduleKind && moduleKind <= core.ModuleKindNodeNext &&
		!(core.ModuleResolutionKindNode16 <= moduleResolution && moduleResolution <= core.ModuleResolutionKindNodeNext) {
		moduleKindName := moduleKind.String()
		var moduleResolutionName string
		if v, ok := core.ModuleKindToModuleResolutionKind[moduleKind]; ok {
			moduleResolutionName = v.String()
		} else {
			moduleResolutionName = "Node16"
		}
		createOptionValueDiagnostic("moduleResolution", diagnostics.Option_moduleResolution_must_be_set_to_0_or_left_unspecified_when_option_module_is_set_to_1, moduleResolutionName, moduleKindName)
	} else if core.ModuleResolutionKindNode16 <= moduleResolution && moduleResolution <= core.ModuleResolutionKindNodeNext &&
		!(core.ModuleKindNode16 <= moduleKind && moduleKind <= core.ModuleKindNodeNext) {
		moduleResolutionName := moduleResolution.String()
		createOptionValueDiagnostic("module", diagnostics.Option_module_must_be_set_to_0_when_option_moduleResolution_is_set_to_1, moduleResolutionName, moduleResolutionName)
	}

	// !!! The below needs filesByName, which is not equivalent to p.filesByPath.

	// The emit paths are not verified: the CLI requires --noEmit for the Store
	// program, and outputpaths.ForEachEmittedFile takes Pointer files.
	if !options.NoEmit.IsTrue() && !options.SuppressOutputPathCheck.IsTrue() {
		panic("storecompiler: the output paths of an emitting program are not verified; the CLI requires noEmit")
	}
}
