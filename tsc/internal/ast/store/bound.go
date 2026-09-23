package store

import "github.com/microsoft/TypeScript/tsc/internal/ast"

// The output of the binder that is not in a node header or a reserved slot
// (store-binder-design-20260922.md 2.3). The types are here so that the Store
// owns them; the writer is internal/storebinder.

// FlowRef is an index into Bound.flows. 0 is nil.
type FlowRef uint32

// FlowListRef is an index into Bound.flowLists. 0 is nil.
type FlowListRef uint32

// FlowNode is ast.FlowNode with index links. 16 bytes, no pointers.
type FlowNode struct {
	Flags ast.FlowFlags
	// Node is the AST node. For a SwitchClause or ReduceLabel flow it is the
	// index of the FlowData that holds what the Pointer binder packs into a
	// synthetic node.
	Node        NodeRef
	Antecedent  FlowRef     // all but labels
	Antecedents FlowListRef // the antecedents of a label
}

// FlowList is ast.FlowList with index links. 8 bytes.
type FlowList struct {
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
	SymbolCount             int
	PatternAmbientModules   []PatternAmbientModule
	GlobalExports           SymbolTable
	CommonJSModuleIndicator NodeRef
	diagnostics             []*ast.Diagnostic // bind diagnostics; file is nil (7c)
	symbols                 []*Symbol         // SymbolId -> *Symbol; [0] is nil
	flows                   []FlowNode        // [0] is a sentinel
	flowLists               []FlowList        // [0] is a sentinel
	flowData                []FlowData        // [0] is a sentinel
	locals                  []SymbolTable     // locals slot value -> table; [0] is nil
	containers              []NodeRef         // the NextContainer chain in declaration order
}

// NewBound has the sentinel rows in place, so that index 0 reads as nil
// without a guard.
func NewBound() *Bound {
	return &Bound{
		symbols:   make([]*Symbol, 1),
		flows:     make([]FlowNode, 1),
		flowLists: make([]FlowList, 1),
		flowData:  make([]FlowData, 1),
		locals:    make([]SymbolTable, 1),
	}
}

// Reads. A *FlowNode, *FlowList or *FlowData is valid only until the next
// append to its slab, so the binder does not hold one across a New call.

func (b *Bound) Flow(r FlowRef) *FlowNode           { return &b.flows[r] }
func (b *Bound) FlowList(r FlowListRef) *FlowList   { return &b.flowLists[r] }
func (b *Bound) FlowData(i uint32) *FlowData        { return &b.flowData[i] }
func (b *Bound) SymbolOf(id SymbolId) *Symbol       { return b.symbols[id] }
func (b *Bound) Locals(i uint32) SymbolTable        { return b.locals[i] }
func (b *Bound) Containers() []NodeRef              { return b.containers }
func (b *Bound) BindDiagnostics() []*ast.Diagnostic { return b.diagnostics }

// Writes, for the binder.

func (b *Bound) NewFlow(flags ast.FlowFlags, node NodeRef, antecedent FlowRef) FlowRef {
	r := FlowRef(len(b.flows))
	b.flows = append(b.flows, FlowNode{Flags: flags, Node: node, Antecedent: antecedent})
	return r
}

func (b *Bound) NewFlowList(flow FlowRef, next FlowListRef) FlowListRef {
	r := FlowListRef(len(b.flowLists))
	b.flowLists = append(b.flowLists, FlowList{Flow: flow, Next: next})
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
