package printer

import (
	"github.com/microsoft/TypeScript/tsc/internal/ast"
	"github.com/microsoft/TypeScript/tsc/internal/collections"
	"github.com/microsoft/TypeScript/tsc/internal/core"
	"maps"
	"slices"
	"sync"
	"sync/atomic"
)

type EmitContext struct {
	Factory       *NodeFactory
	storeFile     *ast.SourceFile
	autoGenerate  map[ast.GlobalRef]*AutoGenerateInfo
	textSource    map[ast.GlobalRef]ast.GlobalRef
	original      map[ast.GlobalRef]ast.GlobalRef
	emitNodes     core.LinkStore[ast.GlobalRef, emitNode]
	assignedName  map[ast.GlobalRef]ast.GlobalRef
	classThis     map[ast.GlobalRef]ast.GlobalRef
	varScopeStack core.Stack[*varScope]
	letScopeStack core.Stack[*varScope]
	emitHelpers   collections.OrderedSet[*EmitHelper]
}
type environmentFlags int

const (
	environmentFlagsNone                         environmentFlags = 0
	environmentFlagsInParameters                 environmentFlags = 1 << 0
	environmentFlagsVariablesHoistedInParameters environmentFlags = 1 << 1
)

type varScope struct {
	variables                []ast.Handle
	functions                []ast.Handle
	flags                    environmentFlags
	initializationStatements []ast.Handle
}

func NewEmitContext() *EmitContext {
	c := &EmitContext{}
	c.Factory = NewNodeFactory(c)
	if s := c.Factory.Store(); s != nil {
		ast.RegisterStore(s)
	}
	return c
}

var emitContextPool = sync.Pool{New: func() any {
	return NewEmitContext()
}}

func GetEmitContext() (*EmitContext, func()) {
	c := emitContextPool.Get().(*EmitContext)
	return c, func() {
		c.Reset()
		emitContextPool.Put(c)
	}
}
func (c *EmitContext) Reset() {
	*c = EmitContext{Factory: c.Factory}
}

func (c *EmitContext) BindFileStore(file *ast.SourceFile) {
	if file == nil || file.ParseStore() == nil || c.Factory == nil {
		return
	}
	if c.storeFile == file && c.Factory.Store() == file.ParseStore() {
		return
	}
	c.storeFile = file
	if c.Factory.Store() != file.ParseStore() {
		c.Factory.Factory = ast.NewFactoryOn(file.ParseStore(), ast.FactoryHooks{OnCreate: c.onCreate})
	}
}

func (c *EmitContext) LockParseStoreWriter(file *ast.SourceFile) func() {
	if file == nil || file.ParseStore() == nil {
		return func() {}
	}
	unlock := file.LockParseStoreWriter()
	c.storeFile = file
	ast.RegisterFile(file)
	c.Factory.Factory = ast.NewFactoryOn(file.ParseStore(), ast.FactoryHooks{OnCreate: c.onCreate})
	return func() {
		c.storeFile = nil
		unlock()
	}
}
func (c *EmitContext) NodeIdentity(node ast.Handle) ast.GlobalRef {
	if node.IsNil() {
		return 0
	}
	return node.Global()
}
func (c *EmitContext) nodeForIdentity(identity ast.GlobalRef) ast.Handle {
	if identity == 0 {
		return ast.Handle{}
	}
	return ast.NodeOf(identity)
}
func (c *EmitContext) emitNode(node ast.Handle) *emitNode {
	return c.emitNodes.Get(c.NodeIdentity(node))
}
func (c *EmitContext) tryEmitNode(node ast.Handle) *emitNode {
	return c.emitNodes.TryGet(c.NodeIdentity(node))
}
func (c *EmitContext) hasEmitNode(node ast.Handle) bool {
	return c.emitNodes.Has(c.NodeIdentity(node))
}
func (c *EmitContext) onCreate(node ast.Handle) {
	node.SetFlags(node.Flags() | ast.NodeFlagsSynthesized)
}
func (c *EmitContext) onUpdate(updated ast.Handle, original ast.Handle) {
	c.SetOriginal(updated, original)
}
func (c *EmitContext) onClone(updated ast.Handle, original ast.Handle) {
	c.SetOriginal(updated, original)
	if ast.IsIdentifier(updated) || ast.IsPrivateIdentifier(updated) {
		if autoGenerate := c.autoGenerate[c.NodeIdentity(original)]; autoGenerate != nil {
			autoGenerateCopy := *autoGenerate
			c.autoGenerate[c.NodeIdentity(updated)] = &autoGenerateCopy
		}
	}
}

func (c *EmitContext) StoreFile() *ast.SourceFile {
	return c.storeFile
}

func (c *EmitContext) StoreFactory() *ast.Factory {
	if c.storeFile != nil && c.storeFile.ParseStore() != nil {
		return ast.NewFactoryOn(c.storeFile.ParseStore(), ast.FactoryHooks{OnCreate: c.onCreate})
	}
	return c.Factory.Factory
}

func (c *EmitContext) NewNodeVisitor(visit func(node ast.Handle) ast.Handle) *ast.HandleVisitor {
	return ast.NewHandleVisitor(visit, c.StoreFactory(), ast.HandleVisitorHooks{
		VisitParameters:         c.VisitParameters,
		VisitFunctionBody:       c.VisitFunctionBody,
		VisitIterationBody:      c.VisitIterationBody,
		VisitTopLevelStatements: c.VisitVariableEnvironment,
		VisitEmbeddedStatement:  c.VisitEmbeddedStatement,
	})
}

func (c *EmitContext) StartVariableEnvironment() {
	c.varScopeStack.Push(&varScope{})
	c.StartLexicalEnvironment()
}

func (c *EmitContext) EndVariableEnvironment() []ast.Handle {
	scope := c.varScopeStack.Pop()
	var statements []ast.Handle
	if len(scope.functions) > 0 {
		statements = slices.Clone(scope.functions)
	}
	if len(scope.variables) > 0 {
		varDeclList := c.Factory.NewVariableDeclarationList(c.Factory.NewList(scope.variables), ast.NodeFlagsNone)
		varStatement := c.Factory.NewVariableStatement(0, varDeclList)
		c.SetEmitFlags(varStatement, EFCustomPrologue)
		statements = append(statements, varStatement)
	}
	if len(scope.initializationStatements) > 0 {
		statements = append(statements, scope.initializationStatements...)
	}
	return append(statements, c.EndLexicalEnvironment()...)
}

func (c *EmitContext) EndAndMergeVariableEnvironmentList(statements ast.ListRef) ast.ListRef {
	var nodes []ast.Handle
	if statements != 0 {
		nodes = c.storeFile.ParseStore().ListSlice(statements)
	}
	if result, changed := c.endAndMergeVariableEnvironment(nodes); changed {
		return c.StoreFactory().List(c.storeFile.ParseStore().ListLoc(statements), result...)
	}
	return statements
}

func (c *EmitContext) EndAndMergeVariableEnvironment(statements []ast.Handle) []ast.Handle {
	result, _ := c.endAndMergeVariableEnvironment(statements)
	return result
}
func (c *EmitContext) endAndMergeVariableEnvironment(statements []ast.Handle) ([]ast.Handle, bool) {
	return c.mergeEnvironment(statements, c.EndVariableEnvironment())
}

func (c *EmitContext) AddVariableDeclaration(name ast.Handle) {
	varDecl := c.Factory.NewVariableDeclaration(name, ast.Handle{}, ast.Handle{}, ast.Handle{})
	c.SetEmitFlags(varDecl, EFNoNestedSourceMaps)
	scope := c.varScopeStack.Peek()
	scope.variables = append(scope.variables, varDecl)
	if scope.flags&environmentFlagsInParameters != 0 {
		scope.flags |= environmentFlagsVariablesHoistedInParameters
	}
}

func (c *EmitContext) AddHoistedFunctionDeclaration(node ast.Handle) {
	c.SetEmitFlags(node, EFCustomPrologue)
	scope := c.varScopeStack.Peek()
	scope.functions = append(scope.functions, node)
}

func (c *EmitContext) StartLexicalEnvironment() {
	c.letScopeStack.Push(&varScope{})
}

func (c *EmitContext) EndLexicalEnvironment() []ast.Handle {
	scope := c.letScopeStack.Pop()
	var statements []ast.Handle
	if len(scope.variables) > 0 {
		varDeclList := c.Factory.NewVariableDeclarationList(c.Factory.NewList(scope.variables), ast.NodeFlagsLet)
		varStatement := c.Factory.NewVariableStatement(0, varDeclList)
		c.SetEmitFlags(varStatement, EFCustomPrologue)
		statements = append(statements, varStatement)
	}
	return statements
}

func (c *EmitContext) EndAndMergeLexicalEnvironmentList(statements ast.ListRef) ast.ListRef {
	var nodes []ast.Handle
	if statements != 0 {
		nodes = c.storeFile.ParseStore().ListSlice(statements)
	}
	if result, changed := c.endAndMergeLexicalEnvironment(nodes); changed {
		return c.StoreFactory().List(c.storeFile.ParseStore().ListLoc(statements), result...)
	}
	return statements
}

func (c *EmitContext) EndAndMergeLexicalEnvironment(statements []ast.Handle) []ast.Handle {
	result, _ := c.endAndMergeLexicalEnvironment(statements)
	return result
}

func (c *EmitContext) endAndMergeLexicalEnvironment(statements []ast.Handle) ([]ast.Handle, bool) {
	return c.mergeEnvironment(statements, c.EndLexicalEnvironment())
}

func (c *EmitContext) AddLexicalDeclaration(name ast.Handle) {
	varDecl := c.Factory.NewVariableDeclaration(name, ast.Handle{}, ast.Handle{}, ast.Handle{})
	c.SetEmitFlags(varDecl, EFNoNestedSourceMaps)
	scope := c.letScopeStack.Peek()
	scope.variables = append(scope.variables, varDecl)
}

func (c *EmitContext) MergeEnvironmentList(statements ast.ListRef, declarations []ast.Handle) ast.ListRef {
	if result, changed := c.mergeEnvironment(c.storeFile.ParseStore().ListSlice(statements), declarations); changed {
		return c.StoreFactory().List(c.storeFile.ParseStore().ListLoc(statements), result...)
	}
	return statements
}

func (c *EmitContext) MergeEnvironment(statements []ast.Handle, declarations []ast.Handle) []ast.Handle {
	result, _ := c.mergeEnvironment(statements, declarations)
	return result
}
func (c *EmitContext) mergeEnvironment(statements []ast.Handle, declarations []ast.Handle) ([]ast.Handle, bool) {
	if len(declarations) == 0 {
		return statements, false
	}
	changed := false
	leftStandardPrologueEnd := findSpanEnd(statements, ast.IsPrologueDirective, 0)
	leftHoistedFunctionsEnd := findSpanEndWithEmitContext(c, statements, (*EmitContext).isHoistedFunction, leftStandardPrologueEnd)
	leftHoistedVariablesEnd := findSpanEndWithEmitContext(c, statements, (*EmitContext).isHoistedVariableStatement, leftHoistedFunctionsEnd)
	rightStandardPrologueEnd := findSpanEnd(declarations, ast.IsPrologueDirective, 0)
	rightHoistedFunctionsEnd := findSpanEndWithEmitContext(c, declarations, (*EmitContext).isHoistedFunction, rightStandardPrologueEnd)
	rightHoistedVariablesEnd := findSpanEndWithEmitContext(c, declarations, (*EmitContext).isHoistedVariableStatement, rightHoistedFunctionsEnd)
	rightCustomPrologueEnd := findSpanEndWithEmitContext(c, declarations, (*EmitContext).isCustomPrologue, rightHoistedVariablesEnd)
	if rightCustomPrologueEnd != len(declarations) {
		panic("Expected declarations to be valid standard or custom prologues")
	}
	left := statements
	if rightCustomPrologueEnd > rightHoistedVariablesEnd {
		left = core.Splice(left, leftHoistedVariablesEnd, 0, declarations[rightHoistedVariablesEnd:rightCustomPrologueEnd]...)
		changed = true
	}
	if rightHoistedVariablesEnd > rightHoistedFunctionsEnd {
		left = core.Splice(left, leftHoistedFunctionsEnd, 0, declarations[rightHoistedFunctionsEnd:rightHoistedVariablesEnd]...)
		changed = true
	}
	if rightHoistedFunctionsEnd > rightStandardPrologueEnd {
		left = core.Splice(left, leftStandardPrologueEnd, 0, declarations[rightStandardPrologueEnd:rightHoistedFunctionsEnd]...)
		changed = true
	}
	if rightStandardPrologueEnd > 0 {
		if leftStandardPrologueEnd == 0 {
			left = core.Splice(left, 0, 0, declarations[:rightStandardPrologueEnd]...)
			changed = true
		} else {
			var leftPrologues collections.Set[string]
			for i := range leftStandardPrologueEnd {
				leftPrologue := statements[i]
				leftPrologues.Add(leftPrologue.Expression().Text())
			}
			for i := rightStandardPrologueEnd - 1; i >= 0; i-- {
				rightPrologue := declarations[i]
				if !leftPrologues.Has(rightPrologue.Expression().Text()) {
					left = core.Concatenate([]ast.Handle{rightPrologue}, left)
					changed = true
				}
			}
		}
	}
	return left, changed
}
func (c *EmitContext) isCustomPrologue(node ast.Handle) bool {
	return c.EmitFlags(node)&EFCustomPrologue != 0
}
func (c *EmitContext) isHoistedFunction(node ast.Handle) bool {
	return c.isCustomPrologue(node) && ast.IsFunctionDeclaration(node)
}
func isHoistedVariable(node ast.Handle) bool {
	return ast.IsIdentifier(node.Name()) && node.Initializer().IsNil()
}
func (c *EmitContext) isHoistedVariableStatement(node ast.Handle) bool {
	return c.isCustomPrologue(node) && ast.IsVariableStatement(node) && core.Every(node.Store().ListSlice(node.VariableStatementDeclarationList().VariableDeclarationListDeclarations()), isHoistedVariable)
}

func (c *EmitContext) HasAutoGenerateInfo(node ast.Handle) bool {
	if !node.IsNil() {
		_, ok := c.autoGenerate[c.NodeIdentity(node)]
		return ok
	}
	return false
}

func (c *EmitContext) GetAutoGenerateInfo(name ast.Handle) *AutoGenerateInfo {
	if name.IsNil() {
		return nil
	}
	return c.autoGenerate[c.NodeIdentity(name)]
}

func (c *EmitContext) GetNodeForGeneratedName(name ast.Handle) ast.Handle {
	if autoGenerate := c.GetAutoGenerateInfo(name); autoGenerate != nil && autoGenerate.Flags.IsNode() {
		return c.getNodeForGeneratedNameWorker(autoGenerate.Node, autoGenerate.Id)
	}
	return name
}
func (c *EmitContext) getNodeForGeneratedNameWorker(node ast.Handle, autoGenerateId AutoGenerateId) ast.Handle {
	original := c.Original(node)
	for !original.IsNil() {
		node = original
		if ast.IsMemberName(node) {
			autoGenerate := c.GetAutoGenerateInfo(node)
			if autoGenerate == nil || autoGenerate.Flags.IsNode() && autoGenerate.Id != autoGenerateId {
				break
			}
			if autoGenerate.Flags.IsNode() {
				original = autoGenerate.Node
				continue
			}
		}
		original = c.Original(node)
	}
	return node
}

type AutoGenerateOptions struct {
	Flags  GeneratedIdentifierFlags
	Prefix string
	Suffix string
}

var nextAutoGenerateId atomic.Uint32

type AutoGenerateId uint32
type AutoGenerateInfo struct {
	Flags  GeneratedIdentifierFlags
	Id     AutoGenerateId
	Prefix string
	Suffix string
	Node   ast.Handle
}

func (c *EmitContext) SetOriginal(node ast.Handle, original ast.Handle) {
	c.SetOriginalEx(node, original, false)
}
func (c *EmitContext) UnsetOriginal(node ast.Handle) {
	delete(c.original, c.NodeIdentity(node))
}
func (c *EmitContext) SetOriginalEx(node ast.Handle, original ast.Handle, allowOverwrite bool) {
	if original.IsNil() {
		panic("Original cannot be nil.")
	}
	if c.original == nil {
		c.original = make(map[ast.GlobalRef]ast.GlobalRef)
	}
	nodeIdentity := c.NodeIdentity(node)
	originalIdentity := c.NodeIdentity(original)
	existing, ok := c.original[nodeIdentity]
	if !ok {
		c.original[nodeIdentity] = originalIdentity
		if emitNode := c.emitNodes.TryGet(originalIdentity); emitNode != nil {
			c.emitNodes.Get(nodeIdentity).copyFrom(emitNode)
		}
	} else if !allowOverwrite && existing != originalIdentity {
		panic("Original node already set.")
	} else if allowOverwrite {
		c.original[nodeIdentity] = originalIdentity
	}
}

func (c *EmitContext) Original(node ast.Handle) ast.Handle {
	return c.nodeForIdentity(c.original[c.NodeIdentity(node)])
}

func (c *EmitContext) MostOriginal(node ast.Handle) ast.Handle {
	if !node.IsNil() {
		original := c.Original(node)
		for !original.IsNil() {
			node = original
			original = c.Original(node)
		}
	}
	return node
}

func (c *EmitContext) ParseNode(node ast.Handle) ast.Handle {
	node = c.MostOriginal(node)
	if !node.IsNil() && ast.IsParseTreeNode(node) {
		return node
	}
	return ast.Handle{}
}
func (c *EmitContext) IsFileLevelUniqueName(sourceFile *ast.SourceFile, name string, hasGlobalName func(string) bool) bool {
	if hasGlobalName != nil && hasGlobalName(name) {
		return false
	}
	if original := c.MostOriginal(sourceFile.ParseRoot()); !original.IsNil() {
		if file := ast.GetSourceFileOfNode(original); file != nil {
			sourceFile = file
		}
	}
	return !sourceFile.HasIdentifier(name)
}

type emitNodeFlags uint32

const (
	hasCommentRange emitNodeFlags = 1 << iota
	hasSourceMapRange
)

type SnippetKind int

const (
	SnippetKindTabStop SnippetKind = iota
)

type SnippetElement struct {
	Kind  SnippetKind
	Order int
}
type SynthesizedComment struct {
	Kind               ast.Kind
	Loc                core.TextRange
	HasLeadingNewLine  bool
	HasTrailingNewLine bool
	Text               string
}
type emitNode struct {
	flags                     emitNodeFlags
	emitFlags                 EmitFlags
	commentRange              core.TextRange
	sourceMapRange            core.TextRange
	tokenSourceMapRanges      map[ast.Kind]core.TextRange
	helpers                   []*EmitHelper
	externalHelpersModuleName ast.Handle
	leadingComments           []SynthesizedComment
	trailingComments          []SynthesizedComment
	typeNode                  ast.Handle
	snippetElement            *SnippetElement
}

func (e *emitNode) copyFrom(source *emitNode) {
	e.flags = source.flags
	e.emitFlags = source.emitFlags
	e.commentRange = source.commentRange
	e.sourceMapRange = source.sourceMapRange
	e.tokenSourceMapRanges = maps.Clone(source.tokenSourceMapRanges)
	e.helpers = slices.Clone(source.helpers)
	e.externalHelpersModuleName = source.externalHelpersModuleName
	if source.snippetElement != nil {
		snippetElement := *source.snippetElement
		e.snippetElement = &snippetElement
	}
}
func (c *EmitContext) EmitFlags(node ast.Handle) EmitFlags {
	if emitNode := c.tryEmitNode(node); emitNode != nil {
		return emitNode.emitFlags
	}
	return EFNone
}
func (c *EmitContext) SetEmitFlags(node ast.Handle, flags EmitFlags) {
	c.emitNode(node).emitFlags = flags
}
func (c *EmitContext) AddEmitFlags(node ast.Handle, flags EmitFlags) {
	c.emitNode(node).emitFlags |= flags
}
func (c *EmitContext) SnippetElement(node ast.Handle) *SnippetElement {
	if emitNode := c.tryEmitNode(node); emitNode != nil {
		return emitNode.snippetElement
	}
	return nil
}
func (c *EmitContext) SetSnippetElement(node ast.Handle, snippetElement SnippetElement) {
	c.emitNode(node).snippetElement = &snippetElement
}

func (c *EmitContext) CommentRange(node ast.Handle) core.TextRange {
	if emitNode := c.tryEmitNode(node); emitNode != nil && emitNode.flags&hasCommentRange != 0 {
		return emitNode.commentRange
	}
	return node.Loc()
}

func (c *EmitContext) SetCommentRange(node ast.Handle, loc core.TextRange) {
	emitNode := c.emitNode(node)
	emitNode.commentRange = loc
	emitNode.flags |= hasCommentRange
}

func (c *EmitContext) AssignCommentRange(to ast.Handle, from ast.Handle) {
	c.SetCommentRange(to, c.CommentRange(from))
}

func (c *EmitContext) SourceMapRange(node ast.Handle) core.TextRange {
	if emitNode := c.tryEmitNode(node); emitNode != nil && emitNode.flags&hasSourceMapRange != 0 {
		return emitNode.sourceMapRange
	}
	return node.Loc()
}

func (c *EmitContext) SetSourceMapRange(node ast.Handle, loc core.TextRange) {
	emitNode := c.emitNode(node)
	emitNode.sourceMapRange = loc
	emitNode.flags |= hasSourceMapRange
}

func (c *EmitContext) AssignSourceMapRange(to ast.Handle, from ast.Handle) {
	c.SetSourceMapRange(to, c.SourceMapRange(from))
}

func (c *EmitContext) AssignCommentAndSourceMapRanges(to ast.Handle, from ast.Handle) {
	emitNode := c.emitNode(to)
	commentRange := c.CommentRange(from)
	sourceMapRange := c.SourceMapRange(from)
	emitNode.commentRange = commentRange
	emitNode.sourceMapRange = sourceMapRange
	emitNode.flags |= hasCommentRange | hasSourceMapRange
}

func (c *EmitContext) TokenSourceMapRange(node ast.Handle, kind ast.Kind) (core.TextRange, bool) {
	if emitNode := c.tryEmitNode(node); emitNode != nil && emitNode.tokenSourceMapRanges != nil {
		if loc, ok := emitNode.tokenSourceMapRanges[kind]; ok {
			return loc, true
		}
	}
	return core.TextRange{}, false
}

func (c *EmitContext) SetTokenSourceMapRange(node ast.Handle, kind ast.Kind, loc core.TextRange) {
	emitNode := c.emitNode(node)
	if emitNode.tokenSourceMapRanges == nil {
		emitNode.tokenSourceMapRanges = make(map[ast.Kind]core.TextRange)
	}
	emitNode.tokenSourceMapRanges[kind] = loc
}
func (c *EmitContext) AssignedName(node ast.Handle) ast.Handle {
	return c.nodeForIdentity(c.assignedName[c.NodeIdentity(node)])
}
func (c *EmitContext) TextSource(node ast.Handle) ast.Handle {
	return c.nodeForIdentity(c.textSource[c.NodeIdentity(node)])
}
func (c *EmitContext) SetTextSource(node ast.Handle, source ast.Handle) {
	if c.textSource == nil {
		c.textSource = make(map[ast.GlobalRef]ast.GlobalRef)
	}
	c.textSource[c.NodeIdentity(node)] = c.NodeIdentity(source)
}
func (c *EmitContext) SetAssignedName(node ast.Handle, name ast.Handle) {
	if c.assignedName == nil {
		c.assignedName = make(map[ast.GlobalRef]ast.GlobalRef)
	}
	c.assignedName[c.NodeIdentity(node)] = c.NodeIdentity(name)
}
func (c *EmitContext) ClassThis(node ast.Handle) ast.Handle {
	return c.nodeForIdentity(c.classThis[c.NodeIdentity(node)])
}
func (c *EmitContext) SetClassThis(node ast.Handle, classThis ast.Handle) {
	if c.classThis == nil {
		c.classThis = make(map[ast.GlobalRef]ast.GlobalRef)
	}
	c.classThis[c.NodeIdentity(node)] = c.NodeIdentity(classThis)
}
func (c *EmitContext) RequestEmitHelper(helper *EmitHelper) {
	if helper.Scoped {
		panic("Cannot request a scoped emit helper")
	}
	for _, h := range helper.Dependencies {
		c.RequestEmitHelper(h)
	}
	c.emitHelpers.Add(helper)
}
func (c *EmitContext) ReadEmitHelpers() []*EmitHelper {
	helpers := slices.Collect(c.emitHelpers.Values())
	c.emitHelpers.Clear()
	return helpers
}
func (c *EmitContext) AddEmitHelper(node ast.Handle, helper ...*EmitHelper) {
	emitNode := c.emitNode(node)
	for _, h := range helper {
		emitNode.helpers = core.AppendIfUnique(emitNode.helpers, h)
	}
}
func (c *EmitContext) MoveEmitHelpers(source ast.Handle, target ast.Handle, predicate func(helper *EmitHelper) bool) {
	sourceEmitNode := c.tryEmitNode(source)
	if sourceEmitNode == nil {
		return
	}
	sourceEmitHelpers := sourceEmitNode.helpers
	if len(sourceEmitHelpers) == 0 {
		return
	}
	targetEmitNode := c.emitNode(target)
	helpersRemoved := 0
	for i := range sourceEmitHelpers {
		helper := sourceEmitHelpers[i]
		if predicate(helper) {
			helpersRemoved++
			targetEmitNode.helpers = core.AppendIfUnique(targetEmitNode.helpers, helper)
		} else if helpersRemoved > 0 {
			sourceEmitHelpers[i-helpersRemoved] = helper
		}
	}
	if helpersRemoved > 0 {
		sourceEmitHelpers = sourceEmitHelpers[:len(sourceEmitHelpers)-helpersRemoved]
		sourceEmitNode.helpers = sourceEmitHelpers
	}
}
func (c *EmitContext) GetEmitHelpers(node ast.Handle) []*EmitHelper {
	emitNode := c.tryEmitNode(node)
	if emitNode != nil {
		return emitNode.helpers
	}
	return nil
}
func (c *EmitContext) GetExternalHelpersModuleName(node *ast.SourceFile) ast.Handle {
	if parseNode := c.ParseNode(node.ParseRoot()); !parseNode.IsNil() {
		if emitNode := c.tryEmitNode(parseNode); emitNode != nil {
			return emitNode.externalHelpersModuleName
		}
	}
	return ast.Handle{}
}
func (c *EmitContext) SetExternalHelpersModuleName(node *ast.SourceFile, name ast.Handle) {
	parseNode := c.ParseNode(node.ParseRoot())
	if parseNode.IsNil() {
		panic("Node must be a parse tree node or have an Original pointer to a parse tree node.")
	}
	emitNode := c.emitNode(parseNode)
	emitNode.externalHelpersModuleName = name
}
func (c *EmitContext) HasRecordedExternalHelpers(node *ast.SourceFile) bool {
	if parseNode := c.ParseNode(node.ParseRoot()); !parseNode.IsNil() {
		emitNode := c.tryEmitNode(parseNode)
		return emitNode != nil && (!emitNode.externalHelpersModuleName.IsNil() || emitNode.emitFlags&EFExternalHelpers != 0)
	}
	return false
}
func (c *EmitContext) IsCallToHelper(firstSegment ast.Handle, helperName string) bool {
	return ast.IsCallExpression(firstSegment) && ast.IsIdentifier(firstSegment.Expression()) && (c.EmitFlags(firstSegment.Expression())&EFHelperName) != 0 && firstSegment.Expression().Text() == helperName
}
func (c *EmitContext) VisitVariableEnvironment(nodes ast.ListRef, visitor *ast.HandleVisitor) ast.ListRef {
	c.StartVariableEnvironment()
	return c.EndAndMergeVariableEnvironmentList(visitor.VisitNodes(nodes))
}
func (c *EmitContext) VisitParameters(nodes ast.ListRef, visitor *ast.HandleVisitor) ast.ListRef {
	c.StartVariableEnvironment()
	scope := c.varScopeStack.Peek()
	oldFlags := scope.flags
	scope.flags |= environmentFlagsInParameters
	nodes = visitor.VisitNodes(nodes)
	if scope.flags&environmentFlagsVariablesHoistedInParameters != 0 {
		nodes = c.addDefaultValueAssignmentsIfNeeded(nodes)
	}
	scope.flags = oldFlags
	return nodes
}
func (c *EmitContext) addDefaultValueAssignmentsIfNeeded(nodeList ast.ListRef) ast.ListRef {
	if nodeList == 0 {
		return nodeList
	}
	var result []ast.Handle
	nodes := c.storeFile.ParseStore().ListSlice(nodeList)
	for i, parameter := range nodes {
		updated := c.addDefaultValueAssignmentIfNeeded(parameter)
		if updated != parameter {
			if result == nil {
				result = slices.Clone(nodes)
			}
			result[i] = updated
		}
	}
	if result != nil {
		return c.StoreFactory().List(c.storeFile.ParseStore().ListLoc(nodeList), result...)
	}
	return nodeList
}
func (c *EmitContext) addDefaultValueAssignmentIfNeeded(parameter ast.Handle) ast.Handle {
	if !parameter.DotDotDotToken().IsNil() {
		return parameter
	} else if ast.IsBindingPattern(parameter.Name()) {
		return c.addDefaultValueAssignmentForBindingPattern(parameter)
	} else if !parameter.Initializer().IsNil() {
		return c.addDefaultValueAssignmentForInitializer(parameter, parameter.Name(), parameter.Initializer())
	}
	return parameter
}
func (c *EmitContext) addDefaultValueAssignmentForBindingPattern(parameter ast.Handle) ast.Handle {
	var initNode ast.Handle
	if !parameter.Initializer().IsNil() {
		initNode = c.Factory.NewConditionalExpression(c.Factory.NewStrictEqualityExpression(c.Factory.NewGeneratedNameForNode(parameter), c.Factory.NewVoidZeroExpression()), c.Factory.NewToken(ast.KindQuestionToken), parameter.Initializer(), c.Factory.NewToken(ast.KindColonToken), c.Factory.NewGeneratedNameForNode(parameter))
	} else {
		initNode = c.Factory.NewGeneratedNameForNode(parameter)
	}
	c.AddInitializationStatement(c.Factory.NewVariableStatement(0, c.Factory.NewVariableDeclarationList(c.Factory.NewList([]ast.Handle{c.Factory.NewVariableDeclaration(parameter.Name(), ast.Handle{}, parameter.Type(), initNode)}), ast.NodeFlagsNone)))
	return c.Factory.UpdateParameterDeclaration(parameter, parameter.Modifiers(), parameter.DotDotDotToken(), c.Factory.NewGeneratedNameForNode(parameter), parameter.QuestionToken(), parameter.Type(), ast.Handle{})
}
func (c *EmitContext) addDefaultValueAssignmentForInitializer(parameter ast.Handle, name ast.Handle, initializer ast.Handle) ast.Handle {
	c.AddEmitFlags(initializer, EFNoSourceMap|EFNoComments)
	nameClone := c.Factory.DeepCloneNode(name)
	c.AddEmitFlags(nameClone, EFNoSourceMap)
	initAssignment := c.Factory.NewAssignmentExpression(nameClone, initializer)
	initAssignment.SetLoc(parameter.Loc())
	c.AddEmitFlags(initAssignment, EFNoComments)
	initBlock := c.Factory.NewBlock(c.Factory.NewList([]ast.Handle{c.Factory.NewExpressionStatement(initAssignment)}), false)
	initBlock.SetLoc(parameter.Loc())
	c.AddEmitFlags(initBlock, EFSingleLine|EFNoTrailingSourceMap|EFNoTokenSourceMaps|EFNoComments)
	c.AddInitializationStatement(c.Factory.NewIfStatement(c.Factory.NewTypeCheck(c.Factory.DeepCloneNode(name), "undefined"), initBlock, ast.Handle{}))
	return c.Factory.UpdateParameterDeclaration(parameter, parameter.Modifiers(), parameter.DotDotDotToken(), parameter.Name(), parameter.QuestionToken(), parameter.Type(), ast.Handle{})
}
func (c *EmitContext) AddInitializationStatement(node ast.Handle) {
	scope := c.varScopeStack.Peek()
	if scope == nil {
		panic("Tried to add an initialization statement without a surrounding variable scope")
	}
	c.AddEmitFlags(node, EFCustomPrologue)
	scope.initializationStatements = append(scope.initializationStatements, node)
}
func (c *EmitContext) ConvertToFunctionBlock(node ast.Handle, multiLine bool) ast.Handle {
	if ast.IsBlock(node) {
		return node
	}
	returnStatement := c.Factory.NewReturnStatement(node)
	returnStatement.SetLoc(node.Loc())
	statements := c.StoreFactory().List(node.Loc(), returnStatement)
	block := c.Factory.NewBlock(statements, multiLine)
	block.SetLoc(node.Loc())
	return block
}
func (c *EmitContext) VisitFunctionBody(node ast.Handle, visitor *ast.HandleVisitor) ast.Handle {
	updated := visitor.VisitNode(node)
	declarations := c.EndVariableEnvironment()
	if len(declarations) == 0 {
		return updated
	}
	if updated.IsNil() {
		return c.Factory.NewBlock(c.Factory.NewList(declarations), true)
	}
	if !ast.IsBlock(updated) {
		c.AddEmitFlags(updated, EFNoComments)
		block := c.ConvertToFunctionBlock(updated, false)
		return c.Factory.UpdateBlock(block, c.MergeEnvironmentList(block.StatementList(), declarations), block.BlockMultiLine())
	}
	return c.Factory.UpdateBlock(updated, c.MergeEnvironmentList(updated.StatementList(), declarations), updated.BlockMultiLine())
}
func (c *EmitContext) VisitIterationBody(body ast.Handle, visitor *ast.HandleVisitor) ast.Handle {
	if body.IsNil() {
		return ast.Handle{}
	}
	c.StartLexicalEnvironment()
	updated := c.VisitEmbeddedStatement(body, visitor)
	if updated.IsNil() {
		panic("Expected visitor to return a statement.")
	}
	statements := c.EndLexicalEnvironment()
	if len(statements) > 0 {
		if ast.IsBlock(updated) {
			statements = append(statements, updated.Statements()...)
			loc := updated.Loc()
			if c.storeFile != nil && c.storeFile.ParseStore() != nil && updated.StatementList() != 0 {
				loc = c.storeFile.ParseStore().ListLoc(updated.StatementList())
			}
			statementsList := c.StoreFactory().List(loc, statements...)
			return c.Factory.UpdateBlock(updated, statementsList, updated.BlockMultiLine())
		}
		statements = append(statements, updated)
		return c.Factory.NewBlock(c.Factory.NewList(statements), true)
	}
	return updated
}
func (c *EmitContext) VisitEmbeddedStatement(node ast.Handle, visitor *ast.HandleVisitor) ast.Handle {
	if node.IsNil() {
		return ast.Handle{}
	}
	embeddedStatement := visitor.VisitEmbeddedStatement(node)
	if embeddedStatement.IsNil() || ast.IsNotEmittedStatement(embeddedStatement) {
		emptyStatement := visitor.Factory.NewEmptyStatement()
		emptyStatement.SetLoc(node.Loc())
		c.SetOriginal(emptyStatement, node)
		c.AssignCommentRange(emptyStatement, node)
		return emptyStatement
	}
	return embeddedStatement
}
func (c *EmitContext) SetSyntheticLeadingComments(node ast.Handle, comments []SynthesizedComment) ast.Handle {
	c.emitNode(node).leadingComments = comments
	return node
}
func (c *EmitContext) AddSyntheticLeadingComment(node ast.Handle, kind ast.Kind, text string, hasTrailingNewLine bool) ast.Handle {
	c.emitNode(node).leadingComments = append(c.emitNode(node).leadingComments, SynthesizedComment{Kind: kind, Loc: core.NewTextRange(-1, -1), HasTrailingNewLine: hasTrailingNewLine, Text: text})
	return node
}
func (c *EmitContext) GetSyntheticLeadingComments(node ast.Handle) []SynthesizedComment {
	if c.hasEmitNode(node) {
		return c.emitNode(node).leadingComments
	}
	return nil
}
func (c *EmitContext) SetSyntheticTrailingComments(node ast.Handle, comments []SynthesizedComment) ast.Handle {
	c.emitNode(node).trailingComments = comments
	return node
}
func (c *EmitContext) AddSyntheticTrailingComment(node ast.Handle, kind ast.Kind, text string, hasTrailingNewLine bool) ast.Handle {
	c.emitNode(node).trailingComments = append(c.emitNode(node).trailingComments, SynthesizedComment{Kind: kind, Loc: core.NewTextRange(-1, -1), HasTrailingNewLine: hasTrailingNewLine, Text: text})
	return node
}
func (c *EmitContext) GetSyntheticTrailingComments(node ast.Handle) []SynthesizedComment {
	if c.hasEmitNode(node) {
		return c.emitNode(node).trailingComments
	}
	return nil
}

func (c *EmitContext) SetTypeNode(node ast.Handle, typeNode ast.Handle) {
	c.emitNode(node).typeNode = typeNode
}

func (c *EmitContext) GetTypeNode(node ast.Handle) ast.Handle {
	if emitNode := c.tryEmitNode(node); emitNode != nil {
		return emitNode.typeNode
	}
	return ast.Handle{}
}
func (c *EmitContext) NewNotEmittedStatement(node ast.Handle) ast.Handle {
	statement := c.Factory.NewNotEmittedStatement()
	statement.SetLoc(node.Loc())
	c.SetOriginal(statement, node)
	c.AssignCommentRange(statement, node)
	return statement
}
