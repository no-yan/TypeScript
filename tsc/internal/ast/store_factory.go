package ast

import "github.com/microsoft/TypeScript/tsc/internal/core"

// FactoryHooks mirrors NodeFactoryHooks for the Store-backed factory.
type FactoryHooks struct {
	OnCreate func(Handle)
}

// Factory allocates exclusively into an owned Store. It is the β entry
// point: callers receive Handle values, never *Node.
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

func (f *Factory) Store() *Store { return f.store }

func (f *Factory) Seal() { f.store.Seal() }

func (f *Factory) create(kind Kind, flags NodeFlags, loc core.TextRange, childLen int) Handle {
	h := f.store.Alloc(kind, flags, loc, childLen)
	if f.hooks.OnCreate != nil {
		f.hooks.OnCreate(h)
	}
	return h
}

func (f *Factory) Identifier(text string) Handle {
	h := f.create(KindIdentifier, 0, core.UndefinedTextRange(), 0)
	if text != "" {
		h.SetIdent(f.store.Intern(text))
	}
	return h
}

func (f *Factory) Token(kind Kind) Handle {
	return f.create(kind, 0, core.UndefinedTextRange(), 0)
}

func (f *Factory) BinaryExpression(p BinaryParts) Handle {
	h := f.create(KindBinaryExpression, 0, p.Loc, binSlotCount)
	h.SetChild(binSlotLeft, p.Left)
	h.SetChild(binSlotOperator, p.Operator)
	h.SetChild(binSlotRight, p.Right)
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
	h := f.create(KindArrayLiteralExpression, 0, p.Loc, 0)
	h.SetList(p.Elements)
	return h
}

func (f *Factory) Parameter(p ParameterParts) Handle {
	h := f.create(KindParameter, 0, p.Loc, paramSlotCount)
	h.SetChild(paramSlotDotDotDot, p.DotDotDot)
	h.SetChild(paramSlotName, p.Name)
	h.SetChild(paramSlotQuestion, p.Question)
	h.SetChild(paramSlotType, p.Type)
	h.SetChild(paramSlotInitializer, p.Initializer)
	return h
}

func (f *Factory) FunctionExpression(loc core.TextRange, params ListRef) Handle {
	h := f.create(KindFunctionExpression, 0, loc, 0)
	h.SetList(params)
	return h
}

func (h Handle) ParamType() Handle {
	h.requireKind(KindParameter)
	return h.Child(paramSlotType)
}

func (h Handle) SetParamType(typ Handle) {
	h.requireKind(KindParameter)
	h.SetChild(paramSlotType, typ)
}

func (h Handle) ParamQuestion() Handle {
	h.requireKind(KindParameter)
	return h.Child(paramSlotQuestion)
}

func (h Handle) SetParamQuestion(q Handle) {
	h.requireKind(KindParameter)
	h.SetChild(paramSlotQuestion, q)
}

func (h Handle) Elements() ListRef {
	h.requireKind(KindArrayLiteralExpression)
	return h.List()
}

// Left / Operator / Right are named BinaryExpression slot accessors.
func (h Handle) Left() Handle {
	h.requireKind(KindBinaryExpression)
	return h.Child(binSlotLeft)
}

func (h Handle) Operator() Handle {
	h.requireKind(KindBinaryExpression)
	return h.Child(binSlotOperator)
}

func (h Handle) Right() Handle {
	h.requireKind(KindBinaryExpression)
	return h.Child(binSlotRight)
}

func (h Handle) requireKind(k Kind) {
	h.mustLive()
	if h.s.nodes[h.id].kind != k {
		panic("ast: Handle kind mismatch")
	}
}
