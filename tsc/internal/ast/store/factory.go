package store

import (
	"strings"

	"github.com/microsoft/TypeScript/tsc/internal/ast"
)

// The synthetic Stores: the nodes that a checker makes (R, K3, V1 in
// internal/ast/docs/store-checker-design-20260923.md 2.5 and 2.6, and
// store-checker-execution-design-20260924.md 1, 3 to 5 and 8). A Factory
// fills a Store of fixed node capacity and adds a new one when it is full: a
// published header never moves and a StoreID is never reused. There is no
// table of all the Stores, and a Store refers to no Factory, NodeRegion or
// checker, so a Store that no Node, cache or foreign entry reaches is garbage.
//
// An edge to a node of another Store is a foreign entry of the Store that
// holds the edge (FR1): New writes one for each child in another Store, and
// SetParent writes one into the child's Store when the parent is elsewhere,
// the old Store of an earlier node included.

// regionToken is the identity of a region. It is not zero-size, so that two
// tokens never share an address.
type regionToken struct{ _ byte }

// NodeRegion is the set of synthetic Stores of one checker. Its Factories
// (the checker's and the node builder's) share it but not their active Store
// or hooks.
type NodeRegion struct {
	token *regionToken
	space *IDSpace
	// clones holds the source and the result of every CloneNodeList whose
	// list is writable, in a storeChecks build only: SetListElement refuses
	// both (K3, see CloneNodeList).
	clones map[listKey]struct{}
}

type listKey struct {
	s  *Store
	at ListRef
}

func NewNodeRegion(space *IDSpace) *NodeRegion {
	if space == nil {
		panic("store: a region belongs to a program")
	}
	return &NodeRegion{token: &regionToken{}, space: space}
}

// checkImport is invariant 26: a node or list of s may be the target of an
// edge of this region only if s is a source or lazy JSDoc Store of the same
// program, or a synthetic Store of this region. A StoreID is unique only
// within a program, so a node of another program would collide in NodeKey;
// a node of another region (another checker) is private to it.
func (r *NodeRegion) checkImport(s *Store) {
	if s.owner == r.token || s.owner == nil && s.space == r.space {
		return
	}
	panic("store: a node of another program or region")
}

// FactoryHooks is ast.NodeFactoryHooks.
type FactoryHooks struct {
	OnCreate func(node Node)           // Hooks the creation of a node.
	OnUpdate func(node, original Node) // Hooks the updating of a node.
	OnClone  func(node, original Node) // Hooks the cloning of a node.
}

// Factory is ast.NodeFactory for a region. New, Update and Clone are
// generated (factory_generated.go); the writers are below.
type Factory struct {
	region   *NodeRegion
	active   *Store
	capacity int // real rows per Store
	hooks    FactoryHooks
	// The initial capacity of extra and texts of a new Store. Only the tests
	// of this package set them, to force the reallocation of the columns.
	extraCap, textsCap int
	nodeCount          int
	textCount          int
}

// NewFactory makes a Factory whose Stores hold capacity nodes each. The
// capacity is a performance knob: the tests run with 1 to 3 to force a
// Store boundary between any two nodes.
func NewFactory(r *NodeRegion, capacity int, hooks FactoryHooks) *Factory {
	if capacity < 1 || capacity >= foreignBit {
		panic("store: invalid Store capacity")
	}
	return &Factory{region: r, capacity: capacity, hooks: hooks, extraCap: 16 * capacity, textsCap: 8 * capacity}
}

func (f *Factory) NodeCount() int { return f.nodeCount }
func (f *Factory) TextCount() int { return f.textCount }

// ReleaseArenas is ast.NodeFactory.ReleaseArenas: the Factory lets go of its
// active Store, and the next New starts a new one. The old Stores are not
// cleared, sealed or renumbered: a Node made before stays valid, readable and
// writable, and keeps its Store alive.
func (f *Factory) ReleaseArenas() { f.active = nil }

// privateTables are the flows, symbols and locals of the nodes of a
// synthetic Store: the header's flow and symbol words and the locals slot
// index them (V1). They are scanned, so the header and extra hold no
// pointers. Index 0 is nil in each. texts is the append-only buffer of the
// Store's texts: a text never moves once written, so a string read before a
// later write keeps its value.
type privateTables struct {
	flows   []Flow
	symbols []*Symbol
	locals  []SymbolTable
	texts   strings.Builder
}

// A nil *privateTables (a source Store before its bind, or a lazy JSDoc
// Store) has only index 0 to read.

func (p *privateTables) symbolAt(i uint32) *Symbol {
	if i == 0 {
		return nil
	}
	return p.symbols[i]
}

func (p *privateTables) flowAt(i uint32) Flow {
	if i == 0 {
		return Flow{}
	}
	return p.flows[i]
}

func (p *privateTables) localsAt(i uint32) SymbolTable {
	return p.locals[i]
}

func (f *Factory) newStore() *Store {
	s := &Store{
		nodes:   make([]NodeHeader, 1, 1+f.capacity),
		extra:   make([]uint32, 3, max(f.extraCap, 3)),
		id:      f.region.space.Next(),
		private: &privateTables{flows: make([]Flow, 1), symbols: make([]*Symbol, 1), locals: make([]SymbolTable, 1)},
		owner:   f.region.token,
		space:   f.region.space,
	}
	s.private.texts.Grow(f.textsCap)
	return s
}

// store is the Store that takes a new list block: the active Store, made on
// first use.
func (f *Factory) store() *Store {
	if f.active == nil {
		f.active = f.newStore()
	}
	return f.active
}

// row is the Store that takes a new node: the active Store unless it is
// full. The caller picks it once, before it writes anything, and writes the
// whole node there.
func (f *Factory) row() *Store {
	if s := f.active; s != nil && len(s.nodes) < cap(s.nodes) {
		return s
	}
	f.active = f.newStore()
	return f.active
}

// newNode appends the header of a node whose payload is already written, then
// calls the hook. It returns the Node of the row even if the hook makes other
// nodes (and changes the active Store).
func (f *Factory) newNode(s *Store, kind ast.Kind, flags ast.NodeFlags, mod ast.ModifierFlags, data uint32) Node {
	if kind == ast.KindUnknown {
		panic("store: a node of kind Unknown")
	}
	s.nodes = append(s.nodes, NodeHeader{kind: kind, modifierFlags: uint16(mod), flags: flags, pos: -1, end: -1, data: data})
	n := Node{s: s, h: &s.nodes[len(s.nodes)-1]}
	f.nodeCount++
	if f.hooks.OnCreate != nil {
		f.hooks.OnCreate(n)
	}
	return n
}

// edge encodes v as an edge of dst: 0 for nil (whatever Store the nil comes
// from, and without reading it), the row for a node of dst, else a new
// foreign entry of dst. A node of another program or region panics.
func (r *NodeRegion) edge(dst *Store, v Node) NodeRef {
	if v.IsNil() {
		return NoNodeRef
	}
	if v.s == dst {
		return v.Ref()
	}
	r.checkImport(v.s)
	i := len(dst.foreignNodes)
	if i >= foreignBit {
		panic("store: foreignNodes is full")
	}
	dst.foreignNodes = append(dst.foreignNodes, v)
	return NodeRef(i) | foreignBit
}

// listEdge is edge for a list slot.
func (r *NodeRegion) listEdge(dst *Store, l List) ListRef {
	if l.IsNil() {
		return NoListRef
	}
	if l.s == dst {
		return l.at
	}
	r.checkImport(l.s)
	i := len(dst.foreignLists)
	if i >= foreignBit {
		panic("store: foreignLists is full")
	}
	dst.foreignLists = append(dst.foreignLists, l)
	return ListRef(i) | foreignBit
}

// text writes a text member in s's texts: the [offset, length] pair, offset
// marked textInTexts. A synthetic Store has no source.
func (f *Factory) text(s *Store, text string) (off, length uint32) {
	f.textCount++
	b := &s.private.texts
	off = textInTexts | uint32(b.Len())
	b.WriteString(text)
	s.texts = b.String()
	return off, uint32(len(text))
}

// identifierData is the data word of a synthetic Identifier: textInTexts and
// the index of its [offset, length] pair in extra.
func (f *Factory) identifierData(s *Store, text string) uint32 {
	off, length := f.text(s, text)
	at := uint32(len(s.extra))
	s.extra = append(s.extra, off, length)
	return textInTexts | at
}

// modifierFlags is ast.ModifiersToFlags over a list of any owner.
func modifierFlags(l List) ast.ModifierFlags {
	var flags ast.ModifierFlags
	if l.IsNil() {
		return flags
	}
	for i := range l.Len() {
		flags |= ast.ModifierToFlag(l.At(i).Kind())
	}
	return flags
}

// updateNode is ast.updateNode: an updated node takes the flags and range of
// the original.
func (f *Factory) updateNode(updated, original Node) Node {
	if !SameNode(updated, original) {
		updated.h.flags = original.h.flags
		updated.h.pos, updated.h.end = original.h.pos, original.h.end
		if f.hooks.OnUpdate != nil {
			f.hooks.OnUpdate(updated, original)
		}
	}
	return updated
}

// cloneNode is ast.cloneNode: a shallow copy shares the children and lists of
// the original, and takes neither its parent nor its flow, symbol or locals.
func (f *Factory) cloneNode(updated, original Node) Node {
	f.updateNode(updated, original)
	if !SameNode(updated, original) && f.hooks.OnClone != nil {
		f.hooks.OnClone(updated, original)
	}
	return updated
}

// NewNodeList is ast.NodeFactory.NewNodeList: a block in the active Store,
// with the undefined range.
func (f *Factory) NewNodeList(nodes []Node) List {
	return f.newList(nodes, -1, -1)
}

// NewModifierList is ast.NodeFactory.NewModifierList. The modifier flags are
// the header's, folded when the node is made.
func (f *Factory) NewModifierList(nodes []Node) List {
	return f.NewNodeList(nodes)
}

func (f *Factory) newList(nodes []Node, pos, end int32) List {
	s := f.store()
	at := ListRef(len(s.extra))
	if at >= foreignBit {
		panic("store: extra is full")
	}
	s.extra = append(s.extra, uint32(len(nodes)), uint32(pos), uint32(end))
	for _, n := range nodes {
		// edge may append to the foreign table only, not to extra.
		s.extra = append(s.extra, uint32(f.region.edge(s, n)))
	}
	return List{s, at}
}

// CloneNodeList is ast.NodeList.Clone (K3, invariant 19).
//
// The Pointer NodeList.Clone shares the element slice with the original;
// this one copies the element edges into a new block. No code in the reach of
// the checker relies on a write to the elements of one being seen through
// the other (surveyed 2026-09-24: the only in-place element writes,
// nodebuilderimpl.go:3104-3115, are to a list the node builder has just made
// itself). In the Pointer AST a write to either the clone or the original is
// seen by the other, so a storeChecks build records both, and SetListElement
// panics on either: if code that relies on the sharing appears, it fails
// there instead of silently diverging.
func CloneNodeList(f *Factory, l List) List {
	nodes := make([]Node, l.Len())
	for i := range nodes {
		nodes[i] = l.At(i)
	}
	result := f.newList(nodes, l.Pos(), l.End())
	if storeChecks {
		r := f.region
		if r.clones == nil {
			r.clones = make(map[listKey]struct{})
		}
		r.clones[listKey{result.s, result.at}] = struct{}{}
		// A source or sealed list is refused by every writer already.
		if l.s.owner == r.token && !l.s.sealed {
			r.clones[listKey{l.s, l.at}] = struct{}{}
		}
	}
	return result
}

// The writers. Each writes the Store of its node argument, not the active
// Store, and panics in every build unless that Store is a synthetic Store of
// this Factory's region that is not sealed: a source Store, a lazy JSDoc
// Store and another checker's Store are read-only here (invariants 17 and
// 8), and so is the nil node.

func (f *Factory) checkWritable(n Node) {
	if n.IsNil() {
		panic("store: write to the nil node")
	}
	f.checkWritableStore(n.s)
}

func (f *Factory) checkWritableStore(s *Store) {
	if s.owner != f.region.token || s.sealed {
		panic("store: write to a Store that is not a synthetic Store of this region")
	}
}

// SetParent is child.Parent = parent. It writes the child only; parent may be
// in any Store the region may import, and nil.
func (f *Factory) SetParent(child, parent Node) {
	f.checkWritable(child)
	child.h.parent = f.region.edge(child.s, parent)
}

// SetRange is n.Loc = core.NewTextRange(pos, end).
func (f *Factory) SetRange(n Node, pos, end int32) {
	f.checkWritable(n)
	n.h.pos, n.h.end = pos, end
}

func (f *Factory) AddFlags(n Node, flags ast.NodeFlags) {
	f.checkWritable(n)
	n.h.flags |= flags
}

// SetNodeFlow is n.FlowNodeData().FlowNode = flow: the owner-carrying Flow in
// the Store's private table (V1). A flow is a source Bound's, so its Store
// must belong to the program.
func (f *Factory) SetNodeFlow(n Node, flow Flow) {
	f.checkWritable(n)
	if flow.IsNil() {
		n.h.flow = 0
		return
	}
	f.region.checkImport(flow.bound.store)
	p := n.s.private
	n.h.flow = FlowRef(len(p.flows))
	p.flows = append(p.flows, flow)
}

// SetNodeSymbol is n.DeclarationData().Symbol = s.
func (f *Factory) SetNodeSymbol(n Node, s *Symbol) {
	f.checkWritable(n)
	if s == nil {
		n.h.symbol = 0
		return
	}
	p := n.s.private
	n.h.symbol = SymbolId(len(p.symbols))
	p.symbols = append(p.symbols, s)
}

// SetLocals is n.LocalsContainerData().Locals = locals. The table is kept,
// not copied: a key added to it later is seen through n.Locals(). A kind
// without locals panics, as the Pointer LocalsContainerData does.
func (f *Factory) SetLocals(n Node, locals SymbolTable) {
	f.checkWritable(n)
	slot := localsSlot[n.h.kind&511]
	if slot == 0xFF {
		panic("store: " + n.h.kind.String() + " is not a locals container")
	}
	i := uint32(0)
	if locals != nil {
		p := n.s.private
		i = uint32(len(p.locals))
		p.locals = append(p.locals, locals)
	}
	n.s.extra[int(n.h.data)+int(slot)] = i
}

// SetListElement is l.Nodes[i] = n, in the list's owner.
func (f *Factory) SetListElement(l List, i int, n Node) {
	if l.IsNil() {
		panic("store: write to the nil list")
	}
	f.checkWritableStore(l.s)
	if storeChecks {
		// K3: see CloneNodeList. The Pointer clone and original share their
		// elements; here they do not, so a write to either is refused.
		if _, ok := f.region.clones[listKey{l.s, l.at}]; ok {
			panic("store: SetListElement on a cloned list or the original of one (K3)")
		}
	}
	elems := l.edges()
	elems[i] = f.region.edge(l.s, n)
}
