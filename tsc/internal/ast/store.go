package ast

import (
	"sync/atomic"
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
	tokenFlags TokenFlags
	pos        int32
	end        int32
	parent     NodeRef
	childStart uint32
	childLen   uint32
	identText  uint32
	listStart  uint32
	listLen    uint32
}

type listHeader struct {
	pos   int32
	end   int32
	start uint32
	len   uint32
}

// Store owns the long-lived tree. A Store has one writer at a time and does
// not synchronize its node, list, intern, or side-map mutations. Compiler
// phases transfer exclusive ownership of a file's Store; parallel work must
// write different Stores. Readers may run concurrently only while no writer
// is active. NewFactoryOn does not relax this rule.
//
// StoreSet is separately synchronized for cross-file registration and lookup.
// After Seal, node/list/intern backing arrays are pointer-free (noscan).
// Sparse side maps (symbols) remain scannable on purpose: only declaration
// nodes use them.
type Store struct {
	id            atomic.Uint32 // StoreID assigned by StoreSet.Add; 0 until registered
	nodes         []nodeHeader
	lists         []listHeader
	children      []NodeRef
	listSlots     []ListRef
	internBuf     []byte
	internOff     []uint32 // intern id i occupies internBuf[internOff[i]:internOff[i+1]]
	internIdx     map[string]uint32
	symbols       map[NodeRef]*Symbol
	localSymbols  map[NodeRef]*Symbol
	flows         map[NodeRef]*FlowNode
	endFlows      map[NodeRef]*FlowNode
	returnFlows   map[NodeRef]*FlowNode
	locals        map[NodeRef]SymbolTable
	nextContainer map[NodeRef]NodeRef
	scalarValues  map[uint64]uint64 // packed NodeRef/value-slot key; pointer-free
	stringValues  map[uint64]uint32 // intern ids keyed by NodeRef/value-slot
	objectValues  map[uint64]any    // sparse pointer/slice kind-specific values
	externalChild map[uint64]GlobalRef
	externalList  map[uint64]GlobalRef
	sourceFile    *SourceFile // metadata owner; SourceFile fields stay outside Store
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
	return s.AllocSlots(kind, flags, loc, childLen, 0)
}

func (s *Store) AllocSlots(kind Kind, flags NodeFlags, loc core.TextRange, childLen, listLen int) Handle {
	if s == nil {
		panic("ast: Alloc on nil Store")
	}
	if childLen < 0 {
		panic("ast: negative childLen")
	}
	if listLen < 0 {
		panic("ast: negative listLen")
	}
	if len(s.nodes) >= int(^NodeRef(0)) {
		panic("ast: Store exhausted")
	}
	id := NodeRef(len(s.nodes))
	start := uint32(len(s.children))
	if childLen > 0 {
		s.children = append(s.children, make([]NodeRef, childLen)...)
	}
	listStart := uint32(len(s.listSlots))
	if listLen > 0 {
		s.listSlots = append(s.listSlots, make([]ListRef, listLen)...)
	}
	s.nodes = append(s.nodes, nodeHeader{
		kind:       kind,
		flags:      flags,
		pos:        int32(loc.Pos()),
		end:        int32(loc.End()),
		childStart: start,
		childLen:   uint32(childLen),
		listStart:  listStart,
		listLen:    uint32(listLen),
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
	id := uint32(len(s.internOff) - 1)
	s.internBuf = append(s.internBuf, text...)
	s.internOff = append(s.internOff, uint32(len(s.internBuf)))
	if s.internIdx != nil {
		s.internIdx[text] = id
	}
	return id
}

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

func (s *Store) SetSourceFile(file *SourceFile) {
	if s == nil {
		panic("ast: SetSourceFile on nil Store")
	}
	s.sourceFile = file
}

func (s *Store) SourceFile() *SourceFile {
	if s == nil {
		return nil
	}
	return s.sourceFile
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

func (s *Store) ListHasTrailingComma(list ListRef) bool {
	n := s.ListLen(list)
	if n == 0 {
		return false
	}
	last := s.ListAt(list, n-1)
	return last.Loc().End() < s.ListLoc(list).End()
}

func (s *Store) setListLoc(list ListRef, loc core.TextRange) {
	if list == 0 || s == nil {
		return
	}
	s.lists[list].pos = int32(loc.Pos())
	s.lists[list].end = int32(loc.End())
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

func (s *Store) SetExternalListAt(list ListRef, i int, child GlobalRef) {
	if list == 0 || s == nil {
		panic("ast: SetExternalListAt on missing list")
	}
	l := &s.lists[list]
	if i < 0 || i >= int(l.len) {
		panic("ast: list index out of range")
	}
	key := uint64(list)<<32 | uint64(uint32(i))
	if child == 0 {
		delete(s.externalList, key)
		return
	}
	if s.children[int(l.start)+i] != 0 {
		panic("ast: external list child conflicts with local child")
	}
	if s.externalList == nil {
		s.externalList = make(map[uint64]GlobalRef)
	}
	s.externalList[key] = child
}

func (s *Store) ExternalListAt(list ListRef, i int) GlobalRef {
	if list == 0 || s == nil {
		return 0
	}
	l := &s.lists[list]
	if i < 0 || i >= int(l.len) {
		panic("ast: list index out of range")
	}
	return s.externalList[uint64(list)<<32|uint64(uint32(i))]
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

func (s *Store) SetLocalSymbol(ref NodeRef, sym *Symbol) {
	if s == nil || ref == 0 {
		return
	}
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
	if flow == nil {
		delete(s.flows, ref)
		return
	}
	if s.flows == nil {
		s.flows = make(map[NodeRef]*FlowNode)
	}
	s.flows[ref] = flow
}

func (s *Store) Flow(ref NodeRef) *FlowNode {
	if s == nil || ref == 0 {
		return nil
	}
	return s.flows[ref]
}

func (s *Store) SetEndFlow(ref NodeRef, flow *FlowNode) {
	if s == nil || ref == 0 {
		return
	}
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
	key := h.valueKey(slot)
	if h.s.scalarValues == nil {
		h.s.scalarValues = make(map[uint64]uint64)
	}
	h.s.scalarValues[key] = value
}

func (h Handle) UintValue(slot int) uint64 {
	return h.s.scalarValues[h.valueKey(slot)]
}

func (h Handle) SetStringValue(slot int, value string) {
	key := h.valueKey(slot)
	if value == "" {
		delete(h.s.stringValues, key)
		return
	}
	if h.s.stringValues == nil {
		h.s.stringValues = make(map[uint64]uint32)
	}
	h.s.stringValues[key] = h.s.Intern(value)
}

func (h Handle) StringValue(slot int) string {
	id := h.s.stringValues[h.valueKey(slot)]
	if id == 0 {
		return ""
	}
	return h.s.internText(id)
}

func (h Handle) SetObjectValue(slot int, value any) {
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

func (h Handle) TokenFlags() TokenFlags {
	h.mustLive()
	return h.s.nodes[h.id].tokenFlags
}

func (h Handle) SetTokenFlags(flags TokenFlags) {
	h.mustLive()
	h.s.nodes[h.id].tokenFlags = flags
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

func (h Handle) SetExternalChild(i int, child GlobalRef) {
	h.mustLive()
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
		h.s.externalChild = make(map[uint64]GlobalRef)
	}
	h.s.externalChild[key] = child
}

func (h Handle) ExternalChild(i int) GlobalRef {
	h.mustLive()
	n := &h.s.nodes[h.id]
	if i < 0 || i >= int(n.childLen) {
		panic("ast: child index out of range")
	}
	return h.s.externalChild[h.valueKey(i)]
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
	return h.s.listSlots[int(n.listStart)+i]
}

func (h Handle) SetListSlot(i int, list ListRef) {
	h.mustLive()
	n := &h.s.nodes[h.id]
	if i < 0 || i >= int(n.listLen) {
		panic("ast: list slot out of range")
	}
	h.s.listSlots[int(n.listStart)+i] = list
}

func (h Handle) List() ListRef {
	if h.id == 0 || h.s == nil {
		return 0
	}
	n := &h.s.nodes[h.id]
	if n.listLen == 0 {
		return 0
	}
	return h.s.listSlots[n.listStart]
}

func (h Handle) SetList(list ListRef) {
	h.mustLive()
	n := &h.s.nodes[h.id]
	if n.listLen == 0 {
		panic("ast: SetList on node with no list slots")
	}
	h.s.listSlots[n.listStart] = list
}

// ForEachChild visits non-zero named children, then each list slot. true stops.
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
	for slot := range int(n.listLen) {
		list := h.s.listSlots[int(n.listStart)+slot]
		if list == 0 {
			continue
		}
		l := &h.s.lists[list]
		for i := range int(l.len) {
			ref := h.s.children[int(l.start)+i]
			if ref == 0 {
				continue
			}
			if v(Handle{s: h.s, id: ref}) {
				return true
			}
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
