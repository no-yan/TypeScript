package store

import "github.com/microsoft/TypeScript/tsc/internal/ast"

// Internals that the external tests read.

func (s *Store) NodeAt(ref NodeRef) Node { return s.node(ref) }

// Footprint is the size of each column in bytes.
func (s *Store) Footprint() (nodes, extra, texts int) {
	return len(s.nodes) * 24, len(s.extra) * 4, len(s.texts)
}

func (s *Store) NodeCount() int { return len(s.nodes) - 1 }

// IdentifierTextInTexts reports whether the text of an Identifier lives in
// texts rather than in the source.
func IdentifierTextInTexts(n Node) bool { return n.h.data&textInTexts != 0 }

func Shape(kind ast.Kind) (words uint8, listMask uint16) {
	sh := shapes[kind&511]
	return sh.slots, sh.listMask
}
