package store

import "sync/atomic"

// The StoreID numbering of a program (Y3, invariant 22 of
// internal/ast/docs/store-checker-design-20260923.md 2.4).

// IDSpace is a program's StoreID namespace and its identity: the source
// Stores are 0..nFiles-1, given by the program, and every other Store (lazy
// JSDoc, synthetic) takes the next number here. A number is never reused.
// Every Store of the program holds the IDSpace (Store.space), so that a
// Factory refuses a node of another program, whose StoreIDs may be the same
// numbers (invariant 26).
type IDSpace struct {
	next atomic.Uint32
}

// NewIDSpace numbers the Stores after the nFiles source Stores.
func NewIDSpace(nFiles int) *IDSpace {
	s := &IDSpace{}
	s.next.Store(uint32(nFiles))
	return s
}

// Next is the StoreID of a new Store that is not a source Store. At the end of
// the range it panics without moving the counter, so that no later call
// reissues a number.
func (s *IDSpace) Next() StoreID {
	for {
		id := s.next.Load()
		if id == ^uint32(0) {
			panic("store: the StoreIDs of the program are exhausted")
		}
		if s.next.CompareAndSwap(id, id+1) {
			return StoreID(id)
		}
	}
}
