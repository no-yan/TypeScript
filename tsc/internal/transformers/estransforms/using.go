package estransforms

import (
	"github.com/microsoft/TypeScript/tsc/internal/ast"
	"github.com/microsoft/TypeScript/tsc/internal/core"
	"github.com/microsoft/TypeScript/tsc/internal/debug"
	"github.com/microsoft/TypeScript/tsc/internal/printer"
	"github.com/microsoft/TypeScript/tsc/internal/transformers"
)

type usingDeclarationTransformer struct {
	transformers.Transformer
	exportBindings map[ // Imports and exports must stay at the top level. This means we must hoist all imports, exports, and
	// top-level function declarations and bindings out of the `try` statements we generate. For example:
	//
	// given:
	//
	//  import { w } from "mod";
	//  const x = expr1;
	//  using y = expr2;
	//  const z = expr3;
	//  export function f() {
	//    console.log(z);
	//  }
	//
	// produces:
	//
	//  import { x } from "mod";        // <-- preserved
	//  const x = expr1;                // <-- preserved
	//  var y, z;                       // <-- hoisted
	//  export function f() {           // <-- hoisted
	//    console.log(z);
	//  }
	//  const env_1 = { stack: [], error: void 0, hasError: false };
	//  try {
	//    y = __addDisposableResource(env_1, expr2, false);
	//    z = expr3;
	//  }
	//  catch (e_1) {
	//    env_1.error = e_1;
	//    env_1.hasError = true;
	//  }
	//  finally {
	//    __disposeResource(env_1);
	//  }
	//
	// In this transformation, we hoist `y`, `z`, and `f` to a new outer statement list while moving all other
	// statements in the source file into the `try` block, which is the same approach we use for System module
	// emit. Unlike System module emit, we attempt to preserve all statements prior to the first top-level
	// `using` to isolate the complexity of the transformed output to only where it is necessary.
	// Collect and transform any leading statements up to the first `using` or `await using`. This preserves
	// the original statement order much as is possible.
	// transform the rest of the body
	// add `export {}` declarations for any hoisted bindings.
	/*modifiers*/ /*isTypeOnly*/ /*moduleSpecifier*/ /*attributes*/ /*modifiers*/ /*isExportEquals*/ /*typeNode*/ /*topLevelStatements*/ // given:
	//
	//  for (using x = expr; cond; incr) { ... }
	//
	// produces a shallow transformation to:
	//
	//  {
	//    using x = expr;
	//    for (; cond; incr) { ... }
	//  }
	//
	// before handing the shallow transformation back to the visitor for an in-depth transformation.
	/*modifiers*/ /*initializer*/ /*multiLine*/ // given:
	//
	//  for (using x of y) { ... }
	//
	// produces a shallow transformation to:
	//
	//  for (const x_1 of y) {
	//    using x = x;
	//    ...
	//  }
	//
	// before handing the shallow transformation back to the visitor for an in-depth transformation.
	/*exclamationToken*/ /*type*/ /*modifiers*/ /*multiLine*/ /*exclamationToken*/ /*type*/ // Since binding patterns are a grammar error, we reset `declarations` so we don't process this as a `using`.
	// perform a shallow transform for any named evaluation
	/*ignoreEmptyStringLiteral*/ /*assignedName*/ /*exclamationToken*/ /*type*/ // Only replace the statement if it was valid.
	/*modifiers*/ // NOTE: `node` has already been visited
	// NOTE: `node` has already been visited
	// invalid case of multiple `export default` declarations. Don't assert here, just pass it through
	// given:
	//
	//   export default expr;
	//
	// produces:
	//
	//   // top level
	//   var default_1;
	//   export { default_1 as default };
	//
	//   // body
	//   default_1 = expr;
	/*isExport*/ // give a class or function expression an assigned name, if needed.
	/*ignoreEmptyStringLiteral*/ // NOTE: `node` has already been visited
	// invalid case of multiple `export default` declarations. Don't assert here, just pass it through
	// given:
	//
	//   export = expr;
	//
	// produces:
	//
	//   // top level
	//   var default_1;
	//
	//   try {
	//       // body
	//       default_1 = expr;
	//   } ...
	//
	//   // top level suffix
	//   export = default_1;
	// give a class or function expression an assigned name, if needed.
	// NOTE: `node` has already been visited
	// invalid case of multiple `export default` declarations. Don't assert here, just pass it through
	// When hoisting a class declaration at the top level of a file containing a top-level `using` statement, we
	// must first convert it to a class expression so that we can hoist the binding outside of the `try`.
	// given:
	//
	//  using x = expr;
	//  class C {}
	//
	// produces:
	//
	//  var x, C;
	//  const env_1 = { ... };
	//  try {
	//    x = __addDisposableResource(env_1, expr, false);
	//    C = class {};
	//  }
	//  catch (e_1) {
	//    env_1.error = e_1;
	//    env_1.hasError = true;
	//  }
	//  finally {
	//    __disposeResources(env_1);
	//  }
	//
	// If the class is exported, we also produce an `export { C };`
	/*exportAlias*/ /*ignoreEmptyStringLiteral*/ /*assignedName*/ // In the case of a default export, we create a temporary variable that we export as the default and then
	// assign to that variable.
	//
	// given:
	//
	//  using x = expr;
	//  export default class C {}
	//
	// produces:
	//
	//  export { default_1 as default };
	//  var x, C, default_1;
	//  const env_1 = { ... };
	//  try {
	//    x = __addDisposableResource(env_1, expr, false);
	//    default_1 = C = class {};
	//  }
	//  catch (e_1) {
	//    env_1.error = e_1;
	//    env_1.hasError = true;
	//  }
	//  finally {
	//    __disposeResources(env_1);
	//  }
	//
	// Though we will never reassign `default_1`, this most closely matches the specified runtime semantics.
	/*isExport*/ /*ignoreEmptyStringLiteral*/ // NOTE: `node` has already been visited
	// NOTE: `node` has already been visited
	/*VariableDeclaration|BindingElement*/ // NOTE: `node` has already been visited
	/*exportAlias*/ // NOTE: `node` has already been visited
	/*exclamationToken*/ /*type*/ /*initializer*/ /*isTypeOnly*/ // produces:
	//
	//  const env_1 = { stack: [], error: void 0, hasError: false };
	//
	/*modifiers*/ /*postfixToken*/ /*typeNode*/ /*multiLine*/ /*modifiers*/ /*postfixToken*/ /*typeNode*/ /*modifiers*/ /*postfixToken*/ /*typeNode*/ /*multiLine*/ /*exclamationToken*/ /*typeNode*/ /*modifiers*/ // when `async` is `false`, produces:
	//
	//  try {
	//    <bodyStatements>
	//  }
	//  catch (e_1) {
	//      env_1.error = e_1;
	//      env_1.hasError = true;
	//  }
	//  finally {
	//    __disposeResources(env_1);
	//  }
	// when `async` is `true`, produces:
	//
	//  try {
	//    <bodyStatements>
	//  }
	//  catch (e_1) {
	//      env_1.error = e_1;
	//      env_1.hasError = true;
	//  }
	//  finally {
	//    const result_1 = __disposeResources(env_1);
	//    if (result_1) {
	//      await result_1;
	//    }
	//  }
	// Unfortunately, it is necessary to use two properties to indicate an error because `throw undefined` is legal
	// JavaScript.
	/*multiLine*/ /*exclamationToken*/ /*type*/ /*initializer*/ /*multiLine*/ /*modifiers*/ /*exclamationToken*/ /*type*/ /*elseStatement*/ /*multiLine*/ /*multiLine*/ string]ast.Handle
	exportBindingNames   []string
	exportVars           []ast.Handle
	defaultExportBinding ast.Handle
	exportEqualsBinding  ast.Handle
}

func newUsingDeclarationTransformer(opts *transformers.TransformOptions) *transformers.Transformer {
	tx := &usingDeclarationTransformer{}
	return tx.NewTransformer(tx.visit, opts.Context)
}

type usingKind uint

const (
	usingKindNone usingKind = iota
	usingKindSync
	usingKindAsync
)

func (tx *usingDeclarationTransformer) visit(node ast.Handle) ast.Handle {
	if node.SubtreeFacts()&ast.SubtreeContainsUsing == 0 {
		return node
	}
	switch node.Kind {
	case ast.KindSourceFile:
		node = tx.visitSourceFile(node)
	case ast.KindBlock:
		node = tx.visitBlock(node)
	case ast.KindForStatement:
		node = tx.visitForStatement(node)
	case ast.KindForOfStatement:
		node = tx.visitForOfStatement(node)
	default:
		node = tx.Visitor().VisitEachChild(node)
	}
	return node
}
func (tx *usingDeclarationTransformer) visitSourceFile(node ast.Handle) ast.Handle {
	if ast.GetSourceFileOfNode(node) != nil && ast.GetSourceFileOfNode(node).IsDeclarationFile {
		return node
	}
	var visited ast.Handle
	usingKind := getUsingKindOfStatements(node.Statements())
	if usingKind != usingKindNone {
		tx.EmitContext().StartVariableEnvironment()
		tx.exportBindings = make(map[string]ast.Handle)
		tx.exportVars = nil
		prologue, rest := tx.Factory().SplitStandardPrologue(node.Statements())
		var topLevelStatements []ast.Handle
		topLevelStatements = append(topLevelStatements, core.FirstResult(tx.Visitor().VisitSlice(prologue))...)
		pos := 0
		for pos < len(rest) {
			statement := rest[pos]
			if getUsingKind(statement) != usingKindNone {
				if pos > 0 {
					topLevelStatements = append(topLevelStatements, core.FirstResult(tx.Visitor().VisitSlice(rest[:pos]))...)
				}
				break
			}
			pos++
		}
		if pos >= len(rest) {
			panic("Should have encountered at least one 'using' statement.")
		}
		envBinding := tx.createEnvBinding()
		bodyStatements := tx.transformUsingDeclarations(rest[pos:], envBinding, &topLevelStatements)
		if len(tx.exportBindings) > 0 {
			exportSpecifiers := make([]ast.Handle, 0, len(tx.exportBindingNames))
			for _, name := range tx.exportBindingNames {
				specifier := tx.exportBindings[name]
				debug.Assert(!specifier.IsNil(), "Missing export binding for hoisted export name")
				exportSpecifiers = append(exportSpecifiers, specifier)
			}
			topLevelStatements = append(topLevelStatements, tx.Factory().NewExportDeclaration(0, false, tx.Factory().NewNamedExports(tx.Factory().NewList(exportSpecifiers)), ast.Handle{}, ast.Handle{}))
		}
		topLevelStatements = append(topLevelStatements, tx.EmitContext().EndVariableEnvironment()...)
		if len(tx.exportVars) > 0 {
			topLevelStatements = append(topLevelStatements, tx.Factory().NewVariableStatement(tx.Factory().NewModifierList([]ast.Handle{tx.Factory().NewModifier(ast.KindExportKeyword)}), tx.Factory().NewVariableDeclarationList(tx.Factory().NewList(tx.exportVars), ast.NodeFlagsLet)))
		}
		topLevelStatements = append(topLevelStatements, tx.createDownlevelUsingStatements(bodyStatements, envBinding, usingKind == usingKindAsync)...)
		if !tx.exportEqualsBinding.IsNil() {
			topLevelStatements = append(topLevelStatements, tx.Factory().NewExportAssignment(0, true, ast.Handle{}, tx.exportEqualsBinding))
		}
		visited = tx.Factory().UpdateSourceFile(node, tx.Factory().NewList(topLevelStatements), node.EndOfFileToken())
	} else {
		visited = tx.Visitor().VisitEachChild(node)
	}
	tx.EmitContext().AddEmitHelper(visited, tx.EmitContext().ReadEmitHelpers()...)
	tx.exportVars = nil
	tx.exportBindings = nil
	tx.exportBindingNames = nil
	tx.defaultExportBinding = ast.Handle{}
	tx.exportEqualsBinding = ast.Handle{}
	return visited
}
func (tx *usingDeclarationTransformer) visitBlock(node ast.Handle) ast.Handle {
	usingKind := getUsingKindOfStatements(node.Statements())
	if usingKind != usingKindNone {
		prologue, rest := tx.Factory().SplitStandardPrologue(node.Statements())
		envBinding := tx.createEnvBinding()
		statements := make([]ast.Handle, 0, len(prologue)+2)
		statements = append(statements, core.FirstResult(tx.Visitor().VisitSlice(prologue))...)
		statements = append(statements, tx.createDownlevelUsingStatements(tx.transformUsingDeclarations(rest, envBinding, nil), envBinding, usingKind == usingKindAsync)...)
		statementList := tx.Factory().List(node.Store().ListLoc(node.StatementList()), statements...)
		return tx.Factory().UpdateBlock(node, statementList, node.MultiLine())
	}
	return tx.Visitor().VisitEachChild(node)
}
func (tx *usingDeclarationTransformer) visitForStatement(node ast.Handle) ast.Handle {
	if !node.Initializer().IsNil() && isUsingVariableDeclarationList(node.Initializer()) {
		return tx.Visitor().VisitNode(tx.Factory().NewBlock(tx.Factory().NewList([]ast.Handle{tx.Factory().NewVariableStatement(0, node.Initializer()), tx.Factory().UpdateForStatement(node, ast.Handle{}, node.Condition(), node.Incrementor(), node.Statement())}), false))
	}
	return tx.Visitor().VisitEachChild(node)
}
func (tx *usingDeclarationTransformer) visitForOfStatement(node ast.Handle) ast.Handle {
	if isUsingVariableDeclarationList(node.Initializer()) {
		forInitializer := node.Initializer()
		forDecl := core.FirstOrNil(forInitializer.Declarations())
		if forDecl.IsNil() {
			forDecl = tx.Factory().NewVariableDeclaration(tx.Factory().NewTempVariable(), ast.Handle{}, ast.Handle{}, ast.Handle{})
		}
		isAwaitUsing := getUsingKindOfVariableDeclarationList(forInitializer) == usingKindAsync
		temp := tx.Factory().NewGeneratedNameForNode(forDecl.Name())
		usingVar := tx.Factory().UpdateVariableDeclaration(forDecl, forDecl.Name(), ast.Handle{}, ast.Handle{}, temp)
		usingVarList := tx.Factory().NewVariableDeclarationList(tx.Factory().NewList([]ast.Handle{usingVar}), core.IfElse(isAwaitUsing, ast.NodeFlagsAwaitUsing, ast.NodeFlagsUsing))
		usingVarStatement := tx.Factory().NewVariableStatement(0, usingVarList)
		var statement ast.Handle
		if ast.IsBlock(node.Statement()) {
			statements := make([]ast.Handle, 0, len(node.Statement().Statements())+1)
			statements = append(statements, usingVarStatement)
			statements = append(statements, node.Statement().Statements()...)
			statement = tx.Factory().UpdateBlock(node.Statement(), tx.Factory().NewList(statements), node.Statement().BlockMultiLine())
		} else {
			statement = tx.Factory().NewBlock(tx.Factory().NewList([]ast.Handle{usingVarStatement, node.Statement()}), true)
		}
		return tx.Visitor().VisitNode(tx.Factory().UpdateForInOrOfStatement(node, node.AwaitModifier(), tx.Factory().NewVariableDeclarationList(tx.Factory().NewList([]ast.Handle{tx.Factory().NewVariableDeclaration(temp, ast.Handle{}, ast.Handle{}, ast.Handle{})}), ast.NodeFlagsConst), node.Expression(), statement))
	}
	return tx.Visitor().VisitEachChild(node)
}
func (tx *usingDeclarationTransformer) transformUsingDeclarations(statementsIn []ast.Handle, envBinding ast.Handle, topLevelStatements *[]ast.Handle) []ast.Handle {
	var statements []ast.Handle
	hoist := func(node ast.Handle) ast.Handle {
		if topLevelStatements == nil {
			return node
		}
		switch node.Kind {
		case ast.KindImportDeclaration, ast.KindImportEqualsDeclaration, ast.KindExportDeclaration, ast.KindFunctionDeclaration:
			tx.hoistImportOrExportOrHoistedDeclaration(node, topLevelStatements)
			return ast.Handle{}
		case ast.KindExportAssignment:
			return tx.hoistExportAssignment(node)
		case ast.KindClassDeclaration:
			return tx.hoistClassDeclaration(node)
		case ast.KindVariableStatement:
			return tx.hoistVariableStatement(node)
		}
		return node
	}
	hoistOrAppendNode := func(node ast.Handle) {
		node = hoist(node)
		if !node.IsNil() {
			statements = append(statements, node)
		}
	}
	for _, statement := range statementsIn {
		usingKind := getUsingKind(statement)
		if usingKind != usingKindNone {
			varStatement := statement
			declarationList := varStatement.VariableStatementDeclarationList()
			var declarations []ast.Handle
			for _, declaration := range declarationList.Declarations() {
				if !ast.IsIdentifier(declaration.Name()) {
					declarations = nil
					break
				}
				if isNamedEvaluation(tx.EmitContext(), declaration) {
					declaration = transformNamedEvaluation(tx.EmitContext(), declaration, false, "")
				}
				initializer := tx.Visitor().VisitNode(declaration.Initializer())
				if initializer.IsNil() {
					initializer = tx.Factory().NewVoidZeroExpression()
				}
				declarations = append(declarations, tx.Factory().UpdateVariableDeclaration(declaration, declaration.Name(), ast.Handle{}, ast.Handle{}, tx.Factory().NewAddDisposableResourceHelper(envBinding, initializer, usingKind == usingKindAsync)))
			}
			if len(declarations) > 0 {
				varList := tx.Factory().NewVariableDeclarationList(tx.Factory().NewList(declarations), ast.NodeFlagsConst)
				tx.EmitContext().SetOriginal(varList, declarationList)
				varList.SetLoc(declarationList.Loc())
				hoistOrAppendNode(tx.Factory().UpdateVariableStatement(varStatement, 0, varList))
				continue
			}
		}
		if result := tx.visit(statement); !result.IsNil() {
			if result.Kind == ast.KindSyntaxList {
				for _, node := range result.Store().ListSlice(result.SyntaxListChildren()).All() {
					hoistOrAppendNode(node)
				}
			} else {
				hoistOrAppendNode(result)
			}
		}
	}
	return statements
}
func (tx *usingDeclarationTransformer) hoistImportOrExportOrHoistedDeclaration(node ast.Handle, topLevelStatements *[]ast.Handle) {
	*topLevelStatements = append(*topLevelStatements, node)
}
func (tx *usingDeclarationTransformer) hoistExportAssignment(node ast.Handle) ast.Handle {
	if node.IsExportEquals() {
		return tx.hoistExportEquals(node)
	} else {
		return tx.hoistExportDefault(node)
	}
}
func (tx *usingDeclarationTransformer) hoistExportDefault(node ast.Handle) ast.Handle {
	if !tx.defaultExportBinding.IsNil() {
		return node
	}
	tx.defaultExportBinding = tx.Factory().NewUniqueNameEx("_default", printer.AutoGenerateOptions{Flags: printer.GeneratedIdentifierFlagsReservedInNestedScopes | printer.GeneratedIdentifierFlagsFileLevel | printer.GeneratedIdentifierFlagsOptimistic})
	tx.hoistBindingIdentifier(tx.defaultExportBinding, true, tx.Factory().NewIdentifier("default"), node)
	expression := node.Expression()
	innerExpression := ast.SkipOuterExpressions(expression, ast.OEKAll)
	if isNamedEvaluation(tx.EmitContext(), innerExpression) {
		innerExpression = transformNamedEvaluation(tx.EmitContext(), innerExpression, false, "default")
		expression = tx.Factory().RestoreOuterExpressions(expression, innerExpression, ast.OEKAll)
	}
	assignment := tx.Factory().NewAssignmentExpression(tx.defaultExportBinding, expression)
	return tx.Factory().NewExpressionStatement(assignment)
}
func (tx *usingDeclarationTransformer) hoistExportEquals(node ast.Handle) ast.Handle {
	if !tx.exportEqualsBinding.IsNil() {
		return node
	}
	tx.exportEqualsBinding = tx.Factory().NewUniqueNameEx("_default", printer.AutoGenerateOptions{Flags: printer.GeneratedIdentifierFlagsReservedInNestedScopes | printer.GeneratedIdentifierFlagsFileLevel | printer.GeneratedIdentifierFlagsOptimistic})
	tx.EmitContext().AddVariableDeclaration(tx.exportEqualsBinding)
	assignment := tx.Factory().NewAssignmentExpression(tx.exportEqualsBinding, node.Expression())
	return tx.Factory().NewExpressionStatement(assignment)
}
func (tx *usingDeclarationTransformer) hoistClassDeclaration(node ast.Handle) ast.Handle {
	if node.Name().IsNil() && !tx.defaultExportBinding.IsNil() {
		return node
	}
	isExported := ast.HasSyntacticModifier(node, ast.ModifierFlagsExport)
	isDefault := ast.HasSyntacticModifier(node, ast.ModifierFlagsDefault)
	expression := convertClassDeclarationToClassExpression(tx.EmitContext(), node)
	if !node.Name().IsNil() {
		tx.hoistBindingIdentifier(tx.Factory().GetLocalName(node), isExported && !isDefault, ast.Handle{}, node)
		expression = tx.Factory().NewAssignmentExpression(tx.Factory().GetDeclarationName(node), expression)
		tx.EmitContext().SetOriginal(expression, node)
		tx.EmitContext().SetSourceMapRange(expression, node.Loc())
		tx.EmitContext().SetCommentRange(expression, node.Loc())
		if isNamedEvaluation(tx.EmitContext(), expression) {
			expression = transformNamedEvaluation(tx.EmitContext(), expression, false, "")
		}
	}
	if isDefault && tx.defaultExportBinding.IsNil() {
		tx.defaultExportBinding = tx.Factory().NewUniqueNameEx("_default", printer.AutoGenerateOptions{Flags: printer.GeneratedIdentifierFlagsReservedInNestedScopes | printer.GeneratedIdentifierFlagsFileLevel | printer.GeneratedIdentifierFlagsOptimistic})
		tx.hoistBindingIdentifier(tx.defaultExportBinding, true, tx.Factory().NewIdentifier("default"), node)
		expression = tx.Factory().NewAssignmentExpression(tx.defaultExportBinding, expression)
		tx.EmitContext().SetOriginal(expression, node)
		if isNamedEvaluation(tx.EmitContext(), expression) {
			expression = transformNamedEvaluation(tx.EmitContext(), expression, false, "default")
		}
	}
	return tx.Factory().NewExpressionStatement(expression)
}
func (tx *usingDeclarationTransformer) hoistVariableStatement(node ast.Handle) ast.Handle {
	var expressions []ast.Handle
	isExported := ast.HasSyntacticModifier(node, ast.ModifierFlagsExport)
	for _, variable := range node.VariableStatementDeclarationList().Declarations() {
		tx.hoistBindingElement(variable, isExported, variable)
		if !variable.Initializer().IsNil() {
			expressions = append(expressions, tx.hoistInitializedVariable(variable))
		}
	}
	if len(expressions) > 0 {
		statement := tx.Factory().NewExpressionStatement(tx.Factory().InlineExpressions(expressions))
		tx.EmitContext().SetOriginal(statement, node)
		tx.EmitContext().SetCommentRange(statement, node.Loc())
		tx.EmitContext().SetSourceMapRange(statement, node.Loc())
		return statement
	}
	return ast.Handle{}
}
func (tx *usingDeclarationTransformer) hoistInitializedVariable(node ast.Handle) ast.Handle {
	if node.Initializer().IsNil() {
		panic("Expected initializer")
	}
	var target ast.Handle
	if ast.IsIdentifier(node.Name()) {
		target = tx.Factory().DeepCloneNode(node.Name())
		tx.EmitContext().SetEmitFlags(target, tx.EmitContext().EmitFlags(target) & ^(printer.EFLocalName|printer.EFExportName))
	} else {
		target = transformers.ConvertBindingPatternToAssignmentPattern(tx.EmitContext(), node.Name())
	}
	assignment := tx.Factory().NewAssignmentExpression(target, node.Initializer())
	tx.EmitContext().SetOriginal(assignment, node)
	tx.EmitContext().SetCommentRange(assignment, node.Loc())
	tx.EmitContext().SetSourceMapRange(assignment, node.Loc())
	return assignment
}
func (tx *usingDeclarationTransformer) hoistBindingElement(node ast.Handle, isExportedDeclaration bool, original ast.Handle) {
	if ast.IsBindingPattern(node.Name()) {
		for _, element := range node.Name().Elements() {
			if !element.Name().IsNil() {
				tx.hoistBindingElement(element, isExportedDeclaration, original)
			}
		}
	} else {
		tx.hoistBindingIdentifier(node.Name(), isExportedDeclaration, ast.Handle{}, original)
	}
}
func (tx *usingDeclarationTransformer) hoistBindingIdentifier(node ast.Handle, isExport bool, exportAlias ast.Handle, original ast.Handle) {
	name := node
	if !transformers.IsGeneratedIdentifier(tx.EmitContext(), node) {
		name = tx.Factory().DeepCloneNode(name)
	}
	if isExport {
		if exportAlias.IsNil() && !transformers.IsLocalName(tx.EmitContext(), name) {
			varDecl := tx.Factory().NewVariableDeclaration(name, ast.Handle{}, ast.Handle{}, ast.Handle{})
			if !original.IsNil() {
				tx.EmitContext().SetOriginal(varDecl, original)
			}
			tx.exportVars = append(tx.exportVars, varDecl)
			return
		}
		var localName ast.Handle
		var exportName ast.Handle
		if !exportAlias.IsNil() {
			localName = name
			exportName = exportAlias
		} else {
			exportName = name
		}
		specifier := tx.Factory().NewExportSpecifier(false, localName, exportName)
		if !original.IsNil() {
			tx.EmitContext().SetOriginal(specifier, original)
		}
		if tx.exportBindings == nil {
			tx.exportBindings = make(map[string]ast.Handle)
		}
		if _, ok := tx.exportBindings[name.Text()]; !ok {
			tx.exportBindingNames = append(tx.exportBindingNames, name.Text())
		}
		tx.exportBindings[name.Text()] = specifier
	}
	tx.EmitContext().AddVariableDeclaration(name)
}
func (tx *usingDeclarationTransformer) createEnvBinding() ast.Handle {
	return tx.Factory().NewUniqueName("env")
}
func (tx *usingDeclarationTransformer) createDownlevelUsingStatements(bodyStatements []ast.Handle, envBinding ast.Handle, async bool) []ast.Handle {
	statements := make([]ast.Handle, 0, 2)
	envObject := tx.Factory().NewObjectLiteralExpression(tx.Factory().NewList([]ast.Handle{tx.Factory().NewPropertyAssignment(0, tx.Factory().NewIdentifier("stack"), ast.Handle{}, ast.Handle{}, tx.Factory().NewArrayLiteralExpression(0, false)), tx.Factory().NewPropertyAssignment(0, tx.Factory().NewIdentifier("error"), ast.Handle{}, ast.Handle{}, tx.Factory().NewVoidZeroExpression()), tx.Factory().NewPropertyAssignment(0, tx.Factory().NewIdentifier("hasError"), ast.Handle{}, ast.Handle{}, tx.Factory().NewFalseExpression())}), false)
	envVar := tx.Factory().NewVariableDeclaration(envBinding, ast.Handle{}, ast.Handle{}, envObject)
	envVarList := tx.Factory().NewVariableDeclarationList(tx.Factory().NewList([]ast.Handle{envVar}), ast.NodeFlagsConst)
	envVarStatement := tx.Factory().NewVariableStatement(0, envVarList)
	statements = append(statements, envVarStatement)
	tryBlock := tx.Factory().NewBlock(tx.Factory().NewList(bodyStatements), true)
	bodyCatchBinding := tx.Factory().NewUniqueName("e")
	catchClause := tx.Factory().NewCatchClause(tx.Factory().NewVariableDeclaration(bodyCatchBinding, ast.Handle{}, ast.Handle{}, ast.Handle{}), tx.Factory().NewBlock(tx.Factory().NewList([]ast.Handle{tx.Factory().NewExpressionStatement(tx.Factory().NewAssignmentExpression(tx.Factory().NewPropertyAccessExpression(envBinding, ast.Handle{}, tx.Factory().NewIdentifier("error"), ast.NodeFlagsNone), bodyCatchBinding)), tx.Factory().NewExpressionStatement(tx.Factory().NewAssignmentExpression(tx.Factory().NewPropertyAccessExpression(envBinding, ast.Handle{}, tx.Factory().NewIdentifier("hasError"), ast.NodeFlagsNone), tx.Factory().NewTrueExpression()))}), true))
	var finallyBlock ast.Handle
	if async {
		result := tx.Factory().NewUniqueName("result")
		finallyBlock = tx.Factory().NewBlock(tx.Factory().NewList([]ast.Handle{tx.Factory().NewVariableStatement(0, tx.Factory().NewVariableDeclarationList(tx.Factory().NewList([]ast.Handle{tx.Factory().NewVariableDeclaration(result, ast.Handle{}, ast.Handle{}, tx.Factory().NewDisposeResourcesHelper(envBinding))}), ast.NodeFlagsConst)), tx.Factory().NewIfStatement(result, tx.Factory().NewExpressionStatement(tx.Factory().NewAwaitExpression(result)), ast.Handle{})}), true)
	} else {
		finallyBlock = tx.Factory().NewBlock(tx.Factory().NewList([]ast.Handle{tx.Factory().NewExpressionStatement(tx.Factory().NewDisposeResourcesHelper(envBinding))}), true)
	}
	tryStatement := tx.Factory().NewTryStatement(tryBlock, catchClause, finallyBlock)
	statements = append(statements, tryStatement)
	return statements
}
func isUsingVariableDeclarationList(node ast.Handle) bool {
	return ast.IsVariableDeclarationList(node) && getUsingKindOfVariableDeclarationList(node) != usingKindNone
}
func getUsingKindOfVariableDeclarationList(node ast.Handle) usingKind {
	switch node.Flags() & ast.NodeFlagsBlockScoped {
	case ast.NodeFlagsAwaitUsing:
		return usingKindAsync
	case ast.NodeFlagsUsing:
		return usingKindSync
	default:
		return usingKindNone
	}
}
func getUsingKindOfVariableStatement(node ast.Handle) usingKind {
	return getUsingKindOfVariableDeclarationList(node.VariableStatementDeclarationList())
}
func getUsingKind(statement ast.Handle) usingKind {
	if ast.IsVariableStatement(statement) {
		return getUsingKindOfVariableStatement(statement)
	}
	return usingKindNone
}
func getUsingKindOfStatements(statements []ast.Handle) usingKind {
	result := usingKindNone
	for _, statement := range statements {
		usingKind := getUsingKind(statement)
		if usingKind == usingKindAsync {
			return usingKindAsync
		}
		if usingKind > result {
			result = usingKind
		}
	}
	return result
}
