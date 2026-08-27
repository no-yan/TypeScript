package ast

// ExpandStore copies a Store tree back to *Node. Parser calls this at the
// parse boundary so binder, checker, and printer stay on *Node for this PR.
// Delete this file in PR-6. Do not land this bridge on microsoft/main.
func ExpandStore(root Handle, opts SourceFileParseOptions, text string) *Node {
	if root.Ref() == 0 {
		return nil
	}
	f := NewNodeFactory(NodeFactoryHooks{})
	if root.Kind() == KindSourceFile {
		return expandSourceFile(f, root, opts, text)
	}
	return expandStored(f, root)
}

func expandSourceFile(f *NodeFactory, h Handle, opts SourceFileParseOptions, text string) *Node {
	var stmts *NodeList
	if h.NumListSlots() > listSlotSourceFileStatements {
		stmts = expandNodeList(f, h.Store(), h.ListSlot(listSlotSourceFileStatements))
	}
	var eof *Node
	if h.NumChildren() > slotSourceFileEndOfFileToken {
		eof = expandStored(f, h.Child(slotSourceFileEndOfFileToken))
	}
	n := f.NewSourceFile(opts, text, stmts, eof)
	applyStoreHeader(n, h)
	return n
}

func applyStoreHeader(n *Node, h Handle) {
	if n == nil || h.Ref() == 0 {
		return
	}
	n.Loc = h.Loc()
	n.Flags = h.Flags()
}

func expandNodeList(f *NodeFactory, s *Store, list ListRef) *NodeList {
	if list == 0 || s == nil {
		return nil
	}
	n := s.ListLen(list)
	nodes := make([]*Node, n)
	for i := range n {
		nodes[i] = expandStored(f, s.ListAt(list, i))
	}
	out := f.NewNodeList(nodes)
	out.Loc = s.ListLoc(list)
	return out
}

func expandModifierList(f *NodeFactory, s *Store, list ListRef) *ModifierList {
	if list == 0 || s == nil {
		return nil
	}
	n := s.ListLen(list)
	nodes := make([]*Node, n)
	for i := range n {
		nodes[i] = expandStored(f, s.ListAt(list, i))
	}
	out := f.NewModifierList(nodes)
	out.Loc = s.ListLoc(list)
	return out
}

func expandRawList(f *NodeFactory, s *Store, list ListRef) []*Node {
	if list == 0 || s == nil {
		return nil
	}
	n := s.ListLen(list)
	nodes := make([]*Node, n)
	for i := range n {
		nodes[i] = expandStored(f, s.ListAt(list, i))
	}
	return nodes
}
