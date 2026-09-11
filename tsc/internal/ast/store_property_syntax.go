package ast

// PropertyAccessExpressionRefs snapshots the expression, question-dot token,
// and name of a same-Store PropertyAccessExpression. The caller must know the
// kind. Missing and foreign children retain ChildRef's zero representation.
// Only integer values escape: Store growth is safe, but subsequent syntax
// edits are not reflected in the snapshot.
func (s *Store) PropertyAccessExpressionRefs(ref NodeRef) (NodeRef, NodeRef, NodeRef) {
	if s == nil || ref == NoNodeRef {
		return 0, 0, 0
	}
	n := &s.nodes[ref]
	if n.childLen != 3 {
		panic("ast: expected three PropertyAccessExpression child slots")
	}
	start := int(n.childStart)
	children := s.children[start : start+3]
	return children[0], children[1], children[2]
}
