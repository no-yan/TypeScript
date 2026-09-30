package store

import (
	"fmt"
	"strings"
	"unsafe"

	"github.com/microsoft/TypeScript/tsc/internal/ast"
	"github.com/microsoft/TypeScript/tsc/internal/core"
)

// The node, the list and their edges. The ids and owners are
// internal/ast/docs/store-checker-design-20260923.md 2.3 and 2.4 (Y3, N1',
// E1, F1), the foreign edges 2.6 (FR1, K3), the zero value
// store-node-zero-design-20260926.md (Z1).

type NodeRef uint32 // index into Store.nodes; dense

// NoNodeRef is the nil node: nodes[0] is a sentinel row of kind Unknown, so
// accessors read it without a guard.
const NoNodeRef NodeRef = 0

type ListRef uint32 // index of a list block in Store.extra

// NoListRef is the nil list: extra[0:3] is a sentinel block of length 0.
const NoListRef ListRef = 0

// foreignBit marks an edge (a child or list slot in extra, a header's parent,
// a list element) that is not a row of the Store it is read in but the index
// of an entry of the Store's foreignNodes or foreignLists. Only a synthetic
// Store (Factory) and a lazy JSDoc Store have foreign entries; the edges of a
// source Store are all local (invariants 2.8.3 and 16 of the 7d design).
const foreignBit = 1 << 31

type NodeHeader struct { // 32 bytes, no pointers
	kind ast.Kind
	// The syntactic ModifierFlags, bits 0-15. The Go Pointer AST never sets
	// bit 16 and up (Deprecated, the JSDoc bits, HasComputed*): only the enum
	// export of the API and a local of the LS use them, never a node. So 16
	// bits hold the same value as the Pointer's
	// (store-checker-7d2-design-remainder-20260929.md 2.1).
	modifierFlags uint16
	flags         ast.NodeFlags
	pos, end      int32
	parent        NodeRef // an edge: local, or foreignBit and a foreignNodes index
	data          uint32  // start of the payload in Store.extra; Identifier: see identifierText
	// flow and symbol: in a source Store the FlowRef and SymbolId of its
	// Bound, written by the binder; in a synthetic Store a slot of its private
	// tables, written by the Factory (V1). 0 = nil. Every kind.
	flow   FlowRef
	symbol SymbolId
}

// StoreID names a Store within a program: the source Stores are 0..nFiles-1,
// given by the program; every other Store (lazy JSDoc, synthetic) takes the
// next number of the program's IDSpace. A number is never reused (Y3).
type StoreID uint32

type Store struct {
	nodes []NodeHeader // nodes[0] is a sentinel: kind Unknown, parent 0
	// Payload words and list blocks. extra[0:3] is zero, so list block 0 (the
	// nil list) reads as an empty block without a guard.
	extra []uint32
	src   string
	texts string // texts that are not a substring of src
	id    StoreID
	file  *File  // a source or lazy JSDoc Store; nil in a synthetic Store
	bound *Bound // a source Store once its bind has started; nil otherwise
	// The canonical Node and List of each foreign edge, by index. Only a
	// synthetic or lazy JSDoc Store has any; an entry never refers to another
	// entry, and entries are not deduplicated.
	foreignNodes []Node
	foreignLists []List
	private      *privateTables // a synthetic Store's flows, symbols and locals (factory.go)
	// sealed is set by Seal once the binder is done. The binder's write API
	// panics after it in a storeChecks build; the Factory's writers check it
	// in every build.
	sealed bool
	owner  *regionToken // the region of a synthetic Store; nil otherwise (factory.go)
	space  *IDSpace     // the program; nil for a Store made outside one
}

// Node is the currency for arguments, locals and the fields of the checker's
// structs (N1'). A map key or a link is its NodeKey, never the Node: Node is
// not comparable (Z1), so == and map[Node] do not compile. Compare with
// SameNode.
//
// The zero Node and the sentinel row of any Store are the same nil: IsNil,
// SameNode, Key and File treat them alike. Reading the contents of a nil node
// (Kind, Parent, a member) is outside the contract, as it is for a nil
// *ast.Node.
type Node struct {
	_ [0]func() // not comparable; first, so that it adds no padding
	s *Store
	h *NodeHeader
}

// NodeKey is a node's identity within a program: the key of a map or a link
// (Y3, L1). There is no way back from a NodeKey to a Node; a cache that reads
// the node later holds the Node.
type NodeKey struct {
	store StoreID
	id    NodeRef
}

func (k NodeKey) StoreID() StoreID { return k.store }
func (k NodeKey) ID() NodeRef      { return k.id }

func (s *Store) ID() StoreID { return s.id }

// node resolves an edge. The foreign side is written out here rather than
// called: as a call, Parent() went over the inline budget (7d design 2.6).
// An out-of-range value without foreignBit panics in every build, so that a
// corrupt small value cannot silently name a foreign entry.
func (s *Store) node(e NodeRef) Node {
	if i := int(e); i < len(s.nodes) {
		return Node{s: s, h: &s.nodes[i]}
	}
	if e&foreignBit == 0 {
		panic("store: invalid node edge")
	}
	return s.foreignNodes[e&^foreignBit]
}

// list resolves a list edge (a list slot) to the canonical List: the elements
// are read in the list's owner, not in the Store of the node that holds the
// slot.
func (s *Store) list(e ListRef) List {
	if int(e) < len(s.extra) {
		return List{s, e}
	}
	if e&foreignBit == 0 {
		panic("store: invalid list edge")
	}
	return s.foreignLists[e&^foreignBit]
}

// Node resolves a NodeRef: the elements of a raw List and the rows of a
// Bound's NodeRefs.
func (s *Store) Node(ref NodeRef) Node { return s.node(ref) }

// Seal ends the writes of the binder. A storeChecks build panics on any write
// API call of the binder after it; a Factory writer refuses it in any build.
func (s *Store) Seal() { s.sealed = true }

// Root relies on post-order construction: the root is the last node.
func (s *Store) Root() Node { return s.node(NodeRef(len(s.nodes) - 1)) }

func (n Node) Kind() ast.Kind       { return n.h.kind }
func (n Node) Flags() ast.NodeFlags { return n.h.flags }
func (n Node) Pos() int32           { return n.h.pos }
func (n Node) End() int32           { return n.h.end }
func (n Node) Loc() core.TextRange {
	return core.NewTextRange(int(n.h.pos), int(n.h.end))
}
func (n Node) Parent() Node { return n.s.node(n.h.parent) }

// IsNil is true for the zero Node and for the sentinel of every Store. Kind
// Unknown is the sentinel's alone: no API makes a real row of kind Unknown.
func (n Node) IsNil() bool { return n.h == nil || n.h.kind == ast.KindUnknown }

func (n Node) ModifierFlags() ast.ModifierFlags {
	return ast.ModifierFlags(n.h.modifierFlags)
}

// SameNode is == of two *ast.Node (E1). A live header belongs to one Store,
// and a foreign edge resolves to the canonical Node, so equal headers are the
// same node; two nils are the same whatever Store they come from.
func SameNode(a, b Node) bool { return a.h == b.h || a.IsNil() && b.IsNil() }

// Key is the node's NodeKey; every nil is NodeKey{}.
func (n Node) Key() NodeKey {
	if n.IsNil() {
		return NodeKey{}
	}
	return NodeKey{n.s.id, n.Ref()}
}

// File is ast.GetSourceFileOfNode (F1, invariant 25). A source or lazy JSDoc
// Store knows its file; a synthetic node walks its parents to one, or to nil.
// It never guesses from the file being checked or from an Original.
func (n Node) File() *File {
	if n.IsNil() {
		return nil
	}
	if f := n.s.file; f != nil {
		return f
	}
	return n.parentFile()
}

func (n Node) parentFile() *File {
	for {
		n = n.Parent()
		if n.IsNil() {
			return nil
		}
		if f := n.s.file; f != nil {
			return f
		}
	}
}

// PropertyNameOrName is (*ast.Node).PropertyNameOrName.
func (n Node) PropertyNameOrName() Node {
	name := n.PropertyName()
	if name.IsNil() {
		name = n.Name()
	}
	return name
}

// The id layer of what the binder writes in the header (A1): the raw words.
// The binder writes and reads these; everything else reads the resolving
// layer (Symbol, FlowNode, bound.go), which knows the owner.

func (n Node) FlowRef() FlowRef   { return n.h.flow }
func (n Node) SymbolId() SymbolId { return n.h.symbol }

// The write API of the binder. None writes the sentinel row, in any build
// (Z1); each panics in a storeChecks build once the Store is sealed or when
// it is a synthetic Store, whose writer is the Factory.
func (n Node) AddFlags(f ast.NodeFlags)   { n.checkBinderWrite(); n.h.flags |= f }
func (n Node) ClearFlags(f ast.NodeFlags) { n.checkBinderWrite(); n.h.flags &^= f }
func (n Node) SetFlowRef(f FlowRef)       { n.checkBinderWrite(); n.h.flow = f }
func (n Node) SetSymbolId(id SymbolId)    { n.checkBinderWrite(); n.h.symbol = id }

func (n Node) checkBinderWrite() {
	if n.h.kind == ast.KindUnknown {
		panic("store: write to the nil node")
	}
	n.checkUnsealed()
}

// checkUnsealed is the storeChecks guard of the binder's writes. The
// generated slot setters call it; they cannot write the sentinel, whose kind
// has no slot.
func (n Node) checkUnsealed() {
	if storeChecks && (n.s.sealed || n.s.owner != nil) {
		panic("store: binder write after Seal or to a synthetic Store")
	}
}

// Ref recovers the index from the header's address. Apart from the element
// slice of a List (List.Refs, List.edges), this is the only place that may
// use unsafe.
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

// Text is (*ast.Node).Text: the header for an identifier, the text slot of the
// kinds that have one (textSlot, generated), and the composed text of
// MetaProperty and JsxNamespacedName. Other kinds have no text.
//
// TODO(store): Text does not inline (cost over the budget of 80), so every
// call pays a call and its prologue, and the Identifier path, which hot paths
// hit repeatedly, cannot be folded into the caller. Left as is for now
// (2026-09-22); a hot call site with a known kind uses the typed
// Identifier.Text, which inlines. Measure once the Store is wired in, then
// decide whether to split it into an inline Identifier path and a slow path.
func (n Node) Text() string {
	switch n.h.kind {
	case ast.KindIdentifier, ast.KindPrivateIdentifier:
		return n.identifierText()
	case ast.KindMetaProperty:
		return n.AsMetaProperty().Name().Text()
	case ast.KindJsxNamespacedName:
		ns := n.AsJsxNamespacedName()
		return ns.Namespace().Text() + ":" + ns.Name().Text()
	}
	slot := textSlot[n.h.kind&511]
	if slot == 0xFF {
		return ""
	}
	return n.s.text(int(n.h.data) + int(slot))
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

// List is a view of a block in its owner's extra: [len, pos, end, elem0 ...
// elem(len-1)]. A List is always canonical: s is the Store that holds the
// block, and each element is an edge of s.
type List struct {
	s  *Store
	at ListRef // NoListRef = nil list
}

// IsNil is true for the zero List and for block 0 of any Store.
func (l List) IsNil() bool  { return l.at == NoListRef }
func (l List) Ref() ListRef { return l.at }
func (l List) Len() int     { return int(l.s.extra[l.at]) }
func (l List) Pos() int32   { return int32(l.s.extra[l.at+1]) }
func (l List) End() int32   { return int32(l.s.extra[l.at+2]) }

// SameList is == of two *ast.NodeList: the same block of the same owner, or
// two nils.
func SameList(a, b List) bool {
	return a.s == b.s && a.at == b.at || a.IsNil() && b.IsNil()
}

// Refs is a contiguous view of the elements as NodeRefs. It does not
// allocate. Only the parse and bind of a source Store may use it: the
// elements of a list with foreign edges are not NodeRefs of anything, and a
// storeChecks build panics on such a Store. Everything else reads Len and At.
func (l List) Refs() []NodeRef {
	if storeChecks && l.s.hasForeign() {
		panic("store: Refs on a Store with foreign edges")
	}
	return l.edges()
}

// edges is the raw element slice: each value is an edge of l.s.
func (l List) edges() []NodeRef {
	extra := l.s.extra
	elems := extra[l.at+3 : l.at+3+ListRef(extra[l.at])]
	return unsafe.Slice((*NodeRef)(unsafe.Pointer(unsafe.SliceData(elems))), len(elems))
}

// At resolves the element in the list's owner.
func (l List) At(i int) Node { return l.s.node(l.edges()[i]) }

func (s *Store) hasForeign() bool {
	return s.owner != nil || s.foreignNodes != nil || s.foreignLists != nil
}

// Visitor returns true to stop the walk, like ast.Visitor.
type Visitor func(Node) bool

// Builder writes by id and never holds a *NodeHeader, because append moves the
// headers. The columns under construction are s, the Store that View reads;
// nothing is copied or re-synced on a View. Builder is used only by pointer:
// Node values from View point into s. (strings.Builder panics on a copy after
// the first write, so a copied Builder fails at the first text.) A Builder
// makes a source Store: every edge it writes is local. space and id are the
// program and the file's StoreID (0..nFiles-1); a nil space makes a Store
// outside any program, which no Factory takes.
type Builder struct {
	s       Store           // nodes, extra, src, texts, id, space: the columns under construction
	textBuf strings.Builder // s.texts is textBuf.String(), re-set after every write (textWords)
}

func NewBuilder(src string, space *IDSpace, id StoreID) *Builder {
	b := &Builder{}
	b.Reset(src, space, id)
	return b
}

// Reset starts a file on the same scratch: the sentinel rows are rewritten,
// the capacity is kept, texts is emptied.
func (b *Builder) Reset(src string, space *IDSpace, id StoreID) {
	if cap(b.s.nodes) == 0 {
		b.s.nodes = make([]NodeHeader, 1, 1024)
		b.s.extra = make([]uint32, 3, 1024)
	} else {
		b.s.nodes = b.s.nodes[:1]
		b.s.extra = b.s.extra[:3]
		b.s.nodes[0] = NodeHeader{}
		clear(b.s.extra)
	}
	b.textBuf.Reset()
	b.s.texts = ""
	b.s.src = src
	b.s.id = id
	b.s.space = space
}

// View is the Node of id in the scratch. It points into s.nodes, so it is valid
// only until the next constructor, List or NewXxx call.
func (b *Builder) View(id NodeRef) Node { return b.s.node(id) }

// ViewList is View for a list block.
func (b *Builder) ViewList(at ListRef) List { return List{&b.s, at} }

func (b *Builder) AddFlags(id NodeRef, flags ast.NodeFlags) { b.s.nodes[id].flags |= flags }

// Mark and Truncate are the speculation pair: Truncate drops every node and
// payload word made since Mark. texts is not truncated (strings.Builder has no
// truncate), so a text made during a rewound speculation stays as dead bytes.
func (b *Builder) Mark() (nodes, extra int) { return len(b.s.nodes), len(b.s.extra) }
func (b *Builder) Truncate(nodes, extra int) {
	b.s.nodes, b.s.extra = b.s.nodes[:nodes], b.s.extra[:extra]
}

// Len is the number of rows including the sentinel and any dead node.
func (b *Builder) Len() int { return len(b.s.nodes) }

// List returns the index of the new block. A block index is an edge, so it
// stays below foreignBit.
func (b *Builder) List(pos, end int32, elems []NodeRef) ListRef {
	at := ListRef(len(b.s.extra))
	if at >= foreignBit {
		panic("store: extra is full")
	}
	b.s.extra = append(b.s.extra, uint32(len(elems)), uint32(pos), uint32(end))
	for _, elem := range elems {
		b.s.extra = append(b.s.extra, uint32(elem))
	}
	return at
}

// NewToken is the constructor of every kind without a payload: tokens,
// keywords and the definitions without members.
func (b *Builder) NewToken(kind ast.Kind, flags ast.NodeFlags, pos, end int32) NodeRef {
	if storeChecks && kind == ast.KindUnknown {
		panic("store: a node of kind Unknown")
	}
	return b.header(kind, flags, 0, pos, end, 0)
}

// header appends a row. A row index is an edge, so it stays below foreignBit.
func (b *Builder) header(kind ast.Kind, flags ast.NodeFlags, mod ast.ModifierFlags, pos, end int32, data uint32) NodeRef {
	id := NodeRef(len(b.s.nodes))
	if id >= foreignBit {
		panic("store: nodes is full")
	}
	b.s.nodes = append(b.s.nodes, NodeHeader{
		kind: kind,
		// Bits 0-15: the Pointer never sets the others (NodeHeader.modifierFlags).
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
	if start := int(end) - len(text); start >= 0 && int(end) <= len(b.s.src) && b.s.src[start:end] == text {
		return uint32(start)
	}
	at := uint32(len(b.s.extra))
	off, length := b.textWords(end, text)
	b.s.extra = append(b.s.extra, off, length)
	return textInTexts | at
}

// textWords is the [offset, length] pair of a text member: the source offset
// when the text ends at end, else the offset in texts with textInTexts set.
func (b *Builder) textWords(end int32, text string) (off, length uint32) {
	if start := int(end) - len(text); start >= 0 && int(end) <= len(b.s.src) && b.s.src[start:end] == text {
		return uint32(start), uint32(len(text))
	}
	off = textInTexts | uint32(b.textBuf.Len())
	b.textBuf.WriteString(text)
	b.s.texts = b.textBuf.String() // the buffer may have moved
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
		b.s.nodes[child].parent = parent
	}
}

func (b *Builder) adoptList(at ListRef, parent NodeRef) {
	for _, elem := range b.s.extra[at+3 : at+3+ListRef(b.s.extra[at])] {
		b.s.nodes[elem].parent = parent
	}
}

// modifierFlags folds the kinds of the elements of a modifier block, like
// ast.ModifiersToFlags. Block 0 is empty.
func (b *Builder) modifierFlags(at ListRef) ast.ModifierFlags {
	var flags ast.ModifierFlags
	for _, elem := range b.s.extra[at+3 : at+3+ListRef(b.s.extra[at])] {
		flags |= ast.ModifierToFlag(b.s.nodes[elem].kind)
	}
	return flags
}

// Finish copies to the exact size (Compact). It does not renumber. The
// scratch may be reused for the next file afterwards; texts is cloned so that
// the Store never aliases the builder (store-ast-design-20260922.md 2.4). The
// flow and symbol words of every header are 0: the parser never writes them.
func (b *Builder) Finish() *Store {
	return &Store{
		nodes: append(make([]NodeHeader, 0, len(b.s.nodes)), b.s.nodes...),
		extra: append(make([]uint32, 0, len(b.s.extra)), b.s.extra...),
		src:   b.s.src,
		texts: strings.Clone(b.textBuf.String()),
		id:    b.s.id,
		space: b.s.space,
	}
}

// FinishFile is Finish for a parsed file: f and its Store point at each other
// (F1). File <-> Store is a cycle, which the GC handles.
func (b *Builder) FinishFile(f *File) {
	if storeChecks && b.s.nodes[len(b.s.nodes)-1].kind != ast.KindSourceFile {
		panic("store: the root is not the last node (invariant 20)")
	}
	s := b.Finish()
	s.file = f
	f.Store = s
}
