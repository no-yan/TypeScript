package ast

import "github.com/microsoft/TypeScript/tsc/internal/core"

// FactoryHooks mirrors NodeFactoryHooks for the Store-backed factory.
type FactoryHooks struct {
	OnCreate func(Handle)
}

// Factory allocates exclusively into one Store. NewFactory owns a new Store.
// NewFactoryOn appends into an existing Store so checker synthetics and emit
// updates can share parse children. Factories on the same Store must not be
// used concurrently; ownership transfers between compiler phases.
type Factory struct {
	hooks FactoryHooks
	store *Store
}

func NewFactory(hooks FactoryHooks) *Factory {
	return NewFactoryHint(hooks, 256)
}

func NewFactoryHint(hooks FactoryHooks, hint int) *Factory {
	return &Factory{hooks: hooks, store: NewStore(hint)}
}

func NewFactoryOn(s *Store, hooks FactoryHooks) *Factory {
	if s == nil {
		panic("ast: NewFactoryOn nil Store")
	}
	return &Factory{hooks: hooks, store: s}
}

func (f *Factory) Store() *Store { return f.store }

func (f *Factory) Seal() { f.store.Seal() }

// Finish sets parser-owned source locations and parent edges after a native
// constructor has populated the node. It mirrors the Store work performed by
// Parser.finishNode on the dual-write NodeFactory path.
func (f *Factory) Finish(h Handle, loc core.TextRange) Handle {
	if h.Store() != f.store || h.Ref() == 0 {
		panic("ast: Finish Handle from a different Store")
	}
	h.SetLoc(loc)
	h.ForEachChild(func(child Handle) bool {
		child.SetParent(h)
		return false
	})
	return h
}

func (f *Factory) create(kind Kind, flags NodeFlags, loc core.TextRange, childLen int) Handle {
	return f.createSlots(kind, flags, loc, childLen, 0)
}

func (f *Factory) createSlots(kind Kind, flags NodeFlags, loc core.TextRange, childLen, listLen int) Handle {
	h := f.store.AllocSlots(kind, flags, loc, childLen, listLen)
	if f.hooks.OnCreate != nil {
		f.hooks.OnCreate(h)
	}
	return h
}

func (f *Factory) Identifier(text string) Handle {
	return f.NewIdentifier(text)
}

func (f *Factory) Token(kind Kind) Handle {
	return f.NewToken(kind)
}

func updateHandle(updated, original Handle) Handle {
	if updated != original {
		updated.SetFlags(original.Flags())
		updated.SetLoc(original.Loc())
	}
	return updated
}

func handlesEqual(a, b Handle) bool {
	if a.IsNil() || b.IsNil() {
		return a.IsNil() && b.IsNil()
	}
	return a == b
}

// HandleVisitor walks Store nodes and rebuilds via Factory.Update*.
type HandleVisitor struct {
	Visit   func(Handle) Handle
	Factory *Factory
}

func (v *HandleVisitor) VisitEachChild(node Handle) Handle {
	if node.IsNil() || v == nil || v.Visit == nil {
		return node
	}
	return node.VisitEachChild(v)
}

func (v *HandleVisitor) VisitNode(node Handle) Handle {
	if node.IsNil() || v.Visit == nil {
		return node
	}
	return v.Visit(node)
}

func (v *HandleVisitor) VisitNodes(list ListRef) ListRef {
	if list == 0 || v.Visit == nil || v.Factory == nil {
		return list
	}
	s := v.Factory.Store()
	n := s.ListLen(list)
	changed := false
	elems := make([]Handle, 0, n)
	for i := 0; i < n; i++ {
		old := s.ListAt(list, i)
		visited := v.VisitNode(old)
		if visited != old {
			changed = true
		}
		if !visited.IsNil() {
			elems = append(elems, visited)
		} else {
			changed = true
		}
	}
	if !changed {
		return list
	}
	return v.Factory.List(s.ListLoc(list), elems...)
}

func (f *Factory) BinaryExpression(p BinaryParts) Handle {
	h := f.NewBinaryExpression(0, p.Left, Handle{}, p.Operator, p.Right)
	h.SetLoc(p.Loc)
	return h
}

// List allocates a packed NodeList-equivalent. Elements may be zero.
func (f *Factory) List(loc core.TextRange, elems ...Handle) ListRef {
	list := f.store.AllocList(loc, len(elems))
	for i, e := range elems {
		f.store.SetListAt(list, i, e)
	}
	return list
}

func (f *Factory) ArrayLiteral(p ArrayLiteralParts) Handle {
	h := f.NewArrayLiteralExpression(p.Elements, false)
	h.SetLoc(p.Loc)
	return h
}

func (f *Factory) Parameter(p ParameterParts) Handle {
	h := f.NewParameterDeclaration(0, p.DotDotDot, p.Name, p.Question, p.Type, p.Initializer)
	h.SetLoc(p.Loc)
	return h
}

func (f *Factory) FunctionExpression(loc core.TextRange, params ListRef) Handle {
	h := f.NewFunctionExpression(0, Handle{}, Handle{}, 0, params, Handle{}, Handle{}, Handle{})
	h.SetLoc(loc)
	return h
}

// NewSourceFile creates the Store-owned SourceFile syntax root. File metadata
// is initialized when a pointer view is materialized for legacy consumers.
func (f *Factory) NewSourceFile(statements ListRef, endOfFileToken Handle) Handle {
	h := f.createSlots(KindSourceFile, 0, core.UndefinedTextRange(), slotSourceFileCount, listSlotSourceFileCount)
	h.SetSourceFileEndOfFileToken(endOfFileToken)
	h.SetSourceFileStatements(statements)
	return h
}

func (h Handle) ParamType() Handle {
	h.requireKind(KindParameter)
	return h.ParameterDeclarationType()
}

func (h Handle) SetParamType(typ Handle) {
	h.requireKind(KindParameter)
	h.SetParameterDeclarationType(typ)
}

func (h Handle) ParamQuestion() Handle {
	h.requireKind(KindParameter)
	return h.ParameterDeclarationQuestionToken()
}

func (h Handle) SetParamQuestion(q Handle) {
	h.requireKind(KindParameter)
	h.SetParameterDeclarationQuestionToken(q)
}

func (h Handle) Elements() ListRef {
	h.requireKind(KindArrayLiteralExpression)
	return h.ArrayLiteralExpressionElements()
}

// Left / Operator / Right are named BinaryExpression slot accessors.
func (h Handle) Left() Handle {
	h.requireKind(KindBinaryExpression)
	return h.BinaryExpressionLeft()
}

func (h Handle) Operator() Handle {
	h.requireKind(KindBinaryExpression)
	return h.BinaryExpressionOperatorToken()
}

func (h Handle) Right() Handle {
	h.requireKind(KindBinaryExpression)
	return h.BinaryExpressionRight()
}

func (h Handle) requireKind(k Kind) {
	h.mustLive()
	if h.s.nodes[h.id].kind != k {
		panic("ast: Handle kind mismatch")
	}
}
