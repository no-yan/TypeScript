//go:build storeexp

package storeexp

import "github.com/microsoft/TypeScript/tsc/internal/ast"

// This file is hand-written as a sample of what a generator would emit for
// every kind. A typed view is the same two words as Node under another type:
// creating one loads nothing, and each accessor reads only its own slot.
// Accessors have no nil guard (nodes[0] and list block 0 are sentinels) and no
// kind check outside storeChecks.
//
// Slot numbers are the declaration order of the members in ast.json. The tests
// pin them against shapes_generated.go.

const (
	binaryModifiersSlot = iota
	binaryLeftSlot
	binaryTypeSlot
	binaryOperatorTokenSlot
	binaryRightSlot
)

const (
	callExpressionSlot = iota
	callQuestionDotTokenSlot
	callTypeArgumentsSlot
	callArgumentsSlot
)

const (
	propertyAccessExpressionSlot = iota
	propertyAccessQuestionDotTokenSlot
	propertyAccessNameSlot
)

const (
	functionModifiersSlot = iota
	functionAsteriskTokenSlot
	functionNameSlot
	functionTypeParametersSlot
	functionParametersSlot
	functionTypeSlot
	functionFullSignatureSlot
	functionBodySlot
)

const (
	sourceFileStatementsSlot = iota
	sourceFileEndOfFileTokenSlot
)

// A token has no view: it is a header only.

type Identifier struct {
	s *Store
	h *NodeHeader
}

func (n Node) AsIdentifier() Identifier {
	if storeChecks {
		n.assertKind(ast.KindIdentifier)
	}
	return Identifier(n)
}

func (i Identifier) Text() string { return Node(i).Text() }

type BinaryExpression struct {
	s *Store
	h *NodeHeader
}

func (n Node) AsBinaryExpression() BinaryExpression {
	if storeChecks {
		n.assertKind(ast.KindBinaryExpression)
	}
	return BinaryExpression(n)
}

func (b BinaryExpression) Modifiers() List {
	return List{b.s, b.s.extra[b.h.data+binaryModifiersSlot]}
}

func (b BinaryExpression) Left() Node {
	return b.s.node(NodeRef(b.s.extra[b.h.data+binaryLeftSlot]))
}

func (b BinaryExpression) Type() Node {
	return b.s.node(NodeRef(b.s.extra[b.h.data+binaryTypeSlot]))
}

func (b BinaryExpression) OperatorToken() Node {
	return b.s.node(NodeRef(b.s.extra[b.h.data+binaryOperatorTokenSlot]))
}

func (b BinaryExpression) Right() Node {
	return b.s.node(NodeRef(b.s.extra[b.h.data+binaryRightSlot]))
}

type CallExpression struct {
	s *Store
	h *NodeHeader
}

func (n Node) AsCallExpression() CallExpression {
	if storeChecks {
		n.assertKind(ast.KindCallExpression)
	}
	return CallExpression(n)
}

func (c CallExpression) Expression() Node {
	return c.s.node(NodeRef(c.s.extra[c.h.data+callExpressionSlot]))
}

func (c CallExpression) QuestionDotToken() Node {
	return c.s.node(NodeRef(c.s.extra[c.h.data+callQuestionDotTokenSlot]))
}

func (c CallExpression) TypeArguments() List {
	return List{c.s, c.s.extra[c.h.data+callTypeArgumentsSlot]}
}

func (c CallExpression) Arguments() List {
	return List{c.s, c.s.extra[c.h.data+callArgumentsSlot]}
}

type PropertyAccessExpression struct {
	s *Store
	h *NodeHeader
}

func (n Node) AsPropertyAccessExpression() PropertyAccessExpression {
	if storeChecks {
		n.assertKind(ast.KindPropertyAccessExpression)
	}
	return PropertyAccessExpression(n)
}

func (p PropertyAccessExpression) Expression() Node {
	return p.s.node(NodeRef(p.s.extra[p.h.data+propertyAccessExpressionSlot]))
}

func (p PropertyAccessExpression) QuestionDotToken() Node {
	return p.s.node(NodeRef(p.s.extra[p.h.data+propertyAccessQuestionDotTokenSlot]))
}

func (p PropertyAccessExpression) Name() Node {
	return p.s.node(NodeRef(p.s.extra[p.h.data+propertyAccessNameSlot]))
}

type FunctionDeclaration struct {
	s *Store
	h *NodeHeader
}

func (n Node) AsFunctionDeclaration() FunctionDeclaration {
	if storeChecks {
		n.assertKind(ast.KindFunctionDeclaration)
	}
	return FunctionDeclaration(n)
}

func (f FunctionDeclaration) Modifiers() List {
	return List{f.s, f.s.extra[f.h.data+functionModifiersSlot]}
}

func (f FunctionDeclaration) AsteriskToken() Node {
	return f.s.node(NodeRef(f.s.extra[f.h.data+functionAsteriskTokenSlot]))
}

func (f FunctionDeclaration) Name() Node {
	return f.s.node(NodeRef(f.s.extra[f.h.data+functionNameSlot]))
}

func (f FunctionDeclaration) TypeParameters() List {
	return List{f.s, f.s.extra[f.h.data+functionTypeParametersSlot]}
}

func (f FunctionDeclaration) Parameters() List {
	return List{f.s, f.s.extra[f.h.data+functionParametersSlot]}
}

func (f FunctionDeclaration) Type() Node {
	return f.s.node(NodeRef(f.s.extra[f.h.data+functionTypeSlot]))
}

func (f FunctionDeclaration) FullSignature() Node {
	return f.s.node(NodeRef(f.s.extra[f.h.data+functionFullSignatureSlot]))
}

func (f FunctionDeclaration) Body() Node {
	return f.s.node(NodeRef(f.s.extra[f.h.data+functionBodySlot]))
}

type SourceFile struct {
	s *Store
	h *NodeHeader
}

func (n Node) AsSourceFile() SourceFile {
	if storeChecks {
		n.assertKind(ast.KindSourceFile)
	}
	return SourceFile(n)
}

func (f SourceFile) Statements() List {
	return List{f.s, f.s.extra[f.h.data+sourceFileStatementsSlot]}
}

func (f SourceFile) EndOfFileToken() Node {
	return f.s.node(NodeRef(f.s.extra[f.h.data+sourceFileEndOfFileTokenSlot]))
}

// Kind-polymorphic accessors. One static table lookup picks the slot, so the
// code is the same for every kind: no switch, no jump table. A kind without the
// role returns the nil node.

func (n Node) Name() Node {
	slot := nameSlot[n.h.kind&511]
	if slot == 0xFF {
		return n.s.node(0)
	}
	return n.s.node(NodeRef(n.s.extra[n.h.data+uint32(slot)]))
}

func (n Node) Expression() Node {
	slot := expressionSlot[n.h.kind&511]
	if slot == 0xFF {
		return n.s.node(0)
	}
	return n.s.node(NodeRef(n.s.extra[n.h.data+uint32(slot)]))
}

func (n Node) Type() Node {
	slot := typeSlot[n.h.kind&511]
	if slot == 0xFF {
		return n.s.node(0)
	}
	return n.s.node(NodeRef(n.s.extra[n.h.data+uint32(slot)]))
}

func (n Node) Initializer() Node {
	slot := initializerSlot[n.h.kind&511]
	if slot == 0xFF {
		return n.s.node(0)
	}
	return n.s.node(NodeRef(n.s.extra[n.h.data+uint32(slot)]))
}

func (n Node) Body() Node {
	slot := bodySlot[n.h.kind&511]
	if slot == 0xFF {
		return n.s.node(0)
	}
	return n.s.node(NodeRef(n.s.extra[n.h.data+uint32(slot)]))
}
