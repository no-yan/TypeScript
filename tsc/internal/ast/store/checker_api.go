package store

import (
	"iter"
	"slices"

	"github.com/microsoft/TypeScript/tsc/internal/ast"
	"github.com/microsoft/TypeScript/tsc/internal/core"
	"github.com/microsoft/TypeScript/tsc/internal/diagnostics"
	"github.com/microsoft/TypeScript/tsc/internal/spanmap"
)

// The API that the translated checker, printer and name resolver call
// (internal/ast/docs/store-checker-7d2a-implementation-instructions-20260929.md
// 4, rules R2 to R8 of store-checker-7d2-design-remainder-20260929.md 2.1).
// Each is the Pointer method or function of the same name, written on the
// Store accessors. A method of *ast.Node that returns a *NodeList, and its
// []*Node twin, return a List here (R2).

// ── Node (R2) ──

func (n Node) ModifierNodes() List     { return n.Modifiers() }
func (n Node) TypeArgumentList() List  { return n.TypeArguments() }
func (n Node) ArgumentList() List      { return n.Arguments() }
func (n Node) ElementList() List       { return n.Elements() }
func (n Node) TypeParameterList() List { return n.TypeParameters() }
func (n Node) StatementList() List     { return n.Statements() }
func (n Node) PropertyList() List      { return n.Properties() }
func (n Node) ParameterList() List     { return n.Parameters() }
func (n Node) MemberList() List        { return n.Members() }
func (n Node) Comments() List          { return n.Comment() }

// Decorators is (*ast.Node).Decorators: the decorators among the modifiers.
// It filters, so it is a slice, not a List.
func (n Node) Decorators() []Node {
	modifiers := n.Modifiers()
	if modifiers.IsNil() {
		return nil
	}
	var out []Node
	for i := range modifiers.Len() {
		if m := modifiers.At(i); m.Kind() == ast.KindDecorator {
			out = append(out, m)
		}
	}
	return out
}

func (n Node) IsTypeOnly() bool {
	switch n.Kind() {
	case ast.KindImportEqualsDeclaration:
		return n.AsImportEqualsDeclaration().IsTypeOnly()
	case ast.KindImportSpecifier:
		return n.AsImportSpecifier().IsTypeOnly()
	case ast.KindImportClause:
		return n.AsImportClause().PhaseModifier() == ast.KindTypeKeyword
	case ast.KindExportDeclaration:
		return n.AsExportDeclaration().IsTypeOnly()
	case ast.KindExportSpecifier:
		return n.AsExportSpecifier().IsTypeOnly()
	}
	return false
}

func (n Node) Attributes() Node {
	switch n.Kind() {
	case ast.KindJsxOpeningElement:
		return n.AsJsxOpeningElement().Attributes()
	case ast.KindJsxSelfClosingElement:
		return n.AsJsxSelfClosingElement().Attributes()
	case ast.KindModuleDeclaration:
		return n.AsModuleDeclaration().Attributes()
	}
	panic("Unhandled case in Node.Attributes: " + n.Kind().String())
}

func (n Node) CanHaveStatements() bool {
	switch n.Kind() {
	case ast.KindSourceFile, ast.KindBlock, ast.KindModuleBlock, ast.KindCaseClause, ast.KindDefaultClause:
		return true
	default:
		return false
	}
}

// GetResolutionModeOverride is (*ast.ImportAttributesNode).GetResolutionModeOverride.
func (n Node) GetResolutionModeOverride(grammarErrorOnNode func(node Node, message *diagnostics.Message, args ...any) bool) (core.ResolutionMode, bool) {
	if n.IsNil() {
		return core.ResolutionModeNone, false
	}
	attributes := n.AsImportAttributes().Attributes()
	var attribute Node
	for i := range attributes.Len() {
		if a := attributes.At(i); a.Name().Text() == "resolution-mode" {
			attribute = a
			break
		}
	}
	if attribute.IsNil() {
		return core.ResolutionModeNone, false
	}
	value := attribute.AsImportAttribute().Value()
	if !IsStringLiteralLike(value) {
		return core.ResolutionModeNone, false
	}
	if value.Text() != "import" && value.Text() != "require" {
		if grammarErrorOnNode != nil {
			grammarErrorOnNode(value, diagnostics.X_resolution_mode_should_be_either_require_or_import)
		}
		return core.ResolutionModeNone, false
	}
	if value.Text() == "import" {
		return core.ResolutionModeESM, true
	} else {
		return core.ModuleKindCommonJS, true
	}
}

// ── List (R2, R4') ──

func (l List) Loc() core.TextRange { return core.NewTextRange(int(l.Pos()), int(l.End())) }

func (l List) HasTrailingComma() bool {
	if l.Len() == 0 {
		return false
	}
	return l.At(l.Len()-1).End() < l.End()
}

// Nodes copies the elements (R4', where a List flows into a []*ast.Node). It
// reads each with At, as a foreign edge is not a NodeRef. A nil List is nil,
// as the Nodes of a nil *ast.NodeList read through a nil check are.
func (l List) Nodes() []Node {
	if l.IsNil() {
		return nil
	}
	out := make([]Node, l.Len())
	for i := range out {
		out[i] = l.At(i)
	}
	return out
}

// ── File (R3) ──

func (f *File) Statements() List { return f.Root().Statements() }

// Locals is the file's locals: the root's.
func (f *File) Locals() SymbolTable { return f.Root().Locals() }

func (f *File) GlobalExports() SymbolTable {
	if f.Bound == nil {
		return nil
	}
	return f.Bound.GlobalExports
}

// PatternAmbientModules is the file's field: pointers, as the Pointer's, to
// the entries of the Bound (the checker appends them to its own list).
func (f *File) PatternAmbientModules() []*PatternAmbientModule {
	if f.Bound == nil || len(f.Bound.PatternAmbientModules) == 0 {
		return nil
	}
	out := make([]*PatternAmbientModule, len(f.Bound.PatternAmbientModules))
	for i := range out {
		out[i] = &f.Bound.PatternAmbientModules[i]
	}
	return out
}

func (f *File) CommonJSModuleIndicator() Node {
	if f.Bound == nil {
		return Node{}
	}
	return f.Store.Node(f.Bound.CommonJSModuleIndicator)
}

// ForEachChild visits the children of the root, as (*ast.SourceFile).ForEachChild.
func (f *File) ForEachChild(v Visitor) bool { return f.Root().ForEachChild(v) }

// ECMALineMap is (*ast.SourceFile).ECMALineMap: computed once, under a lock.
func (f *File) ECMALineMap() []core.TextPos {
	f.ecmaLineMapMu.RLock()
	lineMap := f.ecmaLineMap
	f.ecmaLineMapMu.RUnlock()
	if lineMap == nil {
		f.ecmaLineMapMu.Lock()
		defer f.ecmaLineMapMu.Unlock()
		lineMap = f.ecmaLineMap
		if lineMap == nil {
			lineMap = core.ComputeECMALineStarts(f.Text())
			f.ecmaLineMap = lineMap
		}
	}
	return lineMap
}

// ── Sets of nodes (R5) ──

// NodeSet is collections.Set[*ast.Node]: keyed by NodeKey, and it keeps the
// Node so that Keys reads the nodes back.
type NodeSet struct {
	M map[NodeKey]Node
}

func (s *NodeSet) Has(n Node) bool {
	if s == nil {
		return false
	}
	_, ok := s.M[n.Key()]
	return ok
}

func (s *NodeSet) Add(n Node) {
	if s.M == nil {
		s.M = make(map[NodeKey]Node)
	}
	s.M[n.Key()] = n
}

func (s *NodeSet) Delete(n Node) { delete(s.M, n.Key()) }

func (s *NodeSet) Len() int {
	if s == nil {
		return 0
	}
	return len(s.M)
}

// Keys ranges over the nodes, in map order as the Pointer Keys does.
func (s *NodeSet) Keys() iter.Seq[Node] {
	return func(yield func(Node) bool) {
		if s == nil {
			return
		}
		for _, n := range s.M {
			if !yield(n) {
				return
			}
		}
	}
}

func (s *NodeSet) Clear() {
	if s == nil {
		return
	}
	clear(s.M)
}

// AddIfAbsent returns true if the node was not already present in the set.
func (s *NodeSet) AddIfAbsent(n Node) bool {
	if s.Has(n) {
		return false
	}
	s.Add(n)
	return true
}

// OrderedNodeSet is collections.OrderedSet[*ast.Node]: insertion ordered.
type OrderedNodeSet struct {
	keys  []Node
	index map[NodeKey]struct{}
}

func (s *OrderedNodeSet) Add(n Node) {
	if s.index == nil {
		s.index = make(map[NodeKey]struct{})
	}
	if _, ok := s.index[n.Key()]; !ok {
		s.index[n.Key()] = struct{}{}
		s.keys = append(s.keys, n)
	}
}

func (s *OrderedNodeSet) Has(n Node) bool {
	_, ok := s.index[n.Key()]
	return ok
}

func (s *OrderedNodeSet) Delete(n Node) bool {
	if _, ok := s.index[n.Key()]; !ok {
		return false
	}
	delete(s.index, n.Key())
	s.keys = slices.DeleteFunc(s.keys, func(k Node) bool { return SameNode(k, n) })
	return true
}

func (s *OrderedNodeSet) Values() iter.Seq[Node] {
	return func(yield func(Node) bool) {
		for _, n := range s.keys {
			if !yield(n) {
				return
			}
		}
	}
}

func (s *OrderedNodeSet) Clear() {
	clear(s.index)
	clear(s.keys)
	s.keys = s.keys[:0]
}

func (s *OrderedNodeSet) Size() int { return len(s.keys) }

// ── Comparable helpers (R6) ──

// Compact is slices.Compact with SameNode.
func Compact(s []Node) []Node {
	return slices.CompactFunc(s, SameNode)
}

// Coalesce is core.Coalesce with T = Node: a unless it is nil.
func Coalesce(a Node, b Node) Node {
	if a.IsNil() {
		return b
	}
	return a
}

// ── Factory (R7, R8) ──

// Region is the region the Factory makes its Stores in: the nodebuilder's
// EmitContext takes the checker's (5.1 of the 7d-2a instructions).
func (f *Factory) Region() *NodeRegion { return f.region }

// Clone is (*ast.Node).Clone(f): the shallow clone of a node by its kind.
func (f *Factory) Clone(n Node) Node { return f.CloneNode(n) }

// NewModifier is ast.NodeFactory.NewModifier.
func (f *Factory) NewModifier(kind ast.Kind) Node { return f.NewToken(kind) }

// SetIsTypeOf is n.AsImportTypeNode().IsTypeOf = v.
func (f *Factory) SetIsTypeOf(n Node, v bool) {
	f.checkWritable(n)
	n.assertKind(ast.KindImportType)
	n.s.extra[int(n.h.data)+importTypeNodeIsTypeOfSlot] = boolWord(v)
}

// SetTokenFlags is n.AsXxx().TokenFlags = flags, on a kind that has them.
func (f *Factory) SetTokenFlags(n Node, flags ast.TokenFlags) {
	f.checkWritable(n)
	var slot int
	switch n.Kind() {
	case ast.KindStringLiteral:
		slot = stringLiteralTokenFlagsSlot
	case ast.KindNumericLiteral:
		slot = numericLiteralTokenFlagsSlot
	case ast.KindBigIntLiteral:
		slot = bigIntLiteralTokenFlagsSlot
	case ast.KindRegularExpressionLiteral:
		slot = regularExpressionLiteralTokenFlagsSlot
	default:
		panic("store: " + n.Kind().String() + " has no TokenFlags")
	}
	n.s.extra[int(n.h.data)+slot] = uint32(flags)
}

// SetListRange is l.Loc = core.NewTextRange(pos, end): extra at+1 and at+2 of
// the block, in the list's owner.
func (f *Factory) SetListRange(l List, pos, end int32) {
	if l.IsNil() {
		panic("store: write to the nil list")
	}
	f.checkWritableStore(l.s)
	l.s.extra[l.at+1] = uint32(pos)
	l.s.extra[l.at+2] = uint32(end)
}

// ── The data of a synthetic flow node ──

// FlowSwitchClauseData is ast.FlowSwitchClauseData, read from a SwitchClause
// flow: flow.Node.AsFlowSwitchClauseData() is flow.AsFlowSwitchClauseData().
type FlowSwitchClauseData struct {
	SwitchStatement Node
	ClauseStart     int32 // Start index of case/default clause range
	ClauseEnd       int32 // End index of case/default clause range
}

func (d *FlowSwitchClauseData) IsEmpty() bool {
	return d.ClauseStart == d.ClauseEnd
}

// FlowReduceLabelData is ast.FlowReduceLabelData, read from a ReduceLabel
// flow.
type FlowReduceLabelData struct {
	Target      Flow     // Target label
	Antecedents FlowList // Temporary antecedent list
}

func (f Flow) AsFlowSwitchClauseData() *FlowSwitchClauseData {
	switchStatement, clauseStart, clauseEnd := f.SwitchClause()
	return &FlowSwitchClauseData{switchStatement, int32(clauseStart), int32(clauseEnd)}
}

func (f Flow) AsFlowReduceLabelData() *FlowReduceLabelData {
	target, antecedents := f.ReduceLabel()
	return &FlowReduceLabelData{target, antecedents}
}

// OriginalText and SpanMap are (*ast.SourceFile).OriginalText and SpanMap for
// a file that no content mapper produced: a File has no content mapper
// information (the Store parser does not take it), so they are the text and
// nil (2026-09-30, the user's decision for 7d-2a).
func (f *File) OriginalText() string      { return f.text }
func (f *File) SpanMap() *spanmap.SpanMap { return nil }

// HasIdentifier is (*ast.SourceFile).HasIdentifier: the texts of the
// identifiers and literals of the file, collected once.
func (f *File) HasIdentifier(name string) bool {
	f.identifiersOnce.Do(func() {
		var collect func(Node) bool
		collect = func(node Node) bool {
			switch node.Kind() {
			case ast.KindIdentifier,
				ast.KindPrivateIdentifier,
				ast.KindStringLiteral,
				ast.KindNumericLiteral,
				ast.KindBigIntLiteral,
				ast.KindNoSubstitutionTemplateLiteral:
				f.identifiers.Add(node.Text())
			}
			node.ForEachChild(collect)
			return false
		}
		collect(f.Root())
	})
	return f.identifiers.Has(name)
}

// literalTokenFlags and templateFlags are LiteralLikeData().TokenFlags and
// TemplateLiteralLikeData().TemplateFlags on the views that have the slot
// (IsUnterminatedLiteral, utilities_manual.go).
func literalTokenFlags(node Node) ast.TokenFlags {
	switch node.Kind() {
	case ast.KindStringLiteral:
		return node.AsStringLiteral().TokenFlags()
	case ast.KindNumericLiteral:
		return node.AsNumericLiteral().TokenFlags()
	case ast.KindBigIntLiteral:
		return node.AsBigIntLiteral().TokenFlags()
	case ast.KindRegularExpressionLiteral:
		return node.AsRegularExpressionLiteral().TokenFlags()
	}
	return 0
}

func templateFlags(node Node) ast.TokenFlags {
	switch node.Kind() {
	case ast.KindNoSubstitutionTemplateLiteral:
		return node.AsNoSubstitutionTemplateLiteral().TemplateFlags()
	case ast.KindTemplateHead:
		return node.AsTemplateHead().TemplateFlags()
	case ast.KindTemplateMiddle:
		return node.AsTemplateMiddle().TemplateFlags()
	case ast.KindTemplateTail:
		return node.AsTemplateTail().TemplateFlags()
	}
	return 0
}

// NextContainer is x.LocalsContainerData().NextContainer: the container after
// n in its Bound's chain (Bound.containers, the binder's NextContainer chain in
// declaration order). A node that is not in a chain has none.
func (n Node) NextContainer() Node {
	b := n.s.bound
	if b == nil || n.IsNil() {
		return Node{}
	}
	ref := n.Ref()
	for i, c := range b.containers {
		if c == ref {
			if i+1 < len(b.containers) {
				return b.store.node(b.containers[i+1])
			}
			return Node{}
		}
	}
	return Node{}
}

// NewCommentRange is ast.NodeFactory.NewCommentRange: a value, not a node.
func (f *Factory) NewCommentRange(kind ast.Kind, pos int, end int, hasTrailingNewLine bool) ast.CommentRange {
	return ast.CommentRange{
		TextRange:          core.NewTextRange(pos, end),
		Kind:               kind,
		HasTrailingNewLine: hasTrailingNewLine,
	}
}

// KindString is (*ast.Node).KindString (debug.FailBadSyntaxKind takes it).
func (n Node) KindString() string { return n.Kind().String() }

// IsJS is (*ast.SourceFile).IsJS.
func (f *File) IsJS() bool { return IsSourceFileJS(f) }

// Uint64 packs the key into one integer, StoreID in the high 32 bits: the
// Pointer's integer conversions of an ast.NodeId (a links page index, a key
// builder, symbolTableID) take it. Two keys have the same Uint64 only when
// they are the same key.
func (k NodeKey) Uint64() uint64 { return uint64(k.store)<<32 | uint64(k.id) }

// FinishSynthetic finishes b as a Store of its own for the node root of a
// parse outside any file (storeparser.ParseIsolatedEntityName, R16): every
// node at (-1, -1), as the Pointer's markAsSynthetic leaves them, no file,
// and sealed before anyone reads it. An identifier whose text is the source
// from data to end has it moved to texts first. The Builder was reset with the
// program's IDSpace and a StoreID from it.
func (b *Builder) FinishSynthetic(root NodeRef) Node {
	s := b.Finish()
	for i := 1; i < len(s.nodes); i++ {
		h := &s.nodes[i]
		// An identifier's text is the source from data to end: move it to
		// texts before the range goes.
		if (h.kind == ast.KindIdentifier || h.kind == ast.KindPrivateIdentifier) && h.data&textInTexts == 0 {
			text := s.src[h.data:h.end]
			at := uint32(len(s.extra))
			s.extra = append(s.extra, textInTexts|uint32(len(s.texts)), uint32(len(text)))
			s.texts += text
			h.data = textInTexts | at
		}
		h.pos, h.end = -1, -1
	}
	s.sealed = true
	return s.node(root)
}
