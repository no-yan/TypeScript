package ast

const valueSlotFallthroughFlow = 30

func (h Handle) Pos() int {
	if h.IsNil() {
		return 0
	}
	return h.Loc().Pos()
}

func (h Handle) End() int {
	if h.IsNil() {
		return 0
	}
	return h.Loc().End()
}

func (h Handle) Contains(descendant Handle) bool {
	for !descendant.IsNil() {
		if descendant == h {
			return true
		}
		parent := descendant.Parent()
		if parent.IsNil() && descendant.Kind != KindSourceFile {
			panic("descendant is not parented")
		}
		descendant = parent
	}
	return false
}

// JSDocCache owns JSDoc that one consumer parsed lazily for TS files. The
// nodes are allocated into store (a checker's synth Store, or any private
// Store), never into the file's frozen parse Store; their parent edge to the
// host node is a cross-store externalParent on store. A JSDocCache is not safe
// for concurrent use: it must belong to a single writer, like store itself.
type JSDocCache struct {
	store *Store
	docs  map[GlobalRef][]Handle
}

// NewJSDocCache creates a cache that allocates lazily parsed JSDoc into store.
func NewJSDocCache(store *Store) *JSDocCache {
	if store == nil {
		panic("ast: NewJSDocCache nil Store")
	}
	return &JSDocCache{store: store}
}

// Store returns the Store lazily parsed JSDoc is allocated into.
func (c *JSDocCache) Store() *Store { return c.store }

// Len returns the number of host nodes whose JSDoc this cache has parsed.
func (c *JSDocCache) Len() int {
	if c == nil {
		return 0
	}
	return len(c.docs)
}

func (c *JSDocCache) lookup(file *SourceFile, h Handle) []Handle {
	key := h.Global()
	if docs, ok := c.docs[key]; ok {
		return docs
	}
	var docs []Handle
	if parseJSDocForNode != nil {
		docs = parseJSDocForNode(file, h, c.store)
	}
	if c.docs == nil {
		c.docs = make(map[GlobalRef][]Handle)
	}
	c.docs[key] = docs
	return docs
}

func jsdocFileOf(h Handle, file *SourceFile) *SourceFile {
	if file != nil {
		return file
	}
	return h.Store().SourceFile()
}

// JSDocIn returns h's JSDoc. JSDoc the parser attached eagerly (JS files, and
// TS comments with @see/@link) comes from the SourceFile; deferred TS JSDoc is
// parsed on first access into cache. A nil cache falls back to JSDoc(file).
func (h Handle) JSDocIn(file *SourceFile, cache *JSDocCache) []Handle {
	if cache == nil {
		return h.JSDoc(file)
	}
	if h.IsNil() || h.Flags()&NodeFlagsHasJSDoc == 0 {
		return nil
	}
	file = jsdocFileOf(h, file)
	if file == nil {
		return nil
	}
	if cached := file.JSDocHandles(h); len(cached) > 0 || !file.hasLazyJSDoc {
		return cached
	}
	return cache.lookup(file, h)
}

// JSDoc returns h's JSDoc for consumers that do not own a Store (LS, API,
// astnav). Deferred TS JSDoc is parsed into the file's shared side Store on
// first access (see SourceFile.warmSharedJSDoc). The checker must use JSDocIn
// with its own cache instead.
func (h Handle) JSDoc(file *SourceFile) []Handle {
	if h.IsNil() || h.Flags()&NodeFlagsHasJSDoc == 0 {
		return nil
	}
	file = jsdocFileOf(h, file)
	if file == nil {
		return nil
	}
	if cached := file.JSDocHandles(h); len(cached) > 0 || !file.hasLazyJSDoc {
		return cached
	}
	return file.sharedJSDocFor(h)
}

// EagerJSDoc returns only the JSDoc the parser attached at parse time. It
// never triggers lazy parsing, so deferred TS JSDoc is not visible here.
func (h Handle) EagerJSDoc(file *SourceFile) []Handle {
	if h.IsNil() || h.Flags()&NodeFlagsHasJSDoc == 0 {
		return nil
	}
	file = jsdocFileOf(h, file)
	if file == nil {
		return nil
	}
	return file.JSDocHandles(h)
}

func (h Handle) HasModifierKind(kind Kind) bool {
	if h.IsNil() {
		return false
	}
	list := h.Modifiers()
	if list == 0 {
		return false
	}
	s := h.Store()
	n := s.ListLen(list)
	for i := 0; i < n; i++ {
		if s.ListAt(list, i).Kind == kind {
			return true
		}
	}
	return false
}

// ListSlice returns an allocation-free NodeSeq over the packed list.
// Prefer ListLen / ListAt / ListIndexOf when the ListRef is available.
func (s *Store) ListSlice(list ListRef) NodeSeq {
	if s == nil || list == 0 {
		return EmptyNodeSeq
	}
	return NodeSeq{s: s.listOwner(list), list: list}
}

func (h Handle) ListSlice(list ListRef) NodeSeq {
	if h.IsNil() {
		return EmptyNodeSeq
	}
	return h.Store().ListSlice(list)
}

// ListIndexOf finds target in the packed list without materializing []Handle.
// Same-store elements compare via the children column; external slots use ListAt.
func (s *Store) ListIndexOf(list ListRef, target Handle) int {
	if s == nil || list == 0 {
		return -1
	}
	n := s.ListLen(list)
	if n == 0 {
		return -1
	}
	if !target.IsNil() && target.s == s {
		id := target.id
		for i := 0; i < n; i++ {
			if s.ListElem(list, i) == id {
				return i
			}
		}
		// Packed miss: only empty local slots can hold an external handle.
		for i := 0; i < n; i++ {
			if s.ListElem(list, i) != 0 {
				continue
			}
			if s.ListAt(list, i) == target {
				return i
			}
		}
		return -1
	}
	for i := 0; i < n; i++ {
		if s.ListAt(list, i) == target {
			return i
		}
	}
	return -1
}

func (h Handle) Decorators() []Handle {
	if h.IsNil() {
		return nil
	}
	list := h.Modifiers()
	if list == 0 {
		return nil
	}
	s := h.Store()
	n := s.ListLen(list)
	var out []Handle
	for i := 0; i < n; i++ {
		mod := s.ListAt(list, i)
		if mod.Kind == KindDecorator {
			out = append(out, mod)
		}
	}
	return out
}

func (h Handle) ModifierFlags() ModifierFlags {
	if h.IsNil() {
		return 0
	}
	list := h.Modifiers()
	if list == 0 {
		return 0
	}
	var flags ModifierFlags
	s := h.Store()
	n := s.ListLen(list)
	for i := 0; i < n; i++ {
		mod := s.ListAt(list, i)
		if mod.IsNil() {
			continue
		}
		flags |= ModifierToFlag(mod.Kind)
	}
	return flags
}

func (h Handle) IsTypeOnly() bool {
	if h.IsNil() {
		return false
	}
	switch h.Kind {
	case KindImportEqualsDeclaration:
		return h.ImportEqualsDeclarationIsTypeOnly()
	case KindImportSpecifier:
		return h.ImportSpecifierIsTypeOnly()
	case KindImportClause:
		return h.ImportClausePhaseModifier() == KindTypeKeyword
	case KindExportDeclaration:
		return h.ExportDeclarationIsTypeOnly()
	case KindExportSpecifier:
		return h.ExportSpecifierIsTypeOnly()
	}
	return false
}

func (h Handle) PropertyNameOrName() Handle {
	name := h.PropertyName()
	if name.IsNil() {
		return h.Name()
	}
	return name
}

func handleRightMostAssigned(h Handle) Handle {
	for !h.IsNil() && h.Kind == KindBinaryExpression && h.BinaryExpressionOperatorToken().Kind == KindEqualsToken {
		h = h.BinaryExpressionRight()
	}
	return h
}

func (h Handle) LooksLikeAssignmentDeclaration() bool {
	return handleLooksLikeAssignmentDeclaration(h)
}

func (h Handle) RightMostAssigned() Handle {
	return handleRightMostAssigned(h)
}

func handleLooksLikeAssignmentDeclaration(h Handle) bool {
	if h.IsNil() || h.Kind != KindBinaryExpression {
		return false
	}
	op := h.BinaryExpressionOperatorToken()
	if op.IsNil() || op.Kind != KindEqualsToken {
		return false
	}
	left := h.BinaryExpressionLeft()
	if left.IsNil() {
		return false
	}
	switch left.Kind {
	case KindPropertyAccessExpression, KindElementAccessExpression:
		return true
	}
	return false
}

func (h Handle) RawText() string {
	if h.IsNil() {
		return ""
	}
	switch h.Kind {
	case KindTemplateHead:
		return h.TemplateHeadRawText()
	case KindTemplateMiddle:
		return h.TemplateMiddleRawText()
	case KindTemplateTail:
		return h.TemplateTailRawText()
	case KindNoSubstitutionTemplateLiteral:
		return h.Text()
	}
	return ""
}

func (h Handle) CanHaveStatements() bool {
	if h.IsNil() {
		return false
	}
	switch h.Kind {
	case KindSourceFile, KindBlock, KindModuleBlock, KindCaseClause, KindDefaultClause:
		return true
	}
	return false
}

func (h Handle) NodeId() NodeId {
	if h.IsNil() {
		return 0
	}
	return NodeId(h.Global())
}

func (h Handle) KeywordToken() Kind {
	if h.IsNil() {
		return KindUnknown
	}
	return h.MetaPropertyKeywordToken()
}

func (h Handle) IsTypeOf() bool {
	if h.IsNil() {
		return false
	}
	return h.ImportTypeNodeIsTypeOf()
}

func (h Handle) IsExportEquals() bool {
	if h.IsNil() {
		return false
	}
	return h.Kind == KindExportAssignment && h.ExportAssignmentIsExportEquals()
}

func (h Handle) MultiLine() bool {
	if h.IsNil() {
		return false
	}
	switch h.Kind {
	case KindBlock:
		return h.BlockMultiLine()
	case KindArrayLiteralExpression:
		return h.ArrayLiteralExpressionMultiLine()
	case KindObjectLiteralExpression:
		return h.ObjectLiteralExpressionMultiLine()
	}
	return false
}

func (h Handle) TemplateFlags() TokenFlags {
	if h.IsNil() {
		return 0
	}
	return h.TokenFlags()
}

func (h Handle) FallthroughFlowNode() *FlowNode {
	if h.IsNil() {
		return nil
	}
	return storeObjectValue[*FlowNode](h, valueSlotFallthroughFlow)
}

func (h Handle) SetFallthroughFlowNode(flow *FlowNode) {
	if h.IsNil() {
		return
	}
	h.SetObjectValue(valueSlotFallthroughFlow, flow)
}

func GetReparsedHandle(node Handle) Handle {
	return node
}
