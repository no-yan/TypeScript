package estransforms

import (
	"github.com/microsoft/TypeScript/tsc/internal/ast"
	"github.com/microsoft/TypeScript/tsc/internal/collections"
	"github.com/microsoft/TypeScript/tsc/internal/core"
	"github.com/microsoft/TypeScript/tsc/internal/printer"
	"github.com/microsoft/TypeScript/tsc/internal/transformers"
)

type forAwaitHierarchyFacts int

const forAwaitHierarchyFactsNone forAwaitHierarchyFacts = 0
const (
	forAwaitHierarchyFactsHasLexicalThis forAwaitHierarchyFacts = 1 << iota
	forAwaitHierarchyFactsIterationContainer
	forAwaitHierarchyFactsAncestorFactsMask            = 1<<iota - 1
	forAwaitHierarchyFactsSourceFileExcludes           = forAwaitHierarchyFactsIterationContainer
	forAwaitHierarchyFactsStrictModeSourceFileIncludes = forAwaitHierarchyFactsNone
	forAwaitHierarchyFactsClassOrFunctionIncludes      = forAwaitHierarchyFactsHasLexicalThis
	forAwaitHierarchyFactsClassOrFunctionExcludes      = forAwaitHierarchyFactsIterationContainer
	forAwaitHierarchyFactsArrowFunctionIncludes        = forAwaitHierarchyFactsNone
	forAwaitHierarchyFactsArrowFunctionExcludes        = forAwaitHierarchyFactsClassOrFunctionExcludes
	forAwaitHierarchyFactsIterationStatementIncludes   = forAwaitHierarchyFactsIterationContainer
	forAwaitHierarchyFactsIterationStatementExcludes   = forAwaitHierarchyFactsNone
)

type forawaitTransformer struct {
	transformers.Transformer
	superAccessState
	compilerOptions           *core.CompilerOptions
	enclosingFunctionFlags    ast.FunctionFlags
	forAwaitHierarchyFacts    forAwaitHierarchyFacts
	exportedVariableStatement bool
	fallbackNodeVisitor *ast.HandleVisitor
	noAsyncModifierVisitor *ast.HandleVisitor
}

func newforawaitTransformer(opts *transformers.TransformOptions) *transformers.Transformer {
	tx := &forawaitTransformer{compilerOptions: opts.CompilerOptions}
	result := tx.NewTransformer(tx.visit, opts.Context)
	tx.initSuperAccessVisitor(tx.EmitContext(), tx.Factory())
	tx.fallbackNodeVisitor = tx.EmitContext().NewNodeVisitor(tx.visitFallback)
	tx.noAsyncModifierVisitor = tx.EmitContext().NewNodeVisitor(func(node ast.Handle) ast.Handle {
		if node.Kind() == ast.KindAsyncKeyword {
			return ast.Handle{}
		}
		return node
	})
	return result
}
func (tx *forawaitTransformer) affectsSubtree(excludeFacts forAwaitHierarchyFacts, includeFacts forAwaitHierarchyFacts) bool {
	return tx.forAwaitHierarchyFacts != (tx.forAwaitHierarchyFacts&^excludeFacts | includeFacts)
}

func (tx *forawaitTransformer) enterSubtree(excludeFacts forAwaitHierarchyFacts, includeFacts forAwaitHierarchyFacts) forAwaitHierarchyFacts {
	ancestorFacts := tx.forAwaitHierarchyFacts
	tx.forAwaitHierarchyFacts = (tx.forAwaitHierarchyFacts&^excludeFacts | includeFacts) & forAwaitHierarchyFactsAncestorFactsMask
	return ancestorFacts
}

func (tx *forawaitTransformer) exitSubtree(ancestorFacts forAwaitHierarchyFacts) {
	tx.forAwaitHierarchyFacts = ancestorFacts
}
func (tx *forawaitTransformer) visitModifiersNoAsync(modifiers ast.ListRef) ast.ListRef {
	return tx.noAsyncModifierVisitor.VisitModifiers(modifiers)
}
func (tx *forawaitTransformer) doWithHierarchyFacts(cb func(*forawaitTransformer, ast.Handle) ast.Handle, node ast.Handle, excludeFacts forAwaitHierarchyFacts, includeFacts forAwaitHierarchyFacts) ast.Handle {
	if tx.affectsSubtree(excludeFacts, includeFacts) {
		ancestorFacts := tx.enterSubtree(excludeFacts, includeFacts)
		result := cb(tx, node)
		tx.exitSubtree(ancestorFacts)
		return result
	}
	return cb(tx, node)
}
func (tx *forawaitTransformer) visitDefault(node ast.Handle) ast.Handle {
	return tx.Visitor().VisitEachChild(node)
}
func (tx *forawaitTransformer) fallbackVisitor(node ast.Handle) ast.Handle {
	if tx.capturedSuperProperties == nil {
		return node
	}
	switch node.Kind() {
	case ast.KindFunctionExpression, ast.KindFunctionDeclaration, ast.KindMethodDeclaration, ast.KindGetAccessor, ast.KindSetAccessor, ast.KindConstructor:
		return node
	}
	tx.trackSuperAccess(node)
	return tx.fallbackNodeVisitor.VisitEachChild(node)
}
func (tx *forawaitTransformer) visitFallback(node ast.Handle) ast.Handle {
	return tx.fallbackVisitor(node)
}
func (tx *forawaitTransformer) visit(node ast.Handle) ast.Handle {
	if node.SubtreeFacts()&ast.SubtreeContainsForAwaitOrAsyncGenerator == 0 {
		return tx.fallbackVisitor(node)
	}
	tx.trackSuperAccess(node)
	switch node.Kind() {
	case ast.KindSourceFile:
		return tx.visitSourceFile(node)
	case ast.KindAwaitExpression:
		return tx.visitAwaitExpression(node)
	case ast.KindYieldExpression:
		return tx.visitYieldExpression(node)
	case ast.KindReturnStatement:
		return tx.visitReturnStatement(node)
	case ast.KindLabeledStatement:
		return tx.visitLabeledStatement(node)
	case ast.KindDoStatement, ast.KindWhileStatement, ast.KindForInStatement:
		return tx.doWithHierarchyFacts((*forawaitTransformer).visitDefault, node, forAwaitHierarchyFactsIterationStatementExcludes, forAwaitHierarchyFactsIterationStatementIncludes)
	case ast.KindForOfStatement:
		return tx.visitForOfStatement(node, ast.Handle{})
	case ast.KindForStatement:
		return tx.doWithHierarchyFacts((*forawaitTransformer).visitDefault, node, forAwaitHierarchyFactsIterationStatementExcludes, forAwaitHierarchyFactsIterationStatementIncludes)
	case ast.KindConstructor:
		return tx.doWithHierarchyFacts((*forawaitTransformer).visitConstructorDeclaration, node, forAwaitHierarchyFactsClassOrFunctionExcludes, forAwaitHierarchyFactsClassOrFunctionIncludes)
	case ast.KindMethodDeclaration:
		return tx.doWithHierarchyFacts((*forawaitTransformer).visitMethodDeclaration, node, forAwaitHierarchyFactsClassOrFunctionExcludes, forAwaitHierarchyFactsClassOrFunctionIncludes)
	case ast.KindGetAccessor:
		return tx.doWithHierarchyFacts((*forawaitTransformer).visitGetAccessorDeclaration, node, forAwaitHierarchyFactsClassOrFunctionExcludes, forAwaitHierarchyFactsClassOrFunctionIncludes)
	case ast.KindSetAccessor:
		return tx.doWithHierarchyFacts((*forawaitTransformer).visitSetAccessorDeclaration, node, forAwaitHierarchyFactsClassOrFunctionExcludes, forAwaitHierarchyFactsClassOrFunctionIncludes)
	case ast.KindFunctionDeclaration:
		return tx.doWithHierarchyFacts((*forawaitTransformer).visitFunctionDeclaration, node, forAwaitHierarchyFactsClassOrFunctionExcludes, forAwaitHierarchyFactsClassOrFunctionIncludes)
	case ast.KindFunctionExpression:
		return tx.doWithHierarchyFacts((*forawaitTransformer).visitFunctionExpression, node, forAwaitHierarchyFactsClassOrFunctionExcludes, forAwaitHierarchyFactsClassOrFunctionIncludes)
	case ast.KindArrowFunction:
		return tx.doWithHierarchyFacts((*forawaitTransformer).visitArrowFunction, node, forAwaitHierarchyFactsArrowFunctionExcludes, forAwaitHierarchyFactsArrowFunctionIncludes)
	case ast.KindClassDeclaration, ast.KindClassExpression:
		return tx.doWithHierarchyFacts((*forawaitTransformer).visitDefault, node, forAwaitHierarchyFactsClassOrFunctionExcludes, forAwaitHierarchyFactsClassOrFunctionIncludes)
	default:
		return tx.Visitor().VisitEachChild(node)
	}
}
func (tx *forawaitTransformer) visitAwaitExpression(node ast.Handle) ast.Handle {
	if tx.enclosingFunctionFlags&ast.FunctionFlagsAsync != 0 && tx.enclosingFunctionFlags&ast.FunctionFlagsGenerator != 0 {
		result := tx.Factory().NewYieldExpression(ast.Handle{}, tx.Factory().NewAwaitHelper(tx.Visitor().VisitNode(node.Expression())))
		result.SetLoc(node.Loc())
		tx.EmitContext().SetOriginal(result, node)
		return result
	}
	return tx.Visitor().VisitEachChild(node)
}
func (tx *forawaitTransformer) visitYieldExpression(node ast.Handle) ast.Handle {
	if tx.enclosingFunctionFlags&ast.FunctionFlagsAsync != 0 && tx.enclosingFunctionFlags&ast.FunctionFlagsGenerator != 0 {
		if !node.AsteriskToken().IsNil() {
			expression := tx.Visitor().VisitNode(node.Expression())
			asyncValuesResult := tx.Factory().NewAsyncValuesHelper(expression)
			asyncValuesResult.SetLoc(expression.Loc())
			asyncDelegatorResult := tx.Factory().NewAsyncDelegatorHelper(asyncValuesResult)
			asyncDelegatorResult.SetLoc(expression.Loc())
			innerYield := tx.Factory().UpdateYieldExpression(node, node.AsteriskToken(), asyncDelegatorResult)
			awaitedYield := tx.Factory().NewAwaitHelper(innerYield)
			result := tx.Factory().NewYieldExpression(ast.Handle{}, awaitedYield)
			result.SetLoc(node.Loc())
			tx.EmitContext().SetOriginal(result, node)
			return result
		}
		var innerExpression ast.Handle
		if !node.Expression().IsNil() {
			innerExpression = tx.Visitor().VisitNode(node.Expression())
		} else {
			innerExpression = tx.Factory().NewVoidZeroExpression()
		}
		result := tx.Factory().NewYieldExpression(ast.Handle{}, tx.createDownlevelAwait(innerExpression))
		result.SetLoc(node.Loc())
		tx.EmitContext().SetOriginal(result, node)
		return result
	}
	return tx.Visitor().VisitEachChild(node)
}
func (tx *forawaitTransformer) visitReturnStatement(node ast.Handle) ast.Handle {
	if tx.enclosingFunctionFlags&ast.FunctionFlagsAsync != 0 && tx.enclosingFunctionFlags&ast.FunctionFlagsGenerator != 0 {
		var expression ast.Handle
		if !node.Expression().IsNil() {
			expression = tx.Visitor().VisitNode(node.Expression())
		} else {
			expression = tx.Factory().NewVoidZeroExpression()
		}
		return tx.Factory().UpdateReturnStatement(node, tx.createDownlevelAwait(expression))
	}
	return tx.Visitor().VisitEachChild(node)
}
func (tx *forawaitTransformer) visitLabeledStatement(node ast.Handle) ast.Handle {
	if tx.enclosingFunctionFlags&ast.FunctionFlagsAsync != 0 {
		statement := unwrapInnermostStatementOfLabel(node)
		if statement.Kind() == ast.KindForOfStatement && !statement.ForInOrOfStatementAwaitModifier().IsNil() {
			return tx.visitForOfStatement(statement, node)
		}
		return tx.Factory().RestoreEnclosingLabel(tx.Visitor().VisitNode(statement), node)
	}
	return tx.Visitor().VisitEachChild(node)
}

func unwrapInnermostStatementOfLabel(node ast.Handle) ast.Handle {
	for {
		if node.Statement().Kind() != ast.KindLabeledStatement {
			return node.Statement()
		}
		node = node.Statement()
	}
}
func (tx *forawaitTransformer) visitSourceFile(node ast.Handle) ast.Handle {
	ancestorFacts := tx.enterSubtree(forAwaitHierarchyFactsSourceFileExcludes, forAwaitHierarchyFactsStrictModeSourceFileIncludes)
	tx.exportedVariableStatement = false
	visited := tx.Visitor().VisitEachChild(node)
	tx.EmitContext().AddEmitHelper(visited, tx.EmitContext().ReadEmitHelpers()...)
	tx.exitSubtree(ancestorFacts)
	return visited
}

func (tx *forawaitTransformer) visitForOfStatement(node ast.Handle, outermostLabeledStatement ast.Handle) ast.Handle {
	ancestorFacts := tx.enterSubtree(forAwaitHierarchyFactsIterationStatementExcludes, forAwaitHierarchyFactsIterationStatementIncludes)
	var result ast.Handle
	if !node.AwaitModifier().IsNil() {
		result = tx.transformForAwaitOfStatement(node, outermostLabeledStatement, ancestorFacts)
	} else {
		result = tx.Factory().RestoreEnclosingLabel(tx.Visitor().VisitEachChild(node), outermostLabeledStatement)
	}
	tx.exitSubtree(ancestorFacts)
	return result
}
func (tx *forawaitTransformer) convertForOfStatementHead(node ast.Handle, boundValue ast.Handle, nonUserCode ast.Handle) ast.Handle {
	f := tx.Factory()
	value := f.NewTempVariable()
	tx.EmitContext().AddVariableDeclaration(value)
	iteratorValueExpression := f.NewAssignmentExpression(value, boundValue)
	iteratorValueStatement := f.NewExpressionStatement(iteratorValueExpression)
	tx.EmitContext().SetSourceMapRange(iteratorValueStatement, node.Expression().Loc())
	exitNonUserCodeExpression := f.NewAssignmentExpression(nonUserCode, f.NewKeywordExpression(ast.KindFalseKeyword))
	exitNonUserCodeStatement := f.NewExpressionStatement(exitNonUserCodeExpression)
	tx.EmitContext().SetSourceMapRange(exitNonUserCodeStatement, node.Expression().Loc())
	statements := []ast.Handle{iteratorValueStatement, exitNonUserCodeStatement}
	binding := tx.Factory().CreateForOfBindingStatement(node.Initializer(), value)
	statements = append(statements, tx.Visitor().VisitNode(binding))
	var bodyLocation core.TextRange
	var statementsLocation core.TextRange
	statement := tx.Visitor().VisitEmbeddedStatement(node.Statement())
	if ast.IsBlock(statement) {
		statements = append(statements, statement.Statements()...)
		bodyLocation = statement.Loc()
		statementsLocation = statement.Store().ListLoc(statement.StatementList())
	} else {
		statements = append(statements, statement)
	}
	stmtList := f.List(statementsLocation, statements...)
	block := f.NewBlock(stmtList, true)
	block.SetLoc(bodyLocation)
	return block
}
func (tx *forawaitTransformer) createDownlevelAwait(expression ast.Handle) ast.Handle {
	if tx.enclosingFunctionFlags&ast.FunctionFlagsGenerator != 0 {
		return tx.Factory().NewYieldExpression(ast.Handle{}, tx.Factory().NewAwaitHelper(expression))
	}
	return tx.Factory().NewAwaitExpression(expression)
}
func (tx *forawaitTransformer) transformForAwaitOfStatement(node ast.Handle, outermostLabeledStatement ast.Handle, ancestorFacts forAwaitHierarchyFacts) ast.Handle {
	f := tx.Factory()
	expression := tx.Visitor().VisitNode(node.Expression())
	var iterator ast.Handle
	if ast.IsIdentifier(expression) {
		iterator = f.NewGeneratedNameForNode(expression)
	} else {
		iterator = f.NewTempVariable()
	}
	var result ast.Handle
	if ast.IsIdentifier(expression) {
		result = f.NewGeneratedNameForNode(iterator)
	} else {
		result = f.NewTempVariable()
	}
	nonUserCode := f.NewTempVariable()
	done := f.NewTempVariable()
	tx.EmitContext().AddVariableDeclaration(done)
	errorRecord := f.NewUniqueName("e")
	catchVariable := f.NewGeneratedNameForNode(errorRecord)
	returnMethod := f.NewTempVariable()
	callValues := f.NewAsyncValuesHelper(expression)
	callValues.SetLoc(node.Expression().Loc())
	callNext := f.NewCallExpression(f.NewPropertyAccessExpression(iterator, ast.Handle{}, f.NewIdentifier("next"), ast.NodeFlagsNone), ast.Handle{}, 0, f.NewList([]ast.Handle{}), ast.NodeFlagsNone)
	getDone := f.NewPropertyAccessExpression(result, ast.Handle{}, f.NewIdentifier("done"), ast.NodeFlagsNone)
	getValue := f.NewPropertyAccessExpression(result, ast.Handle{}, f.NewIdentifier("value"), ast.NodeFlagsNone)
	callReturn := f.NewFunctionCallCall(returnMethod, iterator, []ast.Handle{})
	tx.EmitContext().AddVariableDeclaration(errorRecord)
	tx.EmitContext().AddVariableDeclaration(returnMethod)
	var initializer ast.Handle
	if ancestorFacts&forAwaitHierarchyFactsIterationContainer != 0 {
		initializer = f.InlineExpressions([]ast.Handle{f.NewAssignmentExpression(errorRecord, f.NewVoidZeroExpression()), callValues})
	} else {
		initializer = callValues
	}
	iteratorDecl := f.NewVariableDeclaration(iterator, ast.Handle{}, ast.Handle{}, initializer)
	iteratorDecl.SetLoc(node.Expression().Loc())
	varDeclList := f.NewVariableDeclarationList(f.NewList([]ast.Handle{f.NewVariableDeclaration(nonUserCode, ast.Handle{}, ast.Handle{}, f.NewKeywordExpression(ast.KindTrueKeyword)), iteratorDecl, f.NewVariableDeclaration(result, ast.Handle{}, ast.Handle{}, ast.Handle{})}), ast.NodeFlagsNone)
	varDeclList.SetLoc(node.Expression().Loc())
	condition := f.InlineExpressions([]ast.Handle{f.NewAssignmentExpression(result, tx.createDownlevelAwait(callNext)), f.NewAssignmentExpression(done, getDone), f.NewPrefixUnaryExpression(ast.KindExclamationToken, done)})
	incrementor := f.NewAssignmentExpression(nonUserCode, f.NewKeywordExpression(ast.KindTrueKeyword))
	forStatement := f.NewForStatement(varDeclList, condition, incrementor, tx.convertForOfStatementHead(node, getValue, nonUserCode))
	forStatement.SetLoc(node.Loc())
	tx.EmitContext().AddEmitFlags(forStatement, printer.EFNoTokenTrailingSourceMaps)
	tx.EmitContext().SetOriginal(forStatement, node)
	tryBlock := f.NewBlock(f.NewList([]ast.Handle{f.RestoreEnclosingLabel(forStatement, outermostLabeledStatement)}), true)
	catchBody := f.NewBlock(f.NewList([]ast.Handle{f.NewExpressionStatement(f.NewAssignmentExpression(errorRecord, f.NewObjectLiteralExpression(f.NewList([]ast.Handle{f.NewPropertyAssignment(0, f.NewIdentifier("error"), ast.Handle{}, ast.Handle{}, catchVariable)}), false)))}), false)
	tx.EmitContext().AddEmitFlags(catchBody, printer.EFSingleLine)
	catchClause := f.NewCatchClause(f.NewVariableDeclaration(catchVariable, ast.Handle{}, ast.Handle{}, ast.Handle{}), catchBody)
	innerIfCondition := f.NewBinaryExpression(0, f.NewBinaryExpression(0, f.NewPrefixUnaryExpression(ast.KindExclamationToken, nonUserCode), ast.Handle{}, f.NewToken(ast.KindAmpersandAmpersandToken), f.NewPrefixUnaryExpression(ast.KindExclamationToken, done)), ast.Handle{}, f.NewToken(ast.KindAmpersandAmpersandToken), f.NewAssignmentExpression(returnMethod, f.NewPropertyAccessExpression(iterator, ast.Handle{}, f.NewIdentifier("return"), ast.NodeFlagsNone)))
	innerIfStatement := f.NewIfStatement(innerIfCondition, f.NewExpressionStatement(tx.createDownlevelAwait(callReturn)), ast.Handle{})
	tx.EmitContext().AddEmitFlags(innerIfStatement, printer.EFSingleLine)
	innerTryBlock := f.NewBlock(f.NewList([]ast.Handle{innerIfStatement}), false)
	innerFinallyIf := f.NewIfStatement(errorRecord, f.NewThrowStatement(f.NewPropertyAccessExpression(errorRecord, ast.Handle{}, f.NewIdentifier("error"), ast.NodeFlagsNone)), ast.Handle{})
	tx.EmitContext().AddEmitFlags(innerFinallyIf, printer.EFSingleLine)
	innerFinallyBlock := f.NewBlock(f.NewList([]ast.Handle{innerFinallyIf}), false)
	tx.EmitContext().AddEmitFlags(innerFinallyBlock, printer.EFSingleLine)
	innerTryStatement := f.NewTryStatement(innerTryBlock, ast.Handle{}, innerFinallyBlock)
	finallyBlock := f.NewBlock(f.NewList([]ast.Handle{innerTryStatement}), true)
	return f.NewTryStatement(tryBlock, catchClause, finallyBlock)
}
func (tx *forawaitTransformer) visitConstructorDeclaration(node ast.Handle) ast.Handle {
	decl := node
	savedEnclosingFunctionFlags := tx.enclosingFunctionFlags
	tx.enclosingFunctionFlags = ast.GetFunctionFlags(node)
	updated := tx.Factory().UpdateConstructorDeclaration(decl, decl.Modifiers(), 0, tx.EmitContext().VisitParameters(decl.ParameterList(), tx.Visitor()), ast.Handle{}, ast.Handle{}, tx.EmitContext().VisitFunctionBody(node.Body(), tx.Visitor()))
	tx.enclosingFunctionFlags = savedEnclosingFunctionFlags
	return updated
}
func (tx *forawaitTransformer) visitGetAccessorDeclaration(node ast.Handle) ast.Handle {
	decl := node
	savedEnclosingFunctionFlags := tx.enclosingFunctionFlags
	tx.enclosingFunctionFlags = ast.GetFunctionFlags(node)
	updated := tx.Factory().UpdateGetAccessorDeclaration(decl, decl.Modifiers(), tx.Visitor().VisitNode(decl.Name()), 0, tx.EmitContext().VisitParameters(decl.ParameterList(), tx.Visitor()), ast.Handle{}, ast.Handle{}, tx.EmitContext().VisitFunctionBody(node.Body(), tx.Visitor()))
	tx.enclosingFunctionFlags = savedEnclosingFunctionFlags
	return updated
}
func (tx *forawaitTransformer) visitSetAccessorDeclaration(node ast.Handle) ast.Handle {
	decl := node
	savedEnclosingFunctionFlags := tx.enclosingFunctionFlags
	tx.enclosingFunctionFlags = ast.GetFunctionFlags(node)
	updated := tx.Factory().UpdateSetAccessorDeclaration(decl, decl.Modifiers(), tx.Visitor().VisitNode(decl.Name()), 0, tx.EmitContext().VisitParameters(decl.ParameterList(), tx.Visitor()), ast.Handle{}, ast.Handle{}, tx.EmitContext().VisitFunctionBody(node.Body(), tx.Visitor()))
	tx.enclosingFunctionFlags = savedEnclosingFunctionFlags
	return updated
}
func (tx *forawaitTransformer) visitMethodDeclaration(node ast.Handle) ast.Handle {
	decl := node
	savedEnclosingFunctionFlags := tx.enclosingFunctionFlags
	tx.enclosingFunctionFlags = ast.GetFunctionFlags(node)
	var modifiers ast.ListRef
	if tx.enclosingFunctionFlags&ast.FunctionFlagsGenerator != 0 {
		modifiers = tx.visitModifiersNoAsync(decl.Modifiers())
	} else {
		modifiers = decl.Modifiers()
	}
	var asteriskToken ast.Handle
	if tx.enclosingFunctionFlags&ast.FunctionFlagsAsync != 0 {
		asteriskToken = ast.Handle{}
	} else {
		asteriskToken = decl.AsteriskToken()
	}
	var parameters ast.ListRef
	var body ast.Handle
	if tx.enclosingFunctionFlags&ast.FunctionFlagsAsync != 0 && tx.enclosingFunctionFlags&ast.FunctionFlagsGenerator != 0 {
		parameters = tx.transformAsyncGeneratorFunctionParameterList(node)
		body = tx.transformAsyncGeneratorFunctionBody(node)
	} else {
		parameters = tx.EmitContext().VisitParameters(decl.ParameterList(), tx.Visitor())
		body = tx.EmitContext().VisitFunctionBody(node.Body(), tx.Visitor())
	}
	updated := tx.Factory().UpdateMethodDeclaration(decl, modifiers, asteriskToken, tx.Visitor().VisitNode(decl.Name()), ast.Handle{}, 0, parameters, ast.Handle{}, ast.Handle{}, body)
	tx.enclosingFunctionFlags = savedEnclosingFunctionFlags
	return updated
}
func (tx *forawaitTransformer) visitFunctionDeclaration(node ast.Handle) ast.Handle {
	decl := node
	savedEnclosingFunctionFlags := tx.enclosingFunctionFlags
	tx.enclosingFunctionFlags = ast.GetFunctionFlags(node)
	var modifiers ast.ListRef
	if tx.enclosingFunctionFlags&ast.FunctionFlagsGenerator != 0 {
		modifiers = tx.visitModifiersNoAsync(decl.Modifiers())
	} else {
		modifiers = decl.Modifiers()
	}
	var asteriskToken ast.Handle
	if tx.enclosingFunctionFlags&ast.FunctionFlagsAsync != 0 {
		asteriskToken = ast.Handle{}
	} else {
		asteriskToken = decl.AsteriskToken()
	}
	var parameters ast.ListRef
	var body ast.Handle
	if tx.enclosingFunctionFlags&ast.FunctionFlagsAsync != 0 && tx.enclosingFunctionFlags&ast.FunctionFlagsGenerator != 0 {
		parameters = tx.transformAsyncGeneratorFunctionParameterList(node)
		body = tx.transformAsyncGeneratorFunctionBody(node)
	} else {
		parameters = tx.EmitContext().VisitParameters(decl.ParameterList(), tx.Visitor())
		body = tx.EmitContext().VisitFunctionBody(node.Body(), tx.Visitor())
	}
	updated := tx.Factory().UpdateFunctionDeclaration(decl, modifiers, asteriskToken, decl.Name(), 0, parameters, ast.Handle{}, ast.Handle{}, body)
	tx.enclosingFunctionFlags = savedEnclosingFunctionFlags
	return updated
}
func (tx *forawaitTransformer) visitArrowFunction(node ast.Handle) ast.Handle {
	decl := node
	savedEnclosingFunctionFlags := tx.enclosingFunctionFlags
	tx.enclosingFunctionFlags = ast.GetFunctionFlags(node)
	updated := tx.Factory().UpdateArrowFunction(decl, decl.Modifiers(), 0, tx.EmitContext().VisitParameters(decl.ParameterList(), tx.Visitor()), ast.Handle{}, ast.Handle{}, decl.EqualsGreaterThanToken(), tx.EmitContext().VisitFunctionBody(node.Body(), tx.Visitor()))
	tx.enclosingFunctionFlags = savedEnclosingFunctionFlags
	return updated
}
func (tx *forawaitTransformer) visitFunctionExpression(node ast.Handle) ast.Handle {
	decl := node
	savedEnclosingFunctionFlags := tx.enclosingFunctionFlags
	tx.enclosingFunctionFlags = ast.GetFunctionFlags(node)
	var modifiers ast.ListRef
	if tx.enclosingFunctionFlags&ast.FunctionFlagsGenerator != 0 {
		modifiers = tx.visitModifiersNoAsync(decl.Modifiers())
	} else {
		modifiers = decl.Modifiers()
	}
	var asteriskToken ast.Handle
	if tx.enclosingFunctionFlags&ast.FunctionFlagsAsync != 0 {
		asteriskToken = ast.Handle{}
	} else {
		asteriskToken = decl.AsteriskToken()
	}
	var parameters ast.ListRef
	var body ast.Handle
	if tx.enclosingFunctionFlags&ast.FunctionFlagsAsync != 0 && tx.enclosingFunctionFlags&ast.FunctionFlagsGenerator != 0 {
		parameters = tx.transformAsyncGeneratorFunctionParameterList(node)
		body = tx.transformAsyncGeneratorFunctionBody(node)
	} else {
		parameters = tx.EmitContext().VisitParameters(decl.ParameterList(), tx.Visitor())
		body = tx.EmitContext().VisitFunctionBody(node.Body(), tx.Visitor())
	}
	updated := tx.Factory().UpdateFunctionExpression(decl, modifiers, asteriskToken, decl.Name(), 0, parameters, ast.Handle{}, ast.Handle{}, body)
	tx.enclosingFunctionFlags = savedEnclosingFunctionFlags
	return updated
}
func (tx *forawaitTransformer) transformAsyncGeneratorFunctionParameterList(node ast.Handle) ast.ListRef {
	if isSimpleParameterList(node.Parameters()) {
		return tx.EmitContext().VisitParameters(node.ParameterList(), tx.Visitor())
	}
	var newParameters []ast.Handle
	for _, parameter := range node.Parameters() {
		param := parameter
		if !param.Initializer().IsNil() || !param.DotDotDotToken().IsNil() {
			break
		}
		newParameter := tx.Factory().NewParameterDeclaration(0, ast.Handle{}, tx.Factory().NewGeneratedNameForNodeEx(param.Name(), printer.AutoGenerateOptions{Flags: printer.GeneratedIdentifierFlagsReservedInNestedScopes}), ast.Handle{}, ast.Handle{}, ast.Handle{})
		newParameters = append(newParameters, newParameter)
	}
	newParametersArray := tx.Factory().List(node.Store().ListLoc(node.ParameterList()), newParameters...)
	return newParametersArray
}
func (tx *forawaitTransformer) transformAsyncGeneratorFunctionBody(node ast.Handle) ast.Handle {
	f := tx.Factory()
	var innerParameters ast.ListRef
	if !isSimpleParameterList(node.Parameters()) {
		innerParameters = tx.EmitContext().VisitParameters(node.ParameterList(), tx.Visitor())
	}
	savedCapturedSuperProperties := tx.capturedSuperProperties
	savedHasSuperElementAccess := tx.hasSuperElementAccess
	savedHasSuperPropertyAssignment := tx.hasSuperPropertyAssignment
	savedSuperBinding := tx.superBinding
	savedSuperIndexBinding := tx.superIndexBinding
	tx.capturedSuperProperties = &collections.OrderedSet[string]{}
	tx.hasSuperElementAccess = false
	tx.hasSuperPropertyAssignment = false
	tx.superBinding = f.NewUniqueNameEx("_super", printer.AutoGenerateOptions{Flags: printer.GeneratedIdentifierFlagsOptimistic | printer.GeneratedIdentifierFlagsFileLevel})
	tx.superIndexBinding = f.NewUniqueNameEx("_superIndex", printer.AutoGenerateOptions{Flags: printer.GeneratedIdentifierFlagsOptimistic | printer.GeneratedIdentifierFlagsFileLevel})
	asyncBody := f.UpdateBlock(node.Body(), tx.Visitor().VisitNodes(node.Body().StatementList()), node.Body().BlockMultiLine())
	asyncBody = f.UpdateBlock(asyncBody, tx.EmitContext().EndAndMergeVariableEnvironmentList(asyncBody.StatementList()), asyncBody.BlockMultiLine())
	emitSuperHelpers := tx.capturedSuperProperties.Size() > 0 || tx.hasSuperElementAccess
	if emitSuperHelpers {
		asyncBody = tx.substituteSuperAccessesInBody(asyncBody)
	}
	var innerParams ast.ListRef
	if innerParameters != 0 {
		innerParams = innerParameters
	} else {
		innerParams = f.NewList([]ast.Handle{})
	}
	var name ast.Handle
	if !node.Name().IsNil() {
		name = f.NewGeneratedNameForNode(node.Name())
	}
	generatorFunc := f.NewFunctionExpression(0, f.NewToken(ast.KindAsteriskToken), name, 0, innerParams, ast.Handle{}, ast.Handle{}, asyncBody)
	returnStatement := f.NewReturnStatement(f.NewAsyncGeneratorHelper(generatorFunc, tx.forAwaitHierarchyFacts&forAwaitHierarchyFactsHasLexicalThis != 0))
	tx.EmitContext().StartVariableEnvironment()
	if emitSuperHelpers {
		if tx.capturedSuperProperties.Size() > 0 {
			tx.EmitContext().AddInitializationStatement(tx.createSuperAccessVariableStatement())
		}
	}
	outerStatements := []ast.Handle{returnStatement}
	block := f.UpdateBlock(node.Body(), tx.EmitContext().EndAndMergeVariableEnvironmentList(f.NewList(outerStatements)), node.Body().BlockMultiLine())
	if emitSuperHelpers && tx.hasSuperElementAccess {
		if tx.hasSuperPropertyAssignment {
			tx.EmitContext().AddEmitHelper(block, printer.AdvancedAsyncSuperHelper)
		} else {
			tx.EmitContext().AddEmitHelper(block, printer.AsyncSuperHelper)
		}
	}
	tx.capturedSuperProperties = savedCapturedSuperProperties
	tx.hasSuperElementAccess = savedHasSuperElementAccess
	tx.hasSuperPropertyAssignment = savedHasSuperPropertyAssignment
	tx.superBinding = savedSuperBinding
	tx.superIndexBinding = savedSuperIndexBinding
	return block
}
