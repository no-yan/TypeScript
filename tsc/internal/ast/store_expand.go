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
		local: make(map[*Node]struct{}),
	}
	var result *Node
	if root.Kind() == KindSourceFile {
		result = expandSourceFile(e, root, opts, text)
	} else {
		result = expandStored(e, root)
	}
	setExpandedParents(result, nil, e)
	applyStoreSideData(root, e)
	return result
}

type storeExpander struct {
	f     *NodeFactory
	nodes map[NodeRef]*Node
	local map[*Node]struct{}
}

func (e *storeExpander) remember(ref NodeRef, node *Node) {
	e.nodes[ref] = node
	e.local[node] = struct{}{}
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
	e.remember(h.Ref(), n)
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

func setExpandedParents(node *Node, parent *Node, e *storeExpander) {
	if node == nil {
		return
	}
	node.Parent = parent
	node.ForEachChild(func(child *Node) bool {
		if _, local := e.local[child]; local {
			setExpandedParents(child, node, e)
		}
		return false
	})
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

func expandStoredChild(e *storeExpander, parent Handle, slot int) *Node {
	if child := parent.Child(slot); child.Ref() != 0 {
		return expandStored(e, child)
	}
	if child := parent.ExternalChild(slot); child != 0 {
		node := NodeOf(child)
		if node == nil {
			panic("ast: unresolved external Store child")
		}
		return node
	}
	return nil
}

func expandStoredListChild(e *storeExpander, store *Store, list ListRef, index int) *Node {
	if child := store.ListAt(list, index); child.Ref() != 0 {
		return expandStored(e, child)
	}
	if child := store.ExternalListAt(list, index); child != 0 {
		node := NodeOf(child)
		if node == nil {
			panic("ast: unresolved external Store list child")
		}
		return node
	}
	return nil
}

func expandNodeList(e *storeExpander, s *Store, list ListRef) *NodeList {
	if list == 0 || s == nil {
		return nil
	}
	n := s.ListLen(list)
	nodes := make([]*Node, n)
	for i := range n {
		nodes[i] = expandStoredListChild(e, s, list, i)
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
		nodes[i] = expandStoredListChild(e, s, list, i)
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
		nodes[i] = expandStoredListChild(e, s, list, i)
	}
	return nodes
}
