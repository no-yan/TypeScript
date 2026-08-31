package ast

import "fmt"

// MaterializeStats measures the temporary pointer bridge used while legacy
// consumers still require *Node.
type MaterializeStats struct {
	NodeCount int
	TextCount int
}

// MaterializeJSONSourceFile builds the legacy pointer view of a SourceFile
// parsed natively into Store. It is intentionally limited to the JSON parser's
// closed kind set; adding a kind to that producer requires adding it here.
func MaterializeJSONSourceFile(root Handle, opts SourceFileParseOptions, text string) (*SourceFile, map[*Node]NodeRef, MaterializeStats) {
	if root.Kind() != KindSourceFile {
		panic("ast: JSON materializer root is not a SourceFile")
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
		case KindEndOfFile, KindTrueKeyword, KindFalseKeyword, KindNullKeyword:
			node = factory.NewToken(h.Kind())
		case KindStringLiteral:
			node = factory.NewStringLiteral(h.StringLiteralText(), h.StringLiteralTokenFlags())
		case KindNumericLiteral:
			node = factory.NewNumericLiteral(h.NumericLiteralText(), h.NumericLiteralTokenFlags())
		case KindPrefixUnaryExpression:
			node = factory.NewPrefixUnaryExpression(
				h.PrefixUnaryExpressionOperator(),
				materialize(h.PrefixUnaryExpressionOperand()),
			)
		case KindArrayLiteralExpression:
			node = factory.NewArrayLiteralExpression(
				materializeList(h.ArrayLiteralExpressionElements()),
				h.ArrayLiteralExpressionMultiLine(),
			)
		case KindObjectLiteralExpression:
			node = factory.NewObjectLiteralExpression(
				materializeList(h.ObjectLiteralExpressionProperties()),
				h.ObjectLiteralExpressionMultiLine(),
			)
		case KindPropertyAssignment:
			node = factory.NewPropertyAssignment(
				nil,
				materialize(h.PropertyAssignmentName()),
				materialize(h.PropertyAssignmentPostfixToken()),
				materialize(h.PropertyAssignmentType()),
				materialize(h.PropertyAssignmentInitializer()),
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
			panic(fmt.Sprintf("ast: unsupported JSON materialization kind %s", h.Kind()))
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
