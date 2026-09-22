//go:build storeexp

package storeexp

import (
	"fmt"
	"unsafe"

	"github.com/microsoft/TypeScript/tsc/internal/ast"
)

type NodeRef uint32 // index into Store.nodes; dense; 0 = nil

type NodeHeader struct { // 24 bytes, no pointers
	kind ast.Kind
	// TODO(storeexp): only the syntactic ModifierFlags, bits 0-15, live here.
	// Bit 16 and up (Deprecated, the JSDoc cache bits 23-27,
	// HasComputedJSDocModifiers, HasComputedFlags) are computed lazily from
	// JSDoc and need a separate path. That path is undesigned; it is decided
	// together with where JSDoc nodes live. See store-ast-design-20260922.md
	// sections 2.1 and 7.
	modifierFlags uint16
	flags         ast.NodeFlags
	pos, end      int32
	parent        NodeRef
	data          uint32 // start of the payload in Store.extra; Identifier: see Text
}

type Store struct {
	nodes []NodeHeader // nodes[0] is a sentinel: kind Unknown, parent 0
	// Child slots and list blocks. extra[0:3] is zero, so list block 0 (the nil
	// list) reads as an empty block without a guard.
	extra []uint32
	src   string
	texts string // texts that are not a substring of src
	file  uint32
}

// Node is the currency for arguments and locals. Store a Ref on the heap.
type Node struct {
	s *Store
	h *NodeHeader
}

type Ref struct {
	file uint32
	id   NodeRef
}

func (s *Store) node(ref NodeRef) Node { return Node{s, &s.nodes[ref]} }

// Root relies on post-order construction: the root is the last node.
func (s *Store) Root() Node { return s.node(NodeRef(len(s.nodes) - 1)) }

func (n Node) Kind() ast.Kind       { return n.h.kind }
func (n Node) Flags() ast.NodeFlags { return n.h.flags }
func (n Node) Pos() int32           { return n.h.pos }
func (n Node) End() int32           { return n.h.end }
func (n Node) Parent() Node         { return n.s.node(n.h.parent) }
func (n Node) IsNil() bool          { return n.h.kind == ast.KindUnknown }
func (n Node) ModifierFlags() ast.ModifierFlags {
	return ast.ModifierFlags(n.h.modifierFlags)
}

// Ref recovers the index from the header's address. Apart from the slice
// conversion in List.Refs, this is the only place that may use unsafe.
func (n Node) Ref() NodeRef {
	base := uintptr(unsafe.Pointer(unsafe.SliceData(n.s.nodes)))
	return NodeRef((uintptr(unsafe.Pointer(n.h)) - base) / unsafe.Sizeof(NodeHeader{}))
}

// textInTexts marks an Identifier whose text is not the tail of its source
// range. Provisional: where this flag lives is an open question in
// store-ast-design-20260922.md section 7.
const textInTexts = 1 << 31

// Text is defined for Identifier only. Other kinds have no text or data word
// in this experiment.
func (n Node) Text() string {
	if storeChecks {
		n.assertKind(ast.KindIdentifier)
	}
	d := n.h.data
	if d&textInTexts != 0 {
		return n.s.escapedText(d &^ textInTexts)
	}
	return n.s.src[d:n.h.end]
}

func (s *Store) escapedText(at uint32) string {
	off, length := s.extra[at], s.extra[at+1]
	return s.texts[off : off+length]
}

func (n Node) assertKind(kind ast.Kind) {
	if n.h.kind != kind {
		panic(fmt.Sprintf("storeexp: %v is not %v", n.h.kind, kind))
	}
}

// List is a view of a block in extra: [len, pos, end, elem0 ... elem(len-1)].
type List struct {
	s  *Store
	at uint32 // index of the block in extra; 0 = nil list
}

func (l List) IsNil() bool { return l.at == 0 }
func (l List) Len() int    { return int(l.s.extra[l.at]) }
func (l List) Pos() int32  { return int32(l.s.extra[l.at+1]) }
func (l List) End() int32  { return int32(l.s.extra[l.at+2]) }

// Refs is a contiguous view of the elements. It does not allocate.
func (l List) Refs() []NodeRef {
	extra := l.s.extra
	elems := extra[l.at+3 : l.at+3+extra[l.at]]
	return unsafe.Slice((*NodeRef)(unsafe.Pointer(unsafe.SliceData(elems))), len(elems))
}

func (l List) At(i int) Node { return l.s.node(l.Refs()[i]) }

// Builder writes by id and never holds a *NodeHeader, because append moves the
// headers. Nodes are handed out only after Finish.
type Builder struct {
	nodes []NodeHeader
	extra []uint32
	texts []byte
	src   string
	file  uint32
}

func NewBuilder(src string, file uint32) *Builder {
	return &Builder{
		nodes: make([]NodeHeader, 1),
		extra: make([]uint32, 3),
		src:   src,
		file:  file,
	}
}

// List returns the index of the new block.
func (b *Builder) List(pos, end int32, elems []NodeRef) uint32 {
	at := uint32(len(b.extra))
	b.extra = append(b.extra, uint32(len(elems)), uint32(pos), uint32(end))
	for _, elem := range elems {
		b.extra = append(b.extra, uint32(elem))
	}
	return at
}

// Node takes one slot per shapes[kind]: a NodeRef for a child slot, a block
// index for a list slot, 0 for an absent one. Children must already exist
// (post-order), so their parent is written here rather than in a later pass.
func (b *Builder) Node(kind ast.Kind, flags ast.NodeFlags, mod ast.ModifierFlags, pos, end int32, slots []uint32) NodeRef {
	sh := shapes[kind&511]
	if len(slots) != int(sh.slots) {
		panic(fmt.Sprintf("storeexp: %v takes %d slots, got %d", kind, sh.slots, len(slots)))
	}
	var data uint32
	if len(slots) != 0 {
		data = uint32(len(b.extra))
		b.extra = append(b.extra, slots...)
	}
	id := b.header(kind, flags, mod, pos, end, data)
	for i, slot := range slots {
		switch {
		case slot == 0:
		case sh.listMask&(1<<i) != 0:
			for _, elem := range b.extra[slot+3 : slot+3+b.extra[slot]] {
				b.nodes[elem].parent = id
			}
		default:
			b.nodes[slot].parent = id
		}
	}
	return id
}

func (b *Builder) Identifier(flags ast.NodeFlags, pos, end int32, text string) NodeRef {
	var data uint32
	if start := int(end) - len(text); start >= 0 && int(end) <= len(b.src) && b.src[start:end] == text {
		data = uint32(start)
	} else {
		data = textInTexts | uint32(len(b.extra))
		b.extra = append(b.extra, uint32(len(b.texts)), uint32(len(text)))
		b.texts = append(b.texts, text...)
	}
	return b.header(ast.KindIdentifier, flags, ast.ModifierFlagsNone, pos, end, data)
}

func (b *Builder) header(kind ast.Kind, flags ast.NodeFlags, mod ast.ModifierFlags, pos, end int32, data uint32) NodeRef {
	id := NodeRef(len(b.nodes))
	b.nodes = append(b.nodes, NodeHeader{
		kind: kind,
		// TODO(storeexp): truncates ModifierFlags to bits 0-15. See the TODO on
		// NodeHeader.modifierFlags for what happens to bit 16 and up.
		modifierFlags: uint16(mod),
		flags:         flags,
		pos:           pos,
		end:           end,
		data:          data,
	})
	return id
}

// Finish copies to the exact size. It does not renumber.
func (b *Builder) Finish() *Store {
	return &Store{
		nodes: append(make([]NodeHeader, 0, len(b.nodes)), b.nodes...),
		extra: append(make([]uint32, 0, len(b.extra)), b.extra...),
		src:   b.src,
		texts: string(b.texts),
		file:  b.file,
	}
}
