package ast

import (
	"math/bits"
	"sync"
	"sync/atomic"
	"unsafe"

	"github.com/microsoft/TypeScript/tsc/internal/core"
)

// NodeRef is a Store index. NoNodeRef is missing. It is not ast.NodeId.
type NodeRef uint32

// ListRef identifies both the owner and index of a packed list. Node headers
// still store 32-bit local indexes; foreign list slots live in a sparse map.
// Qualifying lists allows transforms to reuse unchanged lists across Stores.
type ListRef uint64

const (
	NoNodeRef NodeRef = 0
	NoListRef ListRef = 0
)

func (s *Store) listRef(index uint32) ListRef {
	if index == 0 {
		return 0
	}
	return ListRef(s.ID())<<32 | ListRef(index)
}

func (s *Store) listOwner(list ListRef) *Store {
	id := StoreID(list >> 32)
	if id == 0 || id == s.ID() {
		return s
	}
	if owner := s.listOwners[id]; owner != nil {
		return owner
	}
	owner := identitySet().Store(id)
	if owner == nil {
		panic("ast: missing list owner")
	}
	return owner
}

// retainList is owner-only. Retain a foreign list's Store so structural sharing
// remains valid even after its identity slot is unregistered.
func (s *Store) retainList(list ListRef) {
	if list == 0 {
		return
	}
	owner := s.listOwner(list)
	if owner == s {
		return
	}
	if s.listOwners == nil {
		s.listOwners = make(map[StoreID]*Store)
	}
	s.listOwners[owner.ID()] = owner
}

func (s *Store) listSlot(index uint32) ListRef {
	if local := s.children[index]; local != 0 {
		return s.listRef(uint32(local))
	}
	return s.foreignLists[index]
}

func (s *Store) setListSlot(index uint32, list ListRef) {
	s.mustMutate()
	if list == 0 || s.listOwner(list) == s {
		s.children[index] = NodeRef(list)
		delete(s.foreignLists, index)
		return
	}
	s.retainList(list)
	s.children[index] = 0
	if s.foreignLists == nil {
		s.foreignLists = make(map[uint32]ListRef)
	}
	s.foreignLists[index] = list
}

// Handle is a stack value. Heap-resident structures should hold NodeRef and
// rebuild the Handle via Store.At; a stored Handle carries *Store, which puts
// a pointer word in every element and forces the GC to scan the container.
// Kind is cached from the node header so callers can switch on a plain field
// without a Store load through Kind().
type Handle struct {
	s    *Store
	id   NodeRef
	Kind Kind
}

// handleOf rebuilds a Handle with Kind cached from the header.
func (s *Store) handleOf(id NodeRef) Handle {
	if s == nil || id == NoNodeRef {
		return Handle{}
	}
	return Handle{s: s, id: id, Kind: s.nodes[id].kind}
}

// StoreVisitor returns true to stop walking, matching Visitor on *Node.
type StoreVisitor func(Handle) bool

// nodeHeader packs every per-node scalar into one pointer-free 24-byte row so
// []nodeHeader stays noscan and a multi-field visit hits one cache line
// (two and a half rows per 64-byte line).
//
// Layout notes:
//   - childLen and listLen are uint8. The schema maximum is 6 named children
//     and 4 list slots; AllocSlots panics above 255.
//   - Named child slots and list slots share the children column. A node's
//     named children occupy children[childStart : childStart+childLen] and its
//     list slots follow at children[childStart+childLen : +listLen], so one
//     base index serves both.
//   - Kinds with a primary string slot (identifiers, literals, template parts,
//     JsxText) have no child or list slots, so childStart doubles as their
//     intern id. Ident reads it only when childLen|listLen == 0, and
//     AllocSlots writes 0 there for slotless nodes.
//   - TokenFlags are set on under 4% of nodes and live in a side map.
type nodeHeader struct {
	kind       Kind
	childLen   uint8
	listLen    uint8
	flags      NodeFlags
	pos        int32
	end        int32
	parent     NodeRef
	childStart uint32
}

// maxNodeSlots bounds childLen+listLen so both counts fit the uint8 header
// fields and the shared slot range stays addressable.
const maxNodeSlots = 255

// listBase is the children index of the first list slot.
func (n *nodeHeader) listBase() uint32 { return n.childStart + uint32(n.childLen) }

// identID is the intern id for text-bearing kinds. Nodes with any slot have no
// primary text, so their childStart is never read as an intern id.
func (n *nodeHeader) identID() uint32 {
	if n.childLen|n.listLen != 0 {
		return 0
	}
	return n.childStart
}

type listHeader struct {
	pos   int32
	end   int32
	start uint32
	len   uint32
}

type storePhase uint8

const (
	storePhaseBuild storePhase = iota
	storePhaseCheck
)

// Store owns the long-lived tree. One writer for life. Parse, bind, and JSDoc
// warmup write during build. Freeze publishes the Store as immutable for
// parallel check (no append, no map writes except SubtreeFacts atomics).
// Emit uses a private Store and shares frozen parse nodes/lists. Freeze is
// irreversible, including across Program rebuilds.
//
// StoreSet is separately synchronized for cross-file registration and lookup.
// After Freeze, internIdx is dropped so node/list/intern backing arrays stay
// pointer-free (noscan).
type Store struct {
	registrationMu sync.Mutex
	identityDomain *storeIdentityDomain
	id             atomic.Uint32 // StoreID assigned by StoreSet.Add; 0 until registered
	allocHint      int
	phase          storePhase
	frozenAt       NodeRef
	freezeOnce     sync.Once
	nodes          []nodeHeader
	lists          []listHeader
	children       []NodeRef // named child slots, list slots (as ListRef), and list elements
	internBuf      []byte
	internOff      []uint32 // intern id i occupies internBuf[internOff[i]:internOff[i+1]]
	internIdx      map[string]uint32
	// Symbol and Flow mirror high-fill pointer-AST node fields as dense
	// columns. End/return flow, localSymbol, Locals, and NextContainer stay
	// maps: few nodes set them, and pre-sizing those columns raises B/op.
	//
	// symbolIdx is a noscan NodeRef-indexed column of 1-based indexes into
	// symbolRefs (0 = no symbol). About one node in eight carries a Symbol, so
	// the pointer-bearing slice the GC must scan is eight times smaller than a
	// dense []*Symbol column, and the dense column itself halves in size.
	symbolIdx    []uint32
	symbolRefs   []*Symbol // symbolRefs[0] is the nil sentinel
	localSymbols map[NodeRef]*Symbol
	// flows is a noscan NodeRef-indexed column of flow ids (0 = no flow): a
	// 1-based index into the Store's flow arena, or flowForeignBit | index into
	// foreignFlows for FlowNodes the Store did not allocate (copied from another
	// Store, or built by the checker). The arena is chunked so *FlowNode stays
	// stable while it grows; chunks double from 8 up to 256 entries (see
	// flowSlot) so small files do not pay for a mostly empty chunk.
	flows          []uint32
	flowChunks     [][]FlowNode
	flowCur        []FlowNode // tail of the last chunk: len = used, cap = chunk size
	flowLen        int
	foreignFlows   []*FlowNode
	lastFlow       *FlowNode // flowID cache: the binder stamps runs of nodes with the same flow
	lastFlowID     uint32
	endFlows       map[NodeRef]*FlowNode
	returnFlows    map[NodeRef]*FlowNode
	locals         map[NodeRef]SymbolTable
	nextContainer  map[NodeRef]NodeRef
	tokenFlags     map[NodeRef]TokenFlags // sparse: literals and template parts only
	scalarValues   map[uint64]uint64      // packed NodeRef/value-slot key; pointer-free
	stringValues   map[uint64]uint32      // intern ids keyed by NodeRef/value-slot
	objectValues   map[uint64]any         // sparse pointer/slice kind-specific values
	externalChild  map[uint64]Handle
	externalList   map[uint64]Handle
	externalParent map[NodeRef]Handle
	foreignLists   map[uint32]ListRef
	listOwners     map[StoreID]*Store
	subtreeFacts   []uint32
	sourceFile     *SourceFile // metadata owner; SourceFile fields stay outside Store
}

// BindColumnHintPct is an experiment knob read by NewStore: when > 0, the
// symbolIdx and flows bind columns are pre-sized to (hint+1)*pct/100 entries
// at parse time instead of being allocated by PrepareBindTables. Benchmarks
// set it; production leaves it 0.
var BindColumnHintPct = 0

func NewStore(hint int) *Store {
	if hint < 1 {
		hint = 1
	}
	s := &Store{
		allocHint: hint,
		nodes:     make([]nodeHeader, 1, hint+1),
		lists:     make([]listHeader, 1, max(8, hint/7)),
		children:  make([]NodeRef, 0, hint+hint/2+hint*3/8),
		internBuf: make([]byte, 0, hint),
		internOff: make([]uint32, 2, max(2, hint/32)),
		internIdx: make(map[string]uint32, hint/32),
	}
	if pct := BindColumnHintPct; pct > 0 {
		n := (hint+1)*pct/100 + 1
		s.symbolIdx = make([]uint32, 0, n)
		s.flows = make([]uint32, 0, n)
	}
	return s
}

func (s *Store) Alloc(kind Kind, flags NodeFlags, loc core.TextRange, childLen int) Handle {
	return s.AllocSlots(kind, flags, loc, childLen, 0)
}

func (s *Store) AllocSlots(kind Kind, flags NodeFlags, loc core.TextRange, childLen, listLen int) Handle {
	if s == nil {
		panic("ast: Alloc on nil Store")
	}
	s.mustMutate()
	return Handle{s: s, id: s.appendSlots(kind, flags, loc, childLen, listLen), Kind: kind}
}

// appendSlots is the build-phase allocator. It does not call mustMutate so the
// compiler can keep len/cap of s.nodes in registers across the append. Factory
// parse/emit construction uses this; AllocSlots keeps the freeze check for
// other callers. Do not call after Freeze.
func (s *Store) appendSlots(kind Kind, flags NodeFlags, loc core.TextRange, childLen, listLen int) NodeRef {
	if childLen < 0 {
		panic("ast: negative childLen")
	}
	if listLen < 0 {
		panic("ast: negative listLen")
	}
	if childLen > maxNodeSlots || listLen > maxNodeSlots || childLen+listLen > maxNodeSlots {
		panic("ast: too many node slots")
	}
	if len(s.nodes) >= int(^NodeRef(0)) {
		panic("ast: Store exhausted")
	}
	id := NodeRef(len(s.nodes))
	// Slotless nodes keep childStart 0 so Ident on a text-less node reads the
	// empty intern id instead of a stale slot base.
	start := uint32(0)
	if n := childLen + listLen; n > 0 {
		start = uint32(len(s.children))
		s.children = append(s.children, make([]NodeRef, n)...)
	}
	s.nodes = append(s.nodes, nodeHeader{
		kind:       kind,
		childLen:   uint8(childLen),
		listLen:    uint8(listLen),
		flags:      flags,
		pos:        int32(loc.Pos()),
		end:        int32(loc.End()),
		childStart: start,
	})
	return id
}

func (s *Store) AllocList(loc core.TextRange, n int) ListRef {
	if s == nil {
		panic("ast: AllocList on nil Store")
	}
	s.mustMutate()
	return s.appendList(loc, n)
}

func (s *Store) appendList(loc core.TextRange, n int) ListRef {
	if n < 0 {
		panic("ast: negative list length")
	}
	if len(s.lists) >= int(^uint32(0)) {
		panic("ast: Store lists exhausted")
	}
	RegisterStore(s)
	id := s.listRef(uint32(len(s.lists)))
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
	s.mustMutate()
	return s.intern(text)
}

func (s *Store) intern(text string) uint32 {
	if text == "" {
		return 0
	}
	if id, ok := s.internIdx[text]; ok {
		return id
	}
	id := uint32(len(s.internOff) - 1)
	s.internBuf = append(s.internBuf, text...)
	s.internOff = append(s.internOff, uint32(len(s.internBuf)))
	if s.internIdx != nil {
		s.internIdx[text] = id
	}
	return id
}

// FinishParse writes context flags and source loc after a Factory constructor.
// Parser-only; no mustMutate / mustLive, so the stores are one bounds-checked
// header write. extraFlags is OR-ed into the flags Alloc already stored.
func (s *Store) FinishParse(id NodeRef, extraFlags NodeFlags, pos, end int) {
	n := &s.nodes[id]
	n.flags |= extraFlags
	n.pos = int32(pos)
	n.end = int32(end)
}

func (s *Store) setIdent(id NodeRef, internID uint32) {
	s.nodes[id].childStart = internID
}

// linkChild is the same-store parse attach. Foreign children fall back to
// Handle.SetChild (emit). No mustMutate on the same-store path.
func (s *Store) linkChild(parent NodeRef, slot int, child Handle) {
	n := &s.nodes[parent]
	idx := int(n.childStart) + slot
	if child.id == 0 || child.s == nil {
		s.children[idx] = 0
		return
	}
	if child.s == s {
		s.children[idx] = child.id
		s.nodes[child.id].parent = parent
		return
	}
	Handle{s: s, id: parent, Kind: n.kind}.SetChild(slot, child)
}

func (s *Store) linkChildRef(parent NodeRef, slot int, child NodeRef) {
	n := &s.nodes[parent]
	idx := int(n.childStart) + slot
	s.children[idx] = child
	if child != NoNodeRef {
		s.nodes[child].parent = parent
	}
}

func (s *Store) linkList(parent NodeRef, slot int, list ListRef) {
	n := &s.nodes[parent]
	idx := n.listBase() + uint32(slot)
	if list == 0 || s.listOwner(list) == s {
		s.children[idx] = NodeRef(list)
		if list != 0 {
			lh := &s.lists[uint32(list)]
			for i := uint32(0); i < lh.len; i++ {
				if c := s.children[lh.start+i]; c != 0 {
					s.nodes[c].parent = parent
				}
			}
		}
		return
	}
	Handle{s: s, id: parent, Kind: n.kind}.SetListSlot(slot, list)
}

func (s *Store) Seal() {
	s.mustMutate()
	s.internIdx = nil
}

// Freeze is the irreversible build → check publication barrier.
func (s *Store) Freeze() {
	if s == nil {
		return
	}
	s.freezeOnce.Do(func() {
		s.phase = storePhaseCheck
		s.frozenAt = NodeRef(len(s.nodes))
		s.internIdx = nil
		s.subtreeFacts = make([]uint32, len(s.nodes))
	})
}

func (s *Store) mustMutate() {
	if s != nil && s.phase == storePhaseCheck {
		panic("ast: write to frozen Store")
	}
}

// StoreCheckpoint is a speculative-parse watermark. Restore truncates node,
// list, and child columns back to this point. Interned strings stay; they are
// not counted in Len and are safe to reuse.
type StoreCheckpoint struct {
	nodes    int
	lists    int
	children int
}

func (s *Store) Checkpoint() StoreCheckpoint {
	if s == nil {
		return StoreCheckpoint{}
	}
	return StoreCheckpoint{
		nodes:    len(s.nodes),
		lists:    len(s.lists),
		children: len(s.children),
	}
}

func (s *Store) Restore(cp StoreCheckpoint) {
	s.mustMutate()
	if s == nil {
		return
	}
	if cp.nodes < 1 || cp.nodes > len(s.nodes) ||
		cp.lists < 1 || cp.lists > len(s.lists) ||
		cp.children < 0 || cp.children > len(s.children) {
		panic("ast: invalid Store checkpoint")
	}
	s.nodes = s.nodes[:cp.nodes]
	s.lists = s.lists[:cp.lists]
	s.children = s.children[:cp.children]
	s.symbolIdx = truncateCol(s.symbolIdx, cp.nodes)
	s.flows = truncateCol(s.flows, cp.nodes) // arena entries past the checkpoint are left unreferenced
	cutNodeMap(s.tokenFlags, NodeRef(cp.nodes))
	cutNodeMap(s.localSymbols, NodeRef(cp.nodes))
	cutNodeMap(s.endFlows, NodeRef(cp.nodes))
	cutNodeMap(s.returnFlows, NodeRef(cp.nodes))
	cutNodeMap(s.locals, NodeRef(cp.nodes))
	cutNodeMap(s.nextContainer, NodeRef(cp.nodes))
	cutNodeMap(s.externalParent, NodeRef(cp.nodes))
	for key := range s.externalChild {
		if NodeRef(key>>32) >= NodeRef(cp.nodes) {
			delete(s.externalChild, key)
		}
	}
	for key := range s.externalList {
		if uint32(key>>32) >= uint32(cp.lists) {
			delete(s.externalList, key)
		}
	}
	for slot := range s.foreignLists {
		if slot >= uint32(cp.children) {
			delete(s.foreignLists, slot)
		}
	}
}

func truncateCol[T any](col []T, n int) []T {
	if len(col) > n {
		clear(col[n:])
		return col[:n]
	}
	return col
}

func cutNodeMap[V any](m map[NodeRef]V, min NodeRef) {
	for k := range m {
		if k >= min {
			delete(m, k)
		}
	}
}

func (s *Store) Len() int {
	if s == nil || len(s.nodes) == 0 {
		return 0
	}
	return len(s.nodes) - 1
}

func (s *Store) At(ref NodeRef) Handle {
	return s.handleOf(ref)
}

// HandleOf builds a Handle with a caller-supplied Kind (no header reload).
func HandleOf(s *Store, id NodeRef, kind Kind) Handle {
	if s == nil || id == NoNodeRef {
		return Handle{}
	}
	return Handle{s: s, id: id, Kind: kind}
}

// KindAt returns the node Kind. NoNodeRef yields KindUnknown.
func (s *Store) KindAt(id NodeRef) Kind {
	if s == nil || id == NoNodeRef {
		return KindUnknown
	}
	return s.nodes[id].kind
}

// FlagsAt returns the node flags without constructing a Handle.
func (s *Store) FlagsAt(id NodeRef) NodeFlags {
	if s == nil || id == NoNodeRef {
		return 0
	}
	return s.nodes[id].flags
}

// LocAt returns the source range without constructing a Handle.
func (s *Store) LocAt(id NodeRef) core.TextRange {
	if s == nil || id == NoNodeRef {
		return core.UndefinedTextRange()
	}
	n := &s.nodes[id]
	return core.NewTextRange(int(n.pos), int(n.end))
}

// TextAt returns identifier or literal text without constructing a Handle.
func (s *Store) TextAt(id NodeRef) string {
	if s == nil || id == NoNodeRef {
		return ""
	}
	return s.internText(s.nodes[id].identID())
}

// NumChildrenAt is the named-child slot count without constructing a Handle.
func (s *Store) NumChildrenAt(id NodeRef) int {
	if s == nil || id == NoNodeRef {
		return 0
	}
	return int(s.nodes[id].childLen)
}

// NumListSlotsAt is the list-slot count without constructing a Handle.
func (s *Store) NumListSlotsAt(id NodeRef) int {
	if s == nil || id == NoNodeRef {
		return 0
	}
	return int(s.nodes[id].listLen)
}

// SetFlagsAt updates the node flags without constructing a Handle.
func (s *Store) SetFlagsAt(id NodeRef, flags NodeFlags) {
	if s == nil || id == NoNodeRef {
		return
	}
	s.mustMutate()
	s.nodes[id].flags = flags
}

// ChildRef returns the same-Store child at kind-relative slot. NoNodeRef means
// missing or an external child (use At(parent).Child for the slow path).
func (s *Store) ChildRef(parent NodeRef, slot uint32) NodeRef {
	if s == nil || parent == NoNodeRef {
		return NoNodeRef
	}
	n := &s.nodes[parent]
	if slot >= uint32(n.childLen) {
		panic("ast: child index out of range")
	}
	return s.children[n.childStart+slot]
}

// ListSlotAt returns the ListRef at list-relative slot for parent.
func (s *Store) ListSlotAt(parent NodeRef, slot uint32) ListRef {
	if s == nil || parent == NoNodeRef {
		return NoListRef
	}
	n := &s.nodes[parent]
	if slot >= uint32(n.listLen) {
		panic("ast: list slot out of range")
	}
	return s.listSlot(n.listBase() + slot)
}

// ListElem returns the same-Store element NodeRef at list index i.
// NoNodeRef means missing or external (use ListAt for the slow path).
func (s *Store) ListElem(list ListRef, i int) NodeRef {
	if list != NoListRef && s != nil {
		s = s.listOwner(list)
	}
	if list == NoListRef || s == nil {
		return NoNodeRef
	}
	l := &s.lists[uint32(list)]
	if i < 0 || i >= int(l.len) {
		panic("ast: list index out of range")
	}
	return s.children[int(l.start)+i]
}

// ParentRef returns the packed same-Store parent, or 0.
func (s *Store) ParentRef(id NodeRef) NodeRef {
	if s == nil || id == NoNodeRef {
		return 0
	}
	return s.nodes[id].parent
}

func (s *Store) SetSourceFile(file *SourceFile) {
	if s == nil {
		panic("ast: SetSourceFile on nil Store")
	}
	s.mustMutate()
	s.sourceFile = file
}

func (s *Store) SourceFile() *SourceFile {
	if s == nil {
		return nil
	}
	return s.sourceFile
}

func (s *Store) ListLen(list ListRef) int {
	if list != 0 && s != nil {
		s = s.listOwner(list)
	}
	if list == 0 || s == nil {
		return 0
	}
	return int(s.lists[uint32(list)].len)
}

func (s *Store) ListLoc(list ListRef) core.TextRange {
	if list != 0 && s != nil {
		s = s.listOwner(list)
	}
	if list == 0 || s == nil {
		return core.UndefinedTextRange()
	}
	l := &s.lists[uint32(list)]
	return core.NewTextRange(int(l.pos), int(l.end))
}

func (s *Store) ListAt(list ListRef, i int) Handle {
	if list != 0 && s != nil {
		s = s.listOwner(list)
	}
	if list == 0 || s == nil {
		return Handle{}
	}
	l := &s.lists[uint32(list)]
	if i < 0 || i >= int(l.len) {
		panic("ast: list index out of range")
	}
	if id := s.children[int(l.start)+i]; id != 0 {
		return s.handleOf(id)
	}
	if child := s.externalList[uint64(uint32(list))<<32|uint64(uint32(i))]; !child.IsNil() {
		return child
	}
	return Handle{}
}

// ListRefAt returns the same-store NodeRef stored in list slot i, or 0 when
// the slot is empty or holds an external (cross-store) child. Callers that may
// see external children must use ListAt.
func (s *Store) ListRefAt(list ListRef, i int) NodeRef {
	if list != 0 && s != nil {
		s = s.listOwner(list)
	}
	if list == 0 || s == nil {
		return 0
	}
	l := &s.lists[uint32(list)]
	if i < 0 || i >= int(l.len) {
		panic("ast: list index out of range")
	}
	return s.children[int(l.start)+i]
}

func (s *Store) ListHasTrailingComma(list ListRef) bool {
	n := s.ListLen(list)
	if n == 0 {
		return false
	}
	last := s.ListAt(list, n-1)
	return last.Loc().End() < s.ListLoc(list).End()
}

func (s *Store) setListLoc(list ListRef, loc core.TextRange) {
	if list != 0 && s != nil && s.listOwner(list) != s {
		panic("ast: write to foreign list")
	}
	if list == 0 || s == nil {
		return
	}
	s.mustMutate()
	s.lists[uint32(list)].pos = int32(loc.Pos())
	s.lists[uint32(list)].end = int32(loc.End())
}

func (s *Store) SetListAt(list ListRef, i int, h Handle) {
	if list != 0 && s != nil && s.listOwner(list) != s {
		panic("ast: write to foreign list")
	}
	if list == 0 || s == nil {
		panic("ast: SetListAt on missing list")
	}
	s.mustMutate()
	l := &s.lists[uint32(list)]
	if i < 0 || i >= int(l.len) {
		panic("ast: list index out of range")
	}
	ref := NodeRef(0)
	if h.id != 0 {
		if h.s != s {
			if s.externalList == nil {
				s.externalList = make(map[uint64]Handle)
			}
			s.children[int(l.start)+i] = 0
			s.externalList[uint64(uint32(list))<<32|uint64(uint32(i))] = h
			return
		}
		ref = h.id
	}
	delete(s.externalList, uint64(uint32(list))<<32|uint64(uint32(i)))
	s.children[int(l.start)+i] = ref
}

func (s *Store) SetExternalListAt(list ListRef, i int, child GlobalRef) {
	if list != 0 && s != nil && s.listOwner(list) != s {
		panic("ast: write to foreign list")
	}
	if list == 0 || s == nil {
		panic("ast: SetExternalListAt on missing list")
	}
	s.mustMutate()
	l := &s.lists[uint32(list)]
	if i < 0 || i >= int(l.len) {
		panic("ast: list index out of range")
	}
	key := uint64(uint32(list))<<32 | uint64(uint32(i))
	if child == 0 {
		delete(s.externalList, key)
		return
	}
	if s.children[int(l.start)+i] != 0 {
		panic("ast: external list child conflicts with local child")
	}
	if s.externalList == nil {
		s.externalList = make(map[uint64]Handle)
	}
	s.externalList[key] = NodeOf(child)
}

func (s *Store) ExternalListAt(list ListRef, i int) GlobalRef {
	if list != 0 && s != nil {
		s = s.listOwner(list)
	}
	if list == 0 || s == nil {
		return 0
	}
	l := &s.lists[uint32(list)]
	if i < 0 || i >= int(l.len) {
		panic("ast: list index out of range")
	}
	return s.externalList[uint64(uint32(list))<<32|uint64(uint32(i))].Global()
}

func (s *Store) PrepareBindTables() {
	if s == nil {
		return
	}
	s.mustMutate()
	n := len(s.nodes)
	if n < 1 {
		return
	}
	s.symbolIdx = ensureCol(s.symbolIdx, n)
	if s.symbolRefs == nil {
		s.symbolRefs = make([]*Symbol, 1, max(2, n/8))
	}
	s.flows = ensureCol(s.flows, n)
}

// ensureCol grows col to len n. Capacity past len is always zero (make zeroes
// it and truncateCol clears what it cuts), so reslicing is enough.
func ensureCol[T any](col []T, n int) []T {
	if len(col) >= n {
		return col
	}
	if cap(col) >= n {
		return col[:n]
	}
	grown := make([]T, n)
	copy(grown, col)
	return grown
}

// putCol writes col[ref], growing geometrically so Stores that were not
// pre-sized by PrepareBindTables (checker synthetics) stay amortized O(1).
func putCol[T any](col *[]T, ref NodeRef, v T) {
	i := int(ref)
	if i >= len(*col) {
		*col = ensureCol(*col, max(i+1, 2*len(*col)))
	}
	(*col)[i] = v
}

func getCol[T any](col []T, ref NodeRef) (z T) {
	i := int(ref)
	if i < len(col) {
		return col[i]
	}
	return z
}

func (s *Store) SetSymbol(ref NodeRef, sym *Symbol) {
	if s == nil || ref == 0 {
		return
	}
	s.mustMutate()
	if idx := getCol(s.symbolIdx, ref); idx != 0 {
		s.symbolRefs[idx] = sym
		return
	}
	if sym == nil {
		return
	}
	if s.symbolRefs == nil {
		s.symbolRefs = make([]*Symbol, 1, 8)
	}
	s.symbolRefs = append(s.symbolRefs, sym)
	putCol(&s.symbolIdx, ref, uint32(len(s.symbolRefs)-1))
}

func (s *Store) Symbol(ref NodeRef) *Symbol {
	if s == nil || ref == 0 {
		return nil
	}
	if idx := getCol(s.symbolIdx, ref); idx != 0 {
		return s.symbolRefs[idx]
	}
	return nil
}

func (s *Store) SetLocalSymbol(ref NodeRef, sym *Symbol) {
	if s == nil || ref == 0 {
		return
	}
	s.mustMutate()
	if sym == nil {
		delete(s.localSymbols, ref)
		return
	}
	if s.localSymbols == nil {
		s.localSymbols = make(map[NodeRef]*Symbol)
	}
	s.localSymbols[ref] = sym
}

func (s *Store) LocalSymbol(ref NodeRef) *Symbol {
	if s == nil || ref == 0 {
		return nil
	}
	return s.localSymbols[ref]
}

func (s *Store) SetFlow(ref NodeRef, flow *FlowNode) {
	if s == nil || ref == 0 {
		return
	}
	s.mustMutate()
	putCol(&s.flows, ref, s.flowID(flow))
}

func (s *Store) Flow(ref NodeRef) *FlowNode {
	if s == nil || ref == 0 {
		return nil
	}
	return s.flowAt(getCol(s.flows, ref))
}

const flowForeignBit = 1 << 31

// flowSlot maps a 0-based arena index to its chunk and offset. Chunk sizes are
// 8, 8, 16, 32, 64, 128, then 256 for every chunk after, so chunk c (1..5)
// starts at 1<<(c+2) and chunk 6 starts at 256.
func flowSlot(i uint32) (chunk, off uint32) {
	if i >= 256 {
		return 6 + (i-256)>>8, i & 255
	}
	if i < 8 {
		return 0, i
	}
	c := uint32(bits.Len32(i)) - 3
	return c, i - 1<<(c+2)
}

func flowChunkCap(chunk uint32) int {
	switch {
	case chunk == 0:
		return 8
	case chunk >= 6:
		return 256
	}
	return 4 << chunk
}

// NewFlow allocates a FlowNode in the Store's flow arena and stamps it with
// the id the flows column stores for it.
func (s *Store) NewFlow(flags FlowFlags) *FlowNode {
	n := len(s.flowCur)
	if n == cap(s.flowCur) {
		s.growFlowChunk()
		n = 0
	}
	s.flowCur = s.flowCur[:n+1]
	f := &s.flowCur[n]
	s.flowLen++
	f.Flags = flags
	f.id = uint32(s.flowLen)
	s.lastFlow, s.lastFlowID = f, f.id // the binder usually stores the flow it just made
	return f
}

func (s *Store) growFlowChunk() {
	c, _ := flowSlot(uint32(s.flowLen))
	chunk := make([]FlowNode, flowChunkCap(c))
	s.flowChunks = append(s.flowChunks, chunk)
	s.flowCur = chunk[:0]
}

// FlowCount reports how many FlowNodes the Store's arena holds.
func (s *Store) FlowCount() int { return s.flowLen }

func (s *Store) flowAt(id uint32) *FlowNode {
	if id == 0 {
		return nil
	}
	if id&flowForeignBit != 0 {
		return s.foreignFlows[id&^flowForeignBit]
	}
	c, off := flowSlot(id - 1)
	return &s.flowChunks[c][off]
}

// flowID returns the column id for f. A FlowNode from this Store's arena is
// recognized by address; anything else gets a pointer slot in foreignFlows.
func (s *Store) flowID(f *FlowNode) uint32 {
	if f == nil {
		return 0
	}
	if f == s.lastFlow {
		return s.lastFlowID
	}
	id := f.id
	if c, off := flowSlot(id - 1); id == 0 || id&flowForeignBit != 0 || int(id) > s.flowLen || &s.flowChunks[c][off] != f {
		s.foreignFlows = append(s.foreignFlows, f)
		id = flowForeignBit | uint32(len(s.foreignFlows)-1)
	}
	s.lastFlow, s.lastFlowID = f, id
	return id
}

func (s *Store) SetEndFlow(ref NodeRef, flow *FlowNode) {
	if s == nil || ref == 0 {
		return
	}
	s.mustMutate()
	if flow == nil {
		delete(s.endFlows, ref)
		return
	}
	if s.endFlows == nil {
		s.endFlows = make(map[NodeRef]*FlowNode)
	}
	s.endFlows[ref] = flow
}

func (s *Store) EndFlow(ref NodeRef) *FlowNode {
	if s == nil || ref == 0 {
		return nil
	}
	return s.endFlows[ref]
}

func (s *Store) SetReturnFlow(ref NodeRef, flow *FlowNode) {
	if s == nil || ref == 0 {
		return
	}
	s.mustMutate()
	if flow == nil {
		delete(s.returnFlows, ref)
		return
	}
	if s.returnFlows == nil {
		s.returnFlows = make(map[NodeRef]*FlowNode)
	}
	s.returnFlows[ref] = flow
}

func (s *Store) ReturnFlow(ref NodeRef) *FlowNode {
	if s == nil || ref == 0 {
		return nil
	}
	return s.returnFlows[ref]
}

func (s *Store) SetLocals(ref NodeRef, locals SymbolTable) {
	if s == nil || ref == 0 {
		return
	}
	s.mustMutate()
	if locals == nil {
		delete(s.locals, ref)
		return
	}
	if s.locals == nil {
		s.locals = make(map[NodeRef]SymbolTable)
	}
	s.locals[ref] = locals
}

func (s *Store) Locals(ref NodeRef) SymbolTable {
	if s == nil || ref == 0 {
		return nil
	}
	return s.locals[ref]
}

func (s *Store) SetNextContainer(ref NodeRef, next NodeRef) {
	if s == nil || ref == 0 {
		return
	}
	s.mustMutate()
	if next == 0 {
		delete(s.nextContainer, ref)
		return
	}
	if s.nextContainer == nil {
		s.nextContainer = make(map[NodeRef]NodeRef)
	}
	s.nextContainer[ref] = next
}

func (s *Store) NextContainer(ref NodeRef) NodeRef {
	if s == nil || ref == 0 {
		return 0
	}
	return s.nextContainer[ref]
}

func (h Handle) valueKey(slot int) uint64 {
	h.mustLive()
	if slot < 0 {
		panic("ast: negative value slot")
	}
	return uint64(h.id)<<32 | uint64(uint32(slot))
}

func (h Handle) SetUintValue(slot int, value uint64) {
	h.s.mustMutate()
	key := h.valueKey(slot)
	if h.s.scalarValues == nil {
		h.s.scalarValues = make(map[uint64]uint64, max(1, h.s.allocHint/16))
	}
	h.s.scalarValues[key] = value
}

func (h Handle) UintValue(slot int) uint64 {
	return h.s.scalarValues[h.valueKey(slot)]
}

// UintValueAt reads a scalar value without materializing a Handle. Generated
// NodeRef consumers use this for schema value fields whose representation is a
// sparse Store side table.
func (s *Store) UintValueAt(ref NodeRef, slot int) uint64 {
	if s == nil || ref == 0 {
		return 0
	}
	if slot < 0 {
		panic("ast: negative value slot")
	}
	return s.scalarValues[uint64(ref)<<32|uint64(uint32(slot))]
}

func (h Handle) SetStringValue(slot int, value string) {
	h.s.mustMutate()
	key := h.valueKey(slot)
	if value == "" {
		if primaryStringSlot(h.Kind) == slot {
			h.SetIdent(0)
		}
		delete(h.s.stringValues, key)
		return
	}
	if primaryStringSlot(h.Kind) == slot {
		h.SetIdent(h.s.Intern(value))
		return
	}
	if h.s.stringValues == nil {
		h.s.stringValues = make(map[uint64]uint32, max(1, h.s.allocHint/256))
	}
	h.s.stringValues[key] = h.s.Intern(value)
}

func (h Handle) StringValue(slot int) string {
	if primaryStringSlot(h.Kind) == slot {
		return h.Ident()
	}
	id := h.s.stringValues[h.valueKey(slot)]
	if id == 0 {
		return ""
	}
	return h.s.internText(id)
}

func (h Handle) SetObjectValue(slot int, value any) {
	h.s.mustMutate()
	key := h.valueKey(slot)
	if value == nil {
		delete(h.s.objectValues, key)
		return
	}
	if h.s.objectValues == nil {
		h.s.objectValues = make(map[uint64]any)
	}
	h.s.objectValues[key] = value
}

func storeObjectValue[T any](h Handle, slot int) T {
	var zero T
	value := h.s.objectValues[h.valueKey(slot)]
	if value == nil {
		return zero
	}
	result, ok := value.(T)
	if !ok {
		panic("ast: Store value slot type mismatch")
	}
	return result
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

// IsNil reports the absent node. NodeRef 0 is optional-absent, not NodeIsMissing.
func (h Handle) IsNil() bool { return h.s == nil || h.id == NoNodeRef }

func (h Handle) KindString() string {
	if h.IsNil() {
		return "<nil>"
	}
	return h.Kind.String()
}

func (h Handle) Flags() NodeFlags {
	if h.IsNil() {
		return 0
	}
	h.mustLive()
	return h.s.nodes[h.id].flags
}

func (h Handle) SetFlags(flags NodeFlags) {
	h.mustLive()
	h.s.mustMutate()
	h.s.nodes[h.id].flags = flags
}

func (h Handle) TokenFlags() TokenFlags {
	h.mustLive()
	return h.s.tokenFlags[h.id]
}

func (h Handle) SetTokenFlags(flags TokenFlags) {
	h.mustLive()
	h.s.mustMutate()
	if flags == 0 {
		delete(h.s.tokenFlags, h.id)
		return
	}
	if h.s.tokenFlags == nil {
		h.s.tokenFlags = make(map[NodeRef]TokenFlags, max(1, h.s.allocHint/32))
	}
	h.s.tokenFlags[h.id] = flags
}

func (h Handle) Loc() core.TextRange {
	h.mustLive()
	n := &h.s.nodes[h.id]
	return core.NewTextRange(int(n.pos), int(n.end))
}

func (h Handle) SetLoc(loc core.TextRange) {
	h.mustLive()
	h.s.mustMutate()
	n := &h.s.nodes[h.id]
	n.pos = int32(loc.Pos())
	n.end = int32(loc.End())
}

func (h Handle) Parent() Handle {
	if h.id == 0 || h.s == nil {
		return Handle{}
	}
	if id := h.s.nodes[h.id].parent; id != 0 {
		return h.s.handleOf(id)
	}
	if parent := h.s.externalParent[h.id]; !parent.IsNil() {
		return parent
	}
	return Handle{}
}

func (h Handle) SetParent(p Handle) {
	h.mustLive()
	h.s.mustMutate()
	if p.id == 0 || p.s == nil {
		h.s.nodes[h.id].parent = 0
		delete(h.s.externalParent, h.id)
		return
	}
	if p.s == h.s {
		delete(h.s.externalParent, h.id)
		h.s.nodes[h.id].parent = p.id
		return
	}
	h.s.nodes[h.id].parent = 0
	if h.s.externalParent == nil {
		h.s.externalParent = make(map[NodeRef]Handle)
	}
	h.s.externalParent[h.id] = p
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
	return h.childAt(uint32(i))
}

// childAt reads the kind-relative child slot without childLen checks.
// Hot path is small enough to inline into generated accessors.
func (h Handle) childAt(rel uint32) Handle {
	s := h.s
	id := s.children[s.nodes[h.id].childStart+rel]
	if id == NoNodeRef {
		return h.childAtSlow(rel)
	}
	return Handle{s: s, id: id, Kind: s.nodes[id].kind}
}

func (h Handle) childAtSlow(rel uint32) Handle {
	if h.s.externalChild != nil {
		return h.s.externalChild[h.valueKey(int(rel))]
	}
	return Handle{}
}

func (h Handle) SetChild(i int, c Handle) {
	h.mustLive()
	h.s.mustMutate()
	n := &h.s.nodes[h.id]
	if i < 0 || i >= int(n.childLen) {
		panic("ast: child index out of range")
	}
	slot := int(n.childStart) + i
	if c.id == 0 || c.s == nil {
		h.s.children[slot] = 0
		h.SetExternalChild(i, 0)
		return
	}
	if c.s == h.s {
		h.SetExternalChild(i, 0)
		h.s.children[slot] = c.id
		h.attachSameStore(c)
		return
	}
	h.s.children[slot] = 0
	if h.s.externalChild == nil {
		h.s.externalChild = make(map[uint64]Handle)
	}
	h.s.externalChild[h.valueKey(i)] = c
}

func (h Handle) SetExternalChild(i int, child GlobalRef) {
	h.mustLive()
	h.s.mustMutate()
	n := &h.s.nodes[h.id]
	if i < 0 || i >= int(n.childLen) {
		panic("ast: child index out of range")
	}
	key := h.valueKey(i)
	if child == 0 {
		delete(h.s.externalChild, key)
		return
	}
	if h.s.children[int(n.childStart)+i] != 0 {
		panic("ast: external child conflicts with local child")
	}
	if h.s.externalChild == nil {
		h.s.externalChild = make(map[uint64]Handle)
	}
	h.s.externalChild[key] = NodeOf(child)
}

func (h Handle) ExternalChild(i int) GlobalRef {
	h.mustLive()
	n := &h.s.nodes[h.id]
	if i < 0 || i >= int(n.childLen) {
		panic("ast: child index out of range")
	}
	return h.s.externalChild[h.valueKey(i)].Global()
}

func (h Handle) SetIdent(internID uint32) {
	h.mustLive()
	h.s.mustMutate()
	if internID >= uint32(len(h.s.internOff)-1) {
		panic("ast: intern id out of range")
	}
	n := &h.s.nodes[h.id]
	if n.childLen|n.listLen != 0 {
		panic("ast: SetIdent on a node with child slots")
	}
	n.childStart = internID
}

func (h Handle) Ident() string {
	h.mustLive()
	return h.s.internText(h.s.nodes[h.id].identID())
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

func (h Handle) LocalSymbol() *Symbol {
	if h.id == 0 || h.s == nil {
		return nil
	}
	return h.s.LocalSymbol(h.id)
}

func (h Handle) SetLocalSymbol(sym *Symbol) {
	h.mustLive()
	h.s.SetLocalSymbol(h.id, sym)
}

func (h Handle) FlowNode() *FlowNode {
	if h.id == 0 || h.s == nil {
		return nil
	}
	return h.s.Flow(h.id)
}

func (h Handle) SetFlowNode(flow *FlowNode) {
	h.mustLive()
	h.s.SetFlow(h.id, flow)
}

func (h Handle) EndFlowNode() *FlowNode {
	if h.id == 0 || h.s == nil {
		return nil
	}
	return h.s.EndFlow(h.id)
}

func (h Handle) SetEndFlowNode(flow *FlowNode) {
	h.mustLive()
	h.s.SetEndFlow(h.id, flow)
}

func (h Handle) ReturnFlowNode() *FlowNode {
	if h.id == 0 || h.s == nil {
		return nil
	}
	return h.s.ReturnFlow(h.id)
}

func (h Handle) SetReturnFlowNode(flow *FlowNode) {
	h.mustLive()
	h.s.SetReturnFlow(h.id, flow)
}

func (h Handle) Locals() SymbolTable {
	if h.id == 0 || h.s == nil {
		return nil
	}
	return h.s.Locals(h.id)
}

func (h Handle) SetLocals(locals SymbolTable) {
	h.mustLive()
	h.s.SetLocals(h.id, locals)
}

func (h Handle) NextContainer() Handle {
	if h.id == 0 || h.s == nil {
		return Handle{}
	}
	return h.s.At(h.s.NextContainer(h.id))
}

func (h Handle) SetNextContainer(next Handle) {
	h.mustLive()
	h.s.SetNextContainer(h.id, h.refInStore(next))
}

func (h Handle) NumListSlots() int {
	if h.id == 0 || h.s == nil {
		return 0
	}
	return int(h.s.nodes[h.id].listLen)
}

func (h Handle) ListSlot(i int) ListRef {
	h.mustLive()
	n := &h.s.nodes[h.id]
	if i < 0 || i >= int(n.listLen) {
		panic("ast: list slot out of range")
	}
	return h.s.listSlot(n.listBase() + uint32(i))
}

func (h Handle) SetListSlot(i int, list ListRef) {
	h.mustLive()
	h.s.mustMutate()
	n := &h.s.nodes[h.id]
	if i < 0 || i >= int(n.listLen) {
		panic("ast: list slot out of range")
	}
	h.s.setListSlot(n.listBase()+uint32(i), list)
	h.attachList(list)
}

func (h Handle) List() ListRef {
	if h.id == 0 || h.s == nil {
		return 0
	}
	n := &h.s.nodes[h.id]
	if n.listLen == 0 {
		return 0
	}
	return h.s.listSlot(n.listBase())
}

func (h Handle) SetList(list ListRef) {
	h.mustLive()
	h.s.mustMutate()
	n := &h.s.nodes[h.id]
	if n.listLen == 0 {
		panic("ast: SetList on node with no list slots")
	}
	h.s.setListSlot(n.listBase(), list)
	h.attachList(list)
}

func (h Handle) attachSameStore(c Handle) {
	if c.id == 0 || c.s == nil || c.s != h.s || h.s.phase != storePhaseBuild {
		return
	}
	c.SetParent(h)
}

func (h Handle) attachList(list ListRef) {
	if list == 0 || h.s == nil {
		return
	}
	n := h.s.ListLen(list)
	for i := 0; i < n; i++ {
		h.attachSameStore(h.s.ListAt(list, i))
	}
}

func (h Handle) forEachChildList(list ListRef, v StoreVisitor) bool {
	if list == 0 {
		return false
	}
	for i := range h.s.ListLen(list) {
		child := h.s.ListAt(list, i)
		if !child.IsNil() && v(child) {
			return true
		}
	}
	return false
}

func (h Handle) forEachChildJSDocParameterOrPropertyTag(v StoreVisitor) bool {
	if child := h.JSDocParameterOrPropertyTagTagName(); !child.IsNil() && v(child) {
		return true
	}
	name := h.JSDocParameterOrPropertyTagName()
	typeExpression := h.JSDocParameterOrPropertyTagTypeExpression()
	if h.JSDocParameterOrPropertyTagIsNameFirst() {
		if !name.IsNil() && v(name) {
			return true
		}
		if !typeExpression.IsNil() && v(typeExpression) {
			return true
		}
	} else {
		if !typeExpression.IsNil() && v(typeExpression) {
			return true
		}
		if !name.IsNil() && v(name) {
			return true
		}
	}
	return h.forEachChildList(h.JSDocParameterOrPropertyTagComment(), v)
}

// ForEachChild visits children in syntax schema order. true stops.
func (h Handle) ForEachChild(v StoreVisitor) bool {
	if h.id == 0 || h.s == nil {
		return false
	}
	return h.forEachChildSchema(v)
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
		if c.Store() != h.Store() {
			return false
		}
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
	if other.s == h.s {
		return other.id
	}
	if other.s == nil || h.s == nil {
		panic("ast: Handle from a different Store")
	}
	return NewFactoryOn(h.s, FactoryHooks{}).CopySubtree(other).id
}

// StoreScratch is the parser-owned set of parse-column backing arrays. A Store
// borrows them from NewStoreOnScratch, appends into them while parsing, and
// Compact hands them back after cloning the used prefix into exact-size
// arrays. The scratch keeps its high-water capacity across files, so the
// source-length hint only sizes it the first time; a finished Store never
// carries the hint's over-reserve.
type StoreScratch struct {
	nodes     []nodeHeader
	lists     []listHeader
	children  []NodeRef
	internBuf []byte
	internOff []uint32
}

// NewStoreOnScratch is NewStore with the parse columns backed by sc. The
// scratch is emptied (owned by the Store) until Compact returns it, so a
// parse that panics leaves nothing aliased.
func NewStoreOnScratch(hint int, sc *StoreScratch) *Store {
	if hint < 1 {
		hint = 1
	}
	if cap(sc.nodes) < hint+1 {
		sc.nodes = make([]nodeHeader, 0, hint+1)
	}
	if cap(sc.lists) < max(8, hint/7) {
		sc.lists = make([]listHeader, 0, max(8, hint/7))
	}
	if n := hint + hint/2 + hint*3/8; cap(sc.children) < n {
		sc.children = make([]NodeRef, 0, n)
	}
	if cap(sc.internBuf) < hint {
		sc.internBuf = make([]byte, 0, hint)
	}
	if n := max(2, hint/32); cap(sc.internOff) < n {
		sc.internOff = make([]uint32, 0, n)
	}
	s := &Store{
		allocHint: hint,
		nodes:     append(sc.nodes[:0], nodeHeader{}),
		lists:     append(sc.lists[:0], listHeader{}),
		children:  sc.children[:0],
		internBuf: sc.internBuf[:0],
		internOff: append(sc.internOff[:0], 0, 0),
		internIdx: make(map[string]uint32, hint/32),
	}
	*sc = StoreScratch{}
	return s
}

// Compact clones the parse columns into exact-size arrays and returns the
// borrowed backing arrays to sc. Call once, when parsing is finished.
func (s *Store) Compact(sc *StoreScratch) {
	s.mustMutate()
	sc.nodes, s.nodes = s.nodes, cloneExact(s.nodes)
	sc.lists, s.lists = s.lists, cloneExact(s.lists)
	sc.children, s.children = s.children, cloneExact(s.children)
	sc.internBuf, s.internBuf = s.internBuf, cloneExact(s.internBuf)
	sc.internOff, s.internOff = s.internOff, cloneExact(s.internOff)
}

func cloneExact[T any](x []T) []T {
	y := make([]T, len(x))
	copy(y, x)
	return y
}
