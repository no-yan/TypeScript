package tstransforms

import (
	"github.com/microsoft/TypeScript/tsc/internal/ast"
	"github.com/microsoft/TypeScript/tsc/internal/binder"
	"github.com/microsoft/TypeScript/tsc/internal/core"
	"github.com/microsoft/TypeScript/tsc/internal/jsnum"
	"github.com/microsoft/TypeScript/tsc/internal/printer"
	"github.com/microsoft/TypeScript/tsc/internal/transformers"
	"slices"
)

type RuntimeSyntaxTransformer struct {
	transformers.Transformer
	compilerOptions *core.CompilerOptions
	parentNode      ast.Handle
	currentNode                         ast.Handle
	currentSourceFile                   ast.Handle
	currentScope                        ast.Handle
	currentScopeFirstDeclarationsOfName map[string]ast.Handle
	currentEnum                         ast.Handle
	currentNamespace                    ast.Handle
	resolver                            binder.ReferenceResolver
	emitResolver                        printer.EmitResolver
}

func NewRuntimeSyntaxTransformer(opt *transformers.TransformOptions) *transformers.Transformer {
	compilerOptions := opt.CompilerOptions
	emitContext := opt.Context
	tx := &RuntimeSyntaxTransformer{compilerOptions: compilerOptions, resolver: opt.Resolver, emitResolver: opt.EmitResolver}
	return tx.NewTransformer(tx.visit, emitContext)
}

func (tx *RuntimeSyntaxTransformer) pushNode(node ast.Handle) (grandparentNode ast.Handle) {
	grandparentNode = tx.parentNode
	tx.parentNode = tx.currentNode
	tx.currentNode = node
	return grandparentNode
}

func (tx *RuntimeSyntaxTransformer) popNode(grandparentNode ast.Handle) {
	tx.currentNode = tx.parentNode
	tx.parentNode = grandparentNode
}
func (tx *RuntimeSyntaxTransformer) pushScope(node ast.Handle) (savedCurrentScope ast.Handle, savedCurrentScopeFirstDeclarationsOfName map[string]ast.Handle) {
	savedCurrentScope = tx.currentScope
	savedCurrentScopeFirstDeclarationsOfName = tx.currentScopeFirstDeclarationsOfName
	switch node.Kind {
	case ast.KindSourceFile:
		tx.currentScope = node
		tx.currentSourceFile = node
		tx.currentScopeFirstDeclarationsOfName = nil
	case ast.KindCaseBlock, ast.KindModuleBlock, ast.KindBlock:
		tx.currentScope = node
		tx.currentScopeFirstDeclarationsOfName = nil
	case ast.KindFunctionDeclaration, ast.KindClassDeclaration, ast.KindVariableStatement:
		tx.recordDeclarationInScope(node)
	}
	return savedCurrentScope, savedCurrentScopeFirstDeclarationsOfName
}
func (tx *RuntimeSyntaxTransformer) popScope(savedCurrentScope ast.Handle, savedCurrentScopeFirstDeclarationsOfName map[string]ast.Handle) {
	if tx.currentScope != savedCurrentScope {
		tx.currentScopeFirstDeclarationsOfName = savedCurrentScopeFirstDeclarationsOfName
	}
	tx.currentScope = savedCurrentScope
}

func (tx *RuntimeSyntaxTransformer) visit(node ast.Handle) ast.Handle {
	grandparentNode := tx.pushNode(node)
	defer tx.popNode(grandparentNode)
	savedCurrentScope, savedCurrentScopeFirstDeclarationsOfName := tx.pushScope(node)
	defer tx.popScope(savedCurrentScope, savedCurrentScopeFirstDeclarationsOfName)
	if node.SubtreeFacts()&ast.SubtreeContainsTypeScript == 0 && (tx.currentNamespace.IsNil() && tx.currentEnum.IsNil() || node.SubtreeFacts()&ast.SubtreeContainsIdentifier == 0) {
		return node
	}
	switch node.Kind {
	case ast.KindPublicKeyword, ast.KindPrivateKeyword, ast.KindProtectedKeyword, ast.KindReadonlyKeyword, ast.KindOverrideKeyword:
		node = ast.Handle{}
	case ast.KindEnumDeclaration:
		node = tx.visitEnumDeclaration(node)
	case ast.KindModuleDeclaration:
		node = tx.visitModuleDeclaration(node)
	case ast.KindClassDeclaration:
		node = tx.visitClassDeclaration(node)
	case ast.KindClassExpression:
		node = tx.visitClassExpression(node)
	case ast.KindConstructor:
		node = tx.visitConstructorDeclaration(node)
	case ast.KindFunctionDeclaration:
		node = tx.visitFunctionDeclaration(node)
	case ast.KindVariableStatement:
		node = tx.visitVariableStatement(node)
	case ast.KindExportDeclaration, ast.KindImportDeclaration, ast.KindImportClause:
		if !tx.currentNamespace.IsNil() && !tx.currentScope.IsNil() && tx.currentScope.Kind != ast.KindBlock {
			node = ast.Handle{}
		} else {
			node = tx.Visitor().VisitEachChild(node)
		}
	case ast.KindImportEqualsDeclaration:
		if !tx.currentNamespace.IsNil() && !tx.currentScope.IsNil() && tx.currentScope.Kind != ast.KindBlock && node.ImportEqualsDeclarationModuleReference().Kind == ast.KindExternalModuleReference {
			node = ast.Handle{}
		} else if !tx.currentNamespace.IsNil() && !tx.currentScope.IsNil() && tx.currentScope.Kind == ast.KindBlock && node.ImportEqualsDeclarationModuleReference().Kind != ast.KindExternalModuleReference {
			node = ast.Handle{}
		} else {
			node = tx.visitImportEqualsDeclaration(node)
		}
	case ast.KindIdentifier:
		node = tx.visitIdentifier(node)
	case ast.KindShorthandPropertyAssignment:
		node = tx.visitShorthandPropertyAssignment(node)
	default:
		node = tx.Visitor().VisitEachChild(node)
	}
	return node
}

func (tx *RuntimeSyntaxTransformer) recordDeclarationInScope(node ast.Handle) {
	switch node.Kind {
	case ast.KindVariableStatement:
		tx.recordDeclarationInScope(node.VariableStatementDeclarationList())
		return
	case ast.KindVariableDeclarationList:
		for _, decl := range node.Store().ListSlice(node.VariableDeclarationListDeclarations()) {
			tx.recordDeclarationInScope(decl)
		}
		return
	case ast.KindArrayBindingPattern, ast.KindObjectBindingPattern:
		for _, element := range node.Elements() {
			tx.recordDeclarationInScope(element)
		}
		return
	}
	name := node.Name()
	if !name.IsNil() {
		if ast.IsIdentifier(name) {
			if tx.currentScopeFirstDeclarationsOfName == nil {
				tx.currentScopeFirstDeclarationsOfName = make(map[string]ast.Handle)
			}
			text := name.Text()
			if _, found := tx.currentScopeFirstDeclarationsOfName[text]; !found {
				tx.currentScopeFirstDeclarationsOfName[text] = node
			}
		} else if ast.IsBindingPattern(name) {
			tx.recordDeclarationInScope(name)
		}
	}
}

func (tx *RuntimeSyntaxTransformer) isFirstDeclarationInScope(node ast.Handle) bool {
	name := node.Name()
	if !name.IsNil() && ast.IsIdentifier(name) {
		text := name.Text()
		if firstDeclaration, found := tx.currentScopeFirstDeclarationsOfName[text]; found {
			return firstDeclaration == node
		}
	}
	return false
}
func (tx *RuntimeSyntaxTransformer) isExportOfNamespace(node ast.Handle) bool {
	return !tx.currentNamespace.IsNil() && (tx.currentScope.IsNil() || tx.currentScope.Kind != ast.KindBlock) && node.ModifierFlags()&ast.ModifierFlagsExport != 0
}

func (tx *RuntimeSyntaxTransformer) getExpressionForPropertyName(member ast.Handle) ast.Handle {
	name := member.Name()
	switch name.Kind {
	case ast.KindPrivateIdentifier:
		return tx.Factory().NewIdentifier("")
	case ast.KindComputedPropertyName:
		n := name
		return tx.Visitor().VisitNode(n.Expression())
	case ast.KindIdentifier:
		return tx.Factory().NewStringLiteral(name.Text(), ast.TokenFlagsNone)
	case ast.KindStringLiteral:
		return tx.Factory().NewStringLiteral(name.Text(), ast.TokenFlagsNone)
	case ast.KindNumericLiteral:
		return tx.Factory().NewNumericLiteral(name.Text(), ast.TokenFlagsNone)
	default:
		return name
	}
}

func (tx *RuntimeSyntaxTransformer) getEnumQualifiedElement(enum ast.Handle, member ast.Handle) ast.Handle {
	prop := tx.getNamespaceQualifiedElement(tx.getNamespaceContainerName(enum), tx.getExpressionForPropertyName(member))
	tx.EmitContext().AddEmitFlags(prop, printer.EFNoComments|printer.EFNoNestedComments|printer.EFNoSourceMap|printer.EFNoNestedSourceMaps)
	return prop
}

func (tx *RuntimeSyntaxTransformer) getNamespaceContainerName(node ast.Handle) ast.Handle {
	return tx.Factory().NewGeneratedNameForNode(node)
}

func (tx *RuntimeSyntaxTransformer) getNamespaceQualifiedProperty(ns ast.Handle, name ast.Handle) ast.Handle {
	return tx.Factory().GetNamespaceMemberName(ns, name, printer.NameOptions{AllowSourceMaps: true})
}

func (tx *RuntimeSyntaxTransformer) getNamespaceQualifiedElement(ns ast.Handle, expression ast.Handle) ast.Handle {
	qualifiedName := tx.EmitContext().Factory.NewElementAccessExpression(ns, ast.Handle{}, expression, ast.NodeFlagsNone)
	tx.EmitContext().AssignCommentAndSourceMapRanges(qualifiedName, expression)
	return qualifiedName
}

func (tx *RuntimeSyntaxTransformer) getExportQualifiedReferenceToDeclaration(node ast.Handle) ast.Handle {
	if tx.isExportOfNamespace(node) {
		return tx.Factory().GetExternalModuleOrNamespaceExportName(tx.getNamespaceContainerName(tx.currentNamespace), node, false, true)
	}
	return tx.Factory().GetDeclarationNameEx(node, printer.NameOptions{AllowSourceMaps: true})
}
func (tx *RuntimeSyntaxTransformer) addVarForDeclaration(statements []ast.Handle, node ast.Handle) ([]ast.Handle, bool) {
	tx.recordDeclarationInScope(node)
	if !tx.isFirstDeclarationInScope(node) {
		return statements, false
	}
	name := tx.Factory().GetLocalNameEx(node, printer.AssignedNameOptions{AllowSourceMaps: true})
	varDecl := tx.Factory().NewVariableDeclaration(name, ast.Handle{}, ast.Handle{}, ast.Handle{})
	varFlags := core.IfElse(tx.currentScope == tx.currentSourceFile, ast.NodeFlagsNone, ast.NodeFlagsLet)
	varDecls := tx.Factory().NewVariableDeclarationList(tx.Factory().NewList([]ast.Handle{varDecl}), varFlags)
	modifierMask := ^(ast.ModifierFlagsTypeScriptModifier | ast.ModifierFlagsDecorator)
	if !tx.currentNamespace.IsNil() {
		modifierMask &^= ast.ModifierFlagsExport
	}
	modifiers := transformers.ExtractModifiers(tx.EmitContext(), node.Modifiers(), modifierMask)
	varStatement := tx.Factory().NewVariableStatement(modifiers, varDecls)
	tx.EmitContext().SetOriginal(varDecl, node)
	tx.EmitContext().SetOriginal(varStatement, node)
	if ast.IsEnumDeclaration(node) {
		tx.EmitContext().SetSourceMapRange(varDecls, node.Loc())
	} else {
		tx.EmitContext().SetSourceMapRange(varStatement, node.Loc())
	}
	tx.EmitContext().SetCommentRange(varStatement, node.Loc())
	tx.EmitContext().AddEmitFlags(varStatement, printer.EFNoTrailingComments)
	statements = append(statements, varStatement)
	return statements, true
}
func (tx *RuntimeSyntaxTransformer) visitEnumDeclaration(node ast.Handle) ast.Handle {
	if !tx.shouldEmitEnumDeclaration(node) {
		return tx.EmitContext().NewNotEmittedStatement(node)
	}
	statements := []ast.Handle{}
	statements, varAdded := tx.addVarForDeclaration(statements, node)
	emitFlags := printer.EFNone
	if varAdded && (tx.compilerOptions.GetEmitModuleKind() != core.ModuleKindSystem || tx.currentScope != tx.currentSourceFile) {
		emitFlags |= printer.EFNoLeadingComments
	}
	enumArg := tx.Factory().NewLogicalORExpression(tx.getExportQualifiedReferenceToDeclaration(node), tx.Factory().NewAssignmentExpression(tx.getExportQualifiedReferenceToDeclaration(node), tx.Factory().NewObjectLiteralExpression(tx.Factory().NewList([]ast.Handle{}), false)))
	if tx.isExportOfNamespace(node) {
		localName := tx.Factory().GetLocalNameEx(node, printer.AssignedNameOptions{AllowSourceMaps: true})
		enumArg = tx.Factory().NewAssignmentExpression(localName, enumArg)
	}
	enumParamName := tx.Factory().NewGeneratedNameForNode(node)
	tx.EmitContext().SetSourceMapRange(enumParamName, node.Name().Loc())
	enumParam := tx.Factory().NewParameterDeclaration(0, ast.Handle{}, enumParamName, ast.Handle{}, ast.Handle{}, ast.Handle{})
	enumBody := tx.transformEnumBody(node)
	enumFunc := tx.Factory().NewFunctionExpression(0, ast.Handle{}, ast.Handle{}, 0, tx.Factory().NewList([]ast.Handle{enumParam}), ast.Handle{}, ast.Handle{}, enumBody)
	enumCall := tx.Factory().NewCallExpression(tx.Factory().NewParenthesizedExpression(enumFunc), ast.Handle{}, 0, tx.Factory().NewList([]ast.Handle{enumArg}), ast.NodeFlagsNone)
	enumStatement := tx.Factory().NewExpressionStatement(enumCall)
	tx.EmitContext().SetOriginal(enumStatement, node)
	tx.EmitContext().AssignCommentAndSourceMapRanges(enumStatement, node)
	tx.EmitContext().AddEmitFlags(enumStatement, emitFlags)
	return tx.Factory().NewSyntaxList(tx.Factory().NewList(append(statements, enumStatement)))
}

func (tx *RuntimeSyntaxTransformer) transformEnumBody(node ast.Handle) ast.Handle {
	savedCurrentEnum := tx.currentEnum
	tx.currentEnum = node
	node = tx.Visitor().VisitEachChild(node)
	statements := []ast.Handle{}
	for i := range len(node.Members()) {
		statements = tx.transformEnumMember(statements, node, i)
	}
	statementList := tx.Factory().List(node.Store().ListLoc(node.MemberList()), statements...)
	tx.currentEnum = savedCurrentEnum
	return tx.Factory().NewBlock(statementList, true)
}

func (tx *RuntimeSyntaxTransformer) transformEnumMember(statements []ast.Handle, enum ast.Handle, index int) []ast.Handle {
	memberNode := enum.Members()[index]
	member := memberNode
	savedParent := tx.parentNode
	tx.parentNode = tx.currentNode
	tx.currentNode = memberNode
	expression := member.Initializer()
	var useExplicitReverseMapping bool
	parseNode := tx.EmitContext().ParseNode(memberNode)
	result := tx.emitResolver.GetEnumMemberValue(parseNode)
	switch value := result.Value.(type) {
	case jsnum.Number:
		if c := constantExpression(value, tx.Factory()); !c.IsNil() {
			expression = c
		}
		useExplicitReverseMapping = true
	case string:
		if c := constantExpression(value, tx.Factory()); !c.IsNil() {
			expression = c
		}
	default:
		if expression.IsNil() {
			expression = tx.Factory().NewVoidZeroExpression()
		}
		useExplicitReverseMapping = !result.IsSyntacticallyString
	}
	expression = tx.Factory().NewAssignmentExpression(tx.getEnumQualifiedElement(enum, member), expression)
	if useExplicitReverseMapping {
		expression = tx.Factory().NewAssignmentExpression(tx.Factory().NewElementAccessExpression(tx.getNamespaceContainerName(enum), ast.Handle{}, expression, ast.NodeFlagsNone), tx.getExpressionForPropertyName(member))
	}
	memberStatement := tx.Factory().NewExpressionStatement(expression)
	tx.EmitContext().AssignCommentAndSourceMapRanges(expression, member)
	tx.EmitContext().AssignCommentAndSourceMapRanges(memberStatement, member)
	statements = append(statements, memberStatement)
	tx.currentNode = tx.parentNode
	tx.parentNode = savedParent
	return statements
}
func (tx *RuntimeSyntaxTransformer) visitModuleDeclaration(node ast.Handle) ast.Handle {
	if !tx.shouldEmitModuleDeclaration(node) {
		return tx.EmitContext().NewNotEmittedStatement(node)
	}
	statements := []ast.Handle{}
	statements, varAdded := tx.addVarForDeclaration(statements, node)
	emitFlags := printer.EFNone
	if varAdded && (tx.compilerOptions.GetEmitModuleKind() != core.ModuleKindSystem || tx.currentScope != tx.currentSourceFile) {
		emitFlags |= printer.EFNoLeadingComments
	}
	moduleArg := tx.Factory().NewLogicalORExpression(tx.getExportQualifiedReferenceToDeclaration(node), tx.Factory().NewAssignmentExpression(tx.getExportQualifiedReferenceToDeclaration(node), tx.Factory().NewObjectLiteralExpression(tx.Factory().NewList([]ast.Handle{}), false)))
	if tx.isExportOfNamespace(node) {
		localName := tx.Factory().GetLocalNameEx(node, printer.AssignedNameOptions{AllowSourceMaps: true})
		moduleArg = tx.Factory().NewAssignmentExpression(localName, moduleArg)
	}
	moduleParamName := tx.Factory().NewGeneratedNameForNode(node)
	tx.EmitContext().SetSourceMapRange(moduleParamName, node.Name().Loc())
	moduleParam := tx.Factory().NewParameterDeclaration(0, ast.Handle{}, moduleParamName, ast.Handle{}, ast.Handle{}, ast.Handle{})
	moduleBody := tx.transformModuleBody(node, tx.getNamespaceContainerName(node))
	moduleFunc := tx.Factory().NewFunctionExpression(0, ast.Handle{}, ast.Handle{}, 0, tx.Factory().NewList([]ast.Handle{moduleParam}), ast.Handle{}, ast.Handle{}, moduleBody)
	moduleCall := tx.Factory().NewCallExpression(tx.Factory().NewParenthesizedExpression(moduleFunc), ast.Handle{}, 0, tx.Factory().NewList([]ast.Handle{moduleArg}), ast.NodeFlagsNone)
	moduleStatement := tx.Factory().NewExpressionStatement(moduleCall)
	tx.EmitContext().SetOriginal(moduleStatement, node)
	tx.EmitContext().AssignCommentAndSourceMapRanges(moduleStatement, node)
	tx.EmitContext().AddEmitFlags(moduleStatement, emitFlags)
	return tx.Factory().NewSyntaxList(tx.Factory().NewList(append(statements, moduleStatement)))
}
func (tx *RuntimeSyntaxTransformer) transformModuleBody(node ast.Handle, namespaceLocalName ast.Handle) ast.Handle {
	savedCurrentNamespace := tx.currentNamespace
	savedCurrentScope := tx.currentScope
	savedCurrentScopeFirstDeclarationsOfName := tx.currentScopeFirstDeclarationsOfName
	tx.currentNamespace = node
	tx.currentScopeFirstDeclarationsOfName = nil
	var statements []ast.Handle
	tx.EmitContext().StartVariableEnvironment()
	var statementsLocation core.TextRange
	var blockLocation core.TextRange
	if !node.Body().IsNil() {
		if node.Body().Kind == ast.KindModuleBlock {
			node = tx.Visitor().VisitEachChild(node)
			body := node.Body()
			statements = body.Statements()
			statementsLocation = body.Store().ListLoc(body.StatementList())
			blockLocation = body.Loc()
		} else {
			statements = tx.Visitor().VisitSlice([]ast.Handle{node.Body()})
			moduleBlock := getInnermostModuleDeclarationFromDottedModule(node).Body()
			statementsLocation = moduleBlock.Store().ListLoc(moduleBlock.StatementList()).WithPos(-1)
		}
	}
	tx.currentNamespace = savedCurrentNamespace
	tx.currentScope = savedCurrentScope
	tx.currentScopeFirstDeclarationsOfName = savedCurrentScopeFirstDeclarationsOfName
	statements = tx.EmitContext().EndAndMergeVariableEnvironment(statements)
	statementList := tx.Factory().List(statementsLocation, statements...)
	block := tx.Factory().NewBlock(statementList, true)
	block.SetLoc(blockLocation)
	if node.Body().IsNil() || node.Body().Kind != ast.KindModuleBlock {
		tx.EmitContext().AddEmitFlags(block, printer.EFNoComments)
	}
	return block
}
func (tx *RuntimeSyntaxTransformer) visitImportEqualsDeclaration(node ast.Handle) ast.Handle {
	if node.ModuleReference().Kind == ast.KindExternalModuleReference {
		return tx.Visitor().VisitEachChild(node)
	}
	moduleReference := tx.Factory().CreateExpressionFromEntityName(node.ModuleReference())
	tx.EmitContext().SetEmitFlags(moduleReference, printer.EFNoComments|printer.EFNoNestedComments)
	if !tx.isExportOfNamespace(node) {
		varDecl := tx.Factory().NewVariableDeclaration(node.Name(), ast.Handle{}, ast.Handle{}, moduleReference)
		tx.EmitContext().SetOriginal(varDecl, node)
		varList := tx.Factory().NewVariableDeclarationList(tx.Factory().NewList([]ast.Handle{varDecl}), ast.NodeFlagsNone)
		varModifiers := transformers.ExtractModifiers(tx.EmitContext(), node.Modifiers(), ast.ModifierFlagsExport)
		varStatement := tx.Factory().NewVariableStatement(varModifiers, varList)
		tx.EmitContext().SetOriginal(varStatement, node)
		tx.EmitContext().AssignCommentAndSourceMapRanges(varStatement, node)
		return varStatement
	} else {
		statement := tx.createExportStatement(node.Name(), moduleReference, node.Loc(), node.Loc(), node)
		statement.SetLoc(node.Loc())
		return statement
	}
}
func (tx *RuntimeSyntaxTransformer) visitVariableStatement(node ast.Handle) ast.Handle {
	if tx.isExportOfNamespace(node) {
		expressions := []ast.Handle{}
		for _, declaration := range node.VariableStatementDeclarationList().Declarations() {
			v := declaration
			if v.Initializer().IsNil() {
				continue
			}
			if ast.IsBindingPattern(v.Name()) {
				expression := transformers.FlattenDestructuringAssignment(&tx.Transformer, tx.Visitor().VisitNode(declaration), false, transformers.FlattenLevelAll, tx.createNamespaceExportExpression)
				if !expression.IsNil() {
					expressions = append(expressions, expression)
				}
			} else {
				expression := transformers.ConvertVariableDeclarationToAssignmentExpression(tx.EmitContext(), v)
				if !expression.IsNil() {
					expressions = append(expressions, expression)
				}
			}
		}
		if len(expressions) == 0 {
			return ast.Handle{}
		}
		expression := tx.Factory().InlineExpressions(expressions)
		statement := tx.Factory().NewExpressionStatement(expression)
		tx.EmitContext().SetOriginal(statement, node)
		tx.EmitContext().AssignCommentAndSourceMapRanges(statement, node)
		savedCurrent := tx.currentNode
		tx.currentNode = statement
		statement = tx.Visitor().VisitEachChild(statement)
		tx.currentNode = savedCurrent
		return statement
	}
	return tx.Visitor().VisitEachChild(node)
}

func (tx *RuntimeSyntaxTransformer) createNamespaceExportExpression(exportName ast.Handle, exportValue ast.Handle, location *core.TextRange) ast.Handle {
	memberName := tx.getNamespaceQualifiedProperty(tx.getNamespaceContainerName(tx.currentNamespace), exportName)
	expression := tx.Factory().NewAssignmentExpression(memberName, exportValue)
	if location != nil {
		expression.SetLoc(*location)
	}
	return expression
}
func (tx *RuntimeSyntaxTransformer) visitFunctionDeclaration(node ast.Handle) ast.Handle {
	if tx.isExportOfNamespace(node) {
		updated := tx.Factory().UpdateFunctionDeclaration(node, tx.Visitor().VisitModifiers(transformers.ExtractModifiers(tx.EmitContext(), node.Modifiers(), ^ast.ModifierFlagsExport)), node.AsteriskToken(), tx.Visitor().VisitNode(node.Name()), 0, tx.Visitor().VisitNodes(node.ParameterList()), ast.Handle{}, ast.Handle{}, tx.Visitor().VisitNode(node.Body()))
		export := tx.createExportStatementForDeclaration(node)
		if !export.IsNil() {
			return tx.Factory().NewSyntaxList(tx.Factory().NewList([]ast.Handle{updated, export}))
		}
		return updated
	}
	return tx.Visitor().VisitEachChild(node)
}
func (tx *RuntimeSyntaxTransformer) getParameterProperties(constructor ast.Handle) []ast.Handle {
	var parameterProperties []ast.Handle
	if !constructor.IsNil() {
		for _, parameter := range constructor.Parameters() {
			if ast.IsParameterPropertyDeclaration(parameter, constructor) {
				parameterProperties = append(parameterProperties, parameter)
			}
		}
	}
	return parameterProperties
}
func (tx *RuntimeSyntaxTransformer) visitClassDeclaration(node ast.Handle) ast.Handle {
	exported := tx.isExportOfNamespace(node)
	var modifiers ast.ListRef
	if exported {
		modifiers = tx.Visitor().VisitModifiers(transformers.ExtractModifiers(tx.EmitContext(), node.Modifiers(), ^ast.ModifierFlagsExportDefault))
	} else {
		modifiers = tx.Visitor().VisitModifiers(node.Modifiers())
	}
	name := tx.Visitor().VisitNode(node.Name())
	if name.IsNil() && (exported || ast.ChildIsDecorated(tx.compilerOptions.ExperimentalDecorators.IsTrue(), node, ast.Handle{})) {
		name = tx.Factory().NewGeneratedNameForNode(node)
	}
	heritageClauses := tx.Visitor().VisitNodes(node.HeritageClauses())
	members := tx.Visitor().VisitNodes(node.MemberList())
	parameterProperties := tx.getParameterProperties(core.Find(node.Members(), ast.IsConstructorDeclaration))
	if len(parameterProperties) > 0 {
		var newMembers []ast.Handle
		for _, parameter := range parameterProperties {
			if ast.IsIdentifier(parameter.Name()) {
				parameterProperty := tx.Factory().NewPropertyDeclaration(0, tx.Factory().DeepCloneNode(parameter.Name()), ast.Handle{}, ast.Handle{}, ast.Handle{})
				tx.EmitContext().SetOriginal(parameterProperty, parameter)
				newMembers = append(newMembers, parameterProperty)
			}
		}
		if len(newMembers) > 0 {
			newMembers = append(newMembers, tx.EmitContext().StoreFile().ParseStore().ListSlice(members)...)
			members = tx.Factory().NewList(newMembers)
		}
	}
	updated := tx.Factory().UpdateClassDeclaration(node, modifiers, name, 0, heritageClauses, members)
	if exported {
		export := tx.createExportStatementForDeclaration(node)
		if !export.IsNil() {
			return tx.Factory().NewSyntaxList(tx.Factory().NewList([]ast.Handle{updated, export}))
		}
	}
	return updated
}
func (tx *RuntimeSyntaxTransformer) visitClassExpression(node ast.Handle) ast.Handle {
	modifiers := tx.Visitor().VisitModifiers(transformers.ExtractModifiers(tx.EmitContext(), node.Modifiers(), ^ast.ModifierFlagsExportDefault))
	name := tx.Visitor().VisitNode(node.Name())
	heritageClauses := tx.Visitor().VisitNodes(node.HeritageClauses())
	members := tx.Visitor().VisitNodes(node.MemberList())
	parameterProperties := tx.getParameterProperties(core.Find(node.Members(), ast.IsConstructorDeclaration))
	if len(parameterProperties) > 0 {
		var newMembers []ast.Handle
		for _, parameter := range parameterProperties {
			if ast.IsIdentifier(parameter.Name()) {
				parameterProperty := tx.Factory().NewPropertyDeclaration(0, tx.Factory().DeepCloneNode(parameter.Name()), ast.Handle{}, ast.Handle{}, ast.Handle{})
				tx.EmitContext().SetOriginal(parameterProperty, parameter)
				newMembers = append(newMembers, parameterProperty)
			}
		}
		if len(newMembers) > 0 {
			newMembers = append(newMembers, tx.EmitContext().StoreFile().ParseStore().ListSlice(members)...)
			members = tx.Factory().NewList(newMembers)
		}
	}
	return tx.Factory().UpdateClassExpression(node, modifiers, name, 0, heritageClauses, members)
}
func (tx *RuntimeSyntaxTransformer) visitConstructorDeclaration(node ast.Handle) ast.Handle {
	modifiers := tx.Visitor().VisitModifiers(node.Modifiers())
	parameters := tx.EmitContext().VisitParameters(node.ParameterList(), tx.Visitor())
	body := tx.visitConstructorBody(node.Body(), node)
	return tx.Factory().UpdateConstructorDeclaration(node, modifiers, 0, parameters, ast.Handle{}, ast.Handle{}, body)
}
func (tx *RuntimeSyntaxTransformer) visitConstructorBody(body ast.Handle, constructor ast.Handle) ast.Handle {
	parameterProperties := tx.getParameterProperties(constructor)
	if len(parameterProperties) == 0 {
		return tx.EmitContext().VisitFunctionBody(body, tx.Visitor())
	}
	grandparentOfBody := tx.pushNode(body)
	savedCurrentScope, savedCurrentScopeFirstDeclarationsOfName := tx.pushScope(body)
	tx.EmitContext().StartVariableEnvironment()
	prologue, rest := tx.Factory().SplitStandardPrologue(body.Statements())
	statements := slices.Clone(prologue)
	var parameterPropertyAssignments []ast.Handle
	for _, parameter := range parameterProperties {
		if ast.IsIdentifier(parameter.Name()) {
			propertyName := tx.Factory().DeepCloneNode(parameter.Name())
			propertyName.SetParent(parameter.Name().Parent())
			tx.EmitContext().AddEmitFlags(propertyName, printer.EFNoComments|printer.EFNoSourceMap)
			localName := tx.Factory().DeepCloneNode(parameter.Name())
			localName.SetParent(parameter.Name().Parent())
			tx.EmitContext().AddEmitFlags(localName, printer.EFNoComments)
			parameterProperty := tx.Factory().NewExpressionStatement(tx.Factory().NewAssignmentExpression(tx.Factory().NewPropertyAccessExpression(tx.Factory().NewThisExpression(), ast.Handle{}, propertyName, ast.NodeFlagsNone), localName))
			tx.EmitContext().SetOriginal(parameterProperty, parameter)
			tx.EmitContext().AddEmitFlags(parameterProperty, printer.EFStartOnNewLine)
			parameterPropertyAssignments = append(parameterPropertyAssignments, parameterProperty)
		}
	}
	superPath := transformers.FindSuperStatementIndexPath(rest, 0)
	if len(superPath) > 0 {
		statements = append(statements, tx.transformConstructorBodyWorker(rest, superPath, parameterPropertyAssignments)...)
	} else {
		statements = append(statements, parameterPropertyAssignments...)
		statements = append(statements, core.FirstResult(tx.Visitor().VisitSlice(rest))...)
	}
	statements = tx.EmitContext().EndAndMergeVariableEnvironment(statements)
	statementList := tx.Factory().List(body.Store().ListLoc(body.StatementList()), statements...)
	tx.popScope(savedCurrentScope, savedCurrentScopeFirstDeclarationsOfName)
	tx.popNode(grandparentOfBody)
	updated := tx.Factory().NewBlock(statementList, true)
	tx.EmitContext().SetOriginal(updated, body)
	updated.SetLoc(body.Loc())
	return updated
}
func (tx *RuntimeSyntaxTransformer) transformConstructorBodyWorker(statementsIn []ast.Handle, superPath []int, initializerStatements []ast.Handle) []ast.Handle {
	var statementsOut []ast.Handle
	superStatementIndex := superPath[0]
	superStatement := statementsIn[superStatementIndex]
	statementsOut = append(statementsOut, core.FirstResult(tx.Visitor().VisitSlice(statementsIn[:superStatementIndex]))...)
	if ast.IsTryStatement(superStatement) {
		tryStatement := superStatement
		tryBlock := tryStatement.TryBlock()
		grandparentOfTryStatement := tx.pushNode(tryStatement)
		grandparentOfTryBlock := tx.pushNode(tryBlock)
		savedCurrentScope, savedCurrentScopeFirstDeclarationsOfName := tx.pushScope(tryBlock)
		tryBlockStatements := tx.transformConstructorBodyWorker(tryBlock.Statements(), superPath[1:], initializerStatements)
		tx.popScope(savedCurrentScope, savedCurrentScopeFirstDeclarationsOfName)
		tx.popNode(grandparentOfTryBlock)
		tryBlockStatementList := tx.Factory().List(tryBlock.Store().ListLoc(tryBlock.StatementList()), tryBlockStatements...)
		statementsOut = append(statementsOut, tx.Factory().UpdateTryStatement(tryStatement, tx.Factory().UpdateBlock(tryBlock, tryBlockStatementList, tryBlock.MultiLine()), tx.Visitor().VisitNode(tryStatement.CatchClause()), tx.Visitor().VisitNode(tryStatement.FinallyBlock())))
		tx.popNode(grandparentOfTryStatement)
	} else {
		statementsOut = append(statementsOut, core.FirstResult(tx.Visitor().VisitSlice(statementsIn[superStatementIndex:superStatementIndex+1]))...)
		statementsOut = append(statementsOut, initializerStatements...)
	}
	statementsOut = append(statementsOut, core.FirstResult(tx.Visitor().VisitSlice(statementsIn[superStatementIndex+1:]))...)
	return statementsOut
}
func (tx *RuntimeSyntaxTransformer) visitShorthandPropertyAssignment(node ast.Handle) ast.Handle {
	name := node.Name()
	exportedOrImportedName := tx.visitExpressionIdentifier(name)
	if exportedOrImportedName != name {
		expression := exportedOrImportedName
		if !node.ObjectAssignmentInitializer().IsNil() {
			equalsToken := node.EqualsToken()
			if equalsToken.IsNil() {
				equalsToken = tx.Factory().NewToken(ast.KindEqualsToken)
			}
			expression = tx.Factory().NewBinaryExpression(0, expression, ast.Handle{}, equalsToken, tx.Visitor().VisitNode(node.ObjectAssignmentInitializer()))
		}
		updated := tx.Factory().NewPropertyAssignment(0, node.Name(), ast.Handle{}, ast.Handle{}, expression)
		updated.SetLoc(node.Loc())
		tx.EmitContext().SetOriginal(updated, node)
		tx.EmitContext().AssignCommentAndSourceMapRanges(updated, node)
		return updated
	}
	return tx.Factory().UpdateShorthandPropertyAssignment(node, 0, exportedOrImportedName, ast.Handle{}, ast.Handle{}, node.EqualsToken(), tx.Visitor().VisitNode(node.ObjectAssignmentInitializer()))
}
func (tx *RuntimeSyntaxTransformer) visitIdentifier(node ast.Handle) ast.Handle {
	if transformers.IsIdentifierReference(node, tx.parentNode) {
		return tx.visitExpressionIdentifier(node)
	}
	return node
}
func (tx *RuntimeSyntaxTransformer) visitExpressionIdentifier(node ast.Handle) ast.Handle {
	if (!tx.currentEnum.IsNil() || !tx.currentNamespace.IsNil()) && !transformers.IsGeneratedIdentifier(tx.EmitContext(), node) && !transformers.IsLocalName(tx.EmitContext(), node) {
		location := tx.EmitContext().MostOriginal(node)
		container := tx.resolver.GetReferencedExportContainer(location, false)
		if !container.IsNil() && (ast.IsEnumDeclaration(container) || ast.IsModuleDeclaration(container)) {
			containerName := tx.getNamespaceContainerName(container)
			memberName := tx.Factory().DeepCloneNode(node)
			tx.EmitContext().SetEmitFlags(memberName, printer.EFNoComments|printer.EFNoSourceMap)
			expression := tx.Factory().GetNamespaceMemberName(containerName, memberName, printer.NameOptions{AllowSourceMaps: true})
			tx.EmitContext().AssignCommentAndSourceMapRanges(expression, node)
			return expression
		}
	}
	return node
}
func (tx *RuntimeSyntaxTransformer) createExportStatementForDeclaration(node ast.Handle) ast.Handle {
	exportName := tx.Factory().GetExternalModuleOrNamespaceExportName(tx.getNamespaceContainerName(tx.currentNamespace), node, false, true)
	localName := tx.Factory().GetLocalName(node)
	expression := tx.Factory().NewAssignmentExpression(exportName, localName)
	exportAssignmentSourceMapRange := node.Loc()
	if !node.Name().IsNil() {
		exportAssignmentSourceMapRange = exportAssignmentSourceMapRange.WithPos(node.Name().Pos())
	}
	tx.EmitContext().SetSourceMapRange(expression, exportAssignmentSourceMapRange)
	statement := tx.Factory().NewExpressionStatement(expression)
	exportStatementSourceMapRange := node.Loc().WithPos(-1)
	tx.EmitContext().SetSourceMapRange(statement, exportStatementSourceMapRange)
	return statement
}
func (tx *RuntimeSyntaxTransformer) createExportAssignment(name ast.Handle, expression ast.Handle, exportAssignmentSourceMapRange core.TextRange, original ast.Handle) ast.Handle {
	exportName := tx.getNamespaceQualifiedProperty(tx.getNamespaceContainerName(tx.currentNamespace), name)
	exportAssignment := tx.Factory().NewAssignmentExpression(exportName, expression)
	tx.EmitContext().SetOriginal(exportAssignment, original)
	tx.EmitContext().SetSourceMapRange(exportAssignment, exportAssignmentSourceMapRange)
	return exportAssignment
}
func (tx *RuntimeSyntaxTransformer) createExportStatement(name ast.Handle, expression ast.Handle, exportAssignmentSourceMapRange core.TextRange, exportStatementSourceMapRange core.TextRange, original ast.Handle) ast.Handle {
	exportStatement := tx.Factory().NewExpressionStatement(tx.createExportAssignment(name, expression, exportAssignmentSourceMapRange, original))
	tx.EmitContext().SetOriginal(exportStatement, original)
	tx.EmitContext().SetSourceMapRange(exportStatement, exportStatementSourceMapRange)
	return exportStatement
}
func (tx *RuntimeSyntaxTransformer) shouldEmitEnumDeclaration(node ast.Handle) bool {
	return !ast.IsEnumConst(node) || tx.compilerOptions.ShouldPreserveConstEnums()
}
func (tx *RuntimeSyntaxTransformer) shouldEmitModuleDeclaration(node ast.Handle) bool {
	pn := tx.EmitContext().ParseNode(node)
	if pn.IsNil() {
		return true
	}
	return ast.IsInstantiatedModule(pn, tx.compilerOptions.ShouldPreserveConstEnums())
}
func getInnermostModuleDeclarationFromDottedModule(moduleDeclaration ast.Handle) ast.Handle {
	for !moduleDeclaration.Body().IsNil() && moduleDeclaration.Body().Kind == ast.KindModuleDeclaration {
		moduleDeclaration = moduleDeclaration.Body()
	}
	return moduleDeclaration
}
