package declarations

import (
	"fmt"
	"github.com/microsoft/TypeScript/tsc/internal/ast"
	"github.com/microsoft/TypeScript/tsc/internal/collections"
	"github.com/microsoft/TypeScript/tsc/internal/core"
	"github.com/microsoft/TypeScript/tsc/internal/debug"
	"github.com/microsoft/TypeScript/tsc/internal/diagnostics"
	"github.com/microsoft/TypeScript/tsc/internal/jsnum"
	"github.com/microsoft/TypeScript/tsc/internal/modulespecifiers"
	"github.com/microsoft/TypeScript/tsc/internal/nodebuilder"
	"github.com/microsoft/TypeScript/tsc/internal/printer"
	"github.com/microsoft/TypeScript/tsc/internal/scanner"
	"github.com/microsoft/TypeScript/tsc/internal/transformers"
	"github.com/microsoft/TypeScript/tsc/internal/tspath"
	"iter"
	"slices"
	"strings"
)

type ReferencedFilePair struct {
	file *ast.SourceFile
	ref  *ast.FileReference
}
type OutputPaths interface {
	DeclarationFilePath() string
	JsFilePath() string
}

type DeclarationEmitHost interface {
	modulespecifiers.ModuleSpecifierGenerationHost
	GetCurrentDirectory() string
	UseCaseSensitiveFileNames() bool
	GetSourceFileFromReference(origin *ast.SourceFile, ref *ast.FileReference) *ast.SourceFile
	GetOutputPathsFor(file *ast.SourceFile, forceDtsPaths bool) OutputPaths
	SourceFileMayBeEmitted(file *ast.SourceFile, forceDtsEmit bool) bool
	GetResolutionModeOverride(node ast.Handle) core.ResolutionMode
	GetEffectiveDeclarationFlags(node ast.Handle, flags ast.ModifierFlags) ast.ModifierFlags
	GetEmitResolver() printer.EmitResolver
}
type thisPropertyAssignmentKey struct {
	name      string
	node      ast.Handle
	isStatic  bool
	isPrivate bool
}

func getThisPropertyAssignmentKey(name ast.Handle, node ast.Handle, isStatic bool) thisPropertyAssignmentKey {
	isPrivate := ast.IsPrivateIdentifier(name)
	if !name.IsNil() && !ast.IsDynamicName(name) {
		if nameText, ok := ast.TryGetTextOfPropertyName(name); ok {
			return thisPropertyAssignmentKey{name: nameText, isStatic: isStatic, isPrivate: isPrivate}
		}
	}
	return thisPropertyAssignmentKey{node: node, isStatic: isStatic, isPrivate: isPrivate}
}

type DeclarationTransformer struct {
	transformers.Transformer
	host                             DeclarationEmitHost
	compilerOptions                  *core.CompilerOptions
	tracker                          *SymbolTrackerImpl
	state                            *SymbolTrackerSharedState
	resolver                         printer.EmitResolver
	declarationFilePath              string
	declarationMapPath               string
	needsDeclare                     bool
	needsScopeFixMarker              bool
	resultHasScopeMarker             bool
	enclosingDeclaration             ast.Handle
	resultHasExternalModuleIndicator bool
	suppressNewDiagnosticContexts    bool
	witnessedCjsExports              collections.Set[string]
	lateStatementReplacementMap      map[ast.GlobalRef]ast.Handle
	expandoHosts                     map[ast.GlobalRef]ast.Handle
	expandoMembers                   map[ast.GlobalRef][]ast.Handle
	deferredExpandoAssignments       map[ast.GlobalRef][]ast.Handle
	seenProperties                   collections.Set[thisPropertyAssignmentKey]
	thisPropertyAssignmentsCollected []ast.Handle
	rawReferencedFiles               []ReferencedFilePair
	rawTypeReferenceDirectives       []*ast.FileReference
	rawLibReferenceDirectives        []*ast.FileReference
	bindingNameVisitor               *ast.HandleVisitor
	expressionVisitor                *ast.HandleVisitor
	cjsExportAssignmentVisitor       *ast.HandleVisitor
	exportStrippingVisitor           *ast.HandleVisitor
	thisPropertyVisitor              *ast.HandleVisitor
	cjsExportAssignment              ast.Handle
	cjsExportMembers                 []ast.Handle
	cjsExportAssignmentName          ast.Handle
	declareStrippingVisitor          *ast.HandleVisitor
	inClassExpressionDeclaration     bool
}

func NewDeclarationTransformer(host DeclarationEmitHost, context *printer.EmitContext, compilerOptions *core.CompilerOptions, declarationFilePath string, declarationMapPath string) *DeclarationTransformer {
	resolver := host.GetEmitResolver()
	state := &SymbolTrackerSharedState{isolatedDeclarations: compilerOptions.IsolatedDeclarations.IsTrue(), stripInternal: compilerOptions.StripInternal.IsTrue(), resolver: resolver}
	tracker := NewSymbolTracker(host, resolver, state)
	tx := &DeclarationTransformer{host: host, compilerOptions: compilerOptions, tracker: tracker, state: state, resolver: resolver, declarationFilePath: declarationFilePath, declarationMapPath: declarationMapPath}
	tx.state.reportExpandoFunctionErrors = func(node ast.Handle) {
		if !tx.state.isolatedDeclarations {
			return
		}
		props := resolver.GetPropertiesOfContainerFunction(node)
		for _, p := range props {
			if ast.IsExpandoPropertyDeclaration(ast.NodeOf(p.ValueDeclaration)) {
				errorTarget := ast.NodeOf(p.ValueDeclaration)
				if ast.IsBinaryExpression(errorTarget) {
					errorTarget = errorTarget.BinaryExpressionLeft()
				}
				tx.state.addDiagnostic(createDiagnosticForNode(errorTarget, diagnostics.Assigning_properties_to_functions_without_declaring_them_is_not_supported_with_isolatedDeclarations_Add_an_explicit_declaration_for_the_properties_assigned_to_this_function))
			}
		}
	}
	tx.NewTransformer(tx.visit, context)
	tx.bindingNameVisitor = tx.EmitContext().NewNodeVisitor(tx.visitBindingName)
	tx.expressionVisitor = tx.EmitContext().NewNodeVisitor(tx.visitNestedExpression)
	tx.exportStrippingVisitor = tx.EmitContext().NewNodeVisitor(tx.stripExportModifiers)
	tx.thisPropertyVisitor = tx.EmitContext().NewNodeVisitor(tx.visitThisPropertyAssignments)
	tx.cjsExportAssignmentVisitor = tx.EmitContext().NewNodeVisitor(tx.visitCJSExportAssignments)
	tx.declareStrippingVisitor = tx.EmitContext().NewNodeVisitor(tx.stripDeclareModifiers)
	return tx
}
func (tx *DeclarationTransformer) GetDiagnostics() []*ast.Diagnostic {
	return tx.state.diagnostics
}
func (tx *DeclarationTransformer) shouldStripInternal(node ast.Handle) bool {
	return tx.state.stripInternal && !node.IsNil() && tx.isInternalDeclaration(node, tx.state.currentSourceFile)
}
func (tx *DeclarationTransformer) isInternalDeclaration(node ast.Handle, sourceFile *ast.SourceFile) bool {
	if node.IsNil() {
		return false
	}
	parseTreeNode := tx.EmitContext().MostOriginal(node)
	if !ast.IsParseTreeNode(parseTreeNode) {
		return false
	}
	if parseTreeNode.Kind == ast.KindParameter {
		params := parseTreeNode.Parent().Parameters()
		paramIdx := slices.IndexFunc(params, func(p ast.Handle) bool {
			return p == parseTreeNode
		})
		var previousSibling ast.Handle
		if paramIdx > 0 {
			previousSibling = params[paramIdx-1]
		}
		text := sourceFile.Text()
		var commentRanges []ast.CommentRange
		if !previousSibling.IsNil() {
			trailingPos := scanner.SkipTriviaEx(text, previousSibling.End()+1, &scanner.SkipTriviaOptions{StopAtComments: true})
			for comment := range scanner.GetTrailingCommentRanges(text, trailingPos) {
				commentRanges = append(commentRanges, comment)
			}
			for comment := range scanner.GetLeadingCommentRanges(text, node.Pos()) {
				commentRanges = append(commentRanges, comment)
			}
		} else {
			trailingPos := scanner.SkipTriviaEx(text, node.Pos(), &scanner.SkipTriviaOptions{StopAtComments: true})
			for comment := range scanner.GetTrailingCommentRanges(text, trailingPos) {
				commentRanges = append(commentRanges, comment)
			}
		}
		if len(commentRanges) > 0 {
			return hasInternalAnnotation(commentRanges[len(commentRanges)-1], sourceFile)
		}
		return false
	}
	for commentRange := range tx.getLeadingCommentRangesOfNode(parseTreeNode, sourceFile) {
		if hasInternalAnnotation(commentRange, sourceFile) {
			return true
		}
	}
	return false
}
func (tx *DeclarationTransformer) getLeadingCommentRangesOfNode(node ast.Handle, sourceFile *ast.SourceFile) iter.Seq[ast.CommentRange] {
	if node.IsNil() || node.Kind == ast.KindJsxText {
		return nil
	}
	return scanner.GetLeadingCommentRanges(sourceFile.Text(), node.Pos())
}
func hasInternalAnnotation(commentRange ast.CommentRange, sourceFile *ast.SourceFile) bool {
	comment := sourceFile.Text()[commentRange.Pos():commentRange.End()]
	return strings.Contains(comment, "@internal")
}

const declarationEmitNodeBuilderFlags = nodebuilder.FlagsMultilineObjectLiterals | nodebuilder.FlagsWriteClassExpressionAsTypeLiteral | nodebuilder.FlagsUseTypeOfFunction | nodebuilder.FlagsUseStructuralFallback | nodebuilder.FlagsAllowEmptyTuple | nodebuilder.FlagsGenerateNamesForShadowedTypeParams | nodebuilder.FlagsNoTruncation
const declarationEmitInternalNodeBuilderFlags = nodebuilder.InternalFlagsAllowUnresolvedNames

func (tx *DeclarationTransformer) visit(node ast.Handle) ast.Handle {
	if node.IsNil() {
		return ast.Handle{}
	}
	switch node.Kind {
	case ast.KindSourceFile:
		return tx.visitSourceFile(node)
	case ast.KindFunctionDeclaration, ast.KindModuleDeclaration, ast.KindImportEqualsDeclaration, ast.KindInterfaceDeclaration, ast.KindClassDeclaration, ast.KindJSTypeAliasDeclaration, ast.KindTypeAliasDeclaration, ast.KindEnumDeclaration, ast.KindVariableStatement, ast.KindImportDeclaration, ast.KindJSImportDeclaration, ast.KindExportDeclaration, ast.KindExportAssignment:
		return tx.visitDeclarationStatements(node)
	case ast.KindBreakStatement, ast.KindContinueStatement, ast.KindDebuggerStatement, ast.KindDoStatement, ast.KindEmptyStatement, ast.KindForInStatement, ast.KindForOfStatement, ast.KindForStatement, ast.KindIfStatement, ast.KindLabeledStatement, ast.KindReturnStatement, ast.KindSwitchStatement, ast.KindThrowStatement, ast.KindTryStatement, ast.KindWhileStatement, ast.KindWithStatement, ast.KindNotEmittedStatement, ast.KindBlock, ast.KindMissingDeclaration, ast.KindExpressionStatement:
		return ast.Handle{}
	default:
		return tx.visitDeclarationSubtree(node)
	}
}
func throwDiagnostic(result printer.SymbolAccessibilityResult) *SymbolAccessibilityDiagnostic {
	panic("Diagnostic emitted without context")
}
func (tx *DeclarationTransformer) visitSourceFile(node ast.Handle) ast.Handle {
	tx.cjsExportAssignmentName = ast.Handle{}
	sf := ast.GetSourceFileOfNode(node)
	if sf != nil && sf.IsDeclarationFile {
		return node
	}
	tx.needsDeclare = true
	tx.needsScopeFixMarker = false
	tx.resultHasScopeMarker = false
	tx.enclosingDeclaration = node
	tx.state.getSymbolAccessibilityDiagnostic = throwDiagnostic
	tx.resultHasExternalModuleIndicator = false
	tx.suppressNewDiagnosticContexts = false
	tx.state.lateMarkedStatements = make([]ast.Handle, 0)
	tx.lateStatementReplacementMap = make(map[ast.GlobalRef]ast.Handle)
	tx.expandoHosts = make(map[ast.GlobalRef]ast.Handle)
	tx.expandoMembers = make(map[ast.GlobalRef][]ast.Handle)
	tx.deferredExpandoAssignments = make(map[ast.GlobalRef][]ast.Handle)
	tx.rawReferencedFiles = make([]ReferencedFilePair, 0)
	tx.rawTypeReferenceDirectives = make([]*ast.FileReference, 0)
	tx.rawLibReferenceDirectives = make([]*ast.FileReference, 0)
	tx.witnessedCjsExports.Clear()
	tx.state.currentSourceFile = sf
	tx.collectFileReferences(sf)
	tx.resolver.PrecalculateDeclarationEmitVisibility(sf)
	updated := tx.transformSourceFile(sf)
	tx.state.currentSourceFile = nil
	return updated
}
func (tx *DeclarationTransformer) collectFileReferences(sourceFile *ast.SourceFile) {
	tx.rawReferencedFiles = append(tx.rawReferencedFiles, core.Map(sourceFile.ReferencedFiles, func(ref *ast.FileReference) ReferencedFilePair {
		return ReferencedFilePair{file: sourceFile, ref: ref}
	})...)
	tx.rawTypeReferenceDirectives = append(tx.rawTypeReferenceDirectives, sourceFile.TypeReferenceDirectives...)
	tx.rawLibReferenceDirectives = append(tx.rawLibReferenceDirectives, sourceFile.LibReferenceDirectives...)
}
func nodeOrSyntaxListChildren(node ast.Handle) []ast.Handle {
	if ast.IsSyntaxList(node) {
		return node.Store().ListSlice(node.SyntaxListChildren()).Slice()
	}
	return []ast.Handle{node}
}
func flattenSyntaxLists(nodes []ast.Handle) []ast.Handle {
	return core.FlatMap(nodes, nodeOrSyntaxListChildren)
}
func (tx *DeclarationTransformer) appendCjsExports(combinedStatements ast.ListRef) ast.ListRef {
	result := []ast.Handle{}
	if !tx.cjsExportAssignment.IsNil() {
		result = append(result, tx.cjsExportAssignment)
	}
	result = append(result, tx.cjsExportMembers...)
	store := tx.EmitContext().StoreFile().ParseStore()
	result = append(result, store.ListSlice(combinedStatements).Slice()...)
	statementNodes := flattenSyntaxLists(result)
	if len(statementNodes) != store.ListLen(combinedStatements) {
		combinedStatements = tx.Factory().List(store.ListLoc(combinedStatements), statementNodes...)
	}
	return combinedStatements
}
func (tx *DeclarationTransformer) transformSourceFile(node *ast.SourceFile) ast.Handle {
	tx.cjsExportAssignment = ast.Handle{}
	tx.cjsExportAssignmentName = ast.Handle{}
	tx.cjsExportMembers = nil
	defer func() {
		tx.cjsExportAssignment = ast.Handle{}
		tx.cjsExportAssignmentName = ast.Handle{}
		tx.cjsExportMembers = nil
	}()
	root := node.ParseRoot()
	tx.cjsExportAssignmentVisitor.VisitNode(root)
	tx.expressionVisitor.VisitNode(root)
	store := tx.EmitContext().StoreFile().ParseStore()
	statements := tx.Visitor().VisitTopLevelStatements(root.SourceFileStatements())
	combinedStatements := tx.transformAndReplaceLatePaintedStatements(statements)
	combinedStatements = tx.appendCjsExports(combinedStatements)
	if ast.IsExternalOrCommonJSModule(node) {
		if ast.IsInJSFile(root) {
			if exportEquals := node.Symbol.Exports[ast.InternalSymbolNameExportEquals]; exportEquals != nil && len(exportEquals.Declarations) > 1 {
				for _, n := range ast.DeclarationNodes(exportEquals).All() {
					tx.state.addDiagnostic(createDiagnosticForNode(n, diagnostics.Multiple_module_exports_assignments_cannot_be_serialized_for_declaration_emit))
				}
			}
		}
		if !tx.resultHasExternalModuleIndicator || (tx.needsScopeFixMarker && !tx.resultHasScopeMarker) {
			marker := createEmptyExports(tx.Factory().Factory)
			newList := append(store.ListSlice(combinedStatements).Slice(), marker)
			combinedStatements = tx.EmitContext().StoreFactory().List(store.ListLoc(combinedStatements), newList...)
		}
	}
	outputFilePath := tspath.GetDirectoryPath(tspath.NormalizeSlashes(tx.declarationFilePath))
	result := tx.Factory().UpdateSourceFile(root, combinedStatements, root.EndOfFileToken())
	node.LibReferenceDirectives = tx.getLibReferences()
	node.TypeReferenceDirectives = tx.getTypeReferences()
	node.IsDeclarationFile = true
	node.ReferencedFiles = tx.getReferencedFiles(outputFilePath)
	return result
}
func createEmptyExports(factory ast.HandleFactory) ast.Handle {
	return factory.NewExportDeclaration(0, false, factory.NewNamedExports(factory.NewList([]ast.Handle{})), ast.Handle{}, ast.Handle{})
}
func (tx *DeclarationTransformer) transformAndReplaceLatePaintedStatements(statements ast.ListRef) ast.ListRef {
	for true {
		if len(tx.state.lateMarkedStatements) == 0 {
			break
		}
		next := tx.state.lateMarkedStatements[0]
		tx.state.lateMarkedStatements = tx.state.lateMarkedStatements[1:]
		saveNeedsDeclare := tx.needsDeclare
		tx.needsDeclare = !next.Parent().IsNil() && ast.IsSourceFile(next.Parent())
		result := tx.transformTopLevelDeclaration(next)
		tx.needsDeclare = saveNeedsDeclare
		original := tx.EmitContext().MostOriginal(next)
		id := tx.EmitContext().NodeIdentity(original)
		tx.lateStatementReplacementMap[id] = result
	}
	results := make([]ast.Handle, 0, tx.EmitContext().StoreFile().ParseStore().ListLen(statements))
	for _, statement := range tx.EmitContext().StoreFile().ParseStore().ListSlice(statements).All() {
		if !ast.IsLateVisibilityPaintedStatement(statement) {
			results = append(results, statement)
			continue
		}
		original := tx.EmitContext().MostOriginal(statement)
		id := tx.EmitContext().NodeIdentity(original)
		replacement, ok := tx.lateStatementReplacementMap[id]
		if !ok {
			results = append(results, statement)
			continue
		}
		if replacement.IsNil() {
			continue
		}
		if replacement.Kind == ast.KindSyntaxList {
			if !tx.needsScopeFixMarker || !tx.resultHasExternalModuleIndicator {
				for _, elem := range replacement.Store().ListSlice(replacement.SyntaxListChildren()).All() {
					if needsScopeMarker(elem) {
						tx.needsScopeFixMarker = true
					}
					if ast.IsSourceFile(statement.Parent()) && ast.IsExternalModuleIndicator(elem) {
						tx.resultHasExternalModuleIndicator = true
					}
				}
			}
			results = append(results, replacement.Store().ListSlice(replacement.SyntaxListChildren()).Slice()...)
		} else {
			if needsScopeMarker(replacement) {
				tx.needsScopeFixMarker = true
			}
			if ast.IsSourceFile(statement.Parent()) && ast.IsExternalModuleIndicator(replacement) {
				tx.resultHasExternalModuleIndicator = true
			}
			results = append(results, replacement)
		}
	}
	return tx.Factory().List(tx.EmitContext().StoreFile().ParseStore().ListLoc(statements), results...)
}
func (tx *DeclarationTransformer) getReferencedFiles(outputFilePath string) (results []*ast.FileReference) {
	for _, pair := range tx.rawReferencedFiles {
		sourceFile := pair.file
		ref := pair.ref
		if !ref.Preserve {
			continue
		}
		file := tx.host.GetSourceFileFromReference(sourceFile, ref)
		if file == nil {
			continue
		}
		var declFileName string
		if file.IsDeclarationFile {
			declFileName = file.FileName()
		} else {
			paths := tx.host.GetOutputPathsFor(file, true)
			declFileName = paths.DeclarationFilePath()
			if len(declFileName) == 0 {
				declFileName = paths.JsFilePath()
			}
			if len(declFileName) == 0 {
				declFileName = file.FileName()
			}
		}
		if len(declFileName) == 0 {
			continue
		}
		fileName := tspath.GetRelativePathToDirectoryOrUrl(outputFilePath, declFileName, false, tspath.ComparePathsOptions{CurrentDirectory: tx.host.GetCurrentDirectory(), UseCaseSensitiveFileNames: tx.host.UseCaseSensitiveFileNames()})
		results = append(results, &ast.FileReference{TextRange: core.NewTextRange(-1, -1), FileName: fileName, ResolutionMode: ref.ResolutionMode, Preserve: ref.Preserve})
	}
	return results
}
func (tx *DeclarationTransformer) getLibReferences() (result []*ast.FileReference) {
	for _, ref := range tx.rawLibReferenceDirectives {
		if !ref.Preserve {
			continue
		}
		result = append(result, &ast.FileReference{TextRange: core.NewTextRange(-1, -1), FileName: ref.FileName, ResolutionMode: ref.ResolutionMode, Preserve: ref.Preserve})
	}
	return result
}
func (tx *DeclarationTransformer) getTypeReferences() (result []*ast.FileReference) {
	for _, ref := range tx.rawTypeReferenceDirectives {
		if !ref.Preserve {
			continue
		}
		result = append(result, &ast.FileReference{TextRange: core.NewTextRange(-1, -1), FileName: ref.FileName, ResolutionMode: ref.ResolutionMode, Preserve: ref.Preserve})
	}
	return result
}
func (tx *DeclarationTransformer) setupDiagnosticContext(input ast.Handle) (bool, func()) {
	canProduceDiagnostic := canProduceDiagnostics(input)
	oldWithinObjectLiteralType := tx.suppressNewDiagnosticContexts
	shouldEnterSuppressNewDiagnosticsContextContext := (input.Kind == ast.KindTypeLiteral || input.Kind == ast.KindMappedType) && !(input.Parent().Kind == ast.KindTypeAliasDeclaration || input.Parent().Kind == ast.KindJSTypeAliasDeclaration)
	oldDiag := tx.state.getSymbolAccessibilityDiagnostic
	if canProduceDiagnostic && !tx.suppressNewDiagnosticContexts {
		tx.state.getSymbolAccessibilityDiagnostic = createGetSymbolAccessibilityDiagnosticForNode(input)
	}
	oldName := tx.state.errorNameNode
	if shouldEnterSuppressNewDiagnosticsContextContext {
		tx.suppressNewDiagnosticContexts = true
	}
	return canProduceDiagnostic, func() {
		tx.state.getSymbolAccessibilityDiagnostic = oldDiag
		tx.state.errorNameNode = oldName
		tx.suppressNewDiagnosticContexts = oldWithinObjectLiteralType
	}
}
func (tx *DeclarationTransformer) visitDeclarationSubtree(input ast.Handle) ast.Handle {
	if tx.shouldStripInternal(input) {
		return ast.Handle{}
	}
	if ast.IsDeclaration(input) {
		if isDeclarationAndNotVisible(tx.EmitContext(), tx.resolver, input) {
			return ast.Handle{}
		}
		if ast.HasDynamicName(input) {
			if tx.state.isolatedDeclarations {
				if !tx.resolver.IsDefinitelyReferenceToGlobalSymbolObject(input.Name().Expression()) {
					if ast.IsClassDeclaration(input.Parent()) || ast.IsObjectLiteralExpression(input.Parent()) {
						tx.state.addDiagnostic(createDiagnosticForNode(input, diagnostics.Computed_property_names_on_class_or_object_literals_cannot_be_inferred_with_isolatedDeclarations))
						return ast.Handle{}
					} else if (ast.IsInterfaceDeclaration(input.Parent()) || ast.IsTypeLiteralNode(input.Parent())) && !ast.IsEntityNameExpression(input.Name().Expression()) {
						tx.state.addDiagnostic(createDiagnosticForNode(input, diagnostics.Computed_properties_must_be_number_or_string_literals_variables_or_dotted_expressions_with_isolatedDeclarations))
						return ast.Handle{}
					}
				}
			} else if !tx.resolver.IsLateBound(tx.EmitContext().ParseNode(input)) || !ast.IsEntityNameExpression(input.Name().Expression()) {
				return ast.Handle{}
			}
		}
	}
	if ast.IsFunctionLike(input) && tx.resolver.IsImplementationOfOverload(input) {
		return ast.Handle{}
	}
	if input.Kind == ast.KindSemicolonClassElement {
		return ast.Handle{}
	}
	if ast.IsHeritageClause(input) {
		types := input.HeritageClauseTypes()
		nTypes := input.Store().ListLen(types)
		if nTypes == 0 || (nTypes == 1 && ast.NodeIsMissing(input.Store().ListAt(types, 0))) {
			return ast.Handle{}
		}
	}
	previousEnclosingDeclaration := tx.enclosingDeclaration
	if isEnclosingDeclaration(input) {
		tx.enclosingDeclaration = input
	}
	canProduceDiagnostic, cleanupDiagnosticContext := tx.setupDiagnosticContext(input)
	defer cleanupDiagnosticContext()
	var result ast.Handle
	switch input.Kind {
	case ast.KindMappedType:
		result = tx.transformMappedTypeNode(input)
	case ast.KindHeritageClause:
		result = tx.transformHeritageClause(input)
	case ast.KindMethodSignature:
		result = tx.transformMethodSignatureDeclaration(input)
	case ast.KindMethodDeclaration:
		result = tx.transformMethodDeclaration(input)
	case ast.KindConstructSignature:
		result = tx.transformConstructSignatureDeclaration(input)
	case ast.KindConstructor:
		result = tx.transformConstructorDeclaration(input)
	case ast.KindGetAccessor:
		result = tx.transformGetAccesorDeclaration(input)
	case ast.KindSetAccessor:
		result = tx.transformSetAccessorDeclaration(input)
	case ast.KindPropertyDeclaration:
		result = tx.transformPropertyDeclaration(input)
	case ast.KindPropertySignature:
		result = tx.transformPropertySignatureDeclaration(input)
	case ast.KindCallSignature:
		result = tx.transformCallSignatureDeclaration(input)
	case ast.KindIndexSignature:
		result = tx.transformIndexSignatureDeclaration(input)
	case ast.KindVariableDeclaration:
		result = tx.transformVariableDeclaration(input)
	case ast.KindTypeParameter:
		result = tx.transformTypeParameterDeclaration(input)
	case ast.KindExpressionWithTypeArguments:
		result = tx.transformExpressionWithTypeArguments(input)
	case ast.KindTypeReference:
		result = tx.transformTypeReference(input)
	case ast.KindConditionalType:
		result = tx.transformConditionalTypeNode(input)
	case ast.KindFunctionType:
		result = tx.transformFunctionTypeNode(input)
	case ast.KindConstructorType:
		result = tx.transformConstructorTypeNode(input)
	case ast.KindImportType:
		result = tx.transformImportTypeNode(input)
	case ast.KindTypeQuery:
		tx.checkEntityNameVisibility(input.TypeQueryNodeExprName(), tx.enclosingDeclaration)
		result = tx.Visitor().VisitEachChild(input)
	case ast.KindQualifiedName:
		if input.QualifiedNameRight().Kind == ast.KindPrivateIdentifier {
			tx.state.addDiagnostic(createDiagnosticForNode(input, diagnostics.Declaration_emit_elides_private_members_but_0_refers_to_a_private_member_Write_an_explicit_type_here, input.QualifiedNameRight().Text()))
		}
		result = tx.Visitor().VisitEachChild(input)
	case ast.KindTupleType:
		result = tx.Visitor().VisitEachChild(input)
		if !result.IsNil() {
			if transformers.IsOriginalNodeSingleLine(tx.EmitContext(), input) {
				tx.EmitContext().AddEmitFlags(result, printer.EFSingleLine)
			}
		}
	case ast.KindJSDocTypeExpression:
		result = tx.transformJSDocTypeExpression(input)
	case ast.KindJSDocTypeLiteral:
		result = tx.transformJSDocTypeLiteral(input)
	case ast.KindJSDocPropertyTag:
		result = tx.transformJSDocPropertyTag(input)
	case ast.KindJSDocAllType:
		result = tx.transformJSDocAllType(input)
	case ast.KindJSDocNullableType:
		result = tx.transformJSDocNullableType(input)
	case ast.KindJSDocNonNullableType:
		result = tx.transformJSDocNonNullableType(input)
	case ast.KindJSDocOptionalType:
		result = tx.transformJSDocOptionalType(input)
	case ast.KindJSDocVariadicType:
		result = tx.transformJSDocVariadicType(input)
	default:
		result = tx.Visitor().VisitEachChild(input)
	}
	if !result.IsNil() && canProduceDiagnostic && ast.HasDynamicName(input) {
		tx.checkName(input)
	}
	tx.enclosingDeclaration = previousEnclosingDeclaration
	return result
}
func (tx *DeclarationTransformer) checkName(node ast.Handle) {
	oldDiag := tx.state.getSymbolAccessibilityDiagnostic
	if !tx.suppressNewDiagnosticContexts {
		tx.state.getSymbolAccessibilityDiagnostic = createGetSymbolAccessibilityDiagnosticForNodeName(node)
	}
	tx.state.errorNameNode = node.Name()
	debug.Assert(ast.HasDynamicName(node))
	entityName := node.Name().Expression()
	tx.checkEntityNameVisibility(entityName, tx.enclosingDeclaration)
	if !tx.suppressNewDiagnosticContexts {
		tx.state.getSymbolAccessibilityDiagnostic = oldDiag
	}
	tx.state.errorNameNode = ast.Handle{}
}
func (tx *DeclarationTransformer) transformMappedTypeNode(input ast.Handle) ast.Handle {
	var typeNode ast.Handle
	if input.Type().IsNil() {
		typeNode = tx.Factory().NewKeywordTypeNode(ast.KindAnyKeyword)
	} else {
		typeNode = tx.Visitor().Visit(input.Type())
	}
	return tx.Factory().UpdateMappedTypeNode(input, input.MappedTypeNodeReadonlyToken(), tx.Visitor().Visit(input.MappedTypeNodeTypeParameter()), tx.Visitor().Visit(input.MappedTypeNodeNameType()), input.QuestionToken(), typeNode, 0)
}
func (tx *DeclarationTransformer) transformHeritageClause(clause ast.Handle) ast.Handle {
	retainedClauses := core.Filter(clause.Types(), func(t ast.Handle) bool {
		name := ast.GetHeritageClauseElementName(t)
		return ast.IsEntityName(name) || ast.IsEntityNameExpression(name) || (clause.HeritageClauseToken() == ast.KindExtendsKeyword && ast.IsExpressionWithTypeArguments(t) && t.Expression().Kind == ast.KindNullKeyword)
	})
	if len(retainedClauses) == 0 {
		return ast.Handle{}
	}
	if len(retainedClauses) == len(clause.Types()) {
		return tx.Visitor().VisitEachChild(clause)
	}
	return tx.Factory().UpdateHeritageClause(clause, clause.HeritageClauseToken(), tx.Visitor().VisitNodes(tx.Factory().NewList(retainedClauses)))
}
func (tx *DeclarationTransformer) transformImportTypeNode(input ast.Handle) ast.Handle {
	if !ast.IsLiteralImportTypeNode(input) {
		return input
	}
	return tx.Factory().UpdateImportTypeNode(input, input.IsTypeOf(), tx.Factory().UpdateLiteralTypeNode(input.Argument(), tx.rewriteModuleSpecifier(input, input.Argument().LiteralTypeNodeLiteral())), input.Attributes(), input.Qualifier(), tx.Visitor().VisitNodes(input.TypeArgumentList()))
}
func (tx *DeclarationTransformer) transformConstructorTypeNode(input ast.Handle) ast.Handle {
	return tx.Factory().UpdateConstructorTypeNode(input, tx.ensureModifiers(input), tx.Visitor().VisitNodes(input.TypeParameterList()), tx.updateParamList(input, input.ParameterList()), tx.Visitor().Visit(input.Type()))
}
func (tx *DeclarationTransformer) transformFunctionTypeNode(input ast.Handle) ast.Handle {
	return tx.Factory().UpdateFunctionTypeNode(input, tx.Visitor().VisitNodes(input.TypeParameterList()), tx.updateParamList(input, input.ParameterList()), tx.Visitor().Visit(input.Type()))
}
func (tx *DeclarationTransformer) transformConditionalTypeNode(input ast.Handle) ast.Handle {
	checkType := tx.Visitor().Visit(input.ConditionalTypeNodeCheckType())
	extendsType := tx.Visitor().Visit(input.ConditionalTypeNodeExtendsType())
	oldEnclosingDecl := tx.enclosingDeclaration
	tx.enclosingDeclaration = input.ConditionalTypeNodeTrueType()
	trueType := tx.Visitor().Visit(input.ConditionalTypeNodeTrueType())
	tx.enclosingDeclaration = oldEnclosingDecl
	falseType := tx.Visitor().Visit(input.ConditionalTypeNodeFalseType())
	return tx.Factory().UpdateConditionalTypeNode(input, checkType, extendsType, trueType, falseType)
}
func (tx *DeclarationTransformer) transformTypeReference(input ast.Handle) ast.Handle {
	tx.checkEntityNameVisibility(input.TypeName(), tx.enclosingDeclaration)
	return tx.Visitor().VisitEachChild(input)
}
func (tx *DeclarationTransformer) transformExpressionWithTypeArguments(input ast.Handle) ast.Handle {
	if ast.IsEntityName(input.Expression()) || ast.IsEntityNameExpression(input.Expression()) {
		tx.checkEntityNameVisibility(input.Expression(), tx.enclosingDeclaration)
	}
	return tx.Visitor().VisitEachChild(input)
}
func (tx *DeclarationTransformer) transformTypeParameterDeclaration(input ast.Handle) ast.Handle {
	if isPrivateMethodTypeParameter(tx.host, input) && (!input.DefaultType().IsNil() || !input.Constraint().IsNil()) {
		return tx.Factory().UpdateTypeParameterDeclaration(input, input.Modifiers(), input.Name(), ast.Handle{}, input.Expression(), ast.Handle{})
	}
	return tx.Visitor().VisitEachChild(input)
}
func (tx *DeclarationTransformer) transformVariableDeclaration(input ast.Handle) ast.Handle {
	if !tx.state.currentSourceFile.CommonJSModuleIndicator.IsNil() && ast.IsVariableDeclarationInitializedToRequire(input) {
		return tx.transformCjsRequireVariableDeclaration(input)
	}
	if ast.IsBindingPattern(input.Name()) && hasAnyBindingInitializers(input.Name()) {
		return tx.recreateBindingPattern(input.Name())
	}
	tx.suppressNewDiagnosticContexts = true
	return tx.Factory().UpdateVariableDeclaration(input, tx.bindingNameVisitor.VisitNode(input.Name()), ast.Handle{}, tx.ensureType(input, false), tx.ensureNoInitializer(input))
}
func hasAnyBindingInitializers(bindingPattern ast.Handle) bool {
	for _, elem := range bindingPattern.Elements() {
		if !ast.IsBindingElement(elem) {
			continue
		}
		e := elem
		if !e.Initializer().IsNil() {
			return true
		}
		if !e.Name().IsNil() && ast.IsBindingPattern(e.Name()) && hasAnyBindingInitializers(e.Name()) {
			return true
		}
	}
	return false
}
func (tx *DeclarationTransformer) transformCjsRequireVariableDeclaration(input ast.Handle) ast.Handle {
	args := input.Initializer().CallExpressionArguments()
	specifier := tx.rewriteModuleSpecifier(input, input.Initializer().Store().ListAt(args, 0))
	if ast.IsIdentifier(input.Name()) {
		return tx.Factory().NewImportEqualsDeclaration(0, false, input.Name(), tx.Factory().NewExternalModuleReference(specifier))
	} else if ast.IsArrayBindingPattern(input.Name()) {
		return ast.Handle{}
	} else {
		b := input.Name()
		var importSpecifiers []ast.Handle
		for _, elem := range b.Elements() {
			if !ast.IsIdentifier(elem.Name()) {
				continue
			}
			importSpecifiers = append(importSpecifiers, tx.Factory().NewImportSpecifier(false, elem.PropertyName(), elem.Name()))
		}
		return tx.Factory().NewImportDeclaration(0, tx.Factory().NewImportClause(ast.KindUnknown, ast.Handle{}, tx.Factory().NewNamedImports(tx.Factory().NewList(importSpecifiers))), specifier, ast.Handle{})
	}
}
func (tx *DeclarationTransformer) recreateBindingPattern(input ast.Handle) ast.Handle {
	var results []ast.Handle
	for _, elem := range input.Elements() {
		result := tx.recreateBindingElement(elem)
		if result.IsNil() {
			continue
		}
		if result.Kind == ast.KindSyntaxList {
			results = append(results, result.Store().ListSlice(result.SyntaxListChildren()).Slice()...)
		} else {
			results = append(results, result)
		}
	}
	if len(results) == 0 {
		return ast.Handle{}
	}
	if len(results) == 1 {
		return results[0]
	}
	return tx.Factory().NewSyntaxList(tx.Factory().NewList(results))
}
func (tx *DeclarationTransformer) recreateBindingElement(e ast.Handle) ast.Handle {
	if e.Name().IsNil() {
		return ast.Handle{}
	}
	if !getBindingNameVisible(tx.resolver, e) {
		return ast.Handle{}
	}
	if ast.IsBindingPattern(e.Name()) {
		return tx.recreateBindingPattern(e.Name())
	}
	return tx.Factory().NewVariableDeclaration(e.Name(), ast.Handle{}, tx.ensureType(e, false), ast.Handle{})
}
func (tx *DeclarationTransformer) transformIndexSignatureDeclaration(input ast.Handle) ast.Handle {
	t := tx.Visitor().Visit(input.Type())
	if t.IsNil() {
		t = tx.Factory().NewKeywordTypeNode(ast.KindAnyKeyword)
	}
	return tx.Factory().UpdateIndexSignatureDeclaration(input, tx.ensureModifiers(input), tx.updateParamList(input, input.ParameterList()), t)
}
func (tx *DeclarationTransformer) transformCallSignatureDeclaration(input ast.Handle) ast.Handle {
	return tx.Factory().UpdateCallSignatureDeclaration(input, tx.ensureTypeParams(input, input.TypeParameterList()), tx.updateParamList(input, input.ParameterList()), tx.ensureType(input, false))
}
func (tx *DeclarationTransformer) transformPropertySignatureDeclaration(input ast.Handle) ast.Handle {
	if ast.IsPrivateIdentifier(input.Name()) {
		return ast.Handle{}
	}
	result := tx.Factory().UpdatePropertySignatureDeclaration(input, tx.ensureModifiers(input), input.Name(), input.PostfixToken(), tx.ensureType(input, false), tx.ensureNoInitializer(input))
	tx.preservePartialJsDoc(result, input)
	return result
}
func (tx *DeclarationTransformer) transformPropertyDeclaration(input ast.Handle) ast.Handle {
	if ast.IsPrivateIdentifier(input.Name()) {
		return ast.Handle{}
	}
	postfixToken := input.PostfixToken()
	if !postfixToken.IsNil() && postfixToken.Kind == ast.KindExclamationToken {
		postfixToken = ast.Handle{}
	}
	return tx.Factory().UpdatePropertyDeclaration(input, tx.ensureModifiers(input), input.Name(), postfixToken, tx.ensureType(input, false), tx.ensureNoInitializer(input))
}
func (tx *DeclarationTransformer) transformSetAccessorDeclaration(input ast.Handle) ast.Handle {
	if ast.IsPrivateIdentifier(input.Name()) {
		return ast.Handle{}
	}
	return tx.Factory().UpdateSetAccessorDeclaration(input, tx.ensureModifiers(input), input.Name(), 0, tx.updateAccessorParamList(input, tx.host.GetEffectiveDeclarationFlags(tx.EmitContext().ParseNode(input), ast.ModifierFlagsPrivate) != 0), ast.Handle{}, ast.Handle{}, ast.Handle{})
}
func (tx *DeclarationTransformer) transformGetAccesorDeclaration(input ast.Handle) ast.Handle {
	if ast.IsPrivateIdentifier(input.Name()) {
		return ast.Handle{}
	}
	return tx.Factory().UpdateGetAccessorDeclaration(input, tx.ensureModifiers(input), input.Name(), 0, tx.updateAccessorParamList(input, tx.host.GetEffectiveDeclarationFlags(tx.EmitContext().ParseNode(input), ast.ModifierFlagsPrivate) != 0), tx.ensureType(input, false), ast.Handle{}, ast.Handle{})
}
func (tx *DeclarationTransformer) updateAccessorParamList(input ast.Handle, isPrivate bool) ast.ListRef {
	var newParams []ast.Handle
	if !isPrivate {
		thisParam := ast.GetThisParameter(input)
		if !thisParam.IsNil() {
			newParams = append(newParams, tx.ensureParameter(thisParam))
		}
	}
	if ast.IsSetAccessorDeclaration(input) {
		var valueParam ast.Handle
		if !isPrivate {
			params := input.SetAccessorDeclarationParameters()
			nParams := input.Store().ListLen(params)
			if len(newParams) == 1 && nParams >= 2 {
				valueParam = tx.ensureParameter(input.Store().ListAt(params, 1))
			} else if len(newParams) == 0 && nParams >= 1 {
				valueParam = tx.ensureParameter(input.Store().ListAt(params, 0))
			}
		}
		if valueParam.IsNil() {
			var t ast.Handle
			if !isPrivate {
				t = tx.Factory().NewKeywordTypeNode(ast.KindAnyKeyword)
			}
			valueParam = tx.Factory().NewParameterDeclaration(0, ast.Handle{}, tx.Factory().NewIdentifier("value"), ast.Handle{}, t, ast.Handle{})
		}
		newParams = append(newParams, valueParam)
	}
	return tx.Factory().NewList(newParams)
}
func (tx *DeclarationTransformer) transformConstructorDeclaration(input ast.Handle) ast.Handle {
	return tx.Factory().UpdateConstructorDeclaration(input, tx.ensureModifiers(input), 0, tx.updateParamList(input, input.ParameterList()), ast.Handle{}, ast.Handle{}, ast.Handle{})
}
func (tx *DeclarationTransformer) transformConstructSignatureDeclaration(input ast.Handle) ast.Handle {
	return tx.Factory().UpdateConstructSignatureDeclaration(input, tx.ensureTypeParams(input, input.TypeParameterList()), tx.updateParamList(input, input.ParameterList()), tx.ensureType(input, false))
}
func (tx *DeclarationTransformer) omitPrivateMethodType(input ast.Handle) ast.Handle {
	if input.Symbol() != nil && len(input.Symbol().Declarations) > 0 && ast.NodeOf(input.Symbol().Declarations[0]) != input {
		return ast.Handle{}
	}
	result := tx.Factory().NewPropertyDeclaration(tx.ensureModifiers(input), input.Name(), ast.Handle{}, ast.Handle{}, ast.Handle{})
	tx.preserveJsDoc(result, input)
	return result
}
func (tx *DeclarationTransformer) transformMethodSignatureDeclaration(input ast.Handle) ast.Handle {
	if tx.host.GetEffectiveDeclarationFlags(tx.EmitContext().ParseNode(input), ast.ModifierFlagsPrivate) != 0 {
		return tx.omitPrivateMethodType(input)
	} else if ast.IsPrivateIdentifier(input.Name()) {
		return ast.Handle{}
	} else {
		return tx.Factory().UpdateMethodSignatureDeclaration(input, tx.ensureModifiers(input), input.Name(), input.PostfixToken(), tx.ensureTypeParams(input, input.TypeParameterList()), tx.updateParamList(input, input.ParameterList()), tx.ensureType(input, false))
	}
}
func (tx *DeclarationTransformer) transformMethodDeclaration(input ast.Handle) ast.Handle {
	if tx.host.GetEffectiveDeclarationFlags(tx.EmitContext().ParseNode(input), ast.ModifierFlagsPrivate) != 0 {
		return tx.omitPrivateMethodType(input)
	} else if ast.IsPrivateIdentifier(input.Name()) {
		return ast.Handle{}
	} else {
		return tx.Factory().UpdateMethodDeclaration(input, tx.ensureModifiers(input), ast.Handle{}, input.Name(), input.PostfixToken(), tx.ensureTypeParams(input, input.TypeParameterList()), tx.updateParamList(input, input.ParameterList()), tx.ensureType(input, false), ast.Handle{}, ast.Handle{})
	}
}
func (tx *DeclarationTransformer) visitDeclarationStatements(input ast.Handle) ast.Handle {
	if tx.shouldStripInternal(input) {
		return ast.Handle{}
	}
	switch input.Kind {
	case ast.KindExportDeclaration:
		if ast.IsSourceFile(input.Parent()) {
			tx.resultHasExternalModuleIndicator = true
		}
		tx.resultHasScopeMarker = true
		return tx.Factory().UpdateExportDeclaration(input, input.Modifiers(), input.IsTypeOnly(), input.ExportDeclarationExportClause(), tx.rewriteModuleSpecifier(input, input.ModuleSpecifier()), tx.tryGetResolutionModeOverride(input.ExportDeclarationAttributes()))
	case ast.KindExportAssignment:
		return tx.transformExportAssignment(input, input, input.Expression(), input.ExportAssignmentIsExportEquals())
	default:
		id := tx.EmitContext().NodeIdentity(tx.EmitContext().MostOriginal(input))
		if tx.lateStatementReplacementMap[id].IsNil() {
			tx.lateStatementReplacementMap[id] = tx.transformTopLevelDeclaration(input)
		}
		return input
	}
}
func (tx *DeclarationTransformer) tryGetNameOfAssignedExpression(unwrapped ast.Handle) ast.Handle {
	var nameNode ast.Handle
	var nameText string
	if !ast.IsPropertyAccessExpression(unwrapped) && !unwrapped.Name().IsNil() {
		nameText = unwrapped.Name().Text()
	} else if ast.IsIdentifier(unwrapped) {
		nameText = unwrapped.Text()
	}
	if nameText != "" && nameText != "default" {
		if tx.resolver.IsNameResolvable(tx.enclosingDeclaration, nameText) {
			nameNode = tx.Factory().NewUniqueNameEx(nameText, printer.AutoGenerateOptions{Flags: printer.GeneratedIdentifierFlagsOptimistic})
		} else {
			nameNode = tx.Factory().NewIdentifier(nameText)
		}
	}
	return nameNode
}
func (tx *DeclarationTransformer) getNameOfExportedAssignedExpression(unwrapped ast.Handle, isExportEquals bool) ast.Handle {
	nameNode := tx.tryGetNameOfAssignedExpression(unwrapped)
	if nameNode.IsNil() {
		if isExportEquals && ast.IsSourceFileJS(tx.state.currentSourceFile) {
			nameNode = tx.Factory().NewUniqueNameEx("_exports", printer.AutoGenerateOptions{Flags: printer.GeneratedIdentifierFlagsOptimistic})
		} else {
			nameNode = tx.Factory().NewUniqueNameEx("_default", printer.AutoGenerateOptions{Flags: printer.GeneratedIdentifierFlagsOptimistic})
		}
	}
	tx.cjsExportAssignmentName = nameNode
	return nameNode
}
func (tx *DeclarationTransformer) transformExportAssignment(input ast.Handle, assignment ast.Handle, expression ast.Handle, isExportEquals bool) ast.Handle {
	if ast.IsSourceFile(input.Parent()) {
		tx.resultHasExternalModuleIndicator = true
	}
	tx.resultHasScopeMarker = true
	if ast.IsIdentifier(expression) && (ast.IsSourceFile(input.Parent()) || ast.IsModuleBlock(input.Parent())) {
		exportAssignment := tx.Factory().NewExportAssignment(0, isExportEquals, ast.Handle{}, expression)
		tx.preserveJsDoc(exportAssignment, input)
		return exportAssignment
	}
	unwrapped := ast.SkipOuterExpressions(expression, ast.OEKExpressionTypePassthrough)
	newId := tx.getNameOfExportedAssignedExpression(unwrapped, isExportEquals)
	if ast.IsClassExpression(unwrapped) {
		var mods []ast.Handle
		if tx.needsDeclare {
			mods = append(mods, tx.Factory().NewModifier(ast.KindDeclareKeyword))
		}
		classDecl := tx.transformClassExpressionToDeclaration(unwrapped, newId, tx.Factory().NewModifierList(mods))
		tx.preserveJsDoc(classDecl, input)
		exportAssignment := tx.Factory().NewExportAssignment(0, isExportEquals, ast.Handle{}, newId)
		tx.removeAllComments(exportAssignment)
		return tx.Factory().NewSyntaxList(tx.Factory().NewList([]ast.Handle{exportAssignment, classDecl}))
	} else if ast.IsFunctionLike(unwrapped) {
		var mods []ast.Handle
		if tx.needsDeclare {
			mods = append(mods, tx.Factory().NewModifier(ast.KindDeclareKeyword))
		}
		fullSignatureType := assignment.Type()
		funcDecl := tx.transformFunctionLikeToDeclaration(unwrapped, newId, tx.Factory().NewModifierList(mods), fullSignatureType)
		tx.preserveJsDoc(funcDecl, input)
		exportAssignment := tx.Factory().NewExportAssignment(0, isExportEquals, ast.Handle{}, newId)
		tx.removeAllComments(exportAssignment)
		return tx.Factory().NewSyntaxList(tx.Factory().NewList([]ast.Handle{exportAssignment, funcDecl}))
	}
	tx.state.getSymbolAccessibilityDiagnostic = func(_ printer.SymbolAccessibilityResult) *SymbolAccessibilityDiagnostic {
		return &SymbolAccessibilityDiagnostic{diagnosticMessage: diagnostics.Default_export_of_the_module_has_or_is_using_private_name_0, errorNode: input}
	}
	tx.cjsExportAssignmentName = newId
	tx.tracker.PushErrorFallbackNode(assignment)
	var type_, initializer ast.Handle
	if ast.IsPrimitiveLiteralValue(unwrapParenthesizedExpression(expression), true) {
		initializer = tx.resolver.CreateLiteralConstValue(tx.EmitContext(), tx.EmitContext().ParseNode(assignment), tx.tracker)
	}
	if initializer.IsNil() {
		type_ = tx.ensureType(assignment, false)
	}
	varDecl := tx.Factory().NewVariableDeclaration(newId, ast.Handle{}, type_, initializer)
	tx.tracker.PopErrorFallbackNode()
	var modList ast.ListRef
	if tx.needsDeclare {
		modList = tx.Factory().NewModifierList([]ast.Handle{tx.Factory().NewModifier(ast.KindDeclareKeyword)})
	} else {
		modList = tx.Factory().NewModifierList([]ast.Handle{})
	}
	statement := tx.Factory().NewVariableStatement(modList, tx.Factory().NewVariableDeclarationList(tx.Factory().NewList([]ast.Handle{varDecl}), ast.NodeFlagsConst))
	exportAssignment := tx.Factory().NewExportAssignment(0, isExportEquals, ast.Handle{}, newId)
	tx.preserveJsDoc(statement, input)
	return tx.Factory().NewSyntaxList(tx.Factory().NewList([]ast.Handle{statement, exportAssignment}))
}
func (tx *DeclarationTransformer) transformFunctionLikeToDeclaration(unwrapped ast.Handle, funcName ast.Handle, mods ast.ListRef, fullSignatureType ast.Handle) ast.Handle {
	d := unwrapped
	sig := d.FullSignature()
	if sig.IsNil() {
		sig = fullSignatureType
	}
	if sig.IsNil() {
		return tx.Factory().NewFunctionDeclaration(mods, ast.Handle{}, funcName, tx.ensureTypeParams(unwrapped, d.TypeParameterList()), tx.updateParamList(unwrapped, d.ParameterList()), tx.ensureType(unwrapped, false), tx.Visitor().VisitNode(sig), ast.Handle{})
	} else {
		return tx.Factory().NewVariableStatement(mods, tx.Factory().NewVariableDeclarationList(tx.Factory().NewList([]ast.Handle{tx.Factory().NewVariableDeclaration(funcName, ast.Handle{}, tx.Visitor().VisitNode(sig), ast.Handle{})}), ast.NodeFlagsConst))
	}
}
func (tx *DeclarationTransformer) transformBinaryExpressionToExportDeclaration(input ast.Handle, name ast.Handle) ast.Handle {
	propertyName := input.BinaryExpressionRight()
	tx.tracker.handleSymbolAccessibilityError(tx.resolver.IsEntityNameVisible(propertyName, tx.enclosingDeclaration))
	if ast.IsIdentifier(name) && propertyName.Text() == name.Text() {
		propertyName = ast.Handle{}
	}
	return tx.Factory().NewExportDeclaration(0, false, tx.Factory().NewNamedExports(tx.Factory().NewList([]ast.Handle{tx.Factory().NewExportSpecifier(false, propertyName, name)})), ast.Handle{}, ast.Handle{})
}
func (tx *DeclarationTransformer) transformCommonJSExport(input ast.Handle, name ast.Handle) ast.Handle {
	res := tx.transformCommonJSExportWorker(input, name)
	if res.IsNil() {
		return res
	}
	return tx.wrapInCJSExportNamespace(res)
}
func (tx *DeclarationTransformer) transformCommonJSExportWorker(input ast.Handle, name ast.Handle) ast.Handle {
	var nameText string
	if ast.IsIdentifier(name) || ast.IsStringLiteral(name) {
		nameText = name.Text()
	}
	if tx.witnessedCjsExports.Has(nameText) && nameText != "" {
		return ast.Handle{}
	}
	tx.witnessedCjsExports.Add(nameText)
	tx.resultHasExternalModuleIndicator = true
	tx.resultHasScopeMarker = true
	if isCommonJSAliasExport(input) && ast.IsExpressionStatement(input.Parent()) && ast.IsSourceFile(input.Parent().Parent()) {
		return tx.transformBinaryExpressionToExportDeclaration(input, name)
	}
	if ast.IsBinaryExpression(input) {
		if rhs := unwrapParenthesizedExpression(input.BinaryExpressionRight()); ast.IsClassExpression(rhs) {
			ce := rhs
			classExprName := ce.Name()
			hasExprName := !classExprName.IsNil() && len(classExprName.Text()) > 0
			if hasExprName {
				tx.tracker.watchedClassSymbol = rhs.Symbol()
				tx.tracker.classSymbolTracked = false
				defer func() {
					tx.tracker.watchedClassSymbol = nil
					tx.tracker.classSymbolTracked = false
				}()
				className := tx.Factory().NewIdentifier(classExprName.Text())
				classMods := []ast.Handle{tx.Factory().NewModifier(ast.KindExportKeyword)}
				classDecl := tx.transformClassExpressionToDeclaration(rhs, className, tx.Factory().NewModifierList(classMods))
				tx.preserveJsDoc(classDecl, input)
				namesDiffer := !ast.IsIdentifier(name) || classExprName.Text() != name.Text()
				needsIsolation := namesDiffer || tx.tracker.classSymbolTracked
				if needsIsolation {
					nsName := tx.Factory().NewUniqueNameEx("_ns", printer.AutoGenerateOptions{Flags: printer.GeneratedIdentifierFlagsOptimistic})
					var nsMods []ast.Handle
					if tx.needsDeclare {
						nsMods = append(nsMods, tx.Factory().NewModifier(ast.KindDeclareKeyword))
					}
					nsDecl := tx.Factory().NewModuleDeclaration(tx.Factory().NewModifierList(nsMods), ast.KindNamespaceKeyword, nsName, tx.Factory().NewModuleBlock(tx.Factory().NewList([]ast.Handle{classDecl})))
					aliasBase := "_exported"
					if nameText := name.Text(); ast.IsIdentifier(name) && scanner.IsIdentifierText("_"+nameText, core.LanguageVariantStandard) {
						aliasBase = "_" + nameText
					}
					importAlias := tx.Factory().NewUniqueNameEx(aliasBase, printer.AutoGenerateOptions{Flags: printer.GeneratedIdentifierFlagsOptimistic})
					qualifiedName := tx.Factory().NewQualifiedName(nsName, className)
					importDecl := tx.Factory().NewImportEqualsDeclaration(0, false, importAlias, qualifiedName)
					exportSpecifier := tx.Factory().NewExportSpecifier(false, importAlias, name)
					exportDecl := tx.Factory().NewExportDeclaration(0, false, tx.Factory().NewNamedExports(tx.Factory().NewList([]ast.Handle{exportSpecifier})), ast.Handle{}, ast.Handle{})
					tx.removeAllComments(exportDecl)
					return tx.Factory().NewSyntaxList(tx.Factory().NewList(append([]ast.Handle{nsDecl, importDecl}, exportDecl)))
				}
				var mods []ast.Handle
				mods = append(mods, tx.Factory().NewModifier(ast.KindExportKeyword))
				if tx.needsDeclare {
					mods = append(mods, tx.Factory().NewModifier(ast.KindDeclareKeyword))
				}
				classDecl = tx.Factory().UpdateClassDeclaration(classDecl, tx.Factory().NewModifierList(mods), classDecl.ClassDeclarationName(), classDecl.ClassDeclarationTypeParameters(), classDecl.ClassDeclarationHeritageClauses(), classDecl.ClassDeclarationMembers())
				return classDecl
			}
			var mods []ast.Handle
			mods = append(mods, tx.Factory().NewModifier(ast.KindExportKeyword))
			if tx.needsDeclare {
				mods = append(mods, tx.Factory().NewModifier(ast.KindDeclareKeyword))
			}
			className := name
			if !ast.IsIdentifier(className) {
				className = tx.Factory().NewUniqueNameEx("_class", printer.AutoGenerateOptions{Flags: printer.GeneratedIdentifierFlagsOptimistic})
			}
			classDecl := tx.transformClassExpressionToDeclaration(rhs, className, tx.Factory().NewModifierList(mods))
			tx.preserveJsDoc(classDecl, input)
			if !ast.IsIdentifier(name) {
				exportDecl := tx.Factory().NewExportDeclaration(0, false, tx.Factory().NewNamedExports(tx.Factory().NewList([]ast.Handle{tx.Factory().NewExportSpecifier(false, className, name)})), ast.Handle{}, ast.Handle{})
				tx.removeAllComments(exportDecl)
				return tx.Factory().NewSyntaxList(tx.Factory().NewList([]ast.Handle{classDecl, exportDecl}))
			}
			return classDecl
		}
	}
	if ast.IsIdentifier(name) {
		if name.Text() == "default" {
			newId := tx.Factory().NewUniqueNameEx("_default", printer.AutoGenerateOptions{Flags: printer.GeneratedIdentifierFlagsOptimistic})
			tx.state.getSymbolAccessibilityDiagnostic = func(_ printer.SymbolAccessibilityResult) *SymbolAccessibilityDiagnostic {
				return &SymbolAccessibilityDiagnostic{diagnosticMessage: diagnostics.Default_export_of_the_module_has_or_is_using_private_name_0, errorNode: input}
			}
			tx.tracker.PushErrorFallbackNode(input)
			type_ := tx.ensureType(input, false)
			varDecl := tx.Factory().NewVariableDeclaration(newId, ast.Handle{}, type_, ast.Handle{})
			tx.tracker.PopErrorFallbackNode()
			var modList ast.ListRef
			if tx.needsDeclare {
				modList = tx.Factory().NewModifierList([]ast.Handle{tx.Factory().NewModifier(ast.KindDeclareKeyword)})
			} else {
				modList = tx.Factory().NewModifierList([]ast.Handle{})
			}
			statement := tx.Factory().NewVariableStatement(modList, tx.Factory().NewVariableDeclarationList(tx.Factory().NewList([]ast.Handle{varDecl}), ast.NodeFlagsConst))
			assignment := tx.Factory().NewExportAssignment(input.Modifiers(), false, ast.Handle{}, newId)
			tx.preserveJsDoc(statement, input)
			tx.removeAllComments(assignment)
			return tx.Factory().NewSyntaxList(tx.Factory().NewList([]ast.Handle{statement, assignment}))
		} else if tx.host.GetEmitResolver().GetReferencedValueDeclaration(name) == input || tx.host.GetEmitResolver().GetReferencedValueDeclaration(name).IsNil() {
			tx.tracker.PushErrorFallbackNode(input)
			type_ := tx.ensureType(input, false)
			varDecl := tx.Factory().NewVariableDeclaration(name, ast.Handle{}, type_, ast.Handle{})
			tx.tracker.PopErrorFallbackNode()
			var modList ast.ListRef
			if tx.needsDeclare {
				modList = tx.Factory().NewModifierList([]ast.Handle{tx.Factory().NewModifier(ast.KindExportKeyword), tx.Factory().NewModifier(ast.KindDeclareKeyword)})
			} else {
				modList = tx.Factory().NewModifierList([]ast.Handle{tx.Factory().NewModifier(ast.KindExportKeyword)})
			}
			return tx.Factory().NewVariableStatement(modList, tx.Factory().NewVariableDeclarationList(tx.Factory().NewList([]ast.Handle{varDecl}), ast.NodeFlagsNone))
		}
	}
	newId := tx.Factory().NewUniqueNameEx("_exported", printer.AutoGenerateOptions{Flags: printer.GeneratedIdentifierFlagsOptimistic})
	tx.state.getSymbolAccessibilityDiagnostic = func(_ printer.SymbolAccessibilityResult) *SymbolAccessibilityDiagnostic {
		return &SymbolAccessibilityDiagnostic{diagnosticMessage: diagnostics.Default_export_of_the_module_has_or_is_using_private_name_0, errorNode: input}
	}
	tx.tracker.PushErrorFallbackNode(input)
	type_ := tx.ensureType(input, false)
	varDecl := tx.Factory().NewVariableDeclaration(newId, ast.Handle{}, type_, ast.Handle{})
	tx.tracker.PopErrorFallbackNode()
	var modList ast.ListRef
	if tx.needsDeclare {
		modList = tx.Factory().NewModifierList([]ast.Handle{tx.Factory().NewModifier(ast.KindDeclareKeyword)})
	} else {
		modList = tx.Factory().NewModifierList([]ast.Handle{})
	}
	statement := tx.Factory().NewVariableStatement(modList, tx.Factory().NewVariableDeclarationList(tx.Factory().NewList([]ast.Handle{varDecl}), ast.NodeFlagsConst))
	assignment := tx.Factory().NewExportDeclaration(0, false, tx.Factory().NewNamedExports(tx.Factory().NewList([]ast.Handle{tx.Factory().NewExportSpecifier(false, newId, name)})), ast.Handle{}, ast.Handle{})
	tx.preserveJsDoc(statement, input)
	tx.removeAllComments(assignment)
	return tx.Factory().NewSyntaxList(tx.Factory().NewList([]ast.Handle{statement, assignment}))
}
func (tx *DeclarationTransformer) wrapInCJSExportNamespace(content ast.Handle) ast.Handle {
	if tx.cjsExportAssignmentName.IsNil() {
		return content
	}
	nsName := tx.cjsExportAssignmentName
	var members []ast.Handle
	if content.Kind == ast.KindSyntaxList {
		members = content.Store().ListSlice(content.SyntaxListChildren()).Slice()
	} else {
		members = []ast.Handle{content}
	}
	var nsMods []ast.Handle
	if tx.needsDeclare {
		nsMods = append(nsMods, tx.Factory().NewModifier(ast.KindDeclareKeyword))
	}
	members = tx.declareStrippingVisitor.VisitSlice(members)
	return tx.Factory().NewModuleDeclaration(tx.Factory().NewModifierList(nsMods), ast.KindNamespaceKeyword, nsName, tx.Factory().NewModuleBlock(tx.Factory().NewList(members)))
}
func isCommonJSAliasExport(node ast.Handle) bool {
	if ast.IsBinaryExpression(node) && ast.IsIdentifier(node.BinaryExpressionRight()) {
		if symbol := node.Symbol(); symbol != nil && len(symbol.Declarations) == 1 {
			return true
		}
	}
	return false
}

func (tx *DeclarationTransformer) transformClassExpressionToDeclaration(classExpr ast.Handle, className ast.Handle, modifiers ast.ListRef) ast.Handle {
	previousEnclosingDeclaration := tx.enclosingDeclaration
	tx.enclosingDeclaration = classExpr
	previousInClassExpressionDeclaration := tx.inClassExpressionDeclaration
	tx.inClassExpressionDeclaration = true
	defer func() {
		tx.enclosingDeclaration = previousEnclosingDeclaration
		tx.inClassExpressionDeclaration = previousInClassExpressionDeclaration
	}()
	var extraMembers []ast.Handle
	if ast.IsInJSFile(classExpr) {
		extraMembers = tx.collectThisPropertyAssignments(classExpr)
	}
	members := tx.buildClassMembers(classExpr, extraMembers...)
	typeParameters := tx.ensureTypeParams(classExpr, classExpr.ClassExpressionTypeParameters())
	heritageClauses := tx.Visitor().VisitNodes(classExpr.ClassExpressionHeritageClauses())
	return tx.Factory().NewClassDeclaration(modifiers, className, typeParameters, heritageClauses, members)
}
func (tx *DeclarationTransformer) rewriteModuleSpecifier(parent ast.Handle, input ast.Handle) ast.Handle {
	if input.IsNil() {
		return ast.Handle{}
	}
	tx.resultHasExternalModuleIndicator = tx.resultHasExternalModuleIndicator || (parent.Kind != ast.KindModuleDeclaration && parent.Kind != ast.KindImportType)
	return input
}
func (tx *DeclarationTransformer) tryGetResolutionModeOverride(node ast.Handle) ast.Handle {
	if node.IsNil() {
		return node
	}
	mode := tx.host.GetResolutionModeOverride(node)
	if mode != core.ResolutionModeNone {
		return node
	}
	return ast.Handle{}
}
func (tx *DeclarationTransformer) preserveJsDoc(updated ast.Handle, original ast.Handle) {
	tx.EmitContext().AssignCommentRange(updated, original)
}
func (tx *DeclarationTransformer) preservePartialJsDoc(updated ast.Handle, original ast.Handle) {
	if original.Flags()&ast.NodeFlagsReparsed == 0 {
		return
	}
	jsdoc := core.FirstOrNil(original.EagerJSDoc(ast.GetSourceFileOfNode(original)))
	if jsdoc.IsNil() {
		return
	}
	description := scanner.GetTextOfJSDocComment(jsdoc.Store(), jsdoc.JSDocComment())
	if description == "" {
		return
	}
	comment := "*\n * " + strings.ReplaceAll(description, "\n", "\n * ") + "\n "
	tx.EmitContext().AddSyntheticLeadingComment(updated, ast.KindMultiLineCommentTrivia, comment, true)
}
func (tx *DeclarationTransformer) removeAllComments(node ast.Handle) {
	tx.EmitContext().AddEmitFlags(node, printer.EFNoComments)
}
func (tx *DeclarationTransformer) ensureType(node ast.Handle, ignorePrivate bool) ast.Handle {
	if !ignorePrivate && tx.host.GetEffectiveDeclarationFlags(tx.EmitContext().ParseNode(node), ast.ModifierFlagsPrivate) != 0 {
		return ast.Handle{}
	}
	if tx.shouldPrintWithInitializer(node) {
		return ast.Handle{}
	}
	if !ast.IsExportAssignment(node) && !ast.IsBindingElement(node) && !node.Type().IsNil() && (!ast.IsParameterDeclaration(node) || !tx.resolver.RequiresAddingImplicitUndefined(node, nil, tx.enclosingDeclaration)) {
		if tx.state.currentSourceFile.IsJS() {
			jsFlags := declarationEmitNodeBuilderFlags
			if tx.inClassExpressionDeclaration {
				jsFlags &^= nodebuilder.FlagsWriteClassExpressionAsTypeLiteral
			}
			res := tx.resolver.TryJSTypeNodeToTypeNode(tx.EmitContext(), node.Type(), tx.enclosingDeclaration, jsFlags, declarationEmitInternalNodeBuilderFlags, tx.tracker)
			if !res.IsNil() {
				return res
			}
		} else {
			return tx.Visitor().Visit(node.Type())
		}
	}
	oldErrorNameNode := tx.state.errorNameNode
	tx.state.errorNameNode = node.Name()
	var oldDiag GetSymbolAccessibilityDiagnostic
	if !tx.suppressNewDiagnosticContexts {
		oldDiag = tx.state.getSymbolAccessibilityDiagnostic
		if canProduceDiagnostics(node) {
			tx.state.getSymbolAccessibilityDiagnostic = createGetSymbolAccessibilityDiagnosticForNode(node)
		}
	}
	var typeNode ast.Handle
	flags := declarationEmitNodeBuilderFlags
	if tx.inClassExpressionDeclaration {
		flags &^= nodebuilder.FlagsWriteClassExpressionAsTypeLiteral
	}
	if ast.HasInferredType(node) {
		typeNode = tx.resolver.CreateTypeOfDeclaration(tx.EmitContext(), node, tx.enclosingDeclaration, flags, declarationEmitInternalNodeBuilderFlags, tx.tracker)
	} else if ast.IsFunctionLike(node) {
		typeNode = tx.resolver.CreateReturnTypeOfSignatureDeclaration(tx.EmitContext(), node, tx.enclosingDeclaration, flags, declarationEmitInternalNodeBuilderFlags, tx.tracker)
	} else {
		debug.AssertNever(node)
	}
	tx.state.errorNameNode = oldErrorNameNode
	if !tx.suppressNewDiagnosticContexts {
		tx.state.getSymbolAccessibilityDiagnostic = oldDiag
	}
	if typeNode.IsNil() {
		return tx.Factory().NewKeywordTypeNode(ast.KindAnyKeyword)
	}
	return typeNode
}
func (tx *DeclarationTransformer) shouldPrintWithInitializer(node ast.Handle) bool {
	return canHaveLiteralInitializer(tx.host, node) && !node.Initializer().IsNil() && tx.resolver.IsLiteralConstDeclaration(tx.EmitContext().MostOriginal(node))
}
func (tx *DeclarationTransformer) checkEntityNameVisibility(entityName ast.Handle, enclosingDeclaration ast.Handle) {
	visibilityResult := tx.resolver.IsEntityNameVisible(entityName, enclosingDeclaration)
	tx.tracker.handleSymbolAccessibilityError(visibilityResult)
}

func (tx *DeclarationTransformer) transformTopLevelDeclaration(input ast.Handle) ast.Handle {
	if len(tx.state.lateMarkedStatements) > 0 {
		tx.state.lateMarkedStatements = core.Filter(tx.state.lateMarkedStatements, func(node ast.Handle) bool {
			return node != input
		})
	}
	if tx.shouldStripInternal(input) {
		return ast.Handle{}
	}
	if input.Kind == ast.KindImportEqualsDeclaration {
		return tx.transformImportEqualsDeclaration(input)
	}
	if input.Kind == ast.KindImportDeclaration || input.Kind == ast.KindJSImportDeclaration {
		res := tx.transformImportDeclaration(input)
		if !res.IsNil() && res.Kind != ast.KindImportDeclaration {
			res = tx.Factory().DeepCloneNode(res)
			return res
		}
		return res
	}
	if ast.IsDeclaration(input) && isDeclarationAndNotVisible(tx.EmitContext(), tx.resolver, input) {
		return ast.Handle{}
	}
	if ast.IsFunctionLike(input) && tx.resolver.IsImplementationOfOverload(input) {
		return ast.Handle{}
	}
	original := tx.EmitContext().MostOriginal(input)
	id := tx.EmitContext().NodeIdentity(original)
	_, isExpandoHost := tx.expandoHosts[id]
	_, hasDeferredExpandoAssignments := tx.deferredExpandoAssignments[id]
	if isExpandoHost || hasDeferredExpandoAssignments {
		return tx.createFullExpandoBlock(id)
	}
	previousEnclosingDeclaration := tx.enclosingDeclaration
	if isEnclosingDeclaration(input) {
		tx.enclosingDeclaration = input
	}
	canProduceDiagnostic := canProduceDiagnostics(input)
	oldDiag := tx.state.getSymbolAccessibilityDiagnostic
	oldName := tx.state.errorNameNode
	if canProduceDiagnostic {
		tx.state.getSymbolAccessibilityDiagnostic = createGetSymbolAccessibilityDiagnosticForNode(input)
	}
	saveNeedsDeclare := tx.needsDeclare
	var result ast.Handle
	switch input.Kind {
	case ast.KindTypeAliasDeclaration, ast.KindJSTypeAliasDeclaration:
		result = tx.transformTypeAliasDeclaration(input)
	case ast.KindInterfaceDeclaration:
		result = tx.transformInterfaceDeclaration(input)
	case ast.KindFunctionDeclaration:
		result = tx.transformFunctionDeclaration(input)
	case ast.KindModuleDeclaration:
		result = tx.transformModuleDeclaration(input)
	case ast.KindClassDeclaration:
		result = tx.transformClassDeclaration(input)
	case ast.KindVariableStatement:
		result = tx.transformVariableStatement(input)
	case ast.KindEnumDeclaration:
		result = tx.transformEnumDeclaration(input)
	default:
		panic(fmt.Sprintf("Unhandled top-level node in declaration emit: %q", input.Kind))
	}
	tx.enclosingDeclaration = previousEnclosingDeclaration
	tx.state.getSymbolAccessibilityDiagnostic = oldDiag
	tx.needsDeclare = saveNeedsDeclare
	tx.state.errorNameNode = oldName
	return result
}
func (tx *DeclarationTransformer) transformTypeAliasDeclaration(input ast.Handle) ast.Handle {
	tx.needsDeclare = false
	return tx.Factory().UpdateTypeAliasDeclaration(input, tx.ensureModifiers(input), input.Name(), tx.Visitor().VisitNodes(input.TypeParameterList()), tx.Visitor().Visit(input.Type()))
}
func (tx *DeclarationTransformer) transformInterfaceDeclaration(input ast.Handle) ast.Handle {
	return tx.Factory().UpdateInterfaceDeclaration(input, tx.ensureModifiers(input), input.Name(), tx.Visitor().VisitNodes(input.TypeParameterList()), tx.Visitor().VisitNodes(input.HeritageClauses()), tx.Visitor().VisitNodes(input.MemberList()))
}
func (tx *DeclarationTransformer) transformFunctionDeclaration(input ast.Handle) ast.Handle {
	if tx.resolver.IsExpandoFunctionDeclaration(input) {
		tx.state.reportExpandoFunctionErrors(input)
	}
	return tx.Factory().UpdateFunctionDeclaration(input, tx.ensureModifiers(input), ast.Handle{}, input.Name(), tx.ensureTypeParams(input, input.TypeParameterList()), tx.updateParamList(input, input.ParameterList()), tx.ensureType(input, false), ast.Handle{}, ast.Handle{})
}
func (tx *DeclarationTransformer) transformModuleDeclaration(input ast.Handle) ast.Handle {
	mods := tx.ensureModifiers(input)
	saveNeedsDeclare := tx.needsDeclare
	tx.needsDeclare = false
	inner := input.Body()
	keyword := input.ModuleDeclarationKeyword()
	if keyword != ast.KindGlobalKeyword && (input.Name().IsNil() || !ast.IsStringLiteral(input.Name())) {
		keyword = ast.KindNamespaceKeyword
	}
	if !inner.IsNil() && inner.Kind == ast.KindModuleBlock {
		oldNeedsScopeFix := tx.needsScopeFixMarker
		oldHasScopeFix := tx.resultHasScopeMarker
		tx.resultHasScopeMarker = false
		tx.needsScopeFixMarker = false
		statements := tx.Visitor().VisitNodes(inner.StatementList())
		lateStatements := tx.transformAndReplaceLatePaintedStatements(statements)
		if input.Flags()&ast.NodeFlagsAmbient != 0 {
			tx.needsScopeFixMarker = false
		}
		if !ast.IsGlobalScopeAugmentation(input) && !tx.resultHasScopeMarker && !hasScopeMarker(tx.EmitContext().StoreFile().ParseStore(), lateStatements) {
			if tx.needsScopeFixMarker {
				lateStatements = tx.Factory().NewList(append(tx.EmitContext().StoreFile().ParseStore().ListSlice(lateStatements).Slice(), createEmptyExports(tx.Factory().Factory)))
			} else {
				lateStatements = tx.exportStrippingVisitor.VisitNodes(lateStatements)
			}
		}
		body := tx.Factory().UpdateModuleBlock(inner, lateStatements)
		tx.needsDeclare = saveNeedsDeclare
		tx.needsScopeFixMarker = oldNeedsScopeFix
		tx.resultHasScopeMarker = oldHasScopeFix
		return tx.Factory().UpdateModuleDeclaration(input, mods, keyword, input.Name(), body)
	}
	if !inner.IsNil() {
		tx.Visitor().Visit(inner)
		original := tx.EmitContext().MostOriginal(inner)
		id := tx.EmitContext().NodeIdentity(original)
		body, _ := tx.lateStatementReplacementMap[id]
		delete(tx.lateStatementReplacementMap, id)
		return tx.Factory().UpdateModuleDeclaration(input, mods, keyword, input.Name(), body)
	}
	return tx.Factory().UpdateModuleDeclaration(input, mods, keyword, input.Name(), ast.Handle{})
}
func (tx *DeclarationTransformer) stripExportModifiers(statement ast.Handle) ast.Handle {
	if statement.IsNil() {
		return ast.Handle{}
	}
	parseNode := tx.EmitContext().ParseNode(statement)
	if ast.IsImportEqualsDeclaration(statement) || (!parseNode.IsNil() && tx.host.GetEffectiveDeclarationFlags(parseNode, ast.ModifierFlagsDefault) != 0) || !ast.CanHaveModifiers(statement) {
		return statement
	}
	oldFlags := ast.GetCombinedModifierFlags(statement)
	if oldFlags&ast.ModifierFlagsExport == 0 {
		return statement
	}
	newFlags := oldFlags & (ast.ModifierFlagsAll ^ ast.ModifierFlagsExport)
	modifiers := ast.CreateModifiersFromModifierFlags(newFlags, tx.Factory().NewModifier)
	return ast.ReplaceHandleModifiers(tx.Factory().Factory, statement, tx.Factory().NewModifierList(modifiers))
}

func (tx *DeclarationTransformer) buildClassMembers(classNode ast.Handle, extraMembers ...ast.Handle) ast.ListRef {
	ctor := ast.GetFirstConstructorWithBody(classNode)
	var parameterProperties []ast.Handle
	if !ctor.IsNil() {
		oldDiag := tx.state.getSymbolAccessibilityDiagnostic
		for _, param := range ctor.Store().ListSlice(ctor.ConstructorDeclarationParameters()).All() {
			if !ast.HasSyntacticModifier(param, ast.ModifierFlagsParameterPropertyModifier) || tx.shouldStripInternal(param) {
				continue
			}
			tx.state.getSymbolAccessibilityDiagnostic = createGetSymbolAccessibilityDiagnosticForNode(param)
			if param.Name().Kind == ast.KindIdentifier {
				updated := tx.Factory().NewPropertyDeclaration(tx.ensureModifiers(param), param.Name(), param.QuestionToken(), tx.ensureType(param, false), tx.ensureNoInitializer(param))
				tx.preserveJsDoc(updated, param)
				parameterProperties = append(parameterProperties, updated)
			} else {
				parameterProperties = append(parameterProperties, tx.walkBindingPattern(param.Name(), param)...)
			}
		}
		tx.state.getSymbolAccessibilityDiagnostic = oldDiag
	}
	var privateIdentifier ast.Handle
	if core.Some(classNode.Members(), func(member ast.Handle) bool {
		return !member.Name().IsNil() && ast.IsPrivateIdentifier(member.Name())
	}) {
		privateIdentifier = tx.Factory().NewPropertyDeclaration(0, tx.Factory().NewPrivateIdentifier("#private"), ast.Handle{}, ast.Handle{}, ast.Handle{})
	}
	lateIndexes := tx.resolver.CreateLateBoundIndexSignatures(tx.EmitContext(), classNode, tx.enclosingDeclaration, declarationEmitNodeBuilderFlags, declarationEmitInternalNodeBuilderFlags, tx.tracker)
	memberNodes := make([]ast.Handle, 0, len(classNode.Members()))
	if !privateIdentifier.IsNil() {
		memberNodes = append(memberNodes, privateIdentifier)
	}
	memberNodes = append(memberNodes, lateIndexes...)
	memberNodes = append(memberNodes, parameterProperties...)
	memberNodes = append(memberNodes, extraMembers...)
	visitResult := tx.Visitor().VisitNodes(classNode.MemberList())
	if visitResult != 0 && classNode.Store().ListLen(visitResult) > 0 {
		memberNodes = append(memberNodes, classNode.Store().ListSlice(visitResult).Slice()...)
	}
	return tx.Factory().NewList(memberNodes)
}
func (tx *DeclarationTransformer) transformClassDeclaration(input ast.Handle) ast.Handle {
	previousEnclosingDeclaration := tx.enclosingDeclaration
	tx.enclosingDeclaration = input
	defer func() {
		tx.enclosingDeclaration = previousEnclosingDeclaration
	}()
	tx.state.errorNameNode = input.Name()
	tx.tracker.PushErrorFallbackNode(input)
	defer tx.tracker.PopErrorFallbackNode()
	modifiers := tx.ensureModifiers(input)
	typeParameters := tx.ensureTypeParams(input, input.TypeParameterList())
	var extraMembers []ast.Handle
	if ast.IsInJSFile(input) {
		extraMembers = tx.collectThisPropertyAssignments(input)
	}
	members := tx.buildClassMembers(input, extraMembers...)
	extendsClause := getEffectiveBaseTypeNode(input)
	if !extendsClause.IsNil() && !ast.IsEntityNameExpression(extendsClause.ExpressionWithTypeArgumentsExpression()) && extendsClause.ExpressionWithTypeArgumentsExpression().Kind != ast.KindNullKeyword {
		tx.tracker.ReportInferenceFallback(extendsClause.ExpressionWithTypeArgumentsExpression())
		oldId := "default"
		if ast.NodeIsPresent(input.Name()) && ast.IsIdentifier(input.Name()) && len(input.Name().Text()) > 0 {
			oldId = input.Name().Text()
		}
		newId := tx.Factory().NewUniqueNameEx(oldId+"_base", printer.AutoGenerateOptions{Flags: printer.GeneratedIdentifierFlagsOptimistic})
		tx.state.getSymbolAccessibilityDiagnostic = func(_ printer.SymbolAccessibilityResult) *SymbolAccessibilityDiagnostic {
			return &SymbolAccessibilityDiagnostic{diagnosticMessage: diagnostics.X_extends_clause_of_exported_class_0_has_or_is_using_private_name_1, errorNode: extendsClause, typeName: input.Name()}
		}
		varDecl := tx.Factory().NewVariableDeclaration(newId, ast.Handle{}, tx.resolver.CreateTypeOfExpression(tx.EmitContext(), extendsClause.Expression(), input, declarationEmitNodeBuilderFlags, declarationEmitInternalNodeBuilderFlags, tx.tracker), ast.Handle{})
		var mods ast.ListRef
		if tx.needsDeclare {
			mods = tx.Factory().NewModifierList([]ast.Handle{tx.Factory().NewModifier(ast.KindDeclareKeyword)})
		}
		statement := tx.Factory().NewVariableStatement(mods, tx.Factory().NewVariableDeclarationList(tx.Factory().NewList([]ast.Handle{varDecl}), ast.NodeFlagsConst))
		newHeritageClause := tx.Factory().UpdateHeritageClause(extendsClause.Parent(), extendsClause.Parent().HeritageClauseToken(), tx.Factory().NewList([]ast.Handle{tx.Factory().UpdateExpressionWithTypeArguments(extendsClause, newId, tx.Visitor().VisitNodes(extendsClause.ExpressionWithTypeArgumentsTypeArguments()))}))
		retainedHeritageClauses := tx.Visitor().VisitNodes(input.HeritageClauses())
		heritageList := []ast.Handle{newHeritageClause}
		if retainedHeritageClauses != 0 {
			heritageList = append(heritageList, tx.EmitContext().StoreFile().ParseStore().ListSlice(retainedHeritageClauses).Slice()...)
		}
		heritageClauses := tx.Factory().NewList(heritageList)
		return tx.Factory().NewSyntaxList(tx.Factory().NewList([]ast.Handle{statement, tx.Factory().UpdateClassDeclaration(input, modifiers, input.Name(), typeParameters, heritageClauses, members)}))
	}
	return tx.Factory().UpdateClassDeclaration(input, modifiers, input.Name(), typeParameters, tx.Visitor().VisitNodes(input.HeritageClauses()), members)
}
func (tx *DeclarationTransformer) visitThisPropertyAssignments(node ast.Handle) ast.Handle {
	var thisTarget ast.Handle
	isStatic := false
	thisContainer := ast.GetThisContainer(node, false, false)
	thisTarget = thisContainer.Parent()
	if thisTarget.IsNil() {
		return ast.Handle{}
	}
	if ast.HasStaticModifier(thisContainer) || ast.IsClassStaticBlockDeclaration(thisContainer) {
		isStatic = true
	}
	if thisTarget != tx.enclosingDeclaration {
		return ast.Handle{}
	}
caseBlock:
	switch ast.GetAssignmentDeclarationKind(node) {
	case ast.JSDeclarationKindThisProperty:
		name := ast.GetNameOfDeclaration(node)
		base := tx.resolver.GetReferencedMemberValueDeclaration(node)
		key := getThisPropertyAssignmentKey(name, node, isStatic)
		if base.IsNil() || tx.seenProperties.Has(key) {
			break
		}
		tx.seenProperties.Add(key)
		if thisTarget.HeritageClauses() != 0 && thisTarget.Store().ListLen(thisTarget.HeritageClauses()) > 0 && !isClassExtendingNull(thisTarget) {
			tx.tracker.ReportInferenceFallback(thisTarget)
			if tx.resolver.IsThisPropertyAssignmentDeclarationRedundant(node) {
				break caseBlock
			}
		}
		var mods ast.ListRef
		if isStatic {
			mods = tx.Factory().NewModifierList([]ast.Handle{tx.Factory().NewModifier(ast.KindStaticKeyword)})
		}
		if ast.HasDynamicName(node) {
			if !transformers.IsSimpleInlineableExpression(name) {
				break
			}
			tx.checkName(node)
			name = tx.Factory().NewComputedPropertyName(name)
		}
		if ast.GetTextOfPropertyName(name) == "constructor" {
			break
		}
		if ast.IsIdentifier(name) && !scanner.IsIdentifierText(name.Text(), core.LanguageVariantStandard) {
			name = tx.Factory().NewStringLiteralFromNode(name)
		}
		prop := tx.Factory().NewPropertyDeclaration(mods, name, ast.Handle{}, tx.ensureType(node, false), ast.Handle{})
		if ast.IsExpressionStatement(node.Parent()) {
			tx.preserveJsDoc(prop, node.Parent())
		}
		tx.thisPropertyAssignmentsCollected = append(tx.thisPropertyAssignmentsCollected, prop)
	}
	return tx.thisPropertyVisitor.VisitEachChild(node)
}
func isClassExtendingNull(node ast.Handle) bool {
	if node.IsNil() {
		return false
	}
	extendsClause := ast.GetHeritageClause(node, ast.KindExtendsKeyword)
	if extendsClause.IsNil() {
		return false
	}
	types := extendsClause.HeritageClauseTypes()
	if types == 0 || node.Store().ListLen(types) != 1 {
		return false
	}
	expr := node.Store().ListAt(types, 0).ExpressionWithTypeArgumentsExpression()
	return !expr.IsNil() && expr.Kind == ast.KindNullKeyword
}

func (tx *DeclarationTransformer) collectThisPropertyAssignments(classNode ast.Handle) []ast.Handle {
	members := classNode.MemberList()
	seen := collections.Set[thisPropertyAssignmentKey]{}
	for _, member := range classNode.Store().ListSlice(members).All() {
		if !member.Name().IsNil() {
			isStatic := ast.IsStatic(member)
			seen.Add(getThisPropertyAssignmentKey(member.Name(), member, isStatic))
		}
	}
	tx.seenProperties = seen
	defer tx.seenProperties.Clear()
	tx.thisPropertyAssignmentsCollected = []ast.Handle{}
	defer func() {
		tx.thisPropertyAssignmentsCollected = nil
	}()
	for _, n := range classNode.Store().ListSlice(members).All() {
		tx.thisPropertyVisitor.VisitEachChild(n)
	}
	return tx.thisPropertyAssignmentsCollected
}
func (tx *DeclarationTransformer) walkBindingPattern(pattern ast.Handle, param ast.Handle) []ast.Handle {
	var elems []ast.Handle
	for _, elem := range pattern.Elements() {
		if ast.IsOmittedExpression(elem) {
			continue
		}
		if ast.IsBindingPattern(elem.Name()) {
			elems = append(elems, tx.walkBindingPattern(elem.Name(), param)...)
			continue
		}
		elems = append(elems, tx.Factory().NewPropertyDeclaration(tx.ensureModifiers(param), elem.Name(), ast.Handle{}, tx.ensureType(elem, false), ast.Handle{}))
	}
	return elems
}
func (tx *DeclarationTransformer) transformVariableStatement(input ast.Handle) ast.Handle {
	decls := input.VariableStatementDeclarationList().Store().ListSlice(input.VariableStatementDeclarationList().VariableDeclarationListDeclarations())
	visible := decls.Some(func(decl ast.Handle) bool {
		return getBindingNameVisible(tx.resolver, decl)
	})
	if !visible {
		return ast.Handle{}
	}
	inputNodes := decls.Slice()
	var extraImports []ast.Handle
	if !tx.state.currentSourceFile.CommonJSModuleIndicator.IsNil() {
		var normalDeclarations []ast.Handle
		var imports []ast.Handle
		for _, n := range inputNodes {
			if ast.IsVariableDeclarationInitializedToRequire(n) {
				imports = append(imports, n)
			} else {
				normalDeclarations = append(normalDeclarations, n)
			}
		}
		inputNodes = normalDeclarations
		extraImports = tx.Visitor().VisitSlice(imports)
	}
	nodes := tx.Visitor().VisitSlice(inputNodes)
	if len(nodes) == 0 {
		if len(extraImports) > 0 {
			return tx.Factory().NewSyntaxList(tx.Factory().NewList(extraImports))
		}
		return ast.Handle{}
	}
	nodeList := tx.Factory().NewList(nodes)
	modifiers := tx.ensureModifiers(input)
	var declList ast.Handle
	if ast.IsVarUsing(input.VariableStatementDeclarationList()) || ast.IsVarAwaitUsing(input.VariableStatementDeclarationList()) {
		declList = tx.Factory().NewVariableDeclarationList(nodeList, ast.NodeFlagsConst)
		tx.EmitContext().SetOriginal(declList, input.VariableStatementDeclarationList())
		tx.EmitContext().SetCommentRange(declList, input.VariableStatementDeclarationList().Loc())
		declList.SetLoc(input.VariableStatementDeclarationList().Loc())
	} else {
		declList = tx.Factory().UpdateVariableDeclarationList(input.VariableStatementDeclarationList(), nodeList, input.VariableStatementDeclarationList().Flags())
	}
	res := tx.Factory().UpdateVariableStatement(input, modifiers, declList)
	if len(extraImports) > 0 {
		return tx.Factory().NewSyntaxList(tx.Factory().NewList(append(extraImports, res)))
	}
	return res
}
func (tx *DeclarationTransformer) transformEnumDeclaration(input ast.Handle) ast.Handle {
	return tx.Factory().UpdateEnumDeclaration(input, tx.ensureModifiers(input), input.Name(), tx.Factory().NewList(core.MapNonNil(input.Members(), func(m ast.Handle) ast.Handle {
		if tx.shouldStripInternal(m) {
			return ast.Handle{}
		}
		enumValue := tx.resolver.GetEnumMemberValue(m)
		if tx.state.isolatedDeclarations && !m.Initializer().IsNil() && enumValue.HasExternalReferences && !ast.IsComputedPropertyName(m.Name()) {
			tx.state.addDiagnostic(createDiagnosticForNode(m, diagnostics.Enum_member_initializers_must_be_computable_without_references_to_external_symbols_with_isolatedDeclarations))
		}
		var newInitializer ast.Handle
		switch value := enumValue.Value.(type) {
		case jsnum.Number:
			if value.IsInf() {
				if value > 0 {
					newInitializer = tx.Factory().NewIdentifier("Infinity")
				} else {
					newInitializer = tx.Factory().NewPrefixUnaryExpression(ast.KindMinusToken, tx.Factory().NewIdentifier("Infinity"))
				}
			} else if value.IsNaN() {
				newInitializer = tx.Factory().NewIdentifier("NaN")
			} else if value >= 0 {
				newInitializer = tx.Factory().NewNumericLiteral(value.String(), ast.TokenFlagsNone)
			} else {
				newInitializer = tx.Factory().NewPrefixUnaryExpression(ast.KindMinusToken, tx.Factory().NewNumericLiteral((-value).String(), ast.TokenFlagsNone))
			}
		case string:
			newInitializer = tx.Factory().NewStringLiteral(value, ast.TokenFlagsNone)
		default:
			newInitializer = ast.Handle{}
		}
		result := tx.Factory().UpdateEnumMember(m, m.Name(), newInitializer)
		tx.preserveJsDoc(result, m)
		return result
	})))
}
func (tx *DeclarationTransformer) ensureModifiers(node ast.Handle) ast.ListRef {
	currentFlags := ast.GetCombinedModifierFlags(tx.EmitContext().ParseNode(node)) & ast.ModifierFlagsAll
	newFlags := tx.ensureModifierFlags(node)
	if currentFlags == newFlags {
		mods := node.Modifiers()
		if mods == 0 {
			return mods
		}
		modsSeq := node.Store().ListSlice(mods)
		if canReuseModifierNodes(modsSeq) {
			filtered := make([]ast.Handle, 0, modsSeq.Len())
			for _, m := range modsSeq.All() {
				if ast.IsModifier(m) {
					filtered = append(filtered, m)
				}
			}
			return tx.Factory().NewModifierList(filtered)
		}
	}
	result := ast.CreateModifiersFromModifierFlags(newFlags, tx.Factory().NewModifier)
	if len(result) == 0 {
		return 0
	}
	return tx.Factory().NewModifierList(result)
}
func (tx *DeclarationTransformer) ensureModifierFlags(node ast.Handle) ast.ModifierFlags {
	mask := ast.ModifierFlagsAll ^ (ast.ModifierFlagsPublic | ast.ModifierFlagsAsync | ast.ModifierFlagsOverride)
	additions := ast.ModifierFlagsNone
	if tx.needsDeclare && !isAlwaysType(node) {
		additions = ast.ModifierFlagsAmbient
	}
	parentIsFile := node.Parent().Kind == ast.KindSourceFile
	if !parentIsFile {
		mask ^= ast.ModifierFlagsAmbient
		additions = ast.ModifierFlagsNone
	}
	if ast.IsImplicitlyExportedJSDocDeclaration(node) {
		additions |= ast.ModifierFlagsExport
	}
	return maskModifierFlags(node, mask, additions)
}
func (tx *DeclarationTransformer) ensureTypeParams(node ast.Handle, params ast.ListRef) ast.ListRef {
	if tx.host.GetEffectiveDeclarationFlags(tx.EmitContext().ParseNode(node), ast.ModifierFlagsPrivate) != 0 {
		return 0
	}
	var typeParameters ast.ListRef
	if typeParameters = tx.Visitor().VisitNodes(params); typeParameters != 0 {
		return typeParameters
	}
	oldErrorNameNode := tx.state.errorNameNode
	tx.state.errorNameNode = node.Name()
	var oldDiag GetSymbolAccessibilityDiagnostic
	if !tx.suppressNewDiagnosticContexts {
		oldDiag = tx.state.getSymbolAccessibilityDiagnostic
		if canProduceDiagnostics(node) {
			tx.state.getSymbolAccessibilityDiagnostic = createGetSymbolAccessibilityDiagnosticForNode(node)
		}
	}
	if !node.FullSignature().IsNil() {
		if nodes := tx.resolver.CreateTypeParametersOfSignatureDeclaration(tx.EmitContext(), node, tx.enclosingDeclaration, declarationEmitNodeBuilderFlags, declarationEmitInternalNodeBuilderFlags, tx.tracker); nodes != nil {
			typeParameters = tx.Factory().NewList(nodes)
		}
	}
	tx.state.errorNameNode = oldErrorNameNode
	if !tx.suppressNewDiagnosticContexts {
		tx.state.getSymbolAccessibilityDiagnostic = oldDiag
	}
	return typeParameters
}
func (tx *DeclarationTransformer) updateParamList(node ast.Handle, params ast.ListRef) ast.ListRef {
	if tx.host.GetEffectiveDeclarationFlags(tx.EmitContext().ParseNode(node), ast.ModifierFlagsPrivate) != 0 || node.Store().ListLen(params) == 0 {
		return tx.Factory().NewList([]ast.Handle{})
	}
	results := make([]ast.Handle, node.Store().ListLen(params))
	for i, p := range node.Store().ListSlice(params).All() {
		results[i] = tx.ensureParameter(p)
	}
	return tx.Factory().NewList(results)
}
func (tx *DeclarationTransformer) ensureParameter(p ast.Handle) ast.Handle {
	oldDiag := tx.state.getSymbolAccessibilityDiagnostic
	if !tx.suppressNewDiagnosticContexts {
		tx.state.getSymbolAccessibilityDiagnostic = createGetSymbolAccessibilityDiagnosticForNode(p)
	}
	var questionToken ast.Handle
	if tx.resolver.IsOptionalParameter(p) {
		if !p.QuestionToken().IsNil() {
			questionToken = p.QuestionToken()
		} else {
			questionToken = tx.Factory().NewToken(ast.KindQuestionToken)
		}
	}
	result := tx.Factory().UpdateParameterDeclaration(p, 0, p.DotDotDotToken(), tx.bindingNameVisitor.VisitNode(p.Name()), questionToken, tx.ensureType(p, true), tx.ensureNoInitializer(p))
	tx.state.getSymbolAccessibilityDiagnostic = oldDiag
	return result
}
func (tx *DeclarationTransformer) ensureNoInitializer(node ast.Handle) ast.Handle {
	if tx.shouldPrintWithInitializer(node) {
		unwrappedInitializer := unwrapParenthesizedExpression(node.Initializer())
		if !ast.IsPrimitiveLiteralValue(unwrappedInitializer, true) {
			tx.tracker.ReportInferenceFallback(node)
		}
		return tx.resolver.CreateLiteralConstValue(tx.EmitContext(), tx.EmitContext().ParseNode(node), tx.tracker)
	}
	return ast.Handle{}
}
func (tx *DeclarationTransformer) visitBindingName(node ast.Handle) ast.Handle {
	switch node.Kind {
	case ast.KindIdentifier, ast.KindOmittedExpression:
		return node
	case ast.KindArrayBindingPattern, ast.KindObjectBindingPattern:
		return tx.bindingNameVisitor.VisitEachChild(node)
	case ast.KindBindingElement:
		if !node.PropertyName().IsNil() && ast.IsComputedPropertyName(node.PropertyName()) && ast.IsEntityNameExpression(node.PropertyName().Expression()) {
			tx.checkEntityNameVisibility(node.PropertyName().Expression(), tx.enclosingDeclaration)
		}
		return tx.Factory().UpdateBindingElement(node, node.BindingElementDotDotDotToken(), node.PropertyName(), tx.bindingNameVisitor.VisitNode(node.Name()), ast.Handle{})
	default:
		return node
	}
}
func (tx *DeclarationTransformer) transformImportEqualsDeclaration(decl ast.Handle) ast.Handle {
	if !tx.resolver.IsDeclarationVisible(decl) {
		return ast.Handle{}
	}
	if decl.ModuleReference().Kind == ast.KindExternalModuleReference {
		specifier := ast.GetExternalModuleImportEqualsDeclarationExpression(decl)
		return tx.Factory().UpdateImportEqualsDeclaration(decl, decl.Modifiers(), decl.IsTypeOnly(), decl.Name(), tx.Factory().UpdateExternalModuleReference(decl.ModuleReference(), tx.rewriteModuleSpecifier(decl, specifier)))
	} else {
		oldDiag := tx.state.getSymbolAccessibilityDiagnostic
		tx.state.getSymbolAccessibilityDiagnostic = createGetSymbolAccessibilityDiagnosticForNode(decl)
		tx.checkEntityNameVisibility(decl.ModuleReference(), tx.enclosingDeclaration)
		tx.state.getSymbolAccessibilityDiagnostic = oldDiag
		return decl
	}
}
func (tx *DeclarationTransformer) transformImportDeclaration(decl ast.Handle) ast.Handle {
	if decl.ImportClause().IsNil() {
		return tx.Factory().UpdateImportDeclaration(decl, decl.Modifiers(), decl.ImportClause(), tx.rewriteModuleSpecifier(decl, decl.ModuleSpecifier()), tx.tryGetResolutionModeOverride(decl.Attributes()))
	}
	phaseModifier := decl.ImportClause().ImportClausePhaseModifier()
	if phaseModifier == ast.KindDeferKeyword {
		phaseModifier = ast.KindUnknown
	}
	var visibleDefaultBinding ast.Handle
	if !decl.ImportClause().IsNil() && !decl.ImportClause().Name().IsNil() && tx.resolver.IsDeclarationVisible(decl.ImportClause()) {
		visibleDefaultBinding = decl.ImportClause().Name()
	}
	if decl.ImportClause().ImportClauseNamedBindings().IsNil() {
		if visibleDefaultBinding.IsNil() {
			return ast.Handle{}
		}
		return tx.Factory().UpdateImportDeclaration(decl, decl.Modifiers(), tx.Factory().UpdateImportClause(decl.ImportClause(), phaseModifier, visibleDefaultBinding, ast.Handle{}), tx.rewriteModuleSpecifier(decl, decl.ModuleSpecifier()), tx.tryGetResolutionModeOverride(decl.Attributes()))
	}
	if decl.ImportClause().ImportClauseNamedBindings().Kind == ast.KindNamespaceImport {
		var namedBindings ast.Handle
		if tx.resolver.IsDeclarationVisible(decl.ImportClause().ImportClauseNamedBindings()) {
			namedBindings = decl.ImportClause().ImportClauseNamedBindings()
		}
		if visibleDefaultBinding.IsNil() && namedBindings.IsNil() {
			return ast.Handle{}
		}
		return tx.Factory().UpdateImportDeclaration(decl, decl.Modifiers(), tx.Factory().UpdateImportClause(decl.ImportClause(), phaseModifier, visibleDefaultBinding, namedBindings), tx.rewriteModuleSpecifier(decl, decl.ModuleSpecifier()), tx.tryGetResolutionModeOverride(decl.Attributes()))
	}
	bindingList := core.Filter(decl.ImportClause().ImportClauseNamedBindings().Elements(), func(b ast.Handle) bool {
		return tx.resolver.IsDeclarationVisible(b)
	})
	if len(bindingList) > 0 || !visibleDefaultBinding.IsNil() {
		var namedImports ast.Handle
		if len(bindingList) > 0 {
			namedImports = tx.Factory().UpdateNamedImports(decl.ImportClause().ImportClauseNamedBindings(), tx.Factory().NewList(bindingList))
		}
		return tx.Factory().UpdateImportDeclaration(decl, decl.Modifiers(), tx.Factory().UpdateImportClause(decl.ImportClause(), phaseModifier, visibleDefaultBinding, namedImports), tx.rewriteModuleSpecifier(decl, decl.ModuleSpecifier()), tx.tryGetResolutionModeOverride(decl.Attributes()))
	}
	if tx.resolver.IsImportRequiredByAugmentation(decl) {
		if tx.state.isolatedDeclarations {
			tx.state.addDiagnostic(createDiagnosticForNode(decl, diagnostics.Declaration_emit_for_this_file_requires_preserving_this_import_for_augmentations_This_is_not_supported_with_isolatedDeclarations))
		}
		return tx.Factory().UpdateImportDeclaration(decl, decl.Modifiers(), ast.Handle{}, tx.rewriteModuleSpecifier(decl, decl.ModuleSpecifier()), tx.tryGetResolutionModeOverride(decl.Attributes()))
	}
	return ast.Handle{}
}
func (tx *DeclarationTransformer) transformJSDocTypeExpression(input ast.Handle) ast.Handle {
	return tx.Visitor().Visit(input.Type())
}
func (tx *DeclarationTransformer) transformJSDocTypeLiteral(input ast.Handle) ast.Handle {
	members := tx.Visitor().VisitSlice(input.Store().ListSlice(input.JSDocTypeLiteralJSDocPropertyTags()).Slice())
	replacement := tx.Factory().NewTypeLiteralNode(tx.Factory().NewList(members))
	tx.EmitContext().SetOriginal(replacement, input)
	return replacement
}
func (tx *DeclarationTransformer) transformJSDocPropertyTag(input ast.Handle) ast.Handle {
	replacement := tx.Factory().NewPropertySignatureDeclaration(0, tx.Visitor().Visit(input.TagName()), ast.Handle{}, tx.Visitor().Visit(input.TypeExpression()), ast.Handle{})
	tx.EmitContext().SetOriginal(replacement, input)
	return replacement
}
func (tx *DeclarationTransformer) transformJSDocAllType(input ast.Handle) ast.Handle {
	replacement := tx.Factory().NewKeywordTypeNode(ast.KindAnyKeyword)
	tx.EmitContext().SetOriginal(replacement, input)
	return replacement
}
func (tx *DeclarationTransformer) transformJSDocNullableType(input ast.Handle) ast.Handle {
	replacement := tx.Factory().NewUnionTypeNode(tx.Factory().NewList([]ast.Handle{tx.Visitor().Visit(input.Type()), tx.Factory().NewLiteralTypeNode(tx.Factory().NewKeywordExpression(ast.KindNullKeyword))}))
	tx.EmitContext().SetOriginal(replacement, input)
	return replacement
}
func (tx *DeclarationTransformer) transformJSDocNonNullableType(input ast.Handle) ast.Handle {
	return tx.Visitor().Visit(input.Type())
}
func (tx *DeclarationTransformer) transformJSDocVariadicType(input ast.Handle) ast.Handle {
	replacement := tx.Factory().NewArrayTypeNode(tx.Visitor().Visit(input.Type()))
	tx.EmitContext().SetOriginal(replacement, input)
	return replacement
}
func (tx *DeclarationTransformer) transformJSDocOptionalType(input ast.Handle) ast.Handle {
	replacement := tx.Factory().NewUnionTypeNode(tx.Factory().NewList([]ast.Handle{tx.Visitor().Visit(input.Type()), tx.Factory().NewKeywordTypeNode(ast.KindUndefinedKeyword)}))
	tx.EmitContext().SetOriginal(replacement, input)
	return replacement
}
func (tx *DeclarationTransformer) getNameExpressionPreferringIdentifier(nameExpr ast.Handle) ast.Handle {
	if ast.IsNumericLiteral(nameExpr) {
		nameExpr = tx.Factory().NewStringLiteral(nameExpr.Text(), ast.TokenFlagsNone)
	}
	if ast.IsStringLiteralLike(nameExpr) && scanner.IsIdentifierText(nameExpr.Text(), core.LanguageVariantStandard) {
		result := tx.Factory().NewIdentifier(nameExpr.Text())
		kwKind := scanner.IdentifierToKeywordKind(result)
		if kwKind == ast.KindUnknown || kwKind == ast.KindDefaultKeyword {
			result.SetParent(nameExpr.Parent())
			result.SetFlags(result.Flags() &^ ast.NodeFlagsSynthesized)
			return result
		}
	}
	return nameExpr
}
func isNotDeclareModifier(mod ast.Handle) bool {
	return mod.Kind != ast.KindDeclareKeyword
}
func (tx *DeclarationTransformer) stripDeclareModifiers(node ast.Handle) ast.Handle {
	if node.IsNil() {
		return ast.Handle{}
	}
	mods := node.Modifiers()
	if mods != 0 {
		flags := node.ModifierFlags()
		if flags&ast.ModifierFlagsAmbient != 0 {
			var filtered []ast.Handle
			for _, m := range node.Store().ListSlice(mods).All() {
				if isNotDeclareModifier(m) {
					filtered = append(filtered, m)
				}
			}
			node.SetModifiers(tx.Factory().NewModifierList(filtered))
		}
	}
	return node
}
func (tx *DeclarationTransformer) visitCJSExportAssignments(expression ast.Handle) ast.Handle {
	if !expression.IsNil() {
		_, cleanupDiagnosticContext := tx.setupDiagnosticContext(expression)
		defer cleanupDiagnosticContext()
		switch ast.GetAssignmentDeclarationKind(expression) {
		case ast.JSDeclarationKindModuleExports:
			if !tx.state.currentSourceFile.CommonJSModuleIndicator.IsNil() {
				result := tx.transformExportAssignment(expression.Parent(), expression, expression.BinaryExpressionRight(), true)
				if !result.IsNil() {
					tx.cjsExportAssignment = result
					tx.resultHasScopeMarker = true
					tx.resultHasExternalModuleIndicator = true
				}
			}
		}
		return tx.cjsExportAssignmentVisitor.VisitEachChild(expression)
	}
	return ast.Handle{}
}
func (tx *DeclarationTransformer) visitNestedExpression(expression ast.Handle) ast.Handle {
	if !expression.IsNil() {
		_, cleanupDiagnosticContext := tx.setupDiagnosticContext(expression)
		defer cleanupDiagnosticContext()
		switch ast.GetAssignmentDeclarationKind(expression) {
		case ast.JSDeclarationKindProperty:
			tx.transformExpandoAssignment(expression)
		case ast.JSDeclarationKindExportsProperty:
			if !tx.state.currentSourceFile.CommonJSModuleIndicator.IsNil() {
				result := tx.transformCommonJSExport(expression, tx.getNameExpressionPreferringIdentifier(ast.GetElementOrPropertyAccessName(expression.BinaryExpressionLeft())))
				if !result.IsNil() {
					tx.cjsExportMembers = append(tx.cjsExportMembers, result)
				}
			}
		case ast.JSDeclarationKindObjectDefinePropertyExports:
			if !tx.state.currentSourceFile.CommonJSModuleIndicator.IsNil() {
				result := tx.transformCommonJSExport(expression, tx.getNameExpressionPreferringIdentifier(expression.Arguments()[1]))
				if !result.IsNil() {
					tx.cjsExportMembers = append(tx.cjsExportMembers, result)
				}
			}
		}
		return tx.expressionVisitor.VisitEachChild(expression)
	}
	return ast.Handle{}
}
func (tx *DeclarationTransformer) transformExpandoAssignment(node ast.Handle) {
	left := node.Left()
	symbol := node.Symbol()
	if symbol == nil || symbol.Flags&ast.SymbolFlagsAssignment == 0 {
		return
	}
	ns := ast.GetLeftmostAccessExpression(left)
	if ns.IsNil() || ns.Kind != ast.KindIdentifier {
		return
	}
	declaration := tx.resolver.GetReferencedValueDeclaration(ns)
	if declaration.IsNil() {
		return
	}
	if tx.shouldStripInternal(declaration) {
		return
	}
	if ast.IsVariableDeclaration(declaration) && !declaration.Type().IsNil() {
		return
	}
	if ast.IsFunctionDeclaration(declaration) && !declaration.FullSignature().IsNil() {
		return
	}
	if ast.IsVariableDeclaration(declaration) && !ast.IsFunctionLike(declaration.Initializer()) {
		return
	}
	host := declaration.Symbol()
	if host == nil {
		return
	}
	name := tx.Factory().NewIdentifier(ns.Text())
	property := tx.tryGetPropertyName(left)
	if property == "" || !scanner.IsIdentifierText(property, core.LanguageVariantStandard) {
		return
	}
	hostId := tx.getExpandoHostId(declaration)
	if ast.IsDeclaration(declaration) && isDeclarationAndNotVisible(tx.EmitContext(), tx.resolver, declaration) {
		tx.deferredExpandoAssignments[hostId] = append(tx.deferredExpandoAssignments[hostId], node)
		return
	}
	if ast.IsFunctionDeclaration(declaration) && !shouldEmitFunctionProperties(declaration) {
		return
	}
	tx.transformExpandoHost(name, declaration)
	exportName := tx.Factory().NewIdentifier(property)
	localName := tx.tryGetNameOfAssignedExpression(node)
	if localName.IsNil() && !tx.resolver.IsNameResolvable(tx.enclosingDeclaration, property) && !ast.IsNonContextualKeyword(scanner.StringToToken(exportName.Text())) {
		localName = exportName
	}
	if localName.IsNil() || ast.IsNonContextualKeyword(scanner.StringToToken(localName.Text())) {
		localName = tx.Factory().NewGeneratedNameForNode(node)
	}
	_, cleanupDiagnosticContext := tx.setupDiagnosticContext(node)
	defer cleanupDiagnosticContext()
	if ast.IsIdentifier(node.Right()) {
		result := tx.transformBinaryExpressionToExportDeclaration(node, exportName)
		tx.expandoMembers[hostId] = append(tx.expandoMembers[hostId], result)
		return
	}
	preexistingExpandoHasExport := core.Some(tx.expandoMembers[hostId], ast.IsExportDeclaration)
	var varModifiers ast.ListRef
	if preexistingExpandoHasExport {
		varModifiers = tx.Factory().NewModifierList(ast.CreateModifiersFromModifierFlags(ast.ModifierFlagsExport, tx.Factory().NewModifier))
	}
	synthesizedNamespace := tx.Factory().NewModuleDeclaration(0, ast.KindNamespaceKeyword, name, tx.Factory().NewModuleBlock(tx.Factory().NewList([]ast.Handle{})))
	synthesizedNamespace.SetParent(tx.enclosingDeclaration)
	synthesizedNamespace.SetSymbol(host)
	locals := make(ast.SymbolTable)
	locals[localName.Text()] = symbol
	synthesizedNamespace.SetLocals(locals)
	oldEnclosing := tx.enclosingDeclaration
	tx.enclosingDeclaration = synthesizedNamespace
	defer func() {
		tx.enclosingDeclaration = oldEnclosing
	}()
	statements := []ast.Handle{tx.Factory().NewVariableStatement(varModifiers, tx.Factory().NewVariableDeclarationList(tx.Factory().NewList([]ast.Handle{tx.Factory().NewVariableDeclaration(localName, ast.Handle{}, tx.ensureType(node, false), ast.Handle{})}), ast.NodeFlagsNone))}
	if localName.Text() != exportName.Text() {
		namedExports := tx.Factory().NewNamedExports(tx.Factory().NewList([]ast.Handle{tx.Factory().NewExportSpecifier(false, localName, exportName)}))
		statements = append(statements, tx.Factory().NewExportDeclaration(0, false, namedExports, ast.Handle{}, ast.Handle{}))
	}
	if len(statements) > 1 && !preexistingExpandoHasExport {
		for _, decl := range tx.expandoMembers[hostId] {
			modifierFlags := ast.ModifierFlagsExport | ast.GetCombinedModifierFlags(decl)
			decl.SetModifiers(tx.Factory().NewModifierList(ast.CreateModifiersFromModifierFlags(modifierFlags, tx.Factory().NewModifier)))
		}
	}
	tx.expandoMembers[hostId] = append(tx.expandoMembers[hostId], statements...)
}
func (tx *DeclarationTransformer) getExpandoHostId(declaration ast.Handle) ast.GlobalRef {
	root := core.IfElse(ast.IsVariableDeclaration(declaration), declaration.Parent().Parent(), declaration)
	id := tx.EmitContext().NodeIdentity(tx.EmitContext().MostOriginal(root))
	return id
}
func (tx *DeclarationTransformer) transformExpandoHost(name ast.Handle, declaration ast.Handle) {
	root := core.IfElse(ast.IsVariableDeclaration(declaration), declaration.Parent().Parent(), declaration)
	id := tx.getExpandoHostId(declaration)
	if _, ok := tx.expandoHosts[id]; ok {
		return
	}
	saveNeedsDeclare := tx.needsDeclare
	tx.needsDeclare = true
	modifierFlags := tx.ensureModifierFlags(root)
	defaultExport := modifierFlags&ast.ModifierFlagsExport != 0 && modifierFlags&ast.ModifierFlagsDefault != 0
	tx.needsDeclare = saveNeedsDeclare
	if defaultExport {
		modifierFlags |= ast.ModifierFlagsAmbient
		modifierFlags ^= ast.ModifierFlagsDefault
		modifierFlags ^= ast.ModifierFlagsExport
	}
	_, cleanupDiagnosticContext := tx.setupDiagnosticContext(declaration)
	defer cleanupDiagnosticContext()
	modifiers := tx.Factory().NewModifierList(ast.CreateModifiersFromModifierFlags(modifierFlags, tx.Factory().NewModifier))
	replacement := make([]ast.Handle, 0)
	if ast.IsFunctionDeclaration(declaration) {
		typeParameters, parameters, asteriskToken := extractExpandoHostParams(declaration)
		replacement = append(replacement, tx.Factory().UpdateFunctionDeclaration(declaration, modifiers, asteriskToken, declaration.Name(), tx.ensureTypeParams(declaration, typeParameters), tx.updateParamList(declaration, parameters), tx.ensureType(declaration, false), ast.Handle{}, ast.Handle{}))
	} else if ast.IsVariableDeclaration(declaration) && ast.IsFunctionExpressionOrArrowFunction(declaration.Initializer()) {
		fn := declaration.Initializer()
		typeParameters, parameters, asteriskToken := extractExpandoHostParams(fn)
		replacement = append(replacement, tx.Factory().NewFunctionDeclaration(modifiers, asteriskToken, tx.Factory().NewIdentifier(name.Text()), tx.ensureTypeParams(fn, typeParameters), tx.updateParamList(fn, parameters), tx.ensureType(fn, false), ast.Handle{}, ast.Handle{}))
	} else {
		tx.expandoHosts[id] = tx.transformTopLevelDeclaration(declaration)
		return
	}
	tx.state.reportExpandoFunctionErrors(declaration)
	if defaultExport {
		if ast.IsSourceFile(declaration.Parent()) {
			tx.resultHasExternalModuleIndicator = true
		}
		tx.resultHasScopeMarker = true
		replacement = append(replacement, tx.Factory().NewExportAssignment(0, false, ast.Handle{}, name))
	}
	tx.expandoHosts[id] = tx.Factory().NewSyntaxList(tx.Factory().NewList(replacement))
	if _, ok := tx.lateStatementReplacementMap[id]; ok {
		tx.lateStatementReplacementMap[id] = tx.createFullExpandoBlock(id)
	}
}
func (tx *DeclarationTransformer) createFullExpandoBlock(id ast.GlobalRef) ast.Handle {
	if deferred, ok := tx.deferredExpandoAssignments[id]; ok {
		delete(tx.deferredExpandoAssignments, id)
		for _, assignment := range deferred {
			tx.transformExpandoAssignment(assignment)
		}
	}
	n := tx.expandoHosts[id]
	if addOns, ok := tx.expandoMembers[id]; ok {
		var modifiers ast.ListRef
		var name ast.Handle
		var host []ast.Handle
		if !n.IsNil() && n.Kind == ast.KindSyntaxList {
			for _, c := range n.Store().ListSlice(n.SyntaxListChildren()).All() {
				if !c.Name().IsNil() {
					name = tx.Factory().DeepCloneNode(c.Name())
					if c.Modifiers() != 0 {
						modifiers = tx.Factory().NewModifierList(c.Store().ListSlice(c.Modifiers()).Slice())
					}
					break
				}
			}
			host = n.Store().ListSlice(n.SyntaxListChildren()).Slice()
		} else if !n.IsNil() {
			name = tx.Factory().DeepCloneNode(n.Name())
			if n.Modifiers() != 0 {
				modifiers = tx.Factory().NewModifierList(n.Store().ListSlice(n.Modifiers()).Slice())
			}
			host = []ast.Handle{n}
		}
		if !name.IsNil() {
			moduleDecl := tx.Factory().NewModuleDeclaration(modifiers, ast.KindNamespaceKeyword, name, tx.Factory().NewModuleBlock(tx.Factory().NewList(addOns)))
			members := append(host, moduleDecl)
			return tx.Factory().NewSyntaxList(tx.Factory().NewList(members))
		}
	}
	return n
}
func extractExpandoHostParams(node ast.Handle) (typeParameters ast.ListRef, parameters ast.ListRef, asteriskToken ast.Handle) {
	switch node.Kind {
	case ast.KindFunctionExpression:
		fn := node
		return fn.TypeParameterList(), fn.ParameterList(), fn.AsteriskToken()
	case ast.KindArrowFunction:
		fn := node
		return fn.TypeParameterList(), fn.ParameterList(), fn.AsteriskToken()
	default:
		fn := node
		return fn.TypeParameterList(), fn.ParameterList(), fn.AsteriskToken()
	}
}
func (tx *DeclarationTransformer) tryGetPropertyName(node ast.Handle) string {
	if ast.IsElementAccessExpression(node) {
		return tx.resolver.GetElementAccessExpressionName(node)
	}
	if ast.IsPropertyAccessExpression(node) {
		return node.Name().Text()
	}
	return ""
}
