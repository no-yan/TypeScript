package ast

// BindListSpan is borrowed metadata for one list in the receiver Store.
// Only use with that Store while the syntax list's start/length are unchanged.
// Flags, Symbols, and Flow mutations do not invalidate this metadata.
// Element values are re-read from s.children on each BindListSpanElem call.
// Do not keep a Span across Compact/Restore or list start/length edits.
type BindListSpan struct {
	start  uint32
	length uint32
}

// Len returns the number of list elements covered by the span.
func (v BindListSpan) Len() int { return int(v.length) }

// TryBindListSpan resolves a local-only list header once.
// Returns false for nil Store, missing list, or foreign (other-Store) lists;
// callers must fall back to ListLen/ListElem.
func (s *Store) TryBindListSpan(list ListRef) (BindListSpan, bool) {
	if s == nil || list == 0 {
		return BindListSpan{}, false
	}
	if owner := StoreID(list >> 32); owner != 0 && owner != s.ID() {
		return BindListSpan{}, false
	}
	l := &s.lists[uint32(list)]
	return BindListSpan{start: l.start, length: l.len}, true
}

// BindListSpanElem returns the NodeRef at index i without re-resolving the list owner or header.
func (s *Store) BindListSpanElem(v BindListSpan, i int) NodeRef {
	if i < 0 || i >= int(v.length) {
		panic("ast: list index out of range")
	}
	return s.children[int(v.start)+i]
}
