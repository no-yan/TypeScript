package checker

import (
	"github.com/microsoft/TypeScript/tsc/internal/ast"
	"github.com/microsoft/TypeScript/tsc/internal/core"
)

type nodeLinkStore[V any] struct {
	store   core.PagedLinkStore[V]
	orphans map[*ast.Node]uint64
	next    uint64
}

func (s *nodeLinkStore[V]) key(node *ast.Node) uint64 {
	file := ast.GetSourceFileOfNode(node)
	if file != nil {
		h := file.HandleOf(node)
		if h.Ref() != 0 {
			if g := h.Global(); g != 0 {
				return uint64(g)
			}
		}
	}
	if s.orphans == nil {
		s.orphans = make(map[*ast.Node]uint64)
		s.next = 1 << 63
	}
	if k, ok := s.orphans[node]; ok {
		return k
	}
	s.next++
	s.orphans[node] = s.next
	return s.next
}

func (s *nodeLinkStore[V]) Get(node *ast.Node) *V {
	return s.store.Get(s.key(node))
}

func (s *nodeLinkStore[V]) Has(node *ast.Node) bool {
	return s.store.Has(s.key(node))
}

func (s *nodeLinkStore[V]) TryGet(node *ast.Node) *V {
	return s.store.TryGet(s.key(node))
}

// symbolArenaLinkStore is a links store keyed by symbol references. Values are stored
// indirectly in an arena which is suitable for values where sizeof(V) is larger.
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
