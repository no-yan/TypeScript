package store

import "sync/atomic"

// The StoreID numbering of a program (Y3, invariant 22 of
// internal/ast/docs/store-checker-design-20260923.md 2.4).

// IDSpace is a program's StoreID namespace and its identity. Dynamic programs
// allocate every Store, including source Stores, with Next. Fixed-input
// fixtures and harnesses may reserve their source IDs with NewIDSpace(nFiles).
// A StoreID is never reassigned or reused; its value does not identify a
// Store's kind.
// Every Store of the program holds the IDSpace (Store.space), so that a
// Factory refuses a node of another program, whose StoreIDs may be the same
// numbers (invariant 26).
type IDSpace struct {
	next atomic.Uint32
}

// NewIDSpace reserves 0..nFiles-1 for a known, fixed set of source Stores;
// callers assign each reserved ID at most once, and Next starts at nFiles.
// Dynamic programs pass zero and obtain source IDs from Next as well.
// Do not add source Stores to a fixed reservation or mix numbering schemes.
func NewIDSpace(nFiles int) *IDSpace {
	s := &IDSpace{}
	s.next.Store(uint32(nFiles))
	return s
}

// Next allocates a fresh, unreserved StoreID, including for a dynamic source
// Store. At the end of the range it panics without moving the counter, so that
// no later call reissues a number.
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
