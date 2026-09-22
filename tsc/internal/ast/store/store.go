package store

import (
	"fmt"
	"strings"
	"unsafe"

	"github.com/microsoft/TypeScript/tsc/internal/ast"
)

type NodeRef uint32 // index into Store.nodes; dense

// NoNodeRef is the nil node: nodes[0] is a sentinel row of kind Unknown, so
// accessors read it without a guard.
const NoNodeRef NodeRef = 0

type ListRef uint32 // index of a list block in Store.extra

// NoListRef is the nil list: extra[0:3] is a sentinel block of length 0.
const NoListRef ListRef = 0

type NodeHeader struct { // 24 bytes, no pointers
	kind ast.Kind
	// TODO(store): only the syntactic ModifierFlags, bits 0-15, live here.
	// Bit 16 and up (Deprecated, the JSDoc cache bits 23-27,
	// HasComputedJSDocModifiers, HasComputedFlags) are computed lazily from
	// JSDoc and need a separate path. That path is undesigned; it is decided
	// together with where JSDoc nodes live. See store-ast-design-20260922.md
	// sections 2.1 and 7.
	modifierFlags uint16
	flags         ast.NodeFlags
	pos, end      int32
	parent        NodeRef
	data          uint32 // start of the payload in Store.extra; Identifier: see identifierText
}

type Store struct {
	nodes []NodeHeader // nodes[0] is a sentinel: kind Unknown, parent 0
	// Payload words and list blocks. extra[0:3] is zero, so list block 0 (the
	// nil list) reads as an empty block without a guard.
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

// textInTexts marks a text that is not a substring of the source: in the data
// word of an Identifier, and in the offset word of any other text member.
// Provisional: where this flag lives is an open question in
// store-ast-design-20260922.md section 7.
const textInTexts = 1 << 31

// identifierText is the text of an Identifier or PrivateIdentifier: the
// source from data to end, or, with textInTexts set, the [offset, length] pair
// in extra at the remaining bits.
func (n Node) identifierText() string {
	d := n.h.data
	if d&textInTexts != 0 {
		return n.s.text(int(d &^ textInTexts))
	}
	return n.s.src[d:n.h.end]
}

// text reads the [offset, length] pair at extra[at]. The offset's top bit
// selects texts over src.
func (s *Store) text(at int) string {
	off, length := s.extra[at], s.extra[at+1]
	if off&textInTexts != 0 {
		off &^= textInTexts
		return s.texts[off : off+length]
	}
	return s.src[off : off+length]
}

func (n Node) assertKind(kind ast.Kind) {
	if n.h.kind != kind {
		panic(fmt.Sprintf("store: %v is not %v", n.h.kind, kind))
	}
}

func (n Node) assertKinds(view string, kinds ...ast.Kind) {
	for _, kind := range kinds {
		if n.h.kind == kind {
			return
		}
	}
	panic(fmt.Sprintf("store: %v is not a %s", n.h.kind, view))
}

// List is a view of a block in extra: [len, pos, end, elem0 ... elem(len-1)].
type List struct {
	s  *Store
	at ListRef // NoListRef = nil list
}

func (l List) IsNil() bool  { return l.at == NoListRef }
func (l List) Ref() ListRef { return l.at }
func (l List) Len() int     { return int(l.s.extra[l.at]) }
func (l List) Pos() int32   { return int32(l.s.extra[l.at+1]) }
func (l List) End() int32   { return int32(l.s.extra[l.at+2]) }

// Refs is a contiguous view of the elements. It does not allocate.
func (l List) Refs() []NodeRef {
	extra := l.s.extra
	elems := extra[l.at+3 : l.at+3+ListRef(extra[l.at])]
	return unsafe.Slice((*NodeRef)(unsafe.Pointer(unsafe.SliceData(elems))), len(elems))
}

func (l List) At(i int) Node { return l.s.node(l.Refs()[i]) }

// Visitor returns true to stop the walk, like ast.Visitor.
type Visitor func(Node) bool

// Callers skip the nil list (at == NoListRef) before calling: 60% of the list slots in
// a walk are nil, and Refs() costs 25 instructions (store-ast-design-20260922.md
// 2.3). The check lives at the call site so that visitList stays within the
// inline budget (cost 80).
func visitList(s *Store, at ListRef, v Visitor) bool {
	for _, ref := range (List{s, at}).Refs() {
		if v(s.node(ref)) {
			return true
		}
	}
	return false
}

// Builder writes by id and never holds a *NodeHeader, because append moves the
// headers. Nodes are handed out after Finish; View hands one out during
// construction, valid only until the next append.
type Builder struct {
	nodes []NodeHeader
	extra []uint32
	texts strings.Builder // String() is zero-copy, so View can read it
	src   string
	file  uint32
	view  Store // what View returns; its columns are re-synced on every call
}

func NewBuilder(src string, file uint32) *Builder {
	b := &Builder{}
	b.Reset(src, file)
	return b
}

// Reset starts a file on the same scratch: the sentinel rows are rewritten,
// the capacity is kept, texts is emptied.
func (b *Builder) Reset(src string, file uint32) {
	if cap(b.nodes) == 0 {
		b.nodes = make([]NodeHeader, 1, 1024)
		b.extra = make([]uint32, 3, 1024)
	} else {
		b.nodes = b.nodes[:1]
		b.extra = b.extra[:3]
		b.nodes[0] = NodeHeader{}
		clear(b.extra)
	}
	b.texts.Reset()
	b.src = src
	b.file = file
}

// View is the Node of id in the scratch. It points into nodes, so it is valid
// only until the next constructor, List or NewXxx call.
func (b *Builder) View(id NodeRef) Node {
	b.view = Store{nodes: b.nodes, extra: b.extra, src: b.src, texts: b.texts.String(), file: b.file}
	return b.view.node(id)
}

// ViewList is View for a list block.
func (b *Builder) ViewList(at ListRef) List {
	b.view = Store{nodes: b.nodes, extra: b.extra, src: b.src, texts: b.texts.String(), file: b.file}
	return List{&b.view, at}
}

func (b *Builder) AddFlags(id NodeRef, flags ast.NodeFlags) { b.nodes[id].flags |= flags }
func (b *Builder) SetFlags(id NodeRef, flags ast.NodeFlags) { b.nodes[id].flags = flags }
func (b *Builder) SetLoc(id NodeRef, pos, end int32)        { b.nodes[id].pos, b.nodes[id].end = pos, end }

// Mark and Truncate are the speculation pair: Truncate drops every node and
// payload word made since Mark. texts is not truncated (strings.Builder has no
// truncate), so a text made during a rewound speculation stays as dead bytes.
func (b *Builder) Mark() (nodes, extra int)  { return len(b.nodes), len(b.extra) }
func (b *Builder) Truncate(nodes, extra int) { b.nodes, b.extra = b.nodes[:nodes], b.extra[:extra] }

// Len is the number of rows including the sentinel and any dead node.
func (b *Builder) Len() int { return len(b.nodes) }

// List returns the index of the new block.
func (b *Builder) List(pos, end int32, elems []NodeRef) ListRef {
	at := ListRef(len(b.extra))
	b.extra = append(b.extra, uint32(len(elems)), uint32(pos), uint32(end))
	for _, elem := range elems {
		b.extra = append(b.extra, uint32(elem))
	}
	return at
}

// NewToken is the constructor of every kind without a payload: tokens,
// keywords and the definitions without members.
func (b *Builder) NewToken(kind ast.Kind, flags ast.NodeFlags, pos, end int32) NodeRef {
	return b.header(kind, flags, 0, pos, end, 0)
}

func (b *Builder) header(kind ast.Kind, flags ast.NodeFlags, mod ast.ModifierFlags, pos, end int32, data uint32) NodeRef {
	id := NodeRef(len(b.nodes))
	b.nodes = append(b.nodes, NodeHeader{
		kind: kind,
		// TODO(store): truncates ModifierFlags to bits 0-15. See the TODO on
		// NodeHeader.modifierFlags for what happens to bit 16 and up.
		modifierFlags: uint16(mod),
		flags:         flags,
		pos:           pos,
		end:           end,
		data:          data,
	})
	return id
}

// identifierData is the data word of an Identifier: the source offset of the
// text, or textInTexts and the index of its [offset, length] pair in extra.
func (b *Builder) identifierData(end int32, text string) uint32 {
	if start := int(end) - len(text); start >= 0 && int(end) <= len(b.src) && b.src[start:end] == text {
		return uint32(start)
	}
	at := uint32(len(b.extra))
	off, length := b.textWords(end, text)
	b.extra = append(b.extra, off, length)
	return textInTexts | at
}

// textWords is the [offset, length] pair of a text member: the source offset
// when the text ends at end, else the offset in texts with textInTexts set.
func (b *Builder) textWords(end int32, text string) (off, length uint32) {
	if start := int(end) - len(text); start >= 0 && int(end) <= len(b.src) && b.src[start:end] == text {
		return uint32(start), uint32(len(text))
	}
	off = textInTexts | uint32(b.texts.Len())
	b.texts.WriteString(text)
	return off, uint32(len(text))
}

func boolWord(v bool) uint32 {
	if v {
		return 1
	}
	return 0
}

func (b *Builder) adopt(child, parent NodeRef) {
	if child != NoNodeRef {
		b.nodes[child].parent = parent
	}
}

func (b *Builder) adoptList(at ListRef, parent NodeRef) {
	for _, elem := range b.extra[at+3 : at+3+ListRef(b.extra[at])] {
		b.nodes[elem].parent = parent
	}
}

// modifierFlags folds the kinds of the elements of a modifier block, like
// ast.ModifiersToFlags. Block 0 is empty.
func (b *Builder) modifierFlags(at ListRef) ast.ModifierFlags {
	var flags ast.ModifierFlags
	for _, elem := range b.extra[at+3 : at+3+ListRef(b.extra[at])] {
		flags |= ast.ModifierToFlag(b.nodes[elem].kind)
	}
	return flags
}

// Finish copies to the exact size (Compact). It does not renumber. The
// scratch may be reused for the next file afterwards; texts is cloned so that
// the Store never aliases the builder (store-ast-design-20260922.md 2.4).
func (b *Builder) Finish() *Store {
	return &Store{
		nodes: append(make([]NodeHeader, 0, len(b.nodes)), b.nodes...),
		extra: append(make([]uint32, 0, len(b.extra)), b.extra...),
		src:   b.src,
		texts: strings.Clone(b.texts.String()),
		file:  b.file,
	}
}
