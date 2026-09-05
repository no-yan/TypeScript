package ast

import "iter"

// NodeSeq is an allocation-free node sequence. Range with
//
//	for i, node := range seq.All() { ... }
//
// without materializing a []Handle. Prefer Store.ListLen / ListAt / ListIndexOf
// when the original ListRef is available. Slice is the allocation boundary.
// Zero value is empty.
type NodeSeq struct {
	s       *Store
	list    ListRef
	handles []Handle
	decls   []GlobalRef
}

// EmptyNodeSeq is the empty sequence (zero value).
var EmptyNodeSeq NodeSeq

// NodeSequence adapts a materialized []Handle. Prefer ListSlice / *Seq() when
// the source is a packed list. Empty or nil input yields the empty sequence.
func NodeSequence(handles []Handle) NodeSeq {
	if len(handles) == 0 {
		return EmptyNodeSeq
	}
	return NodeSeq{handles: handles}
}

// All returns an inlinable iterator. Call it in the range clause; do not store
// the result in a variable.
func (n NodeSeq) All() iter.Seq2[int, Handle] {
	return func(yield func(int, Handle) bool) {
		switch {
		case n.s != nil && n.list != 0:
			s, list := n.s, n.list
			ln := s.ListLen(list)
			for i := 0; i < ln; i++ {
				if !yield(i, s.ListAt(list, i)) {
					return
				}
			}
		case n.handles != nil:
			for i, h := range n.handles {
				if !yield(i, h) {
					return
				}
			}
		default:
			dense := 0
			for _, g := range n.decls {
				h := NodeOf(g)
				if h.IsNil() {
					continue
				}
				if !yield(dense, h) {
					return
				}
				dense++
			}
		}
	}
}

// Len returns the number of elements in n.
func (n NodeSeq) Len() int {
	switch {
	case n.s != nil && n.list != 0:
		return n.s.ListLen(n.list)
	case n.handles != nil:
		return len(n.handles)
	default:
		c := 0
		for _, g := range n.decls {
			if !NodeOf(g).IsNil() {
				c++
			}
		}
		return c
	}
}

// At returns the element at dense index i, or a nil Handle if out of range.
func (n NodeSeq) At(i int) Handle {
	if i < 0 {
		return Handle{}
	}
	switch {
	case n.s != nil && n.list != 0:
		if i >= n.s.ListLen(n.list) {
			return Handle{}
		}
		return n.s.ListAt(n.list, i)
	case n.handles != nil:
		if i >= len(n.handles) {
			return Handle{}
		}
		return n.handles[i]
	default:
		dense := 0
		for _, g := range n.decls {
			h := NodeOf(g)
			if h.IsNil() {
				continue
			}
			if dense == i {
				return h
			}
			dense++
		}
		return Handle{}
	}
}

// First returns the first element, or a nil Handle if empty.
func (n NodeSeq) First() Handle {
	switch {
	case n.s != nil && n.list != 0:
		if n.s.ListLen(n.list) == 0 {
			return Handle{}
		}
		return n.s.ListAt(n.list, 0)
	case n.handles != nil:
		if len(n.handles) == 0 {
			return Handle{}
		}
		return n.handles[0]
	default:
		for _, g := range n.decls {
			h := NodeOf(g)
			if !h.IsNil() {
				return h
			}
		}
		return Handle{}
	}
}

// Last returns the last element, or a nil Handle if empty.
func (n NodeSeq) Last() Handle {
	switch {
	case n.s != nil && n.list != 0:
		ln := n.s.ListLen(n.list)
		if ln == 0 {
			return Handle{}
		}
		return n.s.ListAt(n.list, ln-1)
	case n.handles != nil:
		ln := len(n.handles)
		if ln == 0 {
			return Handle{}
		}
		return n.handles[ln-1]
	default:
		var out Handle
		for _, g := range n.decls {
			h := NodeOf(g)
			if !h.IsNil() {
				out = h
			}
		}
		return out
	}
}

// Some reports whether pred holds for any element.
func (n NodeSeq) Some(pred func(Handle) bool) bool {
	if pred == nil {
		return false
	}
	for _, h := range n.All() {
		if pred(h) {
			return true
		}
	}
	return false
}

// Every reports whether pred holds for every element. Vacuously true when empty.
func (n NodeSeq) Every(pred func(Handle) bool) bool {
	if pred == nil {
		return true
	}
	for _, h := range n.All() {
		if !pred(h) {
			return false
		}
	}
	return true
}

// Count returns how many elements satisfy pred.
func (n NodeSeq) Count(pred func(Handle) bool) int {
	if pred == nil {
		return 0
	}
	c := 0
	for _, h := range n.All() {
		if pred(h) {
			c++
		}
	}
	return c
}

// FirstMatching returns the first element for which pred holds, or a nil Handle.
func (n NodeSeq) FirstMatching(pred func(Handle) bool) Handle {
	if pred == nil {
		return Handle{}
	}
	for _, h := range n.All() {
		if pred(h) {
			return h
		}
	}
	return Handle{}
}

// LastMatching returns the last element for which pred holds, or a nil Handle.
func (n NodeSeq) LastMatching(pred func(Handle) bool) Handle {
	if pred == nil {
		return Handle{}
	}
	var out Handle
	for _, h := range n.All() {
		if pred(h) {
			out = h
		}
	}
	return out
}

// Values yields handles without indices.
func (n NodeSeq) Values() iter.Seq[Handle] {
	return func(yield func(Handle) bool) {
		for _, h := range n.All() {
			if !yield(h) {
				return
			}
		}
	}
}

// Slice materializes the sequence into a []Handle. This is the allocation
// boundary. Use only for mutate/sort/slice expr/variadic/[]Handle ownership.
// An empty sequence returns nil.
func (n NodeSeq) Slice() []Handle {
	ln := n.Len()
	if ln == 0 {
		return nil
	}
	out := make([]Handle, 0, ln)
	for _, h := range n.All() {
		out = append(out, h)
	}
	return out
}
