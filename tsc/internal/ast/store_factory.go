package ast

import "github.com/microsoft/TypeScript/tsc/internal/core"

// FactoryHooks mirrors NodeFactoryHooks for the Store-backed factory.
type FactoryHooks struct {
	OnCreate func(Handle)
	OnUpdate func(updated, original Handle)
	OnClone  func(updated, original Handle)
}

// Factory allocates exclusively into one Store. NewFactory owns a new Store.
// NewFactoryOn appends into an existing Store so checker synthetics and emit
// updates can share parse children. Factories on the same Store must not be
// used concurrently; ownership transfers between compiler phases.
type Factory struct {
	hooks FactoryHooks
	store *Store
}

// HandleFactory is the Store factory used by emit and language-service rewrites.
type HandleFactory = *Factory

func NewFactory(hooks FactoryHooks) *Factory {
	return NewFactoryHint(hooks, 256)
}

func NewFactoryHint(hooks FactoryHooks, hint int) *Factory {
	return &Factory{hooks: hooks, store: NewStore(hint)}
}

func NewFactoryOn(s *Store, hooks FactoryHooks) *Factory {
	if s == nil {
		panic("ast: NewFactoryOn nil Store")
	}
	return &Factory{hooks: hooks, store: s}
}

func (f *Factory) NewCommentRange(kind Kind, pos int, end int, hasTrailingNewLine bool) CommentRange {
	return CommentRange{
		TextRange:          core.NewTextRange(pos, end),
		Kind:               kind,
		HasTrailingNewLine: hasTrailingNewLine,
	}
}

func (f *Factory) Store() *Store { return f.store }

func (f *Factory) Seal() { f.store.Seal() }

// Finish sets parser-owned source locations after a native constructor has
// populated the node. Same-store parents are written by SetChild and SetListSlot.
func (f *Factory) Finish(h Handle, loc core.TextRange) Handle {
	if h.Store() != f.store || h.Ref() == 0 {
		panic("ast: Finish Handle from a different Store")
	}
	h.SetLoc(loc)
	return h
}

func (f *Factory) create(kind Kind, flags NodeFlags, loc core.TextRange, childLen int) Handle {
	return f.createSlots(kind, flags, loc, childLen, 0)
}

func (f *Factory) createSlots(kind Kind, flags NodeFlags, loc core.TextRange, childLen, listLen int) Handle {
	h := f.store.AllocSlots(kind, flags, loc, childLen, listLen)
	if f.hooks.OnCreate != nil {
		f.hooks.OnCreate(h)
	}
	return h
}

func (f *Factory) Identifier(text string) Handle {
	return f.NewIdentifier(text)
}

func (f *Factory) Token(kind Kind) Handle {
	return f.NewToken(kind)
}

func (f Factory) updateHandle(updated, original Handle) Handle {
	if updated != original {
		updated.SetFlags(original.Flags())
		updated.SetLoc(original.Loc())
		if f.hooks.OnUpdate != nil {
			f.hooks.OnUpdate(updated, original)
		}
	}
	return updated
}

func handlesEqual(a, b Handle) bool {
	if a.IsNil() || b.IsNil() {
		return a.IsNil() && b.IsNil()
	}
	return a == b
}

// HandleVisitor walks Store nodes and rebuilds via Factory.Update*.
type HandleVisitor struct {
	Visit     func(Handle) Handle
	Factory   *Factory
	Hooks     HandleVisitorHooks
	listStore *Store
}

type HandleVisitorHooks struct {
	VisitNode               func(node Handle, v *HandleVisitor) Handle
	VisitNodes              func(list ListRef, v *HandleVisitor) ListRef
	VisitParameters         func(list ListRef, v *HandleVisitor) ListRef
	VisitFunctionBody       func(node Handle, v *HandleVisitor) Handle
	VisitIterationBody      func(node Handle, v *HandleVisitor) Handle
	VisitEmbeddedStatement  func(node Handle, v *HandleVisitor) Handle
	VisitTopLevelStatements func(list ListRef, v *HandleVisitor) ListRef
	VisitToken              func(node Handle, v *HandleVisitor) Handle
	VisitModifiers          func(list ListRef, v *HandleVisitor) ListRef
}

func NewHandleVisitor(visit func(Handle) Handle, factory *Factory, hooks HandleVisitorHooks) *HandleVisitor {
	return &HandleVisitor{Visit: visit, Factory: factory, Hooks: hooks}
}

func (v *HandleVisitor) bindFactory(store *Store) {
	if store == nil {
		return
	}
	if v.Factory != nil && v.Factory.Store() == store {
		return
	}
	hooks := FactoryHooks{}
	if v.Factory != nil {
		hooks = v.Factory.hooks
	}
	v.Factory = NewFactoryOn(store, hooks)
}

func (v *HandleVisitor) VisitSourceFile(file *SourceFile) *SourceFile {
	if file == nil || v == nil {
		return file
	}
	root := file.ParseRoot()
	if root.IsNil() {
		return file
	}
	v.bindFactory(root.Store())
	updated := v.VisitNode(root)
	if !updated.IsNil() && updated != root {
		file.SetParseRoot(updated)
	}
	return file
}

func (v *HandleVisitor) VisitEachChild(node Handle) Handle {
	if node.IsNil() || v == nil || v.Visit == nil {
		return node
	}
	prev := v.listStore
	v.listStore = node.Store()
	defer func() { v.listStore = prev }()
	if node.Kind == KindSourceFile {
		stmts := v.VisitTopLevelStatements(node.SourceFileStatements())
		eof := v.VisitToken(node.SourceFileEndOfFileToken())
		if v.Factory == nil {
			return node
		}
		return v.Factory.UpdateSourceFile(node, stmts, eof)
	}
	return node.VisitEachChild(v)
}

func (v *HandleVisitor) VisitNode(node Handle) Handle {
	if v != nil && v.Hooks.VisitNode != nil {
		return v.Hooks.VisitNode(node, v)
	}
	return v.DefaultVisitNode(node)
}

func (v *HandleVisitor) DefaultVisitNode(node Handle) Handle {
	if node.IsNil() || v == nil || v.Visit == nil {
		return node
	}
	prev := v.listStore
	v.listStore = node.Store()
	defer func() { v.listStore = prev }()
	visited := v.Visit(node)
	if visited.IsNil() || visited.Kind != KindSyntaxList {
		return visited
	}
	kids := visited.Store().ListSlice(visited.SyntaxListChildren())
	if len(kids) != 1 {
		panic("Expected only a single node to be written to output")
	}
	visited = kids[0]
	if !visited.IsNil() && visited.Kind == KindSyntaxList {
		panic("The result of visiting and lifting a Node may not be SyntaxList")
	}
	return visited
}

func (v *HandleVisitor) VisitParameters(list ListRef) ListRef {
	if v != nil && v.Hooks.VisitParameters != nil {
		return v.Hooks.VisitParameters(list, v)
	}
	return v.VisitNodes(list)
}

func (v *HandleVisitor) VisitFunctionBody(node Handle) Handle {
	if v != nil && v.Hooks.VisitFunctionBody != nil {
		return v.Hooks.VisitFunctionBody(node, v)
	}
	return v.VisitNode(node)
}

func (v *HandleVisitor) VisitIterationBody(node Handle) Handle {
	if v != nil && v.Hooks.VisitIterationBody != nil {
		return v.Hooks.VisitIterationBody(node, v)
	}
	return v.VisitEmbeddedStatement(node)
}

func (v *HandleVisitor) VisitEmbeddedStatement(node Handle) Handle {
	if v != nil && v.Hooks.VisitEmbeddedStatement != nil {
		return v.Hooks.VisitEmbeddedStatement(node, v)
	}
	return v.DefaultVisitEmbeddedStatement(node)
}

// DefaultVisitEmbeddedStatement visits an embedded statement without consulting
// Hooks.VisitEmbeddedStatement. Hook implementations must call this instead of
// VisitEmbeddedStatement, which would re-enter the hook and recurse forever.
//
// TODO: the generated Handle.VisitEachChild visits if/do/while/for bodies via
// VisitNode, so the VisitEmbeddedStatement/VisitIterationBody hooks never fire
// from there (unlike the pointer NodeVisitor, whose generated VisitEachChild
// routes those bodies through visitEmbeddedStatement/visitIterationBody).
// Route those bodies through the hooks in the generator to match.
func (v *HandleVisitor) DefaultVisitEmbeddedStatement(node Handle) Handle {
	if node.IsNil() || v == nil || v.Visit == nil {
		return node
	}
	prev := v.listStore
	v.listStore = node.Store()
	defer func() { v.listStore = prev }()
	return v.liftToBlock(v.Visit(node))
}

func (v *HandleVisitor) liftToBlock(node Handle) Handle {
	if node.IsNil() {
		return node
	}
	var nodes []Handle
	if node.Kind == KindSyntaxList {
		nodes = node.Store().ListSlice(node.SyntaxListChildren())
	} else {
		nodes = []Handle{node}
	}
	if len(nodes) == 1 {
		node = nodes[0]
	} else {
		node = v.Factory.NewBlock(v.Factory.NewList(nodes), true)
	}
	if !node.IsNil() && node.Kind == KindSyntaxList {
		panic("The result of visiting and lifting a Node may not be SyntaxList")
	}
	return node
}

func (v *HandleVisitor) VisitTopLevelStatements(list ListRef) ListRef {
	if v != nil && v.Hooks.VisitTopLevelStatements != nil {
		return v.Hooks.VisitTopLevelStatements(list, v)
	}
	return v.VisitNodes(list)
}

func (v *HandleVisitor) VisitToken(node Handle) Handle {
	if v != nil && v.Hooks.VisitToken != nil {
		return v.Hooks.VisitToken(node, v)
	}
	return v.VisitNode(node)
}

func (v *HandleVisitor) VisitModifiers(list ListRef) ListRef {
	if v != nil && v.Hooks.VisitModifiers != nil {
		return v.Hooks.VisitModifiers(list, v)
	}
	return v.VisitNodes(list)
}

func appendVisitedHandle(out []Handle, visited Handle) []Handle {
	if visited.IsNil() {
		return out
	}
	if visited.Kind == KindSyntaxList {
		return append(out, visited.Store().ListSlice(visited.SyntaxListChildren())...)
	}
	return append(out, visited)
}

func (v *HandleVisitor) VisitSlice(nodes []Handle) []Handle {
	if len(nodes) == 0 || v == nil || v.Visit == nil {
		return nodes
	}
	out := make([]Handle, 0, len(nodes))
	changed := false
	for _, old := range nodes {
		visited := v.Visit(old)
		if visited != old {
			changed = true
		}
		if visited.IsNil() {
			changed = true
			continue
		}
		if visited.Kind == KindSyntaxList {
			changed = true
		}
		out = appendVisitedHandle(out, visited)
	}
	if !changed {
		return nodes
	}
	return out
}

func (v *HandleVisitor) VisitNodes(list ListRef) ListRef {
	if v != nil && v.Hooks.VisitNodes != nil {
		return v.Hooks.VisitNodes(list, v)
	}
	return v.DefaultVisitNodes(list)
}

func (v *HandleVisitor) DefaultVisitNodes(list ListRef) ListRef {
	if list == 0 || v == nil || v.Visit == nil || v.Factory == nil {
		return list
	}
	src := v.listStore
	if src == nil {
		src = v.Factory.Store()
	}
	n := src.ListLen(list)
	changed := false
	elems := make([]Handle, 0, n)
	for i := 0; i < n; i++ {
		old := src.ListAt(list, i)
		visited := v.Visit(old)
		if visited != old {
			changed = true
		}
		if visited.IsNil() {
			changed = true
			continue
		}
		if visited.Kind == KindSyntaxList {
			changed = true
		}
		elems = appendVisitedHandle(elems, visited)
	}
	if !changed {
		return list
	}
	loc := src.ListLoc(list)
	if !src.ListHasTrailingComma(list) && len(elems) > 0 && len(elems) < n && loc.End() >= 0 {
		lastEnd := elems[len(elems)-1].End()
		if lastEnd >= 0 && lastEnd < loc.End() {
			loc = core.NewTextRange(loc.Pos(), lastEnd)
		}
	}
	return v.Factory.List(loc, elems...)
}

func (f *Factory) BinaryExpression(p BinaryParts) Handle {
	h := f.NewBinaryExpression(0, p.Left, Handle{}, p.Operator, p.Right)
	h.SetLoc(p.Loc)
	return h
}

// List allocates a packed NodeList-equivalent. Elements may be zero.
func (f *Factory) List(loc core.TextRange, elems ...Handle) ListRef {
	list := f.store.AllocList(loc, len(elems))
	for i, e := range elems {
		if !e.IsNil() && e.s != f.store {
			e = f.CopySubtree(e)
		}
		f.store.SetListAt(list, i, e)
	}
	return list
}

func (f *Factory) NewList(nodes []Handle) ListRef {
	return f.List(core.UndefinedTextRange(), nodes...)
}

func (f *Factory) RelocateList(list ListRef, loc core.TextRange) ListRef {
	if list == 0 {
		return f.List(loc)
	}
	return f.List(loc, f.store.ListSlice(list)...)
}

func (f *Factory) NewModifier(kind Kind) Handle {
	return f.NewToken(TokenSyntaxKind(kind))
}

func (f *Factory) NewModifierList(nodes []Handle) ListRef {
	return f.NewList(nodes)
}

func (f *Factory) ArrayLiteral(p ArrayLiteralParts) Handle {
	h := f.NewArrayLiteralExpression(p.Elements, false)
	h.SetLoc(p.Loc)
	return h
}

func (f *Factory) Parameter(p ParameterParts) Handle {
	h := f.NewParameterDeclaration(0, p.DotDotDot, p.Name, p.Question, p.Type, p.Initializer)
	h.SetLoc(p.Loc)
	return h
}

func (f *Factory) DeepCloneNode(node Handle) Handle {
	if node.IsNil() || f == nil {
		return node
	}
	if node.Store() != f.store {
		return f.CopySubtree(node)
	}
	var visitor *HandleVisitor
	visitor = NewHandleVisitor(func(n Handle) Handle {
		cloned := visitor.VisitEachChild(n)
		if cloned != n && f.hooks.OnClone != nil {
			f.hooks.OnClone(cloned, n)
		}
		return cloned
	}, f, HandleVisitorHooks{})
	return visitor.VisitNode(node)
}

func (f *Factory) FunctionExpression(loc core.TextRange, params ListRef) Handle {
	h := f.NewFunctionExpression(0, Handle{}, Handle{}, 0, params, Handle{}, Handle{}, Handle{})
	h.SetLoc(loc)
	return h
}

// NewSourceFile creates the Store-owned SourceFile syntax root. File metadata
// is initialized when a pointer view is materialized for legacy consumers.
func (f *Factory) NewSourceFile(statements ListRef, endOfFileToken Handle) Handle {
	h := f.createSlots(KindSourceFile, 0, core.UndefinedTextRange(), slotSourceFileCount, listSlotSourceFileCount)
	h.SetSourceFileEndOfFileToken(endOfFileToken)
	h.SetSourceFileStatements(statements)
	return h
}

func (f *Factory) UpdateSourceFile(node Handle, statements ListRef, endOfFileToken Handle) Handle {
	if node.IsNil() {
		return f.NewSourceFile(statements, endOfFileToken)
	}
	if endOfFileToken.IsNil() {
		endOfFileToken = node.SourceFileEndOfFileToken()
	}
	if statements == node.SourceFileStatements() && endOfFileToken == node.SourceFileEndOfFileToken() {
		return node
	}
	h := f.NewSourceFile(statements, endOfFileToken)
	return f.updateHandle(h, node)
}

func (h Handle) ParamType() Handle {
	h.requireKind(KindParameter)
	return h.ParameterDeclarationType()
}

func (h Handle) SetParamType(typ Handle) {
	h.requireKind(KindParameter)
	h.SetParameterDeclarationType(typ)
}

func (h Handle) ParamQuestion() Handle {
	h.requireKind(KindParameter)
	return h.ParameterDeclarationQuestionToken()
}

func (h Handle) SetParamQuestion(q Handle) {
	h.requireKind(KindParameter)
	h.SetParameterDeclarationQuestionToken(q)
}

func (h Handle) requireKind(k Kind) {
	h.mustLive()
	if h.Kind != k {
		panic("ast: Handle kind mismatch")
	}
}

func ReplaceHandleModifiers(f HandleFactory, node Handle, modifiers ListRef) Handle {
	switch node.Kind {
	case KindTypeParameter:
		return f.UpdateTypeParameterDeclaration(node, modifiers, node.Name(), node.Constraint(), node.Expression(), node.DefaultType())
	case KindParameter:
		return f.UpdateParameterDeclaration(node, modifiers, node.DotDotDotToken(), node.Name(), node.QuestionToken(), node.Type(), node.Initializer())
	case KindConstructorType:
		return f.UpdateConstructorTypeNode(node, modifiers, node.TypeParameterList(), node.ParameterList(), node.Type())
	case KindPropertySignature:
		return f.UpdatePropertySignatureDeclaration(node, modifiers, node.Name(), node.PostfixToken(), node.Type(), node.Initializer())
	case KindPropertyDeclaration:
		return f.UpdatePropertyDeclaration(node, modifiers, node.Name(), node.PostfixToken(), node.Type(), node.Initializer())
	case KindMethodSignature:
		return f.UpdateMethodSignatureDeclaration(node, modifiers, node.Name(), node.PostfixToken(), node.TypeParameterList(), node.ParameterList(), node.Type())
	case KindMethodDeclaration:
		return f.UpdateMethodDeclaration(node, modifiers, node.AsteriskToken(), node.Name(), node.PostfixToken(), node.TypeParameterList(), node.ParameterList(), node.Type(), node.FullSignature(), node.Body())
	case KindConstructor:
		return f.UpdateConstructorDeclaration(node, modifiers, node.TypeParameterList(), node.ParameterList(), node.Type(), node.FullSignature(), node.Body())
	case KindGetAccessor:
		return f.UpdateGetAccessorDeclaration(node, modifiers, node.Name(), node.TypeParameterList(), node.ParameterList(), node.Type(), node.FullSignature(), node.Body())
	case KindSetAccessor:
		return f.UpdateSetAccessorDeclaration(node, modifiers, node.Name(), node.TypeParameterList(), node.ParameterList(), node.Type(), node.FullSignature(), node.Body())
	case KindIndexSignature:
		return f.UpdateIndexSignatureDeclaration(node, modifiers, node.ParameterList(), node.Type())
	case KindFunctionExpression:
		return f.UpdateFunctionExpression(node, modifiers, node.AsteriskToken(), node.Name(), node.TypeParameterList(), node.ParameterList(), node.Type(), node.FullSignature(), node.Body())
	case KindArrowFunction:
		return f.UpdateArrowFunction(node, modifiers, node.TypeParameterList(), node.ParameterList(), node.Type(), node.FullSignature(), node.EqualsGreaterThanToken(), node.Body())
	case KindClassExpression:
		return f.UpdateClassExpression(node, modifiers, node.Name(), node.TypeParameterList(), node.HeritageClauses(), node.MemberList())
	case KindVariableStatement:
		return f.UpdateVariableStatement(node, modifiers, node.VariableStatementDeclarationList())
	case KindFunctionDeclaration:
		return f.UpdateFunctionDeclaration(node, modifiers, node.AsteriskToken(), node.Name(), node.TypeParameterList(), node.ParameterList(), node.Type(), node.FullSignature(), node.Body())
	case KindClassDeclaration:
		return f.UpdateClassDeclaration(node, modifiers, node.Name(), node.TypeParameterList(), node.HeritageClauses(), node.MemberList())
	case KindInterfaceDeclaration:
		return f.UpdateInterfaceDeclaration(node, modifiers, node.Name(), node.TypeParameterList(), node.HeritageClauses(), node.MemberList())
	case KindTypeAliasDeclaration:
		return f.UpdateTypeAliasDeclaration(node, modifiers, node.Name(), node.TypeParameterList(), node.Type())
	case KindEnumDeclaration:
		return f.UpdateEnumDeclaration(node, modifiers, node.Name(), node.MemberList())
	case KindModuleDeclaration:
		return f.UpdateModuleDeclaration(node, modifiers, node.ModuleDeclarationKeyword(), node.Name(), node.Body())
	case KindImportEqualsDeclaration:
		return f.UpdateImportEqualsDeclaration(node, modifiers, node.IsTypeOnly(), node.Name(), node.ModuleReference())
	case KindImportDeclaration:
		return f.UpdateImportDeclaration(node, modifiers, node.ImportClause(), node.ModuleSpecifier(), node.Attributes())
	case KindExportAssignment:
		return f.UpdateExportAssignment(node, modifiers, node.IsExportEquals(), node.Type(), node.Expression())
	case KindExportDeclaration:
		return f.UpdateExportDeclaration(node, modifiers, node.IsTypeOnly(), node.ExportClause(), node.ModuleSpecifier(), node.Attributes())
	}
	panic("ReplaceHandleModifiers: node has no modifiers")
}
