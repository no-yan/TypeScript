package ast

import (
	"unsafe"

	"github.com/microsoft/TypeScript/tsc/internal/core"
)

// NodeRef is a Store index. 0 is missing. It is not ast.NodeId.
type NodeRef uint32

// ListRef indexes a packed node list in Store. 0 is missing.
type ListRef uint32

// Handle is a stack value. Heap-resident structures should hold NodeRef and
// rebuild the Handle via Store.At; a stored Handle carries *Store, which puts
// a pointer word in every element and forces the GC to scan the container.
type Handle struct {
	s  *Store
	id NodeRef
}

// StoreVisitor returns true to stop walking, matching Visitor on *Node.
type StoreVisitor func(Handle) bool

// nodeHeader packs every per-node scalar into one pointer-free row so
// []nodeHeader stays noscan while a multi-field visit still hits one
// cache line. Children live in a separate packed []NodeRef column.
type nodeHeader struct {
	kind       Kind
	flags      NodeFlags
	pos        int32
	end        int32
	parent     NodeRef
	childStart uint32
	childLen   uint32
	identText  uint32
}

type listHeader struct {
	pos   int32
	end   int32
	start uint32
	len   uint32
}

// Store owns the long-lived tree. After Seal, node/list/intern backing
// arrays are pointer-free (noscan). Sparse side maps (symbols) remain
// scannable on purpose: only declaration nodes use them.
type Store struct {
	nodes     []nodeHeader
	lists     []listHeader
	children  []NodeRef
	internBuf []byte
	internOff []uint32 // intern id i occupies internBuf[internOff[i]:internOff[i+1]]
	internIdx map[string]uint32
	symbols   map[NodeRef]*Symbol
}

func NewStore(hint int) *Store {
	if hint < 1 {
		hint = 1
	}
	return &Store{
		nodes:     make([]nodeHeader, 1, hint+1),
		lists:     make([]listHeader, 1, 8),
		internOff: []uint32{0, 0},
		internIdx: make(map[string]uint32),
	}
}

func (s *Store) Alloc(kind Kind, flags NodeFlags, loc core.TextRange, childLen int) Handle {
	if s == nil {
		panic("ast: Alloc on nil Store")
	}
	if childLen < 0 {
		panic("ast: negative childLen")
	}
	if len(s.nodes) >= int(^NodeRef(0)) {
		panic("ast: Store exhausted")
	}
	id := NodeRef(len(s.nodes))
	start := uint32(len(s.children))
	if childLen > 0 {
		s.children = append(s.children, make([]NodeRef, childLen)...)
	}
	s.nodes = append(s.nodes, nodeHeader{
		kind:       kind,
		flags:      flags,
		pos:        int32(loc.Pos()),
		end:        int32(loc.End()),
		childStart: start,
		childLen:   uint32(childLen),
	})
	return Handle{s: s, id: id}
}

func (s *Store) AllocList(loc core.TextRange, n int) ListRef {
	if s == nil {
		panic("ast: AllocList on nil Store")
	}
	if n < 0 {
		panic("ast: negative list length")
	}
	if len(s.lists) >= int(^ListRef(0)) {
		panic("ast: Store lists exhausted")
	}
	id := ListRef(len(s.lists))
	start := uint32(len(s.children))
	if n > 0 {
		s.children = append(s.children, make([]NodeRef, n)...)
	}
	s.lists = append(s.lists, listHeader{
		pos:   int32(loc.Pos()),
		end:   int32(loc.End()),
		start: start,
		len:   uint32(n),
	})
	return id
}

func (s *Store) Intern(text string) uint32 {
	if text == "" {
		return 0
	}
	if id, ok := s.internIdx[text]; ok {
		return id
	}
	if s.internIdx == nil {
		panic("ast: Intern after Seal")
	}
	id := uint32(len(s.internOff) - 1)
	s.internBuf = append(s.internBuf, text...)
	s.internOff = append(s.internOff, uint32(len(s.internBuf)))
	s.internIdx[text] = id
	return id
}

// Seal drops the construction-time intern index. Call once no new text
// will be interned. Side maps are kept.
func (s *Store) Seal() {
	s.internIdx = nil
}

func (s *Store) Len() int {
	if s == nil || len(s.nodes) == 0 {
		return 0
	}
	return len(s.nodes) - 1
}

func (s *Store) At(ref NodeRef) Handle {
	return Handle{s: s, id: ref}
}

func (s *Store) ListLen(list ListRef) int {
	if list == 0 || s == nil {
		return 0
	}
	return int(s.lists[list].len)
}

func (s *Store) ListLoc(list ListRef) core.TextRange {
	if list == 0 || s == nil {
		return core.UndefinedTextRange()
	}
	l := &s.lists[list]
	return core.NewTextRange(int(l.pos), int(l.end))
}

func (s *Store) ListAt(list ListRef, i int) Handle {
	if list == 0 || s == nil {
		return Handle{}
	}
	l := &s.lists[list]
	if i < 0 || i >= int(l.len) {
		panic("ast: list index out of range")
	}
	return Handle{s: s, id: s.children[int(l.start)+i]}
}

func (s *Store) SetListAt(list ListRef, i int, h Handle) {
	if list == 0 || s == nil {
		panic("ast: SetListAt on missing list")
	}
	l := &s.lists[list]
	if i < 0 || i >= int(l.len) {
		panic("ast: list index out of range")
	}
	ref := NodeRef(0)
	if h.id != 0 {
		if h.s != s {
			panic("ast: Handle from a different Store")
		}
		ref = h.id
	}
	s.children[int(l.start)+i] = ref
}

func (s *Store) SetSymbol(ref NodeRef, sym *Symbol) {
	if s == nil || ref == 0 {
		return
	}
	if sym == nil {
		delete(s.symbols, ref)
		return
	}
	if s.symbols == nil {
		s.symbols = make(map[NodeRef]*Symbol)
	}
	s.symbols[ref] = sym
}

func (s *Store) Symbol(ref NodeRef) *Symbol {
	if s == nil || ref == 0 {
		return nil
	}
	return s.symbols[ref]
}

func (s *Store) internText(id uint32) string {
	start, end := s.internOff[id], s.internOff[id+1]
	if start == end {
		return ""
	}
	return unsafe.String(&s.internBuf[start], int(end-start))
}

func (h Handle) Ref() NodeRef { return h.id }

func (h Handle) Store() *Store { return h.s }

func (h Handle) Kind() Kind {
	h.mustLive()
	return h.s.nodes[h.id].kind
}

func (h Handle) Flags() NodeFlags {
	h.mustLive()
	return h.s.nodes[h.id].flags
}

func (h Handle) SetFlags(flags NodeFlags) {
	h.mustLive()
	h.s.nodes[h.id].flags = flags
}

func (h Handle) Loc() core.TextRange {
	h.mustLive()
	n := &h.s.nodes[h.id]
	return core.NewTextRange(int(n.pos), int(n.end))
}

func (h Handle) SetLoc(loc core.TextRange) {
	h.mustLive()
	n := &h.s.nodes[h.id]
	n.pos = int32(loc.Pos())
	n.end = int32(loc.End())
}

func (h Handle) Parent() Handle {
	if h.id == 0 || h.s == nil {
		return Handle{}
	}
	return Handle{s: h.s, id: h.s.nodes[h.id].parent}
}

func (h Handle) SetParent(p Handle) {
	h.mustLive()
	h.s.nodes[h.id].parent = h.refInStore(p)
}

func (h Handle) NumChildren() int {
	if h.id == 0 || h.s == nil {
		return 0
	}
	return int(h.s.nodes[h.id].childLen)
}

func (h Handle) Child(i int) Handle {
	h.mustLive()
	n := &h.s.nodes[h.id]
	if i < 0 || i >= int(n.childLen) {
		panic("ast: child index out of range")
	}
	return Handle{s: h.s, id: h.s.children[int(n.childStart)+i]}
}

func (h Handle) SetChild(i int, c Handle) {
	h.mustLive()
	n := &h.s.nodes[h.id]
	if i < 0 || i >= int(n.childLen) {
		panic("ast: child index out of range")
	}
	h.s.children[int(n.childStart)+i] = h.refInStore(c)
}

func (h Handle) SetIdent(internID uint32) {
	h.mustLive()
	if internID >= uint32(len(h.s.internOff)-1) {
		panic("ast: intern id out of range")
	}
	h.s.nodes[h.id].identText = internID
}

func (h Handle) Ident() string {
	h.mustLive()
	return h.s.internText(h.s.nodes[h.id].identText)
}

// Text is the identifier/literal text when present.
func (h Handle) Text() string { return h.Ident() }

func (h Handle) Symbol() *Symbol {
	if h.id == 0 || h.s == nil {
		return nil
	}
	return h.s.Symbol(h.id)
}

func (h Handle) SetSymbol(sym *Symbol) {
	h.mustLive()
	h.s.SetSymbol(h.id, sym)
}

// ForEachChild visits non-zero children in slot order. true stops.
func (h Handle) ForEachChild(v StoreVisitor) bool {
	if h.id == 0 || h.s == nil {
		return false
	}
	n := &h.s.nodes[h.id]
	for i := range int(n.childLen) {
		ref := h.s.children[int(n.childStart)+i]
		if ref == 0 {
			continue
		}
		if v(Handle{s: h.s, id: ref}) {
			return true
		}
	}
	return false
}

// Walk pre-order visits h then descendants. true stops.
func Walk(h Handle, v StoreVisitor) bool {
	if h.Ref() == 0 {
		return false
	}
	if v(h) {
		return true
	}
	return h.ForEachChild(func(c Handle) bool {
		return Walk(c, v)
	})
}

// SetParentsInChildren assigns Parent on every reachable child, like
// the pointer AST's post-parse parent pass.
func (h Handle) SetParentsInChildren() {
	if h.Ref() == 0 {
		return
	}
	h.ForEachChild(func(c Handle) bool {
		c.SetParent(h)
		c.SetParentsInChildren()
		return false
	})
}

func (h Handle) mustLive() {
	if h.s == nil || h.id == 0 || int(h.id) >= len(h.s.nodes) {
		panic("ast: invalid Handle")
	}
}

func (h Handle) refInStore(other Handle) NodeRef {
	if other.id == 0 {
		return 0
	}
	if other.s != h.s {
		panic("ast: Handle from a different Store")
	}
	return other.id
}
