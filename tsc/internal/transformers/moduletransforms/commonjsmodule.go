package moduletransforms

import (
	"github.com/microsoft/TypeScript/tsc/internal/ast"
	"github.com/microsoft/TypeScript/tsc/internal/binder"
	"github.com/microsoft/TypeScript/tsc/internal/collections"
	"github.com/microsoft/TypeScript/tsc/internal/core"
	"github.com/microsoft/TypeScript/tsc/internal/printer"
	"github.com/microsoft/TypeScript/tsc/internal/transformers"
	"github.com/microsoft/TypeScript/tsc/internal/tspath"
	"slices"
)

func locPtr(h ast.Handle) *core.TextRange {
	if h.IsNil() {
		return nil
	}
	loc := h.Loc()
	return &loc
}

type CommonJSModuleTransformer struct {
	transformers.Transformer
	topLevelVisitor           *ast.HandleVisitor
	topLevelNestedVisitor     *ast.HandleVisitor
	discardedValueVisitor     *ast.HandleVisitor
	assignmentPatternVisitor  *ast.HandleVisitor
	compilerOptions           *core.CompilerOptions
	resolver                  binder.ReferenceResolver
	getEmitModuleFormatOfFile func(file ast.HasFileName) core.ModuleKind
	moduleKind                core.ModuleKind
	languageVersion           core.ScriptTarget
	currentSourceFile         *ast.SourceFile
	currentModuleInfo         *externalModuleInfo
	parentNode                ast.Handle
	currentNode               ast.Handle
}

func NewCommonJSModuleTransformer(opts *transformers.TransformOptions) *transformers.Transformer {
	compilerOptions := opts.CompilerOptions
	emitContext := opts.Context
	tx := &CommonJSModuleTransformer{compilerOptions: compilerOptions, resolver: opts.Resolver, getEmitModuleFormatOfFile: opts.GetEmitModuleFormatOfFile}
	tx.topLevelVisitor = emitContext.NewNodeVisitor(tx.visitTopLevel)
	tx.topLevelNestedVisitor = emitContext.NewNodeVisitor(tx.visitTopLevelNested)
	tx.discardedValueVisitor = emitContext.NewNodeVisitor(tx.visitDiscardedValue)
	tx.assignmentPatternVisitor = emitContext.NewNodeVisitor(tx.visitAssignmentPattern)
	tx.languageVersion = compilerOptions.GetEmitScriptTarget()
	tx.moduleKind = compilerOptions.GetEmitModuleKind()
	return tx.NewTransformer(tx.visit, emitContext)
}

func (tx *CommonJSModuleTransformer) pushNode(node ast.Handle) (grandparentNode ast.Handle) {
	grandparentNode = tx.parentNode
	tx.parentNode = tx.currentNode
	tx.currentNode = node
	return grandparentNode
}

func (tx *CommonJSModuleTransformer) popNode(grandparentNode ast.Handle) {
	tx.currentNode = tx.parentNode
	tx.parentNode = grandparentNode
}

func (tx *CommonJSModuleTransformer) visitTopLevel(node ast.Handle) ast.Handle {
	grandparentNode := tx.pushNode(node)
	defer tx.popNode(grandparentNode)
	switch node.Kind {
	case ast.KindImportDeclaration:
		node = tx.visitTopLevelImportDeclaration(node)
	case ast.KindImportEqualsDeclaration:
		node = tx.visitTopLevelImportEqualsDeclaration(node)
	case ast.KindExportDeclaration:
		node = tx.visitTopLevelExportDeclaration(node)
	case ast.KindExportAssignment:
		node = tx.visitTopLevelExportAssignment(node)
	case ast.KindFunctionDeclaration:
		node = tx.visitTopLevelFunctionDeclaration(node)
	case ast.KindClassDeclaration:
		node = tx.visitTopLevelClassDeclaration(node)
	case ast.KindVariableStatement:
		node = tx.visitTopLevelVariableStatement(node)
	default:
		node = tx.visitTopLevelNestedNoStack(node)
	}
	return node
}

func (tx *CommonJSModuleTransformer) visitTopLevelNested(node ast.Handle) ast.Handle {
	grandparentNode := tx.pushNode(node)
	defer tx.popNode(grandparentNode)
	return tx.visitTopLevelNestedNoStack(node)
}

func (tx *CommonJSModuleTransformer) visitTopLevelNestedNoStack(node ast.Handle) ast.Handle {
	switch node.Kind {
	case ast.KindVariableStatement:
		node = tx.visitTopLevelVariableStatement(node)
	case ast.KindForStatement:
		node = tx.visitTopLevelNestedForStatement(node)
	case ast.KindForInStatement, ast.KindForOfStatement:
		node = tx.visitTopLevelNestedForInOrOfStatement(node)
	case ast.KindDoStatement:
		node = tx.visitTopLevelNestedDoStatement(node)
	case ast.KindWhileStatement:
		node = tx.visitTopLevelNestedWhileStatement(node)
	case ast.KindLabeledStatement:
		node = tx.visitTopLevelNestedLabeledStatement(node)
	case ast.KindWithStatement:
		node = tx.visitTopLevelNestedWithStatement(node)
	case ast.KindIfStatement:
		node = tx.visitTopLevelNestedIfStatement(node)
	case ast.KindSwitchStatement:
		node = tx.visitTopLevelNestedSwitchStatement(node)
	case ast.KindCaseBlock:
		node = tx.visitTopLevelNestedCaseBlock(node)
	case ast.KindCaseClause, ast.KindDefaultClause:
		node = tx.visitTopLevelNestedCaseOrDefaultClause(node)
	case ast.KindTryStatement:
		node = tx.visitTopLevelNestedTryStatement(node)
	case ast.KindCatchClause:
		node = tx.visitTopLevelNestedCatchClause(node)
	case ast.KindBlock:
		node = tx.visitTopLevelNestedBlock(node)
	default:
		node = tx.visitNoStack(node, false)
	}
	return node
}

func (tx *CommonJSModuleTransformer) visit(node ast.Handle) ast.Handle {
	grandparentNode := tx.pushNode(node)
	defer tx.popNode(grandparentNode)
	return tx.visitNoStack(node, false)
}

func (tx *CommonJSModuleTransformer) visitNoStack(node ast.Handle, resultIsDiscarded bool) ast.Handle {
	if !ast.IsSourceFile(node) && node.SubtreeFacts()&(ast.SubtreeContainsDynamicImport|ast.SubtreeContainsIdentifier) == 0 {
		return node
	}
	switch node.Kind {
	case ast.KindSourceFile:
		node = tx.visitSourceFile(node)
	case ast.KindForStatement:
		node = tx.visitForStatement(node)
	case ast.KindForInStatement, ast.KindForOfStatement:
		node = tx.visitForInOrOfStatement(node)
	case ast.KindExpressionStatement:
		node = tx.visitExpressionStatement(node)
	case ast.KindVoidExpression:
		node = tx.visitVoidExpression(node)
	case ast.KindParenthesizedExpression:
		node = tx.visitParenthesizedExpression(node, resultIsDiscarded)
	case ast.KindPartiallyEmittedExpression:
		node = tx.visitPartiallyEmittedExpression(node, resultIsDiscarded)
	case ast.KindCallExpression:
		node = tx.visitCallExpression(node)
	case ast.KindTaggedTemplateExpression:
		node = tx.visitTaggedTemplateExpression(node)
	case ast.KindBinaryExpression:
		node = tx.visitBinaryExpression(node, resultIsDiscarded)
	case ast.KindPrefixUnaryExpression:
		node = tx.visitPrefixUnaryExpression(node, resultIsDiscarded)
	case ast.KindPostfixUnaryExpression:
		node = tx.visitPostfixUnaryExpression(node, resultIsDiscarded)
	case ast.KindShorthandPropertyAssignment:
		node = tx.visitShorthandPropertyAssignment(node)
	case ast.KindIdentifier:
		node = tx.visitIdentifier(node)
	default:
		node = tx.Visitor().VisitEachChild(node)
	}
	return node
}

func (tx *CommonJSModuleTransformer) visitDiscardedValue(node ast.Handle) ast.Handle {
	grandparentNode := tx.pushNode(node)
	defer tx.popNode(grandparentNode)
	return tx.visitNoStack(node, true)
}
func (tx *CommonJSModuleTransformer) visitAssignmentPattern(node ast.Handle) ast.Handle {
	grandparentNode := tx.pushNode(node)
	defer tx.popNode(grandparentNode)
	return tx.visitAssignmentPatternNoStack(node)
}
func (tx *CommonJSModuleTransformer) visitAssignmentPatternNoStack(node ast.Handle) ast.Handle {
	switch node.Kind {
	case ast.KindObjectLiteralExpression, ast.KindArrayLiteralExpression:
		node = tx.assignmentPatternVisitor.VisitEachChild(node)
	case ast.KindPropertyAssignment:
		node = tx.visitAssignmentProperty(node)
	case ast.KindShorthandPropertyAssignment:
		node = tx.visitShorthandAssignmentProperty(node)
	case ast.KindSpreadAssignment:
		node = tx.visitAssignmentRestProperty(node)
	case ast.KindSpreadElement:
		node = tx.visitAssignmentRestElement(node)
	default:
		if ast.IsExpression(node) {
			node = tx.visitAssignmentElement(node)
			break
		}
		node = tx.visitNoStack(node, false)
	}
	return node
}
func (tx *CommonJSModuleTransformer) visitSourceFile(node ast.Handle) ast.Handle {
	file := ast.GetSourceFileOfNode(node)
	if file != nil && file.IsDeclarationFile || !(ast.IsEffectiveExternalModule(file, tx.compilerOptions) || node.SubtreeFacts()&ast.SubtreeContainsDynamicImport != 0) {
		return node
	}
	tx.currentSourceFile = file
	tx.currentModuleInfo = collectExternalModuleInfo(file, tx.compilerOptions, tx.EmitContext(), tx.resolver)
	updated := tx.transformCommonJSModule(node)
	tx.currentSourceFile = nil
	tx.currentModuleInfo = nil
	return updated
}
func (tx *CommonJSModuleTransformer) shouldEmitUnderscoreUnderscoreESModule() bool {
	if tspath.FileExtensionIsOneOf(tx.currentSourceFile.FileName(), tspath.SupportedJSExtensionsFlat) && !tx.currentSourceFile.CommonJSModuleIndicator.IsNil() && (tx.currentSourceFile.ExternalModuleIndicator.IsNil() || tx.currentSourceFile.ExternalModuleIndicator.Kind == ast.KindSourceFile) {
		return false
	}
	if tx.currentModuleInfo.exportEquals.IsNil() && ast.IsExternalModule(tx.currentSourceFile) {
		return true
	}
	return false
}
func (tx *CommonJSModuleTransformer) createUnderscoreUnderscoreESModule() ast.Handle {
	statement := tx.Factory().NewExpressionStatement(tx.Factory().NewCallExpression(tx.Factory().NewPropertyAccessExpression(tx.Factory().NewIdentifier("Object"), ast.Handle{}, tx.Factory().NewIdentifier("defineProperty"), ast.NodeFlagsNone), ast.Handle{}, 0, tx.Factory().NewList([]ast.Handle{tx.Factory().NewIdentifier("exports"), tx.Factory().NewStringLiteral("__esModule", ast.TokenFlagsNone), tx.Factory().NewObjectLiteralExpression(tx.Factory().NewList([]ast.Handle{tx.Factory().NewPropertyAssignment(0, tx.Factory().NewIdentifier("value"), ast.Handle{}, ast.Handle{}, tx.Factory().NewTrueExpression())}), false)}), ast.NodeFlagsNone))
	tx.EmitContext().SetEmitFlags(statement, printer.EFCustomPrologue)
	return statement
}
func (tx *CommonJSModuleTransformer) transformCommonJSModule(node ast.Handle) ast.Handle {
	tx.EmitContext().StartVariableEnvironment()
	prologue, rest := tx.Factory().SplitStandardPrologue(node.Statements())
	statements := slices.Clone(prologue)
	custom, rest := tx.Factory().SplitCustomPrologue(rest)
	statements = append(statements, core.FirstResult(tx.topLevelVisitor.VisitSlice(custom))...)
	if tx.shouldEmitUnderscoreUnderscoreESModule() {
		statements = append(statements, tx.createUnderscoreUnderscoreESModule())
	}
	if len(tx.currentModuleInfo.exportedNames) > 0 {
		const chunkSize = 50
		l := len(tx.currentModuleInfo.exportedNames)
		for i := 0; i < l; i += chunkSize {
			right := tx.Factory().NewVoidZeroExpression()
			for _, nextId := range tx.currentModuleInfo.exportedNames[i:min(i+chunkSize, l)] {
				var left ast.Handle
				if nextId.Kind == ast.KindStringLiteral {
					left = tx.Factory().NewElementAccessExpression(tx.Factory().NewIdentifier("exports"), ast.Handle{}, tx.Factory().NewStringLiteralFromNode(nextId), ast.NodeFlagsNone)
				} else {
					name := tx.Factory().DeepCloneNode(nextId)
					tx.EmitContext().SetEmitFlags(name, printer.EFNoSourceMap|printer.EFNoComments)
					left = tx.Factory().NewPropertyAccessExpression(tx.Factory().NewIdentifier("exports"), ast.Handle{}, name, ast.NodeFlagsNone)
				}
				right = tx.Factory().NewAssignmentExpression(left, right)
			}
			statement := tx.Factory().NewExpressionStatement(right)
			tx.EmitContext().AddEmitFlags(statement, printer.EFCustomPrologue)
			statements = append(statements, statement)
		}
	}
	exportedFunctionsStart := len(statements)
	for f := range tx.currentModuleInfo.exportedFunctions.Values() {
		statements = tx.appendExportsOfClassOrFunctionDeclaration(statements, f)
	}
	for _, s := range statements[exportedFunctionsStart:] {
		tx.EmitContext().AddEmitFlags(s, printer.EFCustomPrologue)
	}
	rest = tx.topLevelVisitor.VisitSlice(rest)
	statements = append(statements, rest...)
	statements = tx.appendExportEqualsIfNeeded(statements)
	statements = tx.EmitContext().EndAndMergeVariableEnvironment(statements)
	statementList := tx.Factory().List(node.Store().ListLoc(node.StatementList()), statements...)
	result := tx.Factory().UpdateSourceFile(node, statementList, node.EndOfFileToken())
	tx.EmitContext().AddEmitHelper(result, tx.EmitContext().ReadEmitHelpers()...)
	externalHelpersImportDeclaration := createExternalHelpersImportDeclarationIfNeeded(tx.EmitContext(), ast.GetSourceFileOfNode(result), tx.compilerOptions, tx.getEmitModuleFormatOfFile(tx.currentSourceFile), false, false, false)
	if !externalHelpersImportDeclaration.IsNil() {
		prologue, rest := tx.Factory().SplitStandardPrologue(result.Statements())
		custom, rest := tx.Factory().SplitCustomPrologue(rest)
		statements := slices.Clone(prologue)
		statements = append(statements, custom...)
		statements = append(statements, tx.topLevelVisitor.VisitNode(externalHelpersImportDeclaration))
		statements = append(statements, rest...)
		statementList := tx.Factory().List(result.Store().ListLoc(result.StatementList()), statements...)
		result = tx.Factory().UpdateSourceFile(result, statementList, node.EndOfFileToken())
	}
	return result
}

func (tx *CommonJSModuleTransformer) appendExportEqualsIfNeeded(statements []ast.Handle) []ast.Handle {
	if !tx.currentModuleInfo.exportEquals.IsNil() {
		expressionResult := tx.visitExportEquals(tx.currentModuleInfo.exportEquals)
		if !expressionResult.IsNil() {
			statement := tx.Factory().NewExpressionStatement(tx.Factory().NewAssignmentExpression(tx.Factory().NewPropertyAccessExpression(tx.Factory().NewIdentifier("module"), ast.Handle{}, tx.Factory().NewIdentifier("exports"), ast.NodeFlagsNone), expressionResult))
			tx.EmitContext().AssignCommentAndSourceMapRanges(statement, tx.currentModuleInfo.exportEquals)
			tx.EmitContext().AddEmitFlags(statement, printer.EFNoComments)
			statements = append(statements, statement)
		}
	}
	return statements
}
func (tx *CommonJSModuleTransformer) visitExportEquals(node ast.Handle) ast.Handle {
	grandparentNode := tx.pushNode(node)
	defer tx.popNode(grandparentNode)
	return tx.Visitor().VisitNode(node.Expression())
}

func (tx *CommonJSModuleTransformer) appendExportsOfImportDeclaration(statements []ast.Handle, decl ast.Handle) []ast.Handle {
	if !tx.currentModuleInfo.exportEquals.IsNil() {
		return statements
	}
	importClause := decl.ImportClause()
	if importClause.IsNil() {
		return statements
	}
	seen := &collections.Set[string]{}
	if !importClause.Name().IsNil() {
		statements = tx.appendExportsOfDeclaration(statements, importClause, seen, false)
	}
	namedBindings := importClause.ImportClauseNamedBindings()
	if !namedBindings.IsNil() {
		switch namedBindings.Kind {
		case ast.KindNamespaceImport:
			statements = tx.appendExportsOfDeclaration(statements, namedBindings, seen, false)
		case ast.KindNamedImports:
			for _, importBinding := range namedBindings.Elements() {
				statements = tx.appendExportsOfDeclaration(statements, importBinding, seen, true)
			}
		}
	}
	return statements
}

func (tx *CommonJSModuleTransformer) appendExportsOfVariableStatement(statements []ast.Handle, node ast.Handle) []ast.Handle {
	return tx.appendExportsOfVariableDeclarationList(statements, node.VariableStatementDeclarationList(), false)
}

func (tx *CommonJSModuleTransformer) appendExportsOfVariableDeclarationList(statements []ast.Handle, node ast.Handle, isForInOrOfInitializer bool) []ast.Handle {
	if !tx.currentModuleInfo.exportEquals.IsNil() {
		return statements
	}
	for _, decl := range node.Declarations() {
		statements = tx.appendExportsOfBindingElement(statements, decl, isForInOrOfInitializer)
	}
	return statements
}

func (tx *CommonJSModuleTransformer) appendExportsOfBindingElement(statements []ast.Handle, decl ast.Handle, isForInOrOfInitializer bool) []ast.Handle {
	if !tx.currentModuleInfo.exportEquals.IsNil() || decl.Name().IsNil() {
		return statements
	}
	if ast.IsBindingPattern(decl.Name()) {
		for _, element := range decl.Name().Elements() {
			if !ast.IsOmittedExpression(element) {
				statements = tx.appendExportsOfBindingElement(statements, element, isForInOrOfInitializer)
			}
		}
	} else if !transformers.IsGeneratedIdentifier(tx.EmitContext(), decl.Name()) && (!ast.IsVariableDeclaration(decl) || !decl.Initializer().IsNil() || isForInOrOfInitializer) {
		statements = tx.appendExportsOfDeclaration(statements, decl, nil, false)
	}
	return statements
}

func (tx *CommonJSModuleTransformer) appendExportsOfClassOrFunctionDeclaration(statements []ast.Handle, decl ast.Handle) []ast.Handle {
	if !tx.currentModuleInfo.exportEquals.IsNil() {
		return statements
	}
	seen := &collections.Set[string]{}
	if ast.HasSyntacticModifier(decl, ast.ModifierFlagsExport) {
		var exportName ast.Handle
		if ast.HasSyntacticModifier(decl, ast.ModifierFlagsDefault) {
			exportName = tx.Factory().NewIdentifier("default")
		} else {
			exportName = tx.Factory().GetDeclarationName(decl)
		}
		exportValue := tx.Factory().GetLocalName(decl)
		statements = tx.appendExportStatement(statements, seen, exportName, exportValue, locPtr(decl), false, false)
	}
	if !decl.Name().IsNil() {
		return tx.appendExportsOfDeclaration(statements, decl, seen, false)
	}
	return statements
}

func (tx *CommonJSModuleTransformer) appendExportsOfDeclaration(statements []ast.Handle, decl ast.Handle, seen *collections.Set[string], liveBinding bool) []ast.Handle {
	if !tx.currentModuleInfo.exportEquals.IsNil() {
		return statements
	}
	if seen == nil {
		seen = &collections.Set[string]{}
	}
	if name := decl.Name(); tx.currentModuleInfo.exportSpecifiers.Len() > 0 && !name.IsNil() && ast.IsIdentifier(name) {
		name = tx.Factory().GetDeclarationName(decl)
		exportSpecifiers := tx.currentModuleInfo.exportSpecifiers.Get(name.Text())
		if len(exportSpecifiers) > 0 {
			exportValue := tx.visitExpressionIdentifier(name)
			for _, exportSpecifier := range exportSpecifiers {
				statements = tx.appendExportStatement(statements, seen, exportSpecifier.Name(), exportValue, locPtr(exportSpecifier.Name()), false, liveBinding)
			}
		}
	}
	return statements
}

func (tx *CommonJSModuleTransformer) appendExportStatement(statements []ast.Handle, seen *collections.Set[string], exportName ast.Handle, expression ast.Handle, location *core.TextRange, allowComments bool, liveBinding bool) []ast.Handle {
	if exportName.Kind != ast.KindStringLiteral {
		if seen.Has(exportName.Text()) {
			return statements
		}
		seen.Add(exportName.Text())
	}
	statements = append(statements, tx.createExportStatement(exportName, expression, location, allowComments, liveBinding))
	return statements
}

func (tx *CommonJSModuleTransformer) createExportStatement(name ast.Handle, value ast.Handle, location *core.TextRange, allowComments bool, liveBinding bool) ast.Handle {
	statement := tx.Factory().NewExpressionStatement(tx.createExportExpression(name, value, nil, liveBinding))
	if location != nil {
		tx.EmitContext().SetCommentRange(statement, *location)
	}
	tx.EmitContext().AddEmitFlags(statement, printer.EFStartOnNewLine)
	if !allowComments {
		tx.EmitContext().AddEmitFlags(statement, printer.EFNoComments)
	}
	return statement
}

func (tx *CommonJSModuleTransformer) createExportExpression(name ast.Handle, value ast.Handle, location *core.TextRange, liveBinding bool) ast.Handle {
	var expression ast.Handle
	if liveBinding {
		expression = tx.Factory().NewCallExpression(tx.Factory().NewPropertyAccessExpression(tx.Factory().NewIdentifier("Object"), ast.Handle{}, tx.Factory().NewIdentifier("defineProperty"), ast.NodeFlagsNone), ast.Handle{}, 0, tx.Factory().NewList([]ast.Handle{tx.Factory().NewIdentifier("exports"), tx.Factory().NewStringLiteralFromNode(name), tx.Factory().NewObjectLiteralExpression(tx.Factory().NewList([]ast.Handle{tx.Factory().NewPropertyAssignment(0, tx.Factory().NewIdentifier("enumerable"), ast.Handle{}, ast.Handle{}, tx.Factory().NewTrueExpression()), tx.Factory().NewPropertyAssignment(0, tx.Factory().NewIdentifier("get"), ast.Handle{}, ast.Handle{}, tx.Factory().NewFunctionExpression(0, ast.Handle{}, ast.Handle{}, 0, tx.Factory().NewList([]ast.Handle{}), ast.Handle{}, ast.Handle{}, tx.Factory().NewBlock(tx.Factory().NewList([]ast.Handle{tx.Factory().NewReturnStatement(value)}), false)))}), false)}), ast.NodeFlagsNone)
	} else {
		var left ast.Handle
		if name.Kind == ast.KindStringLiteral {
			left = tx.Factory().NewElementAccessExpression(tx.Factory().NewIdentifier("exports"), ast.Handle{}, tx.Factory().NewStringLiteralFromNode(name), ast.NodeFlagsNone)
		} else {
			left = tx.Factory().NewPropertyAccessExpression(tx.Factory().NewIdentifier("exports"), ast.Handle{}, tx.Factory().DeepCloneNode(name), ast.NodeFlagsNone)
		}
		expression = tx.Factory().NewAssignmentExpression(left, value)
	}
	if location != nil {
		tx.EmitContext().SetCommentRange(expression, *location)
	}
	return expression
}

func (tx *CommonJSModuleTransformer) createRequireCall(node ast.Handle) ast.Handle {
	var args []ast.Handle
	moduleName := getExternalModuleNameLiteral(tx.Factory(), node, tx.currentSourceFile, nil, nil, tx.compilerOptions)
	if !moduleName.IsNil() {
		args = append(args, rewriteModuleSpecifier(tx.EmitContext(), moduleName, tx.compilerOptions))
	}
	return tx.Factory().NewCallExpression(tx.Factory().NewIdentifier("require"), ast.Handle{}, 0, tx.Factory().NewList(args), ast.NodeFlagsNone)
}
func (tx *CommonJSModuleTransformer) getHelperExpressionForExport(node ast.Handle, innerExpr ast.Handle) ast.Handle {
	if getExportNeedsImportStarHelper(node) {
		return tx.Visitor().VisitNode(tx.Factory().NewImportStarHelper(innerExpr))
	}
	return innerExpr
}
func (tx *CommonJSModuleTransformer) getHelperExpressionForImport(node ast.Handle, innerExpr ast.Handle) ast.Handle {
	if getImportNeedsImportStarHelper(node) {
		return tx.Visitor().VisitNode(tx.Factory().NewImportStarHelper(innerExpr))
	}
	if getImportNeedsImportDefaultHelper(node) {
		return tx.Visitor().VisitNode(tx.Factory().NewImportDefaultHelper(innerExpr))
	}
	return innerExpr
}
func (tx *CommonJSModuleTransformer) visitTopLevelImportDeclaration(node ast.Handle) ast.Handle {
	if node.ImportClause().IsNil() {
		statement := tx.Factory().NewExpressionStatement(tx.createRequireCall(node))
		tx.EmitContext().SetOriginal(statement, node)
		tx.EmitContext().AssignCommentAndSourceMapRanges(statement, node)
		return statement
	}
	var statements []ast.Handle
	var variables []ast.Handle
	namespaceDeclaration := ast.GetNamespaceDeclarationNode(node)
	if !namespaceDeclaration.IsNil() && !ast.IsDefaultImport(node) {
		variables = append(variables, tx.Factory().NewVariableDeclaration(tx.Factory().DeepCloneNode(namespaceDeclaration.Name()), ast.Handle{}, ast.Handle{}, tx.getHelperExpressionForImport(node, tx.createRequireCall(node))))
	} else {
		variables = append(variables, tx.Factory().NewVariableDeclaration(tx.Factory().NewGeneratedNameForNode(node), ast.Handle{}, ast.Handle{}, tx.getHelperExpressionForImport(node, tx.createRequireCall(node))))
		if !namespaceDeclaration.IsNil() && ast.IsDefaultImport(node) {
			variables = append(variables, tx.Factory().NewVariableDeclaration(tx.Factory().DeepCloneNode(namespaceDeclaration.Name()), ast.Handle{}, ast.Handle{}, tx.Factory().NewGeneratedNameForNode(node)))
		}
	}
	varStatement := tx.Factory().NewVariableStatement(0, tx.Factory().NewVariableDeclarationList(tx.Factory().NewList(variables), ast.NodeFlagsConst))
	tx.EmitContext().SetOriginal(varStatement, node)
	tx.EmitContext().AssignCommentAndSourceMapRanges(varStatement, node)
	statements = append(statements, varStatement)
	statements = tx.appendExportsOfImportDeclaration(statements, node)
	return transformers.SingleOrMany(statements, tx.Factory())
}
func (tx *CommonJSModuleTransformer) visitTopLevelImportEqualsDeclaration(node ast.Handle) ast.Handle {
	if !ast.IsExternalModuleImportEqualsDeclaration(node) {
		panic("import= for internal module references should be handled in an earlier transformer.")
	}
	var statements []ast.Handle
	if ast.HasSyntacticModifier(node, ast.ModifierFlagsExport) {
		statement := tx.Factory().NewExpressionStatement(tx.createExportExpression(node.Name(), tx.createRequireCall(node), locPtr(node), false))
		tx.EmitContext().SetOriginal(statement, node)
		tx.EmitContext().AssignCommentAndSourceMapRanges(statement, node)
		statements = append(statements, statement)
	} else {
		statement := tx.Factory().NewVariableStatement(0, tx.Factory().NewVariableDeclarationList(tx.Factory().NewList([]ast.Handle{tx.Factory().NewVariableDeclaration(tx.Factory().DeepCloneNode(node.Name()), ast.Handle{}, ast.Handle{}, tx.createRequireCall(node))}), ast.NodeFlagsConst))
		tx.EmitContext().SetOriginal(statement, node)
		tx.EmitContext().AssignCommentAndSourceMapRanges(statement, node)
		statements = append(statements, statement)
	}
	statements = tx.appendExportsOfDeclaration(statements, node, nil, false)
	return transformers.SingleOrMany(statements, tx.Factory())
}
func (tx *CommonJSModuleTransformer) visitTopLevelExportDeclaration(node ast.Handle) ast.Handle {
	if node.ModuleSpecifier().IsNil() {
		return ast.Handle{}
	}
	generatedName := tx.Factory().NewGeneratedNameForNode(node)
	if !node.ExportClause().IsNil() && ast.IsNamedExports(node.ExportClause()) {
		var statements []ast.Handle
		varStatement := tx.Factory().NewVariableStatement(0, tx.Factory().NewVariableDeclarationList(tx.Factory().NewList([]ast.Handle{tx.Factory().NewVariableDeclaration(generatedName, ast.Handle{}, ast.Handle{}, tx.createRequireCall(node))}), ast.NodeFlagsNone))
		tx.EmitContext().SetOriginal(varStatement, node)
		tx.EmitContext().AssignCommentAndSourceMapRanges(varStatement, node)
		statements = append(statements, varStatement)
		for _, specifier := range node.ExportClause().Elements() {
			specifierName := specifier.PropertyNameOrName()
			exportNeedsImportDefault := ast.ModuleExportNameIsDefault(specifierName)
			var target ast.Handle
			if exportNeedsImportDefault {
				target = tx.Factory().NewImportDefaultHelper(generatedName)
			} else {
				target = generatedName
			}
			var exportName ast.Handle
			if ast.IsStringLiteral(specifier.Name()) {
				exportName = tx.Factory().NewStringLiteralFromNode(specifier.Name())
			} else {
				exportName = tx.Factory().GetExportName(specifier)
			}
			var exportedValue ast.Handle
			if ast.IsStringLiteral(specifierName) {
				exportedValue = tx.Factory().NewElementAccessExpression(target, ast.Handle{}, specifierName, ast.NodeFlagsNone)
			} else {
				exportedValue = tx.Factory().NewPropertyAccessExpression(target, ast.Handle{}, specifierName, ast.NodeFlagsNone)
			}
			statement := tx.Factory().NewExpressionStatement(tx.createExportExpression(exportName, exportedValue, nil, true))
			tx.EmitContext().SetOriginal(statement, specifier)
			tx.EmitContext().AssignCommentAndSourceMapRanges(statement, specifier)
			statements = append(statements, statement)
		}
		return transformers.SingleOrMany(statements, tx.Factory())
	}
	if !node.ExportClause().IsNil() {
		var exportName ast.Handle
		if ast.IsStringLiteral(node.ExportClause().Name()) {
			exportName = tx.Factory().NewStringLiteralFromNode(node.ExportClause().Name())
		} else {
			exportName = tx.Factory().DeepCloneNode(node.ExportClause().Name())
		}
		statement := tx.Factory().NewExpressionStatement(tx.createExportExpression(exportName, tx.getHelperExpressionForExport(node, tx.createRequireCall(node)), nil, false))
		tx.EmitContext().SetOriginal(statement, node)
		tx.EmitContext().AssignCommentAndSourceMapRanges(statement, node)
		return statement
	}
	statement := tx.Factory().NewExpressionStatement(tx.Visitor().VisitNode(tx.Factory().NewExportStarHelper(tx.createRequireCall(node), tx.Factory().NewIdentifier("exports"))))
	tx.EmitContext().SetOriginal(statement, node)
	tx.EmitContext().AssignCommentAndSourceMapRanges(statement, node)
	return statement
}
func (tx *CommonJSModuleTransformer) visitTopLevelExportAssignment(node ast.Handle) ast.Handle {
	if node.IsExportEquals() {
		return ast.Handle{}
	}
	return tx.createExportStatement(tx.Factory().NewIdentifier("default"), tx.Visitor().VisitNode(node.Expression()), locPtr(node), true, false)
}
func (tx *CommonJSModuleTransformer) visitTopLevelFunctionDeclaration(node ast.Handle) ast.Handle {
	if ast.HasSyntacticModifier(node, ast.ModifierFlagsExport) {
		return tx.Factory().UpdateFunctionDeclaration(node, transformers.ExtractModifiers(tx.EmitContext(), node.Modifiers(), ^ast.ModifierFlagsExportDefault), node.AsteriskToken(), tx.Factory().GetDeclarationName(node), 0, tx.Visitor().VisitNodes(node.ParameterList()), ast.Handle{}, ast.Handle{}, tx.Visitor().VisitNode(node.Body()))
	} else {
		return tx.Visitor().VisitEachChild(node)
	}
}
func (tx *CommonJSModuleTransformer) visitTopLevelClassDeclaration(node ast.Handle) ast.Handle {
	var statements []ast.Handle
	if ast.HasSyntacticModifier(node, ast.ModifierFlagsExport) {
		statements = append(statements, tx.Factory().UpdateClassDeclaration(node, tx.Visitor().VisitModifiers(transformers.ExtractModifiers(tx.EmitContext(), node.Modifiers(), ^ast.ModifierFlagsExportDefault)), tx.Factory().GetDeclarationName(node), 0, tx.Visitor().VisitNodes(node.HeritageClauses()), tx.Visitor().VisitNodes(node.MemberList())))
	} else {
		statements = append(statements, tx.Visitor().VisitEachChild(node))
	}
	statements = tx.appendExportsOfClassOrFunctionDeclaration(statements, node)
	return transformers.SingleOrMany(statements, tx.Factory())
}
func (tx *CommonJSModuleTransformer) visitTopLevelVariableStatement(node ast.Handle) ast.Handle {
	var statements []ast.Handle
	if ast.HasSyntacticModifier(node, ast.ModifierFlagsExport) {
		var variables []ast.Handle
		var expressions []ast.Handle
		var modifiers ast.ListRef
		commitPendingVariables := func() {
			if len(variables) > 0 {
				variableList := tx.Factory().NewList(variables)
				statement := tx.Factory().UpdateVariableStatement(node, modifiers, tx.Factory().UpdateVariableDeclarationList(node.VariableStatementDeclarationList(), variableList, node.VariableStatementDeclarationList().Flags()))
				if len(statements) > 0 {
					tx.EmitContext().AddEmitFlags(statement, printer.EFNoComments)
				}
				statements = append(statements, statement)
				variables = nil
			}
		}
		commitPendingExpressions := func() {
			if len(expressions) > 0 {
				statement := tx.Factory().NewExpressionStatement(tx.Factory().InlineExpressions(expressions))
				tx.EmitContext().AssignCommentAndSourceMapRanges(statement, node)
				if len(statements) > 0 {
					tx.EmitContext().AddEmitFlags(statement, printer.EFNoComments)
				}
				statements = append(statements, statement)
				expressions = nil
			}
		}
		pushVariable := func(variable ast.Handle) {
			commitPendingExpressions()
			variables = append(variables, variable)
		}
		pushExpression := func(expression ast.Handle) {
			commitPendingVariables()
			expressions = append(expressions, expression)
		}
		for _, variable := range node.VariableStatementDeclarationList().Declarations() {
			v := variable
			if ast.IsIdentifier(v.Name()) && transformers.IsLocalName(tx.EmitContext(), v.Name()) {
				if modifiers == 0 {
					modifiers = transformers.ExtractModifiers(tx.EmitContext(), node.Modifiers(), ^ast.ModifierFlagsExportDefault)
				}
				if !v.Initializer().IsNil() {
					variable = tx.Factory().UpdateVariableDeclaration(v, v.Name(), ast.Handle{}, ast.Handle{}, tx.createExportExpression(v.Name(), tx.Visitor().VisitNode(v.Initializer()), nil, false))
				}
				pushVariable(variable)
			} else if !v.Initializer().IsNil() && !ast.IsBindingPattern(v.Name()) && (ast.IsArrowFunction(v.Initializer()) || ast.IsFunctionExpression(v.Initializer()) || ast.IsClassExpression(v.Initializer())) {
				pushVariable(tx.Factory().NewVariableDeclaration(v.Name(), v.ExclamationToken(), v.Type(), tx.Visitor().VisitNode(v.Initializer())))
				propertyAccess := tx.Factory().NewPropertyAccessExpression(tx.Factory().NewIdentifier("exports"), ast.Handle{}, v.Name(), ast.NodeFlagsNone)
				tx.EmitContext().AssignCommentAndSourceMapRanges(propertyAccess, v.Name())
				pushExpression(tx.Factory().NewAssignmentExpression(propertyAccess, tx.Factory().DeepCloneNode(v.Name())))
			} else if ast.IsIdentifier(v.Name()) {
				expression := tx.transformInitializedVariable(v)
				if !expression.IsNil() {
					pushExpression(tx.Visitor().VisitNode(expression))
				}
			} else if ast.IsBindingPattern(v.Name()) {
				expression := tx.transformInitializedVariable(v)
				if !expression.IsNil() {
					pushExpression(expression)
				}
			} else {
				expression := transformers.ConvertVariableDeclarationToAssignmentExpression(tx.EmitContext(), v)
				if !expression.IsNil() {
					pushExpression(tx.Visitor().VisitNode(expression))
				}
			}
		}
		commitPendingVariables()
		commitPendingExpressions()
		statements = tx.appendExportsOfVariableStatement(statements, node)
		return transformers.SingleOrMany(statements, tx.Factory())
	}
	return tx.visitTopLevelNestedVariableStatement(node)
}
func (tx *CommonJSModuleTransformer) transformInitializedVariable(node ast.Handle) ast.Handle {
	if node.Initializer().IsNil() {
		return ast.Handle{}
	}
	name := node.Name()
	if ast.IsBindingPattern(name) {
		assignment := transformers.ConvertVariableDeclarationToAssignmentExpression(tx.EmitContext(), node)
		grandparentNode := tx.pushNode(assignment)
		defer tx.popNode(grandparentNode)
		return tx.visitDestructuringAssignment(assignment, true)
	}
	propertyAccess := tx.Factory().NewPropertyAccessExpression(tx.Factory().NewIdentifier("exports"), ast.Handle{}, name, ast.NodeFlagsNone)
	tx.EmitContext().AssignCommentAndSourceMapRanges(propertyAccess, name)
	return tx.Factory().NewAssignmentExpression(propertyAccess, node.Initializer())
}

func (tx *CommonJSModuleTransformer) visitTopLevelNestedVariableStatement(node ast.Handle) ast.Handle {
	var statements []ast.Handle
	statements = append(statements, tx.Visitor().VisitEachChild(node))
	statements = tx.appendExportsOfVariableStatement(statements, node)
	return transformers.SingleOrMany(statements, tx.Factory())
}

func (tx *CommonJSModuleTransformer) visitTopLevelNestedForStatement(node ast.Handle) ast.Handle {
	if !node.Initializer().IsNil() && ast.IsVariableDeclarationList(node.Initializer()) && node.Initializer().Flags()&ast.NodeFlagsBlockScoped == 0 {
		exportStatements := tx.appendExportsOfVariableDeclarationList(nil, node.Initializer(), false)
		if len(exportStatements) > 0 {
			var statements []ast.Handle
			varDeclList := tx.discardedValueVisitor.VisitNode(node.Initializer())
			varStatement := tx.Factory().NewVariableStatement(0, varDeclList)
			statements = append(statements, varStatement)
			statements = append(statements, exportStatements...)
			condition := tx.Visitor().VisitNode(node.Condition())
			incrementor := tx.discardedValueVisitor.VisitNode(node.Incrementor())
			body := tx.EmitContext().VisitIterationBody(node.Statement(), tx.topLevelNestedVisitor)
			statements = append(statements, tx.Factory().UpdateForStatement(node, ast.Handle{}, condition, incrementor, body))
			return transformers.SingleOrMany(statements, tx.Factory())
		}
	}
	return tx.Factory().UpdateForStatement(node, tx.discardedValueVisitor.VisitNode(node.Initializer()), tx.Visitor().VisitNode(node.Condition()), tx.discardedValueVisitor.VisitNode(node.Incrementor()), tx.EmitContext().VisitIterationBody(node.Statement(), tx.topLevelNestedVisitor))
}

func (tx *CommonJSModuleTransformer) visitTopLevelNestedForInOrOfStatement(node ast.Handle) ast.Handle {
	if ast.IsVariableDeclarationList(node.Initializer()) && node.Initializer().Flags()&ast.NodeFlagsBlockScoped == 0 {
		exportStatements := tx.appendExportsOfVariableDeclarationList(nil, node.Initializer(), true)
		if len(exportStatements) > 0 {
			initializer := tx.discardedValueVisitor.VisitNode(node.Initializer())
			expression := tx.Visitor().VisitNode(node.Expression())
			body := tx.EmitContext().VisitIterationBody(node.Statement(), tx.topLevelNestedVisitor)
			if ast.IsBlock(body) {
				block := body
				bodyStatements := append(exportStatements, block.StatementsSeq().Slice()...)
				bodyStatementList := tx.Factory().List(block.Store().ListLoc(block.StatementList()), bodyStatements...)
				body = tx.Factory().UpdateBlock(block, bodyStatementList, block.MultiLine())
			} else {
				bodyStatements := append(exportStatements, body)
				body = tx.Factory().NewBlock(tx.Factory().NewList(bodyStatements), true)
			}
			return tx.Factory().UpdateForInOrOfStatement(node, node.AwaitModifier(), initializer, expression, body)
		}
	}
	return tx.Factory().UpdateForInOrOfStatement(node, node.AwaitModifier(), tx.discardedValueVisitor.VisitNode(node.Initializer()), tx.Visitor().VisitNode(node.Expression()), tx.EmitContext().VisitIterationBody(node.Statement(), tx.topLevelNestedVisitor))
}

func (tx *CommonJSModuleTransformer) visitTopLevelNestedDoStatement(node ast.Handle) ast.Handle {
	return tx.Factory().UpdateDoStatement(node, tx.EmitContext().VisitIterationBody(node.Statement(), tx.topLevelNestedVisitor), tx.Visitor().VisitNode(node.Expression()))
}

func (tx *CommonJSModuleTransformer) visitTopLevelNestedWhileStatement(node ast.Handle) ast.Handle {
	return tx.Factory().UpdateWhileStatement(node, tx.Visitor().VisitNode(node.Expression()), tx.EmitContext().VisitIterationBody(node.Statement(), tx.topLevelNestedVisitor))
}

func (tx *CommonJSModuleTransformer) visitTopLevelNestedLabeledStatement(node ast.Handle) ast.Handle {
	statement := tx.topLevelNestedVisitor.VisitEmbeddedStatement(node.Statement())
	if statement.IsNil() {
		statement = tx.Factory().NewEmptyStatement()
	}
	return tx.Factory().UpdateLabeledStatement(node, node.Label(), statement)
}

func (tx *CommonJSModuleTransformer) visitTopLevelNestedWithStatement(node ast.Handle) ast.Handle {
	return tx.Factory().UpdateWithStatement(node, tx.Visitor().VisitNode(node.Expression()), tx.topLevelNestedVisitor.VisitEmbeddedStatement(node.Statement()))
}

func (tx *CommonJSModuleTransformer) visitTopLevelNestedIfStatement(node ast.Handle) ast.Handle {
	expression := tx.Visitor().VisitNode(node.Expression())
	thenStatement := tx.topLevelNestedVisitor.VisitEmbeddedStatement(node.ThenStatement())
	if thenStatement.IsNil() {
		thenStatement = tx.Factory().NewBlock(tx.Factory().NewList(nil), false)
	}
	elseStatement := tx.topLevelNestedVisitor.VisitEmbeddedStatement(node.ElseStatement())
	return tx.Factory().UpdateIfStatement(node, expression, thenStatement, elseStatement)
}

func (tx *CommonJSModuleTransformer) visitTopLevelNestedSwitchStatement(node ast.Handle) ast.Handle {
	return tx.Factory().UpdateSwitchStatement(node, tx.Visitor().VisitNode(node.Expression()), tx.topLevelNestedVisitor.VisitNode(node.CaseBlock()))
}

func (tx *CommonJSModuleTransformer) visitTopLevelNestedCaseBlock(node ast.Handle) ast.Handle {
	return tx.topLevelNestedVisitor.VisitEachChild(node)
}

func (tx *CommonJSModuleTransformer) visitTopLevelNestedCaseOrDefaultClause(node ast.Handle) ast.Handle {
	return tx.Factory().UpdateCaseOrDefaultClause(node, tx.Visitor().VisitNode(node.Expression()), tx.topLevelNestedVisitor.VisitNodes(node.StatementList()))
}

func (tx *CommonJSModuleTransformer) visitTopLevelNestedTryStatement(node ast.Handle) ast.Handle {
	return tx.topLevelNestedVisitor.VisitEachChild(node)
}

func (tx *CommonJSModuleTransformer) visitTopLevelNestedCatchClause(node ast.Handle) ast.Handle {
	return tx.Factory().UpdateCatchClause(node, node.VariableDeclaration(), tx.topLevelNestedVisitor.VisitNode(node.Block()))
}

func (tx *CommonJSModuleTransformer) visitTopLevelNestedBlock(node ast.Handle) ast.Handle {
	return tx.topLevelNestedVisitor.VisitEachChild(node)
}
func (tx *CommonJSModuleTransformer) visitForStatement(node ast.Handle) ast.Handle {
	return tx.Factory().UpdateForStatement(node, tx.discardedValueVisitor.VisitNode(node.Initializer()), tx.Visitor().VisitNode(node.Condition()), tx.discardedValueVisitor.VisitNode(node.Incrementor()), tx.EmitContext().VisitIterationBody(node.Statement(), tx.topLevelNestedVisitor))
}
func (tx *CommonJSModuleTransformer) visitForInOrOfStatement(node ast.Handle) ast.Handle {
	return tx.Factory().UpdateForInOrOfStatement(node, node.AwaitModifier(), tx.discardedValueVisitor.VisitNode(node.Initializer()), tx.Visitor().VisitNode(node.Expression()), tx.EmitContext().VisitIterationBody(node.Statement(), tx.topLevelNestedVisitor))
}

func (tx *CommonJSModuleTransformer) visitExpressionStatement(node ast.Handle) ast.Handle {
	return tx.discardedValueVisitor.VisitEachChild(node)
}

func (tx *CommonJSModuleTransformer) visitVoidExpression(node ast.Handle) ast.Handle {
	return tx.discardedValueVisitor.VisitEachChild(node)
}

func (tx *CommonJSModuleTransformer) visitParenthesizedExpression(node ast.Handle, resultIsDiscarded bool) ast.Handle {
	expression := core.IfElse(resultIsDiscarded, tx.discardedValueVisitor, tx.Visitor()).VisitNode(node.Expression())
	return tx.Factory().UpdateParenthesizedExpression(node, expression)
}

func (tx *CommonJSModuleTransformer) visitPartiallyEmittedExpression(node ast.Handle, resultIsDiscarded bool) ast.Handle {
	expression := core.IfElse(resultIsDiscarded, tx.discardedValueVisitor, tx.Visitor()).VisitNode(node.Expression())
	return tx.Factory().UpdatePartiallyEmittedExpression(node, expression)
}

func (tx *CommonJSModuleTransformer) visitBinaryExpression(node ast.Handle, resultIsDiscarded bool) ast.Handle {
	if ast.IsDestructuringAssignment(node) {
		return tx.visitDestructuringAssignment(node, resultIsDiscarded)
	}
	if ast.IsAssignmentExpression(node, false) {
		return tx.visitAssignmentExpression(node)
	}
	if ast.IsCommaExpression(node) {
		return tx.visitCommaExpression(node, resultIsDiscarded)
	}
	return tx.Visitor().VisitEachChild(node)
}
func (tx *CommonJSModuleTransformer) visitAssignmentExpression(node ast.Handle) ast.Handle {
	if ast.IsIdentifier(node.Left()) && (!transformers.IsGeneratedIdentifier(tx.EmitContext(), node.Left()) || isFileLevelReservedGeneratedIdentifier(tx.EmitContext(), node.Left())) && !transformers.IsLocalName(tx.EmitContext(), node.Left()) {
		exportedNames := tx.getExports(node.Left())
		if len(exportedNames) > 0 {
			expression := tx.Visitor().VisitEachChild(node)
			for _, exportName := range exportedNames {
				expression = tx.createExportExpression(exportName, expression, locPtr(node), false)
			}
			return expression
		}
	}
	return tx.Visitor().VisitEachChild(node)
}

func (tx *CommonJSModuleTransformer) visitDestructuringAssignment(node ast.Handle, valueIsDiscarded bool) ast.Handle {
	if tx.destructuringNeedsFlattening(node.Left()) {
		return transformers.FlattenDestructuringAssignment(&tx.Transformer, node, !valueIsDiscarded, transformers.FlattenLevelAll, tx.createAllExportExpressions)
	}
	return tx.Visitor().VisitEachChild(node)
}

func (tx *CommonJSModuleTransformer) destructuringNeedsFlattening(node ast.Handle) bool {
	if ast.IsObjectLiteralExpression(node) {
		for _, elem := range node.Properties() {
			switch elem.Kind {
			case ast.KindPropertyAssignment:
				if tx.destructuringNeedsFlattening(elem.Initializer()) {
					return true
				}
			case ast.KindShorthandPropertyAssignment:
				if tx.destructuringNeedsFlattening(elem.Name()) {
					return true
				}
			case ast.KindSpreadAssignment:
				if tx.destructuringNeedsFlattening(elem.Expression()) {
					return true
				}
			case ast.KindMethodDeclaration, ast.KindGetAccessor, ast.KindSetAccessor:
				return false
			}
		}
	} else if ast.IsArrayLiteralExpression(node) {
		for _, elem := range node.Store().ListSlice(node.ArrayLiteralExpressionElements()) {
			if ast.IsSpreadElement(elem) {
				if tx.destructuringNeedsFlattening(elem.Expression()) {
					return true
				}
			} else if tx.destructuringNeedsFlattening(elem) {
				return true
			}
		}
	} else if ast.IsIdentifier(node) {
		exportedNames := tx.getExports(node)
		if transformers.IsExportName(tx.EmitContext(), node) {
			return len(exportedNames) > 1
		}
		if len(exportedNames) == 0 {
			return false
		}
		if len(exportedNames) == 1 && tx.isDirectExport(node) && exportedNames[0].Text() == node.Text() {
			return false
		}
		return true
	}
	return false
}

func (tx *CommonJSModuleTransformer) createAllExportExpressions(name ast.Handle, value ast.Handle, location *core.TextRange) ast.Handle {
	exportedNames := tx.getExports(name)
	if len(exportedNames) > 0 {
		var expression ast.Handle
		if tx.isDirectExport(name) {
			exportName := tx.Factory().DeepCloneNode(name)
			tx.EmitContext().AddEmitFlags(exportName, printer.EFNoComments|printer.EFNoSourceMap)
			propertyAccess := tx.Factory().NewPropertyAccessExpression(tx.Factory().NewIdentifier("exports"), ast.Handle{}, exportName, ast.NodeFlagsNone)
			tx.EmitContext().AddEmitFlags(propertyAccess, printer.EFNoComments)
			expression = tx.Factory().NewAssignmentExpression(propertyAccess, value)
			tx.EmitContext().AssignCommentAndSourceMapRanges(expression, name)
		} else {
			expression = tx.Factory().NewAssignmentExpression(name, value)
		}
		for _, exportName := range exportedNames {
			expression = tx.createExportExpression(exportName, expression, location, false)
		}
		return expression
	}
	if tx.isDirectExport(name) {
		exportName := tx.Factory().DeepCloneNode(name)
		tx.EmitContext().AddEmitFlags(exportName, printer.EFNoComments|printer.EFNoSourceMap)
		propertyAccess := tx.Factory().NewPropertyAccessExpression(tx.Factory().NewIdentifier("exports"), ast.Handle{}, exportName, ast.NodeFlagsNone)
		tx.EmitContext().AddEmitFlags(propertyAccess, printer.EFNoComments)
		result := tx.Factory().NewAssignmentExpression(propertyAccess, value)
		tx.EmitContext().AssignCommentAndSourceMapRanges(result, name)
		return result
	}
	return tx.Factory().NewAssignmentExpression(name, value)
}

func (tx *CommonJSModuleTransformer) isDirectExport(name ast.Handle) bool {
	exportContainer := tx.resolver.GetReferencedExportContainer(tx.EmitContext().MostOriginal(name), false)
	return !exportContainer.IsNil() && ast.IsSourceFile(exportContainer)
}
func (tx *CommonJSModuleTransformer) visitAssignmentProperty(node ast.Handle) ast.Handle {
	return tx.Factory().UpdatePropertyAssignment(node, 0, tx.Visitor().VisitNode(node.Name()), ast.Handle{}, ast.Handle{}, tx.assignmentPatternVisitor.VisitNode(node.Initializer()))
}
func (tx *CommonJSModuleTransformer) visitShorthandAssignmentProperty(node ast.Handle) ast.Handle {
	target := tx.visitDestructuringAssignmentTargetNoStack(node.Name())
	if ast.IsIdentifier(target) {
		return tx.Factory().UpdateShorthandPropertyAssignment(node, 0, target, ast.Handle{}, ast.Handle{}, node.EqualsToken(), tx.Visitor().VisitNode(node.ObjectAssignmentInitializer()))
	}
	if !node.ObjectAssignmentInitializer().IsNil() {
		equalsToken := node.EqualsToken()
		if equalsToken.IsNil() {
			equalsToken = tx.Factory().NewToken(ast.KindEqualsToken)
		}
		target = tx.Factory().NewBinaryExpression(0, target, ast.Handle{}, equalsToken, tx.Visitor().VisitNode(node.ObjectAssignmentInitializer()))
	}
	updated := tx.Factory().NewPropertyAssignment(0, node.Name(), ast.Handle{}, ast.Handle{}, target)
	tx.EmitContext().SetOriginal(updated, node)
	tx.EmitContext().AssignCommentAndSourceMapRanges(updated, node)
	return updated
}
func (tx *CommonJSModuleTransformer) visitAssignmentRestProperty(node ast.Handle) ast.Handle {
	return tx.Factory().UpdateSpreadAssignment(node, tx.visitDestructuringAssignmentTarget(node.Expression()))
}
func (tx *CommonJSModuleTransformer) visitAssignmentRestElement(node ast.Handle) ast.Handle {
	return tx.Factory().UpdateSpreadElement(node, tx.visitDestructuringAssignmentTarget(node.Expression()))
}
func (tx *CommonJSModuleTransformer) visitAssignmentElement(node ast.Handle) ast.Handle {
	if ast.IsBinaryExpression(node) {
		n := node
		if n.OperatorToken().Kind == ast.KindEqualsToken {
			return tx.Factory().UpdateBinaryExpression(n, 0, tx.visitDestructuringAssignmentTarget(n.Left()), ast.Handle{}, n.OperatorToken(), tx.Visitor().VisitNode(n.Right()))
		}
	}
	return tx.visitDestructuringAssignmentTargetNoStack(node)
}
func (tx *CommonJSModuleTransformer) visitDestructuringAssignmentTarget(node ast.Handle) ast.Handle {
	grandparentNode := tx.pushNode(node)
	defer tx.popNode(grandparentNode)
	switch node.Kind {
	case ast.KindObjectLiteralExpression, ast.KindArrayLiteralExpression:
		node = tx.visitAssignmentPatternNoStack(node)
	default:
		node = tx.visitDestructuringAssignmentTargetNoStack(node)
	}
	return node
}
func (tx *CommonJSModuleTransformer) visitDestructuringAssignmentTargetNoStack(node ast.Handle) ast.Handle {
	if ast.IsIdentifier(node) && (!transformers.IsGeneratedIdentifier(tx.EmitContext(), node) || isFileLevelReservedGeneratedIdentifier(tx.EmitContext(), node)) && !transformers.IsLocalName(tx.EmitContext(), node) {
		expression := tx.visitExpressionIdentifier(node)
		exportedNames := tx.getExports(node)
		if len(exportedNames) > 0 {
			value := tx.Factory().NewUniqueNameEx("value", printer.AutoGenerateOptions{Flags: printer.GeneratedIdentifierFlagsOptimistic})
			expression = tx.Factory().NewAssignmentExpression(expression, value)
			for _, exportName := range exportedNames {
				expression = tx.createExportExpression(exportName, expression, nil, false)
			}
			statement := tx.Factory().NewExpressionStatement(expression)
			statementList := tx.Factory().NewList([]ast.Handle{statement})
			param := tx.Factory().NewParameterDeclaration(0, ast.Handle{}, value, ast.Handle{}, ast.Handle{}, ast.Handle{})
			valueSetter := tx.Factory().NewSetAccessorDeclaration(0, tx.Factory().NewIdentifier("value"), 0, tx.Factory().NewList([]ast.Handle{param}), ast.Handle{}, ast.Handle{}, tx.Factory().NewBlock(statementList, false))
			propertyList := tx.Factory().NewList([]ast.Handle{valueSetter})
			expression = tx.Factory().NewObjectLiteralExpression(propertyList, false)
			expression = tx.Factory().NewPropertyAccessExpression(expression, ast.Handle{}, tx.Factory().NewIdentifier("value"), ast.NodeFlagsNone)
		}
		return expression
	}
	return tx.visitNoStack(node, false)
}

func (tx *CommonJSModuleTransformer) visitCommaExpression(node ast.Handle, resultIsDiscarded bool) ast.Handle {
	left := tx.discardedValueVisitor.VisitNode(node.Left())
	right := core.IfElse(resultIsDiscarded, tx.discardedValueVisitor, tx.Visitor()).VisitNode(node.Right())
	return tx.Factory().UpdateBinaryExpression(node, 0, left, ast.Handle{}, node.OperatorToken(), right)
}

func (tx *CommonJSModuleTransformer) visitPrefixUnaryExpression(node ast.Handle, resultIsDiscarded bool) ast.Handle {
	if (node.PrefixUnaryExpressionOperator() == ast.KindPlusPlusToken || node.PrefixUnaryExpressionOperator() == ast.KindMinusMinusToken) && ast.IsIdentifier(node.Operand()) && !transformers.IsLocalName(tx.EmitContext(), node.Operand()) {
		exportedNames := tx.getExports(node.Operand())
		if len(exportedNames) > 0 {
			expression := tx.Factory().UpdatePrefixUnaryExpression(node, node.PrefixUnaryExpressionOperator(), tx.Visitor().VisitNode(node.Operand()))
			for _, exportName := range exportedNames {
				expression = tx.createExportExpression(exportName, expression, nil, false)
				tx.EmitContext().AssignCommentAndSourceMapRanges(expression, node)
			}
			return expression
		}
	}
	return tx.Visitor().VisitEachChild(node)
}

func (tx *CommonJSModuleTransformer) visitPostfixUnaryExpression(node ast.Handle, resultIsDiscarded bool) ast.Handle {
	if (node.PostfixUnaryExpressionOperator() == ast.KindPlusPlusToken || node.PostfixUnaryExpressionOperator() == ast.KindMinusMinusToken) && ast.IsIdentifier(node.Operand()) && !transformers.IsLocalName(tx.EmitContext(), node.Operand()) {
		exportedNames := tx.getExports(node.Operand())
		if len(exportedNames) > 0 {
			var temp ast.Handle
			expression := tx.Factory().UpdatePostfixUnaryExpression(node, tx.Visitor().VisitNode(node.Operand()), node.PostfixUnaryExpressionOperator())
			if !resultIsDiscarded {
				temp = tx.Factory().NewTempVariable()
				tx.EmitContext().AddVariableDeclaration(temp)
				expression = tx.Factory().NewAssignmentExpression(temp, expression)
				tx.EmitContext().AssignCommentAndSourceMapRanges(expression, node)
			}
			expression = tx.Factory().NewCommaExpression(expression, tx.Factory().DeepCloneNode(node.Operand()))
			tx.EmitContext().AssignCommentAndSourceMapRanges(expression, node)
			for _, exportName := range exportedNames {
				expression = tx.createExportExpression(exportName, expression, nil, false)
				tx.EmitContext().AssignCommentAndSourceMapRanges(expression, node)
			}
			if !temp.IsNil() {
				expression = tx.Factory().NewCommaExpression(expression, temp)
				tx.EmitContext().AssignCommentAndSourceMapRanges(expression, node)
			}
			return expression
		}
	}
	return tx.Visitor().VisitEachChild(node)
}

func (tx *CommonJSModuleTransformer) visitCallExpression(node ast.Handle) ast.Handle {
	needsRewrite := false
	if tx.compilerOptions.RewriteRelativeImportExtensions.IsTrue() {
		if ast.IsImportCall(node) && len(node.Arguments()) > 0 || ast.IsInJSFile(node) && ast.IsRequireCall(node, false) {
			needsRewrite = true
		}
	}
	if ast.IsImportCall(node) && tx.shouldTransformImportCall() {
		return tx.visitImportCallExpression(node, needsRewrite)
	}
	if needsRewrite {
		return tx.shimOrRewriteImportOrRequireCall(node)
	}
	if ast.IsIdentifier(node.Expression()) {
		expression := tx.visitExpressionIdentifier(node.Expression())
		updated := tx.Factory().UpdateCallExpression(node, expression, node.QuestionDotToken(), 0, tx.Visitor().VisitNodes(node.ArgumentList()), node.Flags())
		if !ast.IsIdentifier(expression) && !transformers.IsHelperName(tx.EmitContext(), node.Expression()) {
			tx.EmitContext().AddEmitFlags(updated, printer.EFIndirectCall)
		}
		return updated
	}
	return tx.Visitor().VisitEachChild(node)
}
func (tx *CommonJSModuleTransformer) shouldTransformImportCall() bool {
	return ast.ShouldTransformImportCall(tx.currentSourceFile.FileName(), tx.compilerOptions, tx.getEmitModuleFormatOfFile(tx.currentSourceFile))
}
func (tx *CommonJSModuleTransformer) visitImportCallExpression(node ast.Handle, rewriteOrShim bool) ast.Handle {
	if tx.moduleKind == core.ModuleKindNone && tx.languageVersion >= core.ScriptTargetES2020 {
		return tx.Visitor().VisitEachChild(node)
	}
	externalModuleName := getExternalModuleNameLiteral(tx.Factory(), node, tx.currentSourceFile, nil, nil, tx.compilerOptions)
	firstArgument := tx.Visitor().VisitNode(core.FirstOrNil(node.Arguments()))
	var argument ast.Handle
	if !externalModuleName.IsNil() && (firstArgument.IsNil() || !ast.IsStringLiteral(firstArgument) || firstArgument.Text() != externalModuleName.Text()) {
		argument = externalModuleName
	} else if !firstArgument.IsNil() && rewriteOrShim {
		if ast.IsStringLiteral(firstArgument) {
			argument = rewriteModuleSpecifier(tx.EmitContext(), firstArgument, tx.compilerOptions)
		} else {
			argument = tx.Factory().NewRewriteRelativeImportExtensionsHelper(firstArgument, tx.compilerOptions.Jsx == core.JsxEmitPreserve)
		}
	} else {
		argument = firstArgument
	}
	return tx.createImportCallExpressionCommonJS(argument)
}
func (tx *CommonJSModuleTransformer) createImportCallExpressionCommonJS(arg ast.Handle) ast.Handle {
	needSyncEval := !arg.IsNil() && !isSimpleInlineableExpression(arg)
	var promiseResolveArguments []ast.Handle
	if needSyncEval {
		promiseResolveArguments = []ast.Handle{tx.Factory().NewTemplateExpression(tx.Factory().NewTemplateHead("", "", ast.TokenFlagsNone), tx.Factory().NewList([]ast.Handle{tx.Factory().NewTemplateSpan(arg, tx.Factory().NewTemplateTail("", "", ast.TokenFlagsNone))}))}
	}
	promiseResolveCall := tx.Factory().NewCallExpression(tx.Factory().NewPropertyAccessExpression(tx.Factory().NewIdentifier("Promise"), ast.Handle{}, tx.Factory().NewIdentifier("resolve"), ast.NodeFlagsNone), ast.Handle{}, 0, tx.Factory().NewList(promiseResolveArguments), ast.NodeFlagsNone)
	var requireArguments []ast.Handle
	if needSyncEval {
		requireArguments = []ast.Handle{tx.Factory().NewIdentifier("s")}
	} else if !arg.IsNil() {
		requireArguments = []ast.Handle{arg}
	}
	requireCall := tx.Factory().NewImportStarHelper(tx.Factory().NewCallExpression(tx.Factory().NewIdentifier("require"), ast.Handle{}, 0, tx.Factory().NewList(requireArguments), ast.NodeFlagsNone))
	var parameters []ast.Handle
	if needSyncEval {
		parameters = []ast.Handle{tx.Factory().NewParameterDeclaration(0, ast.Handle{}, tx.Factory().NewIdentifier("s"), ast.Handle{}, ast.Handle{}, ast.Handle{})}
	}
	function := tx.Factory().NewArrowFunction(0, 0, tx.Factory().NewList(parameters), ast.Handle{}, ast.Handle{}, tx.Factory().NewToken(ast.KindEqualsGreaterThanToken), requireCall)
	downleveledImport := tx.Factory().NewCallExpression(tx.Factory().NewPropertyAccessExpression(promiseResolveCall, ast.Handle{}, tx.Factory().NewIdentifier("then"), ast.NodeFlagsNone), ast.Handle{}, 0, tx.Factory().NewList([]ast.Handle{function}), ast.NodeFlagsNone)
	return downleveledImport
}
func (tx *CommonJSModuleTransformer) shimOrRewriteImportOrRequireCall(node ast.Handle) ast.Handle {
	expression := tx.Visitor().VisitNode(node.Expression())
	argumentsList := node.ArgumentList()
	if len(node.Arguments()) > 0 {
		firstArgument := tx.Visitor().VisitNode(node.Arguments()[0])
		firstArgumentChanged := false
		if ast.IsStringLiteralLike(firstArgument) {
			rewritten := rewriteModuleSpecifier(tx.EmitContext(), firstArgument, tx.compilerOptions)
			firstArgumentChanged = rewritten != firstArgument
			firstArgument = rewritten
		} else {
			firstArgument = tx.Factory().NewRewriteRelativeImportExtensionsHelper(firstArgument, tx.compilerOptions.Jsx == core.JsxEmitPreserve)
			firstArgumentChanged = true
		}
		rest := tx.Visitor().VisitSlice(node.Arguments()[1:])
		if firstArgumentChanged || len(rest) != len(node.Arguments()[1:]) {
			arguments := append([]ast.Handle{firstArgument}, rest...)
			argumentsList = tx.Factory().List(node.Store().ListLoc(node.ArgumentList()), arguments...)
		}
	}
	return tx.Factory().UpdateCallExpression(node, expression, node.QuestionDotToken(), 0, argumentsList, node.Flags())
}

func (tx *CommonJSModuleTransformer) visitTaggedTemplateExpression(node ast.Handle) ast.Handle {
	if ast.IsIdentifier(node.Tag()) {
		expression := tx.visitExpressionIdentifier(node.Tag())
		updated := tx.Factory().UpdateTaggedTemplateExpression(node, expression, ast.Handle{}, 0, tx.Visitor().VisitNode(node.Template()), node.Flags())
		if !ast.IsIdentifier(expression) && !transformers.IsHelperName(tx.EmitContext(), node.Tag()) {
			tx.EmitContext().AddEmitFlags(updated, printer.EFIndirectCall)
		}
		return updated
	}
	return tx.Visitor().VisitEachChild(node)
}

func (tx *CommonJSModuleTransformer) visitShorthandPropertyAssignment(node ast.Handle) ast.Handle {
	name := node.Name()
	exportedOrImportedName := tx.visitExpressionIdentifier(name)
	if exportedOrImportedName != name {
		expression := exportedOrImportedName
		if !node.ObjectAssignmentInitializer().IsNil() {
			expression = tx.Factory().NewAssignmentExpression(expression, tx.Visitor().VisitNode(node.ObjectAssignmentInitializer()))
		}
		assignment := tx.Factory().NewPropertyAssignment(0, name, ast.Handle{}, ast.Handle{}, expression)
		assignment.SetLoc(node.Loc())
		tx.EmitContext().AssignCommentAndSourceMapRanges(assignment, node)
		return assignment
	}
	return tx.Factory().UpdateShorthandPropertyAssignment(node, 0, exportedOrImportedName, ast.Handle{}, ast.Handle{}, node.EqualsToken(), tx.Visitor().VisitNode(node.ObjectAssignmentInitializer()))
}

func (tx *CommonJSModuleTransformer) visitIdentifier(node ast.Handle) ast.Handle {
	if transformers.IsIdentifierReference(node, tx.parentNode) {
		return tx.visitExpressionIdentifier(node)
	}
	return node
}

func (tx *CommonJSModuleTransformer) visitExpressionIdentifier(node ast.Handle) ast.Handle {
	if info := tx.EmitContext().GetAutoGenerateInfo(node); !(info != nil && !info.Flags.HasAllowNameSubstitution()) && !transformers.IsHelperName(tx.EmitContext(), node) && !transformers.IsLocalName(tx.EmitContext(), node) && !isDeclarationNameOfEnumOrNamespace(tx.EmitContext(), node) {
		exportContainer := tx.resolver.GetReferencedExportContainer(tx.EmitContext().MostOriginal(node), transformers.IsExportName(tx.EmitContext(), node))
		if !exportContainer.IsNil() && ast.IsSourceFile(exportContainer) {
			reference := tx.Factory().NewPropertyAccessExpression(tx.Factory().NewIdentifier("exports"), ast.Handle{}, tx.Factory().DeepCloneNode(node), ast.NodeFlagsNone)
			tx.EmitContext().AssignCommentAndSourceMapRanges(reference, node)
			reference.SetLoc(node.Loc())
			return reference
		}
		importDeclaration := tx.resolver.GetReferencedImportDeclaration(tx.EmitContext().MostOriginal(node))
		if !importDeclaration.IsNil() {
			if ast.IsImportClause(importDeclaration) {
				reference := tx.Factory().NewPropertyAccessExpression(tx.Factory().NewGeneratedNameForNode(importDeclaration.Parent()), ast.Handle{}, tx.Factory().NewIdentifier("default"), ast.NodeFlagsNone)
				tx.EmitContext().AssignCommentAndSourceMapRanges(reference, node)
				reference.SetLoc(node.Loc())
				return reference
			}
			if ast.IsImportSpecifier(importDeclaration) {
				name := importDeclaration.PropertyNameOrName()
				decl := ast.FindAncestor(importDeclaration, ast.IsImportDeclaration)
				targetNode := importDeclaration
				if !decl.IsNil() {
					targetNode = decl
				}
				target := tx.Factory().NewGeneratedNameForNode(targetNode)
				var reference ast.Handle
				if ast.IsStringLiteral(name) {
					reference = tx.Factory().NewElementAccessExpression(target, ast.Handle{}, tx.Factory().NewStringLiteralFromNode(name), ast.NodeFlagsNone)
				} else {
					referenceName := tx.Factory().DeepCloneNode(name)
					tx.EmitContext().AddEmitFlags(referenceName, printer.EFNoSourceMap|printer.EFNoComments)
					reference = tx.Factory().NewPropertyAccessExpression(target, ast.Handle{}, referenceName, ast.NodeFlagsNone)
				}
				tx.EmitContext().AssignCommentAndSourceMapRanges(reference, node)
				reference.SetLoc(node.Loc())
				return reference
			}
		}
	}
	return node
}

func (tx *CommonJSModuleTransformer) getExports(name ast.Handle) []ast.Handle {
	if !transformers.IsGeneratedIdentifier(tx.EmitContext(), name) {
		importDeclaration := tx.resolver.GetReferencedImportDeclaration(tx.EmitContext().MostOriginal(name))
		if !importDeclaration.IsNil() {
			return tx.currentModuleInfo.exportedBindings.Get(importDeclaration)
		}
		var bindingsSet collections.Set[ast.Handle]
		var bindings []ast.Handle
		declarations := tx.resolver.GetReferencedValueDeclarations(tx.EmitContext().MostOriginal(name))
		if declarations != nil {
			for _, declaration := range declarations {
				exportedBindings := tx.currentModuleInfo.exportedBindings.Get(declaration)
				for _, binding := range exportedBindings {
					if !bindingsSet.Has(binding) {
						bindingsSet.Add(binding)
						bindings = append(bindings, binding)
					}
				}
			}
			return bindings
		}
	} else if isFileLevelReservedGeneratedIdentifier(tx.EmitContext(), name) {
		exportSpecifiers := tx.currentModuleInfo.exportSpecifiers.Get(name.Text())
		if exportSpecifiers != nil {
			var exportedNames []ast.Handle
			for _, exportSpecifier := range exportSpecifiers {
				exportedNames = append(exportedNames, exportSpecifier.Name())
			}
			return exportedNames
		}
	}
	return nil
}
