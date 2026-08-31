package ast

// ExpandStore copies a Store tree back to *Node. Parser calls this at the
// parse boundary so binder, checker, and printer stay on *Node for this PR.
// Delete this file in PR-6. Do not land this bridge on microsoft/main.
func ExpandStore(root Handle, opts SourceFileParseOptions, text string) *Node {
	if root.Ref() == 0 {
		return nil
	}
	e := &storeExpander{
		f:     NewNodeFactory(NodeFactoryHooks{}),
		nodes: make(map[NodeRef]*Node),
	}
	var result *Node
	if root.Kind() == KindSourceFile {
		result = expandSourceFile(e, root, opts, text)
	} else {
		result = expandStored(e, root)
	}
	SetParentInChildren(result)
	applyStoreSideData(root, e)
	return result
}

type storeExpander struct {
	f     *NodeFactory
	nodes map[NodeRef]*Node
}

func expandSourceFile(e *storeExpander, h Handle, opts SourceFileParseOptions, text string) *Node {
	var stmts *NodeList
	if h.NumListSlots() > listSlotSourceFileStatements {
		stmts = expandNodeList(e, h.Store(), h.ListSlot(listSlotSourceFileStatements))
	}
	var eof *Node
	if h.NumChildren() > slotSourceFileEndOfFileToken {
		eof = expandStored(e, h.Child(slotSourceFileEndOfFileToken))
	}
	n := e.f.NewSourceFile(opts, text, stmts, eof)
	e.nodes[h.Ref()] = n
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

func applyStoreSideData(root Handle, e *storeExpander) {
	Walk(root, func(h Handle) bool {
		n := e.nodes[h.Ref()]
		if n == nil {
			return false
		}
		if declaration := n.DeclarationData(); declaration != nil {
			declaration.Symbol = h.Symbol()
		}
		if exportable := n.ExportableData(); exportable != nil {
			exportable.LocalSymbol = h.LocalSymbol()
		}
		if locals := n.LocalsContainerData(); locals != nil {
			locals.Locals = h.Locals()
			locals.NextContainer = e.nodes[h.NextContainer().Ref()]
		}
		if flow := n.FlowNodeData(); flow != nil {
			flow.FlowNode = h.FlowNode()
		}
		if body := n.BodyData(); body != nil {
			body.EndFlowNode = h.EndFlowNode()
		}
		switch n.Kind {
		case KindConstructor:
			n.AsConstructorDeclaration().ReturnFlowNode = h.ReturnFlowNode()
		case KindFunctionDeclaration:
			n.AsFunctionDeclaration().ReturnFlowNode = h.ReturnFlowNode()
		case KindFunctionExpression:
			n.AsFunctionExpression().ReturnFlowNode = h.ReturnFlowNode()
		case KindClassStaticBlockDeclaration:
			n.AsClassStaticBlockDeclaration().ReturnFlowNode = h.ReturnFlowNode()
		}
		return false
	})
}

func expandNodeList(e *storeExpander, s *Store, list ListRef) *NodeList {
	if list == 0 || s == nil {
		return nil
	}
	n := s.ListLen(list)
	nodes := make([]*Node, n)
	for i := range n {
		nodes[i] = expandStored(e, s.ListAt(list, i))
	}
	out := e.f.NewNodeList(nodes)
	out.Loc = s.ListLoc(list)
	return out
}

func expandModifierList(e *storeExpander, s *Store, list ListRef) *ModifierList {
	if list == 0 || s == nil {
		return nil
	}
	n := s.ListLen(list)
	nodes := make([]*Node, n)
	for i := range n {
		nodes[i] = expandStored(e, s.ListAt(list, i))
	}
	out := e.f.NewModifierList(nodes)
	out.Loc = s.ListLoc(list)
	return out
}

func expandRawList(e *storeExpander, s *Store, list ListRef) []*Node {
	if list == 0 || s == nil {
		return nil
	}
	n := s.ListLen(list)
	nodes := make([]*Node, n)
	for i := range n {
		nodes[i] = expandStored(e, s.ListAt(list, i))
	}
	return nodes
}
