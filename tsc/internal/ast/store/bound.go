package store

import "github.com/microsoft/TypeScript/tsc/internal/ast"

// The output of the binder that is not in a node header or a reserved slot
// (store-binder-design-20260922.md 2.3). The types are here so that the Store
// owns them; the writer is internal/storebinder. The resolving layer (Flow,
// FlowList, Node.Symbol, Node.FlowNode, Node.Locals) carries the owner:
// internal/ast/docs/store-checker-design-20260923.md 2.5 (A1, V1).

// FlowRef is an index into Bound.flows. 0 is nil.
type FlowRef uint32

// FlowListRef is an index into Bound.flowLists. 0 is nil.
type FlowListRef uint32

// FlowNode is a row of the flow slab: ast.FlowNode with index links. 16
// bytes, no pointers. Outside the binder a flow is read as a Flow.
type FlowNode struct {
	Flags ast.FlowFlags
	// Node is the AST node, a row of the Bound's Store. For a SwitchClause or
	// ReduceLabel flow it is the index of the FlowData that holds what the
	// Pointer binder packs into a synthetic node.
	Node        NodeRef
	Antecedent  FlowRef     // all but labels
	Antecedents FlowListRef // the antecedents of a label
}

// FlowListEntry is a row of the flow list slab: ast.FlowList with index
// links. 8 bytes. Outside the binder a flow list is read as a FlowList.
type FlowListEntry struct {
	Flow FlowRef
	Next FlowListRef
}

// FlowData is the payload of a synthetic flow node. SwitchClause:
// (switchStatement NodeRef, clauseStart, clauseEnd). ReduceLabel: (target
// FlowRef, antecedents FlowListRef, 0).
type FlowData struct{ A, B, C uint32 }

// Bound is what the binder writes about a file besides the headers and the
// reserved slots: the fields of ast.SourceFile that the binder sets, and the
// slabs that the FlowRef, FlowListRef, SymbolId and locals slot values index.
// The file's own symbol is not here: it is the root header's symbol, as it is
// the root's DeclarationBase.Symbol in the Pointer AST (File.Symbol).
type Bound struct {
	store                   *Store // the Store whose NodeRefs the slabs hold (invariant 21)
	SymbolCount             int
	PatternAmbientModules   []PatternAmbientModule
	GlobalExports           SymbolTable
	CommonJSModuleIndicator NodeRef
	diagnostics             []*ast.Diagnostic // bind diagnostics; file is nil (7c)
	symbols                 []*Symbol         // SymbolId -> *Symbol; [0] is nil
	flows                   []FlowNode        // [0] is a sentinel
	flowLists               []FlowListEntry   // [0] is a sentinel
	flowData                []FlowData        // [0] is a sentinel
	locals                  []SymbolTable     // locals slot value -> table; [0] is nil
	containers              []NodeRef         // the NextContainer chain in declaration order
}

// NewBound has the sentinel rows in place, so that index 0 reads as nil
// without a guard. The Bound and s point at each other.
func NewBound(s *Store) *Bound {
	b := &Bound{
		store:     s,
		symbols:   make([]*Symbol, 1),
		flows:     make([]FlowNode, 1),
		flowLists: make([]FlowListEntry, 1),
		flowData:  make([]FlowData, 1),
		locals:    make([]SymbolTable, 1),
	}
	s.bound = b
	return b
}

// BoundScratch holds the grown arrays of the Bound slabs from one file to
// the next. The binder writes a file's slabs into it and Compact copies them
// to their exact size, as Builder.Finish does for the nodes: append grows a
// slab by 1.25 past 256 elements, so a slab grown from empty allocates about
// 5 times its final length and keeps up to a quarter of it as slack.
type BoundScratch struct {
	symbols    []*Symbol
	flows      []FlowNode
	flowLists  []FlowListEntry
	flowData   []FlowData
	locals     []SymbolTable
	containers []NodeRef
}

// NewBoundIn is NewBound on the arrays of scratch. The Bound must be
// compacted into scratch before scratch starts another file.
func NewBoundIn(scratch *BoundScratch, s *Store) *Bound {
	b := &Bound{
		store:      s,
		symbols:    append(scratch.symbols[:0], nil),
		flows:      append(scratch.flows[:0], FlowNode{}),
		flowLists:  append(scratch.flowLists[:0], FlowListEntry{}),
		flowData:   append(scratch.flowData[:0], FlowData{}),
		locals:     append(scratch.locals[:0], nil),
		containers: scratch.containers[:0],
	}
	s.bound = b
	return b
}

// Compact gives b slabs of the exact size and s the grown arrays back. The
// pointers in s are cleared so that the scratch does not keep the file's
// symbols and tables alive.
func (b *Bound) Compact(s *BoundScratch) {
	s.symbols, b.symbols = b.symbols, exact(b.symbols)
	s.flows, b.flows = b.flows, exact(b.flows)
	s.flowLists, b.flowLists = b.flowLists, exact(b.flowLists)
	s.flowData, b.flowData = b.flowData, exact(b.flowData)
	s.locals, b.locals = b.locals, exact(b.locals)
	s.containers, b.containers = b.containers, exact(b.containers)
	clear(s.symbols)
	clear(s.locals)
}

func exact[T any](x []T) []T {
	if len(x) == 0 {
		return nil
	}
	return append(make([]T, 0, len(x)), x...)
}

// Reads, for the binder and its tests. A *FlowNode, *FlowListEntry or
// *FlowData is valid only until the next append to its slab, so the binder
// does not hold one across a New call. After Seal, a flow is read as a Flow.

func (b *Bound) Store() *Store                         { return b.store }
func (b *Bound) Flow(r FlowRef) *FlowNode              { return &b.flows[r] }
func (b *Bound) FlowList(r FlowListRef) *FlowListEntry { return &b.flowLists[r] }
func (b *Bound) FlowData(i uint32) *FlowData           { return &b.flowData[i] }
func (b *Bound) SymbolOf(id SymbolId) *Symbol          { return b.symbols[id] }
func (b *Bound) Locals(i uint32) SymbolTable           { return b.locals[i] }
func (b *Bound) Containers() []NodeRef                 { return b.containers }
func (b *Bound) BindDiagnostics() []*ast.Diagnostic    { return b.diagnostics }

// Writes, for the binder.

func (b *Bound) NewFlow(flags ast.FlowFlags, node NodeRef, antecedent FlowRef) FlowRef {
	r := FlowRef(len(b.flows))
	b.flows = append(b.flows, FlowNode{Flags: flags, Node: node, Antecedent: antecedent})
	return r
}

func (b *Bound) NewFlowList(flow FlowRef, next FlowListRef) FlowListRef {
	r := FlowListRef(len(b.flowLists))
	b.flowLists = append(b.flowLists, FlowListEntry{Flow: flow, Next: next})
	return r
}

func (b *Bound) NewFlowData(a, bb, c uint32) uint32 {
	i := uint32(len(b.flowData))
	b.flowData = append(b.flowData, FlowData{a, bb, c})
	return i
}

// AddSymbol gives s its id (its index, 1-based) and returns it.
func (b *Bound) AddSymbol(s *Symbol) SymbolId {
	s.id = SymbolId(len(b.symbols))
	b.symbols = append(b.symbols, s)
	return s.id
}

// NewLocals makes a table and returns its index, the value of a locals slot.
func (b *Bound) NewLocals() uint32 {
	i := uint32(len(b.locals))
	b.locals = append(b.locals, make(SymbolTable))
	return i
}

func (b *Bound) AddContainer(ref NodeRef)        { b.containers = append(b.containers, ref) }
func (b *Bound) AddDiagnostic(d *ast.Diagnostic) { b.diagnostics = append(b.diagnostics, d) }

// What the tests and benchmarks measure. Nothing else reads these.

// FlowCounts is the length of the flow slabs, sentinels included.
func (b *Bound) FlowCounts() (flows, flowLists, flowData int) {
	return len(b.flows), len(b.flowLists), len(b.flowData)
}

// Footprint is the size of the slabs in bytes: the symbol table (8 per id),
// the flow slabs, the locals table (8 per slot) and the containers. The
// symbols themselves and the maps are not counted.
func (b *Bound) Footprint() int {
	return len(b.symbols)*8 + len(b.flows)*16 + len(b.flowLists)*8 + len(b.flowData)*12 + len(b.locals)*8 + len(b.containers)*4
}

// Flow is a flow node with its owner, the resolving layer's *ast.FlowNode
// (V1): the Node, the antecedents and the FlowData of the flow are read in
// the Bound it carries, never in the Store of the node that led to it. The
// zero Flow is nil. 16 bytes. Every Flow is canonical: a non-nil one is its
// Bound and index, and every nil is the zero Flow, so == and a map key are the
// Pointer ones, nil included. A resolver that reads a FlowRef therefore
// returns Flow{} for 0, never a Bound with 0 (TestStorageFlowGraph checks
// every resolver against the Pointer graph).
type Flow struct {
	bound *Bound
	ref   FlowRef
}

func (f Flow) IsNil() bool          { return f.ref == 0 }
func (f Flow) Flags() ast.FlowFlags { return f.bound.flows[f.ref].Flags }

// Node is the flow's node, in the Bound's Store. A SwitchClause or
// ReduceLabel flow has none: it has SwitchClause or ReduceLabel.
func (f Flow) Node() Node {
	row := &f.bound.flows[f.ref]
	if storeChecks && row.Flags&(ast.FlowFlagsSwitchClause|ast.FlowFlagsReduceLabel) != 0 {
		panic("store: Node of a SwitchClause or ReduceLabel flow")
	}
	return f.bound.store.node(row.Node)
}

func (f Flow) Antecedent() Flow {
	r := f.bound.flows[f.ref].Antecedent
	if r == 0 {
		return Flow{}
	}
	return Flow{f.bound, r}
}

func (f Flow) Antecedents() FlowList {
	return FlowList{f.bound, f.bound.flows[f.ref].Antecedents}
}

// SwitchClause is the FlowSwitchClauseData of a SwitchClause flow.
func (f Flow) SwitchClause() (switchStatement Node, clauseStart, clauseEnd int) {
	d := &f.bound.flowData[f.bound.flows[f.ref].Node]
	return f.bound.store.node(NodeRef(d.A)), int(d.B), int(d.C)
}

// ReduceLabel is the FlowReduceLabelData of a ReduceLabel flow.
func (f Flow) ReduceLabel() (target Flow, antecedents FlowList) {
	d := &f.bound.flowData[f.bound.flows[f.ref].Node]
	antecedents = FlowList{f.bound, FlowListRef(d.B)}
	if d.A == 0 {
		return Flow{}, antecedents
	}
	return Flow{f.bound, FlowRef(d.A)}, antecedents
}

// FlowList is an *ast.FlowList with its owner. The zero FlowList is nil.
type FlowList struct {
	bound *Bound
	ref   FlowListRef
}

func (l FlowList) IsNil() bool    { return l.ref == 0 }
func (l FlowList) Next() FlowList { return FlowList{l.bound, l.bound.flowLists[l.ref].Next} }

func (l FlowList) Flow() Flow {
	r := l.bound.flowLists[l.ref].Flow
	if r == 0 {
		return Flow{}
	}
	return Flow{l.bound, r}
}

// The resolving layer of the header and the reserved slots (A1, V1): Pointer's
// names, answered by the owner of the node. A source Store reads its Bound; a
// synthetic Store reads its private tables; a Store with neither (a lazy
// JSDoc Store, or a source Store before its bind) has only zeros and answers
// nil. One branch, on s.bound.

// The role accessors of the reserved slots (Locals, LocalSymbol, EndFlowNode,
// ReturnFlowNode, FallthroughFlowNode) are generated on the same functions.

func (n Node) Symbol() *Symbol { return n.s.symbolAt(uint32(n.h.symbol)) }
func (n Node) FlowNode() Flow  { return n.s.flowAt(uint32(n.h.flow)) }

func (s *Store) symbolAt(i uint32) *Symbol {
	if b := s.bound; b != nil {
		s.checkResolve()
		return b.symbols[i]
	}
	return s.private.symbolAt(i)
}

// flowAt drops the Bound of a 0 by a select, not the early return of the
// other resolvers: many nodes have no flow, so a branch here would be
// unpredictable.
func (s *Store) flowAt(i uint32) Flow {
	b := s.bound
	if b == nil {
		return s.private.flowAt(i)
	}
	s.checkResolve()
	if i == 0 {
		b = nil
	}
	return Flow{b, FlowRef(i)}
}

// localsAt answers nil for slot 0, which is also what LocalsRef gives a kind
// without the slot, without reading a table.
func (s *Store) localsAt(i uint32) SymbolTable {
	if i == 0 {
		return nil
	}
	if b := s.bound; b != nil {
		s.checkResolve()
		return b.locals[i]
	}
	return s.private.localsAt(i)
}

// checkResolve: the resolving layer reads a source Store only after its bind,
// when the slabs no longer move.
func (s *Store) checkResolve() {
	if storeChecks && !s.sealed {
		panic("store: resolving read before Seal")
	}
}
