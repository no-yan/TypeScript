package checker

import (
	"github.com/microsoft/TypeScript/tsc/internal/ast"
	"github.com/microsoft/TypeScript/tsc/internal/core"
)

type nodeLinkStore[V any] struct {
	store   core.PagedLinkStore[V]
	orphans map[ // symbolArenaLinkStore is a links store keyed by symbol references. Values are stored
	// indirectly in an arena which is suitable for values where sizeof(V) is larger.
	ast.Handle]uint64
	next uint64
}

func (s *nodeLinkStore[V]) key(node ast.Handle) uint64 {
	file := ast.GetSourceFileOfNode(node)
	if file != nil {
		if g := node.Global(); g != 0 {
			return uint64(g)
		}
	}
	if s.orphans == nil {
		s.orphans = make(map[ast.Handle]uint64)
		s.next = 1 << 63
	}
	if k, ok := s.orphans[node]; ok {
		return k
	}
	s.next++
	s.orphans[node] = s.next
	return s.next
}
func (s *nodeLinkStore[V]) Get(node ast.Handle) *V {
	return s.store.Get(s.key(node))
}
func (s *nodeLinkStore[V]) Has(node ast.Handle) bool {
	return s.store.Has(s.key(node))
}
func (s *nodeLinkStore[V]) TryGet(node ast.Handle) *V {
	return s.store.TryGet(s.key(node))
}

type symbolArenaLinkStore[V any] struct {
	store core.PagedLinkStore[*V]
	arena core.Arena[V]
}

func (s *symbolArenaLinkStore[V]) Get(symbol *ast.Symbol) *V {
	link := s.store.Get(uint64(ast.GetSymbolId(symbol)))
	if *link == nil {
		*link = s.arena.New()
	}
	return *link
}
func (s *symbolArenaLinkStore[V]) Has(symbol *ast.Symbol) bool {
	return s.TryGet(symbol) != nil
}
func (s *symbolArenaLinkStore[V]) TryGet(symbol *ast.Symbol) *V {
	if link := s.store.TryGet(uint64(ast.GetSymbolId(symbol))); link != nil {
		return *link
	}
	return nil
}

// globalLinkStore is a node-keyed links store whose map key is the node's
// GlobalRef (StoreID<<32 | NodeRef). A uint64 key takes the runtime's
// mapaccess1_fast64 path; keying by the 16-byte ast.Handle struct goes through
// the generic memhash + type-eq path, which profiled 3x slower on the checker.
// Nodes of an unregistered Store (StoreID 0) fall back to a Handle-keyed map.
type globalLinkStore[V any] struct {
	store   core.LinkStore[ast.GlobalRef, V]
	orphans core.LinkStore[ast.Handle, V]
}

func globalKey(node ast.Handle) ast.GlobalRef {
	if id := node.Store().ID(); id != 0 {
		return ast.MakeGlobalRef(id, node.Ref())
	}
	return 0
}

func (s *globalLinkStore[V]) Get(node ast.Handle) *V {
	if key := globalKey(node); key != 0 {
		return s.store.Get(key)
	}
	return s.orphans.Get(node)
}

func (s *globalLinkStore[V]) Has(node ast.Handle) bool {
	if key := globalKey(node); key != 0 {
		return s.store.Has(key)
	}
	return s.orphans.Has(node)
}

func (s *globalLinkStore[V]) TryGet(node ast.Handle) *V {
	if key := globalKey(node); key != 0 {
		return s.store.TryGet(key)
	}
	return s.orphans.TryGet(node)
}
