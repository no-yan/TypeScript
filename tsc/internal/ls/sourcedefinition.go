package ls

import (
	"context"
	"github.com/microsoft/TypeScript/tsc/internal/ast"
	"github.com/microsoft/TypeScript/tsc/internal/astnav"
	"github.com/microsoft/TypeScript/tsc/internal/binder"
	"github.com/microsoft/TypeScript/tsc/internal/checker"
	"github.com/microsoft/TypeScript/tsc/internal/collections"
	"github.com/microsoft/TypeScript/tsc/internal/compiler"
	"github.com/microsoft/TypeScript/tsc/internal/core"
	"github.com/microsoft/TypeScript/tsc/internal/ls/lsconv"
	"github.com/microsoft/TypeScript/tsc/internal/lsp/lsproto"
	"github.com/microsoft/TypeScript/tsc/internal/module"
	"github.com/microsoft/TypeScript/tsc/internal/modulespecifiers"
	"github.com/microsoft/TypeScript/tsc/internal/parser"
	"github.com/microsoft/TypeScript/tsc/internal/spanmap"
	"github.com/microsoft/TypeScript/tsc/internal/tspath"
	"github.com/microsoft/TypeScript/tsc/internal/vfs"
	"math"
	"slices"
	"strings"
)

func (l *LanguageService) ProvideSourceDefinition(ctx context.Context, documentURI lsproto.DocumentUri, position lsproto.Position) (lsproto.DefinitionResponse, error) {
	program, file := l.getProgramAndFile(documentURI)
	positions := lsconv.FromLSPPositionForSourceFile(l.converters, file, position, spanmap.FeatureDefinition)
	results := make([]lsproto.DefinitionResponse, 0, len(positions))
	for _, mapped := range positions {
		if mapped.Fidelity.IsSingleSegment() {
			result, err := l.provideSourceDefinitionAtPosition(ctx, program, mapped.Script, mapped.Position)
			if err != nil {
				return lsproto.DefinitionResponse{}, err
			}
			results = append(results, result)
		}
	}
	return combineDefinitionResponses(results, lsproto.GetClientCapabilities(ctx).TextDocument.Definition.LinkSupport), nil
}
func (l *LanguageService) provideSourceDefinitionAtPosition(ctx context.Context, program *compiler.Program, file *ast.SourceFile, textPos core.TextPos) (lsproto.DefinitionResponse, error) {
	caps := lsproto.GetClientCapabilities(ctx)
	clientSupportsLink := caps.TextDocument.Definition.LinkSupport
	pos := int(textPos)
	resolver := l.newSourceDefResolver(program, file.FileName())
	node := astnav.GetTouchingPropertyName(file, pos)
	if node.Kind == ast.KindSourceFile {
		if declarations, ref := resolver.resolveTripleSlashReference(file, pos, program); len(declarations) != 0 {
			originSelectionRange, _ := l.createLspRangeFromBounds(ref.Pos(), ref.End(), file)
			return l.createDefinitionLocations(originSelectionRange, clientSupportsLink, declarations, nil, spanmap.FeatureDefinition), nil
		}
		return lsproto.LocationOrLocationsOrDefinitionLinksOrNull{}, nil
	}
	originSelectionRange, _ := l.createLspRangeFromNode(node, file)
	containingModuleSpecifier := findContainingModuleSpecifier(node)
	if node == containingModuleSpecifier {
		specifierMode := program.GetModeForUsageLocation(file, containingModuleSpecifier)
		if implementationFile := resolver.resolveImplementation(containingModuleSpecifier.Text(), specifierMode); implementationFile != "" {
			if sourceFile := resolver.getOrParseSourceFile(implementationFile); sourceFile != nil {
				return l.createDefinitionLocations(originSelectionRange, clientSupportsLink, getSourceDefinitionEntryDeclarations(sourceFile), nil, spanmap.FeatureDefinition), nil
			}
		}
		return l.provideDefinitionAtPosition(ctx, program, file, textPos, clientSupportsLink), nil
	}
	var resolvedImplFile string
	if !containingModuleSpecifier.IsNil() {
		specifierMode := program.GetModeForUsageLocation(file, containingModuleSpecifier)
		resolvedImplFile = resolver.resolveImplementation(containingModuleSpecifier.Text(), specifierMode)
	}
	if resolvedImplFile != "" {
		names := getCandidateSourceDeclarationNames(node, ast.Handle{})
		moduleResults := resolver.searchImplementationFile(node, resolvedImplFile, names)
		if len(moduleResults) != 0 {
			if !ast.IsPartOfTypeNode(node) && !ast.IsPartOfTypeOnlyImportOrExportDeclaration(node) || hasConcreteSourceDeclarations(moduleResults) {
				return l.createDefinitionLocations(originSelectionRange, clientSupportsLink, uniqueDeclarationNodes(moduleResults), nil, spanmap.FeatureDefinition), nil
			}
		}
	}
	checkerDeclarations, moduleSpecifier := getSourceDefCheckerInfo(ctx, program, file, node)
	declarations := resolver.resolveFromCheckerInfo(node, resolvedImplFile, checkerDeclarations, moduleSpecifier)
	if len(declarations) == 0 {
		if !containingModuleSpecifier.IsNil() && resolvedImplFile != "" && !hasConcreteSourceDeclarations(checkerDeclarations) {
			if sourceFile := resolver.getOrParseSourceFile(resolvedImplFile); sourceFile != nil {
				return l.createDefinitionLocations(originSelectionRange, clientSupportsLink, getSourceDefinitionEntryDeclarations(sourceFile), nil, spanmap.FeatureDefinition), nil
			}
		}
		return l.provideDefinitionAtPosition(ctx, program, file, textPos, clientSupportsLink), nil
	}
	return l.createDefinitionLocations(originSelectionRange, clientSupportsLink, declarations, nil, spanmap.FeatureDefinition), nil
}

type sourceDefResolver struct {
	ls            *LanguageService
	fs            vfs.FS
	options       *core.CompilerOptions
	getSourceFile func(string) *ast.SourceFile
	resolveFrom   string
	resolver      *module.Resolver
	parsedFiles   map[string]*ast.SourceFile
}

func (l *LanguageService) newSourceDefResolver(program *compiler.Program, resolveFrom string) *sourceDefResolver {
	options := program.Options()
	noDtsOptions := options.Clone()
	noDtsOptions.NoDtsResolution = core.TSTrue
	return &sourceDefResolver{ls: l, fs: program.Host().FS(), options: options, getSourceFile: program.GetSourceFile, resolveFrom: resolveFrom, resolver: module.NewResolver(program.Host(), noDtsOptions, program.GetGlobalTypingsCacheLocation(), "", program.CommandLine().ContentMapperExtensions())}
}

func (r *sourceDefResolver) resolveFromCheckerInfo(node ast.Handle, resolvedImplFile string, checkerDeclarations []ast.Handle, moduleSpecifier string) []ast.Handle {
	if resolvedImplFile == "" && moduleSpecifier != "" {
		resolvedImplFile = r.resolveImplementation(moduleSpecifier, r.inferImpliedNodeFormat(r.resolveFrom))
	}
	if len(checkerDeclarations) == 0 && resolvedImplFile != "" {
		names := getCandidateSourceDeclarationNames(node, ast.Handle{})
		if results := r.searchImplementationFile(node, resolvedImplFile, names); results != nil {
			return uniqueDeclarationNodes(results)
		}
	}
	var declarations []ast.Handle
	for _, declaration := range checkerDeclarations {
		declarations = append(declarations, r.mapDeclarationToSource(node, declaration, resolvedImplFile)...)
	}
	declarations = uniqueDeclarationNodes(declarations)
	if hasConcreteSourceDeclarations(declarations) {
		return declarations
	}
	return nil
}

func getSourceDefCheckerInfo(ctx context.Context, program *compiler.Program, file *ast.SourceFile, node ast.Handle) ([]ast.Handle, string) {
	c, done := program.GetTypeCheckerForFile(ctx, file)
	defer done()
	declarations := getDeclarationsFromLocation(c, node)
	isPropertyName := !node.Parent().IsNil() && ast.IsAccessExpression(node.Parent()) && node.Parent().Name() == node
	if len(declarations) == 0 && isPropertyName {
		if left := node.Parent().Expression(); !left.IsNil() {
			if prop := c.GetPropertyOfType(c.GetTypeAtLocation(left), node.Text()); prop != nil {
				declarations = ast.DeclarationNodes(prop).Slice()
			}
		}
	}
	if calledDeclaration := tryGetSignatureDeclaration(c, node); !calledDeclaration.IsNil() {
		nonFunctionDeclarations := core.Filter(declarations, func(node ast.Handle) bool {
			return !ast.IsFunctionLike(node)
		})
		declarations = append(nonFunctionDeclarations, calledDeclaration)
	}
	var moduleSpecifier string
	resolveNode := node
	if isPropertyName {
		expr := node.Parent().Expression()
		for !expr.IsNil() && ast.IsAccessExpression(expr) {
			expr = expr.Expression()
		}
		if !expr.IsNil() {
			resolveNode = expr
		}
	}
	if sym := c.GetSymbolAtLocation(resolveNode); sym != nil {
		for _, d := range ast.DeclarationNodes(sym).All() {
			if !ast.IsImportSpecifier(d) && !ast.IsImportClause(d) && !ast.IsNamespaceImport(d) && !ast.IsImportEqualsDeclaration(d) {
				continue
			}
			if spec := checker.TryGetModuleSpecifierFromDeclaration(d); !spec.IsNil() {
				moduleSpecifier = spec.Text()
				break
			}
		}
	}
	return declarations, moduleSpecifier
}

func (r *sourceDefResolver) resolveTripleSlashReference(file *ast.SourceFile, pos int, program *compiler.Program) ([]ast.Handle, *ast.FileReference) {
	ref := getReferenceAtPosition(file, pos, program)
	if ref == nil || ref.file == nil {
		return nil, nil
	}
	if !ref.file.IsDeclarationFile {
		return getSourceDefinitionEntryDeclarations(ref.file), ref.reference
	}
	dtsFileName := ref.file.FileName()
	preferredMode := r.inferImpliedNodeFormat(dtsFileName)
	implementationFile := r.findImplementationFileFromDtsFileName(dtsFileName, preferredMode)
	if implementationFile == "" {
		return nil, nil
	}
	sourceFile := r.getOrParseSourceFile(implementationFile)
	if sourceFile == nil {
		return nil, nil
	}
	return getSourceDefinitionEntryDeclarations(sourceFile), ref.reference
}

func (r *sourceDefResolver) searchImplementationFile(originalNode ast.Handle, implementationFile string, names []string) []ast.Handle {
	if implementationFile == "" {
		return nil
	}
	sourceFile := r.getOrParseSourceFile(implementationFile)
	if sourceFile == nil {
		return nil
	}
	if isDefaultImportName(originalNode) {
		defaultDeclarations := r.findDeclarationsInFile(implementationFile, []string{"default"}, &collections.Set[string]{})
		if len(defaultDeclarations) != 0 {
			return filterPreferredSourceDeclarations(originalNode, defaultDeclarations)
		}
		return getSourceDefinitionEntryDeclarations(sourceFile)
	}
	declarations := r.findDeclarationsInFile(implementationFile, names, &collections.Set[string]{})
	if len(declarations) != 0 {
		return filterPreferredSourceDeclarations(originalNode, declarations)
	}
	return nil
}
func isDefaultImportName(node ast.Handle) bool {
	if node.IsNil() || node.Parent().IsNil() || !ast.IsImportClause(node.Parent()) || node.Parent().Name() != node || node.Parent().Parent().IsNil() {
		return false
	}
	return ast.IsDefaultImport(node.Parent().Parent())
}
func getSourceDefinitionEntryNode(sourceFile *ast.SourceFile) ast.Handle {
	if len(sourceFile.ParseRoot().Statements()) != 0 {
		return sourceFile.ParseRoot().Statements()[0]
	}
	return sourceFile.ParseRoot()
}
func getSourceDefinitionEntryDeclarations(sourceFile *ast.SourceFile) []ast.Handle {
	return []ast.Handle{getSourceDefinitionEntryNode(sourceFile)}
}
func (r *sourceDefResolver) mapDeclarationToSource(originalNode ast.Handle, declaration ast.Handle, resolvedImplFile string) []ast.Handle {
	file, startPos := getFileAndStartPosFromDeclaration(declaration)
	fileName := file.FileName()
	if mapped := r.ls.tryGetSourcePosition(fileName, startPos); mapped != nil {
		if sourceFile := r.getOrParseSourceFile(mapped.FileName); sourceFile != nil {
			return []ast.Handle{findClosestDeclarationNode(sourceFile, mapped.Pos)}
		}
	}
	if !tspath.IsDeclarationFileName(fileName) {
		return []ast.Handle{declaration}
	}
	implementationFile := resolvedImplFile
	if implementationFile == "" {
		dtsFileName := ast.GetSourceFileOfNode(declaration).FileName()
		preferredMode := r.inferImpliedNodeFormat(dtsFileName)
		implementationFile = r.findImplementationFileFromDtsFileName(dtsFileName, preferredMode)
	}
	return r.searchImplementationFile(originalNode, implementationFile, getCandidateSourceDeclarationNames(originalNode, declaration))
}
func (r *sourceDefResolver) findImplementationFileFromDtsFileName(dtsFileName string, preferredMode core.ResolutionMode) string {
	if jsExt := module.TryGetJSExtensionForFile(dtsFileName, r.options); jsExt != "" {
		candidate := tspath.ChangeExtension(dtsFileName, jsExt)
		if r.fs.FileExists(candidate) {
			return candidate
		}
	}
	parts := modulespecifiers.GetNodeModulePathParts(dtsFileName)
	if parts == nil {
		return ""
	}
	if strings.LastIndex(dtsFileName, "/node_modules/") != parts.TopLevelNodeModulesIndex {
		return ""
	}
	packageNamePathPart := dtsFileName[parts.TopLevelPackageNameIndex+1 : parts.PackageRootIndex]
	packageName := module.GetPackageNameFromTypesPackageName(module.UnmangleScopedPackageName(packageNamePathPart))
	if packageName == "" {
		return ""
	}
	pathToFileInPackage := dtsFileName[parts.PackageRootIndex+1:]
	if pathToFileInPackage != "" {
		specifier := packageName + "/" + tspath.RemoveFileExtension(pathToFileInPackage)
		if implementationFile := r.resolveImplementation(specifier, preferredMode); implementationFile != "" {
			return implementationFile
		}
	}
	return r.resolveImplementation(packageName, preferredMode)
}
func (r *sourceDefResolver) resolveImplementation(moduleName string, preferredMode core.ResolutionMode) string {
	return r.resolveImplementationFrom(moduleName, r.resolveFrom, preferredMode)
}
func (r *sourceDefResolver) resolveImplementationFrom(moduleName string, resolveFromFile string, preferredMode core.ResolutionMode) string {
	modes := []core.ResolutionMode{preferredMode}
	if preferredMode != core.ModuleKindESNext {
		modes = append(modes, core.ModuleKindESNext)
	}
	if preferredMode != core.ModuleKindCommonJS {
		modes = append(modes, core.ModuleKindCommonJS)
	}
	for _, mode := range modes {
		resolved, _ := r.resolver.ResolveModuleName(moduleName, resolveFromFile, mode, nil)
		if resolved != nil && resolved.IsResolved() && !tspath.IsDeclarationFileName(resolved.ResolvedFileName) {
			return resolved.ResolvedFileName
		}
	}
	return ""
}
func (r *sourceDefResolver) getOrParseSourceFile(fileName string) *ast.SourceFile {
	if sourceFile := r.getSourceFile(fileName); sourceFile != nil {
		return sourceFile
	}
	if sourceFile, ok := r.parsedFiles[fileName]; ok {
		return sourceFile
	}
	var sourceFile *ast.SourceFile
	if text, ok := r.ls.ReadFile(fileName); ok {
		sourceFile = parser.ParseSourceFile(ast.SourceFileParseOptions{FileName: fileName, Path: r.ls.toPath(fileName)}, text, core.EnsureScriptKindFromFileName(fileName))
		binder.BindSourceFile(sourceFile)
	}
	if r.parsedFiles == nil {
		r.parsedFiles = map[string]*ast.SourceFile{}
	}
	r.parsedFiles[fileName] = sourceFile
	return sourceFile
}

func (r *sourceDefResolver) inferImpliedNodeFormat(fileName string) core.ResolutionMode {
	var packageJsonType string
	if scope := r.resolver.GetPackageScopeForPath(tspath.GetDirectoryPath(fileName)); scope.Exists() {
		if value, ok := scope.Contents.Type.GetValue(); ok {
			packageJsonType = value
		}
	}
	return ast.GetImpliedNodeFormatForFile(fileName, packageJsonType)
}
func findContainingModuleSpecifier(node ast.Handle) ast.Handle {
	for current := node; !current.IsNil(); current = current.Parent() {
		if ast.IsAnyImportOrReExport(current) || ast.IsRequireCall(current, true) || ast.IsImportCall(current) {
			if moduleSpecifier := ast.GetExternalModuleName(current); !moduleSpecifier.IsNil() && ast.IsStringLiteralLike(moduleSpecifier) {
				return moduleSpecifier
			}
		}
	}
	return ast.Handle{}
}
func (r *sourceDefResolver) findDeclarationsInFile(fileName string, names []string, seen *collections.Set[string]) []ast.Handle {
	if fileName == "" || len(names) == 0 {
		return nil
	}
	if !seen.AddIfAbsent(fileName) {
		return nil
	}
	sourceFile := r.getOrParseSourceFile(fileName)
	if sourceFile == nil {
		return nil
	}
	declarations := findDeclarationNodesByName(sourceFile, names)
	if len(declarations) != 0 && hasConcreteSourceDeclarations(declarations) {
		return declarations
	}
	var forwarded []ast.Handle
	for _, forwardedFile := range r.getForwardedImplementationFiles(sourceFile) {
		forwarded = append(forwarded, r.findDeclarationsInFile(forwardedFile, names, seen)...)
	}
	if len(forwarded) != 0 {
		if hasConcreteSourceDeclarations(forwarded) {
			return uniqueDeclarationNodes(forwarded)
		}
		return uniqueDeclarationNodes(append(slices.Clip(declarations), forwarded...))
	}
	return declarations
}
func (r *sourceDefResolver) getForwardedImplementationFiles(sourceFile *ast.SourceFile) []string {
	preferredMode := r.inferImpliedNodeFormat(sourceFile.FileName())
	var files []string
	for _, imp := range sourceFile.Imports() {
		moduleName := imp.Text()
		if implementationFile := r.resolveImplementationFrom(moduleName, sourceFile.FileName(), preferredMode); implementationFile != "" {
			files = append(files, implementationFile)
		}
	}
	return core.Deduplicate(files)
}
func getCandidateSourceDeclarationNames(originalNode ast.Handle, declaration ast.Handle) []string {
	var names []string
	if !declaration.IsNil() {
		if name := ast.GetNameOfDeclaration(declaration); !name.IsNil() {
			if text := ast.GetTextOfPropertyName(name); text != "" {
				names = append(names, text)
			}
		}
		if declaration.Kind == ast.KindExportAssignment {
			names = append(names, "default")
		}
		if (ast.IsFunctionDeclaration(declaration) || ast.IsClassDeclaration(declaration)) && declaration.ModifierFlags()&ast.ModifierFlagsExportDefault == ast.ModifierFlagsExportDefault {
			names = append(names, "default")
		}
		if ast.IsImportSpecifier(declaration) || ast.IsExportSpecifier(declaration) {
			if propName := declaration.PropertyName(); !propName.IsNil() {
				names = append(names, propName.Text())
			}
		}
	}
	if !originalNode.IsNil() {
		if ast.IsIdentifier(originalNode) || ast.IsPrivateIdentifier(originalNode) {
			names = append(names, originalNode.Text())
		}
		if isDefaultImportName(originalNode) {
			names = append(names, "default")
		}
		if !originalNode.Parent().IsNil() {
			if ast.IsImportSpecifier(originalNode.Parent()) || ast.IsExportSpecifier(originalNode.Parent()) {
				if propName := originalNode.Parent().PropertyName(); !propName.IsNil() {
					names = append(names, propName.Text())
				}
			}
		}
	}
	return names
}
func findDeclarationNodesByName(sourceFile *ast.SourceFile, names []string) []ast.Handle {
	names = core.Deduplicate(core.Filter(names, func(name string) bool {
		return name != ""
	}))
	if len(names) == 0 {
		return nil
	}
	var wanted collections.Set[string]
	wantDefault := false
	for _, name := range names {
		if name == "default" {
			wantDefault = true
			continue
		}
		wanted.Add(name)
	}
	type candidate struct {
		node  ast.Handle
		depth int
	}
	var candidates []candidate
	minDepth := math.MaxInt
	var visit ast.StoreVisitor
	visit = func(node ast.Handle) bool {
		matched := false
		if name := ast.GetNameOfDeclaration(node); !name.IsNil() {
			if text := ast.GetTextOfPropertyName(name); text != "" {
				if wanted.Has(text) {
					matched = true
				}
			}
		}
		if wantDefault && node.Kind == ast.KindExportAssignment {
			matched = true
		}
		if wantDefault && (ast.IsFunctionDeclaration(node) || ast.IsClassDeclaration(node)) && node.ModifierFlags()&ast.ModifierFlagsExportDefault == ast.ModifierFlagsExportDefault {
			matched = true
		}
		if matched {
			depth := getContainerDepth(node)
			candidates = append(candidates, candidate{node: node, depth: depth})
			if depth < minDepth {
				minDepth = depth
			}
		}
		return node.ForEachChild(visit)
	}
	sourceFile.ParseRoot().ForEachChild(visit)
	var declarations []ast.Handle
	for _, c := range candidates {
		if c.depth == minDepth {
			declarations = append(declarations, c.node)
		}
	}
	return uniqueDeclarationNodes(declarations)
}

func getContainerDepth(node ast.Handle) int {
	depth := 0
	current := node
	for !current.IsNil() {
		current = getContainerNode(current)
		depth++
	}
	return depth
}
func filterPreferredSourceDeclarations(originalNode ast.Handle, declarations []ast.Handle) []ast.Handle {
	if len(declarations) <= 1 || originalNode.IsNil() {
		return declarations
	}
	if preferred := getPropertyLikeSourceDeclarations(originalNode, declarations); len(preferred) != 0 {
		return preferred
	}
	if preferred := core.Filter(declarations, isConcreteSourceDeclaration); len(preferred) != 0 {
		return preferred
	}
	return declarations
}
func getPropertyLikeSourceDeclarations(originalNode ast.Handle, declarations []ast.Handle) []ast.Handle {
	if originalNode.Parent().IsNil() || !ast.IsAccessExpression(originalNode.Parent()) || originalNode.Parent().Name() != originalNode {
		return nil
	}
	return core.Filter(declarations, func(node ast.Handle) bool {
		switch node.Kind {
		case ast.KindPropertyAssignment, ast.KindShorthandPropertyAssignment, ast.KindPropertyDeclaration, ast.KindPropertySignature, ast.KindMethodDeclaration, ast.KindMethodSignature, ast.KindGetAccessor, ast.KindSetAccessor, ast.KindEnumMember:
			return true
		default:
			return false
		}
	})
}
func hasConcreteSourceDeclarations(declarations []ast.Handle) bool {
	return slices.ContainsFunc(declarations, isConcreteSourceDeclaration)
}
func isConcreteSourceDeclaration(node ast.Handle) bool {
	if !ast.IsDeclaration(node) || node.Kind == ast.KindExportAssignment {
		return false
	}
	if (ast.IsBinaryExpression(node) || ast.IsCallExpression(node)) && ast.GetAssignmentDeclarationKind(node) != ast.JSDeclarationKindNone {
		return false
	}
	switch node.Kind {
	case ast.KindParameter, ast.KindTypeParameter, ast.KindBindingElement, ast.KindImportClause, ast.KindImportSpecifier, ast.KindNamespaceImport, ast.KindExportSpecifier, ast.KindPropertyAccessExpression, ast.KindElementAccessExpression:
		return false
	default:
		return true
	}
}
func uniqueDeclarationNodes(nodes []ast.Handle) []ast.Handle {
	type declarationKey struct {
		fileName string
		loc      core.TextRange
	}
	var seen collections.Set[declarationKey]
	result := make([]ast.Handle, 0, len(nodes))
	for _, node := range nodes {
		if node.IsNil() {
			continue
		}
		fileName := ast.GetSourceFileOfNode(node).FileName()
		key := declarationKey{fileName: fileName, loc: node.Loc()}
		if !seen.AddIfAbsent(key) {
			continue
		}
		result = append(result, node)
	}
	return result
}
func findClosestDeclarationNode(sourceFile *ast.SourceFile, pos int) ast.Handle {
	node := astnav.GetTouchingPropertyName(sourceFile, pos)
	for current := node; !current.IsNil(); current = current.Parent() {
		if ast.IsDeclaration(current) || current.Kind == ast.KindExportAssignment {
			return current
		}
	}
	return getSourceDefinitionEntryNode(sourceFile)
}
