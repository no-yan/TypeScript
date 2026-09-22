//go:build storeexp

package storeexp

// Visitor returns true to stop the walk, like ast.Visitor.
type Visitor func(Node) bool

// Callers skip the nil list (at == 0) before calling: 60% of the list slots in
// a walk are nil, and Refs() costs 25 instructions (store-ast-design-20260922.md
// 2.3). The check lives at the call site so that visitList stays within the
// inline budget (cost 80).
func visitList(s *Store, at uint32, v Visitor) bool {
	for _, ref := range (List{s, at}).Refs() {
		if v(s.node(ref)) {
			return true
		}
	}
	return false
}

// ForEachChildShape is ForEachChild without the kind switch: it loops over the
// slots that shapes[kind] describes. It is an optimization candidate
// (store-ast-design-20260922.md section 8) that is measured next to the switch.
func (n Node) ForEachChildShape(v Visitor) bool {
	sh := shapes[n.h.kind&511]
	s, data := n.s, n.h.data
	for i := range uint32(sh.slots) {
		slot := s.extra[data+i]
		if sh.listMask&(1<<i) != 0 {
			if slot != 0 && visitList(s, slot, v) {
				return true
			}
		} else if slot != 0 && v(s.node(NodeRef(slot))) {
			return true
		}
	}
	return false
}
