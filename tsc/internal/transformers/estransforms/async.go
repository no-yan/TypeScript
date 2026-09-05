package estransforms

import (
	"github.com/microsoft/TypeScript/tsc/internal/ast"
	"github.com/microsoft/TypeScript/tsc/internal/collections"
	"github.com/microsoft/TypeScript/tsc/internal/printer"
	"github.com/microsoft/TypeScript/tsc/internal/transformers"
)

type asyncContextFlags int

const (
	asyncContextNonTopLevel asyncContextFlags = 1 << iota
	asyncContextHasLexicalThis
)

type lexicalArgumentsInfo struct {
	binding ast.Handle
	used    bool
}
type asyncTransformer struct {
	transformers.Transformer
	superAccessState
	contextFlags                    asyncContextFlags
	enclosingFunctionParameterNames *collections.Set[string]
	lexicalArguments                lexicalArgumentsInfo
	asyncBodyVisitor                *ast.HandleVisitor
	fallbackNodeVisitor             *ast.HandleVisitor
}

func newAsyncTransformer(opts *transformers.TransformOptions) *transformers.Transformer {
	tx := &asyncTransformer{}
	result := tx.NewTransformer(tx.visit, opts.Context)
	tx.initSuperAccessVisitor(tx.EmitContext(), tx.Factory())
	tx.asyncBodyVisitor = tx.EmitContext().NewNodeVisitor(tx.visitAsyncBodyNode)
	tx.fallbackNodeVisitor = tx.EmitContext().NewNodeVisitor(tx.visitFallback)
	return result
}
func (tx *asyncTransformer) visitSourceFile(node ast.Handle) ast.Handle {
	if ast.GetSourceFileOfNode(node) != nil && ast.GetSourceFileOfNode(node).IsDeclarationFile {
		return node
	}
	tx.setContextFlag(asyncContextNonTopLevel, false)
	tx.setContextFlag(asyncContextHasLexicalThis, false)
	visited := tx.Visitor().VisitEachChild(node)
	tx.EmitContext().AddEmitHelper(visited, tx.EmitContext().ReadEmitHelpers()...)
	return visited
}
func (tx *asyncTransformer) setContextFlag(flag asyncContextFlags, val bool) {
	if val {
		tx.contextFlags |= flag
	} else {
		tx.contextFlags &^= flag
	}
}
func (tx *asyncTransformer) inContext(flags asyncContextFlags) bool {
	return tx.contextFlags&flags != 0
}
func (tx *asyncTransformer) inTopLevelContext() bool {
	return !tx.inContext(asyncContextNonTopLevel)
}
func (tx *asyncTransformer) inHasLexicalThisContext() bool {
	return tx.inContext(asyncContextHasLexicalThis)
}
func (tx *asyncTransformer) doWithContext(flags asyncContextFlags, cb func(*asyncTransformer, ast.Handle) ast.Handle, node ast.Handle) ast.Handle {
	flagsToSet := flags & ^tx.contextFlags
	if flagsToSet != 0 {
		tx.setContextFlag(flagsToSet, true)
		result := cb(tx, node)
		tx.setContextFlag(flagsToSet, false)
		return result
	}
	return cb(tx, node)
}
func (tx *asyncTransformer) visitDefault(node ast.Handle) ast.Handle {
	return tx.Visitor().VisitEachChild(node)
}
func (tx *asyncTransformer) fallbackVisitor(node ast.Handle) ast.Handle {
	if tx.capturedSuperProperties == nil && tx.lexicalArguments.binding.IsNil() {
		return node
	}
	tx.trackSuperAccess(node)
	switch node.Kind {
	case ast.KindFunctionExpression, ast.KindFunctionDeclaration, ast.KindMethodDeclaration, ast.KindGetAccessor, ast.KindSetAccessor, ast.KindConstructor:
		return node
	case ast.KindParameter, ast.KindBindingElement, ast.KindVariableDeclaration:
	case ast.KindIdentifier:
		if !tx.lexicalArguments.binding.IsNil() && node.Text() == "arguments" && !ast.IsIdentifierName(node) && !ast.IsLabelName(node) {
			tx.lexicalArguments.used = true
			return tx.lexicalArguments.binding
		}
	}
	return tx.fallbackNodeVisitor.VisitEachChild(node)
}
func (tx *asyncTransformer) visitFallback(node ast.Handle) ast.Handle {
	return tx.fallbackVisitor(node)
}
func (tx *asyncTransformer) visit(node ast.Handle) ast.Handle {
	if tx.EmitContext().EmitFlags(node)&printer.EFNoLexicalThis != 0 && tx.inHasLexicalThisContext() {
		tx.setContextFlag(asyncContextHasLexicalThis, false)
		defer tx.setContextFlag(asyncContextHasLexicalThis, true)
	}
	if node.SubtreeFacts()&(ast.SubtreeContainsAnyAwait|ast.SubtreeContainsAwait) == 0 {
		return tx.fallbackVisitor(node)
	}
	tx.trackSuperAccess(node)
	switch node.Kind {
	case ast.KindAsyncKeyword:
		return ast.Handle{}
	case ast.KindSourceFile:
		return tx.visitSourceFile(node)
	case ast.KindAwaitExpression:
		return tx.visitAwaitExpression(node)
	case ast.KindMethodDeclaration:
		return tx.doWithContext(asyncContextNonTopLevel|asyncContextHasLexicalThis, (*asyncTransformer).visitMethodDeclaration, node)
	case ast.KindFunctionDeclaration:
		return tx.doWithContext(asyncContextNonTopLevel|asyncContextHasLexicalThis, (*asyncTransformer).visitFunctionDeclaration, node)
	case ast.KindFunctionExpression:
		return tx.doWithContext(asyncContextNonTopLevel|asyncContextHasLexicalThis, (*asyncTransformer).visitFunctionExpression, node)
	case ast.KindArrowFunction:
		return tx.doWithContext(asyncContextNonTopLevel, (*asyncTransformer).visitArrowFunction, node)
	case ast.KindGetAccessor:
		return tx.doWithContext(asyncContextNonTopLevel|asyncContextHasLexicalThis, (*asyncTransformer).visitGetAccessorDeclaration, node)
	case ast.KindSetAccessor:
		return tx.doWithContext(asyncContextNonTopLevel|asyncContextHasLexicalThis, (*asyncTransformer).visitSetAccessorDeclaration, node)
	case ast.KindConstructor:
		return tx.doWithContext(asyncContextNonTopLevel|asyncContextHasLexicalThis, (*asyncTransformer).visitConstructorDeclaration, node)
	case ast.KindClassDeclaration, ast.KindClassExpression:
		return tx.doWithContext(asyncContextNonTopLevel|asyncContextHasLexicalThis, (*asyncTransformer).visitDefault, node)
	default:
		return tx.Visitor().VisitEachChild(node)
	}
}
func (tx *asyncTransformer) visitAsyncBodyNode(node ast.Handle) ast.Handle {
	if isNodeWithPossibleHoistedDeclaration(node) {
		switch node.Kind {
		case ast.KindVariableStatement:
			return tx.visitVariableStatementInAsyncBody(node)
		case ast.KindForStatement:
			return tx.visitForStatementInAsyncBody(node)
		case ast.KindForInStatement:
			return tx.visitForInStatementInAsyncBody(node)
		case ast.KindForOfStatement:
			return tx.visitForOfStatementInAsyncBody(node)
		case ast.KindCatchClause:
			return tx.visitCatchClauseInAsyncBody(node)
		case ast.KindBlock, ast.KindSwitchStatement, ast.KindCaseBlock, ast.KindCaseClause, ast.KindDefaultClause, ast.KindTryStatement, ast.KindDoStatement, ast.KindWhileStatement, ast.KindIfStatement, ast.KindWithStatement, ast.KindLabeledStatement:
			return tx.asyncBodyVisitor.VisitEachChild(node)
		}
	}
	return tx.visit(node)
}
func (tx *asyncTransformer) visitCatchClauseInAsyncBody(node ast.Handle) ast.Handle {
	catchClauseNames := &collections.Set[string]{}
	if !node.VariableDeclaration().IsNil() {
		tx.recordDeclarationName(node.VariableDeclaration(), catchClauseNames)
	}
	var catchClauseUnshadowedNames *collections.Set[string]
	for escapedName := range catchClauseNames.Keys() {
		if tx.enclosingFunctionParameterNames != nil && tx.enclosingFunctionParameterNames.Has(escapedName) {
			if catchClauseUnshadowedNames == nil {
				catchClauseUnshadowedNames = tx.enclosingFunctionParameterNames.Clone()
			}
			catchClauseUnshadowedNames.Delete(escapedName)
		}
	}
	if catchClauseUnshadowedNames != nil {
		savedEnclosingFunctionParameterNames := tx.enclosingFunctionParameterNames
		tx.enclosingFunctionParameterNames = catchClauseUnshadowedNames
		result := tx.asyncBodyVisitor.VisitEachChild(node)
		tx.enclosingFunctionParameterNames = savedEnclosingFunctionParameterNames
		return result
	}
	return tx.asyncBodyVisitor.VisitEachChild(node)
}
func (tx *asyncTransformer) visitVariableStatementInAsyncBody(node ast.Handle) ast.Handle {
	declList := node.VariableStatementDeclarationList()
	if tx.isVariableDeclarationListWithCollidingName(declList) {
		expression := tx.visitVariableDeclarationListWithCollidingNames(declList, false)
		if !expression.IsNil() {
			return tx.Factory().NewExpressionStatement(expression)
		}
		return ast.Handle{}
	}
	return tx.Visitor().VisitEachChild(node)
}
func (tx *asyncTransformer) visitForInStatementInAsyncBody(node ast.Handle) ast.Handle {
	var visitedInitializer ast.Handle
	if tx.isVariableDeclarationListWithCollidingName(node.Initializer()) {
		visitedInitializer = tx.visitVariableDeclarationListWithCollidingNames(node.Initializer(), true)
	} else {
		visitedInitializer = tx.Visitor().VisitNode(node.Initializer())
	}
	return tx.Factory().UpdateForInOrOfStatement(node, ast.Handle{}, visitedInitializer, tx.Visitor().VisitNode(node.Expression()), tx.asyncBodyVisitor.VisitEmbeddedStatement(node.Statement()))
}
func (tx *asyncTransformer) visitForOfStatementInAsyncBody(node ast.Handle) ast.Handle {
	var visitedInitializer ast.Handle
	if tx.isVariableDeclarationListWithCollidingName(node.Initializer()) {
		visitedInitializer = tx.visitVariableDeclarationListWithCollidingNames(node.Initializer(), true)
	} else {
		visitedInitializer = tx.Visitor().VisitNode(node.Initializer())
	}
	return tx.Factory().UpdateForInOrOfStatement(node, tx.Visitor().VisitNode(node.AwaitModifier()), visitedInitializer, tx.Visitor().VisitNode(node.Expression()), tx.asyncBodyVisitor.VisitEmbeddedStatement(node.Statement()))
}
func (tx *asyncTransformer) visitForStatementInAsyncBody(node ast.Handle) ast.Handle {
	initializer := node.Initializer()
	var visitedInitializer ast.Handle
	if !initializer.IsNil() && tx.isVariableDeclarationListWithCollidingName(initializer) {
		visitedInitializer = tx.visitVariableDeclarationListWithCollidingNames(initializer, false)
	} else {
		visitedInitializer = tx.Visitor().VisitNode(node.Initializer())
	}
	return tx.Factory().UpdateForStatement(node, visitedInitializer, tx.Visitor().VisitNode(node.Condition()), tx.Visitor().VisitNode(node.Incrementor()), tx.asyncBodyVisitor.VisitEmbeddedStatement(node.Statement()))
}

func (tx *asyncTransformer) visitAwaitExpression(node ast.Handle) ast.Handle {
	if tx.inTopLevelContext() {
		return tx.Visitor().VisitEachChild(node)
	}
	yieldExpr := tx.Factory().NewYieldExpression(ast.Handle{}, tx.Visitor().VisitNode(node.Expression()))
	yieldExpr.SetLoc(node.Loc())
	tx.EmitContext().SetOriginal(yieldExpr, node)
	return yieldExpr
}
func (tx *asyncTransformer) visitConstructorDeclaration(node ast.Handle) ast.Handle {
	decl := node
	savedLexicalArguments := tx.lexicalArguments
	tx.lexicalArguments = lexicalArgumentsInfo{}
	updated := tx.Factory().UpdateConstructorDeclaration(decl, tx.Visitor().VisitModifiers(decl.Modifiers()), 0, tx.EmitContext().VisitParameters(decl.ParameterList(), tx.Visitor()), ast.Handle{}, ast.Handle{}, tx.transformMethodBody(node))
	tx.lexicalArguments = savedLexicalArguments
	return updated
}

func (tx *asyncTransformer) visitMethodDeclaration(node ast.Handle) ast.Handle {
	decl := node
	functionFlags := ast.GetFunctionFlags(node)
	savedLexicalArguments := tx.lexicalArguments
	tx.lexicalArguments = lexicalArgumentsInfo{}
	var parameters ast.ListRef
	var body ast.Handle
	if functionFlags&ast.FunctionFlagsAsync != 0 {
		parameters = tx.transformAsyncFunctionParameterList(node)
		body = tx.transformAsyncFunctionBody(node, parameters)
	} else {
		parameters = tx.EmitContext().VisitParameters(decl.ParameterList(), tx.Visitor())
		body = tx.transformMethodBody(node)
	}
	updated := tx.Factory().UpdateMethodDeclaration(decl, tx.Visitor().VisitModifiers(decl.Modifiers()), decl.AsteriskToken(), decl.Name(), ast.Handle{}, 0, parameters, ast.Handle{}, ast.Handle{}, body)
	tx.lexicalArguments = savedLexicalArguments
	return updated
}
func (tx *asyncTransformer) visitGetAccessorDeclaration(node ast.Handle) ast.Handle {
	decl := node
	savedLexicalArguments := tx.lexicalArguments
	tx.lexicalArguments = lexicalArgumentsInfo{}
	updated := tx.Factory().UpdateGetAccessorDeclaration(decl, tx.Visitor().VisitModifiers(decl.Modifiers()), decl.Name(), 0, tx.EmitContext().VisitParameters(decl.ParameterList(), tx.Visitor()), ast.Handle{}, ast.Handle{}, tx.transformMethodBody(node))
	tx.lexicalArguments = savedLexicalArguments
	return updated
}
func (tx *asyncTransformer) visitSetAccessorDeclaration(node ast.Handle) ast.Handle {
	decl := node
	savedLexicalArguments := tx.lexicalArguments
	tx.lexicalArguments = lexicalArgumentsInfo{}
	updated := tx.Factory().UpdateSetAccessorDeclaration(decl, tx.Visitor().VisitModifiers(decl.Modifiers()), decl.Name(), 0, tx.EmitContext().VisitParameters(decl.ParameterList(), tx.Visitor()), ast.Handle{}, ast.Handle{}, tx.transformMethodBody(node))
	tx.lexicalArguments = savedLexicalArguments
	return updated
}

func (tx *asyncTransformer) visitFunctionDeclaration(node ast.Handle) ast.Handle {
	decl := node
	functionFlags := ast.GetFunctionFlags(node)
	savedLexicalArguments := tx.lexicalArguments
	tx.lexicalArguments = lexicalArgumentsInfo{}
	var parameters ast.ListRef
	var body ast.Handle
	if functionFlags&ast.FunctionFlagsAsync != 0 {
		parameters = tx.transformAsyncFunctionParameterList(node)
		body = tx.transformAsyncFunctionBody(node, parameters)
	} else {
		parameters = tx.EmitContext().VisitParameters(decl.ParameterList(), tx.Visitor())
		body = tx.EmitContext().VisitFunctionBody(decl.Body(), tx.Visitor())
	}
	updated := tx.Factory().UpdateFunctionDeclaration(decl, tx.Visitor().VisitModifiers(decl.Modifiers()), decl.AsteriskToken(), tx.Visitor().VisitNode(decl.Name()), 0, parameters, ast.Handle{}, ast.Handle{}, body)
	tx.lexicalArguments = savedLexicalArguments
	return updated
}

func (tx *asyncTransformer) visitFunctionExpression(node ast.Handle) ast.Handle {
	decl := node
	functionFlags := ast.GetFunctionFlags(node)
	savedLexicalArguments := tx.lexicalArguments
	tx.lexicalArguments = lexicalArgumentsInfo{}
	var parameters ast.ListRef
	var body ast.Handle
	if functionFlags&ast.FunctionFlagsAsync != 0 {
		parameters = tx.transformAsyncFunctionParameterList(node)
		body = tx.transformAsyncFunctionBody(node, parameters)
	} else {
		parameters = tx.EmitContext().VisitParameters(decl.ParameterList(), tx.Visitor())
		body = tx.EmitContext().VisitFunctionBody(decl.Body(), tx.Visitor())
	}
	updated := tx.Factory().UpdateFunctionExpression(decl, tx.Visitor().VisitModifiers(decl.Modifiers()), decl.AsteriskToken(), tx.Visitor().VisitNode(decl.Name()), 0, parameters, ast.Handle{}, ast.Handle{}, body)
	tx.lexicalArguments = savedLexicalArguments
	return updated
}

func (tx *asyncTransformer) visitArrowFunction(node ast.Handle) ast.Handle {
	if tx.EmitContext().EmitFlags(node)&printer.EFNoLexicalArguments != 0 {
		savedLexicalArguments := tx.lexicalArguments
		tx.lexicalArguments = lexicalArgumentsInfo{}
		defer func() {
			tx.lexicalArguments = savedLexicalArguments
		}()
	}
	decl := node
	functionFlags := ast.GetFunctionFlags(node)
	var parameters ast.ListRef
	var body ast.Handle
	if functionFlags&ast.FunctionFlagsAsync != 0 {
		parameters = tx.transformAsyncFunctionParameterList(node)
		body = tx.transformAsyncFunctionBody(node, parameters)
	} else {
		parameters = tx.EmitContext().VisitParameters(decl.ParameterList(), tx.Visitor())
		body = tx.EmitContext().VisitFunctionBody(decl.Body(), tx.Visitor())
	}
	return tx.Factory().UpdateArrowFunction(decl, tx.Visitor().VisitModifiers(decl.Modifiers()), 0, parameters, ast.Handle{}, ast.Handle{}, decl.EqualsGreaterThanToken(), body)
}
func (tx *asyncTransformer) recordDeclarationName(node ast.Handle, names *collections.Set[string]) {
	name := node.Name()
	if name.IsNil() {
		return
	}
	if ast.IsIdentifier(name) {
		names.Add(name.Text())
	} else if ast.IsBindingPattern(name) {
		for _, element := range name.Store().ListSlice(name.BindingPatternElements()).All() {
			if !ast.IsOmittedExpression(element) {
				tx.recordDeclarationName(element, names)
			}
		}
	}
}
func (tx *asyncTransformer) isVariableDeclarationListWithCollidingName(node ast.Handle) bool {
	return !node.IsNil() && ast.IsVariableDeclarationList(node) && node.Flags()&ast.NodeFlagsBlockScoped == 0 && node.DeclarationsSeq().Some(tx.collidesWithParameterName)
}
func (tx *asyncTransformer) visitVariableDeclarationListWithCollidingNames(node ast.Handle, hasReceiver bool) ast.Handle {
	tx.hoistVariableDeclarationList(node)
	var variables []ast.Handle
	for _, decl := range node.Declarations() {
		if !decl.VariableDeclarationInitializer().IsNil() {
			variables = append(variables, decl)
		}
	}
	if len(variables) == 0 {
		if hasReceiver {
			name := node.Declarations()[0].Name()
			var target ast.Handle
			if ast.IsBindingPattern(name) {
				target = transformers.ConvertBindingPatternToAssignmentPattern(tx.EmitContext(), name)
			} else {
				target = name
			}
			return tx.Visitor().VisitNode(target)
		}
		return ast.Handle{}
	}
	var expressions []ast.Handle
	for _, variable := range variables {
		expressions = append(expressions, tx.transformInitializedVariable(variable))
	}
	return tx.Factory().InlineExpressions(expressions)
}
func (tx *asyncTransformer) hoistVariableDeclarationList(node ast.Handle) {
	for _, decl := range node.Declarations() {
		tx.hoistVariable(decl)
	}
}
func (tx *asyncTransformer) hoistVariable(node ast.Handle) {
	name := node.Name()
	if name.IsNil() {
		return
	}
	if ast.IsIdentifier(name) {
		tx.EmitContext().AddVariableDeclaration(name)
	} else if ast.IsBindingPattern(name) {
		for _, element := range name.Store().ListSlice(name.BindingPatternElements()).All() {
			if !ast.IsOmittedExpression(element) {
				tx.hoistVariable(element)
			}
		}
	}
}
func (tx *asyncTransformer) transformInitializedVariable(node ast.Handle) ast.Handle {
	var target ast.Handle
	if ast.IsBindingPattern(node.Name()) {
		target = transformers.ConvertBindingPatternToAssignmentPattern(tx.EmitContext(), node.Name())
	} else {
		target = node.Name()
	}
	converted := tx.Factory().NewAssignmentExpression(target, node.Initializer())
	tx.EmitContext().SetSourceMapRange(converted, node.Loc())
	return tx.Visitor().VisitNode(converted)
}
func (tx *asyncTransformer) collidesWithParameterName(node ast.Handle) bool {
	name := node.Name()
	if name.IsNil() {
		return false
	}
	if ast.IsIdentifier(name) {
		return tx.enclosingFunctionParameterNames != nil && tx.enclosingFunctionParameterNames.Has(name.Text())
	}
	if ast.IsBindingPattern(name) {
		for _, element := range name.Store().ListSlice(name.BindingPatternElements()).All() {
			if !ast.IsOmittedExpression(element) && tx.collidesWithParameterName(element) {
				return true
			}
		}
	}
	return false
}
func (tx *asyncTransformer) transformMethodBody(node ast.Handle) ast.Handle {
	savedCapturedSuperProperties := tx.capturedSuperProperties
	savedHasSuperElementAccess := tx.hasSuperElementAccess
	savedHasSuperPropertyAssignment := tx.hasSuperPropertyAssignment
	savedSuperBinding := tx.superBinding
	savedSuperIndexBinding := tx.superIndexBinding
	tx.capturedSuperProperties = &collections.OrderedSet[string]{}
	tx.hasSuperElementAccess = false
	tx.hasSuperPropertyAssignment = false
	tx.superBinding = tx.Factory().NewUniqueNameEx("_super", printer.AutoGenerateOptions{Flags: printer.GeneratedIdentifierFlagsOptimistic | printer.GeneratedIdentifierFlagsFileLevel})
	tx.superIndexBinding = tx.Factory().NewUniqueNameEx("_superIndex", printer.AutoGenerateOptions{Flags: printer.GeneratedIdentifierFlagsOptimistic | printer.GeneratedIdentifierFlagsFileLevel})
	tx.EmitContext().StartVariableEnvironment()
	updated := tx.EmitContext().VisitFunctionBody(node.Body(), tx.Visitor())
	emitSuperHelpers := (tx.capturedSuperProperties.Size() > 0 || tx.hasSuperElementAccess) && (ast.GetFunctionFlags(tx.getOriginalIfFunctionLike(node))&ast.FunctionFlagsAsyncGenerator) != ast.FunctionFlagsAsyncGenerator
	if emitSuperHelpers {
		if tx.capturedSuperProperties.Size() > 0 {
			tx.EmitContext().AddInitializationStatement(tx.createSuperAccessVariableStatement())
		}
	}
	mergedStatements := tx.EmitContext().EndAndMergeVariableEnvironmentList(updated.StatementList())
	if emitSuperHelpers && tx.hasSuperElementAccess && !updated.BlockMultiLine() {
		newBlock := tx.Factory().NewBlock(mergedStatements, true)
		newBlock.SetLoc(updated.Loc())
		updated = newBlock
	} else {
		updated = tx.Factory().UpdateBlock(updated, mergedStatements, updated.BlockMultiLine())
	}
	if emitSuperHelpers && tx.hasSuperElementAccess {
		if tx.hasSuperPropertyAssignment {
			tx.EmitContext().AddEmitHelper(updated, printer.AdvancedAsyncSuperHelper)
		} else {
			tx.EmitContext().AddEmitHelper(updated, printer.AsyncSuperHelper)
		}
	}
	tx.capturedSuperProperties = savedCapturedSuperProperties
	tx.hasSuperElementAccess = savedHasSuperElementAccess
	tx.hasSuperPropertyAssignment = savedHasSuperPropertyAssignment
	tx.superBinding = savedSuperBinding
	tx.superIndexBinding = savedSuperIndexBinding
	return updated
}
func (tx *asyncTransformer) createCaptureArgumentsStatement() ast.Handle {
	variable := tx.Factory().NewVariableDeclaration(tx.lexicalArguments.binding, ast.Handle{}, ast.Handle{}, tx.Factory().NewIdentifier("arguments"))
	declList := tx.Factory().NewVariableDeclarationList(tx.Factory().NewList([]ast.Handle{variable}), ast.NodeFlagsNone)
	statement := tx.Factory().NewVariableStatement(0, declList)
	tx.EmitContext().AddEmitFlags(statement, printer.EFStartOnNewLine|printer.EFCustomPrologue)
	return statement
}
func (tx *asyncTransformer) transformAsyncFunctionParameterList(node ast.Handle) ast.ListRef {
	if isSimpleParameterList(node.Parameters()) {
		return tx.EmitContext().VisitParameters(node.ParameterList(), tx.Visitor())
	}
	var newParameters []ast.Handle
	for _, parameter := range node.Parameters() {
		param := parameter
		if !param.Initializer().IsNil() || !param.DotDotDotToken().IsNil() {
			if node.Kind == ast.KindArrowFunction {
				restParameter := tx.Factory().NewParameterDeclaration(0, tx.Factory().NewToken(ast.KindDotDotDotToken), tx.Factory().NewUniqueNameEx("args", printer.AutoGenerateOptions{Flags: printer.GeneratedIdentifierFlagsReservedInNestedScopes}), ast.Handle{}, ast.Handle{}, ast.Handle{})
				newParameters = append(newParameters, restParameter)
			}
			break
		}
		newParameter := tx.Factory().NewParameterDeclaration(0, ast.Handle{}, tx.Factory().NewGeneratedNameForNodeEx(param.Name(), printer.AutoGenerateOptions{Flags: printer.GeneratedIdentifierFlagsReservedInNestedScopes}), ast.Handle{}, ast.Handle{}, ast.Handle{})
		newParameters = append(newParameters, newParameter)
	}
	newParametersArray := tx.Factory().List(node.Store().ListLoc(node.ParameterList()), newParameters...)
	return newParametersArray
}
func (tx *asyncTransformer) transformAsyncFunctionBody(node ast.Handle, outerParameters ast.ListRef) ast.Handle {
	isArrow := node.Kind == ast.KindArrowFunction
	savedCapturedSuperProperties := tx.capturedSuperProperties
	savedHasSuperElementAccess := tx.hasSuperElementAccess
	savedHasSuperPropertyAssignment := tx.hasSuperPropertyAssignment
	savedSuperBinding := tx.superBinding
	savedSuperIndexBinding := tx.superIndexBinding
	if !isArrow {
		tx.capturedSuperProperties = &collections.OrderedSet[string]{}
		tx.hasSuperElementAccess = false
		tx.hasSuperPropertyAssignment = false
		tx.superBinding = tx.Factory().NewUniqueNameEx("_super", printer.AutoGenerateOptions{Flags: printer.GeneratedIdentifierFlagsOptimistic | printer.GeneratedIdentifierFlagsFileLevel})
		tx.superIndexBinding = tx.Factory().NewUniqueNameEx("_superIndex", printer.AutoGenerateOptions{Flags: printer.GeneratedIdentifierFlagsOptimistic | printer.GeneratedIdentifierFlagsFileLevel})
	}
	innerParameters := (ast.ListRef)(0)
	if !isSimpleParameterList(node.Parameters()) {
		innerParameters = tx.EmitContext().VisitParameters(node.ParameterList(), tx.Visitor())
	}
	savedLexicalArguments := tx.lexicalArguments
	captureLexicalArguments := tx.lexicalArguments.binding.IsNil()
	if captureLexicalArguments {
		tx.lexicalArguments = lexicalArgumentsInfo{binding: tx.Factory().NewUniqueName("arguments")}
	}
	var argumentsExpression ast.Handle
	if innerParameters != 0 {
		if isArrow {
			var parameterBindings []ast.Handle
			outerLen := node.Store().ListLen(outerParameters)
			for i, param := range node.Parameters() {
				if i >= outerLen {
					break
				}
				originalParameter := param
				outerParameter := node.Store().ListAt(outerParameters, i)
				if !originalParameter.Initializer().IsNil() || !originalParameter.DotDotDotToken().IsNil() {
					parameterBindings = append(parameterBindings, tx.Factory().NewSpreadElement(outerParameter.Name()))
					break
				}
				parameterBindings = append(parameterBindings, outerParameter.Name())
			}
			argumentsExpression = tx.Factory().NewArrayLiteralExpression(tx.Factory().NewList(parameterBindings), false)
		} else {
			argumentsExpression = tx.Factory().NewIdentifier("arguments")
		}
	}
	savedEnclosingFunctionParameterNames := tx.enclosingFunctionParameterNames
	tx.enclosingFunctionParameterNames = &collections.Set[string]{}
	for _, parameter := range node.Parameters() {
		tx.recordDeclarationName(parameter, tx.enclosingFunctionParameterNames)
	}
	hasLexicalThis := tx.inHasLexicalThisContext()
	asyncBody := tx.transformAsyncFunctionBodyWorker(node.Body())
	asyncBody = tx.Factory().UpdateBlock(asyncBody, tx.EmitContext().EndAndMergeVariableEnvironmentList(asyncBody.StatementList()), asyncBody.BlockMultiLine())
	emitSuperHelpers := tx.capturedSuperProperties != nil && (tx.capturedSuperProperties.Size() > 0 || tx.hasSuperElementAccess)
	if emitSuperHelpers {
		innerParameters = tx.superAccessVisitor.VisitNodes(innerParameters)
		asyncBody = tx.substituteSuperAccessesInBody(asyncBody)
	}
	var result ast.Handle
	if !isArrow {
		tx.EmitContext().StartVariableEnvironment()
		if emitSuperHelpers {
			if tx.capturedSuperProperties.Size() > 0 {
				tx.EmitContext().AddInitializationStatement(tx.createSuperAccessVariableStatement())
			}
		}
		if captureLexicalArguments && tx.lexicalArguments.used {
			tx.EmitContext().AddInitializationStatement(tx.createCaptureArgumentsStatement())
		}
		statements := []ast.Handle{tx.Factory().NewReturnStatement(tx.Factory().NewAwaiterHelper(hasLexicalThis, argumentsExpression, innerParameters, asyncBody))}
		block := tx.Factory().NewBlock(tx.EmitContext().EndAndMergeVariableEnvironmentList(tx.Factory().NewList(statements)), true)
		block.SetLoc(node.Body().Loc())
		if emitSuperHelpers && tx.hasSuperElementAccess {
			if tx.hasSuperPropertyAssignment {
				tx.EmitContext().AddEmitHelper(block, printer.AdvancedAsyncSuperHelper)
			} else {
				tx.EmitContext().AddEmitHelper(block, printer.AsyncSuperHelper)
			}
		}
		result = block
	} else {
		result = tx.Factory().NewAwaiterHelper(hasLexicalThis, argumentsExpression, innerParameters, asyncBody)
		if captureLexicalArguments && tx.lexicalArguments.used {
			block := tx.EmitContext().ConvertToFunctionBlock(result, true)
			if !ast.IsBlock(result) {
				tx.EmitContext().SetOriginal(block.Store().ListAt(block.StatementList(), 0), result)
			}
			result = tx.Factory().UpdateBlock(block, tx.EmitContext().MergeEnvironmentList(block.StatementList(), []ast.Handle{tx.createCaptureArgumentsStatement()}), block.BlockMultiLine())
		}
	}
	tx.enclosingFunctionParameterNames = savedEnclosingFunctionParameterNames
	if !isArrow {
		tx.capturedSuperProperties = savedCapturedSuperProperties
		tx.hasSuperElementAccess = savedHasSuperElementAccess
		tx.hasSuperPropertyAssignment = savedHasSuperPropertyAssignment
		tx.superBinding = savedSuperBinding
		tx.superIndexBinding = savedSuperIndexBinding
		tx.lexicalArguments = savedLexicalArguments
	} else if captureLexicalArguments && !tx.lexicalArguments.used {
		tx.lexicalArguments = savedLexicalArguments
	} else if captureLexicalArguments {
		tx.lexicalArguments.used = false
	}
	return result
}
func (tx *asyncTransformer) transformAsyncFunctionBodyWorker(body ast.Handle) ast.Handle {
	if ast.IsBlock(body) {
		return tx.Factory().UpdateBlock(body, tx.asyncBodyVisitor.VisitNodes(body.StatementList()), body.BlockMultiLine())
	}
	visited := tx.asyncBodyVisitor.VisitNode(body)
	ret := tx.Factory().NewReturnStatement(visited)
	ret.SetLoc(body.Loc())
	list := tx.Factory().List(body.Loc(), []ast.Handle{ret}...)
	block := tx.Factory().NewBlock(list, false)
	block.SetLoc(body.Loc())
	return block
}

func assignmentTargetContainsSuperProperty(node ast.Handle) bool {
	switch node.Kind {
	case ast.KindPropertyAccessExpression, ast.KindElementAccessExpression:
		return node.Expression().Kind == ast.KindSuperKeyword
	case ast.KindParenthesizedExpression:
		return assignmentTargetContainsSuperProperty(node.ParenthesizedExpressionExpression())
	case ast.KindArrayLiteralExpression:
		return node.ElementsSeq().Some(assignmentTargetContainsSuperProperty)
	case ast.KindObjectLiteralExpression:
		for _, prop := range node.Store().ListSlice(node.ObjectLiteralExpressionProperties()).All() {
			switch prop.Kind {
			case ast.KindPropertyAssignment:
				if assignmentTargetContainsSuperProperty(prop.PropertyAssignmentInitializer()) {
					return true
				}
			case ast.KindShorthandPropertyAssignment:
				if assignmentTargetContainsSuperProperty(prop.ShorthandPropertyAssignmentName()) {
					return true
				}
			case ast.KindSpreadAssignment:
				if assignmentTargetContainsSuperProperty(prop.SpreadAssignmentExpression()) {
					return true
				}
			}
		}
	case ast.KindSpreadElement:
		return assignmentTargetContainsSuperProperty(node.SpreadElementExpression())
	}
	return false
}

func isUpdateExpression(node ast.Handle) bool {
	if ast.IsPrefixUnaryExpression(node) {
		op := node.PrefixUnaryExpressionOperator()
		return op == ast.KindPlusPlusToken || op == ast.KindMinusMinusToken
	}
	if ast.IsPostfixUnaryExpression(node) {
		op := node.PostfixUnaryExpressionOperator()
		return op == ast.KindPlusPlusToken || op == ast.KindMinusMinusToken
	}
	return false
}
func (tx *asyncTransformer) getOriginalIfFunctionLike(node ast.Handle) ast.Handle {
	original := tx.EmitContext().MostOriginal(node)
	if !original.IsNil() && ast.IsFunctionLikeDeclaration(original) {
		return original
	}
	return node
}

func isSimpleParameterList(params []ast.Handle) bool {
	for _, param := range params {
		p := param
		if !p.Initializer().IsNil() || !ast.IsIdentifier(p.Name()) {
			return false
		}
	}
	return true
}

func isNodeWithPossibleHoistedDeclaration(node ast.Handle) bool {
	switch node.Kind {
	case ast.KindBlock, ast.KindVariableStatement, ast.KindWithStatement, ast.KindIfStatement, ast.KindSwitchStatement, ast.KindCaseBlock, ast.KindCaseClause, ast.KindDefaultClause, ast.KindLabeledStatement, ast.KindForStatement, ast.KindForInStatement, ast.KindForOfStatement, ast.KindDoStatement, ast.KindWhileStatement, ast.KindTryStatement, ast.KindCatchClause:
		return true
	}
	return false
}
