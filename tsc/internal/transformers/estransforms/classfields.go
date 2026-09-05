package estransforms

import (
	"github.com/microsoft/TypeScript/tsc/internal/ast"
	"github.com/microsoft/TypeScript/tsc/internal/binder"
	"github.com/microsoft/TypeScript/tsc/internal/collections"
	"github.com/microsoft/TypeScript/tsc/internal/core"
	"github.com/microsoft/TypeScript/tsc/internal/debug"
	"github.com/microsoft/TypeScript/tsc/internal/printer"
	"github.com/microsoft/TypeScript/tsc/internal/scanner"
	"github.com/microsoft/TypeScript/tsc/internal/transformers"
	"iter"
)

type classFacts int

const (
	classFactsNone                                       classFacts = 0
	classFactsClassWasDecorated                          classFacts = 1 << 0
	classFactsNeedsClassConstructorReference             classFacts = 1 << 1
	classFactsNeedsClassSuperReference                   classFacts = 1 << 2
	classFactsNeedsSubstitutionForThisInClassStaticField classFacts = 1 << 3
	classFactsWillHoistInitializersToConstructor         classFacts = 1 << 4
)

type privateIdentifierInfo struct {
	kind                 printer.PrivateIdentifierKind
	brandCheckIdentifier ast.Handle
	isStatic             bool
	isValid              bool
	variableName         ast.Handle
	methodName           ast.Handle
	getterName           ast.Handle
	setterName           ast.Handle
}

type privateEnvironmentData struct {
	className   ast.Handle
	weakSetName ast.Handle
}

type privateEnvironment struct {
	data                 privateEnvironmentData
	members              map[string]*privateIdentifierInfo
	generatedIdentifiers map[ast.Handle]*privateIdentifierInfo
}

type classLexicalEnvironment struct {
	facts               classFacts
	classConstructor    ast.Handle
	classThis           ast.Handle
	superClassReference ast.Handle
}

type classLexicalEnv struct {
	previous   *classLexicalEnv
	data       *classLexicalEnvironment
	privateEnv *privateEnvironment
}
type classFieldsTransformer struct {
	transformers.Transformer
	compilerOptions                                   *core.CompilerOptions
	resolver                                          binder.ReferenceResolver
	shouldTransformInitializersUsingSet               bool
	shouldTransformInitializersUsingDefine            bool
	shouldTransformInitializers                       bool
	shouldTransformPrivateElementsOrClassStaticBlocks bool
	shouldTransformAutoAccessors                      bool
	shouldTransformThisInStaticInitializers           bool
	shouldTransformSuperInStaticInitializers          bool
	shouldTransformPrivateStaticElementsInFile        bool
	legacyDecorators                                  bool
	pendingExpressions                                []ast.Handle
	pendingStatements                                 []ast.Handle
	lexicalEnvironment                                *classLexicalEnv
	currentClassContainer                             ast.Handle
	currentClassElement                               ast.Handle
	classAliases                                      map[ast.Handle]ast.Handle
	enclosingClassDeclarations                        collections.Set[ast.Handle]
	inIterationStatement                              bool
	insideComputedPropertyName                        bool
	parentNode                                        ast.Handle
	currentNode                                       ast.Handle
	modifierVisitor                                   *ast.HandleVisitor
	discardedValueVisitor                             *ast.HandleVisitor
	heritageClauseVisitor                             *ast.HandleVisitor
	assignmentTargetVisitor                           *ast.HandleVisitor
	classElementVisitor                               *ast.HandleVisitor
	accessorFieldResultVisitor                        *ast.HandleVisitor
	arrayAssignmentElementVisitor                     *ast.HandleVisitor
	objectAssignmentElementVisitor                    *ast.HandleVisitor
	substitutionVisitor                               *ast.HandleVisitor
	isAnonymousClassNeedingAssignedName               func(ast.Handle) bool
}

func newClassFieldsTransformer(opts *transformers.TransformOptions) *transformers.Transformer {
	languageVersion := opts.CompilerOptions.GetEmitScriptTarget()
	useDefineForClassFields := opts.CompilerOptions.GetUseDefineForClassFields()
	if languageVersion >= core.ScriptTargetESNext && useDefineForClassFields {
		return nil
	}
	tx := &classFieldsTransformer{compilerOptions: opts.CompilerOptions, resolver: opts.Resolver, legacyDecorators: opts.CompilerOptions.ExperimentalDecorators.IsTrue()}
	tx.shouldTransformInitializersUsingSet = !useDefineForClassFields
	tx.shouldTransformInitializersUsingDefine = useDefineForClassFields && languageVersion < core.ScriptTargetES2022
	tx.shouldTransformInitializers = tx.shouldTransformInitializersUsingSet || tx.shouldTransformInitializersUsingDefine
	tx.shouldTransformPrivateElementsOrClassStaticBlocks = languageVersion < core.ScriptTargetES2022
	tx.shouldTransformAutoAccessors = languageVersion < core.ScriptTargetESNext
	tx.shouldTransformThisInStaticInitializers = languageVersion < core.ScriptTargetES2022
	tx.shouldTransformSuperInStaticInitializers = tx.shouldTransformThisInStaticInitializers
	result := tx.NewTransformer(tx.visit, opts.Context)
	tx.modifierVisitor = tx.EmitContext().NewNodeVisitor(tx.visitModifier)
	tx.discardedValueVisitor = tx.EmitContext().NewNodeVisitor(tx.visitDiscardedValue)
	tx.heritageClauseVisitor = tx.EmitContext().NewNodeVisitor(tx.visitHeritageClause)
	tx.assignmentTargetVisitor = tx.EmitContext().NewNodeVisitor(tx.visitAssignmentTarget)
	tx.classElementVisitor = tx.EmitContext().NewNodeVisitor(tx.visitClassElement)
	tx.accessorFieldResultVisitor = tx.EmitContext().NewNodeVisitor(tx.visitAccessorFieldResult)
	tx.arrayAssignmentElementVisitor = tx.EmitContext().NewNodeVisitor(tx.visitArrayAssignmentElement)
	tx.objectAssignmentElementVisitor = tx.EmitContext().NewNodeVisitor(tx.visitObjectAssignmentElement)
	tx.substitutionVisitor = tx.EmitContext().NewNodeVisitor(tx.visitForSubstitution)
	tx.isAnonymousClassNeedingAssignedName = tx.isAnonymousClassNeedingAssignedNameWorker
	return result
}

func (tx *classFieldsTransformer) requiresBlockScopedVar() bool {
	return tx.inIterationStatement && !tx.currentClassContainer.IsNil() && ast.IsClassExpression(tx.currentClassContainer)
}

func (tx *classFieldsTransformer) classExpressionNeedsBlockScopedTemp() bool {
	if !tx.requiresBlockScopedVar() {
		return false
	}
	for _, member := range tx.currentClassContainer.Members() {
		if ast.IsPropertyDeclaration(member) && !ast.HasStaticModifier(member) && !member.Name().IsNil() && ast.IsComputedPropertyName(member.Name()) {
			return true
		}
	}
	return false
}
func (tx *classFieldsTransformer) visitSourceFile(node ast.Handle) ast.Handle {
	if ast.GetSourceFileOfNode(node) != nil && ast.GetSourceFileOfNode(node).IsDeclarationFile {
		return node
	}
	tx.lexicalEnvironment = nil
	tx.shouldTransformPrivateStaticElementsInFile = tx.EmitContext().EmitFlags(node)&printer.EFTransformPrivateStaticElements != 0
	tx.classAliases = make(map[ast.Handle]ast.Handle)
	tx.enclosingClassDeclarations.Clear()
	visited := tx.Visitor().VisitEachChild(node)
	tx.EmitContext().AddEmitHelper(visited, tx.EmitContext().ReadEmitHelpers()...)
	tx.classAliases = nil
	tx.enclosingClassDeclarations.Clear()
	return visited
}
func (tx *classFieldsTransformer) visitModifier(node ast.Handle) ast.Handle {
	if node.Kind == ast.KindAccessorKeyword {
		if tx.shouldTransformAutoAccessorsInCurrentClass() {
			return ast.Handle{}
		}
		return node
	}
	if ast.IsModifier(node) {
		return node
	}
	return ast.Handle{}
}
func (tx *classFieldsTransformer) pushNode(node ast.Handle) (grandparentNode ast.Handle) {
	grandparentNode = tx.parentNode
	tx.parentNode = tx.currentNode
	tx.currentNode = node
	return grandparentNode
}
func (tx *classFieldsTransformer) popNode(grandparentNode ast.Handle) {
	tx.currentNode = tx.parentNode
	tx.parentNode = grandparentNode
}

func (tx *classFieldsTransformer) visitForSubstitution(node ast.Handle) ast.Handle {
	if node.Kind == ast.KindIdentifier {
		return tx.visitIdentifier(node)
	}
	if node.Kind == ast.KindPropertyAccessExpression && ast.IsIdentifier(node.PropertyAccessExpressionName()) {
		return tx.visitPropertyAccessExpressionForSubstitution(node)
	}
	return tx.substitutionVisitor.VisitEachChild(node)
}

func (tx *classFieldsTransformer) visit(node ast.Handle) ast.Handle {
	grandparentNode := tx.pushNode(node)
	defer tx.popNode(grandparentNode)
	if node.SubtreeFacts()&(ast.SubtreeContainsClassFields|ast.SubtreeContainsLexicalThisOrSuper) == 0 {
		if !tx.currentClassContainer.IsNil() && len(tx.classAliases) > 0 {
			return tx.visitForSubstitution(node)
		}
		return node
	}
	switch node.Kind {
	case ast.KindSourceFile:
		return tx.visitSourceFile(node)
	case ast.KindClassDeclaration:
		return tx.visitClassDeclaration(node)
	case ast.KindClassExpression:
		return tx.visitClassExpression(node)
	case ast.KindClassStaticBlockDeclaration, ast.KindPropertyDeclaration:
		panic("Use `classElementVisitor` instead.")
	case ast.KindPropertyAssignment:
		return tx.visitPropertyAssignment(node)
	case ast.KindVariableStatement:
		return tx.visitVariableStatement(node)
	case ast.KindVariableDeclaration:
		return tx.visitVariableDeclaration(node)
	case ast.KindParameter:
		return tx.visitParameterDeclaration(node)
	case ast.KindBindingElement:
		return tx.visitBindingElement(node)
	case ast.KindExportAssignment:
		return tx.visitExportAssignment(node)
	case ast.KindPrivateIdentifier:
		return tx.visitPrivateIdentifier(node)
	case ast.KindPropertyAccessExpression:
		return tx.visitPropertyAccessExpression(node)
	case ast.KindElementAccessExpression:
		return tx.visitElementAccessExpression(node)
	case ast.KindPrefixUnaryExpression, ast.KindPostfixUnaryExpression:
		return tx.visitPreOrPostfixUnaryExpression(node, false)
	case ast.KindBinaryExpression:
		return tx.visitBinaryExpression(node, false)
	case ast.KindParenthesizedExpression:
		return tx.visitParenthesizedExpression(node, false)
	case ast.KindCallExpression:
		return tx.visitCallExpression(node)
	case ast.KindExpressionStatement:
		return tx.visitExpressionStatement(node)
	case ast.KindTaggedTemplateExpression:
		return tx.visitTaggedTemplateExpression(node)
	case ast.KindForStatement:
		return tx.visitForStatement(node)
	case ast.KindForInStatement, ast.KindForOfStatement, ast.KindDoStatement, ast.KindWhileStatement:
		return tx.setInIterationStatementAnd(true, (*classFieldsTransformer).visitEachChildOfNode, node)
	case ast.KindThisKeyword:
		return tx.visitThisExpression(node)
	case ast.KindFunctionDeclaration, ast.KindFunctionExpression:
		return tx.setInIterationStatementAnd(false, (*classFieldsTransformer).visitFunctionExpressionOrDeclaration, node)
	case ast.KindConstructor, ast.KindMethodDeclaration, ast.KindGetAccessor, ast.KindSetAccessor:
		return tx.setInIterationStatementAnd(false, (*classFieldsTransformer).setClassElementAndVisitEachChild, node)
	default:
		return tx.Visitor().VisitEachChild(node)
	}
}

func (tx *classFieldsTransformer) visitDiscardedValue(node ast.Handle) ast.Handle {
	switch node.Kind {
	case ast.KindPrefixUnaryExpression, ast.KindPostfixUnaryExpression:
		return tx.visitPreOrPostfixUnaryExpression(node, true)
	case ast.KindBinaryExpression:
		return tx.visitBinaryExpression(node, true)
	case ast.KindParenthesizedExpression:
		return tx.visitParenthesizedExpression(node, true)
	default:
		return tx.visit(node)
	}
}

func (tx *classFieldsTransformer) visitHeritageClause(node ast.Handle) ast.Handle {
	switch node.Kind {
	case ast.KindHeritageClause:
		return tx.heritageClauseVisitor.VisitEachChild(node)
	case ast.KindExpressionWithTypeArguments:
		return tx.visitExpressionWithTypeArgumentsInHeritageClause(node)
	default:
		return tx.visit(node)
	}
}

func (tx *classFieldsTransformer) visitAssignmentTarget(node ast.Handle) ast.Handle {
	switch node.Kind {
	case ast.KindObjectLiteralExpression, ast.KindArrayLiteralExpression:
		return tx.visitAssignmentPattern(node)
	default:
		return tx.visit(node)
	}
}
func (tx *classFieldsTransformer) visitDestructuringAssignmentTarget(node ast.Handle) ast.Handle {
	if ast.IsObjectLiteralExpression(node) || ast.IsArrayLiteralExpression(node) {
		return tx.visitAssignmentPattern(node)
	}
	if ast.IsPropertyAccessExpression(node) && ast.IsPrivateIdentifier(node.PropertyAccessExpressionName()) {
		return tx.wrapPrivateIdentifierForDestructuringTarget(node)
	}
	if tx.shouldTransformSuperInStaticInitializers && !tx.currentClassElement.IsNil() && ast.IsSuperProperty(node) && isStaticPropertyDeclarationOrClassStaticBlock(tx.currentClassElement) && tx.lexicalEnvironment != nil && tx.lexicalEnvironment.data != nil {
		data := tx.lexicalEnvironment.data
		if data.facts&classFactsClassWasDecorated != 0 {
			return tx.visitInvalidSuperProperty(node)
		}
		if !data.classConstructor.IsNil() && !data.superClassReference.IsNil() {
			var name ast.Handle
			if ast.IsElementAccessExpression(node) {
				name = tx.Visitor().VisitNode(node.ElementAccessExpressionArgumentExpression())
			} else if ast.IsPropertyAccessExpression(node) && ast.IsIdentifier(node.PropertyAccessExpressionName()) {
				name = tx.Factory().NewStringLiteralFromNode(node.PropertyAccessExpressionName())
			}
			if !name.IsNil() {
				temp := tx.Factory().NewTempVariable()
				setExpr := tx.Factory().NewReflectSetCall(data.superClassReference, name, temp, data.classConstructor)
				return tx.Factory().NewAssignmentTargetWrapper(temp, setExpr)
			}
		}
	}
	return tx.Visitor().VisitEachChild(node)
}

func (tx *classFieldsTransformer) visitClassElement(node ast.Handle) ast.Handle {
	switch node.Kind {
	case ast.KindConstructor:
		return tx.setCurrentClassElementAnd(node, (*classFieldsTransformer).visitConstructorDeclaration, node)
	case ast.KindGetAccessor, ast.KindSetAccessor, ast.KindMethodDeclaration:
		return tx.setCurrentClassElementAnd(node, (*classFieldsTransformer).visitMethodOrAccessorDeclaration, node)
	case ast.KindPropertyDeclaration:
		return tx.setCurrentClassElementAnd(node, (*classFieldsTransformer).visitPropertyDeclaration, node)
	case ast.KindClassStaticBlockDeclaration:
		return tx.setCurrentClassElementAnd(node, (*classFieldsTransformer).visitClassStaticBlockDeclaration, node)
	case ast.KindComputedPropertyName:
		return tx.visitComputedPropertyName(node)
	case ast.KindSemicolonClassElement:
		return node
	default:
		if ast.IsModifierLike(node) {
			return tx.visitModifier(node)
		}
		return tx.visit(node)
	}
}

func (tx *classFieldsTransformer) visitPropertyName(name ast.Handle) ast.Handle {
	if ast.IsComputedPropertyName(name) {
		return tx.visitComputedPropertyName(name)
	}
	return tx.Visitor().VisitNode(name)
}

func (tx *classFieldsTransformer) visitAccessorFieldResult(node ast.Handle) ast.Handle {
	switch node.Kind {
	case ast.KindPropertyDeclaration:
		return tx.transformFieldInitializer(node)
	case ast.KindGetAccessor, ast.KindSetAccessor:
		return tx.visitClassElement(node)
	default:
		debug.FailBadSyntaxKind(node, "Expected node to either be a PropertyDeclaration, GetAccessorDeclaration, or SetAccessorDeclaration")
		return ast.Handle{}
	}
}

func (tx *classFieldsTransformer) visitIdentifier(node ast.Handle) ast.Handle {
	declaration := tx.resolver.GetReferencedValueDeclaration(tx.EmitContext().MostOriginal(node))
	if !declaration.IsNil() {
		if alias, ok := tx.classAliases[declaration]; ok && tx.enclosingClassDeclarations.Has(declaration) {
			clone := tx.Factory().DeepCloneNode(alias)
			tx.EmitContext().SetSourceMapRange(clone, node.Loc())
			tx.EmitContext().SetCommentRange(clone, node.Loc())
			return clone
		}
	}
	return node
}

func (tx *classFieldsTransformer) visitPrivateIdentifier(node ast.Handle) ast.Handle {
	if !tx.shouldTransformPrivateElementsOrClassStaticBlocks {
		return node
	}
	if !tx.parentNode.IsNil() && ast.IsStatement(tx.parentNode) {
		return node
	}
	result := tx.Factory().NewIdentifier("")
	tx.EmitContext().SetOriginal(result, node)
	return result
}

func (tx *classFieldsTransformer) transformPrivateIdentifierInInExpression(node ast.Handle) ast.Handle {
	info := tx.accessPrivateIdentifier(node.Left())
	if info != nil {
		receiver := tx.Visitor().VisitNode(node.Right())
		result := tx.Factory().NewClassPrivateFieldInHelper(info.brandCheckIdentifier, receiver)
		tx.EmitContext().SetOriginal(result, node)
		return result
	}
	return tx.Visitor().VisitEachChild(node)
}
func (tx *classFieldsTransformer) visitPropertyAssignment(node ast.Handle) ast.Handle {
	if isNamedEvaluationAnd(tx.EmitContext(), node, tx.isAnonymousClassNeedingAssignedName) {
		node = transformNamedEvaluation(tx.EmitContext(), node, false, "")
	}
	return tx.Visitor().VisitEachChild(node)
}
func (tx *classFieldsTransformer) visitVariableStatement(node ast.Handle) ast.Handle {
	savedPendingStatements := tx.pendingStatements
	tx.pendingStatements = nil
	visitedNode := tx.Visitor().VisitEachChild(node)
	if len(tx.pendingStatements) > 0 {
		result := make([]ast.Handle, 0, 1+len(tx.pendingStatements))
		result = append(result, visitedNode)
		result = append(result, tx.pendingStatements...)
		tx.pendingStatements = savedPendingStatements
		return tx.Factory().NewSyntaxList(tx.Factory().NewList(result))
	}
	tx.pendingStatements = savedPendingStatements
	return visitedNode
}
func (tx *classFieldsTransformer) visitVariableDeclaration(node ast.Handle) ast.Handle {
	if isNamedEvaluationAnd(tx.EmitContext(), node, tx.isAnonymousClassNeedingAssignedName) {
		node = transformNamedEvaluation(tx.EmitContext(), node, false, "")
	}
	return tx.Visitor().VisitEachChild(node)
}
func (tx *classFieldsTransformer) visitParameterDeclaration(node ast.Handle) ast.Handle {
	if isNamedEvaluationAnd(tx.EmitContext(), node, tx.isAnonymousClassNeedingAssignedName) {
		node = transformNamedEvaluation(tx.EmitContext(), node, false, "")
	}
	return tx.Visitor().VisitEachChild(node)
}
func (tx *classFieldsTransformer) visitBindingElement(node ast.Handle) ast.Handle {
	if isNamedEvaluationAnd(tx.EmitContext(), node, tx.isAnonymousClassNeedingAssignedName) {
		node = transformNamedEvaluation(tx.EmitContext(), node, false, "")
	}
	return tx.Visitor().VisitEachChild(node)
}
func (tx *classFieldsTransformer) visitExportAssignment(node ast.Handle) ast.Handle {
	if isNamedEvaluationAnd(tx.EmitContext(), node, tx.isAnonymousClassNeedingAssignedName) {
		assignedName := ""
		if !node.IsExportEquals() {
			assignedName = "default"
		}
		node = transformNamedEvaluation(tx.EmitContext(), node, true, assignedName)
	}
	return tx.Visitor().VisitEachChild(node)
}
func (tx *classFieldsTransformer) injectPendingExpressions(expression ast.Handle) ast.Handle {
	if len(tx.pendingExpressions) > 0 {
		if ast.IsParenthesizedExpression(expression) {
			tx.pendingExpressions = append(tx.pendingExpressions, expression.Expression())
			expression = tx.Factory().UpdateParenthesizedExpression(expression, tx.Factory().InlineExpressions(tx.pendingExpressions))
		} else {
			exprs := append(tx.pendingExpressions, expression)
			expression = tx.Factory().InlineExpressions(exprs)
		}
		tx.pendingExpressions = nil
	}
	return expression
}
func (tx *classFieldsTransformer) visitComputedPropertyName(node ast.Handle) ast.Handle {
	savedLexicalEnvironment := tx.lexicalEnvironment
	savedInsideComputedPropertyName := tx.insideComputedPropertyName
	tx.insideComputedPropertyName = true
	if tx.lexicalEnvironment != nil && tx.lexicalEnvironment.previous != nil {
		tx.lexicalEnvironment = tx.lexicalEnvironment.previous
	}
	expression := tx.Visitor().VisitNode(node.Expression())
	tx.lexicalEnvironment = savedLexicalEnvironment
	tx.insideComputedPropertyName = savedInsideComputedPropertyName
	return tx.Factory().UpdateComputedPropertyName(node, tx.injectPendingExpressions(expression))
}
func (tx *classFieldsTransformer) visitConstructorDeclaration(node ast.Handle) ast.Handle {
	if !tx.currentClassContainer.IsNil() {
		return tx.transformConstructor(node, tx.currentClassContainer)
	}
	return tx.Visitor().VisitEachChild(node)
}
func (tx *classFieldsTransformer) shouldTransformClassElementToWeakMap(node ast.Handle) bool {
	if tx.shouldTransformPrivateElementsOrClassStaticBlocks {
		return true
	}
	return tx.shouldAlwaysTransformPrivateStaticElements(node)
}
func (tx *classFieldsTransformer) shouldAlwaysTransformPrivateStaticElements(node ast.Handle) bool {
	return ast.HasStaticModifier(node) && tx.EmitContext().EmitFlags(node)&printer.EFTransformPrivateStaticElements != 0
}

func (tx *classFieldsTransformer) nodeHasTransformPrivateStaticElementsFlag(node ast.Handle) bool {
	return tx.EmitContext().EmitFlags(node)&printer.EFTransformPrivateStaticElements != 0
}
func (tx *classFieldsTransformer) visitMethodOrAccessorDeclaration(node ast.Handle) ast.Handle {
	debug.Assert(!ast.HasDecorators(node))
	if !ast.IsPrivateIdentifierClassElementDeclaration(node) || !tx.shouldTransformClassElementToWeakMap(node) {
		return tx.classElementVisitor.VisitEachChild(node)
	}
	info := tx.accessPrivateIdentifier(node.Name())
	debug.Assert(info != nil, "Undeclared private name for property declaration.")
	if !info.isValid {
		return node
	}
	functionName := tx.getHoistedFunctionName(node)
	if !functionName.IsNil() {
		modifiers := tx.extractNonStaticNonAccessorModifiers(node)
		tx.EmitContext().StartVariableEnvironment()
		saved := tx.inIterationStatement
		tx.inIterationStatement = false
		body := tx.EmitContext().VisitFunctionBody(node.Body(), tx.Visitor())
		params := tx.Visitor().VisitNodes(node.ParameterList())
		tx.inIterationStatement = saved
		funcExpr := tx.Factory().NewFunctionExpression(modifiers, node.AsteriskToken(), functionName, 0, params, ast.Handle{}, ast.Handle{}, body)
		assignment := tx.Factory().NewAssignmentExpression(functionName, funcExpr)
		tx.addPendingExpressions(assignment)
	}
	return ast.Handle{}
}
func (tx *classFieldsTransformer) extractNonStaticNonAccessorModifiers(node ast.Handle) ast.ListRef {
	return transformers.ExtractModifiers(tx.EmitContext(), node.Modifiers(), ^(ast.ModifierFlagsStatic | ast.ModifierFlagsAccessor))
}
func (tx *classFieldsTransformer) setCurrentClassElementAnd(classElement ast.Handle, visitor func(tx *classFieldsTransformer, node ast.Handle) ast.Handle, node ast.Handle) ast.Handle {
	if classElement != tx.currentClassElement {
		saved := tx.currentClassElement
		tx.currentClassElement = classElement
		result := visitor(tx, node)
		tx.currentClassElement = saved
		return result
	}
	return visitor(tx, node)
}

func (tx *classFieldsTransformer) visitEachChildOfNode(node ast.Handle) ast.Handle {
	return tx.Visitor().VisitEachChild(node)
}
func (tx *classFieldsTransformer) setInIterationStatementAnd(inIteration bool, visitor func(tx *classFieldsTransformer, node ast.Handle) ast.Handle, node ast.Handle) ast.Handle {
	if tx.inIterationStatement != inIteration {
		saved := tx.inIterationStatement
		tx.inIterationStatement = inIteration
		result := visitor(tx, node)
		tx.inIterationStatement = saved
		return result
	}
	return visitor(tx, node)
}
func (tx *classFieldsTransformer) clearClassElementAndVisitEachChild(node ast.Handle) ast.Handle {
	return tx.setCurrentClassElementAnd(ast.Handle{}, (*classFieldsTransformer).visitEachChildOfNode, node)
}

func (tx *classFieldsTransformer) visitFunctionExpressionOrDeclaration(node ast.Handle) ast.Handle {
	if !tx.currentClassElement.IsNil() {
		original := tx.EmitContext().MostOriginal(node)
		if original != node && !tx.currentClassContainer.IsNil() {
			for _, member := range tx.currentClassContainer.Members() {
				if tx.EmitContext().MostOriginal(member) == original && ast.IsStatic(member) {
					return tx.visitEachChildOfNode(node)
				}
			}
		}
	}
	return tx.setCurrentClassElementAnd(ast.Handle{}, (*classFieldsTransformer).visitEachChildOfNode, node)
}
func (tx *classFieldsTransformer) setClassElementAndVisitEachChild(node ast.Handle) ast.Handle {
	return tx.setCurrentClassElementAnd(node, (*classFieldsTransformer).visitEachChildOfNode, node)
}
func (tx *classFieldsTransformer) getHoistedFunctionName(node ast.Handle) ast.Handle {
	debug.Assert(!node.Name().IsNil() && ast.IsPrivateIdentifier(node.Name()))
	info := tx.accessPrivateIdentifier(node.Name())
	debug.Assert(info != nil, "Undeclared private name for property declaration.")
	if info.kind == printer.PrivateIdentifierKindMethod {
		return info.methodName
	}
	if info.kind == printer.PrivateIdentifierKindAccessor {
		if ast.IsGetAccessorDeclaration(node) {
			return info.getterName
		}
		if ast.IsSetAccessorDeclaration(node) {
			return info.setterName
		}
	}
	return ast.Handle{}
}
func (tx *classFieldsTransformer) tryGetClassThis() ast.Handle {
	if classThis := tx.tryGetClassThisNoContainer(); !classThis.IsNil() {
		return classThis
	}
	if !tx.currentClassContainer.IsNil() {
		return tx.currentClassContainer.Name()
	}
	return ast.Handle{}
}
func (tx *classFieldsTransformer) tryGetClassThisNoContainer() ast.Handle {
	lex := tx.getClassLexicalEnvironment()
	if !lex.classThis.IsNil() {
		return lex.classThis
	}
	if !lex.classConstructor.IsNil() {
		return lex.classConstructor
	}
	return ast.Handle{}
}

func (tx *classFieldsTransformer) transformAutoAccessor(node ast.Handle) ast.Handle {
	commentRange := tx.EmitContext().CommentRange(node)
	sourceMapRange := tx.EmitContext().SourceMapRange(node)
	name := node.Name()
	getterName := name
	setterName := name
	if ast.IsComputedPropertyName(name) && !transformers.IsSimpleInlineableExpression(name.Expression()) {
		cacheAssignment := findComputedPropertyNameCacheAssignment(tx.EmitContext(), name)
		if !cacheAssignment.IsNil() {
			getterName = tx.Factory().UpdateComputedPropertyName(name, tx.Visitor().VisitNode(name.Expression()))
			setterName = tx.Factory().UpdateComputedPropertyName(name, cacheAssignment.Left())
		} else {
			temp := tx.Factory().NewTempVariable()
			tx.EmitContext().SetSourceMapRange(temp, name.Expression().Loc())
			tx.EmitContext().AddVariableDeclaration(temp)
			expression := tx.Visitor().VisitNode(name.Expression())
			assignment := tx.Factory().NewAssignmentExpression(temp, expression)
			tx.EmitContext().SetSourceMapRange(assignment, name.Expression().Loc())
			getterName = tx.Factory().UpdateComputedPropertyName(name, assignment)
			setterName = tx.Factory().UpdateComputedPropertyName(name, temp)
		}
	}
	modifiers := tx.modifierVisitor.VisitModifiers(node.Modifiers())
	backingField := createAccessorPropertyBackingField(tx.Factory(), node, modifiers, node.Initializer())
	tx.EmitContext().SetOriginal(backingField, node)
	tx.EmitContext().AddEmitFlags(backingField, printer.EFNoComments)
	tx.EmitContext().SetSourceMapRange(backingField, sourceMapRange)
	var receiver ast.Handle
	if ast.IsStatic(node) {
		receiver = tx.tryGetClassThis()
		if receiver.IsNil() {
			receiver = tx.Factory().NewThisExpression()
		}
	} else {
		receiver = tx.Factory().NewThisExpression()
	}
	getter := tx.createAccessorPropertyGetRedirector(node, modifiers, getterName, receiver)
	tx.EmitContext().SetOriginal(getter, node)
	tx.EmitContext().SetCommentRange(getter, commentRange)
	tx.EmitContext().SetSourceMapRange(getter, sourceMapRange)
	setterModifiers := modifiers
	setter := tx.createAccessorPropertySetRedirector(node, setterModifiers, setterName, receiver)
	tx.EmitContext().SetOriginal(setter, node)
	tx.EmitContext().AddEmitFlags(setter, printer.EFNoComments)
	tx.EmitContext().SetSourceMapRange(setter, sourceMapRange)
	visited := tx.accessorFieldResultVisitor.VisitSlice([]ast.Handle{backingField, getter, setter})
	return tx.Factory().NewSyntaxList(tx.Factory().NewList(visited))
}
func (tx *classFieldsTransformer) transformPrivateFieldInitializer(node ast.Handle) ast.Handle {
	if tx.shouldTransformClassElementToWeakMap(node) {
		info := tx.accessPrivateIdentifier(node.Name())
		debug.Assert(info != nil, "Undeclared private name for property declaration.")
		if !info.isValid {
			return node
		}
		if info.isStatic && !tx.shouldTransformPrivateElementsOrClassStaticBlocks {
			statement := tx.transformPropertyOrClassStaticBlock(node, tx.Factory().NewThisExpression())
			if !statement.IsNil() {
				return tx.Factory().NewClassStaticBlockDeclaration(0, tx.Factory().NewBlock(tx.Factory().NewList([]ast.Handle{statement}), true))
			}
		}
		return ast.Handle{}
	}
	if tx.shouldTransformInitializersUsingSet && !ast.HasStaticModifier(node) && tx.lexicalEnvironment != nil && tx.lexicalEnvironment.data != nil && tx.lexicalEnvironment.data.facts&classFactsWillHoistInitializersToConstructor != 0 {
		return tx.Factory().UpdatePropertyDeclaration(node, tx.Visitor().VisitModifiers(node.Modifiers()), node.Name(), ast.Handle{}, ast.Handle{}, ast.Handle{})
	}
	if isNamedEvaluationAnd(tx.EmitContext(), node, tx.isAnonymousClassNeedingAssignedName) {
		node = transformNamedEvaluation(tx.EmitContext(), node, false, "")
	}
	return tx.Factory().UpdatePropertyDeclaration(node, tx.modifierVisitor.VisitModifiers(node.Modifiers()), tx.visitPropertyName(node.Name()), ast.Handle{}, ast.Handle{}, tx.Visitor().VisitNode(node.Initializer()))
}
func (tx *classFieldsTransformer) transformPublicFieldInitializer(node ast.Handle) ast.Handle {
	if tx.shouldTransformInitializers && !ast.IsAutoAccessorPropertyDeclaration(node) {
		expr := tx.getPropertyNameExpressionIfNeeded(node.Name(), !node.Initializer().IsNil() || tx.compilerOptions.GetUseDefineForClassFields())
		if !expr.IsNil() {
			for e := range flattenCommaList(expr) {
				tx.addPendingExpressions(e)
			}
		}
		if ast.IsStatic(node) && !tx.shouldTransformPrivateElementsOrClassStaticBlocks {
			initializerStatement := tx.transformPropertyOrClassStaticBlock(node, tx.Factory().NewThisExpression())
			if !initializerStatement.IsNil() {
				staticBlock := tx.Factory().NewClassStaticBlockDeclaration(0, tx.Factory().NewBlock(tx.Factory().NewList([]ast.Handle{initializerStatement}), false))
				tx.EmitContext().SetOriginal(staticBlock, node)
				tx.EmitContext().SetCommentRange(staticBlock, node.Loc())
				tx.EmitContext().AddEmitFlags(initializerStatement, printer.EFNoComments)
				return staticBlock
			}
		}
		return ast.Handle{}
	}
	return tx.Factory().UpdatePropertyDeclaration(node, tx.modifierVisitor.VisitModifiers(node.Modifiers()), tx.visitPropertyName(node.Name()), ast.Handle{}, ast.Handle{}, tx.Visitor().VisitNode(node.Initializer()))
}
func (tx *classFieldsTransformer) transformFieldInitializer(node ast.Handle) ast.Handle {
	debug.Assert(!ast.HasDecorators(node), "Decorators should already have been transformed and elided.")
	if ast.IsPrivateIdentifierClassElementDeclaration(node) {
		return tx.transformPrivateFieldInitializer(node)
	}
	return tx.transformPublicFieldInitializer(node)
}
func (tx *classFieldsTransformer) shouldTransformAutoAccessorsInCurrentClass() bool {
	if tx.shouldTransformAutoAccessors {
		return true
	}
	return tx.lexicalEnvironment != nil && tx.lexicalEnvironment.data != nil && tx.lexicalEnvironment.data.facts&classFactsWillHoistInitializersToConstructor != 0
}
func (tx *classFieldsTransformer) visitPropertyDeclaration(node ast.Handle) ast.Handle {
	propDecl := node
	if ast.IsAutoAccessorPropertyDeclaration(node) && (tx.shouldTransformAutoAccessorsInCurrentClass() || ast.HasStaticModifier(node) && tx.shouldAlwaysTransformPrivateStaticElements(node)) {
		return tx.transformAutoAccessor(propDecl)
	}
	return tx.transformFieldInitializer(propDecl)
}
func (tx *classFieldsTransformer) createPrivateIdentifierAccess(info *privateIdentifierInfo, receiver ast.Handle) ast.Handle {
	receiver = tx.Visitor().VisitNode(receiver)
	return tx.createPrivateIdentifierAccessHelper(info, receiver)
}
func (tx *classFieldsTransformer) createPrivateIdentifierAccessHelper(info *privateIdentifierInfo, receiver ast.Handle) ast.Handle {
	tx.EmitContext().SetCommentRange(receiver, core.NewTextRange(-1, receiver.End()))
	switch info.kind {
	case printer.PrivateIdentifierKindAccessor:
		return tx.Factory().NewClassPrivateFieldGetHelper(receiver, info.brandCheckIdentifier, info.kind, info.getterName)
	case printer.PrivateIdentifierKindMethod:
		return tx.Factory().NewClassPrivateFieldGetHelper(receiver, info.brandCheckIdentifier, info.kind, info.methodName)
	case printer.PrivateIdentifierKindField:
		var f ast.Handle
		if info.isStatic {
			f = info.variableName
		}
		return tx.Factory().NewClassPrivateFieldGetHelper(receiver, info.brandCheckIdentifier, info.kind, f)
	case printer.PrivateIdentifierKindUntransformed:
		debug.Fail("Access helpers should not be created for untransformed private elements")
		return ast.Handle{}
	}
	debug.AssertNever(info, "Unknown private element type")
	return ast.Handle{}
}
func (tx *classFieldsTransformer) visitPropertyAccessExpression(node ast.Handle) ast.Handle {
	if ast.IsPrivateIdentifier(node.Name()) {
		info := tx.accessPrivateIdentifier(node.Name())
		if info != nil {
			result := tx.createPrivateIdentifierAccess(info, node.Expression())
			tx.EmitContext().SetOriginal(result, node)
			result.SetLoc(node.Loc())
			return result
		}
	}
	if tx.shouldTransformSuperInStaticInitializers && !tx.currentClassElement.IsNil() && ast.IsSuperProperty(node) && ast.IsIdentifier(node.Name()) && isStaticPropertyDeclarationOrClassStaticBlock(tx.currentClassElement) && tx.lexicalEnvironment != nil && tx.lexicalEnvironment.data != nil {
		data := tx.lexicalEnvironment.data
		if data.facts&classFactsClassWasDecorated != 0 {
			return tx.visitInvalidSuperProperty(node)
		}
		if !data.classConstructor.IsNil() && !data.superClassReference.IsNil() {
			superProperty := tx.Factory().NewReflectGetCall(data.superClassReference, tx.Factory().NewStringLiteralFromNode(node.Name()), data.classConstructor)
			tx.EmitContext().SetOriginal(superProperty, node.Expression())
			superProperty.SetLoc(node.Expression().Loc())
			return superProperty
		}
	}
	if ast.IsIdentifier(node.Name()) {
		return tx.visitPropertyAccessExpressionForSubstitution(node)
	}
	return tx.Visitor().VisitEachChild(node)
}

func (tx *classFieldsTransformer) visitPropertyAccessExpressionForSubstitution(node ast.Handle) ast.Handle {
	expression := tx.Visitor().VisitNode(node.Expression())
	if expression != node.Expression() {
		return tx.Factory().UpdatePropertyAccessExpression(node, expression, node.QuestionDotToken(), node.Name(), node.Flags())
	}
	return node
}
func (tx *classFieldsTransformer) visitElementAccessExpression(node ast.Handle) ast.Handle {
	if tx.shouldTransformSuperInStaticInitializers && !tx.currentClassElement.IsNil() && ast.IsSuperProperty(node) && isStaticPropertyDeclarationOrClassStaticBlock(tx.currentClassElement) && tx.lexicalEnvironment != nil && tx.lexicalEnvironment.data != nil {
		data := tx.lexicalEnvironment.data
		if data.facts&classFactsClassWasDecorated != 0 {
			return tx.visitInvalidSuperProperty(node)
		}
		if !data.classConstructor.IsNil() && !data.superClassReference.IsNil() {
			superProperty := tx.Factory().NewReflectGetCall(data.superClassReference, tx.Visitor().VisitNode(node.ArgumentExpression()), data.classConstructor)
			tx.EmitContext().SetOriginal(superProperty, node.Expression())
			superProperty.SetLoc(node.Expression().Loc())
			return superProperty
		}
	}
	return tx.Visitor().VisitEachChild(node)
}
func (tx *classFieldsTransformer) visitPreOrPostfixUnaryExpression(node ast.Handle, discarded bool) ast.Handle {
	var operator ast.Kind
	var operand ast.Handle
	if ast.IsPrefixUnaryExpression(node) {
		operator = node.PrefixUnaryExpressionOperator()
		operand = node.PrefixUnaryExpressionOperand()
	} else {
		operator = node.PostfixUnaryExpressionOperator()
		operand = node.PostfixUnaryExpressionOperand()
	}
	if operator == ast.KindPlusPlusToken || operator == ast.KindMinusMinusToken {
		operandSkipped := ast.SkipParentheses(operand)
		if ast.IsPropertyAccessExpression(operandSkipped) && ast.IsPrivateIdentifier(operandSkipped.Name()) {
			info := tx.accessPrivateIdentifier(operandSkipped.Name())
			if info != nil {
				receiver := tx.Visitor().VisitNode(operandSkipped.Expression())
				readExpression, initializeExpression := tx.createCopiableReceiverExpr(receiver)
				expression := tx.createPrivateIdentifierAccessHelper(info, readExpression)
				var temp ast.Handle
				if !ast.IsPrefixUnaryExpression(node) && !discarded {
					temp = tx.Factory().NewTempVariable()
					tx.EmitContext().AddVariableDeclaration(temp)
				}
				expression = expandPreOrPostfixIncrementOrDecrementExpression(tx.Factory(), tx.EmitContext(), node, expression, temp)
				assignReceiver := readExpression
				if !initializeExpression.IsNil() {
					assignReceiver = initializeExpression
				}
				expression = tx.createPrivateIdentifierAssignment(info, assignReceiver, expression, ast.KindEqualsToken)
				tx.EmitContext().SetOriginal(expression, node)
				expression.SetLoc(node.Loc())
				if !temp.IsNil() {
					expression = tx.Factory().NewCommaExpression(expression, temp)
					expression.SetLoc(node.Loc())
				}
				return expression
			}
		} else if tx.shouldTransformSuperInStaticInitializers && !tx.currentClassElement.IsNil() && ast.IsSuperProperty(operandSkipped) && isStaticPropertyDeclarationOrClassStaticBlock(tx.currentClassElement) && tx.lexicalEnvironment != nil && tx.lexicalEnvironment.data != nil {
			data := tx.lexicalEnvironment.data
			if data.facts&classFactsClassWasDecorated != 0 {
				visitedExpr := tx.visitInvalidSuperProperty(operandSkipped)
				if ast.IsPrefixUnaryExpression(node) {
					return tx.Factory().UpdatePrefixUnaryExpression(node, node.PrefixUnaryExpressionOperator(), visitedExpr)
				}
				return tx.Factory().UpdatePostfixUnaryExpression(node, visitedExpr, node.PostfixUnaryExpressionOperator())
			}
			if !data.classConstructor.IsNil() && !data.superClassReference.IsNil() {
				var setterName ast.Handle
				var getterName ast.Handle
				if ast.IsPropertyAccessExpression(operandSkipped) {
					if ast.IsIdentifier(operandSkipped.Name()) {
						getterName = tx.Factory().NewStringLiteralFromNode(operandSkipped.Name())
						setterName = getterName
					}
				} else if ast.IsElementAccessExpression(operandSkipped) {
					if transformers.IsSimpleInlineableExpression(operandSkipped.ElementAccessExpressionArgumentExpression()) {
						getterName = operandSkipped.ElementAccessExpressionArgumentExpression()
						setterName = getterName
					} else {
						getterName = tx.Factory().NewTempVariable()
						tx.EmitContext().AddVariableDeclaration(getterName)
						setterName = tx.Factory().NewAssignmentExpression(getterName, tx.Visitor().VisitNode(operandSkipped.ElementAccessExpressionArgumentExpression()))
					}
				}
				if !setterName.IsNil() && !getterName.IsNil() {
					expression := tx.Factory().NewReflectGetCall(data.superClassReference, getterName, data.classConstructor)
					expression.SetLoc(operandSkipped.Loc())
					var temp ast.Handle
					if !discarded {
						temp = tx.Factory().NewTempVariable()
						tx.EmitContext().AddVariableDeclaration(temp)
					}
					expression = expandPreOrPostfixIncrementOrDecrementExpression(tx.Factory(), tx.EmitContext(), node, expression, temp)
					expression = tx.Factory().NewReflectSetCall(data.superClassReference, setterName, expression, data.classConstructor)
					tx.EmitContext().SetOriginal(expression, node)
					expression.SetLoc(node.Loc())
					if !temp.IsNil() {
						expression = tx.Factory().NewCommaExpression(expression, temp)
						expression.SetLoc(node.Loc())
					}
					return expression
				}
			}
		}
	}
	return tx.Visitor().VisitEachChild(node)
}
func (tx *classFieldsTransformer) visitForStatement(node ast.Handle) ast.Handle {
	initializer := tx.discardedValueVisitor.VisitNode(node.Initializer())
	condition := tx.Visitor().VisitNode(node.Condition())
	incrementor := tx.discardedValueVisitor.VisitNode(node.Incrementor())
	saved := tx.inIterationStatement
	tx.inIterationStatement = true
	body := tx.EmitContext().VisitIterationBody(node.Statement(), tx.Visitor())
	tx.inIterationStatement = saved
	return tx.Factory().UpdateForStatement(node, initializer, condition, incrementor, body)
}
func (tx *classFieldsTransformer) visitExpressionStatement(node ast.Handle) ast.Handle {
	if ast.IsPrivateIdentifier(node.Expression()) && tx.shouldTransformPrivateElementsOrClassStaticBlocks {
		return node
	}
	return tx.Factory().UpdateExpressionStatement(node, tx.discardedValueVisitor.VisitNode(node.Expression()))
}
func (tx *classFieldsTransformer) createCopiableReceiverExpr(receiver ast.Handle) (readExpression ast.Handle, initializeExpression ast.Handle) {
	clone := receiver
	if !ast.NodeIsSynthesized(receiver) {
		clone = tx.Factory().DeepCloneNode(receiver)
	}
	if transformers.IsSimpleInlineableExpression(receiver) {
		return clone, ast.Handle{}
	}
	readExpression = tx.Factory().NewTempVariable()
	tx.EmitContext().AddVariableDeclaration(readExpression)
	initializeExpression = tx.Factory().NewAssignmentExpression(readExpression, clone)
	return readExpression, initializeExpression
}
func (tx *classFieldsTransformer) visitCallExpression(node ast.Handle) ast.Handle {
	if ast.IsPropertyAccessExpression(node.Expression()) && ast.IsPrivateIdentifier(node.Expression().PropertyAccessExpressionName()) && tx.accessPrivateIdentifier(node.Expression().PropertyAccessExpressionName()) != nil {
		thisArg, target := tx.createCallBinding(node.Expression())
		visitedTarget := tx.Visitor().VisitNode(target)
		visitedThisArg := tx.Visitor().VisitNode(thisArg)
		visitedArgs := tx.Visitor().VisitSlice(node.Arguments())
		allArgs := make([]ast.Handle, 0, 1+len(visitedArgs))
		allArgs = append(allArgs, visitedThisArg)
		allArgs = append(allArgs, visitedArgs...)
		if node.Flags()&ast.NodeFlagsOptionalChain != 0 {
			return tx.Factory().UpdateCallExpression(node, tx.Factory().NewPropertyAccessExpression(visitedTarget, node.QuestionDotToken(), tx.Factory().NewIdentifier("call"), ast.NodeFlagsOptionalChain), ast.Handle{}, 0, tx.Factory().NewList(allArgs), node.Flags())
		}
		return tx.Factory().UpdateCallExpression(node, tx.Factory().NewPropertyAccessExpression(visitedTarget, ast.Handle{}, tx.Factory().NewIdentifier("call"), ast.NodeFlagsNone), ast.Handle{}, 0, tx.Factory().NewList(allArgs), node.Flags())
	}
	if tx.shouldTransformSuperInStaticInitializers && !tx.currentClassElement.IsNil() && ast.IsSuperProperty(node.Expression()) && isStaticPropertyDeclarationOrClassStaticBlock(tx.currentClassElement) && tx.lexicalEnvironment != nil && tx.lexicalEnvironment.data != nil && !tx.lexicalEnvironment.data.classConstructor.IsNil() {
		invocation := tx.Factory().NewFunctionCallCall(tx.Visitor().VisitNode(node.Expression()), tx.lexicalEnvironment.data.classConstructor, tx.Visitor().VisitSlice(node.Arguments()))
		tx.EmitContext().SetOriginal(invocation, node)
		invocation.SetLoc(node.Loc())
		return invocation
	}
	return tx.Visitor().VisitEachChild(node)
}
func (tx *classFieldsTransformer) visitTaggedTemplateExpression(node ast.Handle) ast.Handle {
	if ast.IsPropertyAccessExpression(node.Tag()) && ast.IsPrivateIdentifier(node.Tag().PropertyAccessExpressionName()) && tx.accessPrivateIdentifier(node.Tag().PropertyAccessExpressionName()) != nil {
		thisArg, target := tx.createCallBinding(node.Tag())
		bindExpr := tx.Factory().NewCallExpression(tx.Factory().NewPropertyAccessExpression(tx.Visitor().VisitNode(target), ast.Handle{}, tx.Factory().NewIdentifier("bind"), ast.NodeFlagsNone), ast.Handle{}, 0, tx.Factory().NewList([]ast.Handle{tx.Visitor().VisitNode(thisArg)}), ast.NodeFlagsNone)
		return tx.Factory().UpdateTaggedTemplateExpression(node, bindExpr, ast.Handle{}, 0, tx.Visitor().VisitNode(node.Template()), node.Flags())
	}
	if tx.shouldTransformSuperInStaticInitializers && !tx.currentClassElement.IsNil() && ast.IsSuperProperty(node.Tag()) && isStaticPropertyDeclarationOrClassStaticBlock(tx.currentClassElement) && tx.lexicalEnvironment != nil && tx.lexicalEnvironment.data != nil && !tx.lexicalEnvironment.data.classConstructor.IsNil() {
		invocation := tx.Factory().NewFunctionBindCall(tx.Visitor().VisitNode(node.Tag()), tx.lexicalEnvironment.data.classConstructor, nil)
		tx.EmitContext().SetOriginal(invocation, node)
		invocation.SetLoc(node.Loc())
		return tx.Factory().UpdateTaggedTemplateExpression(node, invocation, ast.Handle{}, 0, tx.Visitor().VisitNode(node.Template()), node.Flags())
	}
	return tx.Visitor().VisitEachChild(node)
}
func (tx *classFieldsTransformer) transformClassStaticBlockDeclaration(node ast.Handle) ast.Handle {
	if tx.shouldTransformPrivateElementsOrClassStaticBlocks {
		if isClassThisAssignmentBlock(tx.EmitContext(), node) {
			body := node.ClassStaticBlockDeclarationBody()
			result := tx.Visitor().VisitNode(body.Store().ListAt(body.BlockStatements(), 0).Expression())
			if ast.IsAssignmentExpression(result, true) {
				binary := result
				if binary.Left() == binary.Right() {
					return ast.Handle{}
				}
			}
			return result
		}
		if isClassNamedEvaluationHelperBlock(tx.EmitContext(), node) {
			body := node.ClassStaticBlockDeclarationBody()
			return tx.Visitor().VisitNode(body.Store().ListAt(body.BlockStatements(), 0).Expression())
		}
		tx.EmitContext().StartVariableEnvironment()
		// setCurrentClassElementAndVisitStatements takes []Handle ownership.
		statements := tx.setCurrentClassElementAndVisitStatements(node, node.ClassStaticBlockDeclarationBody().StatementsSeq().Slice())
		statements = tx.EmitContext().EndAndMergeVariableEnvironment(statements)
		iife := tx.Factory().NewImmediatelyInvokedArrowFunction(statements)
		arrowFunction := ast.SkipParentheses(iife.Expression())
		tx.EmitContext().SetOriginal(arrowFunction, node)
		tx.EmitContext().AddEmitFlags(arrowFunction, printer.EFNoLexicalArguments)
		srcBlock := node.ClassStaticBlockDeclarationBody()
		arrowBody := arrowFunction.ArrowFunctionBody()
		arrowBody.SetBlockStatements(tx.Factory().RelocateList(arrowBody.BlockStatements(), srcBlock.Store().ListLoc(srcBlock.BlockStatements())))
		tx.EmitContext().SetOriginal(iife, node)
		tx.EmitContext().AssignSourceMapRange(iife, node)
		tx.EmitContext().AddEmitFlags(arrowFunction, printer.EFNoLexicalThis)
		return iife
	}
	return ast.Handle{}
}
func (tx *classFieldsTransformer) setCurrentClassElementAndVisitStatements(classElement ast.Handle, statements []ast.Handle) []ast.Handle {
	savedCurrentClassElement := tx.currentClassElement
	tx.currentClassElement = classElement
	result := tx.Visitor().VisitSlice(statements)
	tx.currentClassElement = savedCurrentClassElement
	return result
}
func (tx *classFieldsTransformer) isAnonymousClassNeedingAssignedNameWorker(node ast.Handle) bool {
	if ast.IsClassExpression(node) && node.Name().IsNil() {
		staticPropertiesOrClassStaticBlocks := tx.getStaticPropertiesAndClassStaticBlock(node)
		if core.Some(staticPropertiesOrClassStaticBlocks, func(n ast.Handle) bool {
			return isClassNamedEvaluationHelperBlock(tx.EmitContext(), n)
		}) {
			return false
		}
		hasTransformableStatics := (tx.shouldTransformPrivateElementsOrClassStaticBlocks || tx.nodeHasTransformPrivateStaticElementsFlag(node)) && core.Some(staticPropertiesOrClassStaticBlocks, func(n ast.Handle) bool {
			return ast.IsClassStaticBlockDeclaration(n) || ast.IsPrivateIdentifierClassElementDeclaration(n) || tx.shouldTransformInitializers && ast.IsInitializedProperty(n)
		})
		return hasTransformableStatics
	}
	return false
}
func (tx *classFieldsTransformer) visitBinaryExpression(node ast.Handle, discarded bool) ast.Handle {
	if ast.IsDestructuringAssignment(node) {
		savedPendingExpressions := tx.pendingExpressions
		tx.pendingExpressions = nil
		updated := tx.Factory().UpdateBinaryExpression(node, 0, tx.assignmentTargetVisitor.VisitNode(node.Left()), ast.Handle{}, node.OperatorToken(), tx.Visitor().VisitNode(node.Right()))
		var result ast.Handle
		if len(tx.pendingExpressions) > 0 {
			exprs := append(tx.pendingExpressions, updated)
			result = tx.Factory().InlineExpressions(exprs)
		} else {
			result = updated
		}
		tx.pendingExpressions = savedPendingExpressions
		return result
	}
	if ast.IsAssignmentExpression(node, false) {
		if isNamedEvaluationAnd(tx.EmitContext(), node, tx.isAnonymousClassNeedingAssignedName) {
			node = transformNamedEvaluation(tx.EmitContext(), node, false, "")
			debug.Assert(!node.IsNil() && ast.IsAssignmentExpression(node, false))
		}
		left := ast.SkipOuterExpressions(node.Left(), ast.OEKPartiallyEmittedExpressions|ast.OEKParentheses)
		if ast.IsPropertyAccessExpression(left) && ast.IsPrivateIdentifier(left.Name()) {
			info := tx.accessPrivateIdentifier(left.Name())
			if info != nil {
				result := tx.createPrivateIdentifierAssignment(info, left.Expression(), node.Right(), node.OperatorToken().Kind)
				tx.EmitContext().SetOriginal(result, node)
				result.SetLoc(node.Loc())
				return result
			}
		} else if tx.shouldTransformSuperInStaticInitializers && !tx.currentClassElement.IsNil() && ast.IsSuperProperty(node.Left()) && isStaticPropertyDeclarationOrClassStaticBlock(tx.currentClassElement) && tx.lexicalEnvironment != nil && tx.lexicalEnvironment.data != nil {
			data := tx.lexicalEnvironment.data
			if data.facts&classFactsClassWasDecorated != 0 {
				return tx.Factory().UpdateBinaryExpression(node, 0, tx.visitInvalidSuperProperty(node.Left()), ast.Handle{}, node.OperatorToken(), tx.Visitor().VisitNode(node.Right()))
			}
			if !data.classConstructor.IsNil() && !data.superClassReference.IsNil() {
				var setterName ast.Handle
				if ast.IsElementAccessExpression(node.Left()) {
					setterName = tx.Visitor().VisitNode(node.Left().ElementAccessExpressionArgumentExpression())
				} else if ast.IsPropertyAccessExpression(node.Left()) && ast.IsIdentifier(node.Left().PropertyAccessExpressionName()) {
					setterName = tx.Factory().NewStringLiteralFromNode(node.Left().PropertyAccessExpressionName())
				}
				if !setterName.IsNil() {
					expression := tx.Visitor().VisitNode(node.Right())
					if ast.IsCompoundAssignment(node.OperatorToken().Kind) {
						getterName := setterName
						if !transformers.IsSimpleInlineableExpression(setterName) {
							getterName = tx.Factory().NewTempVariable()
							tx.EmitContext().AddVariableDeclaration(getterName)
							setterName = tx.Factory().NewAssignmentExpression(getterName, setterName)
						}
						superPropertyGet := tx.Factory().NewReflectGetCall(data.superClassReference, getterName, data.classConstructor)
						tx.EmitContext().SetOriginal(superPropertyGet, node.Left())
						superPropertyGet.SetLoc(node.Left().Loc())
						expression = tx.Factory().NewBinaryExpression(0, superPropertyGet, ast.Handle{}, tx.Factory().NewToken(transformers.GetNonAssignmentOperatorForCompoundAssignment(node.OperatorToken().Kind)), expression)
						expression.SetLoc(node.Loc())
					}
					var temp ast.Handle
					if !discarded {
						temp = tx.Factory().NewTempVariable()
						tx.EmitContext().AddVariableDeclaration(temp)
					}
					if !temp.IsNil() {
						expression = tx.Factory().NewAssignmentExpression(temp, expression)
						expression.SetLoc(node.Loc())
					}
					expression = tx.Factory().NewReflectSetCall(data.superClassReference, setterName, expression, data.classConstructor)
					tx.EmitContext().SetOriginal(expression, node)
					expression.SetLoc(node.Loc())
					if !temp.IsNil() {
						expression = tx.Factory().NewCommaExpression(expression, temp)
						expression.SetLoc(node.Loc())
					}
					return expression
				}
			}
		}
	}
	if node.OperatorToken().Kind == ast.KindInKeyword && ast.IsPrivateIdentifier(node.Left()) {
		return tx.transformPrivateIdentifierInInExpression(node)
	}
	return tx.Visitor().VisitEachChild(node)
}
func (tx *classFieldsTransformer) visitParenthesizedExpression(node ast.Handle, discarded bool) ast.Handle {
	if discarded {
		expression := tx.discardedValueVisitor.VisitNode(node.Expression())
		return tx.Factory().UpdateParenthesizedExpression(node, expression)
	}
	expression := tx.Visitor().VisitNode(node.Expression())
	return tx.Factory().UpdateParenthesizedExpression(node, expression)
}
func (tx *classFieldsTransformer) createPrivateIdentifierAssignment(info *privateIdentifierInfo, receiver ast.Handle, right ast.Handle, operator ast.Kind) ast.Handle {
	receiver = tx.Visitor().VisitNode(receiver)
	right = tx.Visitor().VisitNode(right)
	if ast.IsCompoundAssignment(operator) {
		readExpression, initializeExpression := tx.createCopiableReceiverExpr(receiver)
		if !initializeExpression.IsNil() {
			receiver = initializeExpression
		} else {
			receiver = readExpression
		}
		right = tx.Factory().NewBinaryExpression(0, tx.createPrivateIdentifierAccessHelper(info, readExpression), ast.Handle{}, tx.Factory().NewToken(transformers.GetNonAssignmentOperatorForCompoundAssignment(operator)), right)
	}
	tx.EmitContext().SetCommentRange(receiver, core.NewTextRange(-1, receiver.End()))
	switch info.kind {
	case printer.PrivateIdentifierKindAccessor:
		return tx.Factory().NewClassPrivateFieldSetHelper(receiver, info.brandCheckIdentifier, right, info.kind, info.setterName)
	case printer.PrivateIdentifierKindMethod:
		return tx.Factory().NewClassPrivateFieldSetHelper(receiver, info.brandCheckIdentifier, right, info.kind, ast.Handle{})
	case printer.PrivateIdentifierKindField:
		var f ast.Handle
		if info.isStatic {
			f = info.variableName
		}
		return tx.Factory().NewClassPrivateFieldSetHelper(receiver, info.brandCheckIdentifier, right, info.kind, f)
	case printer.PrivateIdentifierKindUntransformed:
		debug.Fail("Access helpers should not be created for untransformed private elements")
		return ast.Handle{}
	}
	debug.AssertNever(info, "Unknown private element type")
	return ast.Handle{}
}
func (tx *classFieldsTransformer) getPrivateInstanceMethodsAndAccessors(node ast.Handle) []ast.Handle {
	return core.Filter(node.Members(), isNonStaticMethodOrAccessorWithPrivateName)
}

func (tx *classFieldsTransformer) memberContainsConstructorReference(member ast.Handle, classDecl ast.Handle) bool {
	classOriginal := tx.EmitContext().MostOriginal(classDecl)
	className := ast.GetNameOfDeclaration(classDecl)
	var check func(n ast.Handle) bool
	check = func(n ast.Handle) bool {
		if ast.IsIdentifier(n) && n != className {
			decl := tx.resolver.GetReferencedValueDeclaration(n)
			if decl == classOriginal {
				return true
			}
		}
		if ast.IsPropertyAccessExpression(n) {
			return check(n.Expression())
		}
		return n.ForEachChild(check)
	}
	if ast.IsClassStaticBlockDeclaration(member) {
		body := member.ClassStaticBlockDeclarationBody()
		if !body.IsNil() && check(body) {
			return true
		}
	} else {
		body := member.Body()
		if !body.IsNil() && check(body) {
			return true
		}
	}
	if ast.IsPropertyDeclaration(member) {
		init := member.Initializer()
		if !init.IsNil() && check(init) {
			return true
		}
	}
	return false
}

func (tx *classFieldsTransformer) classContainsConstructorReference(node ast.Handle) bool {
	for _, member := range node.Members() {
		if tx.memberContainsConstructorReference(member, node) {
			return true
		}
	}
	return false
}
func (tx *classFieldsTransformer) getClassFacts(node ast.Handle) classFacts {
	facts := classFactsNone
	original := tx.EmitContext().MostOriginal(node)
	if ast.IsClassLike(original) && ast.ClassOrConstructorParameterIsDecorated(tx.legacyDecorators, original) {
		facts |= classFactsClassWasDecorated
	}
	if tx.shouldTransformPrivateElementsOrClassStaticBlocks && (classHasClassThisAssignment(tx.EmitContext(), node) || classHasExplicitlyAssignedName(tx.EmitContext(), node)) {
		facts |= classFactsNeedsClassConstructorReference
	}
	var containsPublicInstanceFields bool
	var containsInitializedPublicInstanceFields bool
	var containsInstancePrivateElements bool
	var containsInstanceAutoAccessors bool
	for _, member := range node.Members() {
		if ast.IsStatic(member) {
			if !member.Name().IsNil() && (ast.IsPrivateIdentifier(member.Name()) || ast.IsAutoAccessorPropertyDeclaration(member)) && tx.shouldTransformPrivateElementsOrClassStaticBlocks {
				facts |= classFactsNeedsClassConstructorReference
			} else if ast.IsAutoAccessorPropertyDeclaration(member) && tx.shouldTransformAutoAccessors && node.Name().IsNil() && tx.EmitContext().ClassThis(node).IsNil() {
				facts |= classFactsNeedsClassConstructorReference
			}
			if ast.IsPropertyDeclaration(member) || ast.IsClassStaticBlockDeclaration(member) {
				if tx.shouldTransformThisInStaticInitializers && member.SubtreeFacts()&ast.SubtreeContainsLexicalThis != 0 {
					facts |= classFactsNeedsSubstitutionForThisInClassStaticField
					if facts&classFactsClassWasDecorated == 0 {
						facts |= classFactsNeedsClassConstructorReference
					}
				}
				if tx.shouldTransformSuperInStaticInitializers && member.SubtreeFacts()&ast.SubtreeContainsLexicalSuper != 0 {
					if facts&classFactsClassWasDecorated == 0 {
						facts |= classFactsNeedsClassConstructorReference | classFactsNeedsClassSuperReference
					}
				}
			}
		} else if !ast.HasAbstractModifier(tx.EmitContext().MostOriginal(member)) {
			if ast.IsAutoAccessorPropertyDeclaration(member) {
				containsInstanceAutoAccessors = true
				containsInstancePrivateElements = containsInstancePrivateElements || ast.IsPrivateIdentifierClassElementDeclaration(member)
			} else if ast.IsPrivateIdentifierClassElementDeclaration(member) {
				containsInstancePrivateElements = true
				if tx.memberContainsConstructorReference(member, node) {
					facts |= classFactsNeedsClassConstructorReference
				}
			} else if ast.IsPropertyDeclaration(member) {
				containsPublicInstanceFields = true
				containsInitializedPublicInstanceFields = containsInitializedPublicInstanceFields || !member.Initializer().IsNil()
			}
		}
	}
	willHoistInitializersToConstructor := (tx.shouldTransformInitializersUsingDefine && containsPublicInstanceFields) || (tx.shouldTransformInitializersUsingSet && containsInitializedPublicInstanceFields) || (tx.shouldTransformPrivateElementsOrClassStaticBlocks && containsInstancePrivateElements) || (tx.shouldTransformPrivateElementsOrClassStaticBlocks && containsInstanceAutoAccessors && tx.shouldTransformAutoAccessors)
	if willHoistInitializersToConstructor {
		facts |= classFactsWillHoistInitializersToConstructor
	}
	return facts
}
func (tx *classFieldsTransformer) visitExpressionWithTypeArgumentsInHeritageClause(node ast.Handle) ast.Handle {
	facts := classFactsNone
	if tx.lexicalEnvironment != nil && tx.lexicalEnvironment.data != nil {
		facts = tx.lexicalEnvironment.data.facts
	}
	if facts&classFactsNeedsClassSuperReference != 0 {
		temp := tx.Factory().NewTempVariableEx(printer.AutoGenerateOptions{Flags: printer.GeneratedIdentifierFlagsReservedInNestedScopes})
		tx.EmitContext().AddVariableDeclaration(temp)
		tx.getClassLexicalEnvironment().superClassReference = temp
		return tx.Factory().UpdateExpressionWithTypeArguments(node, tx.Factory().NewAssignmentExpression(temp, tx.Visitor().VisitNode(node.Expression())), 0)
	}
	return tx.heritageClauseVisitor.VisitEachChild(node)
}
func (tx *classFieldsTransformer) visitInNewClassLexicalEnvironment(node ast.Handle, visitor func(tx *classFieldsTransformer, node ast.Handle, facts classFacts) ast.Handle) ast.Handle {
	savedCurrentClassContainer := tx.currentClassContainer
	savedPendingExpressions := tx.pendingExpressions
	savedLexicalEnvironment := tx.lexicalEnvironment
	tx.currentClassContainer = node
	tx.pendingExpressions = nil
	tx.startClassLexicalEnvironment()
	original := tx.EmitContext().MostOriginal(node)
	tx.enclosingClassDeclarations.Add(original)
	if tx.shouldTransformPrivateElementsOrClassStaticBlocks || tx.nodeHasTransformPrivateStaticElementsFlag(node) {
		name := ast.GetNameOfDeclaration(node)
		if !name.IsNil() && ast.IsIdentifier(name) {
			tx.getPrivateIdentifierEnvironment().data.className = name
		} else if assignedName := tx.EmitContext().AssignedName(node); !assignedName.IsNil() {
			if ast.IsStringLiteral(assignedName) {
				if textSourceNode := tx.EmitContext().TextSource(assignedName); !textSourceNode.IsNil() && ast.IsIdentifier(textSourceNode) {
					tx.getPrivateIdentifierEnvironment().data.className = textSourceNode
				} else if scanner.IsIdentifierText(assignedName.Text(), core.LanguageVariantStandard) {
					prefixName := tx.Factory().NewIdentifier(assignedName.Text())
					tx.getPrivateIdentifierEnvironment().data.className = prefixName
				}
			}
		}
	}
	if tx.shouldTransformPrivateElementsOrClassStaticBlocks {
		privateInstanceMethodsAndAccessors := tx.getPrivateInstanceMethodsAndAccessors(node)
		if len(privateInstanceMethodsAndAccessors) > 0 {
			tx.getPrivateIdentifierEnvironment().data.weakSetName = tx.createHoistedVariableForClass("instances", privateInstanceMethodsAndAccessors[0].Name(), "")
		}
	}
	facts := tx.getClassFacts(node)
	if facts != classFactsNone {
		tx.getClassLexicalEnvironment().facts = facts
	}
	result := visitor(tx, node, facts)
	tx.enclosingClassDeclarations.Delete(original)
	tx.endClassLexicalEnvironment()
	debug.Assert(tx.lexicalEnvironment == savedLexicalEnvironment)
	tx.currentClassContainer = savedCurrentClassContainer
	tx.pendingExpressions = savedPendingExpressions
	tx.lexicalEnvironment = savedLexicalEnvironment
	return result
}
func (tx *classFieldsTransformer) visitClassDeclaration(node ast.Handle) ast.Handle {
	return tx.visitInNewClassLexicalEnvironment(node, (*classFieldsTransformer).visitClassDeclarationInNewClassLexicalEnvironment)
}
func (tx *classFieldsTransformer) visitClassDeclarationInNewClassLexicalEnvironment(node ast.Handle, facts classFacts) ast.Handle {
	classDecl := node
	var pendingClassReferenceAssignment ast.Handle
	if facts&classFactsNeedsClassConstructorReference != 0 {
		if tx.shouldTransformPrivateElementsOrClassStaticBlocks && !tx.EmitContext().ClassThis(node).IsNil() {
			classThis := tx.EmitContext().ClassThis(node)
			tx.getClassLexicalEnvironment().classConstructor = classThis
			pendingClassReferenceAssignment = tx.Factory().NewAssignmentExpression(classThis, tx.Factory().GetLocalName(node))
		} else {
			temp := tx.Factory().NewTempVariableEx(printer.AutoGenerateOptions{Flags: printer.GeneratedIdentifierFlagsReservedInNestedScopes})
			tx.EmitContext().AddVariableDeclaration(temp)
			tx.getClassLexicalEnvironment().classConstructor = tx.Factory().DeepCloneNode(temp)
			pendingClassReferenceAssignment = tx.Factory().NewAssignmentExpression(temp, tx.Factory().GetLocalName(node))
		}
	}
	if !tx.EmitContext().ClassThis(node).IsNil() {
		tx.getClassLexicalEnvironment().classThis = tx.EmitContext().ClassThis(node)
	}
	isClassWithConstructorReference := tx.classContainsConstructorReference(node)
	alias := tx.getClassLexicalEnvironment().classConstructor
	if isClassWithConstructorReference && !alias.IsNil() {
		tx.classAliases[tx.EmitContext().MostOriginal(node)] = alias
	}
	modifiers := tx.modifierVisitor.VisitModifiers(classDecl.Modifiers())
	heritageClauses := tx.heritageClauseVisitor.VisitNodes(classDecl.HeritageClauses())
	members, membersPrologue := tx.transformClassMembers(node)
	var statements []ast.Handle
	if !pendingClassReferenceAssignment.IsNil() {
		tx.pendingExpressions = append([]ast.Handle{pendingClassReferenceAssignment}, tx.pendingExpressions...)
	}
	if len(tx.pendingExpressions) > 0 {
		statements = append(statements, tx.Factory().NewExpressionStatement(tx.Factory().InlineExpressions(tx.pendingExpressions)))
	}
	name := classDecl.Name()
	if tx.shouldTransformInitializersUsingSet || tx.shouldTransformPrivateElementsOrClassStaticBlocks {
		staticProperties := tx.getStaticPropertiesAndClassStaticBlock(node)
		if len(staticProperties) > 0 {
			if name.IsNil() {
				name = tx.Factory().NewGeneratedNameForNode(node)
			}
			statements = tx.addPropertyOrClassStaticBlockStatements(statements, staticProperties, tx.Factory().GetLocalName(node))
		}
	}
	isExport := ast.HasSyntacticModifier(node, ast.ModifierFlagsExport)
	isDefault := ast.HasSyntacticModifier(node, ast.ModifierFlagsDefault)
	if len(statements) > 0 && isExport && isDefault {
		modifiers = transformers.ExtractModifiers(tx.EmitContext(), modifiers, ^ast.ModifierFlagsExportDefault)
		exportAssignment := tx.Factory().NewExportAssignment(0, false, ast.Handle{}, tx.Factory().GetLocalName(node))
		statements = append(statements, exportAssignment)
	}
	updatedClass := tx.Factory().UpdateClassDeclaration(classDecl, modifiers, name, 0, heritageClauses, members)
	result := make([]ast.Handle, 0, 1+len(statements)+1)
	if !membersPrologue.IsNil() {
		result = append(result, tx.Factory().NewExpressionStatement(membersPrologue))
	}
	result = append(result, updatedClass)
	result = append(result, statements...)
	return tx.Factory().NewSyntaxList(tx.Factory().NewList(result))
}
func (tx *classFieldsTransformer) visitClassExpression(node ast.Handle) ast.Handle {
	return tx.visitInNewClassLexicalEnvironment(node, (*classFieldsTransformer).visitClassExpressionInNewClassLexicalEnvironment)
}
func (tx *classFieldsTransformer) visitClassExpressionInNewClassLexicalEnvironment(node ast.Handle, facts classFacts) ast.Handle {
	classExpr := node
	isDecoratedClassDeclaration := facts&classFactsClassWasDecorated != 0
	if !tx.EmitContext().ClassThis(node).IsNil() {
		tx.getClassLexicalEnvironment().classThis = tx.EmitContext().ClassThis(node)
	}
	var temp ast.Handle
	if facts&classFactsNeedsClassConstructorReference != 0 {
		if (tx.shouldTransformPrivateElementsOrClassStaticBlocks || tx.nodeHasTransformPrivateStaticElementsFlag(node)) && !tx.EmitContext().ClassThis(node).IsNil() {
			classThis := tx.EmitContext().ClassThis(node)
			tx.getClassLexicalEnvironment().classConstructor = classThis
			temp = classThis
		} else {
			temp = tx.Factory().NewTempVariableEx(printer.AutoGenerateOptions{Flags: printer.GeneratedIdentifierFlagsReservedInNestedScopes})
			if tx.classExpressionNeedsBlockScopedTemp() {
				tx.EmitContext().AddLexicalDeclaration(temp)
			} else {
				tx.EmitContext().AddVariableDeclaration(temp)
			}
			tx.getClassLexicalEnvironment().classConstructor = tx.Factory().DeepCloneNode(temp)
		}
	}
	staticPropertiesOrClassStaticBlocks := tx.getStaticPropertiesAndClassStaticBlock(node)
	isClassWithConstructorReference := false
	hasTransformableStatics := false
	deferTempDeclaration := false
	if !isDecoratedClassDeclaration {
		isClassWithConstructorReference = tx.classContainsConstructorReference(node)
		hasTransformableStatics = (tx.shouldTransformPrivateElementsOrClassStaticBlocks || tx.nodeHasTransformPrivateStaticElementsFlag(node)) && core.Some(staticPropertiesOrClassStaticBlocks, func(n ast.Handle) bool {
			return ast.IsClassStaticBlockDeclaration(n) || ast.IsPrivateIdentifierClassElementDeclaration(n) || (tx.shouldTransformInitializers && ast.IsInitializedProperty(n))
		})
		willHavePrivatePendingExpressions := tx.shouldTransformPrivateElementsOrClassStaticBlocks && core.Some(node.Members(), func(n ast.Handle) bool {
			return ast.IsPrivateIdentifierClassElementDeclaration(n) && !ast.HasStaticModifier(n) && tx.shouldTransformClassElementToWeakMap(n)
		})
		willNeedTempWrapper := hasTransformableStatics || willHavePrivatePendingExpressions
		if isClassWithConstructorReference && willNeedTempWrapper && tx.getClassLexicalEnvironment().classConstructor.IsNil() {
			temp = tx.Factory().NewTempVariableEx(printer.AutoGenerateOptions{Flags: printer.GeneratedIdentifierFlagsReservedInNestedScopes})
			deferTempDeclaration = true
			tx.getClassLexicalEnvironment().classConstructor = tx.Factory().DeepCloneNode(temp)
		}
		if alias := tx.getClassLexicalEnvironment().classConstructor; isClassWithConstructorReference && willNeedTempWrapper && !alias.IsNil() {
			tx.classAliases[tx.EmitContext().MostOriginal(node)] = alias
		}
	}
	modifiers := tx.modifierVisitor.VisitModifiers(classExpr.Modifiers())
	heritageClauses := tx.heritageClauseVisitor.VisitNodes(classExpr.HeritageClauses())
	members, membersPrologue := tx.transformClassMembers(node)
	if deferTempDeclaration {
		if tx.classExpressionNeedsBlockScopedTemp() {
			tx.EmitContext().AddLexicalDeclaration(temp)
		} else {
			tx.EmitContext().AddVariableDeclaration(temp)
		}
	}
	classExpression := tx.Factory().UpdateClassExpression(classExpr, modifiers, classExpr.Name(), 0, heritageClauses, members)
	var expressions []ast.Handle
	if !membersPrologue.IsNil() {
		expressions = append(expressions, membersPrologue)
	}
	if !isDecoratedClassDeclaration {
		if hasTransformableStatics || len(tx.pendingExpressions) > 0 {
			if temp.IsNil() {
				temp = tx.Factory().NewTempVariableEx(printer.AutoGenerateOptions{Flags: printer.GeneratedIdentifierFlagsReservedInNestedScopes})
				if tx.classExpressionNeedsBlockScopedTemp() {
					tx.EmitContext().AddLexicalDeclaration(temp)
				} else {
					tx.EmitContext().AddVariableDeclaration(temp)
				}
				tx.getClassLexicalEnvironment().classConstructor = tx.Factory().DeepCloneNode(temp)
				if isClassWithConstructorReference {
					tx.classAliases[tx.EmitContext().MostOriginal(node)] = tx.getClassLexicalEnvironment().classConstructor
				}
			}
			expressions = append(expressions, tx.Factory().NewAssignmentExpression(temp, classExpression))
			expressions = append(expressions, tx.pendingExpressions...)
			expressions = append(expressions, tx.generateInitializedPropertyExpressionsOrClassStaticBlock(staticPropertiesOrClassStaticBlocks, temp)...)
			expressions = append(expressions, tx.Factory().DeepCloneNode(temp))
		} else {
			expressions = append(expressions, classExpression)
		}
	} else {
		if len(tx.pendingExpressions) > 0 {
			for _, expr := range tx.pendingExpressions {
				tx.pendingStatements = append(tx.pendingStatements, tx.Factory().NewExpressionStatement(expr))
			}
		}
		if len(staticPropertiesOrClassStaticBlocks) > 0 {
			classThisOrName := tx.EmitContext().ClassThis(node)
			if classThisOrName.IsNil() {
				classThisOrName = tx.Factory().GetLocalName(node)
			}
			tx.pendingStatements = tx.addPropertyOrClassStaticBlockStatements(tx.pendingStatements, staticPropertiesOrClassStaticBlocks, classThisOrName)
		}
		if !temp.IsNil() {
			expressions = append(expressions, tx.Factory().NewAssignmentExpression(temp, classExpression))
		} else if tx.shouldTransformPrivateElementsOrClassStaticBlocks && !tx.EmitContext().ClassThis(node).IsNil() {
			expressions = append(expressions, tx.Factory().NewAssignmentExpression(tx.EmitContext().ClassThis(node), classExpression))
		} else {
			expressions = append(expressions, classExpression)
		}
	}
	if len(expressions) > 1 {
		tx.EmitContext().AddEmitFlags(classExpression, printer.EFIndented)
		for _, expr := range expressions {
			tx.EmitContext().AddEmitFlags(expr, printer.EFStartOnNewLine)
		}
	}
	return tx.Factory().InlineExpressions(expressions)
}
func (tx *classFieldsTransformer) visitClassStaticBlockDeclaration(node ast.Handle) ast.Handle {
	if !tx.shouldTransformPrivateElementsOrClassStaticBlocks {
		return tx.Visitor().VisitEachChild(node)
	}
	return ast.Handle{}
}

func (tx *classFieldsTransformer) visitThisExpression(node ast.Handle) ast.Handle {
	if tx.insideComputedPropertyName && tx.shouldTransformThisInStaticInitializers && tx.lexicalEnvironment != nil && tx.lexicalEnvironment.data != nil {
		if tx.lexicalEnvironment.data.facts&classFactsClassWasDecorated == 0 || tx.legacyDecorators {
			if classThis := tx.tryGetClassThisNoContainer(); !classThis.IsNil() {
				return classThis
			}
		}
	}
	if tx.shouldTransformThisInStaticInitializers && !tx.currentClassElement.IsNil() && (ast.IsClassStaticBlockDeclaration(tx.currentClassElement) || (ast.IsPropertyDeclaration(tx.currentClassElement) && ast.HasStaticModifier(tx.currentClassElement))) && tx.lexicalEnvironment != nil && tx.lexicalEnvironment.data != nil {
		if classThis := tx.tryGetClassThisNoContainer(); !classThis.IsNil() {
			return classThis
		}
		if tx.lexicalEnvironment.data.facts&classFactsClassWasDecorated != 0 && tx.legacyDecorators {
			return tx.Factory().NewParenthesizedExpression(tx.Factory().NewVoidZeroExpression())
		}
	}
	return node
}
func (tx *classFieldsTransformer) transformClassMembers(node ast.Handle) (members ast.ListRef, prologue ast.Handle) {
	shouldTransformPrivateStaticElementsInClass := tx.EmitContext().EmitFlags(node)&printer.EFTransformPrivateStaticElements != 0
	if tx.shouldTransformPrivateElementsOrClassStaticBlocks || tx.shouldTransformPrivateStaticElementsInFile {
		for _, member := range node.Members() {
			if ast.IsPrivateIdentifierClassElementDeclaration(member) {
				if tx.shouldTransformClassElementToWeakMap(member) {
					tx.addPrivateIdentifierToEnvironment(member)
				} else {
					env := tx.getPrivateIdentifierEnvironment()
					tx.setPrivateIdentifier(env, member.Name(), &privateIdentifierInfo{kind: printer.PrivateIdentifierKindUntransformed})
				}
			}
		}
		if tx.shouldTransformPrivateElementsOrClassStaticBlocks {
			if len(tx.getPrivateInstanceMethodsAndAccessors(node)) > 0 {
				tx.createBrandCheckWeakSetForPrivateMethods()
			}
		}
		if tx.shouldTransformAutoAccessorsInCurrentClass() {
			for _, member := range node.Members() {
				if ast.IsAutoAccessorPropertyDeclaration(member) {
					storageName := tx.Factory().NewGeneratedPrivateNameForNodeEx(member.Name(), printer.AutoGenerateOptions{Suffix: "_accessor_storage"})
					if tx.shouldTransformPrivateElementsOrClassStaticBlocks || shouldTransformPrivateStaticElementsInClass && ast.HasStaticModifier(member) {
						tx.addPrivateIdentifierPropertyDeclarationToEnvironment(member, storageName)
					} else {
						env := tx.getPrivateIdentifierEnvironment()
						if _, ok := tx.getPrivateIdentifier(env, storageName); !ok {
							tx.setPrivateIdentifier(env, storageName, &privateIdentifierInfo{kind: printer.PrivateIdentifierKindUntransformed})
						}
					}
				}
			}
		}
	}
	members = tx.classElementVisitor.VisitNodes(node.MemberList())
	var syntheticConstructor ast.Handle
	if !node.Store().ListSlice(members).Some(ast.IsConstructorDeclaration) {
		syntheticConstructor = tx.transformConstructor(ast.Handle{}, node)
	}
	var syntheticStaticBlock ast.Handle
	if !tx.shouldTransformPrivateElementsOrClassStaticBlocks && len(tx.pendingExpressions) > 0 {
		statement := tx.Factory().NewExpressionStatement(tx.Factory().InlineExpressions(tx.pendingExpressions))
		if statement.SubtreeFacts()&ast.SubtreeContainsLexicalThisOrSuper != 0 {
			temp := tx.Factory().NewTempVariable()
			tx.EmitContext().AddVariableDeclaration(temp)
			arrow := tx.Factory().NewArrowFunction(0, 0, tx.Factory().NewList(nil), ast.Handle{}, ast.Handle{}, tx.Factory().NewToken(ast.KindEqualsGreaterThanToken), tx.Factory().NewBlock(tx.Factory().NewList([]ast.Handle{statement}), false))
			prologue = tx.Factory().NewAssignmentExpression(temp, arrow)
			statement = tx.Factory().NewExpressionStatement(tx.Factory().NewCallExpression(temp, ast.Handle{}, 0, tx.Factory().NewList(nil), ast.NodeFlagsNone))
		}
		block := tx.Factory().NewBlock(tx.Factory().NewList([]ast.Handle{statement}), false)
		syntheticStaticBlock = tx.Factory().NewClassStaticBlockDeclaration(0, block)
		tx.pendingExpressions = nil
	}
	if !syntheticConstructor.IsNil() || !syntheticStaticBlock.IsNil() {
		membersArray := make([]ast.Handle, 0, node.Store().ListLen(members)+2)
		classThisIdx := -1
		namedEvalIdx := -1
		for i, n := range node.Store().ListSlice(members).All() {
			if classThisIdx < 0 && isClassThisAssignmentBlock(tx.EmitContext(), n) {
				classThisIdx = i
			}
			if namedEvalIdx < 0 && isClassNamedEvaluationHelperBlock(tx.EmitContext(), n) {
				namedEvalIdx = i
			}
			if classThisIdx >= 0 && namedEvalIdx >= 0 {
				break
			}
		}
		if classThisIdx >= 0 {
			membersArray = append(membersArray, node.Store().ListAt(members, classThisIdx))
		}
		if namedEvalIdx >= 0 {
			membersArray = append(membersArray, node.Store().ListAt(members, namedEvalIdx))
		}
		if !syntheticConstructor.IsNil() {
			membersArray = append(membersArray, syntheticConstructor)
		}
		if !syntheticStaticBlock.IsNil() {
			membersArray = append(membersArray, syntheticStaticBlock)
		}
		for i, member := range node.Store().ListSlice(members).All() {
			if i != classThisIdx && i != namedEvalIdx {
				membersArray = append(membersArray, member)
			}
		}
		members = tx.Factory().RelocateList(tx.Factory().NewList(membersArray), node.Store().ListLoc(node.MemberList()))
	}
	return members, prologue
}
func (tx *classFieldsTransformer) createBrandCheckWeakSetForPrivateMethods() {
	env := tx.getPrivateIdentifierEnvironment()
	weakSetName := env.data.weakSetName
	debug.Assert(!weakSetName.IsNil(), "weakSetName should be set in private identifier environment")
	tx.addPendingExpressions(tx.Factory().NewAssignmentExpression(weakSetName, tx.Factory().NewNewExpression(tx.Factory().NewIdentifier("WeakSet"), 0, tx.Factory().NewList(nil))))
}
func (tx *classFieldsTransformer) transformConstructor(constructor ast.Handle, container ast.Handle) ast.Handle {
	if tx.lexicalEnvironment == nil || tx.lexicalEnvironment.data == nil || tx.lexicalEnvironment.data.facts&classFactsWillHoistInitializersToConstructor == 0 {
		if !constructor.IsNil() {
			return tx.Visitor().VisitEachChild(constructor)
		}
		return ast.Handle{}
	}
	extendsClauseElement := ast.GetClassExtendsHeritageElement(container)
	isDerivedClass := !extendsClauseElement.IsNil() && ast.SkipOuterExpressions(extendsClauseElement.Expression(), ast.OEKAll).Kind != ast.KindNullKeyword
	var parameters ast.ListRef
	if !constructor.IsNil() {
		parameters = tx.Visitor().VisitNodes(constructor.ParameterList())
	}
	body := tx.transformConstructorBody(container, constructor, isDerivedClass)
	if body.IsNil() {
		if !constructor.IsNil() {
			return tx.Visitor().VisitEachChild(constructor)
		}
		return ast.Handle{}
	}
	if !constructor.IsNil() {
		debug.Assert(parameters != 0)
		return tx.Factory().UpdateConstructorDeclaration(constructor, 0, 0, parameters, ast.Handle{}, ast.Handle{}, body)
	}
	if parameters == 0 {
		parameters = tx.Factory().NewList(nil)
	}
	result := tx.Factory().NewConstructorDeclaration(0, 0, parameters, ast.Handle{}, ast.Handle{}, body)
	result.SetLoc(container.Loc())
	return result
}
func (tx *classFieldsTransformer) transformConstructorBodyWorker(statementsOut []ast.Handle, statementsIn []ast.Handle, statementOffset int, superPath []int, superPathDepth int, initializerStatements []ast.Handle, constructor ast.Handle) []ast.Handle {
	superStatementIndex := superPath[superPathDepth]
	superStatement := statementsIn[superStatementIndex]
	visited := tx.Visitor().VisitSlice(statementsIn[statementOffset:superStatementIndex])
	statementsOut = append(statementsOut, visited...)
	statementOffset = superStatementIndex + 1
	if ast.IsTryStatement(superStatement) {
		tryBlock := superStatement.TryStatementTryBlock()
		tryBlockStatements := tx.transformConstructorBodyWorker(nil, tryBlock.Statements(), 0, superPath, superPathDepth+1, initializerStatements, constructor)
		tryStatementList := tx.Factory().List(tryBlock.Store().ListLoc(tryBlock.StatementList()), tryBlockStatements...)
		catchClause := tx.Visitor().VisitNode(superStatement.TryStatementCatchClause())
		finallyBlock := tx.Visitor().VisitNode(superStatement.TryStatementFinallyBlock())
		updated := tx.Factory().UpdateTryStatement(superStatement, tx.Factory().UpdateBlock(tryBlock, tryStatementList, tryBlock.MultiLine()), catchClause, finallyBlock)
		statementsOut = append(statementsOut, updated)
	} else {
		visited := tx.Visitor().VisitSlice(statementsIn[superStatementIndex : superStatementIndex+1])
		statementsOut = append(statementsOut, visited...)
		for statementOffset < len(statementsIn) {
			stmt := statementsIn[statementOffset]
			orig := tx.EmitContext().MostOriginal(stmt)
			if ast.IsParameterPropertyDeclaration(orig, constructor) {
				statementOffset++
			} else {
				break
			}
		}
		statementsOut = append(statementsOut, initializerStatements...)
	}
	visited2 := tx.Visitor().VisitSlice(statementsIn[statementOffset:])
	statementsOut = append(statementsOut, visited2...)
	return statementsOut
}
func (tx *classFieldsTransformer) transformConstructorBody(container ast.Handle, constructor ast.Handle, isDerivedClass bool) ast.Handle {
	instanceProperties := tx.getProperties(container, false, false)
	properties := instanceProperties
	if !tx.compilerOptions.GetUseDefineForClassFields() {
		properties = core.Filter(properties, func(prop ast.Handle) bool {
			return !prop.Initializer().IsNil() || ast.IsPrivateIdentifier(prop.Name()) || ast.HasAccessorModifier(prop)
		})
	}
	privateMethodsAndAccessors := tx.getPrivateInstanceMethodsAndAccessors(container)
	needsConstructorBody := len(properties) > 0 || len(privateMethodsAndAccessors) > 0
	if constructor.IsNil() && !needsConstructorBody {
		return tx.EmitContext().VisitFunctionBody(ast.Handle{}, tx.Visitor())
	}
	tx.EmitContext().StartVariableEnvironment()
	needsSyntheticConstructor := constructor.IsNil() && isDerivedClass
	var statements []ast.Handle
	var initializerStatements []ast.Handle
	receiver := tx.Factory().NewThisExpression()
	initializerStatements = tx.addInstanceMethodStatements(initializerStatements, privateMethodsAndAccessors, receiver)
	if !constructor.IsNil() {
		parameterProperties := core.Filter(instanceProperties, func(prop ast.Handle) bool {
			return ast.IsParameterPropertyDeclaration(tx.EmitContext().MostOriginal(prop), constructor)
		})
		nonParameterProperties := core.Filter(properties, func(prop ast.Handle) bool {
			return !ast.IsParameterPropertyDeclaration(tx.EmitContext().MostOriginal(prop), constructor)
		})
		initializerStatements = tx.addPropertyOrClassStaticBlockStatements(initializerStatements, parameterProperties, receiver)
		initializerStatements = tx.addPropertyOrClassStaticBlockStatements(initializerStatements, nonParameterProperties, receiver)
	} else {
		initializerStatements = tx.addPropertyOrClassStaticBlockStatements(initializerStatements, properties, receiver)
	}
	if !constructor.IsNil() && !constructor.Body().IsNil() {
		body := constructor.Body()
		for _, stmt := range body.Statements() {
			if ast.IsPrologueDirective(stmt) {
				statements = append(statements, stmt)
			} else {
				break
			}
		}
		statementOffset := len(statements)
		superPath := transformers.FindSuperStatementIndexPath(body.Statements(), statementOffset)
		if len(superPath) > 0 {
			statements = tx.transformConstructorBodyWorker(statements, body.Statements(), statementOffset, superPath, 0, initializerStatements, constructor)
		} else {
			for statementOffset < len(body.Statements()) {
				stmt := body.Statements()[statementOffset]
				orig := tx.EmitContext().MostOriginal(stmt)
				if ast.IsParameterPropertyDeclaration(orig, constructor) {
					statementOffset++
				} else {
					break
				}
			}
			statements = append(statements, initializerStatements...)
			visited := tx.Visitor().VisitSlice(body.Statements()[statementOffset:])
			statements = append(statements, visited...)
		}
	} else {
		if needsSyntheticConstructor {
			superCall := tx.Factory().NewExpressionStatement(tx.Factory().NewCallExpression(tx.Factory().NewKeywordExpression(ast.KindSuperKeyword), ast.Handle{}, 0, tx.Factory().NewList([]ast.Handle{tx.Factory().NewSpreadElement(tx.Factory().NewIdentifier("arguments"))}), ast.NodeFlagsNone))
			statements = append(statements, superCall)
		}
		statements = append(statements, initializerStatements...)
	}
	statements = tx.EmitContext().EndAndMergeVariableEnvironment(statements)
	if len(statements) == 0 && constructor.IsNil() {
		return ast.Handle{}
	}
	var multiLine bool
	if !constructor.IsNil() && !constructor.Body().IsNil() && constructor.Body().Store().ListLen(constructor.Body().BlockStatements()) >= len(statements) {
		multiLine = constructor.Body().BlockMultiLine()
	} else {
		multiLine = len(statements) > 0
	}
	var statementList ast.ListRef
	if !constructor.IsNil() && !constructor.Body().IsNil() {
		statementList = tx.Factory().List(constructor.Body().Store().ListLoc(constructor.Body().BlockStatements()), statements...)
	} else {
		statementList = tx.Factory().List(core.NewTextRange(container.Store().ListLoc(container.MemberList()).Pos(), container.Store().ListLoc(container.MemberList()).End()), statements...)
	}
	block := tx.Factory().NewBlock(statementList, multiLine)
	if !constructor.IsNil() && !constructor.Body().IsNil() {
		block.SetLoc(constructor.Body().Loc())
	}
	return block
}

func (tx *classFieldsTransformer) addPropertyOrClassStaticBlockStatements(statements []ast.Handle, properties []ast.Handle, receiver ast.Handle) []ast.Handle {
	for _, property := range properties {
		if ast.IsStatic(property) && !tx.shouldTransformPrivateElementsOrClassStaticBlocks {
			continue
		}
		statement := tx.transformPropertyOrClassStaticBlock(property, receiver)
		if !statement.IsNil() {
			statements = append(statements, statement)
		}
	}
	return statements
}
func (tx *classFieldsTransformer) transformPropertyOrClassStaticBlock(property ast.Handle, receiver ast.Handle) ast.Handle {
	var expression ast.Handle
	if ast.IsClassStaticBlockDeclaration(property) {
		expression = tx.setCurrentClassElementAnd(property, (*classFieldsTransformer).transformClassStaticBlockDeclaration, property)
	} else {
		expression = tx.transformProperty(property, receiver)
	}
	if expression.IsNil() {
		return ast.Handle{}
	}
	statement := tx.Factory().NewExpressionStatement(expression)
	tx.EmitContext().SetOriginal(statement, property)
	tx.EmitContext().AddEmitFlags(statement, tx.EmitContext().EmitFlags(property)&printer.EFNoComments)
	tx.EmitContext().SetCommentRange(statement, property.Loc())
	propertyOriginalNode := tx.EmitContext().MostOriginal(property)
	if ast.IsParameterDeclaration(propertyOriginalNode) {
		tx.EmitContext().SetSourceMapRange(statement, propertyOriginalNode.Loc())
		tx.EmitContext().AddEmitFlags(statement, printer.EFNoComments)
	} else {
		tx.EmitContext().SetSourceMapRange(statement, transformers.MoveRangePastModifiers(property))
	}
	tx.EmitContext().SetSyntheticLeadingComments(expression, nil)
	tx.EmitContext().SetSyntheticTrailingComments(expression, nil)
	if ast.HasAccessorModifier(propertyOriginalNode) {
		tx.EmitContext().AddEmitFlags(statement, printer.EFNoComments)
	}
	return statement
}

func (tx *classFieldsTransformer) generateInitializedPropertyExpressionsOrClassStaticBlock(propertiesOrClassStaticBlocks []ast.Handle, receiver ast.Handle) []ast.Handle {
	var expressions []ast.Handle
	for _, property := range propertiesOrClassStaticBlocks {
		var expression ast.Handle
		if ast.IsClassStaticBlockDeclaration(property) {
			expression = tx.setCurrentClassElementAnd(property, (*classFieldsTransformer).transformClassStaticBlockDeclaration, property)
		} else {
			expression = tx.transformProperty(property, receiver)
		}
		if expression.IsNil() {
			continue
		}
		tx.EmitContext().SetOriginalEx(expression, property, true)
		tx.EmitContext().AssignCommentAndSourceMapRanges(expression, property)
		expressions = append(expressions, expression)
	}
	return expressions
}

func (tx *classFieldsTransformer) transformProperty(property ast.Handle, receiver ast.Handle) ast.Handle {
	savedCurrentClassElement := tx.currentClassElement
	transformed := tx.transformPropertyWorker(property, receiver)
	if !transformed.IsNil() && ast.HasStaticModifier(property) {
		tx.EmitContext().AddEmitFlags(transformed, printer.EFNoLexicalThis)
	}
	if !transformed.IsNil() && ast.HasStaticModifier(property) && tx.lexicalEnvironment != nil && tx.lexicalEnvironment.data != nil && tx.lexicalEnvironment.data.facts != 0 {
		tx.EmitContext().SetOriginal(transformed, property)
		tx.EmitContext().SetSourceMapRange(transformed, tx.EmitContext().SourceMapRange(property.Name()))
	}
	tx.currentClassElement = savedCurrentClassElement
	return transformed
}
func (tx *classFieldsTransformer) transformPropertyWorker(property ast.Handle, receiver ast.Handle) ast.Handle {
	emitAssignment := !tx.compilerOptions.GetUseDefineForClassFields()
	if isNamedEvaluationAnd(tx.EmitContext(), property, tx.isAnonymousClassNeedingAssignedName) {
		property = transformNamedEvaluation(tx.EmitContext(), property, false, "")
	}
	propertyName := property.Name()
	if ast.HasAccessorModifier(property) {
		propertyName = tx.Factory().NewGeneratedPrivateNameForNodeEx(property.Name(), printer.AutoGenerateOptions{Suffix: "_accessor_storage"})
	} else if ast.IsComputedPropertyName(propertyName) && !transformers.IsSimpleInlineableExpression(propertyName.Expression()) {
		propertyName = tx.Factory().UpdateComputedPropertyName(propertyName, tx.Factory().NewGeneratedNameForNode(propertyName))
	}
	if ast.HasStaticModifier(property) {
		tx.currentClassElement = property
	}
	if ast.IsPrivateIdentifier(propertyName) && tx.shouldTransformClassElementToWeakMap(property) {
		info := tx.accessPrivateIdentifier(propertyName)
		if info != nil {
			if info.kind == printer.PrivateIdentifierKindField {
				if !info.isStatic {
					return createPrivateInstanceFieldInitializer(tx.Factory(), receiver, tx.Visitor().VisitNode(property.Initializer()), info.brandCheckIdentifier)
				}
				return createPrivateStaticFieldInitializer(tx.Factory(), info.variableName, tx.Visitor().VisitNode(property.Initializer()))
			}
			return ast.Handle{}
		} else {
			debug.Fail("Undeclared private name for property declaration.")
		}
	}
	if (ast.IsPrivateIdentifier(propertyName) || ast.HasStaticModifier(property)) && property.Initializer().IsNil() {
		return ast.Handle{}
	}
	if ast.HasAbstractModifier(tx.EmitContext().MostOriginal(property)) {
		return ast.Handle{}
	}
	initializer := tx.Visitor().VisitNode(property.Initializer())
	propertyOriginalNode := tx.EmitContext().MostOriginal(property)
	if ast.IsParameterPropertyDeclaration(propertyOriginalNode, propertyOriginalNode.Parent()) && ast.IsIdentifier(propertyName) {
		localName := tx.Factory().DeepCloneNode(propertyName)
		if !initializer.IsNil() {
			if ast.IsParenthesizedExpression(initializer) && ast.IsCommaExpression(initializer.Expression()) && tx.EmitContext().IsCallToHelper(initializer.Expression().BinaryExpressionLeft(), "__runInitializers") && ast.IsVoidExpression(initializer.Expression().BinaryExpressionRight()) && ast.IsNumericLiteral(initializer.Expression().BinaryExpressionRight().Expression()) {
				initializer = initializer.Expression().BinaryExpressionLeft()
			}
			initializer = tx.Factory().InlineExpressions([]ast.Handle{initializer, localName})
		} else {
			initializer = localName
		}
		tx.EmitContext().AddEmitFlags(propertyName, printer.EFNoComments|printer.EFNoSourceMap)
		tx.EmitContext().SetSourceMapRange(localName, propertyOriginalNode.Name().Loc())
		tx.EmitContext().AddEmitFlags(localName, printer.EFNoComments)
	} else if initializer.IsNil() {
		initializer = tx.Factory().NewVoidZeroExpression()
	}
	if emitAssignment || ast.IsPrivateIdentifier(propertyName) {
		memberAccess := createMemberAccessForPropertyName(tx.Factory(), tx.EmitContext(), receiver, propertyName, propertyName)
		tx.EmitContext().AddEmitFlags(memberAccess, printer.EFNoLeadingComments)
		return tx.Factory().NewAssignmentExpression(memberAccess, initializer)
	}
	var name ast.Handle
	if ast.IsComputedPropertyName(propertyName) {
		name = propertyName.Expression()
	} else if ast.IsIdentifier(propertyName) {
		name = tx.Factory().NewStringLiteral(propertyName.Text(), ast.TokenFlagsNone)
	} else {
		name = propertyName
	}
	descriptor := tx.Factory().NewObjectLiteralExpression(tx.Factory().NewList([]ast.Handle{tx.Factory().NewPropertyAssignment(0, tx.Factory().NewIdentifier("enumerable"), ast.Handle{}, ast.Handle{}, tx.Factory().NewTrueExpression()), tx.Factory().NewPropertyAssignment(0, tx.Factory().NewIdentifier("configurable"), ast.Handle{}, ast.Handle{}, tx.Factory().NewTrueExpression()), tx.Factory().NewPropertyAssignment(0, tx.Factory().NewIdentifier("writable"), ast.Handle{}, ast.Handle{}, tx.Factory().NewTrueExpression()), tx.Factory().NewPropertyAssignment(0, tx.Factory().NewIdentifier("value"), ast.Handle{}, ast.Handle{}, initializer)}), true)
	return tx.Factory().NewObjectDefinePropertyCall(receiver, name, descriptor)
}

func (tx *classFieldsTransformer) addInstanceMethodStatements(statements []ast.Handle, methods []ast.Handle, receiver ast.Handle) []ast.Handle {
	if !tx.shouldTransformPrivateElementsOrClassStaticBlocks || len(methods) == 0 {
		return statements
	}
	env := tx.getPrivateIdentifierEnvironment()
	weakSetName := env.data.weakSetName
	debug.Assert(!weakSetName.IsNil(), "weakSetName should be set in private identifier environment")
	return append(statements, tx.Factory().NewExpressionStatement(createPrivateInstanceMethodInitializer(tx.Factory(), receiver, weakSetName)))
}
func (tx *classFieldsTransformer) visitInvalidSuperProperty(node ast.Handle) ast.Handle {
	if ast.IsPropertyAccessExpression(node) {
		return tx.Factory().UpdatePropertyAccessExpression(node, tx.Factory().NewVoidZeroExpression(), ast.Handle{}, node.Name(), node.Flags())
	}
	return tx.Factory().UpdateElementAccessExpression(node, tx.Factory().NewVoidZeroExpression(), ast.Handle{}, tx.Visitor().VisitNode(node.ElementAccessExpressionArgumentExpression()), node.Flags())
}

func (tx *classFieldsTransformer) getPropertyNameExpressionIfNeeded(name ast.Handle, shouldHoist bool) ast.Handle {
	if !ast.IsComputedPropertyName(name) {
		return ast.Handle{}
	}
	cacheAssignment := findComputedPropertyNameCacheAssignment(tx.EmitContext(), name)
	savedLexicalEnvironment := tx.lexicalEnvironment
	savedInsideComputedPropertyName := tx.insideComputedPropertyName
	tx.insideComputedPropertyName = true
	if tx.lexicalEnvironment != nil && tx.lexicalEnvironment.previous != nil {
		tx.lexicalEnvironment = tx.lexicalEnvironment.previous
	}
	expression := tx.Visitor().VisitNode(name.Expression())
	tx.lexicalEnvironment = savedLexicalEnvironment
	tx.insideComputedPropertyName = savedInsideComputedPropertyName
	innerExpression := ast.SkipPartiallyEmittedExpressions(expression)
	inlinable := transformers.IsSimpleInlineableExpression(innerExpression)
	alreadyTransformed := !cacheAssignment.IsNil() || (ast.IsAssignmentExpression(innerExpression, true) && ast.IsIdentifier(innerExpression.BinaryExpressionLeft()) && transformers.IsGeneratedIdentifier(tx.EmitContext(), innerExpression.BinaryExpressionLeft()))
	if !alreadyTransformed && !inlinable && shouldHoist {
		generatedName := tx.Factory().NewGeneratedNameForNode(name)
		if tx.requiresBlockScopedVar() {
			tx.EmitContext().AddLexicalDeclaration(generatedName)
		} else {
			tx.EmitContext().AddVariableDeclaration(generatedName)
		}
		return tx.Factory().NewAssignmentExpression(generatedName, expression)
	}
	if inlinable || ast.IsIdentifier(innerExpression) {
		return ast.Handle{}
	}
	return expression
}
func (tx *classFieldsTransformer) startClassLexicalEnvironment() {
	tx.lexicalEnvironment = &classLexicalEnv{previous: tx.lexicalEnvironment}
}
func (tx *classFieldsTransformer) endClassLexicalEnvironment() {
	tx.lexicalEnvironment = tx.lexicalEnvironment.previous
}
func (tx *classFieldsTransformer) getClassLexicalEnvironment() *classLexicalEnvironment {
	debug.Assert(tx.lexicalEnvironment != nil)
	if tx.lexicalEnvironment.data == nil {
		tx.lexicalEnvironment.data = &classLexicalEnvironment{}
	}
	return tx.lexicalEnvironment.data
}
func (tx *classFieldsTransformer) getPrivateIdentifierEnvironment() *privateEnvironment {
	debug.Assert(tx.lexicalEnvironment != nil)
	if tx.lexicalEnvironment.privateEnv == nil {
		tx.lexicalEnvironment.privateEnv = &privateEnvironment{members: make(map[string]*privateIdentifierInfo)}
	}
	return tx.lexicalEnvironment.privateEnv
}
func (tx *classFieldsTransformer) addPendingExpressions(exprs ...ast.Handle) {
	tx.pendingExpressions = append(tx.pendingExpressions, exprs...)
}
func (tx *classFieldsTransformer) addPrivateIdentifierPropertyDeclarationToEnvironment(node ast.Handle, name ast.Handle) {
	lex := tx.getClassLexicalEnvironment()
	env := tx.getPrivateIdentifierEnvironment()
	isStatic := ast.HasStaticModifier(node)
	previousInfo, _ := tx.getPrivateIdentifier(env, name)
	isValid := !tx.isReservedPrivateName(name) && previousInfo == nil
	if isStatic {
		brandCheckIdentifier := lex.classThis
		if brandCheckIdentifier.IsNil() {
			brandCheckIdentifier = lex.classConstructor
		}
		variableName := tx.createHoistedVariableForPrivateName(name, "")
		tx.setPrivateIdentifier(env, name, &privateIdentifierInfo{kind: printer.PrivateIdentifierKindField, isStatic: true, brandCheckIdentifier: brandCheckIdentifier, variableName: variableName, isValid: isValid})
	} else {
		weakMapName := tx.createHoistedVariableForPrivateName(name, "")
		tx.setPrivateIdentifier(env, name, &privateIdentifierInfo{kind: printer.PrivateIdentifierKindField, isStatic: false, brandCheckIdentifier: weakMapName, isValid: isValid})
		tx.addPendingExpressions(tx.Factory().NewAssignmentExpression(weakMapName, tx.Factory().NewNewExpression(tx.Factory().NewIdentifier("WeakMap"), 0, tx.Factory().NewList(nil))))
	}
}
func (tx *classFieldsTransformer) addPrivateIdentifierMethodToEnvironment(name ast.Handle, lex *classLexicalEnvironment, env *privateEnvironment, isStatic bool, isValid bool) {
	methodName := tx.createHoistedVariableForPrivateName(name, "")
	var brandCheckIdentifier ast.Handle
	if isStatic {
		brandCheckIdentifier = lex.classThis
		if brandCheckIdentifier.IsNil() {
			brandCheckIdentifier = lex.classConstructor
		}
		debug.Assert(!brandCheckIdentifier.IsNil(), "classConstructor should be set in private identifier environment")
	} else {
		brandCheckIdentifier = env.data.weakSetName
	}
	tx.setPrivateIdentifier(env, name, &privateIdentifierInfo{kind: printer.PrivateIdentifierKindMethod, methodName: methodName, brandCheckIdentifier: brandCheckIdentifier, isStatic: isStatic, isValid: isValid})
}
func (tx *classFieldsTransformer) addPrivateIdentifierGetAccessorToEnvironment(name ast.Handle, lex *classLexicalEnvironment, env *privateEnvironment, isStatic bool, isValid bool, previousInfo *privateIdentifierInfo) {
	getterName := tx.createHoistedVariableForPrivateName(name, "_get")
	var brandCheckIdentifier ast.Handle
	if isStatic {
		brandCheckIdentifier = lex.classThis
		if brandCheckIdentifier.IsNil() {
			brandCheckIdentifier = lex.classConstructor
		}
		debug.Assert(!brandCheckIdentifier.IsNil(), "classConstructor should be set in private identifier environment")
	} else {
		brandCheckIdentifier = env.data.weakSetName
		debug.Assert(!brandCheckIdentifier.IsNil(), "weakSetName should be set in private identifier environment")
	}
	if previousInfo != nil && previousInfo.kind == printer.PrivateIdentifierKindAccessor && previousInfo.isStatic == isStatic && previousInfo.getterName.IsNil() {
		previousInfo.getterName = getterName
	} else {
		tx.setPrivateIdentifier(env, name, &privateIdentifierInfo{kind: printer.PrivateIdentifierKindAccessor, getterName: getterName, brandCheckIdentifier: brandCheckIdentifier, isStatic: isStatic, isValid: isValid})
	}
}
func (tx *classFieldsTransformer) addPrivateIdentifierSetAccessorToEnvironment(name ast.Handle, lex *classLexicalEnvironment, env *privateEnvironment, isStatic bool, isValid bool, previousInfo *privateIdentifierInfo) {
	setterName := tx.createHoistedVariableForPrivateName(name, "_set")
	var brandCheckIdentifier ast.Handle
	if isStatic {
		brandCheckIdentifier = lex.classThis
		if brandCheckIdentifier.IsNil() {
			brandCheckIdentifier = lex.classConstructor
		}
		debug.Assert(!brandCheckIdentifier.IsNil(), "classConstructor should be set in private identifier environment")
	} else {
		brandCheckIdentifier = env.data.weakSetName
		debug.Assert(!brandCheckIdentifier.IsNil(), "weakSetName should be set in private identifier environment")
	}
	if previousInfo != nil && previousInfo.kind == printer.PrivateIdentifierKindAccessor && previousInfo.isStatic == isStatic && previousInfo.setterName.IsNil() {
		previousInfo.setterName = setterName
	} else {
		tx.setPrivateIdentifier(env, name, &privateIdentifierInfo{kind: printer.PrivateIdentifierKindAccessor, setterName: setterName, brandCheckIdentifier: brandCheckIdentifier, isStatic: isStatic, isValid: isValid})
	}
}
func (tx *classFieldsTransformer) addPrivateIdentifierAutoAccessorToEnvironment(node ast.Handle, name ast.Handle, lex *classLexicalEnvironment, env *privateEnvironment, isStatic bool, isValid bool) {
	getterName := tx.createHoistedVariableForPrivateName(name, "_get")
	setterName := tx.createHoistedVariableForPrivateName(name, "_set")
	var brandCheckIdentifier ast.Handle
	if isStatic {
		brandCheckIdentifier = lex.classThis
		if brandCheckIdentifier.IsNil() {
			brandCheckIdentifier = lex.classConstructor
		}
		debug.Assert(!brandCheckIdentifier.IsNil(), "classConstructor should be set in private identifier environment")
	} else {
		brandCheckIdentifier = env.data.weakSetName
		debug.Assert(!brandCheckIdentifier.IsNil(), "weakSetName should be set in private identifier environment")
	}
	tx.setPrivateIdentifier(env, name, &privateIdentifierInfo{kind: printer.PrivateIdentifierKindAccessor, getterName: getterName, setterName: setterName, brandCheckIdentifier: brandCheckIdentifier, isStatic: isStatic, isValid: isValid})
}
func (tx *classFieldsTransformer) addPrivateIdentifierToEnvironment(node ast.Handle) {
	lex := tx.getClassLexicalEnvironment()
	env := tx.getPrivateIdentifierEnvironment()
	name := node.Name()
	isStatic := ast.HasStaticModifier(node)
	previousInfo, _ := tx.getPrivateIdentifier(env, name)
	isValid := !tx.isReservedPrivateName(name) && previousInfo == nil
	if ast.IsAutoAccessorPropertyDeclaration(node) {
		tx.addPrivateIdentifierAutoAccessorToEnvironment(node, name, lex, env, isStatic, isValid)
	} else if ast.IsPropertyDeclaration(node) {
		tx.addPrivateIdentifierPropertyDeclarationToEnvironment(node, name)
	} else if ast.IsMethodDeclaration(node) {
		tx.addPrivateIdentifierMethodToEnvironment(name, lex, env, isStatic, isValid)
	} else if ast.IsGetAccessorDeclaration(node) {
		tx.addPrivateIdentifierGetAccessorToEnvironment(name, lex, env, isStatic, isValid, previousInfo)
	} else if ast.IsSetAccessorDeclaration(node) {
		tx.addPrivateIdentifierSetAccessorToEnvironment(name, lex, env, isStatic, isValid, previousInfo)
	}
}
func (tx *classFieldsTransformer) setPrivateIdentifier(env *privateEnvironment, name ast.Handle, info *privateIdentifierInfo) {
	if tx.EmitContext().HasAutoGenerateInfo(name) {
		if env.generatedIdentifiers == nil {
			env.generatedIdentifiers = make(map[ast.Handle]*privateIdentifierInfo)
		}
		env.generatedIdentifiers[tx.EmitContext().GetNodeForGeneratedName(name)] = info
	} else {
		env.members[name.Text()] = info
	}
}
func (tx *classFieldsTransformer) getPrivateIdentifier(env *privateEnvironment, name ast.Handle) (*privateIdentifierInfo, bool) {
	if tx.EmitContext().HasAutoGenerateInfo(name) {
		info, ok := env.generatedIdentifiers[tx.EmitContext().GetNodeForGeneratedName(name)]
		return info, ok
	}
	info, ok := env.members[name.Text()]
	return info, ok
}
func (tx *classFieldsTransformer) createHoistedVariableForClass(nameText string, node ast.Handle, suffix string) ast.Handle {
	env := tx.getPrivateIdentifierEnvironment()
	var identifier ast.Handle
	if !env.data.className.IsNil() {
		prefix := "_" + env.data.className.Text() + "_"
		identifier = tx.Factory().NewUniqueNameEx(prefix+nameText, printer.AutoGenerateOptions{Flags: printer.GeneratedIdentifierFlagsOptimistic | printer.GeneratedIdentifierFlagsReservedInNestedScopes, Suffix: suffix})
	} else {
		identifier = tx.Factory().NewUniqueNameEx("_"+nameText, printer.AutoGenerateOptions{Flags: printer.GeneratedIdentifierFlagsOptimistic | printer.GeneratedIdentifierFlagsReservedInNestedScopes, Suffix: suffix})
	}
	if tx.requiresBlockScopedVar() {
		tx.EmitContext().AddLexicalDeclaration(identifier)
	} else {
		tx.EmitContext().AddVariableDeclaration(identifier)
	}
	return identifier
}
func (tx *classFieldsTransformer) createHoistedVariableForClassFromNode(name ast.Handle, suffix string) ast.Handle {
	env := tx.getPrivateIdentifierEnvironment()
	var prefix string
	if !env.data.className.IsNil() {
		prefix = "_" + env.data.className.Text() + "_"
	} else {
		prefix = "_"
	}
	identifier := tx.Factory().NewGeneratedNameForNodeEx(name, printer.AutoGenerateOptions{Flags: printer.GeneratedIdentifierFlagsOptimistic | printer.GeneratedIdentifierFlagsReservedInNestedScopes, Prefix: prefix, Suffix: suffix})
	if tx.requiresBlockScopedVar() {
		tx.EmitContext().AddLexicalDeclaration(identifier)
	} else {
		tx.EmitContext().AddVariableDeclaration(identifier)
	}
	return identifier
}
func (tx *classFieldsTransformer) createHoistedVariableForPrivateName(name ast.Handle, suffix string) ast.Handle {
	if tx.EmitContext().HasAutoGenerateInfo(name) {
		return tx.createHoistedVariableForClassFromNode(name, suffix)
	}
	text := name.Text()
	if len(text) >= 1 && text[0] == '#' {
		text = text[1:]
	}
	return tx.createHoistedVariableForClass(text, name, suffix)
}

func (tx *classFieldsTransformer) accessPrivateIdentifier(name ast.Handle) *privateIdentifierInfo {
	for env := tx.lexicalEnvironment; env != nil; env = env.previous {
		if env.privateEnv != nil {
			if info, ok := tx.getPrivateIdentifier(env.privateEnv, name); ok {
				if info.kind == printer.PrivateIdentifierKindUntransformed {
					return nil
				}
				return info
			}
		}
	}
	return nil
}
func (tx *classFieldsTransformer) wrapPrivateIdentifierForDestructuringTarget(node ast.Handle) ast.Handle {
	prop := node
	parameter := tx.Factory().NewGeneratedNameForNode(node)
	info := tx.accessPrivateIdentifier(prop.Name())
	if info == nil {
		return tx.Visitor().VisitEachChild(node)
	}
	receiver := prop.Expression()
	isThisOrSuperProperty := prop.Expression().Kind == ast.KindThisKeyword || prop.Expression().Kind == ast.KindSuperKeyword
	if isThisOrSuperProperty || !transformers.IsSimpleCopiableExpression(prop.Expression()) {
		receiver = tx.Factory().NewTempVariableEx(printer.AutoGenerateOptions{Flags: printer.GeneratedIdentifierFlagsReservedInNestedScopes})
		tx.EmitContext().AddVariableDeclaration(receiver)
		tx.pendingExpressions = append(tx.pendingExpressions, tx.Factory().NewAssignmentExpression(receiver, tx.Visitor().VisitNode(prop.Expression())))
	}
	assignExpr := tx.createPrivateIdentifierAssignment(info, receiver, parameter, ast.KindEqualsToken)
	return tx.Factory().NewAssignmentTargetWrapper(parameter, assignExpr)
}
func (tx *classFieldsTransformer) visitAssignmentElement(node ast.Handle) ast.Handle {
	if isNamedEvaluationAnd(tx.EmitContext(), node, tx.isAnonymousClassNeedingAssignedName) {
		node = transformNamedEvaluation(tx.EmitContext(), node, false, "")
	}
	if ast.IsAssignmentExpression(node, true) {
		left := tx.visitDestructuringAssignmentTarget(node.BinaryExpressionLeft())
		right := tx.Visitor().VisitNode(node.BinaryExpressionRight())
		return tx.Factory().UpdateBinaryExpression(node, 0, left, ast.Handle{}, node.BinaryExpressionOperatorToken(), right)
	}
	return tx.visitDestructuringAssignmentTarget(node)
}
func (tx *classFieldsTransformer) visitAssignmentRestElement(node ast.Handle) ast.Handle {
	spread := node
	if ast.IsLeftHandSideExpression(spread.Expression()) {
		expr := tx.visitDestructuringAssignmentTarget(spread.Expression())
		return tx.Factory().UpdateSpreadElement(spread, expr)
	}
	return tx.Visitor().VisitEachChild(node)
}
func (tx *classFieldsTransformer) visitArrayAssignmentElement(node ast.Handle) ast.Handle {
	if ast.IsArrayBindingOrAssignmentElement(node) {
		if ast.IsSpreadElement(node) {
			return tx.visitAssignmentRestElement(node)
		}
		if node.Kind != ast.KindOmittedExpression {
			return tx.visitAssignmentElement(node)
		}
	}
	return tx.Visitor().VisitEachChild(node)
}
func (tx *classFieldsTransformer) visitAssignmentProperty(node ast.Handle) ast.Handle {
	prop := node
	name := tx.Visitor().VisitNode(prop.Name())
	init := prop.Initializer()
	if ast.IsAssignmentExpression(init, true) {
		assignElem := tx.visitAssignmentElement(init)
		return tx.Factory().UpdatePropertyAssignment(prop, 0, name, ast.Handle{}, ast.Handle{}, assignElem)
	}
	if ast.IsLeftHandSideExpression(init) {
		target := tx.visitDestructuringAssignmentTarget(init)
		return tx.Factory().UpdatePropertyAssignment(prop, 0, name, ast.Handle{}, ast.Handle{}, target)
	}
	return tx.Visitor().VisitEachChild(node)
}
func (tx *classFieldsTransformer) visitShorthandAssignmentProperty(node ast.Handle) ast.Handle {
	if isNamedEvaluationAnd(tx.EmitContext(), node, tx.isAnonymousClassNeedingAssignedName) {
		node = transformNamedEvaluation(tx.EmitContext(), node, false, "")
	}
	return tx.Visitor().VisitEachChild(node)
}
func (tx *classFieldsTransformer) visitAssignmentRestProperty(node ast.Handle) ast.Handle {
	spread := node
	if ast.IsLeftHandSideExpression(spread.Expression()) {
		expr := tx.visitDestructuringAssignmentTarget(spread.Expression())
		return tx.Factory().UpdateSpreadAssignment(spread, expr)
	}
	return tx.Visitor().VisitEachChild(node)
}
func (tx *classFieldsTransformer) visitObjectAssignmentElement(node ast.Handle) ast.Handle {
	debug.Assert(!node.IsNil() && ast.IsObjectBindingOrAssignmentElement(node))
	if ast.IsSpreadAssignment(node) {
		return tx.visitAssignmentRestProperty(node)
	}
	if ast.IsShorthandPropertyAssignment(node) {
		return tx.visitShorthandAssignmentProperty(node)
	}
	if ast.IsPropertyAssignment(node) {
		return tx.visitAssignmentProperty(node)
	}
	return tx.Visitor().VisitEachChild(node)
}
func (tx *classFieldsTransformer) visitAssignmentPattern(node ast.Handle) ast.Handle {
	if ast.IsArrayLiteralExpression(node) {
		return tx.Factory().UpdateArrayLiteralExpression(node, tx.arrayAssignmentElementVisitor.VisitNodes(node.ArrayLiteralExpressionElements()), node.ArrayLiteralExpressionMultiLine())
	}
	return tx.Factory().UpdateObjectLiteralExpression(node, tx.objectAssignmentElementVisitor.VisitNodes(node.ObjectLiteralExpressionProperties()), node.ObjectLiteralExpressionMultiLine())
}
func createPrivateStaticFieldInitializer(factory *printer.NodeFactory, variableName ast.Handle, initializer ast.Handle) ast.Handle {
	if initializer.IsNil() {
		initializer = factory.NewVoidZeroExpression()
	}
	return factory.NewAssignmentExpression(variableName, factory.NewObjectLiteralExpression(factory.NewList([]ast.Handle{factory.NewPropertyAssignment(0, factory.NewIdentifier("value"), ast.Handle{}, ast.Handle{}, initializer)}), false))
}
func createPrivateInstanceFieldInitializer(factory *printer.NodeFactory, receiver ast.Handle, initializer ast.Handle, weakMapName ast.Handle) ast.Handle {
	if initializer.IsNil() {
		initializer = factory.NewVoidZeroExpression()
	}
	return factory.NewMethodCall(weakMapName, factory.NewIdentifier("set"), []ast.Handle{receiver, initializer})
}
func createPrivateInstanceMethodInitializer(factory *printer.NodeFactory, receiver ast.Handle, weakSetName ast.Handle) ast.Handle {
	return factory.NewMethodCall(weakSetName, factory.NewIdentifier("add"), []ast.Handle{receiver})
}
func (tx *classFieldsTransformer) isReservedPrivateName(node ast.Handle) bool {
	return !(ast.IsPrivateIdentifier(node) && tx.EmitContext().HasAutoGenerateInfo(node)) && node.Text() == "#constructor"
}
func isStaticPropertyDeclarationOrClassStaticBlock(node ast.Handle) bool {
	return ast.IsClassStaticBlockDeclaration(node) || (ast.IsPropertyDeclaration(node) && ast.HasStaticModifier(node))
}
func (tx *classFieldsTransformer) getProperties(node ast.Handle, requireInitializer bool, isStatic bool) []ast.Handle {
	var result []ast.Handle
	for _, member := range node.Members() {
		if ast.IsPropertyDeclaration(member) && (!requireInitializer || !member.Initializer().IsNil()) && ast.HasStaticModifier(member) == isStatic {
			result = append(result, member)
		}
	}
	return result
}
func (tx *classFieldsTransformer) getStaticPropertiesAndClassStaticBlock(node ast.Handle) []ast.Handle {
	var result []ast.Handle
	for _, member := range node.Members() {
		if ast.IsClassStaticBlockDeclaration(member) || (ast.IsPropertyDeclaration(member) && ast.HasStaticModifier(member)) {
			result = append(result, member)
		}
	}
	return result
}

func classHasClassThisAssignment(emitContext *printer.EmitContext, node ast.Handle) bool {
	for _, member := range node.Members() {
		if isClassThisAssignmentBlock(emitContext, member) {
			return true
		}
	}
	return false
}
func isNonStaticMethodOrAccessorWithPrivateName(member ast.Handle) bool {
	return !ast.IsStatic(member) && (ast.IsMethodOrAccessor(member) || ast.IsAutoAccessorPropertyDeclaration(member)) && ast.IsPrivateIdentifier(member.Name())
}
func createMemberAccessForPropertyName(factory *printer.NodeFactory, emitContext *printer.EmitContext, receiver ast.Handle, name ast.Handle, location ast.Handle) ast.Handle {
	if ast.IsComputedPropertyName(name) {
		expression := factory.NewElementAccessExpression(receiver, ast.Handle{}, name.Expression(), ast.NodeFlagsNone)
		expression.SetLoc(location.Loc())
		return expression
	}
	var expression ast.Handle
	if ast.IsIdentifier(name) || ast.IsPrivateIdentifier(name) {
		expression = factory.NewPropertyAccessExpression(receiver, ast.Handle{}, name, ast.NodeFlagsNone)
	} else {
		expression = factory.NewElementAccessExpression(receiver, ast.Handle{}, name, ast.NodeFlagsNone)
	}
	emitContext.SetCommentRange(expression, name.Loc())
	emitContext.SetSourceMapRange(expression, name.Loc())
	emitContext.AddEmitFlags(expression, printer.EFNoNestedSourceMaps)
	return expression
}
func (tx *classFieldsTransformer) createCallBinding(node ast.Handle) (thisArg ast.Handle, target ast.Handle) {
	if ast.IsSuperProperty(node) {
		return tx.Factory().NewThisExpression(), node
	}
	if ast.IsPropertyAccessExpression(node) {
		expr := node
		if shouldBeCapturedInTempVariable(expr.Expression()) {
			thisArg = tx.Factory().NewTempVariable()
			tx.EmitContext().AddVariableDeclaration(thisArg)
			target = tx.Factory().NewPropertyAccessExpression(tx.Factory().NewParenthesizedExpression(tx.Factory().NewAssignmentExpression(thisArg, expr.Expression())), ast.Handle{}, expr.Name(), ast.NodeFlagsNone)
			return thisArg, target
		}
		return expr.Expression(), node
	}
	thisArg = tx.Factory().NewVoidZeroExpression()
	target = node
	return thisArg, target
}
func shouldBeCapturedInTempVariable(node ast.Handle) bool {
	target := ast.SkipParentheses(node)
	switch target.Kind {
	case ast.KindIdentifier, ast.KindThisKeyword, ast.KindNumericLiteral, ast.KindBigIntLiteral, ast.KindStringLiteral:
		return false
	default:
		return true
	}
}
func (tx *classFieldsTransformer) createAccessorPropertyGetRedirector(node ast.Handle, modifiers ast.ListRef, name ast.Handle, receiver ast.Handle) ast.Handle {
	backingFieldName := tx.Factory().NewGeneratedPrivateNameForNodeEx(node.Name(), printer.AutoGenerateOptions{Suffix: "_accessor_storage"})
	returnExpr := tx.Factory().NewPropertyAccessExpression(receiver, ast.Handle{}, backingFieldName, ast.NodeFlagsNone)
	returnStmt := tx.Factory().NewReturnStatement(returnExpr)
	body := tx.Factory().NewBlock(tx.Factory().NewList([]ast.Handle{returnStmt}), false)
	return tx.Factory().NewGetAccessorDeclaration(modifiers, name, 0, tx.Factory().NewList([]ast.Handle{}), ast.Handle{}, ast.Handle{}, body)
}
func (tx *classFieldsTransformer) createAccessorPropertySetRedirector(node ast.Handle, modifiers ast.ListRef, name ast.Handle, receiver ast.Handle) ast.Handle {
	backingFieldName := tx.Factory().NewGeneratedPrivateNameForNodeEx(node.Name(), printer.AutoGenerateOptions{Suffix: "_accessor_storage"})
	valueParam := tx.Factory().NewParameterDeclaration(0, ast.Handle{}, tx.Factory().NewIdentifier("value"), ast.Handle{}, ast.Handle{}, ast.Handle{})
	assignExpr := tx.Factory().NewAssignmentExpression(tx.Factory().NewPropertyAccessExpression(receiver, ast.Handle{}, backingFieldName, ast.NodeFlagsNone), tx.Factory().NewIdentifier("value"))
	exprStmt := tx.Factory().NewExpressionStatement(assignExpr)
	body := tx.Factory().NewBlock(tx.Factory().NewList([]ast.Handle{exprStmt}), false)
	return tx.Factory().NewSetAccessorDeclaration(modifiers, name, 0, tx.Factory().NewList([]ast.Handle{valueParam}), ast.Handle{}, ast.Handle{}, body)
}

func flattenCommaList(node ast.Handle) iter.Seq[ast.Handle] {
	return func(yield func(ast.Handle) bool) {
		flattenCommaListWorker(node, yield)
	}
}
func flattenCommaListWorker(node ast.Handle, yield func(ast.Handle) bool) bool {
	if ast.IsParenthesizedExpression(node) && ast.NodeIsSynthesized(node) {
		return flattenCommaListWorker(node.Expression(), yield)
	} else if ast.IsCommaExpression(node) {
		return flattenCommaListWorker(node.BinaryExpressionLeft(), yield) && flattenCommaListWorker(node.BinaryExpressionRight(), yield)
	} else {
		return yield(node)
	}
}
func findComputedPropertyNameCacheAssignment(emitContext *printer.EmitContext, name ast.Handle) ast.Handle {
	node := name.Expression()
	for {
		node = ast.SkipOuterExpressions(node, 0)
		if ast.IsBinaryExpression(node) && node.BinaryExpressionOperatorToken().Kind == ast.KindCommaToken {
			node = node.BinaryExpressionRight()
			continue
		}
		if ast.IsAssignmentExpression(node, true) && ast.IsIdentifier(node.BinaryExpressionLeft()) {
			return node
		}
		break
	}
	return ast.Handle{}
}
func expandPreOrPostfixIncrementOrDecrementExpression(factory *printer.NodeFactory, emitContext *printer.EmitContext, node ast.Handle, expression ast.Handle, resultVariable ast.Handle) ast.Handle {
	var operator ast.Kind
	var operand ast.Handle
	if ast.IsPrefixUnaryExpression(node) {
		operator = node.PrefixUnaryExpressionOperator()
		operand = node.PrefixUnaryExpressionOperand()
	} else {
		operator = node.PostfixUnaryExpressionOperator()
		operand = node.PostfixUnaryExpressionOperand()
	}
	temp := factory.NewTempVariable()
	emitContext.AddVariableDeclaration(temp)
	expression = factory.NewAssignmentExpression(temp, expression)
	expression.SetLoc(operand.Loc())
	var operation ast.Handle
	if ast.IsPrefixUnaryExpression(node) {
		operation = factory.NewPrefixUnaryExpression(operator, temp)
	} else {
		operation = factory.NewPostfixUnaryExpression(temp, operator)
	}
	operation.SetLoc(node.Loc())
	if !resultVariable.IsNil() {
		operation = factory.NewAssignmentExpression(resultVariable, operation)
		operation.SetLoc(node.Loc())
	}
	expression = factory.NewCommaExpression(expression, operation)
	expression.SetLoc(node.Loc())
	if ast.IsPostfixUnaryExpression(node) {
		expression = factory.NewCommaExpression(expression, temp)
		expression.SetLoc(node.Loc())
	}
	return expression
}
