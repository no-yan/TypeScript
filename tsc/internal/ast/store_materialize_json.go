package ast

import "fmt"

// MaterializeStats measures the temporary pointer bridge used while legacy
// consumers still require *Node.
type MaterializeStats struct {
	NodeCount int
	TextCount int
}

// MaterializeSourceFile is the single pointer boundary for Store-native parse
// producers. It is intentionally limited to their closed kind set; adding a
// produced kind requires adding its fidelity mapping here.
func MaterializeSourceFile(root Handle, opts SourceFileParseOptions, text string) (*SourceFile, map[*Node]NodeRef, MaterializeStats) {
	if root.Kind() != KindSourceFile {
		panic("ast: materializer root is not a SourceFile")
	}

	factory := NewNodeFactory(NodeFactoryHooks{})
	refs := make(map[*Node]NodeRef, root.Store().Len())
	var materialize func(Handle) *Node

	materializeList := func(list ListRef) *NodeList {
		if list == 0 {
			return nil
		}
		nodes := make([]*Node, root.Store().ListLen(list))
		for i := range nodes {
			nodes[i] = materialize(root.Store().ListAt(list, i))
		}
		result := factory.NewNodeList(nodes)
		result.Loc = root.Store().ListLoc(list)
		return result
	}

	materializeModifiers := func(list ListRef) *ModifierList {
		if list == 0 {
			return nil
		}
		nodes := make([]*Node, root.Store().ListLen(list))
		for i := range nodes {
			nodes[i] = materialize(root.Store().ListAt(list, i))
		}
		result := factory.NewModifierList(nodes)
		result.Loc = root.Store().ListLoc(list)
		return result
	}

	materialize = func(h Handle) *Node {
		if h.Ref() == 0 {
			return nil
		}
		var node *Node
		switch h.Kind() {
		case KindTrueKeyword, KindFalseKeyword, KindNullKeyword, KindThisKeyword, KindSuperKeyword:
			node = factory.NewKeywordExpression(h.Kind())
		case KindEndOfFile, KindQuestionToken, KindColonToken, KindCommaToken,
			KindQuestionDotToken, KindEqualsEqualsToken, KindExclamationEqualsToken,
			KindEqualsEqualsEqualsToken, KindExclamationEqualsEqualsToken,
			KindLessThanToken, KindGreaterThanToken, KindLessThanEqualsToken,
			KindGreaterThanEqualsToken, KindInstanceOfKeyword, KindInKeyword,
			KindLessThanLessThanToken, KindGreaterThanGreaterThanToken,
			KindGreaterThanGreaterThanGreaterThanToken, KindPlusToken, KindMinusToken,
			KindAsteriskToken, KindSlashToken, KindPercentToken, KindAsteriskAsteriskToken,
			KindBarToken, KindCaretToken, KindAmpersandToken, KindBarBarToken,
			KindAmpersandAmpersandToken, KindQuestionQuestionToken,
			KindEqualsToken, KindPlusEqualsToken, KindMinusEqualsToken,
			KindAsteriskEqualsToken, KindAsteriskAsteriskEqualsToken,
			KindSlashEqualsToken, KindPercentEqualsToken, KindAmpersandEqualsToken,
			KindBarEqualsToken, KindCaretEqualsToken, KindLessThanLessThanEqualsToken,
			KindGreaterThanGreaterThanEqualsToken, KindGreaterThanGreaterThanGreaterThanEqualsToken,
			KindBarBarEqualsToken, KindAmpersandAmpersandEqualsToken, KindQuestionQuestionEqualsToken,
			KindEqualsGreaterThanToken, KindExclamationToken, KindDotDotDotToken,
			KindExportKeyword, KindDefaultKeyword, KindDeclareKeyword, KindAsyncKeyword,
			KindPublicKeyword, KindPrivateKeyword, KindProtectedKeyword, KindReadonlyKeyword,
			KindStaticKeyword, KindAbstractKeyword, KindOverrideKeyword, KindAccessorKeyword,
			KindLetKeyword, KindVarKeyword, KindAssertsKeyword:
			node = factory.NewToken(h.Kind())
		case KindIdentifier:
			node = factory.NewIdentifier(h.IdentifierText())
		case KindQualifiedName:
			node = factory.NewQualifiedName(
				materialize(h.QualifiedNameLeft()),
				materialize(h.QualifiedNameRight()),
			)
		case KindComputedPropertyName:
			node = factory.NewComputedPropertyName(materialize(h.ComputedPropertyNameExpression()))
		case KindStringLiteral:
			node = factory.NewStringLiteral(h.StringLiteralText(), h.StringLiteralTokenFlags())
		case KindNumericLiteral:
			node = factory.NewNumericLiteral(h.NumericLiteralText(), h.NumericLiteralTokenFlags())
		case KindBigIntLiteral:
			node = factory.NewBigIntLiteral(h.BigIntLiteralText(), h.BigIntLiteralTokenFlags())
		case KindNoSubstitutionTemplateLiteral:
			node = factory.NewNoSubstitutionTemplateLiteral(
				h.NoSubstitutionTemplateLiteralText(),
				h.NoSubstitutionTemplateLiteralTemplateFlags(),
			)
		case KindRegularExpressionLiteral:
			node = factory.NewRegularExpressionLiteral(
				h.RegularExpressionLiteralText(),
				h.RegularExpressionLiteralTokenFlags(),
			)
		case KindPrefixUnaryExpression:
			node = factory.NewPrefixUnaryExpression(
				h.PrefixUnaryExpressionOperator(),
				materialize(h.PrefixUnaryExpressionOperand()),
			)
		case KindPostfixUnaryExpression:
			node = factory.NewPostfixUnaryExpression(
				materialize(h.PostfixUnaryExpressionOperand()),
				h.PostfixUnaryExpressionOperator(),
			)
		case KindBinaryExpression:
			node = factory.NewBinaryExpression(
				nil,
				materialize(h.BinaryExpressionLeft()),
				materialize(h.BinaryExpressionType()),
				materialize(h.BinaryExpressionOperatorToken()),
				materialize(h.BinaryExpressionRight()),
			)
		case KindConditionalExpression:
			node = factory.NewConditionalExpression(
				materialize(h.ConditionalExpressionCondition()),
				materialize(h.ConditionalExpressionQuestionToken()),
				materialize(h.ConditionalExpressionWhenTrue()),
				materialize(h.ConditionalExpressionColonToken()),
				materialize(h.ConditionalExpressionWhenFalse()),
			)
		case KindParenthesizedExpression:
			node = factory.NewParenthesizedExpression(
				materialize(h.ParenthesizedExpressionExpression()),
			)
		case KindPropertyAccessExpression:
			node = factory.NewPropertyAccessExpression(
				materialize(h.PropertyAccessExpressionExpression()),
				materialize(h.PropertyAccessExpressionQuestionDotToken()),
				materialize(h.PropertyAccessExpressionName()),
				h.Flags(),
			)
		case KindElementAccessExpression:
			node = factory.NewElementAccessExpression(
				materialize(h.ElementAccessExpressionExpression()),
				materialize(h.ElementAccessExpressionQuestionDotToken()),
				materialize(h.ElementAccessExpressionArgumentExpression()),
				h.Flags(),
			)
		case KindCallExpression:
			node = factory.NewCallExpression(
				materialize(h.CallExpressionExpression()),
				materialize(h.CallExpressionQuestionDotToken()),
				materializeList(h.CallExpressionTypeArguments()),
				materializeList(h.CallExpressionArguments()),
				h.Flags(),
			)
		case KindNewExpression:
			node = factory.NewNewExpression(
				materialize(h.NewExpressionExpression()),
				materializeList(h.NewExpressionTypeArguments()),
				materializeList(h.NewExpressionArguments()),
			)
		case KindTemplateExpression:
			node = factory.NewTemplateExpression(
				materialize(h.TemplateExpressionHead()),
				materializeList(h.TemplateExpressionTemplateSpans()),
			)
		case KindTemplateSpan:
			node = factory.NewTemplateSpan(
				materialize(h.TemplateSpanExpression()),
				materialize(h.TemplateSpanLiteral()),
			)
		case KindTaggedTemplateExpression:
			node = factory.NewTaggedTemplateExpression(
				materialize(h.TaggedTemplateExpressionTag()),
				materialize(h.TaggedTemplateExpressionQuestionDotToken()),
				materializeList(h.TaggedTemplateExpressionTypeArguments()),
				materialize(h.TaggedTemplateExpressionTemplate()),
				h.Flags(),
			)
		case KindTemplateHead:
			node = factory.NewTemplateHead(
				h.TemplateHeadText(),
				h.TemplateHeadRawText(),
				h.TemplateHeadTemplateFlags(),
			)
		case KindTemplateMiddle:
			node = factory.NewTemplateMiddle(
				h.TemplateMiddleText(),
				h.TemplateMiddleRawText(),
				h.TemplateMiddleTemplateFlags(),
			)
		case KindTemplateTail:
			node = factory.NewTemplateTail(
				h.TemplateTailText(),
				h.TemplateTailRawText(),
				h.TemplateTailTemplateFlags(),
			)
		case KindArrayLiteralExpression:
			node = factory.NewArrayLiteralExpression(
				materializeList(h.ArrayLiteralExpressionElements()),
				h.ArrayLiteralExpressionMultiLine(),
			)
		case KindOmittedExpression:
			node = factory.NewOmittedExpression()
		case KindSpreadElement:
			node = factory.NewSpreadElement(materialize(h.SpreadElementExpression()))
		case KindObjectLiteralExpression:
			node = factory.NewObjectLiteralExpression(
				materializeList(h.ObjectLiteralExpressionProperties()),
				h.ObjectLiteralExpressionMultiLine(),
			)
		case KindSpreadAssignment:
			node = factory.NewSpreadAssignment(materialize(h.SpreadAssignmentExpression()))
		case KindPropertyAssignment:
			node = factory.NewPropertyAssignment(
				nil,
				materialize(h.PropertyAssignmentName()),
				materialize(h.PropertyAssignmentPostfixToken()),
				materialize(h.PropertyAssignmentType()),
				materialize(h.PropertyAssignmentInitializer()),
			)
		case KindShorthandPropertyAssignment:
			node = factory.NewShorthandPropertyAssignment(
				nil,
				materialize(h.ShorthandPropertyAssignmentName()),
				materialize(h.ShorthandPropertyAssignmentPostfixToken()),
				materialize(h.ShorthandPropertyAssignmentType()),
				materialize(h.ShorthandPropertyAssignmentEqualsToken()),
				materialize(h.ShorthandPropertyAssignmentObjectAssignmentInitializer()),
			)
		case KindAnyKeyword, KindBigIntKeyword, KindBooleanKeyword,
			KindIntrinsicKeyword, KindNeverKeyword, KindNumberKeyword,
			KindObjectKeyword, KindStringKeyword, KindSymbolKeyword,
			KindUndefinedKeyword, KindUnknownKeyword, KindVoidKeyword,
			KindConstKeyword:
			node = factory.NewKeywordTypeNode(h.Kind())
		case KindTypeReference:
			node = factory.NewTypeReferenceNode(
				materialize(h.TypeReferenceNodeTypeName()),
				materializeList(h.TypeReferenceNodeTypeArguments()),
			)
		case KindArrayType:
			node = factory.NewArrayTypeNode(materialize(h.ArrayTypeNodeElementType()))
		case KindExpressionStatement:
			node = factory.NewExpressionStatement(materialize(h.ExpressionStatementExpression()))
		case KindEmptyStatement:
			node = factory.NewEmptyStatement()
		case KindDebuggerStatement:
			node = factory.NewDebuggerStatement()
		case KindBlock:
			node = factory.NewBlock(materializeList(h.BlockStatements()), h.BlockMultiLine())
		case KindVariableStatement:
			node = factory.NewVariableStatement(
				materializeModifiers(h.VariableStatementModifiers()),
				materialize(h.VariableStatementDeclarationList()),
			)
		case KindVariableDeclarationList:
			node = factory.NewVariableDeclarationList(
				materializeList(h.VariableDeclarationListDeclarations()),
				h.Flags(),
			)
		case KindVariableDeclaration:
			node = factory.NewVariableDeclaration(
				materialize(h.VariableDeclarationName()),
				materialize(h.VariableDeclarationExclamationToken()),
				materialize(h.VariableDeclarationType()),
				materialize(h.VariableDeclarationInitializer()),
			)
		case KindObjectBindingPattern, KindArrayBindingPattern:
			node = factory.NewBindingPattern(h.Kind(), materializeList(h.BindingPatternElements()))
		case KindBindingElement:
			node = factory.NewBindingElement(
				materialize(h.BindingElementDotDotDotToken()),
				materialize(h.BindingElementPropertyName()),
				materialize(h.BindingElementName()),
				materialize(h.BindingElementInitializer()),
			)
		case KindParameter:
			node = factory.NewParameterDeclaration(
				materializeModifiers(h.ParameterDeclarationModifiers()),
				materialize(h.ParameterDeclarationDotDotDotToken()),
				materialize(h.ParameterDeclarationName()),
				materialize(h.ParameterDeclarationQuestionToken()),
				materialize(h.ParameterDeclarationType()),
				materialize(h.ParameterDeclarationInitializer()),
			)
		case KindFunctionDeclaration:
			node = factory.NewFunctionDeclaration(
				materializeModifiers(h.FunctionDeclarationModifiers()),
				materialize(h.FunctionDeclarationAsteriskToken()),
				materialize(h.FunctionDeclarationName()),
				materializeList(h.FunctionDeclarationTypeParameters()),
				materializeList(h.FunctionDeclarationParameters()),
				materialize(h.FunctionDeclarationType()),
				materialize(h.FunctionDeclarationFullSignature()),
				materialize(h.FunctionDeclarationBody()),
			)
		case KindIfStatement:
			node = factory.NewIfStatement(
				materialize(h.IfStatementExpression()),
				materialize(h.IfStatementThenStatement()),
				materialize(h.IfStatementElseStatement()),
			)
		case KindDoStatement:
			node = factory.NewDoStatement(
				materialize(h.DoStatementStatement()),
				materialize(h.DoStatementExpression()),
			)
		case KindWhileStatement:
			node = factory.NewWhileStatement(
				materialize(h.WhileStatementExpression()),
				materialize(h.WhileStatementStatement()),
			)
		case KindForStatement:
			node = factory.NewForStatement(
				materialize(h.ForStatementInitializer()),
				materialize(h.ForStatementCondition()),
				materialize(h.ForStatementIncrementor()),
				materialize(h.ForStatementStatement()),
			)
		case KindForInStatement, KindForOfStatement:
			node = factory.NewForInOrOfStatement(
				h.Kind(),
				materialize(h.ForInOrOfStatementAwaitModifier()),
				materialize(h.ForInOrOfStatementInitializer()),
				materialize(h.ForInOrOfStatementExpression()),
				materialize(h.ForInOrOfStatementStatement()),
			)
		case KindBreakStatement:
			node = factory.NewBreakStatement(materialize(h.BreakStatementLabel()))
		case KindContinueStatement:
			node = factory.NewContinueStatement(materialize(h.ContinueStatementLabel()))
		case KindReturnStatement:
			node = factory.NewReturnStatement(materialize(h.ReturnStatementExpression()))
		case KindThrowStatement:
			node = factory.NewThrowStatement(materialize(h.ThrowStatementExpression()))
		case KindTryStatement:
			node = factory.NewTryStatement(
				materialize(h.TryStatementTryBlock()),
				materialize(h.TryStatementCatchClause()),
				materialize(h.TryStatementFinallyBlock()),
			)
		case KindCatchClause:
			node = factory.NewCatchClause(
				materialize(h.CatchClauseVariableDeclaration()),
				materialize(h.CatchClauseBlock()),
			)
		case KindSwitchStatement:
			node = factory.NewSwitchStatement(
				materialize(h.SwitchStatementExpression()),
				materialize(h.SwitchStatementCaseBlock()),
			)
		case KindCaseBlock:
			node = factory.NewCaseBlock(materializeList(h.CaseBlockClauses()))
		case KindCaseClause, KindDefaultClause:
			node = factory.NewCaseOrDefaultClause(
				h.Kind(),
				materialize(h.CaseOrDefaultClauseExpression()),
				materializeList(h.CaseOrDefaultClauseStatements()),
			)
		case KindLabeledStatement:
			node = factory.NewLabeledStatement(
				materialize(h.LabeledStatementLabel()),
				materialize(h.LabeledStatementStatement()),
			)
		case KindTypeAliasDeclaration:
			node = factory.NewTypeAliasDeclaration(
				materializeModifiers(h.TypeAliasDeclarationModifiers()),
				materialize(h.TypeAliasDeclarationName()),
				materializeList(h.TypeAliasDeclarationTypeParameters()),
				materialize(h.TypeAliasDeclarationType()),
			)
		case KindInterfaceDeclaration:
			node = factory.NewInterfaceDeclaration(
				materializeModifiers(h.InterfaceDeclarationModifiers()),
				materialize(h.InterfaceDeclarationName()),
				materializeList(h.InterfaceDeclarationTypeParameters()),
				materializeList(h.InterfaceDeclarationHeritageClauses()),
				materializeList(h.InterfaceDeclarationMembers()),
			)
		case KindClassDeclaration:
			node = factory.NewClassDeclaration(
				materializeModifiers(h.ClassDeclarationModifiers()),
				materialize(h.ClassDeclarationName()),
				materializeList(h.ClassDeclarationTypeParameters()),
				materializeList(h.ClassDeclarationHeritageClauses()),
				materializeList(h.ClassDeclarationMembers()),
			)
		case KindHeritageClause:
			node = factory.NewHeritageClause(
				h.HeritageClauseToken(),
				materializeList(h.HeritageClauseTypes()),
			)
		case KindExpressionWithTypeArguments:
			node = factory.NewExpressionWithTypeArguments(
				materialize(h.ExpressionWithTypeArgumentsExpression()),
				materializeList(h.ExpressionWithTypeArgumentsTypeArguments()),
			)
		case KindPropertySignature:
			node = factory.NewPropertySignatureDeclaration(
				materializeModifiers(h.PropertySignatureDeclarationModifiers()),
				materialize(h.PropertySignatureDeclarationName()),
				materialize(h.PropertySignatureDeclarationPostfixToken()),
				materialize(h.PropertySignatureDeclarationType()),
				materialize(h.PropertySignatureDeclarationInitializer()),
			)
		case KindMethodSignature:
			node = factory.NewMethodSignatureDeclaration(
				materializeModifiers(h.MethodSignatureDeclarationModifiers()),
				materialize(h.MethodSignatureDeclarationName()),
				materialize(h.MethodSignatureDeclarationPostfixToken()),
				materializeList(h.MethodSignatureDeclarationTypeParameters()),
				materializeList(h.MethodSignatureDeclarationParameters()),
				materialize(h.MethodSignatureDeclarationType()),
			)
		case KindPropertyDeclaration:
			node = factory.NewPropertyDeclaration(
				materializeModifiers(h.PropertyDeclarationModifiers()),
				materialize(h.PropertyDeclarationName()),
				materialize(h.PropertyDeclarationPostfixToken()),
				materialize(h.PropertyDeclarationType()),
				materialize(h.PropertyDeclarationInitializer()),
			)
		case KindMethodDeclaration:
			node = factory.NewMethodDeclaration(
				materializeModifiers(h.MethodDeclarationModifiers()),
				materialize(h.MethodDeclarationAsteriskToken()),
				materialize(h.MethodDeclarationName()),
				materialize(h.MethodDeclarationPostfixToken()),
				materializeList(h.MethodDeclarationTypeParameters()),
				materializeList(h.MethodDeclarationParameters()),
				materialize(h.MethodDeclarationType()),
				materialize(h.MethodDeclarationFullSignature()),
				materialize(h.MethodDeclarationBody()),
			)
		case KindConstructor:
			node = factory.NewConstructorDeclaration(
				materializeModifiers(h.ConstructorDeclarationModifiers()),
				materializeList(h.ConstructorDeclarationTypeParameters()),
				materializeList(h.ConstructorDeclarationParameters()),
				materialize(h.ConstructorDeclarationType()),
				materialize(h.ConstructorDeclarationFullSignature()),
				materialize(h.ConstructorDeclarationBody()),
			)
		case KindSemicolonClassElement:
			node = factory.NewSemicolonClassElement()
		case KindEnumDeclaration:
			node = factory.NewEnumDeclaration(
				materializeModifiers(h.EnumDeclarationModifiers()),
				materialize(h.EnumDeclarationName()),
				materializeList(h.EnumDeclarationMembers()),
			)
		case KindEnumMember:
			node = factory.NewEnumMember(
				materialize(h.EnumMemberName()),
				materialize(h.EnumMemberInitializer()),
			)
		case KindModuleDeclaration:
			node = factory.NewModuleDeclaration(
				materializeModifiers(h.ModuleDeclarationModifiers()),
				h.ModuleDeclarationKeyword(),
				materialize(h.ModuleDeclarationName()),
				materialize(h.ModuleDeclarationBody()),
			)
		case KindModuleBlock:
			node = factory.NewModuleBlock(materializeList(h.ModuleBlockStatements()))
		case KindImportDeclaration:
			node = factory.NewImportDeclaration(
				materializeModifiers(h.ImportDeclarationModifiers()),
				materialize(h.ImportDeclarationImportClause()),
				materialize(h.ImportDeclarationModuleSpecifier()),
				materialize(h.ImportDeclarationAttributes()),
			)
		case KindImportClause:
			node = factory.NewImportClause(
				h.ImportClausePhaseModifier(),
				materialize(h.ImportClauseName()),
				materialize(h.ImportClauseNamedBindings()),
			)
		case KindNamedImports:
			node = factory.NewNamedImports(materializeList(h.NamedImportsElements()))
		case KindNamespaceImport:
			node = factory.NewNamespaceImport(materialize(h.NamespaceImportName()))
		case KindImportSpecifier:
			node = factory.NewImportSpecifier(
				h.ImportSpecifierIsTypeOnly(),
				materialize(h.ImportSpecifierPropertyName()),
				materialize(h.ImportSpecifierName()),
			)
		case KindExportDeclaration:
			node = factory.NewExportDeclaration(
				materializeModifiers(h.ExportDeclarationModifiers()),
				h.ExportDeclarationIsTypeOnly(),
				materialize(h.ExportDeclarationExportClause()),
				materialize(h.ExportDeclarationModuleSpecifier()),
				materialize(h.ExportDeclarationAttributes()),
			)
		case KindNamedExports:
			node = factory.NewNamedExports(materializeList(h.NamedExportsElements()))
		case KindNamespaceExport:
			node = factory.NewNamespaceExport(materialize(h.NamespaceExportName()))
		case KindExportSpecifier:
			node = factory.NewExportSpecifier(
				h.ExportSpecifierIsTypeOnly(),
				materialize(h.ExportSpecifierPropertyName()),
				materialize(h.ExportSpecifierName()),
			)
		case KindExportAssignment:
			node = factory.NewExportAssignment(
				materializeModifiers(h.ExportAssignmentModifiers()),
				h.ExportAssignmentIsExportEquals(),
				materialize(h.ExportAssignmentType()),
				materialize(h.ExportAssignmentExpression()),
			)
		case KindTypeParameter:
			node = factory.NewTypeParameterDeclaration(
				materializeModifiers(h.TypeParameterDeclarationModifiers()),
				materialize(h.TypeParameterDeclarationName()),
				materialize(h.TypeParameterDeclarationConstraint()),
				materialize(h.TypeParameterDeclarationExpression()),
				materialize(h.TypeParameterDeclarationDefaultType()),
			)
		case KindUnionType:
			node = factory.NewUnionTypeNode(materializeList(h.UnionTypeNodeTypes()))
		case KindIntersectionType:
			node = factory.NewIntersectionTypeNode(materializeList(h.IntersectionTypeNodeTypes()))
		case KindTypeOperator:
			node = factory.NewTypeOperatorNode(
				h.TypeOperatorNodeOperator(),
				materialize(h.TypeOperatorNodeType()),
			)
		case KindLiteralType:
			node = factory.NewLiteralTypeNode(materialize(h.LiteralTypeNodeLiteral()))
		case KindTypeQuery:
			node = factory.NewTypeQueryNode(
				materialize(h.TypeQueryNodeExprName()),
				materializeList(h.TypeQueryNodeTypeArguments()),
			)
		case KindTypeLiteral:
			node = factory.NewTypeLiteralNode(materializeList(h.TypeLiteralNodeMembers()))
		case KindParenthesizedType:
			node = factory.NewParenthesizedTypeNode(materialize(h.ParenthesizedTypeNodeType()))
		case KindArrowFunction:
			node = factory.NewArrowFunction(
				materializeModifiers(h.ArrowFunctionModifiers()),
				materializeList(h.ArrowFunctionTypeParameters()),
				materializeList(h.ArrowFunctionParameters()),
				materialize(h.ArrowFunctionType()),
				materialize(h.ArrowFunctionFullSignature()),
				materialize(h.ArrowFunctionEqualsGreaterThanToken()),
				materialize(h.ArrowFunctionBody()),
			)
		case KindAsExpression:
			node = factory.NewAsExpression(
				materialize(h.AsExpressionExpression()),
				materialize(h.AsExpressionType()),
			)
		case KindSatisfiesExpression:
			node = factory.NewSatisfiesExpression(
				materialize(h.SatisfiesExpressionExpression()),
				materialize(h.SatisfiesExpressionType()),
			)
		case KindFunctionExpression:
			node = factory.NewFunctionExpression(
				materializeModifiers(h.FunctionExpressionModifiers()),
				materialize(h.FunctionExpressionAsteriskToken()),
				materialize(h.FunctionExpressionName()),
				materializeList(h.FunctionExpressionTypeParameters()),
				materializeList(h.FunctionExpressionParameters()),
				materialize(h.FunctionExpressionType()),
				materialize(h.FunctionExpressionFullSignature()),
				materialize(h.FunctionExpressionBody()),
			)
		case KindClassExpression:
			node = factory.NewClassExpression(
				materializeModifiers(h.ClassExpressionModifiers()),
				materialize(h.ClassExpressionName()),
				materializeList(h.ClassExpressionTypeParameters()),
				materializeList(h.ClassExpressionHeritageClauses()),
				materializeList(h.ClassExpressionMembers()),
			)
		case KindMetaProperty:
			node = factory.NewMetaProperty(
				h.MetaPropertyKeywordToken(),
				materialize(h.MetaPropertyName()),
			)
		case KindNonNullExpression:
			node = factory.NewNonNullExpression(
				materialize(h.NonNullExpressionExpression()),
				h.Flags(),
			)
		case KindTypeOfExpression:
			node = factory.NewTypeOfExpression(materialize(h.TypeOfExpressionExpression()))
		case KindVoidExpression:
			node = factory.NewVoidExpression(materialize(h.VoidExpressionExpression()))
		case KindDeleteExpression:
			node = factory.NewDeleteExpression(materialize(h.DeleteExpressionExpression()))
		case KindAwaitExpression:
			node = factory.NewAwaitExpression(materialize(h.AwaitExpressionExpression()))
		case KindYieldExpression:
			node = factory.NewYieldExpression(
				materialize(h.YieldExpressionAsteriskToken()),
				materialize(h.YieldExpressionExpression()),
			)
		case KindTypeAssertionExpression:
			node = factory.NewTypeAssertion(
				materialize(h.TypeAssertionType()),
				materialize(h.TypeAssertionExpression()),
			)
		case KindFunctionType:
			node = factory.NewFunctionTypeNode(
				materializeList(h.FunctionTypeNodeTypeParameters()),
				materializeList(h.FunctionTypeNodeParameters()),
				materialize(h.FunctionTypeNodeType()),
			)
		case KindConstructorType:
			node = factory.NewConstructorTypeNode(
				materializeModifiers(h.ConstructorTypeNodeModifiers()),
				materializeList(h.ConstructorTypeNodeTypeParameters()),
				materializeList(h.ConstructorTypeNodeParameters()),
				materialize(h.ConstructorTypeNodeType()),
			)
		case KindConditionalType:
			node = factory.NewConditionalTypeNode(
				materialize(h.ConditionalTypeNodeCheckType()),
				materialize(h.ConditionalTypeNodeExtendsType()),
				materialize(h.ConditionalTypeNodeTrueType()),
				materialize(h.ConditionalTypeNodeFalseType()),
			)
		case KindInferType:
			node = factory.NewInferTypeNode(materialize(h.InferTypeNodeTypeParameter()))
		case KindIndexedAccessType:
			node = factory.NewIndexedAccessTypeNode(
				materialize(h.IndexedAccessTypeNodeObjectType()),
				materialize(h.IndexedAccessTypeNodeIndexType()),
			)
		case KindThisType:
			node = factory.NewThisTypeNode()
		case KindTypePredicate:
			node = factory.NewTypePredicateNode(
				materialize(h.TypePredicateNodeAssertsModifier()),
				materialize(h.TypePredicateNodeParameterName()),
				materialize(h.TypePredicateNodeType()),
			)
		case KindMappedType:
			node = factory.NewMappedTypeNode(
				materialize(h.MappedTypeNodeReadonlyToken()),
				materialize(h.MappedTypeNodeTypeParameter()),
				materialize(h.MappedTypeNodeNameType()),
				materialize(h.MappedTypeNodeQuestionToken()),
				materialize(h.MappedTypeNodeType()),
				materializeList(h.MappedTypeNodeMembers()),
			)
		case KindTupleType:
			node = factory.NewTupleTypeNode(materializeList(h.TupleTypeNodeElements()))
		case KindNamedTupleMember:
			node = factory.NewNamedTupleMember(
				materialize(h.NamedTupleMemberDotDotDotToken()),
				materialize(h.NamedTupleMemberName()),
				materialize(h.NamedTupleMemberQuestionToken()),
				materialize(h.NamedTupleMemberType()),
			)
		case KindOptionalType:
			node = factory.NewOptionalTypeNode(materialize(h.OptionalTypeNodeType()))
		case KindRestType:
			node = factory.NewRestTypeNode(materialize(h.RestTypeNodeType()))
		case KindImportType:
			node = factory.NewImportTypeNode(
				h.ImportTypeNodeIsTypeOf(),
				materialize(h.ImportTypeNodeArgument()),
				materialize(h.ImportTypeNodeAttributes()),
				materialize(h.ImportTypeNodeQualifier()),
				materializeList(h.ImportTypeNodeTypeArguments()),
			)
		case KindTemplateLiteralType:
			node = factory.NewTemplateLiteralTypeNode(
				materialize(h.TemplateLiteralTypeNodeHead()),
				materializeList(h.TemplateLiteralTypeNodeTemplateSpans()),
			)
		case KindTemplateLiteralTypeSpan:
			node = factory.NewTemplateLiteralTypeSpan(
				materialize(h.TemplateLiteralTypeSpanType()),
				materialize(h.TemplateLiteralTypeSpanLiteral()),
			)
		case KindCallSignature:
			node = factory.NewCallSignatureDeclaration(
				materializeList(h.CallSignatureDeclarationTypeParameters()),
				materializeList(h.CallSignatureDeclarationParameters()),
				materialize(h.CallSignatureDeclarationType()),
			)
		case KindConstructSignature:
			node = factory.NewConstructSignatureDeclaration(
				materializeList(h.ConstructSignatureDeclarationTypeParameters()),
				materializeList(h.ConstructSignatureDeclarationParameters()),
				materialize(h.ConstructSignatureDeclarationType()),
			)
		case KindIndexSignature:
			node = factory.NewIndexSignatureDeclaration(
				materializeModifiers(h.IndexSignatureDeclarationModifiers()),
				materializeList(h.IndexSignatureDeclarationParameters()),
				materialize(h.IndexSignatureDeclarationType()),
			)
		case KindGetAccessor:
			node = factory.NewGetAccessorDeclaration(
				materializeModifiers(h.GetAccessorDeclarationModifiers()),
				materialize(h.GetAccessorDeclarationName()),
				materializeList(h.GetAccessorDeclarationTypeParameters()),
				materializeList(h.GetAccessorDeclarationParameters()),
				materialize(h.GetAccessorDeclarationType()),
				materialize(h.GetAccessorDeclarationFullSignature()),
				materialize(h.GetAccessorDeclarationBody()),
			)
		case KindSetAccessor:
			node = factory.NewSetAccessorDeclaration(
				materializeModifiers(h.SetAccessorDeclarationModifiers()),
				materialize(h.SetAccessorDeclarationName()),
				materializeList(h.SetAccessorDeclarationTypeParameters()),
				materializeList(h.SetAccessorDeclarationParameters()),
				materialize(h.SetAccessorDeclarationType()),
				materialize(h.SetAccessorDeclarationFullSignature()),
				materialize(h.SetAccessorDeclarationBody()),
			)
		case KindSourceFile:
			node = factory.NewSourceFile(
				opts,
				text,
				materializeList(h.SourceFileStatements()),
				materialize(h.SourceFileEndOfFileToken()),
			)
		default:
			panic(fmt.Sprintf("ast: unsupported parse materialization kind %s", h.Kind()))
		}

		node.Loc = h.Loc()
		node.Flags = h.Flags()
		node.ForEachChild(func(child *Node) bool {
			child.Parent = node
			return false
		})
		refs[node] = h.Ref()
		return node
	}

	file := materialize(root).AsSourceFile()
	return file, refs, MaterializeStats{
		NodeCount: factory.NodeCount(),
		TextCount: factory.TextCount(),
	}
}

// MaterializeJSONSourceFile is retained for callers that name the JSON
// producer; all native parsers share MaterializeSourceFile above.
func MaterializeJSONSourceFile(root Handle, opts SourceFileParseOptions, text string) (*SourceFile, map[*Node]NodeRef, MaterializeStats) {
	return MaterializeSourceFile(root, opts, text)
}
