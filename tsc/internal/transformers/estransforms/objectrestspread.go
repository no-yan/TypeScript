package estransforms

import (
	"github.com/microsoft/TypeScript/tsc/internal/ast"
	"github.com/microsoft/TypeScript/tsc/internal/core"
	"github.com/microsoft/TypeScript/tsc/internal/printer"
	"github.com/microsoft/TypeScript/tsc/internal/transformers"
)

type objectRestSpreadTransformer struct {
	transformers.Transformer
	compilerOptions                           *core.CompilerOptions
	inExportedVariableStatement               bool
	expressionResultIsUnused                  bool
	parametersWithPrecedingObjectRestOrSpread map[ // Save the expressionResultIsUnused flag set by the parent for this node,
	// then reset to false for children (the default). Specific cases below override as needed.
	// Binding patterns are converted into a generated name and are
	// evaluated inside the function body.
	// EmitContext().VisitFunctionBody is not used here because this transformer needs to inject

	// object rest assignments between visiting the body and merging the variable environment.
	// In cases where a binding pattern is simply '[]' or '{}',
	// we usually don't want to emit a var declaration; however, in the presence
	// of an initializer, we must emit that expression to preserve side effects.
	// Converts a parameter initializer into a function body statement, i.e.:
	//
	//  function f(x = 1) { }
	//
	// becomes
	//
	//  function f(x) {
	//    if (typeof x === "undefined") { x = 1; }
	//  }
	// If we are here it is because the name contains a binding pattern with a rest somewhere in it.
	// spread elements emit like so:
	// non-spread elements are chunked together into object literals, and then all are passed to __assign:
	//     { a, ...o, b } => __assign(__assign({a}, o), {b});
	// If the first element is a spread element, then the first argument to __assign is {}:
	//     { ...o, a, b, ...o2 } => __assign(__assign(__assign({}, o), {a, b}), o2)
	//
	// We cannot call __assign with more than two elements, since any element could cause side effects. For
	// example:
	//      var k = { a: 1, b: 2 };
	//      var o = { a: 3, ...k, b: k.a++ };
	//      // expected: { a: 1, b: 1 }
	// If we translate the above to `__assign({ a: 3 }, k, { b: k.a++ })`, the `k.a++` will evaluate before
	// `k` is spread and we end up with `{ a: 2, b: 1 }`.
	//
	// This also occurs for spread elements, not just property assignments:
	//      var k = { a: 1, get b() { l = { z: 9 }; return 2; } };
	//      var l = { c: 3 };
	//      var o = { ...k, ...l };
	//      // expected: { a: 1, b: 2, z: 9 }
	// If we translate the above to `__assign({}, k, l)`, the `l` will evaluate before `k` is spread and we
	// end up with `{ a: 1, b: 2, c: 3 }`
	ast.Handle]struct{}
}

func (ch *objectRestSpreadTransformer) visit(node ast.Handle) ast.Handle {
	if node.SubtreeFacts()&ast.SubtreeContainsESObjectRestOrSpread == 0 && ch.parametersWithPrecedingObjectRestOrSpread == nil {
		return node
	}
	expressionResultIsUnused := ch.expressionResultIsUnused
	ch.expressionResultIsUnused = false
	defer func() {
		ch.expressionResultIsUnused = expressionResultIsUnused
	}()
	switch node.Kind {
	case ast.KindSourceFile:
		return ch.visitSourceFile(node)
	case ast.KindObjectLiteralExpression:
		return ch.visitObjectLiteralExpression(node)
	case ast.KindBinaryExpression:
		return ch.visitBinaryExpression(node, expressionResultIsUnused)
	case ast.KindExpressionStatement:
		ch.expressionResultIsUnused = true
		return ch.Visitor().VisitEachChild(node)
	case ast.KindParenthesizedExpression:
		ch.expressionResultIsUnused = expressionResultIsUnused
		return ch.Visitor().VisitEachChild(node)
	case ast.KindForOfStatement:
		return ch.visitForOftatement(node)
	case ast.KindVariableStatement:
		return ch.visitVariableStatement(node)
	case ast.KindVariableDeclaration:
		return ch.visitVariableDeclaration(node)
	case ast.KindCatchClause:
		return ch.visitCatchClause(node)
	case ast.KindParameter:
		return ch.visitParameter(node)
	case ast.KindConstructor:
		return ch.visitContructorDeclaration(node)
	case ast.KindGetAccessor:
		return ch.visitGetAccessorDeclaration(node)
	case ast.KindSetAccessor:
		return ch.visitSetAccessorDeclaration(node)
	case ast.KindMethodDeclaration:
		return ch.visitMethodDeclaration(node)
	case ast.KindFunctionDeclaration:
		return ch.visitFunctionDeclaration(node)
	case ast.KindArrowFunction:
		return ch.visitArrowFunction(node)
	case ast.KindFunctionExpression:
		return ch.visitFunctionExpression(node)
	default:
		return ch.Visitor().VisitEachChild(node)
	}
}
func (ch *objectRestSpreadTransformer) visitSourceFile(node ast.Handle) ast.Handle {
	visited := ch.Visitor().VisitEachChild(node)
	ch.EmitContext().AddEmitHelper(visited, ch.EmitContext().ReadEmitHelpers()...)
	return visited
}
func (ch *objectRestSpreadTransformer) visitParameter(node ast.Handle) ast.Handle {
	if ch.parametersWithPrecedingObjectRestOrSpread != nil {
		if _, ok := ch.parametersWithPrecedingObjectRestOrSpread[node]; ok {
			name := node.Name()
			if ast.IsBindingPattern(name) {
				name = ch.Factory().NewGeneratedNameForNode(node)
			}
			return ch.Factory().UpdateParameterDeclaration(node, 0, node.DotDotDotToken(), name, ast.Handle{}, ast.Handle{}, ast.Handle{})
		}
	}
	if node.SubtreeFacts()&ast.SubtreeContainsObjectRestOrSpread != 0 {
		return ch.Factory().UpdateParameterDeclaration(node, 0, node.DotDotDotToken(), ch.Factory().NewGeneratedNameForNode(node), ast.Handle{}, ast.Handle{}, ch.Visitor().VisitNode(node.Initializer()))
	}
	return ch.Visitor().VisitEachChild(node)
}
func (ch *objectRestSpreadTransformer) collectParametersWithPrecedingObjectRestOrSpread(node ast.Handle) map[ast.Handle]struct{} {
	var result map[ast.Handle]struct{}
	for _, parameter := range node.Parameters() {
		if result != nil {
			result[parameter] = struct{}{}
		} else if parameter.SubtreeFacts()&ast.SubtreeContainsObjectRestOrSpread != 0 {
			result = make(map[ast.Handle]struct{})
		}
	}
	return result
}

type oldParamScope map[ast.Handle]struct{}

func (ch *objectRestSpreadTransformer) enterParameterListContext(node ast.Handle) oldParamScope {
	old := ch.parametersWithPrecedingObjectRestOrSpread
	ch.parametersWithPrecedingObjectRestOrSpread = ch.collectParametersWithPrecedingObjectRestOrSpread(node)
	return oldParamScope(old)
}
func (ch *objectRestSpreadTransformer) exitParameterListContext(scope oldParamScope) {
	ch.parametersWithPrecedingObjectRestOrSpread = map[ast.Handle]struct{}(scope)
}
func (ch *objectRestSpreadTransformer) visitContructorDeclaration(node ast.Handle) ast.Handle {
	old := ch.enterParameterListContext(node)
	defer ch.exitParameterListContext(old)
	return ch.Factory().UpdateConstructorDeclaration(node, node.Modifiers(), 0, ch.Visitor().VisitNodes(node.ParameterList()), ast.Handle{}, ast.Handle{}, ch.transformFunctionBody(node))
}
func (ch *objectRestSpreadTransformer) visitGetAccessorDeclaration(node ast.Handle) ast.Handle {
	old := ch.enterParameterListContext(node)
	defer ch.exitParameterListContext(old)
	return ch.Factory().UpdateGetAccessorDeclaration(node, node.Modifiers(), ch.Visitor().VisitNode(node.Name()), 0, ch.Visitor().VisitNodes(node.ParameterList()), ast.Handle{}, ast.Handle{}, ch.transformFunctionBody(node))
}
func (ch *objectRestSpreadTransformer) visitSetAccessorDeclaration(node ast.Handle) ast.Handle {
	old := ch.enterParameterListContext(node)
	defer ch.exitParameterListContext(old)
	return ch.Factory().UpdateSetAccessorDeclaration(node, node.Modifiers(), ch.Visitor().VisitNode(node.Name()), 0, ch.Visitor().VisitNodes(node.ParameterList()), ast.Handle{}, ast.Handle{}, ch.transformFunctionBody(node))
}
func (ch *objectRestSpreadTransformer) visitMethodDeclaration(node ast.Handle) ast.Handle {
	old := ch.enterParameterListContext(node)
	defer ch.exitParameterListContext(old)
	return ch.Factory().UpdateMethodDeclaration(node, node.Modifiers(), node.AsteriskToken(), ch.Visitor().VisitNode(node.Name()), node.PostfixToken(), 0, ch.Visitor().VisitNodes(node.ParameterList()), ast.Handle{}, ast.Handle{}, ch.transformFunctionBody(node))
}
func (ch *objectRestSpreadTransformer) visitFunctionDeclaration(node ast.Handle) ast.Handle {
	old := ch.enterParameterListContext(node)
	defer ch.exitParameterListContext(old)
	return ch.Factory().UpdateFunctionDeclaration(node, node.Modifiers(), node.AsteriskToken(), ch.Visitor().VisitNode(node.Name()), 0, ch.Visitor().VisitNodes(node.ParameterList()), ast.Handle{}, ast.Handle{}, ch.transformFunctionBody(node))
}
func (ch *objectRestSpreadTransformer) visitArrowFunction(node ast.Handle) ast.Handle {
	old := ch.enterParameterListContext(node)
	defer ch.exitParameterListContext(old)
	return ch.Factory().UpdateArrowFunction(node, node.Modifiers(), 0, ch.Visitor().VisitNodes(node.ParameterList()), ast.Handle{}, ast.Handle{}, node.EqualsGreaterThanToken(), ch.transformFunctionBody(node))
}
func (ch *objectRestSpreadTransformer) visitFunctionExpression(node ast.Handle) ast.Handle {
	old := ch.enterParameterListContext(node)
	defer ch.exitParameterListContext(old)
	return ch.Factory().UpdateFunctionExpression(node, node.Modifiers(), node.AsteriskToken(), ch.Visitor().VisitNode(node.Name()), 0, ch.Visitor().VisitNodes(node.ParameterList()), ast.Handle{}, ast.Handle{}, ch.transformFunctionBody(node))
}
func (ch *objectRestSpreadTransformer) transformFunctionBody(node ast.Handle) ast.Handle {
	ch.EmitContext().StartVariableEnvironment()
	body := ch.Visitor().VisitNode(node.Body())
	extras := ch.EmitContext().EndVariableEnvironment()
	ch.EmitContext().StartVariableEnvironment()
	newStatements := ch.collectObjectRestAssignments(node)
	extras = ch.EmitContext().EndAndMergeVariableEnvironment(extras)
	if len(newStatements) == 0 && len(extras) == 0 {
		return body
	}
	if body.IsNil() {
		body = ch.Factory().NewBlock(ch.Factory().NewList([]ast.Handle{}), true)
	}
	var prefix []ast.Handle
	var suffix []ast.Handle
	if ast.IsBlock(body) {
		custom := false
		for i, statement := range body.Statements() {
			if !custom && ast.IsPrologueDirective(statement) {
				prefix = append(prefix, statement)
			} else if ch.EmitContext().EmitFlags(statement)&printer.EFCustomPrologue != 0 {
				custom = true
				prefix = append(prefix, statement)
			} else {
				suffix = body.Statements()[i:]
				break
			}
		}
	} else {
		ret := ch.Factory().NewReturnStatement(body)
		ret.SetLoc(body.Loc())
		list := ch.Factory().List(body.Loc(), []ast.Handle{}...)
		body = ch.Factory().NewBlock(list, true)
		suffix = append(suffix, ret)
	}
	newStatementList := ch.Factory().NewList(append(append(append(prefix, extras...), newStatements...), suffix...))
	newStatementList = ch.Factory().RelocateList(newStatementList, body.Store().ListLoc(body.StatementList()))
	return ch.Factory().UpdateBlock(body, newStatementList, body.BlockMultiLine())
}
func (ch *objectRestSpreadTransformer) collectObjectRestAssignments(node ast.Handle) []ast.Handle {
	containsPrecedingObjectRestOrSpread := false
	var results []ast.Handle
	for _, parameter := range node.Parameters() {
		if containsPrecedingObjectRestOrSpread {
			if ast.IsBindingPattern(parameter.Name()) {
				if len(parameter.Name().Elements()) > 0 {
					declarations := transformers.FlattenDestructuringBinding(&ch.Transformer, parameter, ch.Factory().NewGeneratedNameForNode(parameter), transformers.FlattenLevelAll, false, false)
					if !declarations.IsNil() {
						decls := []ast.Handle{declarations}
						if declarations.Kind == ast.KindSyntaxList {
							// NewList takes []Handle ownership.
							decls = declarations.ChildrenSeq().Slice()
						}
						declarationList := ch.Factory().NewVariableDeclarationList(ch.Factory().NewList(decls), ast.NodeFlagsNone)
						statement := ch.Factory().NewVariableStatement(0, declarationList)
						ch.EmitContext().AddEmitFlags(statement, printer.EFCustomPrologue)
						results = append(results, statement)
					}
				} else if !parameter.Initializer().IsNil() {
					name := ch.Factory().NewGeneratedNameForNode(parameter)
					initializer := ch.Visitor().VisitNode(parameter.Initializer())
					assignment := ch.Factory().NewAssignmentExpression(name, initializer)
					statement := ch.Factory().NewExpressionStatement(assignment)
					ch.EmitContext().AddEmitFlags(statement, printer.EFCustomPrologue)
					results = append(results, statement)
				}
			} else if !parameter.Initializer().IsNil() {
				name := ch.Factory().DeepCloneNode(parameter.Name())
				name.SetLoc(parameter.Name().Loc())
				ch.EmitContext().AddEmitFlags(name, printer.EFNoSourceMap)
				initializer := ch.Visitor().VisitNode(parameter.Initializer())
				ch.EmitContext().AddEmitFlags(initializer, printer.EFNoSourceMap|printer.EFNoComments)
				assignment := ch.Factory().NewAssignmentExpression(name, initializer)
				assignment.SetLoc(parameter.Loc())
				ch.EmitContext().AddEmitFlags(assignment, printer.EFNoComments)
				block := ch.Factory().NewBlock(ch.Factory().NewList([]ast.Handle{ch.Factory().NewExpressionStatement(assignment)}), false)
				block.SetLoc(parameter.Loc())
				ch.EmitContext().AddEmitFlags(block, printer.EFSingleLine|printer.EFNoTrailingSourceMap|printer.EFNoTokenSourceMaps|printer.EFNoComments)
				typeCheck := ch.Factory().NewTypeCheck(ch.Factory().DeepCloneNode(name), "undefined")
				statement := ch.Factory().NewIfStatement(typeCheck, block, ast.Handle{})
				statement.SetLoc(parameter.Loc())
				ch.EmitContext().AddEmitFlags(statement, printer.EFNoTokenSourceMaps|printer.EFNoTrailingSourceMap|printer.EFCustomPrologue|printer.EFNoComments|printer.EFStartOnNewLine)
				results = append(results, statement)
			}
		} else if parameter.SubtreeFacts()&ast.SubtreeContainsObjectRestOrSpread != 0 {
			containsPrecedingObjectRestOrSpread = true
			declarations := transformers.FlattenDestructuringBinding(&ch.Transformer, parameter, ch.Factory().NewGeneratedNameForNode(parameter), transformers.FlattenLevelObjectRest, false, true)
			if !declarations.IsNil() {
				decls := []ast.Handle{declarations}
				if declarations.Kind == ast.KindSyntaxList {
					// NewList takes []Handle ownership.
					decls = declarations.ChildrenSeq().Slice()
				}
				declarationList := ch.Factory().NewVariableDeclarationList(ch.Factory().NewList(decls), ast.NodeFlagsNone)
				statement := ch.Factory().NewVariableStatement(0, declarationList)
				ch.EmitContext().AddEmitFlags(statement, printer.EFCustomPrologue)
				results = append(results, statement)
			}
		}
	}
	return results
}
func (ch *objectRestSpreadTransformer) visitCatchClause(node ast.Handle) ast.Handle {
	if !node.VariableDeclaration().IsNil() && ast.IsBindingPattern(node.VariableDeclaration().Name()) && node.VariableDeclaration().Name().SubtreeFacts()&ast.SubtreeContainsObjectRestOrSpread != 0 {
		name := ch.Factory().NewGeneratedNameForNode(node.VariableDeclaration().Name())
		updatedDecl := ch.Factory().UpdateVariableDeclaration(node.VariableDeclaration(), node.VariableDeclaration().Name(), ast.Handle{}, ast.Handle{}, name)
		visitedBindings := transformers.FlattenDestructuringBinding(&ch.Transformer, updatedDecl, ast.Handle{}, transformers.FlattenLevelObjectRest, false, false)
		block := ch.Visitor().VisitNode(node.Block())
		if !visitedBindings.IsNil() {
			var decls []ast.Handle
			if visitedBindings.Kind == ast.KindSyntaxList {
				// NewList takes []Handle ownership.
				decls = visitedBindings.ChildrenSeq().Slice()
			} else {
				decls = []ast.Handle{visitedBindings}
			}
			newStatement := ch.Factory().NewVariableStatement(0, ch.Factory().NewVariableDeclarationList(ch.Factory().NewList(decls), ast.NodeFlagsNone))
			statements := []ast.Handle{newStatement}
			statements = append(statements, block.Statements()...)
			statementList := ch.Factory().List(block.Store().ListLoc(block.StatementList()), statements...)
			block = ch.Factory().UpdateBlock(block, statementList, block.BlockMultiLine())
		}
		return ch.Factory().UpdateCatchClause(node, ch.Factory().UpdateVariableDeclaration(node.VariableDeclaration(), name, ast.Handle{}, ast.Handle{}, ast.Handle{}), block)
	}
	return ch.Visitor().VisitEachChild(node)
}
func (ch *objectRestSpreadTransformer) visitVariableStatement(node ast.Handle) ast.Handle {
	if ast.HasSyntacticModifier(node, ast.ModifierFlagsExport) {
		oldInExportedVariableStatement := ch.inExportedVariableStatement
		ch.inExportedVariableStatement = true
		result := ch.Visitor().VisitEachChild(node)
		ch.inExportedVariableStatement = oldInExportedVariableStatement
		return result
	}
	return ch.Visitor().VisitEachChild(node)
}
func (ch *objectRestSpreadTransformer) visitVariableDeclaration(node ast.Handle) ast.Handle {
	if ch.inExportedVariableStatement {
		ch.inExportedVariableStatement = false
		result := ch.visitVariableDeclarationWorker(node, true)
		ch.inExportedVariableStatement = true
		return result
	}
	return ch.visitVariableDeclarationWorker(node, false)
}
func (ch *objectRestSpreadTransformer) visitVariableDeclarationWorker(node ast.Handle, exported bool) ast.Handle {
	if ast.IsBindingPattern(node.Name()) && node.SubtreeFacts()&ast.SubtreeContainsObjectRestOrSpread != 0 {
		return transformers.FlattenDestructuringBinding(&ch.Transformer, node, ast.Handle{}, transformers.FlattenLevelObjectRest, exported, false)
	}
	return ch.Visitor().VisitEachChild(node)
}
func (ch *objectRestSpreadTransformer) visitForOftatement(node ast.Handle) ast.Handle {
	if node.Initializer().SubtreeFacts()&ast.SubtreeContainsObjectRestOrSpread != 0 || (ast.IsAssignmentPattern(node.Initializer()) && ast.ContainsObjectRestOrSpread(node.Initializer())) {
		initializerWithoutParens := ast.SkipParentheses(node.Initializer())
		if ast.IsVariableDeclarationList(initializerWithoutParens) || ast.IsAssignmentPattern(initializerWithoutParens) {
			var bodyLocation core.TextRange
			var statementsLocation core.TextRange
			temp := ch.Factory().NewTempVariable()
			res := ch.Visitor().VisitNode(ch.Factory().CreateForOfBindingStatement(initializerWithoutParens, temp))
			statements := make([]ast.Handle, 0, 1)
			if !res.IsNil() {
				statements = append(statements, res)
			}
			if ast.IsBlock(node.Statement()) {
				for _, statement := range node.Statement().Statements() {
					visited := ch.Visitor().VisitEachChild(statement)
					if !visited.IsNil() {
						statements = append(statements, visited)
					}
				}
				bodyLocation = node.Statement().Loc()
				statementsLocation = node.Statement().Store().ListLoc(node.Statement().StatementList())
			} else if !node.Statement().IsNil() {
				statements = append(statements, ch.Visitor().VisitEachChild(node.Statement()))
				bodyLocation = node.Statement().Loc()
				statementsLocation = node.Statement().Loc()
			}
			list := ch.Factory().NewVariableDeclarationList(ch.Factory().NewList([]ast.Handle{ch.Factory().NewVariableDeclaration(temp, ast.Handle{}, ast.Handle{}, ast.Handle{})}), ast.NodeFlagsLet)
			list.SetLoc(node.Initializer().Loc())
			expr := ch.Visitor().VisitEachChild(node.Expression())
			statementsList := ch.Factory().List(statementsLocation, statements...)
			block := ch.Factory().NewBlock(statementsList, true)
			block.SetLoc(bodyLocation)
			return ch.Factory().UpdateForInOrOfStatement(node, node.AwaitModifier(), list, expr, block)
		}
	}
	return ch.Visitor().VisitEachChild(node)
}
func (ch *objectRestSpreadTransformer) visitBinaryExpression(node ast.Handle, expressionResultIsUnused bool) ast.Handle {
	if ast.IsDestructuringAssignment(node) && ast.ContainsObjectRestOrSpread(node.Left()) {
		return transformers.FlattenDestructuringAssignment(&ch.Transformer, node, !expressionResultIsUnused, transformers.FlattenLevelObjectRest, nil)
	}
	if node.OperatorToken().Kind == ast.KindCommaToken {
		ch.expressionResultIsUnused = true
		left := ch.Visitor().VisitNode(node.Left())
		ch.expressionResultIsUnused = expressionResultIsUnused
		right := ch.Visitor().VisitNode(node.Right())
		return ch.Factory().UpdateBinaryExpression(node, 0, left, ast.Handle{}, node.OperatorToken(), right)
	}
	return ch.Visitor().VisitEachChild(node)
}
func (ch *objectRestSpreadTransformer) visitObjectLiteralExpression(node ast.Handle) ast.Handle {
	if (node.SubtreeFacts() & ast.SubtreeContainsObjectRestOrSpread) == 0 {
		return ch.Visitor().VisitEachChild(node)
	}
	objects := ch.chunkObjectLiteralElements(node.PropertyList())
	if len(objects) > 0 && objects[0].Kind != ast.KindObjectLiteralExpression {
		objects = append([]ast.Handle{ch.Factory().NewObjectLiteralExpression(ch.Factory().NewList(nil), false)}, objects...)
	}
	expression := objects[0]
	if len(objects) > 1 {
		for i, obj := range objects {
			if i == 0 {
				continue
			}
			expression = ch.Factory().NewAssignHelper([]ast.Handle{expression, obj}, ch.compilerOptions.GetEmitScriptTarget())
		}
		return expression
	}
	return ch.Factory().NewAssignHelper(objects, ch.compilerOptions.GetEmitScriptTarget())
}
func (ch *objectRestSpreadTransformer) chunkObjectLiteralElements(list ast.ListRef) []ast.Handle {
	if list == 0 {
		return nil
	}
	store := ch.Factory().Store()
	if store.ListLen(list) == 0 {
		return nil
	}
	var chunkObject []ast.Handle
	objects := make([]ast.Handle, 0, 1)
	for _, e := range store.ListSlice(list).All() {
		if e.Kind == ast.KindSpreadAssignment {
			if len(chunkObject) > 0 {
				objects = append(objects, ch.Factory().NewObjectLiteralExpression(ch.Factory().NewList(chunkObject), false))
				chunkObject = nil
			}
			target := e.Expression()
			objects = append(objects, ch.Visitor().VisitNode(target))
		} else {
			var elem ast.Handle
			if e.Kind == ast.KindPropertyAssignment {
				elem = ch.Factory().NewPropertyAssignment(0, e.Name(), ast.Handle{}, ast.Handle{}, ch.Visitor().VisitNode(e.Initializer()))
			} else {
				elem = ch.Visitor().VisitNode(e)
			}
			chunkObject = append(chunkObject, elem)
		}
	}
	if len(chunkObject) > 0 {
		objects = append(objects, ch.Factory().NewObjectLiteralExpression(ch.Factory().NewList(chunkObject), false))
	}
	return objects
}
func newObjectRestSpreadTransformer(opts *transformers.TransformOptions) *transformers.Transformer {
	tx := &objectRestSpreadTransformer{compilerOptions: opts.CompilerOptions}
	return tx.NewTransformer(tx.visit, opts.Context)
}
