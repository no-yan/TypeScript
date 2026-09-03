package tsoptions

import (
	"cmp"
	"github.com/microsoft/TypeScript/tsc/internal/ast"
	"github.com/microsoft/TypeScript/tsc/internal/collections"
	"github.com/microsoft/TypeScript/tsc/internal/contentmapper"
	"github.com/microsoft/TypeScript/tsc/internal/core"
	"github.com/microsoft/TypeScript/tsc/internal/debug"
	"github.com/microsoft/TypeScript/tsc/internal/diagnostics"
	"github.com/microsoft/TypeScript/tsc/internal/jsnum"
	"github.com/microsoft/TypeScript/tsc/internal/locale"
	"github.com/microsoft/TypeScript/tsc/internal/module"
	"github.com/microsoft/TypeScript/tsc/internal/parser"
	"github.com/microsoft/TypeScript/tsc/internal/scanner"
	"github.com/microsoft/TypeScript/tsc/internal/tspath"
	"github.com/microsoft/TypeScript/tsc/internal/vfs"
	"github.com/microsoft/TypeScript/tsc/internal/vfs/vfsmatch"
	"maps"
	"reflect"
	"slices"
	"strings"
)

type extendsResult struct {
	options *core.CompilerOptions
	include []any // should never be needed since this is root
	// should be a list of projectReference
	// Category: diagnostics.Projects,
	// list of content mapper objects
	// Category: diagnostics.File_Management,
	// Category: diagnostics.File_Management,
	// DefaultValueDescription: diagnostics.if_files_is_specified_otherwise_Asterisk_Asterisk_Slash_Asterisk,
	// Category: diagnostics.File_Management,
	// DefaultValueDescription: diagnostics.Node_modules_bower_components_jspm_packages_plus_the_value_of_outDir_if_one_is_specified,
	// Present to report errors (user specified specs), validatedIncludeSpecs are used for file name matching
	// Present to report errors (user specified specs), validatedExcludeSpecs are used for file name matching
	// Note that the case of the config path has not yet been normalized, as no files have been imported into the project yet
	// TsConfigOnlyOption,
	// Ensure value is verified except for extends which is handled in its own way for error reporting
	/*unknownOptionErrorText*/ /*alternateMode*/ /*unknownDidYouMeanDiagnostic*/ /*optionsNameMap*/ // errors = append(errors, ast.NewCompilerDiagnostic(diagnostics.Unknown_compiler_option_0_Did_you_mean_1, keyText, core.FindKey(parentOption.ElementOptions, keyText)))
	// Last-ditch error recovery. Somewhat useful because the JSON parser will recover from some parse errors by
	// synthesizing a top-level array literal expression. There's a reasonable chance the first element of that
	// array is a well-formed configuration object, made into an array element by stray characters.
	/*returnValue*/ // as ArrayLiteralExpression | undefined
	// If the path isn't a rooted or relative path, resolve like a module
	// this assumes any `key`, `value` pair in `options` will have `value` already be the correct type. this function should no error handling
	// !!! probably should be an error
	// Case-insensitive match found but exact case doesn't match - provide "did you mean" suggestion
	// Filter out invalid values
	// Always return an empty array, even if elements is nil.
	// The parser will produce nil slices instead of allocating empty ones.
	// Use the `getNormalizedAbsolutePath` function to avoid canonicalizing the path, as it must remain noncanonical
	// until consistent casing errors are reported
	// ParseConfigFileTextToJson parses the text of the tsconfig.json file
	// fileName is the path to the config file
	// jsonText is the text of the config file
	/*jsonConversionNotifier*/ // tracing?.push(tracing.Phase.Parse, "parseJsonSourceFileConfigFileContent", { path: sourceFile.fileName });
	/*json*/ // tracing?.pop();
	// Notify key value set, if user asked for it
	// convertToJson converts the json syntax tree into the json value and report errors
	// This returns the json value (apart from checking errors) only if returnValue provided is true.
	// Otherwise it just checks the errors and returns undefined
	// todo: how to manage null
	// not valid JSON syntax
	// Currently having element option declaration in the tsconfig with type "object"
	// determines if it needs onSetValidOptionKeyValueInParent callback or not
	// At moment there are only "compilerOptions", "typeAcquisition" and "typingOptions"
	// that satisfies it and need it to modify options set in them (for normalizing file paths)
	// vs what we set in the json
	// If need arises, we can modify this interface and callbacks as needed
	// Not in expected format
	// ParseJsonConfigFileContent parses the contents of a config file (tsconfig.json).
	// jsonNode: The contents of the config file to parse
	// host: Instance of ParseConfigHost used to enumerate files in folder.
	// basePath: A root directory to resolve relative path entries in the config file to. e.g. outDir
	/*sourceFile*/ /*existingOptionsRaw*/ // convertToObject converts the json syntax tree into the json value
	/*returnValue*/ /*jsonConversionNotifier*/ // Bypass the cache when we detect a cycle in the resolution stack.
	// The cache locks entries during parsing, and a cycle would cause the same goroutine
	// to re-lock the same entry, resulting in a deadlock. Let parseConfig handle the
	// circularity error via its own resolution stack check.
	// parseConfig just extracts options/include/exclude/files out of a config file.
	// It does not resolve the included files.
	// If we end up needing to resolve relative paths from 'paths' relative to
	// the config file location, we'll need to know where that config file was.
	// Since 'paths' can be inherited from an extended config in another directory,
	// we wouldn't know which directory to use unless we store it here.
	// copy the resolution stack so it is never reused between branches in potential diamond-problem scenarios.
	// parseJsonConfigFileContentWorker parses the contents of a config file from json or json source file (tsconfig.json).
	// json: The contents of the config file to parse
	// sourceFile: sourceFile corresponding to the Json
	// host: Instance of ParseConfigHost used to enumerate files in folder.
	// basePath: A root directory to resolve relative path entries in the config file to. e.g. outDir
	// resolutionStack: Only present for backwards-compatibility. Should be empty.
	// The exclude spec list is converted into a regular expression, which allows us to quickly
	// test whether a file or directory should be excluded before recursively traversing the
	// file system.
	/*disallowTrailingRecursion*/ /*disallowTrailingRecursion*/ // Without the flag the mappers are not trusted to run, so drop them entirely: their extensions are
	// not registered and their files are not intercepted (they are treated as unknown foreign files).
	// Resolve each mapper's package.json now so its name, version, and run command are available to
	// everything downstream (diagnostics, build-info staleness) without executing anything.
	// Matches **, /**, **/, and /**/, but not a**b.
	// Strip optional trailing slash, then check if it ends with /** or is just **
	// We used to use the regex /(^|\/)\*\*\/(.*\/)?\.\.($|\/)/ to check for this case, but
	// in v8, that has polynomial performance because the recursive wildcard match - **/ -
	// can be matched in many arbitrary positions when multiple are present, resulting
	// in bad backtracking (and we don't care which is matched - just that some /.. segment
	// comes after some **/ segment).
	// getContentMapperSyntax returns the tsconfig JSON node to attribute a diagnostic about the content
	// mapper at index to: the value of subKey within that mapper's object (when subKey is non-empty),
	// falling back to the mapper element, then to the "contentMappers" array. An index outside the array
	// (e.g. -1) yields the array itself. Returns nil when there is no source file (JSON API).
	// getContentMappersKeySyntax returns the "contentMappers" property key node, used to attribute a
	// diagnostic about the setting as a whole rather than a specific mapper.
	// getContentMapperExtensionSyntax returns the node for a specific extension string within the content
	// mapper at index, falling back to the "extensions" array or the mapper element.
	// setContentMapperDiagnosticLocation attaches a source location to a content mapper diagnostic when a
	// tsconfig source file and node are available (the jsonSourceFile API), leaving it as a location-less
	// compiler diagnostic otherwise (the JSON API).
	// !!! don't hardcode this; use options declarations?
	// hasFileWithHigherPriorityExtension determines whether a literal or wildcard file has already been included that has a higher extension priority.
	// file is the path to the file.
	// d.ts files match with .ts extension and with case sensitive sorting the file order for same files with ts tsx and dts extension is
	// d.ts, .ts, .tsx in that order so we need to handle tsx and dts of same same name case here and in remove files with same extensions
	// So dont match .d.ts files with .ts extension
	// LEGACY BEHAVIOR: An off-by-one bug somewhere in the extension priority system for wildcard module loading allowed declaration
	// files to be loaded alongside their js(x) counterparts. We regard this as generally undesirable, but retain the behavior to
	// prevent breakage.
	// Removes files included via wildcard expansion with a lower extension priority that have already been included.
	// file is the path to the file.
	// getFileNamesFromConfigSpecs gets the file names from the provided config file specs that contain, files, include, exclude and
	// other properties needed to resolve the file names
	// configFileSpecs is the config file specs extracted with file names to include, wildcards to include/exclude and other details
	// basePath is the base path for any relative file specifications.
	// options is the Compiler options.
	// host is the host used to resolve files and directories.
	// extraExtensions are additional file extensions (e.g. from content mappers) to treat as supported.
	// considering this is the current directory
	// Literal file names (provided via the "files" array in tsconfig.json) are stored in a
	// file map with a possibly case insensitive key. We use this map later when when including
	// wildcard paths.
	// Wildcard paths (provided via the "includes" array in tsconfig.json) are stored in a
	// file map with a possibly case insensitive key. We use this map to store paths matched
	// via wildcard, and to handle extension priority.
	// Wildcard paths of json files (provided via the "includes" array in tsconfig.json) are stored in a
	// file map with a possibly case insensitive key. We use this map to store paths matched
	// via wildcard of *.json kind
	// Rather than re-query this for each file and filespec, we query the supported extensions
	// once and store it on the expansion context.
	// Literal files are always included verbatim. An "include" or "exclude" specification cannot
	// remove a literal file.
	// If we have already included a literal or wildcard path with a
	// higher priority extension, we should skip this file.
	//
	// This handles cases where we may encounter both <file>.ts and
	// <file>.d.ts (or <file>.js if "allowJs" is enabled) in the same
	// directory when they are compilation outputs.
	// We may have included a wildcard path with a lower priority
	// extension due to the user-defined order of entries in the
	// "include" array. If there is a lower priority extension in the
	// same directory, we should remove it.
	// Reads the config file and reports errors.
	// these are unrecoverable errors--exit to report them as diagnostics
	// tsConfigSourceFile.resolvedPath = tsConfigSourceFile.FileName()

	// tsConfigSourceFile.originalFileName = tsConfigSourceFile.FileName()

	exclude             []any
	files               []any
	contentMappers      []any
	compileOnSave       bool
	extendedSourceFiles collections.Set[string]
}

var compilerOptionsDeclaration = &CommandLineOption{Name: "compilerOptions", Kind: CommandLineOptionTypeObject, ElementOptions: CommandLineCompilerOptionsMap}
var compileOnSaveCommandLineOption = &CommandLineOption{Name: "compileOnSave", Kind: CommandLineOptionTypeBoolean, DefaultValueDescription: false}
var extendsOptionDeclaration = &CommandLineOption{Name: "extends", Kind: CommandLineOptionTypeListOrElement, Category: diagnostics.File_Management, ElementOptions: commandLineOptionsToMap([]*CommandLineOption{{Name: "extends", Kind: CommandLineOptionTypeString}})}
var tsconfigRootOptionsMap = &CommandLineOption{Name: "undefined", Kind: CommandLineOptionTypeObject, ElementOptions: commandLineOptionsToMap([]*CommandLineOption{compilerOptionsDeclaration, typeAcquisitionDeclaration, extendsOptionDeclaration, {Name: "references", Kind: CommandLineOptionTypeList}, {Name: "contentMappers", Kind: CommandLineOptionTypeList}, {Name: "files", Kind: CommandLineOptionTypeList}, {Name: "include", Kind: CommandLineOptionTypeList}, {Name: "exclude", Kind: CommandLineOptionTypeList}, compileOnSaveCommandLineOption})}

type configFileSpecs struct {
	filesSpecs                              any
	includeSpecs                            any
	excludeSpecs                            any
	validatedFilesSpec                      []string
	validatedIncludeSpecs                   []string
	validatedExcludeSpecs                   []string
	validatedFilesSpecBeforeSubstitution    []string
	validatedIncludeSpecsBeforeSubstitution []string
	isDefaultIncludeSpec                    bool
}

func (c *configFileSpecs) matchesExclude(fileName string, comparePathsOptions tspath.ComparePathsOptions) bool {
	if len(c.validatedExcludeSpecs) == 0 {
		return false
	}
	excludeMatcher := vfsmatch.NewSpecMatcher(c.validatedExcludeSpecs, comparePathsOptions.CurrentDirectory, vfsmatch.UsageExclude, comparePathsOptions.UseCaseSensitiveFileNames)
	if excludeMatcher == nil {
		return false
	}
	if excludeMatcher.MatchString(fileName) {
		return true
	}
	if !tspath.HasExtension(fileName) {
		if excludeMatcher.MatchString(tspath.EnsureTrailingDirectorySeparator(fileName)) {
			return true
		}
	}
	return false
}
func (c *configFileSpecs) getMatchedIncludeSpec(fileName string, comparePathsOptions tspath.ComparePathsOptions) string {
	if len(c.validatedIncludeSpecs) == 0 {
		return ""
	}
	for index, spec := range c.validatedIncludeSpecs {
		includeMatcher := vfsmatch.NewSpecMatcher([]string{spec}, comparePathsOptions.CurrentDirectory, vfsmatch.UsageFiles, comparePathsOptions.UseCaseSensitiveFileNames)
		if includeMatcher != nil && includeMatcher.MatchString(fileName) {
			return c.validatedIncludeSpecsBeforeSubstitution[index]
		}
	}
	return ""
}
func (c *configFileSpecs) getMatchedFileSpec(fileName string, comparePathsOptions tspath.ComparePathsOptions) string {
	if len(c.validatedFilesSpec) == 0 {
		return ""
	}
	filePath := tspath.ToPath(fileName, comparePathsOptions.CurrentDirectory, comparePathsOptions.UseCaseSensitiveFileNames)
	for index, spec := range c.validatedFilesSpec {
		if tspath.ToPath(spec, comparePathsOptions.CurrentDirectory, comparePathsOptions.UseCaseSensitiveFileNames) == filePath {
			return c.validatedFilesSpecBeforeSubstitution[index]
		}
	}
	return ""
}

type ExtendedConfigCache interface {
	GetExtendedConfig(fileName string, path tspath.Path, resolutionStack []tspath.Path, host ParseConfigHost) *ExtendedConfigCacheEntry
}
type ExtendedConfigCacheEntry struct {
	extendedResult *TsConfigSourceFile
	extendedConfig *parsedTsconfig
	errors         []*ast.Diagnostic
}

func (e *ExtendedConfigCacheEntry) ExtendedFileNames() []string {
	if e.extendedResult != nil {
		return e.extendedResult.ExtendedSourceFiles
	}
	return nil
}

type parsedTsconfig struct {
	raw                any
	options            *core.CompilerOptions
	typeAcquisition    *core.TypeAcquisition
	extendedConfigPath any
}

func parseOwnConfigOfJsonSourceFile(sourceFile *ast.SourceFile, host ParseConfigHost, basePath string, configFileName string) (*parsedTsconfig, []*ast.Diagnostic) {
	compilerOptions := getDefaultCompilerOptions(configFileName)
	typeAcquisition := getDefaultTypeAcquisition(configFileName)
	var extendedConfigPath any
	var rootCompilerOptions []ast.Handle
	var errors []*ast.Diagnostic
	onPropertySet := func(keyText string, value any, propertyAssignment ast.Handle, parentOption *CommandLineOption, option *CommandLineOption) (any, []*ast.Diagnostic) {
		var propertySetErrors []*ast.Diagnostic
		if option != nil && option != extendsOptionDeclaration {
			value, propertySetErrors = convertJsonOption(option, value, basePath, propertyAssignment, propertyAssignment.Initializer(), sourceFile)
		}
		if parentOption != nil && parentOption.Name != "undefined" && value != nil {
			if option != nil && option.Name != "" {
				var parseDiagnostics []*ast.Diagnostic
				switch parentOption.Name {
				case "compilerOptions":
					parseDiagnostics = ParseCompilerOptions(option.Name, value, compilerOptions)
				case "typeAcquisition":
					parseDiagnostics = ParseTypeAcquisition(option.Name, value, typeAcquisition)
				}
				propertySetErrors = append(propertySetErrors, parseDiagnostics...)
			} else if keyText != "" && extraKeyDiagnostics(parentOption.Name) != nil {
				unknownNameDiag := extraKeyDiagnostics(parentOption.Name)
				if parentOption.ElementOptions != nil {
					possibleOption := parentOption.ElementOptions.Get(keyText)
					if possibleOption == nil {
						possibleOption = parentOption.ElementOptions.GetSpellingSuggestion(keyText)
					}
					if possibleOption != nil && possibleOption.Name != keyText {
						propertySetErrors = append(propertySetErrors, CreateDiagnosticForNodeInSourceFileOrCompilerDiagnostic(sourceFile, propertyAssignment.Name(), extraKeyDidYouMeanDiagnostics(parentOption.Name), keyText, possibleOption.Name))
					} else {
						propertySetErrors = append(propertySetErrors, createUnknownOptionError(keyText, unknownNameDiag, "", propertyAssignment.Name(), sourceFile, nil, nil, nil))
					}
				} else {
				}
			}
		} else if parentOption == tsconfigRootOptionsMap {
			if option == extendsOptionDeclaration {
				configPath, err := getExtendsConfigPathOrArray(value, host, basePath, configFileName, propertyAssignment, propertyAssignment.Initializer(), sourceFile)
				extendedConfigPath = configPath
				propertySetErrors = append(propertySetErrors, err...)
			} else if option == nil {
				if keyText == "excludes" {
					propertySetErrors = append(propertySetErrors, CreateDiagnosticForNodeInSourceFile(sourceFile, propertyAssignment.Name(), diagnostics.Unknown_option_excludes_Did_you_mean_exclude))
				}
				if core.Find(optionsForCompiler, func(option *CommandLineOption) bool {
					return option.Name == keyText
				}) != nil {
					rootCompilerOptions = append(rootCompilerOptions, propertyAssignment.Name())
				}
			}
		}
		return value, propertySetErrors
	}
	json, err := convertConfigFileToObject(sourceFile, &jsonConversionNotifier{tsconfigRootOptionsMap, onPropertySet})
	errors = append(errors, err...)
	if jsonObject, ok := json.(*collections.OrderedMap[string, any]); len(rootCompilerOptions) != 0 && ok && !jsonObject.Has("compilerOptions") {
		errors = append(errors, CreateDiagnosticForNodeInSourceFile(sourceFile, rootCompilerOptions[0], diagnostics.X_0_should_be_set_inside_the_compilerOptions_object_of_the_config_json_file, ast.GetTextOfPropertyName(rootCompilerOptions[0])))
	}
	return &parsedTsconfig{raw: json, options: compilerOptions, typeAcquisition: typeAcquisition, extendedConfigPath: extendedConfigPath}, errors
}

type TsConfigSourceFile struct {
	ExtendedSourceFiles []string
	configFileSpecs     *configFileSpecs
	SourceFile          *ast.SourceFile
}

func tsconfigToSourceFile(tsconfigSourceFile *TsConfigSourceFile) *ast.SourceFile {
	if tsconfigSourceFile == nil {
		return nil
	}
	return tsconfigSourceFile.SourceFile
}
func NewTsconfigSourceFileFromFilePath(configFileName string, configPath tspath.Path, configSourceText string) *TsConfigSourceFile {
	sourceFile := parser.ParseSourceFile(ast.SourceFileParseOptions{FileName: configFileName, Path: configPath}, configSourceText, core.ScriptKindJSON)
	return &TsConfigSourceFile{SourceFile: sourceFile}
}

type jsonConversionNotifier struct {
	rootOptions   *CommandLineOption
	onPropertySet func(keyText string, value any, propertyAssignment ast.Handle, parentOption *CommandLineOption, option *CommandLineOption) (any, []*ast.Diagnostic)
}

func convertConfigFileToObject(sourceFile *ast.SourceFile, jsonConversionNotifier *jsonConversionNotifier) (any, []*ast.Diagnostic) {
	var rootExpression ast.Handle
	if len(sourceFile.ParseRoot().Statements()) > 0 {
		rootExpression = sourceFile.ParseRoot().Statements()[0].Expression()
	}
	if !rootExpression.IsNil() && rootExpression.Kind != ast.KindObjectLiteralExpression {
		baseFileName := "tsconfig.json"
		if tspath.GetBaseFileName(sourceFile.FileName()) == "jsconfig.json" {
			baseFileName = "jsconfig.json"
		}
		errors := []*ast.Diagnostic{CreateDiagnosticForNodeInSourceFile(sourceFile, rootExpression, diagnostics.The_root_value_of_a_0_file_must_be_an_object, baseFileName)}
		if ast.IsArrayLiteralExpression(rootExpression) {
			firstObject := core.Find(rootExpression.Elements(), ast.IsObjectLiteralExpression)
			if !firstObject.IsNil() {
				return convertToJson(sourceFile, firstObject, true, jsonConversionNotifier)
			}
		}
		return &collections.OrderedMap[string, any]{}, errors
	}
	return convertToJson(sourceFile, rootExpression, true, jsonConversionNotifier)
}

var orderedMapType = reflect.TypeFor[*collections.OrderedMap[string, any]]()

func isCompilerOptionsValue(option *CommandLineOption, value any) bool {
	if option != nil {
		if value == nil {
			return !option.DisallowNullOrUndefined()
		}
		if option.Kind == "list" {
			return reflect.TypeOf(value).Kind() == reflect.Slice
		}
		if option.Kind == "listOrElement" {
			if reflect.TypeOf(value).Kind() == reflect.Slice {
				return true
			} else {
				return isCompilerOptionsValue(option.Elements(), value)
			}
		}
		if option.Kind == "string" {
			return reflect.TypeOf(value).Kind() == reflect.String
		}
		if option.Kind == "boolean" {
			return reflect.TypeOf(value).Kind() == reflect.Bool
		}
		if option.Kind == "number" {
			return reflect.TypeOf(value).Kind() == reflect.Float64
		}
		if option.Kind == "object" {
			return reflect.TypeOf(value) == orderedMapType
		}
		if option.Kind == "enum" && reflect.TypeOf(value).Kind() == reflect.String {
			return true
		}
	}
	return false
}
func validateJsonOptionValue(opt *CommandLineOption, val any, valueExpression ast.Handle, sourceFile *ast.SourceFile) (any, []*ast.Diagnostic) {
	if val == nil {
		return nil, nil
	}
	var errors []*ast.Diagnostic
	switch opt.extraValidation {
	case extraValidationSpec:
		if diag := specToDiagnostic(val.(string), false); diag != nil {
			errors = append(errors, CreateDiagnosticForNodeInSourceFileOrCompilerDiagnostic(sourceFile, valueExpression, diag))
		}
	case extraValidationLocale:
		if _, ok := locale.Parse(val.(string)); !ok {
			errors = append(errors, CreateDiagnosticForNodeInSourceFileOrCompilerDiagnostic(sourceFile, valueExpression, diagnostics.Locale_must_be_an_IETF_BCP_47_language_tag_Examples_Colon_0_1, "en", "ja-jp"))
		}
	}
	if len(errors) > 0 {
		return nil, errors
	}
	return val, nil
}
func convertJsonOptionOfListType(option *CommandLineOption, values any, basePath string, propertyAssignment ast.Handle, valueExpression ast.Handle, sourceFile *ast.SourceFile) ([]any, []*ast.Diagnostic) {
	var expression ast.Handle
	var errors []*ast.Diagnostic
	if values, ok := values.([]any); ok {
		mappedValues := core.MapIndex(values, func(v any, index int) any {
			if !valueExpression.IsNil() {
				expression = valueExpression.Elements()[index]
			}
			result, err := convertJsonOption(option.Elements(), v, basePath, propertyAssignment, expression, sourceFile)
			errors = append(errors, err...)
			return result
		})
		filteredValues := mappedValues
		if !option.listPreserveFalsyValues {
			filteredValues = core.Filter(mappedValues, func(v any) bool {
				return (v != nil && v != false && v != 0 && v != "")
			})
		}
		return filteredValues, errors
	}
	return nil, errors
}

const configDirTemplate = "${configDir}"

func startsWithConfigDirTemplate(value any) bool {
	str, ok := value.(string)
	if !ok {
		return false
	}
	return strings.HasPrefix(strings.ToLower(str), strings.ToLower(configDirTemplate))
}
func normalizeNonListOptionValue(option *CommandLineOption, basePath string, value any) any {
	if option.IsFilePath {
		value = tspath.NormalizeSlashes(value.(string))
		if !startsWithConfigDirTemplate(value) {
			value = tspath.GetNormalizedAbsolutePath(value.(string), basePath)
		}
		if value == "" {
			value = "."
		}
	}
	return value
}
func convertJsonOption(opt *CommandLineOption, value any, basePath string, propertyAssignment ast.Handle, valueExpression ast.Handle, sourceFile *ast.SourceFile) (any, []*ast.Diagnostic) {
	if opt.IsCommandLineOnly {
		var nodeValue ast.Handle
		if !propertyAssignment.IsNil() {
			nodeValue = propertyAssignment.Name()
		}
		if sourceFile == nil && nodeValue.IsNil() {
			return nil, []*ast.Diagnostic{ast.NewCompilerDiagnostic(diagnostics.Option_0_can_only_be_specified_on_command_line, opt.Name)}
		} else {
			return nil, []*ast.Diagnostic{CreateDiagnosticForNodeInSourceFileOrCompilerDiagnostic(sourceFile, nodeValue, diagnostics.Option_0_can_only_be_specified_on_command_line, opt.Name)}
		}
	}
	if isCompilerOptionsValue(opt, value) {
		switch opt.Kind {
		case CommandLineOptionTypeList:
			return convertJsonOptionOfListType(opt, value, basePath, propertyAssignment, valueExpression, sourceFile)
		case CommandLineOptionTypeListOrElement:
			if reflect.TypeOf(value).Kind() == reflect.Slice {
				return convertJsonOptionOfListType(opt, value, basePath, propertyAssignment, valueExpression, sourceFile)
			} else {
				return convertJsonOption(opt.Elements(), value, basePath, propertyAssignment, valueExpression, sourceFile)
			}
		case CommandLineOptionTypeEnum:
			if value == nil {
				return nil, nil
			}
			return convertJsonOptionOfEnumType(opt, value.(string), valueExpression, sourceFile)
		}
		validatedValue, errors := validateJsonOptionValue(opt, value, valueExpression, sourceFile)
		if len(errors) > 0 || validatedValue == nil {
			return validatedValue, errors
		} else {
			return normalizeNonListOptionValue(opt, basePath, validatedValue), errors
		}
	} else {
		return nil, []*ast.Diagnostic{CreateDiagnosticForNodeInSourceFileOrCompilerDiagnostic(sourceFile, valueExpression, diagnostics.Compiler_option_0_requires_a_value_of_type_1, opt.Name, getCompilerOptionValueTypeString(opt))}
	}
}
func getExtendsConfigPathOrArray(value CompilerOptionsValue, host ParseConfigHost, basePath string, configFileName string, propertyAssignment ast.Handle, valueExpression ast.Handle, sourceFile *ast.SourceFile) ([]string, []*ast.Diagnostic) {
	var extendedConfigPathArray []string
	newBase := basePath
	if configFileName != "" {
		newBase = directoryOfCombinedPath(configFileName, basePath)
	}
	if value == nil {
		_, errors := convertJsonOption(extendsOptionDeclaration, value, basePath, propertyAssignment, valueExpression, sourceFile)
		return extendedConfigPathArray, errors
	}
	if reflect.TypeOf(value).Kind() == reflect.String {
		val, err := getExtendsConfigPath(value.(string), host, newBase, valueExpression, sourceFile)
		if val != "" {
			extendedConfigPathArray = append(extendedConfigPathArray, val)
		}
		return extendedConfigPathArray, err
	}
	var errors []*ast.Diagnostic
	if reflect.TypeOf(value).Kind() == reflect.Slice {
		for index, fileName := range value.([]any) {
			var expression ast.Handle
			if !valueExpression.IsNil() {
				expression = valueExpression.Elements()[index]
			}
			if reflect.TypeOf(fileName).Kind() == reflect.String {
				val, err := getExtendsConfigPath(fileName.(string), host, newBase, expression, sourceFile)
				if val != "" {
					extendedConfigPathArray = append(extendedConfigPathArray, val)
				}
				errors = append(errors, err...)
			} else {
				_, err := convertJsonOption(extendsOptionDeclaration.Elements(), value, basePath, propertyAssignment, expression, sourceFile)
				errors = append(errors, err...)
			}
		}
	} else {
		_, errors = convertJsonOption(extendsOptionDeclaration, value, basePath, propertyAssignment, valueExpression, sourceFile)
	}
	return extendedConfigPathArray, errors
}
func getExtendsConfigPath(extendedConfig string, host ParseConfigHost, basePath string, valueExpression ast.Handle, sourceFile *ast.SourceFile) (string, []*ast.Diagnostic) {
	extendedConfig = tspath.NormalizeSlashes(extendedConfig)
	var errors []*ast.Diagnostic
	var errorFile *ast.SourceFile
	if sourceFile != nil {
		errorFile = sourceFile
	}
	if tspath.IsRootedDiskPath(extendedConfig) || strings.HasPrefix(extendedConfig, "./") || strings.HasPrefix(extendedConfig, "../") {
		extendedConfigPath := tspath.GetNormalizedAbsolutePath(extendedConfig, basePath)
		if !host.FS().FileExists(extendedConfigPath) && !strings.HasSuffix(extendedConfigPath, tspath.ExtensionJson) {
			extendedConfigPath = extendedConfigPath + tspath.ExtensionJson
			if !host.FS().FileExists(extendedConfigPath) {
				errors = append(errors, CreateDiagnosticForNodeInSourceFileOrCompilerDiagnostic(errorFile, valueExpression, diagnostics.File_0_not_found, extendedConfig))
				return "", errors
			}
		}
		return extendedConfigPath, errors
	}
	resolverHost := &resolverHost{host}
	if resolved := module.ResolveConfig(extendedConfig, tspath.CombinePaths(basePath, "tsconfig.json"), resolverHost); resolved.IsResolved() {
		return resolved.ResolvedFileName, errors
	}
	if extendedConfig == "" {
		errors = append(errors, CreateDiagnosticForNodeInSourceFileOrCompilerDiagnostic(errorFile, valueExpression, diagnostics.Compiler_option_0_cannot_be_given_an_empty_string, "extends"))
	} else {
		errors = append(errors, CreateDiagnosticForNodeInSourceFileOrCompilerDiagnostic(errorFile, valueExpression, diagnostics.File_0_not_found, extendedConfig))
	}
	return "", errors
}

type tsConfigOptions struct {
	prop       map[string][]string
	references []*core.ProjectReference
	notDefined string
}
type CommandLineOptionNameMap map[string]*CommandLineOption

func (m CommandLineOptionNameMap) Get(name string) *CommandLineOption {
	opt, ok := m[name]
	if !ok {
		opt, _ = m[strings.ToLower(name)]
	}
	return opt
}
func (m CommandLineOptionNameMap) GetSpellingSuggestion(name string) *CommandLineOption {
	return core.GetSpellingSuggestion(name, maps.Values(m), func(option *CommandLineOption) string {
		return option.Name
	}, func(a *CommandLineOption, b *CommandLineOption) int {
		return strings.Compare(a.Name, b.Name)
	})
}
func commandLineOptionsToMap(compilerOptions []*CommandLineOption) CommandLineOptionNameMap {
	result := make(map[string]*CommandLineOption, len(compilerOptions)*2)
	for i := range compilerOptions {
		result[compilerOptions[i].Name] = compilerOptions[i]
		result[strings.ToLower(compilerOptions[i].Name)] = compilerOptions[i]
	}
	return result
}

var CommandLineCompilerOptionsMap CommandLineOptionNameMap = commandLineOptionsToMap(OptionsDeclarations)

func convertMapToOptions[O optionParser](compilerOptions *collections.OrderedMap[string, any], result O) O {
	for key, value := range compilerOptions.Entries() {
		result.ParseOption(key, value)
	}
	return result
}
func convertOptionsFromJson[O optionParser](optionsNameMap CommandLineOptionNameMap, jsonOptions any, basePath string, result O) (O, []*ast.Diagnostic) {
	if jsonOptions == nil {
		return result, nil
	}
	jsonMap, ok := jsonOptions.(*collections.OrderedMap[string, any])
	if !ok {
		return result, nil
	}
	var errors []*ast.Diagnostic
	for key, value := range jsonMap.Entries() {
		opt := optionsNameMap.Get(key)
		if opt != nil && opt.Name != key {
			errors = append(errors, CreateDiagnosticForNodeInSourceFileOrCompilerDiagnostic(nil, ast.Handle{}, result.UnknownDidYouMeanDiagnostic(), key, opt.Name))
			continue
		}
		if opt == nil {
			errors = append(errors, createUnknownOptionError(key, result.UnknownOptionDiagnostic(), "", ast.Handle{}, nil, nil, result.UnknownDidYouMeanDiagnostic(), optionsNameMap))
			continue
		}
		convertJson, err := convertJsonOption(opt, value, basePath, ast.Handle{}, ast.Handle{}, nil)
		errors = append(errors, err...)
		compilerOptionsErr := result.ParseOption(key, convertJson)
		errors = append(errors, compilerOptionsErr...)
	}
	return result, errors
}
func convertArrayLiteralExpressionToJson(sourceFile *ast.SourceFile, elements []ast.Handle, elementOption *CommandLineOption, returnValue bool) (any, []*ast.Diagnostic) {
	if !returnValue {
		for _, element := range elements {
			convertPropertyValueToJson(sourceFile, element, elementOption, returnValue, nil)
		}
		return nil, nil
	}
	if len(elements) == 0 {
		return []any{}, nil
	}
	var errors []*ast.Diagnostic
	var value []any
	for _, element := range elements {
		convertedValue, err := convertPropertyValueToJson(sourceFile, element, elementOption, returnValue, nil)
		errors = append(errors, err...)
		if convertedValue != nil {
			value = append(value, convertedValue)
		}
	}
	return value, errors
}
func directoryOfCombinedPath(fileName string, basePath string) string {
	return tspath.GetDirectoryPath(tspath.GetNormalizedAbsolutePath(fileName, basePath))
}

func ParseConfigFileTextToJson(fileName string, path tspath.Path, jsonText string) (any, []*ast.Diagnostic) {
	opts := ast.SourceFileParseOptions{FileName: fileName, Path: path}
	if root, ok := parser.ParseJSONStore(opts, jsonText); ok {
		if config, ok := convertJSONStoreConfig(root); ok {
			return config, nil
		}
	}
	jsonSourceFile := parser.ParseSourceFile(opts, jsonText, core.ScriptKindJSON)
	config, errors := convertConfigFileToObject(jsonSourceFile, nil)
	if len(jsonSourceFile.Diagnostics()) > 0 {
		errors = []*ast.Diagnostic{jsonSourceFile.Diagnostics()[0]}
	}
	return config, errors
}
func convertJSONStoreConfig(root ast.Handle) (any, bool) {
	if root.Kind != ast.KindSourceFile {
		return nil, false
	}
	store := root.Store()
	statements := root.SourceFileStatements()
	switch store.ListLen(statements) {
	case 0:
		return struct{}{}, true
	case 1:
		statement := store.ListAt(statements, 0)
		if statement.Kind != ast.KindExpressionStatement {
			return nil, false
		}
		expression := statement.ExpressionStatementExpression()
		if expression.Kind != ast.KindObjectLiteralExpression {
			return nil, false
		}
		return convertJSONStoreValue(expression)
	default:
		return nil, false
	}
}
func convertJSONStoreValue(value ast.Handle) (any, bool) {
	switch value.Kind {
	case ast.KindTrueKeyword:
		return true, true
	case ast.KindFalseKeyword:
		return false, true
	case ast.KindNullKeyword:
		return nil, true
	case ast.KindStringLiteral:
		return value.StringLiteralText(), true
	case ast.KindNumericLiteral:
		return float64(jsnum.FromString(value.NumericLiteralText())), true
	case ast.KindPrefixUnaryExpression:
		operand := value.PrefixUnaryExpressionOperand()
		if value.PrefixUnaryExpressionOperator() != ast.KindMinusToken || operand.Kind != ast.KindNumericLiteral {
			return nil, false
		}
		return float64(-jsnum.FromString(operand.NumericLiteralText())), true
	case ast.KindArrayLiteralExpression:
		store := value.Store()
		elements := value.ArrayLiteralExpressionElements()
		result := make([]any, store.ListLen(elements))
		for i := range result {
			element, ok := convertJSONStoreValue(store.ListAt(elements, i))
			if !ok {
				return nil, false
			}
			result[i] = element
		}
		return result, true
	case ast.KindObjectLiteralExpression:
		store := value.Store()
		properties := value.ObjectLiteralExpressionProperties()
		result := collections.NewOrderedMapWithSizeHint[string, any](store.ListLen(properties))
		for i := range store.ListLen(properties) {
			property := store.ListAt(properties, i)
			if property.Kind != ast.KindPropertyAssignment {
				return nil, false
			}
			name := property.PropertyAssignmentName()
			if name.Kind != ast.KindStringLiteral {
				return nil, false
			}
			initializer, ok := convertJSONStoreValue(property.PropertyAssignmentInitializer())
			if !ok {
				return nil, false
			}
			result.Set(name.StringLiteralText(), initializer)
		}
		return result, true
	default:
		return nil, false
	}
}

type ParseConfigHost interface {
	FS() vfs.FS
	GetCurrentDirectory() string
}
type resolverHost struct{ ParseConfigHost }

func (r *resolverHost) Trace(msg string) {
}
func ParseJsonSourceFileConfigFileContent(sourceFile *TsConfigSourceFile, host ParseConfigHost, basePath string, existingOptions *core.CompilerOptions, existingOptionsRaw *collections.OrderedMap[string, any], configFileName string, resolutionStack []tspath.Path, extendedConfigCache ExtendedConfigCache) *ParsedCommandLine {
	result := parseJsonConfigFileContentWorker(nil, sourceFile, host, basePath, existingOptions, existingOptionsRaw, configFileName, resolutionStack, extendedConfigCache)
	return result
}
func convertObjectLiteralExpressionToJson(sourceFile *ast.SourceFile, returnValue bool, node ast.Handle, objectOption *CommandLineOption, jsonConversionNotifier *jsonConversionNotifier) (*collections.OrderedMap[string, any], []*ast.Diagnostic) {
	var result *collections.OrderedMap[string, any]
	if returnValue {
		result = &collections.OrderedMap[string, any]{}
	}
	var errors []*ast.Diagnostic
	for _, element := range node.Properties() {
		if element.Kind != ast.KindPropertyAssignment {
			errors = append(errors, ast.NewDiagnostic(sourceFile, element.Loc(), diagnostics.Property_assignment_expected))
			continue
		}
		if token := element.QuestionToken(); !token.IsNil() {
			errors = append(errors, ast.NewDiagnostic(sourceFile, token.Loc(), diagnostics.The_0_modifier_can_only_be_used_in_TypeScript_files, "?"))
		}
		textOfKey := ""
		if !ast.IsComputedNonLiteralName(element.Name()) {
			textOfKey, _ = ast.TryGetTextOfPropertyName(element.Name())
		}
		keyText := textOfKey
		var option *CommandLineOption = nil
		if keyText != "" && objectOption != nil && objectOption.ElementOptions != nil {
			option = objectOption.ElementOptions.Get(keyText)
			if option != nil && option.Name != keyText {
				option = nil
			}
		}
		value, err := convertPropertyValueToJson(sourceFile, element.PropertyAssignmentInitializer(), option, returnValue, jsonConversionNotifier)
		errors = append(errors, err...)
		if keyText != "" {
			if returnValue {
				result.Set(keyText, value)
			}
			if jsonConversionNotifier != nil {
				_, err := jsonConversionNotifier.onPropertySet(keyText, value, element, objectOption, option)
				errors = append(errors, err...)
			}
		}
	}
	return result, errors
}

func convertToJson(sourceFile *ast.SourceFile, rootExpression ast.Handle, returnValue bool, jsonConversionNotifier *jsonConversionNotifier) (any, []*ast.Diagnostic) {
	if rootExpression.IsNil() {
		if returnValue {
			return struct{}{}, nil
		} else {
			return nil, nil
		}
	}
	var rootOptions *CommandLineOption
	if jsonConversionNotifier != nil {
		rootOptions = jsonConversionNotifier.rootOptions
	}
	return convertPropertyValueToJson(sourceFile, rootExpression, rootOptions, returnValue, jsonConversionNotifier)
}
func isDoubleQuotedString(node ast.Handle) bool {
	return ast.IsStringLiteral(node)
}
func convertPropertyValueToJson(sourceFile *ast.SourceFile, valueExpression ast.Handle, option *CommandLineOption, returnValue bool, jsonConversionNotifier *jsonConversionNotifier) (any, []*ast.Diagnostic) {
	switch valueExpression.Kind {
	case ast.KindTrueKeyword:
		return true, nil
	case ast.KindFalseKeyword:
		return false, nil
	case ast.KindNullKeyword:
		return nil, nil
	case ast.KindStringLiteral:
		if !isDoubleQuotedString(valueExpression) {
			return valueExpression.Text(), []*ast.Diagnostic{ast.NewDiagnostic(sourceFile, valueExpression.Loc(), diagnostics.String_literal_with_double_quotes_expected)}
		}
		return valueExpression.Text(), nil
	case ast.KindNumericLiteral:
		return float64(jsnum.FromString(valueExpression.Text())), nil
	case ast.KindPrefixUnaryExpression:
		if valueExpression.PrefixUnaryExpressionOperator() != ast.KindMinusToken || valueExpression.PrefixUnaryExpressionOperand().Kind != ast.KindNumericLiteral {
			break
		}
		return float64(-jsnum.FromString(valueExpression.PrefixUnaryExpressionOperand().Text())), nil
	case ast.KindObjectLiteralExpression:
		objectLiteralExpression := valueExpression
		return convertObjectLiteralExpressionToJson(sourceFile, returnValue, objectLiteralExpression, option, jsonConversionNotifier)
	case ast.KindArrayLiteralExpression:
		result, errors := convertArrayLiteralExpressionToJson(sourceFile, valueExpression.Elements(), option, returnValue)
		return result, errors
	}
	var errors []*ast.Diagnostic
	if option != nil {
		errors = []*ast.Diagnostic{ast.NewDiagnostic(sourceFile, valueExpression.Loc(), diagnostics.Compiler_option_0_requires_a_value_of_type_1, option.Name, getCompilerOptionValueTypeString(option))}
	} else {
		errors = []*ast.Diagnostic{ast.NewDiagnostic(sourceFile, valueExpression.Loc(), diagnostics.Property_value_can_only_be_string_literal_numeric_literal_true_false_null_object_literal_or_array_literal)}
	}
	return nil, errors
}

func ParseJsonConfigFileContent(json any, host ParseConfigHost, basePath string, existingOptions *core.CompilerOptions, configFileName string, resolutionStack []tspath.Path, extendedConfigCache ExtendedConfigCache) *ParsedCommandLine {
	normalized := normalizeJsonValue(json)
	jsonObject, ok := normalized.(*collections.OrderedMap[string, any])
	if !ok {
		jsonObject = &collections.OrderedMap[string, any]{}
	}
	result := parseJsonConfigFileContentWorker(jsonObject, nil, host, basePath, existingOptions, nil, configFileName, resolutionStack, extendedConfigCache)
	return result
}
func normalizeJsonValue(value any) any {
	switch value := value.(type) {
	case *collections.OrderedMap[string, any]:
		for key, child := range value.Entries() {
			value.Set(key, normalizeJsonValue(child))
		}
		return value
	case map[string]any:
		result := collections.NewOrderedMapWithSizeHint[string, any](len(value))
		for _, key := range slices.Sorted(maps.Keys(value)) {
			child := value[key]
			result.Set(key, normalizeJsonValue(child))
		}
		return result
	case []any:
		result := make([]any, len(value))
		for i, child := range value {
			result[i] = normalizeJsonValue(child)
		}
		return result
	default:
		reflected := reflect.ValueOf(value)
		if !reflected.IsValid() || (reflected.Kind() != reflect.Slice && reflected.Kind() != reflect.Array) {
			return value
		}
		if reflected.Kind() == reflect.Slice && reflected.IsNil() {
			return nil
		}
		result := make([]any, reflected.Len())
		for i := range reflected.Len() {
			result[i] = normalizeJsonValue(reflected.Index(i).Interface())
		}
		return result
	}
}

func convertToObject(sourceFile *ast.SourceFile) (any, []*ast.Diagnostic) {
	var rootExpression ast.Handle
	if len(sourceFile.ParseRoot().Statements()) != 0 {
		rootExpression = sourceFile.ParseRoot().Statements()[0].Expression()
	}
	return convertToJson(sourceFile, rootExpression, true, nil)
}
func getDefaultCompilerOptions(configFileName string) *core.CompilerOptions {
	options := &core.CompilerOptions{}
	if configFileName != "" && tspath.GetBaseFileName(configFileName) == "jsconfig.json" {
		depth := 2
		options = &core.CompilerOptions{AllowJs: core.TSTrue, MaxNodeModuleJsDepth: &depth, SkipLibCheck: core.TSTrue, NoEmit: core.TSTrue}
	}
	return options
}
func getDefaultTypeAcquisition(configFileName string) *core.TypeAcquisition {
	options := &core.TypeAcquisition{}
	if configFileName != "" && tspath.GetBaseFileName(configFileName) == "jsconfig.json" {
		options.Enable = core.TSTrue
	}
	return options
}
func convertCompilerOptionsFromJsonWorker(jsonOptions any, basePath string, configFileName string) (*core.CompilerOptions, []*ast.Diagnostic) {
	options := getDefaultCompilerOptions(configFileName)
	_, errors := convertOptionsFromJson(CommandLineCompilerOptionsMap, jsonOptions, basePath, &compilerOptionsParser{options})
	if configFileName != "" {
		options.ConfigFilePath = tspath.NormalizeSlashes(configFileName)
	}
	return options, errors
}
func convertTypeAcquisitionFromJsonWorker(jsonOptions any, basePath string, configFileName string) (*core.TypeAcquisition, []*ast.Diagnostic) {
	options := getDefaultTypeAcquisition(configFileName)
	_, errors := convertOptionsFromJson(typeAcquisitionDeclaration.ElementOptions, jsonOptions, basePath, &typeAcquisitionParser{options})
	return options, errors
}
func parseOwnConfigOfJson(json *collections.OrderedMap[string, any], host ParseConfigHost, basePath string, configFileName string) (*parsedTsconfig, []*ast.Diagnostic) {
	var errors []*ast.Diagnostic
	if json.Has("excludes") {
		errors = append(errors, ast.NewCompilerDiagnostic(diagnostics.Unknown_option_excludes_Did_you_mean_exclude))
	}
	options, err := convertCompilerOptionsFromJsonWorker(json.GetOrZero("compilerOptions"), basePath, configFileName)
	typeAcquisition, err2 := convertTypeAcquisitionFromJsonWorker(json.GetOrZero("typeAcquisition"), basePath, configFileName)
	errors = append(append(errors, err...), err2...)
	if compileOnSave, ok := json.Get("compileOnSave"); ok {
		converted, compileOnSaveErrors := convertJsonOption(compileOnSaveCommandLineOption, compileOnSave, basePath, ast.Handle{}, ast.Handle{}, nil)
		errors = append(errors, compileOnSaveErrors...)
		json.Set("compileOnSave", converted)
	}
	var extendedConfigPath []string
	if extends := json.GetOrZero("extends"); extends != nil && extends != "" {
		extendedConfigPath, err = getExtendsConfigPathOrArray(extends, host, basePath, configFileName, ast.Handle{}, ast.Handle{}, nil)
		errors = append(errors, err...)
	}
	parsedConfig := &parsedTsconfig{raw: json, options: options, typeAcquisition: typeAcquisition, extendedConfigPath: extendedConfigPath}
	return parsedConfig, errors
}
func readJsonConfigFile(fileName string, path tspath.Path, readFile func(fileName string) (string, bool)) (*TsConfigSourceFile, []*ast.Diagnostic) {
	text, diagnostic := tryReadFile(fileName, readFile, []*ast.Diagnostic{})
	if text != "" {
		return &TsConfigSourceFile{SourceFile: parser.ParseSourceFile(ast.SourceFileParseOptions{FileName: fileName, Path: path}, text, core.ScriptKindJSON)}, diagnostic
	} else {
		file := &TsConfigSourceFile{SourceFile: parser.ParseSourceFile(ast.SourceFileParseOptions{FileName: fileName, Path: path}, "", core.ScriptKindJSON)}
		file.SourceFile.SetDiagnostics(diagnostic)
		return file, diagnostic
	}
}
func getExtendedConfig(sourceFile *TsConfigSourceFile, extendedConfigFileName string, host ParseConfigHost, resolutionStack []tspath.Path, extendedConfigCache ExtendedConfigCache, result *extendsResult) (*parsedTsconfig, []*ast.Diagnostic) {
	var errors []*ast.Diagnostic
	extendedConfigPath := tspath.ToPath(extendedConfigFileName, host.GetCurrentDirectory(), host.FS().UseCaseSensitiveFileNames())
	var cacheEntry *ExtendedConfigCacheEntry
	if extendedConfigCache != nil && !slices.Contains(resolutionStack, extendedConfigPath) {
		cacheEntry = extendedConfigCache.GetExtendedConfig(extendedConfigFileName, extendedConfigPath, resolutionStack, host)
	} else {
		cacheEntry = ParseExtendedConfig(extendedConfigFileName, extendedConfigPath, resolutionStack, host, extendedConfigCache)
	}
	if len(cacheEntry.errors) > 0 {
		errors = append(errors, cacheEntry.errors...)
	}
	if cacheEntry.extendedResult != nil {
		if sourceFile != nil {
			result.extendedSourceFiles.Add(cacheEntry.extendedResult.SourceFile.FileName())
			for _, extendedSourceFile := range cacheEntry.extendedResult.ExtendedSourceFiles {
				result.extendedSourceFiles.Add(extendedSourceFile)
			}
		}
	}
	return cacheEntry.extendedConfig, errors
}
func ParseExtendedConfig(fileName string, path tspath.Path, resolutionStack []tspath.Path, host ParseConfigHost, extendedConfigCache ExtendedConfigCache) *ExtendedConfigCacheEntry {
	extendedResult, readErrors := readJsonConfigFile(fileName, path, host.FS().ReadFile)
	entry := &ExtendedConfigCacheEntry{extendedResult: extendedResult}
	if len(readErrors) > 0 {
		entry.errors = readErrors
		return entry
	}
	if parseDiagnostics := extendedResult.SourceFile.Diagnostics(); len(parseDiagnostics) > 0 {
		entry.errors = parseDiagnostics
		return entry
	}
	var parseErrors []*ast.Diagnostic
	entry.extendedConfig, parseErrors = parseConfig(nil, extendedResult, host, tspath.GetDirectoryPath(fileName), tspath.GetBaseFileName(fileName), resolutionStack, extendedConfigCache)
	entry.errors = parseErrors
	return entry
}

func parseConfig(json *collections.OrderedMap[string, any], sourceFile *TsConfigSourceFile, host ParseConfigHost, basePath string, configFileName string, resolutionStack []tspath.Path, extendedConfigCache ExtendedConfigCache) (*parsedTsconfig, []*ast.Diagnostic) {
	basePath = tspath.NormalizeSlashes(basePath)
	resolvedPath := tspath.ToPath(configFileName, basePath, host.FS().UseCaseSensitiveFileNames())
	var errors []*ast.Diagnostic
	if slices.Contains(resolutionStack, resolvedPath) {
		var result *parsedTsconfig
		errors = append(errors, ast.NewCompilerDiagnostic(diagnostics.Circularity_detected_while_resolving_configuration_Colon_0))
		if json.Size() == 0 {
			result = &parsedTsconfig{raw: json}
		} else {
			rawResult, err := convertToObject(sourceFile.SourceFile)
			errors = append(errors, err...)
			result = &parsedTsconfig{raw: rawResult}
		}
		return result, errors
	}
	var ownConfig *parsedTsconfig
	var err []*ast.Diagnostic
	if json != nil {
		ownConfig, err = parseOwnConfigOfJson(json, host, basePath, configFileName)
	} else {
		ownConfig, err = parseOwnConfigOfJsonSourceFile(tsconfigToSourceFile(sourceFile), host, basePath, configFileName)
	}
	errors = append(errors, err...)
	if ownConfig.options != nil && ownConfig.options.Paths != nil {
		ownConfig.options.PathsBasePath = basePath
	}
	applyExtendedConfig := func(result *extendsResult, extendedConfigPath string) {
		extendedConfig, extendedErrors := getExtendedConfig(sourceFile, extendedConfigPath, host, resolutionStack, extendedConfigCache, result)
		errors = append(errors, extendedErrors...)
		if extendedConfig != nil && extendedConfig.options != nil {
			extendsRaw := extendedConfig.raw
			relativeDifference := ""
			setPropertyValue := func(propertyName string) {
				if rawMap, ok := ownConfig.raw.(*collections.OrderedMap[string, any]); ok && rawMap.Has(propertyName) {
					return
				}
				if propertyName == "include" || propertyName == "exclude" || propertyName == "files" {
					if rawMap, ok := extendsRaw.(*collections.OrderedMap[string, any]); ok && rawMap.Has(propertyName) {
						if slice, _ := rawMap.GetOrZero(propertyName).([]any); slice != nil {
							value := core.Map(slice, func(path any) any {
								pathStr, isString := path.(string)
								if !isString {
									return path
								}
								if startsWithConfigDirTemplate(path) || tspath.IsRootedDiskPath(pathStr) {
									return pathStr
								} else {
									if relativeDifference == "" {
										t := tspath.ComparePathsOptions{UseCaseSensitiveFileNames: host.FS().UseCaseSensitiveFileNames(), CurrentDirectory: basePath}
										relativeDifference = tspath.ConvertToRelativePath(tspath.GetDirectoryPath(extendedConfigPath), t)
									}
									return tspath.CombinePaths(relativeDifference, pathStr)
								}
							})
							if propertyName == "include" {
								result.include = value
							} else if propertyName == "exclude" {
								result.exclude = value
							} else if propertyName == "files" {
								result.files = value
							}
						}
					}
				}
			}
			setPropertyValue("include")
			setPropertyValue("exclude")
			setPropertyValue("files")
			if extendedRawMap, ok := extendsRaw.(*collections.OrderedMap[string, any]); ok && extendedRawMap.Has("contentMappers") {
				result.contentMappers, _ = extendedRawMap.GetOrZero("contentMappers").([]any)
			}
			if extendedRawMap, ok := extendsRaw.(*collections.OrderedMap[string, any]); ok && extendedRawMap.Has("compileOnSave") {
				if compileOnSave, ok := extendedRawMap.GetOrZero("compileOnSave").(bool); ok {
					result.compileOnSave = compileOnSave
				}
			}
			mergeCompilerOptions(result.options, extendedConfig.options, extendsRaw)
		}
	}
	if ownConfig.extendedConfigPath != nil {
		resolutionStack = append(resolutionStack, resolvedPath)
		var result *extendsResult = &extendsResult{options: &core.CompilerOptions{}}
		if reflect.TypeOf(ownConfig.extendedConfigPath).Kind() == reflect.String {
			applyExtendedConfig(result, ownConfig.extendedConfigPath.(string))
		} else if configPath, ok := ownConfig.extendedConfigPath.([]string); ok {
			for _, extendedConfigPath := range configPath {
				applyExtendedConfig(result, extendedConfigPath)
			}
		}
		if result.include != nil {
			ownConfig.raw.(*collections.OrderedMap[string, any]).Set("include", result.include)
		}
		if result.exclude != nil {
			ownConfig.raw.(*collections.OrderedMap[string, any]).Set("exclude", result.exclude)
		}
		if result.files != nil {
			ownConfig.raw.(*collections.OrderedMap[string, any]).Set("files", result.files)
		}
		if result.contentMappers != nil && !ownConfig.raw.(*collections.OrderedMap[string, any]).Has("contentMappers") {
			ownConfig.raw.(*collections.OrderedMap[string, any]).Set("contentMappers", result.contentMappers)
		}
		if result.compileOnSave && !ownConfig.raw.(*collections.OrderedMap[string, any]).Has("compileOnSave") {
			ownConfig.raw.(*collections.OrderedMap[string, any]).Set("compileOnSave", result.compileOnSave)
		}
		if sourceFile != nil {
			for extendedSourceFile := range result.extendedSourceFiles.Keys() {
				sourceFile.ExtendedSourceFiles = core.InsertSorted(sourceFile.ExtendedSourceFiles, extendedSourceFile, cmp.Compare)
			}
		}
		ownConfig.options = mergeCompilerOptions(result.options, ownConfig.options, ownConfig.raw)
	}
	return ownConfig, errors
}

const defaultIncludeSpec = "**/*"

type propOfRaw struct {
	sliceValue []any
	wrongValue string
}

func isStringValue(value any) bool {
	_, ok := value.(string)
	return ok
}

func parseJsonConfigFileContentWorker(json *collections.OrderedMap[string, any], sourceFile *TsConfigSourceFile, host ParseConfigHost, basePath string, existingOptions *core.CompilerOptions, existingOptionsRaw *collections.OrderedMap[string, any], configFileName string, resolutionStack []tspath.Path, extendedConfigCache ExtendedConfigCache) *ParsedCommandLine {
	debug.Assert((json == nil && sourceFile != nil) || (json != nil && sourceFile == nil))
	basePathForFileNames := ""
	if configFileName != "" {
		basePathForFileNames = tspath.NormalizePath(directoryOfCombinedPath(configFileName, basePath))
	} else {
		basePathForFileNames = tspath.NormalizePath(basePath)
	}
	var errors []*ast.Diagnostic
	parsedConfig, errors := parseConfig(json, sourceFile, host, basePath, configFileName, resolutionStack, extendedConfigCache)
	mergeCompilerOptions(parsedConfig.options, existingOptions, existingOptionsRaw)
	handleOptionConfigDirTemplateSubstitution(parsedConfig.options, basePathForFileNames)
	rawConfig := parseJsonToStringKey(parsedConfig.raw)
	if configFileName != "" && parsedConfig.options != nil {
		parsedConfig.options.ConfigFilePath = tspath.NormalizeSlashes(configFileName)
	}
	getPropFromRaw := func(prop string, validateElement func(value any) bool, elementTypeName string) propOfRaw {
		value, exists := rawConfig.Get(prop)
		if exists && value != nil {
			if reflect.TypeOf(value).Kind() == reflect.Slice {
				result := rawConfig.GetOrZero(prop)
				if _, ok := result.([]any); ok {
					if sourceFile == nil && !core.Every(result.([]any), validateElement) {
						errors = append(errors, ast.NewCompilerDiagnostic(diagnostics.Compiler_option_0_requires_a_value_of_type_1, prop, elementTypeName))
					}
				}
				return propOfRaw{sliceValue: result.([]any)}
			} else if sourceFile == nil {
				errors = append(errors, ast.NewCompilerDiagnostic(diagnostics.Compiler_option_0_requires_a_value_of_type_1, prop, "Array"))
				return propOfRaw{sliceValue: nil, wrongValue: "not-array"}
			}
		}
		return propOfRaw{sliceValue: nil, wrongValue: "no-prop"}
	}
	referencesOfRaw := getPropFromRaw("references", func(element any) bool {
		return reflect.TypeOf(element) == orderedMapType
	}, "object")
	fileSpecs := getPropFromRaw("files", isStringValue, "string")
	if fileSpecs.sliceValue != nil || fileSpecs.wrongValue == "" {
		hasZeroOrNoReferences := false
		if referencesOfRaw.wrongValue == "no-prop" || referencesOfRaw.wrongValue == "not-array" || len(referencesOfRaw.sliceValue) == 0 {
			hasZeroOrNoReferences = true
		}
		hasExtends := rawConfig.GetOrZero("extends")
		if fileSpecs.sliceValue != nil && len(fileSpecs.sliceValue) == 0 && hasZeroOrNoReferences && hasExtends == nil {
			if sourceFile != nil {
				var fileName string
				if configFileName != "" {
					fileName = configFileName
				} else {
					fileName = "tsconfig.json"
				}
				diagnosticMessage := diagnostics.The_files_list_in_config_file_0_is_empty
				nodeValue := ForEachTsConfigPropArray(sourceFile.SourceFile, "files", func(property ast.Handle) ast.Handle {
					return property.Initializer()
				})
				errors = append(errors, CreateDiagnosticForNodeInSourceFile(sourceFile.SourceFile, nodeValue, diagnosticMessage, fileName))
			} else {
				errors = append(errors, ast.NewCompilerDiagnostic(diagnostics.The_files_list_in_config_file_0_is_empty, configFileName))
			}
		}
	}
	includeSpecs := getPropFromRaw("include", isStringValue, "string")
	excludeSpecs := getPropFromRaw("exclude", isStringValue, "string")
	isDefaultIncludeSpec := false
	if excludeSpecs.wrongValue == "no-prop" && parsedConfig.options != nil {
		outDir := parsedConfig.options.OutDir
		declarationDir := parsedConfig.options.DeclarationDir
		if outDir != "" || declarationDir != "" {
			var values []any
			if outDir != "" {
				values = append(values, outDir)
			}
			if declarationDir != "" {
				values = append(values, declarationDir)
			}
			excludeSpecs = propOfRaw{sliceValue: values}
		}
	}
	if fileSpecs.sliceValue == nil && includeSpecs.sliceValue == nil {
		includeSpecs = propOfRaw{sliceValue: []any{defaultIncludeSpec}}
		isDefaultIncludeSpec = true
	}
	var validatedIncludeSpecs []string
	var validatedIncludeSpecsBeforeSubstitution []string
	var validatedExcludeSpecs []string
	var validatedFilesSpec []string
	var validatedFilesSpecBeforeSubstitution []string
	if includeSpecs.sliceValue != nil {
		var err []*ast.Diagnostic
		validatedIncludeSpecsBeforeSubstitution, err = validateSpecs(includeSpecs.sliceValue, true, tsconfigToSourceFile(sourceFile), "include")
		errors = append(errors, err...)
		if validatedIncludeSpecs = getSubstitutedStringArrayWithConfigDirTemplate(validatedIncludeSpecsBeforeSubstitution, basePathForFileNames); validatedIncludeSpecs == nil {
			validatedIncludeSpecs = validatedIncludeSpecsBeforeSubstitution
		}
	}
	if excludeSpecs.sliceValue != nil {
		var err []*ast.Diagnostic
		validatedExcludeSpecs, err = validateSpecs(excludeSpecs.sliceValue, false, tsconfigToSourceFile(sourceFile), "exclude")
		errors = append(errors, err...)
		if validatedExcludeSpecsWithSubstitution := getSubstitutedStringArrayWithConfigDirTemplate(validatedExcludeSpecs, basePathForFileNames); validatedExcludeSpecsWithSubstitution != nil {
			validatedExcludeSpecs = validatedExcludeSpecsWithSubstitution
		}
	}
	if fileSpecs.sliceValue != nil {
		fileSpecs := core.Filter(fileSpecs.sliceValue, isStringValue)
		for _, spec := range fileSpecs {
			if spec, ok := spec.(string); ok {
				validatedFilesSpecBeforeSubstitution = append(validatedFilesSpecBeforeSubstitution, spec)
			}
		}
		if validatedFilesSpec = getSubstitutedStringArrayWithConfigDirTemplate(validatedFilesSpecBeforeSubstitution, basePathForFileNames); validatedFilesSpec == nil {
			validatedFilesSpec = validatedFilesSpecBeforeSubstitution
		}
	}
	configFileSpecs := configFileSpecs{fileSpecs.sliceValue, includeSpecs.sliceValue, excludeSpecs.sliceValue, validatedFilesSpec, validatedIncludeSpecs, validatedExcludeSpecs, validatedFilesSpecBeforeSubstitution, validatedIncludeSpecsBeforeSubstitution, isDefaultIncludeSpec}
	if sourceFile != nil {
		sourceFile.configFileSpecs = &configFileSpecs
	}
	var contentMapperSourceFile *ast.SourceFile
	if sourceFile != nil {
		contentMapperSourceFile = sourceFile.SourceFile
	}
	var contentMappers []*contentmapper.Mapper
	var contentMapperIndices []int
	contentMappersOfRaw := getPropFromRaw("contentMappers", func(element any) bool {
		return reflect.TypeOf(element) == orderedMapType
	}, "object")
	for i, element := range contentMappersOfRaw.sliceValue {
		mapper, mapperErrors := parseContentMapper(element)
		for _, mapperError := range mapperErrors {
			errors = append(errors, setContentMapperDiagnosticLocation(mapperError, contentMapperSourceFile, getContentMapperSyntax(contentMapperSourceFile, i, "")))
		}
		if mapper != nil {
			contentMappers = append(contentMappers, mapper)
			contentMapperIndices = append(contentMapperIndices, i)
		}
	}
	totalContentMapperExtensions := 0
	for _, mapper := range contentMappers {
		totalContentMapperExtensions += len(mapper.Definition.Extensions)
	}
	seenContentMapperExtensions := make(map[string]struct{}, totalContentMapperExtensions)
	contentMapperExtensions := make([]string, 0, totalContentMapperExtensions)
	nativeExtensions := core.Flatten(tspath.AllSupportedExtensionsWithJson)
	canonicalExtension := func(extension string) string {
		return tspath.GetCanonicalFileName(extension, host.FS().UseCaseSensitiveFileNames())
	}
	for j, mapper := range contentMappers {
		validExtensions := make([]string, 0, len(mapper.Definition.Extensions))
		for _, ext := range mapper.Definition.Extensions {
			extNode := getContentMapperExtensionSyntax(contentMapperSourceFile, contentMapperIndices[j], ext)
			canonicalExt := canonicalExtension(ext)
			switch {
			case !strings.HasPrefix(ext, "."):
				errors = append(errors, setContentMapperDiagnosticLocation(ast.NewCompilerDiagnostic(diagnostics.Content_mapper_file_extension_0_must_begin_with_a, ext), contentMapperSourceFile, extNode))
			case slices.ContainsFunc(nativeExtensions, func(nativeExtension string) bool {
				return strings.EqualFold(nativeExtension, ext)
			}):
				errors = append(errors, setContentMapperDiagnosticLocation(ast.NewCompilerDiagnostic(diagnostics.Content_mapper_file_extension_0_is_a_built_in_extension_and_cannot_be_registered_by_a_content_mapper, ext), contentMapperSourceFile, extNode))
			default:
				if _, seen := seenContentMapperExtensions[canonicalExt]; seen {
					errors = append(errors, setContentMapperDiagnosticLocation(ast.NewCompilerDiagnostic(diagnostics.Content_mapper_file_extension_0_is_registered_by_more_than_one_content_mapper, ext), contentMapperSourceFile, extNode))
				} else {
					seenContentMapperExtensions[canonicalExt] = struct{}{}
					contentMapperExtensions = append(contentMapperExtensions, ext)
					validExtensions = append(validExtensions, ext)
				}
			}
		}
		mapper.Definition.Extensions = validExtensions
	}
	if len(contentMappers) > 0 && !(parsedConfig.options != nil && parsedConfig.options.RunExternalCode.IsTrue()) {
		errors = append(errors, setContentMapperDiagnosticLocation(ast.NewCompilerDiagnostic(diagnostics.Content_mappers_require_the_runExternalCode_command_line_flag_to_be_enabled), contentMapperSourceFile, getContentMappersKeySyntax(contentMapperSourceFile)))
		contentMappers = nil
		contentMapperExtensions = nil
	} else if len(contentMappers) > 0 {
		containingFile := configFileName
		if containingFile == "" {
			containingFile = tspath.CombinePaths(basePathForFileNames, "tsconfig.json")
		}
		resolvedContentMappers := make([]*contentmapper.Mapper, 0, len(contentMappers))
		for j, mapper := range contentMappers {
			manifest, packageDirectory, diagnostic := resolveContentMapperManifest(host, containingFile, mapper.Package)
			mapper.PackageDirectory = packageDirectory
			if diagnostic != nil {
				errors = append(errors, setContentMapperDiagnosticLocation(diagnostic, contentMapperSourceFile, getContentMapperSyntax(contentMapperSourceFile, contentMapperIndices[j], "package")))
				continue
			}
			mapper.Manifest = manifest
			resolvedContentMappers = append(resolvedContentMappers, mapper)
		}
		contentMappers = resolvedContentMappers
		contentMapperExtensions = core.FlatMap(contentMappers, func(mapper *contentmapper.Mapper) []string {
			return mapper.Definition.Extensions
		})
	}
	getFileNames := func(basePath string) ([]string, int) {
		parsedConfigOptions := parsedConfig.options
		fileNames, literalFileNamesLen := getFileNamesFromConfigSpecs(configFileSpecs, basePath, parsedConfigOptions, host.FS(), contentMapperExtensions)
		if shouldReportNoInputFiles(fileNames, canJsonReportNoInputFiles(rawConfig), resolutionStack) {
			includeSpecs := configFileSpecs.includeSpecs
			excludeSpecs := configFileSpecs.excludeSpecs
			if includeSpecs == nil {
				includeSpecs = []string{}
			}
			if excludeSpecs == nil {
				excludeSpecs = []string{}
			}
			errors = append(errors, ast.NewCompilerDiagnostic(diagnostics.No_inputs_were_found_in_config_file_0_Specified_include_paths_were_1_and_exclude_paths_were_2, configFileName, core.Must(core.StringifyJson(includeSpecs, "", "")), core.Must(core.StringifyJson(excludeSpecs, "", ""))))
		}
		return fileNames, literalFileNamesLen
	}
	getProjectReferences := func(basePath string) []*core.ProjectReference {
		var projectReferences []*core.ProjectReference
		newReferencesOfRaw := getPropFromRaw("references", func(element any) bool {
			return reflect.TypeOf(element) == orderedMapType
		}, "object")
		if newReferencesOfRaw.sliceValue != nil {
			projectReferences = []*core.ProjectReference{}
			for index, reference := range newReferencesOfRaw.sliceValue {
				ref := parseProjectReference(reference)
				if ref == nil {
					continue
				}
				if !ref.hasPath || !ref.pathValid {
					errors = append(errors, createDiagnosticAtProjectReferenceProperty(sourceFile, index, "path", diagnostics.Compiler_option_0_requires_a_value_of_type_1, "reference.path", "string"))
					continue
				}
				if ref.reference.Path == "" {
					errors = append(errors, createDiagnosticAtProjectReferenceProperty(sourceFile, index, "path", diagnostics.Compiler_option_0_cannot_be_given_an_empty_string, "reference.path"))
					continue
				}
				if ref.hasCircular && !ref.circularValid {
					errors = append(errors, createDiagnosticAtProjectReferenceProperty(sourceFile, index, "circular", diagnostics.Compiler_option_0_requires_a_value_of_type_1, "reference.circular", "boolean"))
				}
				projectReferences = append(projectReferences, &core.ProjectReference{Path: tspath.GetNormalizedAbsolutePath(ref.reference.Path, basePath), OriginalPath: ref.reference.Path, Circular: ref.reference.Circular})
			}
		}
		return projectReferences
	}
	fileNames, literalFileNamesLen := getFileNames(basePathForFileNames)
	compileOnSave := new(false)
	if raw, ok := parsedConfig.raw.(*collections.OrderedMap[string, any]); ok {
		if value, ok := raw.GetOrZero("compileOnSave").(bool); ok {
			compileOnSave = &value
		}
	}
	return &ParsedCommandLine{ParsedConfig: &ParsedOptions{CompilerOptions: parsedConfig.options, TypeAcquisition: parsedConfig.typeAcquisition, FileNames: fileNames, ProjectReferences: getProjectReferences(basePathForFileNames), ContentMappers: contentMappers}, ConfigFile: sourceFile, Raw: parsedConfig.raw, Errors: errors, CompileOnSave: compileOnSave, comparePathsOptions: tspath.ComparePathsOptions{UseCaseSensitiveFileNames: host.FS().UseCaseSensitiveFileNames(), CurrentDirectory: basePathForFileNames}, literalFileNamesLen: literalFileNamesLen}
}
func canJsonReportNoInputFiles(rawConfig *collections.OrderedMap[string, any]) bool {
	filesExists := rawConfig.Has("files")
	referencesExists := rawConfig.Has("references")
	return !filesExists && !referencesExists
}
func shouldReportNoInputFiles(fileNames []string, canJsonReportNoInputFiles bool, resolutionStack []tspath.Path) bool {
	return len(fileNames) == 0 && canJsonReportNoInputFiles && len(resolutionStack) == 0
}
func validateSpecs(specs any, disallowTrailingRecursion bool, jsonSourceFile *ast.SourceFile, specKey string) ([]string, []*ast.Diagnostic) {
	createDiagnostic := func(message *diagnostics.Message, spec string) *ast.Diagnostic {
		element := GetTsConfigPropArrayElementValue(jsonSourceFile, specKey, spec)
		return CreateDiagnosticForNodeInSourceFileOrCompilerDiagnostic(jsonSourceFile, element, message, spec)
	}
	var errors []*ast.Diagnostic
	var finalSpecs []string
	for _, value := range specs.([]any) {
		spec, ok := value.(string)
		if !ok {
			continue
		}
		diag := specToDiagnostic(spec, disallowTrailingRecursion)
		if diag != nil {
			errors = append(errors, createDiagnostic(diag, spec))
		} else {
			finalSpecs = append(finalSpecs, spec)
		}
	}
	return finalSpecs, errors
}
func specToDiagnostic(spec string, disallowTrailingRecursion bool) *diagnostics.Message {
	if disallowTrailingRecursion && invalidTrailingRecursion(spec) {
		return diagnostics.File_specification_cannot_end_in_a_recursive_directory_wildcard_Asterisk_Asterisk_Colon_0
	}
	if invalidDotDotAfterRecursiveWildcard(spec) {
		return diagnostics.File_specification_cannot_contain_a_parent_directory_that_appears_after_a_recursive_directory_wildcard_Asterisk_Asterisk_Colon_0
	}
	return nil
}
func invalidTrailingRecursion(spec string) bool {
	s := strings.TrimSuffix(spec, "/")
	return s == "**" || strings.HasSuffix(s, "/**")
}
func invalidDotDotAfterRecursiveWildcard(s string) bool {
	var wildcardIndex int
	if strings.HasPrefix(s, "**/") {
		wildcardIndex = 0
	} else {
		wildcardIndex = strings.Index(s, "/**/")
	}
	if wildcardIndex == -1 {
		return false
	}
	var lastDotIndex int
	if strings.HasSuffix(s, "/..") {
		lastDotIndex = len(s)
	} else {
		lastDotIndex = strings.LastIndex(s, "/../")
	}
	return lastDotIndex > wildcardIndex
}
func GetTsConfigPropArrayElementValue(tsConfigSourceFile *ast.SourceFile, propKey string, elementValue string) ast.Handle {
	callback := GetCallbackForFindingPropertyAssignmentByValue(elementValue)
	return ForEachTsConfigPropArray(tsConfigSourceFile, propKey, func(property ast.Handle) ast.Handle {
		return callback(property)
	})
}
func ForEachTsConfigPropArray[T any](tsConfigSourceFile *ast.SourceFile, propKey string, callback func(property ast.Handle) T) T {
	if tsConfigSourceFile != nil {
		return ForEachPropertyAssignment(getTsConfigObjectLiteralExpression(tsConfigSourceFile), propKey, callback)
	}
	var zero T
	return zero
}
func CreateDiagnosticAtReferenceSyntax(config *ParsedCommandLine, index int, message *diagnostics.Message, args ...any) *ast.Diagnostic {
	return ForEachTsConfigPropArray(config.ConfigFile.SourceFile, "references", func(property ast.Handle) *ast.Diagnostic {
		if ast.IsArrayLiteralExpression(property.Initializer()) {
			value := property.Initializer().Elements()
			if len(value) > index {
				return CreateDiagnosticForNodeInSourceFile(config.ConfigFile.SourceFile, value[index], message, args...)
			}
		}
		return nil
	})
}
func createDiagnosticAtProjectReferenceProperty(sourceFile *TsConfigSourceFile, index int, propertyName string, message *diagnostics.Message, args ...any) *ast.Diagnostic {
	var node ast.Handle
	if sourceFile != nil {
		node = ForEachTsConfigPropArray(sourceFile.SourceFile, "references", func(property ast.Handle) ast.Handle {
			if ast.IsArrayLiteralExpression(property.Initializer()) {
				elements := property.Initializer().Elements()
				if len(elements) > index && ast.IsObjectLiteralExpression(elements[index]) {
					if propertyNode := ForEachPropertyAssignment(elements[index], propertyName, func(property ast.Handle) ast.Handle {
						return property.Initializer()
					}); !propertyNode.IsNil() {
						return propertyNode
					}
					return elements[index]
				}
			}
			return ast.Handle{}
		})
	}
	return CreateDiagnosticForNodeInSourceFileOrCompilerDiagnostic(tsconfigToSourceFile(sourceFile), node, message, args...)
}
func GetCallbackForFindingPropertyAssignmentByValue(value string) func(property ast.Handle) ast.Handle {
	return func(property ast.Handle) ast.Handle {
		if ast.IsArrayLiteralExpression(property.Initializer()) {
			return core.Find(property.Initializer().Elements(), func(element ast.Handle) bool {
				return ast.IsStringLiteral(element) && element.Text() == value
			})
		}
		return ast.Handle{}
	}
}
func GetOptionsSyntaxByArrayElementValue(objectLiteral ast.Handle, propKey string, elementValue string) ast.Handle {
	return ForEachPropertyAssignment(objectLiteral, propKey, GetCallbackForFindingPropertyAssignmentByValue(elementValue))
}

func getContentMapperSyntax(sourceFile *ast.SourceFile, index int, subKey string) ast.Handle {
	if sourceFile == nil {
		return ast.Handle{}
	}
	return ForEachTsConfigPropArray(sourceFile, "contentMappers", func(property ast.Handle) ast.Handle {
		if !ast.IsArrayLiteralExpression(property.Initializer()) {
			return property.Initializer()
		}
		elements := property.Initializer().Elements()
		if index < 0 || index >= len(elements) {
			return property.Initializer()
		}
		element := elements[index]
		if subKey != "" && ast.IsObjectLiteralExpression(element) {
			if node := ForEachPropertyAssignment(element, subKey, func(property ast.Handle) ast.Handle {
				return property.Initializer()
			}); !node.IsNil() {
				return node
			}
		}
		return element
	})
}
func GetContentMapperOptionDiagnosticLocation(config *ParsedCommandLine, mapper *contentmapper.Mapper, path []contentmapper.OptionPathSegment) (*ast.SourceFile, core.TextRange) {
	if config == nil || config.ConfigFile == nil {
		return nil, core.UndefinedTextRange()
	}
	index := slices.Index(config.ContentMappers(), mapper)
	mapperNode := getContentMapperSyntax(config.ConfigFile.SourceFile, index, "")
	node := getContentMapperSyntax(config.ConfigFile.SourceFile, index, "options")
	if node.IsNil() {
		node = mapperNode
	}
	for _, segment := range path {
		var next ast.Handle
		switch {
		case segment.IsIndex && ast.IsArrayLiteralExpression(node):
			elements := node.Elements()
			if segment.Index < len(elements) {
				next = elements[segment.Index]
			}
		case !segment.IsIndex && ast.IsObjectLiteralExpression(node):
			next = ForEachPropertyAssignment(node, segment.Property, func(property ast.Handle) ast.Handle {
				return property.Initializer()
			})
		}
		if next.IsNil() {
			break
		}
		node = next
	}
	if node.IsNil() {
		return nil, core.UndefinedTextRange()
	}
	file := config.ConfigFile.SourceFile
	return file, core.NewTextRange(scanner.SkipTrivia(file.Text(), node.Pos()), node.End())
}

func getContentMappersKeySyntax(sourceFile *ast.SourceFile) ast.Handle {
	if sourceFile == nil {
		return ast.Handle{}
	}
	return ForEachTsConfigPropArray(sourceFile, "contentMappers", func(property ast.Handle) ast.Handle {
		return property.Name()
	})
}

func getContentMapperExtensionSyntax(sourceFile *ast.SourceFile, index int, ext string) ast.Handle {
	node := getContentMapperSyntax(sourceFile, index, "extensions")
	if !node.IsNil() && ast.IsArrayLiteralExpression(node) {
		if element := core.Find(node.Elements(), func(element ast.Handle) bool {
			return ast.IsStringLiteral(element) && element.Text() == ext
		}); !element.IsNil() {
			return element
		}
	}
	return node
}

func setContentMapperDiagnosticLocation(diagnostic *ast.Diagnostic, sourceFile *ast.SourceFile, node ast.Handle) *ast.Diagnostic {
	if sourceFile != nil && !node.IsNil() {
		diagnostic.SetFile(sourceFile)
		diagnostic.SetLocation(core.NewTextRange(scanner.SkipTrivia(sourceFile.Text(), node.Pos()), node.End()))
	}
	return diagnostic
}
func ForEachPropertyAssignment[T any](objectLiteral ast.Handle, key string, callback func(property ast.Handle) T, key2 ...string) T {
	if !objectLiteral.IsNil() {
		for _, property := range objectLiteral.Properties() {
			if !ast.IsPropertyAssignment(property) {
				continue
			}
			if propName, ok := ast.TryGetTextOfPropertyName(property.Name()); ok {
				if propName == key || (len(key2) > 0 && key2[0] == propName) {
					return callback(property)
				}
			}
		}
	}
	var zero T
	return zero
}
func getTsConfigObjectLiteralExpression(tsConfigSourceFile *ast.SourceFile) ast.Handle {
	if tsConfigSourceFile != nil && len(tsConfigSourceFile.ParseRoot().Statements()) > 0 {
		expression := tsConfigSourceFile.ParseRoot().Statements()[0].Expression()
		if ast.IsObjectLiteralExpression(expression) {
			return expression
		}
	}
	return ast.Handle{}
}
func getSubstitutedPathWithConfigDirTemplate(value string, basePath string) string {
	return tspath.GetNormalizedAbsolutePath(strings.Replace(value, configDirTemplate, "./", 1), basePath)
}
func getSubstitutedStringArrayWithConfigDirTemplate(list []string, basePath string) []string {
	var result []string
	for i, element := range list {
		if startsWithConfigDirTemplate(element) {
			if result == nil {
				result = slices.Clone(list)
			}
			result[i] = getSubstitutedPathWithConfigDirTemplate(element, basePath)
		}
	}
	if result != nil {
		return result
	}
	return nil
}
func handleOptionConfigDirTemplateSubstitution(compilerOptions *core.CompilerOptions, basePath string) {
	if compilerOptions == nil {
		return
	}
	var paths *collections.OrderedMap[string, []string]
	for k, v := range compilerOptions.Paths.Entries() {
		if substitution := getSubstitutedStringArrayWithConfigDirTemplate(v, basePath); substitution != nil {
			if paths == nil {
				paths = compilerOptions.Paths.Clone()
				compilerOptions.Paths = paths
			}
			paths.Set(k, substitution)
		}
	}
	if rootDirs := getSubstitutedStringArrayWithConfigDirTemplate(compilerOptions.RootDirs, basePath); rootDirs != nil {
		compilerOptions.RootDirs = rootDirs
	}
	if typeRoots := getSubstitutedStringArrayWithConfigDirTemplate(compilerOptions.TypeRoots, basePath); typeRoots != nil {
		compilerOptions.TypeRoots = typeRoots
	}
	if startsWithConfigDirTemplate(compilerOptions.GenerateCpuProfile) {
		compilerOptions.GenerateCpuProfile = getSubstitutedPathWithConfigDirTemplate(compilerOptions.GenerateCpuProfile, basePath)
	}
	if startsWithConfigDirTemplate(compilerOptions.GenerateTrace) {
		compilerOptions.GenerateTrace = getSubstitutedPathWithConfigDirTemplate(compilerOptions.GenerateTrace, basePath)
	}
	if startsWithConfigDirTemplate(compilerOptions.OutFile) {
		compilerOptions.OutFile = getSubstitutedPathWithConfigDirTemplate(compilerOptions.OutFile, basePath)
	}
	if startsWithConfigDirTemplate(compilerOptions.OutDir) {
		compilerOptions.OutDir = getSubstitutedPathWithConfigDirTemplate(compilerOptions.OutDir, basePath)
	}
	if startsWithConfigDirTemplate(compilerOptions.RootDir) {
		compilerOptions.RootDir = getSubstitutedPathWithConfigDirTemplate(compilerOptions.RootDir, basePath)
	}
	if startsWithConfigDirTemplate(compilerOptions.TsBuildInfoFile) {
		compilerOptions.TsBuildInfoFile = getSubstitutedPathWithConfigDirTemplate(compilerOptions.TsBuildInfoFile, basePath)
	}
	if startsWithConfigDirTemplate(compilerOptions.BaseUrl) {
		compilerOptions.BaseUrl = getSubstitutedPathWithConfigDirTemplate(compilerOptions.BaseUrl, basePath)
	}
	if startsWithConfigDirTemplate(compilerOptions.DeclarationDir) {
		compilerOptions.DeclarationDir = getSubstitutedPathWithConfigDirTemplate(compilerOptions.DeclarationDir, basePath)
	}
}

func hasFileWithHigherPriorityExtension(file string, extensions [][]string, hasFile func(fileName string) bool) bool {
	var extensionGroup []string
	for _, group := range extensions {
		if tspath.FileExtensionIsOneOf(file, group) {
			extensionGroup = append(extensionGroup, group...)
		}
	}
	if len(extensionGroup) == 0 {
		return false
	}
	for _, ext := range extensionGroup {
		if tspath.FileExtensionIs(file, ext) && (ext != tspath.ExtensionTs || !tspath.FileExtensionIs(file, tspath.ExtensionDts)) {
			return false
		}
		if hasFile(tspath.ChangeExtension(file, ext)) {
			if ext == tspath.ExtensionDts && (tspath.FileExtensionIs(file, tspath.ExtensionJs) || tspath.FileExtensionIs(file, tspath.ExtensionJsx)) {
				continue
			}
			return true
		}
	}
	return false
}

func removeWildcardFilesWithLowerPriorityExtension(file string, wildcardFiles *collections.OrderedMap[string, string], extensions [][]string, keyMapper func(value string) string) {
	var extensionGroup []string
	for _, group := range extensions {
		if tspath.FileExtensionIsOneOf(file, group) {
			extensionGroup = append(extensionGroup, group...)
		}
	}
	if extensionGroup == nil {
		return
	}
	for i := len(extensionGroup) - 1; i >= 0; i-- {
		ext := extensionGroup[i]
		if tspath.FileExtensionIs(file, ext) {
			return
		}
		lowerPriorityPath := keyMapper(tspath.ChangeExtension(file, ext))
		wildcardFiles.Delete(lowerPriorityPath)
	}
}

func getFileNamesFromConfigSpecs(configFileSpecs configFileSpecs, basePath string, options *core.CompilerOptions, host vfs.FS, extraExtensions []string) ([]string, int) {
	basePath = tspath.NormalizePath(basePath)
	keyMappper := func(value string) string {
		return tspath.GetCanonicalFileName(value, host.UseCaseSensitiveFileNames())
	}
	var literalFileMap collections.OrderedMap[string, string]
	var wildcardFileMap collections.OrderedMap[string, string]
	var wildCardJsonFileMap collections.OrderedMap[string, string]
	validatedFilesSpec := configFileSpecs.validatedFilesSpec
	validatedIncludeSpecs := configFileSpecs.validatedIncludeSpecs
	validatedExcludeSpecs := configFileSpecs.validatedExcludeSpecs
	supportedExtensions := GetSupportedExtensions(options, extraExtensions)
	supportedExtensionsWithJsonIfResolveJsonModule := GetSupportedExtensionsWithJsonIfResolveJsonModule(options, supportedExtensions)
	for _, fileName := range validatedFilesSpec {
		file := tspath.GetNormalizedAbsolutePath(fileName, basePath)
		literalFileMap.Set(keyMappper(fileName), file)
	}
	var jsonOnlyIncludeMatchers *vfsmatch.SpecMatcher
	if len(validatedIncludeSpecs) > 0 {
		files := vfsmatch.ReadDirectory(host, basePath, basePath, core.Flatten(supportedExtensionsWithJsonIfResolveJsonModule), validatedExcludeSpecs, validatedIncludeSpecs, vfsmatch.UnlimitedDepth)
		for _, file := range files {
			if tspath.FileExtensionIs(file, tspath.ExtensionJson) {
				if jsonOnlyIncludeMatchers == nil {
					includes := core.Filter(validatedIncludeSpecs, func(include string) bool {
						return strings.HasSuffix(include, tspath.ExtensionJson)
					})
					jsonOnlyIncludeMatchers = vfsmatch.NewSpecMatcher(includes, basePath, vfsmatch.UsageFiles, host.UseCaseSensitiveFileNames())
				}
				var includeIndex int = -1
				if jsonOnlyIncludeMatchers != nil {
					includeIndex = jsonOnlyIncludeMatchers.MatchIndex(file)
				}
				if includeIndex != -1 {
					key := keyMappper(file)
					if !literalFileMap.Has(key) && !wildCardJsonFileMap.Has(key) {
						wildCardJsonFileMap.Set(key, file)
					}
				}
				continue
			}
			if hasFileWithHigherPriorityExtension(file, supportedExtensions, func(fileName string) bool {
				canonicalFileName := keyMappper(fileName)
				return literalFileMap.Has(canonicalFileName) || wildcardFileMap.Has(canonicalFileName)
			}) {
				continue
			}
			removeWildcardFilesWithLowerPriorityExtension(file, &wildcardFileMap, supportedExtensions, keyMappper)
			key := keyMappper(file)
			if !literalFileMap.Has(key) && !wildcardFileMap.Has(key) {
				wildcardFileMap.Set(key, file)
			}
		}
	}
	files := make([]string, 0, literalFileMap.Size()+wildcardFileMap.Size()+wildCardJsonFileMap.Size())
	for file := range literalFileMap.Values() {
		files = append(files, file)
	}
	for file := range wildcardFileMap.Values() {
		files = append(files, file)
	}
	for file := range wildCardJsonFileMap.Values() {
		files = append(files, file)
	}
	return files, literalFileMap.Size()
}
func GetSupportedExtensions(compilerOptions *core.CompilerOptions, extraExtensions []string) [][]string {
	needJSExtensions := compilerOptions.GetAllowJS()
	var builtins [][]string
	if needJSExtensions {
		builtins = tspath.AllSupportedExtensions
	} else {
		builtins = tspath.SupportedTSExtensions
	}
	if len(extraExtensions) == 0 {
		return builtins
	}
	flatBuiltins := core.Flatten(builtins)
	var result [][]string
	for _, ext := range extraExtensions {
		if !slices.Contains(flatBuiltins, ext) {
			result = append(result, []string{ext})
		}
	}
	if len(result) == 0 {
		return builtins
	}
	return slices.Concat(builtins, result)
}
func GetSupportedExtensionsWithJsonIfResolveJsonModule(compilerOptions *core.CompilerOptions, supportedExtensions [][]string) [][]string {
	if compilerOptions == nil || !compilerOptions.GetResolveJsonModule() {
		return supportedExtensions
	}
	if core.Same(supportedExtensions, tspath.AllSupportedExtensions) {
		return tspath.AllSupportedExtensionsWithJson
	}
	if core.Same(supportedExtensions, tspath.SupportedTSExtensions) {
		return tspath.SupportedTSExtensionsWithJson
	}
	return slices.Concat(supportedExtensions, [][]string{{tspath.ExtensionJson}})
}

func GetParsedCommandLineOfConfigFile(configFileName string, options *core.CompilerOptions, optionsRaw *collections.OrderedMap[string, any], sys ParseConfigHost, extendedConfigCache ExtendedConfigCache) (*ParsedCommandLine, []*ast.Diagnostic) {
	configFileName = tspath.GetNormalizedAbsolutePath(configFileName, sys.GetCurrentDirectory())
	return GetParsedCommandLineOfConfigFilePath(configFileName, tspath.ToPath(configFileName, sys.GetCurrentDirectory(), sys.FS().UseCaseSensitiveFileNames()), options, optionsRaw, sys, extendedConfigCache)
}
func GetParsedCommandLineOfConfigFilePath(configFileName string, path tspath.Path, options *core.CompilerOptions, optionsRaw *collections.OrderedMap[string, any], sys ParseConfigHost, extendedConfigCache ExtendedConfigCache) (*ParsedCommandLine, []*ast.Diagnostic) {
	errors := []*ast.Diagnostic{}
	configFileText, errors := tryReadFile(configFileName, sys.FS().ReadFile, errors)
	if len(errors) > 0 {
		return nil, errors
	}
	tsConfigSourceFile := NewTsconfigSourceFileFromFilePath(configFileName, path, configFileText)
	return ParseJsonSourceFileConfigFileContent(tsConfigSourceFile, sys, tspath.GetDirectoryPath(configFileName), options, optionsRaw, configFileName, nil, extendedConfigCache), nil
}
