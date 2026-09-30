package store

// The parts of VisitEachChild (visit_generated.go) that the generator does not
// write, as in the Pointer AST (ast.go): the hand-written visitor of
// JSDocParameterOrPropertyTag, and the visit of a raw list.
// internal/ast/docs/store-checker-7d2a-implementation-instructions-20260929.md
// 3.3 (R12).

// visitEachChild_JSDocParameterOrPropertyTag is ast.visitEachChild_JSDocParameterOrPropertyTag.
func visitEachChild_JSDocParameterOrPropertyTag(node JSDocParameterOrPropertyTag, v *NodeVisitor) Node {
	return v.Factory.UpdateJSDocParameterOrPropertyTag(node, v.visitNode(node.TagName()), v.visitNode(node.Name()), node.IsBracketed(), v.visitNode(node.TypeExpression()), node.IsNameFirst(), v.visitNodes(node.Comment()))
}

// visitRawNodes is the Pointer generator's visit of a raw []*Node member:
// core.SameMap with visitNode, without the list hooks. A changed list is a
// new list of the visitor's Factory; an unchanged one is the list itself.
func (v *NodeVisitor) visitRawNodes(l List) List {
	for i := range l.Len() {
		value := l.At(i)
		mapped := v.visitNode(value)
		if !SameNode(mapped, value) {
			result := make([]Node, l.Len())
			for j := range i {
				result[j] = l.At(j)
			}
			result[i] = mapped
			for j := i + 1; j < l.Len(); j++ {
				result[j] = v.visitNode(l.At(j))
			}
			return v.Factory.NewNodeList(result)
		}
	}
	return l
}
