package storebinder_test

import (
	"fmt"
	"regexp"
	"slices"

	"github.com/microsoft/TypeScript/tsc/internal/ast"
	"github.com/microsoft/TypeScript/tsc/internal/ast/store"
	"github.com/microsoft/TypeScript/tsc/internal/ast/store/storetest"
)

// The driver that compares the output of the two binders on the node pairs of
// storetest.Pairs (store-binder-implementation-instructions-20260922.md 6.1):
// the flags after the bind, the symbol, LocalSymbol and Locals of every node,
// the flow graph from every node's flow, EndFlowNode, ReturnFlowNode and
// FallthroughFlowNode, and the file's fields. Symbols and flows are paired
// once (memo), so a shared or cyclic graph is walked once and a Pointer
// symbol paired with two Store symbols is a mismatch.

type bindComparer struct {
	storetest.Mismatches
	opts       storetest.Options
	pointer    *ast.SourceFile
	file       *store.File
	s          *store.Store
	bound      *store.Bound
	symbols    map[*ast.Symbol]*store.Symbol
	symbolsRev map[*store.Symbol]*ast.Symbol
	flows      map[*ast.FlowNode]store.FlowRef
	flowsRev   map[store.FlowRef]*ast.FlowNode
}

// The ids in symbol names that the two binders assign differently: the class
// symbol id of a private name (#<id>@name) and the node id of a pattern
// ambient module with attributes (pattern@<id>). Nothing else is normalized.
var (
	privateNameId = regexp.MustCompile(`#[0-9]+@`)
	patternId     = regexp.MustCompile(`pattern@[0-9:]+`)
)

func normalizeName(name string) string {
	return patternId.ReplaceAllString(privateNameId.ReplaceAllString(name, "#@"), "pattern@")
}

func (m *bindComparer) add(owner *ast.Node, label string) {
	m.Mismatches[owner.Kind.String()+" "+label]++
}

// node compares a node reference: nil against the sentinel, else kind, pos and end.
func (m *bindComparer) node(label string, owner *ast.Node, p *ast.Node, n store.Node) {
	if p == nil || n.IsNil() {
		if (p == nil) != n.IsNil() {
			m.add(owner, label)
		}
		return
	}
	if n.Kind() != p.Kind || int(n.Pos()) != p.Pos() || int(n.End()) != p.End() {
		m.add(owner, label)
	}
}

func (m *bindComparer) ref(label string, owner *ast.Node, p *ast.Node, r store.Ref) {
	if r != (store.Ref{}) && r.File() != m.file.Root().FileRef().File() {
		m.add(owner, label+" (Ref.file)")
	}
	m.node(label, owner, p, m.s.Node(r.Id()))
}

func (m *bindComparer) symbol(label string, owner *ast.Node, p *ast.Symbol, s *store.Symbol) {
	if p == nil || s == nil {
		if (p == nil) != (s == nil) {
			m.add(owner, label)
		}
		return
	}
	if prev, ok := m.symbols[p]; ok {
		if prev != s {
			m.add(owner, label+" (symbol paired twice)")
		}
		return
	}
	if prev, ok := m.symbolsRev[s]; ok && prev != p {
		m.add(owner, label+" (symbol paired twice)")
		return
	}
	m.symbols[p] = s
	m.symbolsRev[s] = p
	if normalizeName(p.Name) != normalizeName(s.Name) {
		m.add(owner, label+" Name")
	}
	if p.Flags != s.Flags {
		m.add(owner, label+" Flags")
	}
	if p.CheckFlags != s.CheckFlags {
		m.add(owner, label+" CheckFlags")
	}
	if len(p.Declarations) != len(s.Declarations) {
		m.add(owner, label+" Declarations")
	} else {
		for i, d := range p.Declarations {
			m.ref(label+" Declarations", owner, d, s.Declarations[i])
		}
	}
	m.ref(label+" ValueDeclaration", owner, p.ValueDeclaration, s.ValueDeclaration)
	m.symbol(label+" Parent", owner, p.Parent, s.Parent)
	m.symbol(label+" ExportSymbol", owner, p.ExportSymbol, s.ExportSymbol)
	m.table(label+" Members", owner, p.Members, s.Members)
	m.table(label+" Exports", owner, p.Exports, s.Exports)
}

// table compares the key sets and the values. A key is matched after the id
// normalization of the names; when several Store keys normalize alike (two
// pattern ambient modules that differ only in their attributes), the one
// whose first declaration is the Pointer symbol's is taken.
func (m *bindComparer) table(label string, owner *ast.Node, p ast.SymbolTable, s store.SymbolTable) {
	if (p == nil) != (s == nil) {
		m.add(owner, label+" (nil table)")
	}
	normalized := make(map[string][]*store.Symbol, len(s))
	for name, sv := range s {
		key := normalizeName(name)
		normalized[key] = append(normalized[key], sv)
	}
	for name, pv := range p {
		candidates := normalized[normalizeName(name)]
		var sv *store.Symbol
		switch {
		case len(candidates) == 1:
			sv = candidates[0]
		case len(candidates) > 1 && len(pv.Declarations) > 0:
			for _, c := range candidates {
				if len(c.Declarations) > 0 && m.sameNode(pv.Declarations[0], m.s.Node(c.Declarations[0].Id())) {
					sv = c
					break
				}
			}
		}
		if sv == nil {
			m.add(owner, label+" key")
			continue
		}
		m.symbol(label+" value", owner, pv, sv)
	}
	if len(p) != len(s) {
		m.add(owner, label+" size")
	}
}

func (m *bindComparer) sameNode(p *ast.Node, n store.Node) bool {
	return n.Kind() == p.Kind && int(n.Pos()) == p.Pos() && int(n.End()) == p.End()
}

func (m *bindComparer) flow(label string, owner *ast.Node, p *ast.FlowNode, r store.FlowRef) {
	if p == nil || r == 0 {
		if (p == nil) != (r == 0) {
			m.add(owner, label)
		}
		return
	}
	if prev, ok := m.flows[p]; ok {
		if prev != r {
			m.add(owner, label+" (flow paired twice)")
		}
		return
	}
	if prev, ok := m.flowsRev[r]; ok && prev != p {
		m.add(owner, label+" (flow paired twice)")
		return
	}
	m.flows[p] = r
	m.flowsRev[r] = p
	f := m.bound.Flow(r)
	if f.Flags != p.Flags {
		m.add(owner, label+" Flags")
	}
	switch {
	case p.Flags&ast.FlowFlagsSwitchClause != 0:
		data := p.Node.AsFlowSwitchClauseData()
		d := m.bound.FlowData(uint32(f.Node))
		m.node(label+" SwitchStatement", owner, data.SwitchStatement, m.s.Node(store.NodeRef(d.A)))
		if int32(d.B) != data.ClauseStart || int32(d.C) != data.ClauseEnd {
			m.add(owner, label+" clause range")
		}
	case p.Flags&ast.FlowFlagsReduceLabel != 0:
		data := p.Node.AsFlowReduceLabelData()
		d := m.bound.FlowData(uint32(f.Node))
		m.flow(label+" Target", owner, data.Target, store.FlowRef(d.A))
		m.flowList(label+" reduced Antecedents", owner, data.Antecedents, store.FlowListRef(d.B))
	default:
		m.node(label+" Node", owner, p.Node, m.s.Node(f.Node))
	}
	m.flow(label+" Antecedent", owner, p.Antecedent, f.Antecedent)
	m.flowList(label+" Antecedents", owner, p.Antecedents, f.Antecedents)
}

func (m *bindComparer) flowList(label string, owner *ast.Node, p *ast.FlowList, r store.FlowListRef) {
	for p != nil && r != 0 {
		l := m.bound.FlowList(r)
		m.flow(label+" element", owner, p.Flow, l.Flow)
		p, r = p.Next, l.Next
	}
	if p != nil || r != 0 {
		m.add(owner, label+" length")
	}
}

// pair compares everything the binder wrote on one node.
func (m *bindComparer) pair(p *ast.Node, n store.Node) {
	if (n.Flags()^p.Flags)&^m.opts.MaskFlags != 0 {
		m.add(p, "Flags")
	}
	m.symbol("Symbol", p, p.Symbol(), m.bound.SymbolOf(n.Symbol()))
	m.symbol("LocalSymbol", p, p.LocalSymbol(), m.bound.SymbolOf(n.LocalSymbol()))
	slot, hasSlot := n.LocalsSlot()
	if hasSlot != (p.LocalsContainerData() != nil) {
		m.add(p, "Locals (kind)")
	}
	var locals store.SymbolTable
	if slot != 0 {
		locals = m.bound.Locals(slot)
	}
	m.table("Locals", p, p.Locals(), locals)
	var flow *ast.FlowNode
	if data := p.FlowNodeData(); data != nil {
		flow = data.FlowNode
	}
	m.flow("FlowNode", p, flow, n.FlowNode())
	var endFlow *ast.FlowNode
	if data := p.BodyData(); data != nil {
		endFlow = data.EndFlowNode
	}
	m.flow("EndFlowNode", p, endFlow, n.EndFlowNode())
	var returnFlow *ast.FlowNode
	switch p.Kind {
	case ast.KindConstructor:
		returnFlow = p.AsConstructorDeclaration().ReturnFlowNode
	case ast.KindFunctionDeclaration:
		returnFlow = p.AsFunctionDeclaration().ReturnFlowNode
	case ast.KindFunctionExpression:
		returnFlow = p.AsFunctionExpression().ReturnFlowNode
	case ast.KindClassStaticBlockDeclaration:
		returnFlow = p.AsClassStaticBlockDeclaration().ReturnFlowNode
	}
	m.flow("ReturnFlowNode", p, returnFlow, n.ReturnFlowNode())
	var fallthroughFlow *ast.FlowNode
	if p.Kind == ast.KindCaseClause || p.Kind == ast.KindDefaultClause {
		fallthroughFlow = p.AsCaseOrDefaultClause().FallthroughFlowNode
	}
	m.flow("FallthroughFlowNode", p, fallthroughFlow, n.FallthroughFlowNode())
}

// A diagnostic as compared between the two binders: the Store one has no file.
type diagnosticKey struct {
	pos, length int
	code        int32
	message     string
}

func diagnosticKeys(ds []*ast.Diagnostic) []diagnosticKey {
	keys := make([]diagnosticKey, len(ds))
	for i, d := range ds {
		keys[i] = diagnosticKey{d.Pos(), d.Len(), d.Code(), d.MessageText()}
	}
	return keys
}

// fromJSDoc reports a Pointer node the Store cannot have: one the Pointer
// parser made from JSDoc.
func (m *bindComparer) fromJSDoc(p *ast.Node) bool {
	return m.opts.SkipReparsed && p != nil && p.Flags&(ast.NodeFlagsJSDoc|ast.NodeFlagsReparsed) != 0
}

func (m *bindComparer) nodes(label string, ps []*ast.Node, rs []store.NodeRef) {
	root := m.pointer.AsNode()
	ps = slices.DeleteFunc(slices.Clone(ps), m.fromJSDoc)
	if len(ps) != len(rs) {
		m.add(root, label)
		return
	}
	for i := range ps {
		m.node(label, root, ps[i], m.s.Node(rs[i]))
	}
}

// fileFields compares the fields of the file that the binder and the parser set.
func (m *bindComparer) fileFields() {
	root := m.pointer.AsNode()
	if m.pointer.SymbolCount != m.bound.SymbolCount {
		m.add(root, "SymbolCount")
	}
	m.symbol("file Symbol", root, m.pointer.Symbol, m.file.Symbol())
	m.table("GlobalExports", root, m.pointer.GlobalExports, m.bound.GlobalExports)
	if len(m.pointer.PatternAmbientModules) != len(m.bound.PatternAmbientModules) {
		m.add(root, "PatternAmbientModules")
	} else {
		for i, p := range m.pointer.PatternAmbientModules {
			s := m.bound.PatternAmbientModules[i]
			if p.Pattern != s.Pattern {
				m.add(root, "PatternAmbientModules Pattern")
			}
			m.symbol("PatternAmbientModules Symbol", root, p.Symbol, s.Symbol)
		}
	}
	if !slices.Equal(diagnosticKeys(m.pointer.BindDiagnostics()), diagnosticKeys(m.bound.BindDiagnostics())) {
		m.add(root, "BindDiagnostics")
	}
	m.node("CommonJSModuleIndicator", root, m.pointer.CommonJSModuleIndicator, m.s.Node(m.bound.CommonJSModuleIndicator))
	if !m.fromJSDoc(m.pointer.ExternalModuleIndicator) {
		m.node("ExternalModuleIndicator", root, m.pointer.ExternalModuleIndicator, m.s.Node(m.file.ExternalModuleIndicator))
	}
	m.nodes("Imports", m.pointer.Imports(), m.file.Imports)
	m.nodes("ModuleAugmentations", m.pointer.ModuleAugmentations, m.file.ModuleAugmentations)
	if !slices.Equal(m.pointer.AmbientModuleNames, m.file.AmbientModuleNames) {
		m.add(root, "AmbientModuleNames")
	}
	if m.pointer.UsesUriStyleNodeCoreModules != m.file.UsesUriStyleNodeCoreModules {
		m.add(root, "UsesUriStyleNodeCoreModules")
	}
	// The NextContainer chain, from the root's NextContainer on.
	var chain []*ast.Node
	for c := root.LocalsContainerData().NextContainer; c != nil; c = c.LocalsContainerData().NextContainer {
		chain = append(chain, c)
	}
	m.nodes("Containers", chain, m.bound.Containers())
}

// BindEquivalent compares the bound Pointer file and the bound Store file. It
// requires the parse equivalence (storetest.Pairs) first; a file whose walks
// do not line up reports that and nothing else.
func BindEquivalent(pointer *ast.SourceFile, file *store.File, opts storetest.Options) storetest.Mismatches {
	pairs, mismatches := storetest.Pairs(pointer, file.Store, opts)
	if pairs == nil {
		return mismatches
	}
	m := &bindComparer{
		Mismatches: mismatches,
		opts:       opts,
		pointer:    pointer,
		file:       file,
		s:          file.Store,
		bound:      file.Bound,
		symbols:    map[*ast.Symbol]*store.Symbol{},
		symbolsRev: map[*store.Symbol]*ast.Symbol{},
		flows:      map[*ast.FlowNode]store.FlowRef{},
		flowsRev:   map[store.FlowRef]*ast.FlowNode{},
	}
	for _, pair := range pairs {
		m.pair(pair.P, pair.N)
	}
	m.fileFields()
	return m.Mismatches
}

// Why a JS file is left out of the equivalence test: the Pointer parser made
// nodes from JSDoc that the Store does not have, and the Pointer binder's
// output depends on them. The literal rule of store-binder-design-20260922.md
// 2.1 is the first reason; the others are the ways a reparsed node changes
// the bind without carrying a symbol itself.
const (
	reasonBoundReparsed = "a reparsed node with a symbol"               // the design's rule
	reasonBoundSubtree  = "a symbol below a reparsed node"              // @import: the specifier has the symbol, the declaration the flag
	reasonReparsedType  = "a reparsed type annotation on a declaration" // @type: IsExpandoInitializer reads declaration.Type()
	reasonJSDocCast     = "a JSDoc type cast"                           // /** @type {T} */ (e): an AsExpression the Store does not have, which stops narrowing
	reasonOtherReparsed = "another reparsed node"                       // not excluded
)

// reparsedEffects reports the reasons found in the Pointer file.
func reparsedEffects(pointer *ast.SourceFile) map[string]bool {
	reasons := map[string]bool{}
	var hasSymbol func(n *ast.Node) bool
	hasSymbol = func(n *ast.Node) bool {
		if n.Symbol() != nil {
			return true
		}
		found := false
		n.ForEachChild(func(c *ast.Node) bool {
			found = hasSymbol(c)
			return found
		})
		return found
	}
	var visit ast.Visitor
	visit = func(n *ast.Node) bool {
		if n.Flags&ast.NodeFlagsReparsed != 0 {
			switch {
			case n.Symbol() != nil:
				reasons[reasonBoundReparsed] = true
			case hasSymbol(n):
				reasons[reasonBoundSubtree] = true
			case n.Parent != nil && isTypeOfExpandoOrRequireDeclaration(n.Parent, n):
				reasons[reasonReparsedType] = true
			default:
				reasons[reasonOtherReparsed] = true
			}
		}
		if (n.Kind == ast.KindAsExpression || n.Kind == ast.KindSatisfiesExpression) && n.Type().Flags&ast.NodeFlagsReparsed != 0 {
			reasons[reasonJSDocCast] = true
		}
		return n.ForEachChild(visit)
	}
	visit(pointer.AsNode())
	return reasons
}

// isTypeOfExpandoOrRequireDeclaration reports whether typeNode is the type
// annotation that the binder reads: IsExpandoInitializer reads the type of a
// declaration whose initializer is an empty object literal, and
// IsVariableDeclarationInitializedToRequire the type of a variable
// initialized to a require call. Other reparsed annotations do not change
// the bind.
func isTypeOfExpandoOrRequireDeclaration(declaration *ast.Node, typeNode *ast.Node) bool {
	var initializer *ast.Node
	switch declaration.Kind {
	case ast.KindVariableDeclaration:
		if declaration.AsVariableDeclaration().Type != typeNode {
			return false
		}
		initializer = declaration.Initializer()
	case ast.KindBinaryExpression:
		if declaration.AsBinaryExpression().Type != typeNode {
			return false
		}
		initializer = declaration.AsBinaryExpression().Right
	default:
		return false
	}
	if initializer == nil {
		return false
	}
	return ast.IsObjectLiteralExpression(initializer) && len(initializer.Properties()) == 0 ||
		ast.IsRequireCall(initializer, true /*requireStringLiteralLikeArgument*/)
}

// excluded reports whether the reasons leave the file out.
func excluded(reasons map[string]bool) bool {
	return reasons[reasonBoundReparsed] || reasons[reasonBoundSubtree] || reasons[reasonReparsedType] || reasons[reasonJSDocCast]
}

func labelWithExample(label string, name string, index int) string {
	return fmt.Sprintf("%s (first in %s #%d)", label, name, index)
}
