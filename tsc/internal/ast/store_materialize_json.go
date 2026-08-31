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
		case KindPrefixUnaryExpression:
			node = factory.NewPrefixUnaryExpression(
				h.PrefixUnaryExpressionOperator(),
				materialize(h.PrefixUnaryExpressionOperand()),
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
