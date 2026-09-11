package ast

// IfStatementRefs snapshots the condition, then statement, and else statement
// of a same-Store IfStatement. The caller must know the kind. Missing or foreign
// children retain ChildRef's zero representation. Only integer references are
// returned; later syntax edits are not reflected in the snapshot.
func (s *Store) IfStatementRefs(ref NodeRef) (NodeRef, NodeRef, NodeRef) {
	if s == nil || ref == NoNodeRef {
		return 0, 0, 0
	}
	n := &s.nodes[ref]
	if n.childLen != 3 {
		panic("ast: expected three IfStatement child slots")
	}
	start := int(n.childStart)
	children := s.children[start : start+3]
	return children[0], children[1], children[2]
}
