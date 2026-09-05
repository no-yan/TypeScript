package ast

import "iter"

// NodeSeq is an allocation-free node sequence. Range over it with
//
//	for i, node := range seq { ... }
//
// without materializing a []Handle. Prefer Store.ListLen / ListAt / ListIndexOf
// when the original ListRef is available. Slice is the allocation boundary.
type NodeSeq func(yield func(int, Handle) bool)

// EmptyNodeSeq is a non-nil empty sequence. Prefer returning this over a nil
// NodeSeq so ranging never panics.
func EmptyNodeSeq(yield func(int, Handle) bool) {}

func (seq NodeSeq) resolve() NodeSeq {
	if seq == nil {
		return EmptyNodeSeq
	}
	return seq
}

// Len returns the number of elements yielded by seq.
func (seq NodeSeq) Len() int {
	n := 0
	seq.resolve()(func(int, Handle) bool {
		n++
		return true
	})
	return n
}

// At returns the element at dense index i, or a nil Handle if out of range.
func (seq NodeSeq) At(i int) Handle {
	if i < 0 {
		return Handle{}
	}
	var out Handle
	seq.resolve()(func(idx int, h Handle) bool {
		if idx == i {
			out = h
			return false
		}
		return idx < i
	})
	return out
}

// First returns the first element, or a nil Handle if empty.
func (seq NodeSeq) First() Handle {
	var out Handle
	seq.resolve()(func(_ int, h Handle) bool {
		out = h
		return false
	})
	return out
}

// Last returns the last element, or a nil Handle if empty.
func (seq NodeSeq) Last() Handle {
	var out Handle
	seq.resolve()(func(_ int, h Handle) bool {
		out = h
		return true
	})
	return out
}

// Some reports whether pred holds for any element.
func (seq NodeSeq) Some(pred func(Handle) bool) bool {
	if pred == nil {
		return false
	}
	found := false
	seq.resolve()(func(_ int, h Handle) bool {
		if pred(h) {
			found = true
			return false
		}
		return true
	})
	return found
}

// Every reports whether pred holds for every element. Vacuously true when empty.
func (seq NodeSeq) Every(pred func(Handle) bool) bool {
	if pred == nil {
		return true
	}
	ok := true
	seq.resolve()(func(_ int, h Handle) bool {
		if !pred(h) {
			ok = false
			return false
		}
		return true
	})
	return ok
}

// Count returns how many elements satisfy pred.
func (seq NodeSeq) Count(pred func(Handle) bool) int {
	if pred == nil {
		return 0
	}
	n := 0
	seq.resolve()(func(_ int, h Handle) bool {
		if pred(h) {
			n++
		}
		return true
	})
	return n
}

// FirstMatching returns the first element for which pred holds, or a nil Handle.
func (seq NodeSeq) FirstMatching(pred func(Handle) bool) Handle {
	if pred == nil {
		return Handle{}
	}
	var out Handle
	seq.resolve()(func(_ int, h Handle) bool {
		if pred(h) {
			out = h
			return false
		}
		return true
	})
	return out
}

// LastMatching returns the last element for which pred holds, or a nil Handle.
func (seq NodeSeq) LastMatching(pred func(Handle) bool) Handle {
	if pred == nil {
		return Handle{}
	}
	var out Handle
	seq.resolve()(func(_ int, h Handle) bool {
		if pred(h) {
			out = h
		}
		return true
	})
	return out
}

// Values yields handles without indices.
func (seq NodeSeq) Values() iter.Seq[Handle] {
	return func(yield func(Handle) bool) {
		seq.resolve()(func(_ int, h Handle) bool {
			return yield(h)
		})
	}
}

// Slice materializes the sequence into a []Handle. This is the allocation
// boundary — use only for mutate/sort/slice expr/variadic/[]Handle ownership.
// An empty sequence returns nil.
func (seq NodeSeq) Slice() []Handle {
	if seq == nil {
		return nil
	}
	var out []Handle
	seq(func(_ int, h Handle) bool {
		out = append(out, h)
		return true
	})
	return out
}
