package store

import "github.com/microsoft/TypeScript/tsc/internal/ast"

// What the tests and benchmarks of this package and of storeparser measure.
// Nothing else reads these.

// Footprint is the size of each column in bytes.
func (s *Store) Footprint() (nodes, extra, texts int) {
	return len(s.nodes) * 32, len(s.extra) * 4, len(s.texts)
}

// NodeCount is the number of nodes, dead nodes included.
func (s *Store) NodeCount() int { return len(s.nodes) - 1 }

// IdentifierTextInTexts reports whether the text of an Identifier lives in
// texts rather than in the source.
func IdentifierTextInTexts(n Node) bool { return n.h.data&textInTexts != 0 }

func Shape(kind ast.Kind) (words uint8, listMask uint16) {
	sh := shapes[kind&511]
	return sh.slots, sh.listMask
}
