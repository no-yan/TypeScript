package storechecker

import (
	"slices"

	"github.com/microsoft/TypeScript/tsc/internal/ast/store"
)

// The P2 declarations of checker/links.go:
// internal/ast/docs/store-checker-7d2c-abc-implementation-instructions-20261002.md
// 1.

const (
	nodeLinkPageShift = 8
	nodeLinkPageSize  = 1 << nodeLinkPageShift
	nodeLinkPageMask  = nodeLinkPageSize - 1
)

// nodeLinkStore is the Pointer's PagedLinkStore over GetNodeId, with one page
// table per Store. A NodeKey as a uint64 has the StoreID in its high word, so
// a single PagedLinkStore sent every key to its page map. StoreIDs are dense
// (IDSpace), so the outer table grows as Stores are made, synthetic ones
// included. The nil Node is NodeKey{}, one key like any other.
//
// storetranslate:pointer-hash 23820a26eafb977a links.go:nodeLinkStore
type nodeLinkStore[V any] struct {
	stores [][]*[nodeLinkPageSize]V // [StoreID][NodeRef>>nodeLinkPageShift]
}

// Get repeats the lookup of TryGet rather than calling it: TryGet is over the
// inline budget, and Get is the hot one.
//
// storetranslate:pointer-hash d5587bfdc4247b0b links.go:nodeLinkStore.Get
func (s *nodeLinkStore[V]) Get(node store.Node) *V {
	k := node.Key()
	if id := int(k.StoreID()); id < len(s.stores) {
		pages := s.stores[id]
		if i := int(k.ID() >> nodeLinkPageShift); i < len(pages) {
			if page := pages[i]; page != nil {
				return &page[k.ID()&nodeLinkPageMask]
			}
		}
	}
	return s.newPage(k)
}

// newPage grows the tables to the key and gives it a page, as
// PagedLinkStore.Get does.
//
// storetranslate:store-only
// storetranslate:pointer-hash d5587bfdc4247b0b links.go:nodeLinkStore.Get
func (s *nodeLinkStore[V]) newPage(k store.NodeKey) *V {
	id := int(k.StoreID())
	if id >= len(s.stores) {
		s.stores = slices.Grow(s.stores, id-len(s.stores)+1)[:id+1]
	}
	pages := s.stores[id]
	i := int(k.ID() >> nodeLinkPageShift)
	if i >= len(pages) {
		pages = slices.Grow(pages, i-len(pages)+1)[:i+1]
		s.stores[id] = pages
	}
	page := pages[i]
	if page == nil {
		page = new([nodeLinkPageSize]V)
		pages[i] = page
	}
	return &page[k.ID()&nodeLinkPageMask]
}

// storetranslate:pointer-hash 0a6c3cc96a69d318 links.go:nodeLinkStore.Has
func (s *nodeLinkStore[V]) Has(node store.Node) bool {
	return s.TryGet(node) != nil
}

// storetranslate:pointer-hash 84bb3b500a6da07a links.go:nodeLinkStore.TryGet
func (s *nodeLinkStore[V]) TryGet(node store.Node) *V {
	k := node.Key()
	if id := int(k.StoreID()); id < len(s.stores) {
		pages := s.stores[id]
		if i := int(k.ID() >> nodeLinkPageShift); i < len(pages) {
			if page := pages[i]; page != nil {
				return &page[k.ID()&nodeLinkPageMask]
			}
		}
	}
	return nil
}
