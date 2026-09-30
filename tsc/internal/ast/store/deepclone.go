package store

// DeepCloneNode is ast.NodeFactory.DeepCloneNode (ast/deepclone.go) on the
// Store: every node of the tree is cloned into the Factory's region with the
// synthetic range, and every list with it, the last element of a list with a
// trailing comma at (-2, -2). The writes go through the Factory's writers, on
// the nodes and lists it has just made (R8).
// internal/ast/docs/store-checker-7d2a-implementation-instructions-20260929.md
// 4.3.

func getDeepCloneVisitor(f *Factory, syntheticLocation bool) *NodeVisitor {
	var visitor *NodeVisitor
	visitor = NewNodeVisitor(
		func(node Node) Node {
			visited := visitor.VisitEachChild(node)
			if !SameNode(visited, node) {
				if syntheticLocation {
					f.SetRange(visited, -1, -1)
				}
				return visited
			}
			c := f.Clone(node) // forcibly clone leaf nodes, which will then cascade new nodes/arrays upwards via `update` calls
			// Deep clones are done to copy a node across files, so here, we explicitly make the location range synthetic on all cloned nodes
			if syntheticLocation {
				f.SetRange(c, -1, -1)
			}
			return c
		},
		f,
		NodeVisitorHooks{
			VisitNodes: func(nodes List, v *NodeVisitor) List {
				if nodes.IsNil() {
					return List{}
				}
				visited := v.VisitNodes(nodes)
				var newList List
				if !SameList(visited, nodes) {
					newList = visited
				} else {
					newList = CloneNodeList(v.Factory, nodes)
				}
				if syntheticLocation {
					v.Factory.SetListRange(newList, -1, -1)
					if nodes.HasTrailingComma() {
						v.Factory.SetRange(newList.At(newList.Len()-1), -2, -2)
					}
				}
				return newList
			},
			VisitModifiers: func(nodes List, v *NodeVisitor) List {
				if nodes.IsNil() {
					return List{}
				}
				visited := v.VisitModifiers(nodes)
				var newList List
				if !SameList(visited, nodes) {
					newList = visited
				} else {
					newList = CloneNodeList(v.Factory, nodes)
				}
				if syntheticLocation {
					v.Factory.SetListRange(newList, -1, -1)
					if nodes.HasTrailingComma() {
						v.Factory.SetRange(newList.At(newList.Len()-1), -2, -2)
					}
				}
				return newList
			},
		},
	)
	return visitor
}

func (f *Factory) DeepCloneNode(node Node) Node {
	return getDeepCloneVisitor(f, true /*syntheticLocation*/).VisitNode(node)
}
