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
// Since aa8149273b the Pointer's store keys a node by an id its checker deals
// in blocks, to pack the links densely, and keeps the values in an arena, so
// that TryGet answers nil for a key that Get has not made. A NodeKey is dense
// already; the pages keep the values, and TryGet answers a zero value for
// such a key on a page that exists, as the Pointer's did before. The one node
// link store, symbolNodeLinks, is read by TryGet only for its resolvedSymbol,
// nil either way (upstream-sync-20261021.md S6, the user's decision).
//
// storetranslate:pointer-hash 2455b06c970c36ef links.go:nodeLinkStore
type nodeLinkStore[V any] struct {
	stores [][]*[nodeLinkPageSize]V // [StoreID][NodeRef>>nodeLinkPageShift]
}

// Get repeats the lookup of TryGet rather than calling it: TryGet is over the
// inline budget, and Get is the hot one.
//
// storetranslate:pointer-hash b1d86f5d08f764b6 links.go:nodeLinkStore.Get
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
// PagedLinkStore.Get did before aa8149273b.
//
// storetranslate:store-only
// storetranslate:pointer-hash b1d86f5d08f764b6 links.go:nodeLinkStore.Get
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

// storetranslate:pointer-hash 81a01ee89bd7af2e links.go:nodeLinkStore.Has
func (s *nodeLinkStore[V]) Has(node store.Node) bool {
	return s.TryGet(node) != nil
}

// storetranslate:pointer-hash 81e7a121203b3ba6 links.go:nodeLinkStore.TryGet
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
