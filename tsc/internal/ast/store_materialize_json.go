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

	materialize = func(h Handle) *Node {
		if h.Ref() == 0 {
			return nil
		}
		var node *Node
		switch h.Kind() {
		case KindTrueKeyword, KindFalseKeyword, KindNullKeyword, KindThisKeyword:
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
			KindAmpersandAmpersandToken, KindQuestionQuestionToken:
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
			KindUndefinedKeyword, KindUnknownKeyword, KindVoidKeyword:
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
