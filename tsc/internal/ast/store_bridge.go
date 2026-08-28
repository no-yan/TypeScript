package ast

import "github.com/microsoft/TypeScript/tsc/internal/core"

// AttachStore makes NodeFactory.New* allocate matching rows in s.
// Parser uses this so ParseSourceFile writes Store through NewFactory
// without rewriting every *Node call site in this PR.
func (f *NodeFactory) AttachStore(s *Store) {
	if f == nil {
		panic("ast: AttachStore on nil NodeFactory")
	}
	if s == nil {
		panic("ast: AttachStore nil Store")
	}
	f.store = s
	f.nodeRef = make(map[*Node]NodeRef)
}

// Parser AttachStore wipes; checker reuses the parse map so parse children keep their refs.
func (f *NodeFactory) AttachStoreMap(s *Store, m map[*Node]NodeRef) {
	if f == nil {
		panic("ast: AttachStoreMap on nil NodeFactory")
	}
	if s == nil {
		panic("ast: AttachStoreMap nil Store")
	}
	f.store = s
	if m == nil {
		f.nodeRef = make(map[*Node]NodeRef)
	} else {
		f.nodeRef = m
	}
}

func (f *NodeFactory) Store() *Store {
	if f == nil {
		return nil
	}
	return f.store
}

func (f *NodeFactory) HandleOf(n *Node) Handle {
	if f == nil || f.store == nil || n == nil || f.nodeRef == nil {
		return Handle{}
	}
	id, ok := f.nodeRef[n]
	if !ok {
		return Handle{}
	}
	return f.store.At(id)
}

func (f *NodeFactory) StoreSync(n *Node) {
	h := f.HandleOf(n)
	if h.Ref() == 0 {
		return
	}
	h.SetLoc(n.Loc)
	h.SetFlags(n.Flags)
}

func (f *NodeFactory) storeAlloc(node *Node, childLen, listLen int) Handle {
	if f == nil || f.store == nil || node == nil {
		return Handle{}
	}
	if f.nodeRef == nil {
		f.nodeRef = make(map[*Node]NodeRef)
	}
	h := f.store.AllocSlots(node.Kind, node.Flags, node.Loc, childLen, listLen)
	f.nodeRef[node] = h.Ref()
	return h
}

func (f *NodeFactory) storeHandle(n *Node) Handle {
	if h := f.HandleOf(n); h.Ref() != 0 {
		return h
	}
	file := GetSourceFileOfNode(n)
	if file == nil {
		return Handle{}
	}
	return file.HandleOf(n)
}

func (f *NodeFactory) storeList(list *NodeList) ListRef {
	if f == nil || f.store == nil || list == nil {
		return 0
	}
	return f.allocListFromNodes(list.Loc, list.Nodes)
}

func (f *NodeFactory) storeModifierList(list *ModifierList) ListRef {
	if f == nil || f.store == nil || list == nil {
		return 0
	}
	return f.allocListFromNodes(list.Loc, list.Nodes)
}

func (f *NodeFactory) storeRawList(nodes []*Node) ListRef {
	if f == nil || f.store == nil || nodes == nil {
		return 0
	}
	loc := core.UndefinedTextRange()
	if len(nodes) > 0 {
		loc = core.NewTextRange(nodes[0].Pos(), nodes[len(nodes)-1].End())
	}
	return f.allocListFromNodes(loc, nodes)
}

func (f *NodeFactory) allocListFromNodes(loc core.TextRange, nodes []*Node) ListRef {
	list := f.store.AllocList(loc, len(nodes))
	for i, n := range nodes {
		f.store.SetListAt(list, i, f.storeHandle(n))
	}
	return list
}

func (f *NodeFactory) TakeNodeRef() map[*Node]NodeRef {
	if f == nil {
		return nil
	}
	m := f.nodeRef
	f.nodeRef = nil
	return m
}
