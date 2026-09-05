package moduletransforms

import (
	"github.com/microsoft/TypeScript/tsc/internal/ast"
	"github.com/microsoft/TypeScript/tsc/internal/binder"
	"github.com/microsoft/TypeScript/tsc/internal/core"
	"github.com/microsoft/TypeScript/tsc/internal/printer"
	"github.com/microsoft/TypeScript/tsc/internal/transformers"
	"slices"
)

type ESModuleTransformer struct {
	transformers.Transformer
	compilerOptions           *core.CompilerOptions
	resolver                  binder.ReferenceResolver
	getEmitModuleFormatOfFile func(file ast.HasFileName) core.ModuleKind
	currentSourceFile         *ast.SourceFile
	importRequireStatements   *importRequireStatements
	helperNameSubstitutions   map[ // Visits source elements that are not top-level or top-level nested statements.
	/*hasExportStarsToExportValues*/ /*hasImportStar*/ /*hasImportDefault*/ // The helpers import must be visited so that `import x = require("tslib")`
	// (TypeScript-only syntax) is transformed to `const x = require("tslib")`
	// for CJS output files via visitImportEqualsDeclaration.
	/*modifiers*/ // Though an error in es2020 modules, in node-flavor es2020 modules, we can helpfully transform this to a synthetic `require` call
	// To give easy access to a synchronous `require` in node-flavor esm. We do the transform even in scenarios where we error, but `import.meta.url`
	// is available, just because the output is reasonable for a node-like runtime.
	/*modifiers*/ /*exclamationToken*/ /*type*/ /*modifiers*/ /*isTypeOnly*/ /*isTypeOnly*/ /*propertyName*/ /*moduleSpecifier*/ /*attributes*/ // Elide `export=` as it is not legal with --module ES6
	/*questionDotToken*/ // Either ill-formed or don't need to be transformed.
	/*modifiers*/ /*isTypeOnly*/ /*modifiers*/ /*phaseModifier*/ /*name*/ /*modifiers*/ /*isExportEquals*/ /*typeNode*/ /*modifiers*/ /*isTypeOnly*/ /*isTypeOnly*/ /*moduleSpecifier*/ /*attributes*/ /*requireStringLiteralLikeArgument*/ /*typeArguments*/ /*ImportDeclaration | ImportEqualsDeclaration | ExportDeclaration*/ /*host*/ /*emitResolver*/ /*questionDotToken*/ /*typeArguments*/ /*modifiers*/ /*phaseModifier*/ /*name*/ /*isTypeOnly*/ /*attributes*/ /*modifiers*/ /*exclamationToken*/ /*type*/ /*questionDotToken*/ /*typeArguments*/ /*questionDotToken*/ /*questionDotToken*/ /*typeArguments*/ string]ast.Handle
}
type importRequireStatements struct {
	statements        []ast.Handle
	requireHelperName ast.Handle
}

func NewESModuleTransformer(opts *transformers.TransformOptions) *transformers.Transformer {
	compilerOptions := opts.CompilerOptions
	tx := &ESModuleTransformer{compilerOptions: compilerOptions, resolver: opts.Resolver, getEmitModuleFormatOfFile: opts.GetEmitModuleFormatOfFile}
	return tx.NewTransformer(tx.visit, opts.Context)
}

func (tx *ESModuleTransformer) visit(node ast.Handle) ast.Handle {
	switch node.Kind {
	case ast.KindSourceFile:
		node = tx.visitSourceFile(node)
	case ast.KindImportDeclaration:
		node = tx.visitImportDeclaration(node)
	case ast.KindImportEqualsDeclaration:
		node = tx.visitImportEqualsDeclaration(node)
	case ast.KindExportAssignment:
		node = tx.visitExportAssignment(node)
	case ast.KindExportDeclaration:
		node = tx.visitExportDeclaration(node)
	case ast.KindCallExpression:
		node = tx.visitCallExpression(node)
	default:
		node = tx.Visitor().VisitEachChild(node)
	}
	return node
}
func (tx *ESModuleTransformer) visitSourceFile(node ast.Handle) ast.Handle {
	if ast.GetSourceFileOfNode(node) != nil && ast.GetSourceFileOfNode(node).IsDeclarationFile || !(ast.IsExternalModule(ast.GetSourceFileOfNode(node)) || tx.compilerOptions.GetIsolatedModules()) {
		return node
	}
	tx.currentSourceFile = ast.GetSourceFileOfNode(node)
	tx.importRequireStatements = nil
	result := tx.Visitor().VisitEachChild(node)
	tx.EmitContext().AddEmitHelper(result, tx.EmitContext().ReadEmitHelpers()...)
	externalHelpersImportDeclaration := createExternalHelpersImportDeclarationIfNeeded(tx.EmitContext(), ast.GetSourceFileOfNode(result), tx.compilerOptions, tx.getEmitModuleFormatOfFile(ast.GetSourceFileOfNode(node)), false, false, false)
	if !externalHelpersImportDeclaration.IsNil() || tx.importRequireStatements != nil {
		prologue, rest := tx.Factory().SplitStandardPrologue(result.Statements())
		custom, rest := tx.Factory().SplitCustomPrologue(rest)
		statements := slices.Clone(prologue)
		statements = append(statements, custom...)
		if !externalHelpersImportDeclaration.IsNil() {
			statements = append(statements, tx.Visitor().VisitNode(externalHelpersImportDeclaration))
		}
		if tx.importRequireStatements != nil {
			statements = append(statements, tx.importRequireStatements.statements...)
		}
		statements = append(statements, rest...)
		statementList := tx.Factory().List(result.Store().ListLoc(result.StatementList()), statements...)
		result = tx.Factory().UpdateSourceFile(result, statementList, node.EndOfFileToken())
	}
	if ast.IsExternalModule(ast.GetSourceFileOfNode(result)) && tx.compilerOptions.GetEmitModuleKind() != core.ModuleKindPreserve && !core.Some(result.Statements(), ast.IsExternalModuleIndicator) {
		statements := slices.Clone(result.Statements())
		statements = append(statements, createEmptyImports(tx.Factory()))
		statementList := tx.Factory().List(result.Store().ListLoc(result.StatementList()), statements...)
		result = tx.Factory().UpdateSourceFile(result, statementList, node.EndOfFileToken())
	}
	tx.importRequireStatements = nil
	tx.currentSourceFile = nil
	return result
}
func (tx *ESModuleTransformer) visitImportDeclaration(node ast.Handle) ast.Handle {
	if !tx.compilerOptions.RewriteRelativeImportExtensions.IsTrue() {
		return node
	}
	updatedModuleSpecifier := rewriteModuleSpecifier(tx.EmitContext(), node.ModuleSpecifier(), tx.compilerOptions)
	return tx.Factory().UpdateImportDeclaration(node, 0, tx.Visitor().VisitNode(node.ImportClause()), updatedModuleSpecifier, tx.Visitor().VisitNode(node.Attributes()))
}
func (tx *ESModuleTransformer) visitImportEqualsDeclaration(node ast.Handle) ast.Handle {
	if tx.compilerOptions.GetEmitModuleKind() < core.ModuleKindNode16 {
		return ast.Handle{}
	}
	if !ast.IsExternalModuleImportEqualsDeclaration(node) {
		panic("import= for internal module references should be handled in an earlier transformer.")
	}
	varStatement := tx.Factory().NewVariableStatement(0, tx.Factory().NewVariableDeclarationList(tx.Factory().NewList([]ast.Handle{tx.Factory().NewVariableDeclaration(tx.Factory().DeepCloneNode(node.Name()), ast.Handle{}, ast.Handle{}, tx.createRequireCall(node))}), ast.NodeFlagsConst))
	tx.EmitContext().SetOriginal(varStatement, node)
	tx.EmitContext().AssignCommentAndSourceMapRanges(varStatement, node)
	var statements []ast.Handle
	statements = append(statements, varStatement)
	statements = tx.appendExportsOfImportEqualsDeclaration(statements, node)
	return transformers.SingleOrMany(statements, tx.Factory())
}
func (tx *ESModuleTransformer) appendExportsOfImportEqualsDeclaration(statements []ast.Handle, node ast.Handle) []ast.Handle {
	if ast.HasSyntacticModifier(node, ast.ModifierFlagsExport) {
		statements = append(statements, tx.Factory().NewExportDeclaration(0, false, tx.Factory().NewNamedExports(tx.Factory().NewList([]ast.Handle{tx.Factory().NewExportSpecifier(false, ast.Handle{}, tx.Factory().DeepCloneNode(node.Name()))})), ast.Handle{}, ast.Handle{}))
	}
	return statements
}
func (tx *ESModuleTransformer) visitExportAssignment(node ast.Handle) ast.Handle {
	if !node.IsExportEquals() {
		return tx.Visitor().VisitEachChild(node)
	}
	if tx.compilerOptions.GetEmitModuleKind() != core.ModuleKindPreserve {
		return ast.Handle{}
	}
	statement := tx.Factory().NewExpressionStatement(tx.Factory().NewAssignmentExpression(tx.Factory().NewPropertyAccessExpression(tx.Factory().NewIdentifier("module"), ast.Handle{}, tx.Factory().NewIdentifier("exports"), ast.NodeFlagsNone), tx.Visitor().VisitNode(node.Expression())))
	tx.EmitContext().SetOriginal(statement, node)
	return statement
}
func (tx *ESModuleTransformer) visitExportDeclaration(node ast.Handle) ast.Handle {
	if node.ModuleSpecifier().IsNil() {
		return node
	}
	updatedModuleSpecifier := rewriteModuleSpecifier(tx.EmitContext(), node.ModuleSpecifier(), tx.compilerOptions)
	if tx.compilerOptions.Module > core.ModuleKindES2015 || node.ExportClause().IsNil() || !ast.IsNamespaceExport(node.ExportClause()) {
		return tx.Factory().UpdateExportDeclaration(node, 0, false, node.ExportClause(), updatedModuleSpecifier, tx.Visitor().VisitNode(node.Attributes()))
	}
	oldIdentifier := node.ExportClause().Name()
	synthName := tx.Factory().NewGeneratedNameForNode(oldIdentifier)
	importDecl := tx.Factory().NewImportDeclaration(0, tx.Factory().NewImportClause(ast.KindUnknown, ast.Handle{}, tx.Factory().NewNamespaceImport(synthName)), updatedModuleSpecifier, tx.Visitor().VisitNode(node.Attributes()))
	tx.EmitContext().SetOriginal(importDecl, node.ExportClause())
	var exportDecl ast.Handle
	if ast.IsExportNamespaceAsDefaultDeclaration(node) {
		exportDecl = tx.Factory().NewExportAssignment(0, false, ast.Handle{}, synthName)
	} else {
		exportDecl = tx.Factory().NewExportDeclaration(0, false, tx.Factory().NewNamedExports(tx.Factory().NewList([]ast.Handle{tx.Factory().NewExportSpecifier(false, synthName, oldIdentifier)})), ast.Handle{}, ast.Handle{})
	}
	tx.EmitContext().SetOriginal(exportDecl, node)
	return transformers.SingleOrMany([]ast.Handle{importDecl, exportDecl}, tx.Factory())
}
func (tx *ESModuleTransformer) visitCallExpression(node ast.Handle) ast.Handle {
	if tx.compilerOptions.RewriteRelativeImportExtensions.IsTrue() {
		if ast.IsImportCall(node) && len(node.Arguments()) > 0 || ast.IsInJSFile(node) && ast.IsRequireCall(node, false) {
			return tx.visitImportOrRequireCall(node)
		}
	}
	return tx.Visitor().VisitEachChild(node)
}
func (tx *ESModuleTransformer) visitImportOrRequireCall(node ast.Handle) ast.Handle {
	if len(node.Arguments()) == 0 {
		return tx.Visitor().VisitEachChild(node)
	}
	expression := tx.Visitor().VisitNode(node.Expression())
	var argument ast.Handle
	if ast.IsStringLiteralLike(node.Arguments()[0]) {
		argument = rewriteModuleSpecifier(tx.EmitContext(), node.Arguments()[0], tx.compilerOptions)
	} else {
		argument = tx.Factory().NewRewriteRelativeImportExtensionsHelper(node.Arguments()[0], tx.compilerOptions.Jsx == core.JsxEmitPreserve)
	}
	var arguments []ast.Handle
	arguments = append(arguments, argument)
	rest := core.FirstResult(tx.Visitor().VisitSlice(node.Arguments()[1:]))
	arguments = append(arguments, rest...)
	argumentList := tx.Factory().List(node.Store().ListLoc(node.ArgumentList()), arguments...)
	return tx.Factory().UpdateCallExpression(node, expression, node.QuestionDotToken(), 0, argumentList, node.Flags())
}
func (tx *ESModuleTransformer) createRequireCall(node ast.Handle) ast.Handle {
	moduleName := getExternalModuleNameLiteral(tx.Factory(), node, tx.currentSourceFile, nil, nil, tx.compilerOptions)
	var args []ast.Handle
	if !moduleName.IsNil() {
		args = append(args, rewriteModuleSpecifier(tx.EmitContext(), moduleName, tx.compilerOptions))
	}
	if tx.compilerOptions.GetEmitModuleKind() == core.ModuleKindPreserve {
		return tx.Factory().NewCallExpression(tx.Factory().NewIdentifier("require"), ast.Handle{}, 0, tx.Factory().NewList(args), ast.NodeFlagsNone)
	}
	if tx.importRequireStatements == nil {
		createRequireName := tx.Factory().NewUniqueNameEx("_createRequire", printer.AutoGenerateOptions{Flags: printer.GeneratedIdentifierFlagsOptimistic | printer.GeneratedIdentifierFlagsFileLevel})
		importStatement := tx.Factory().NewImportDeclaration(0, tx.Factory().NewImportClause(ast.KindUnknown, ast.Handle{}, tx.Factory().NewNamedImports(tx.Factory().NewList([]ast.Handle{tx.Factory().NewImportSpecifier(false, tx.Factory().NewIdentifier("createRequire"), createRequireName)}))), tx.Factory().NewStringLiteral("module", ast.TokenFlagsNone), ast.Handle{})
		tx.EmitContext().AddEmitFlags(importStatement, printer.EFCustomPrologue)
		requireHelperName := tx.Factory().NewUniqueNameEx("__require", printer.AutoGenerateOptions{Flags: printer.GeneratedIdentifierFlagsOptimistic | printer.GeneratedIdentifierFlagsFileLevel})
		requireStatement := tx.Factory().NewVariableStatement(0, tx.Factory().NewVariableDeclarationList(tx.Factory().NewList([]ast.Handle{tx.Factory().NewVariableDeclaration(requireHelperName, ast.Handle{}, ast.Handle{}, tx.Factory().NewCallExpression(tx.Factory().DeepCloneNode(createRequireName), ast.Handle{}, 0, tx.Factory().NewList([]ast.Handle{tx.Factory().NewPropertyAccessExpression(tx.Factory().NewMetaProperty(ast.KindImportKeyword, tx.Factory().NewIdentifier("meta")), ast.Handle{}, tx.Factory().NewIdentifier("url"), ast.NodeFlagsNone)}), ast.NodeFlagsNone))}), ast.NodeFlagsConst))
		tx.EmitContext().AddEmitFlags(requireStatement, printer.EFCustomPrologue)
		tx.importRequireStatements = &importRequireStatements{statements: []ast.Handle{importStatement, requireStatement}, requireHelperName: requireHelperName}
	}
	return tx.Factory().NewCallExpression(tx.Factory().DeepCloneNode(tx.importRequireStatements.requireHelperName), ast.Handle{}, 0, tx.Factory().NewList(args), ast.NodeFlagsNone)
}
