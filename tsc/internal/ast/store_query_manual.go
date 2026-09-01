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
		if parent.IsNil() && descendant.Kind() != KindSourceFile {
			panic("descendant is not parented")
		}
		descendant = parent
	}
	return false
}

func (h Handle) JSDoc(file *SourceFile) []Handle {
	if h.IsNil() || h.Flags()&NodeFlagsHasJSDoc == 0 {
		return nil
	}
	if file == nil {
		file = h.Store().SourceFile()
	}
	if file == nil {
		return nil
	}
	if cached := file.JSDocHandles(h); len(cached) > 0 || !file.hasLazyJSDoc {
		return cached
	}
	if parseJSDocForNode == nil {
		return nil
	}
	docs := parseJSDocForNode(file, h)
	file.CacheJSDocHandles(h, docs)
	return docs
}

func (h Handle) EagerJSDoc(file *SourceFile) []Handle {
	return h.JSDoc(file)
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
		if s.ListAt(list, i).Kind() == kind {
			return true
		}
	}
	return false
}

func (s *Store) ListSlice(list ListRef) []Handle {
	if s == nil || list == 0 {
		return nil
	}
	n := s.ListLen(list)
	out := make([]Handle, n)
	for i := 0; i < n; i++ {
		out[i] = s.ListAt(list, i)
	}
	return out
}

func (h Handle) ListSlice(list ListRef) []Handle {
	if h.IsNil() {
		return nil
	}
	return h.Store().ListSlice(list)
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
		if mod.Kind() == KindDecorator {
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
		flags |= ModifierToFlag(mod.Kind())
	}
	return flags
}

func (h Handle) IsTypeOnly() bool {
	if h.IsNil() {
		return false
	}
	switch h.Kind() {
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
	for !h.IsNil() && h.Kind() == KindBinaryExpression && h.BinaryExpressionOperatorToken().Kind() == KindEqualsToken {
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
	if h.IsNil() || h.Kind() != KindBinaryExpression {
		return false
	}
	op := h.BinaryExpressionOperatorToken()
	if op.IsNil() || op.Kind() != KindEqualsToken {
		return false
	}
	left := h.BinaryExpressionLeft()
	if left.IsNil() {
		return false
	}
	switch left.Kind() {
	case KindPropertyAccessExpression, KindElementAccessExpression:
		return true
	}
	return false
}

func (h Handle) RawText() string {
	if h.IsNil() {
		return ""
	}
	switch h.Kind() {
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
	switch h.Kind() {
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
	return h.Kind() == KindExportAssignment && h.ExportAssignmentIsExportEquals()
}

func (h Handle) MultiLine() bool {
	if h.IsNil() {
		return false
	}
	switch h.Kind() {
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
