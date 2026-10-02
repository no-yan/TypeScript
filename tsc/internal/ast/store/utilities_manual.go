package store

import (
	"slices"

	"github.com/microsoft/TypeScript/tsc/internal/ast"
)

// The P2 replacements of internal/ast/docs/store-checker-design-20260923.md
// 2.1 ("手書き差分の持ち方"): the functions of package ast that the rules of
// tools/cmd/storetranslate do not translate, written by hand. The translator
// does not emit a function that is here. Each carries the hash of the Pointer
// function it was written against; when that function changes, the
// translation stops until this version is checked and the hash updated
// (go run ./cmd/storetranslate -hash Name, from the tools module).

// GetLocals takes the Bound: the locals of a source Store live in its Bound,
// behind the container's reserved slot, and the binder makes a table on
// first use. A kind without the slot panics, as the Pointer version does on
// a nil LocalsContainerData. Only the binder calls it; everything else reads
// Node.Locals.
//
// storetranslate:pointer-hash e858c44b5f9bb737 utilities.go:GetLocals
func GetLocals(bound *Bound, container Node) SymbolTable {
	i, ok := container.LocalsRef()
	if !ok {
		panic("store: " + container.Kind().String() + " is not a locals container")
	}
	if i == 0 {
		i = bound.NewLocals()
		container.SetLocalsRef(i)
	}
	return bound.Locals(i)
}

// GetNodeAtPosition descends from the root into the child whose range holds
// position, and with includeJSDoc into a JSDoc comment of the node first.
// The file's root is file.Root(), not the file itself.
//
// storetranslate:pointer-hash f2ca68b56aa87bec utilities.go:GetNodeAtPosition
func GetNodeAtPosition(file *File, position int, includeJSDoc bool) Node {
	current := file.Root()
	for {
		var child Node
		if includeJSDoc {
			for _, jsdoc := range current.JSDoc(file) {
				if nodeContainsPosition(jsdoc, position) {
					child = jsdoc
					break
				}
			}
		}
		if child.IsNil() {
			current.ForEachChild(func(node Node) bool {
				if nodeContainsPosition(node, position) {
					child = node
					return true
				}
				return false
			})
		}
		if child.IsNil() || IsMetaProperty(child) {
			return current
		}
		current = child
	}
}

// SetParseJSDocForNode registers the lazy JSDoc parser (jsdoc.go): the
// parser takes the Store File and host, and returns Store nodes.
//
// storetranslate:pointer-hash e231d5a508aa9186 ast.go:SetParseJSDocForNode
func SetParseJSDocForNode(parse func(f *File, host Node) []Node) {
	parseJSDocForNode = parse
}

// GetSymbolId is ast.GetSymbolId on the gid (I1): a non-zero id, unique in
// the process, given on the first call and shared by the bind and every
// checker. The Bound's dense SymbolId is not it. It is 64 bits like
// ast.SymbolId: a narrower id would wrap to 0 and to ids given before, and a
// type cache keyed by it would confuse two symbols. Unlike the Pointer
// counter, newSymbolId does not wrap.
//
// storetranslate:pointer-hash 2d51f4a544629e45 utilities.go:GetSymbolId
func GetSymbolId(s *Symbol) uint64 {
	if id := s.gid.Load(); id != 0 {
		return id
	}
	return newGetSymbolId(s)
}

// newGetSymbolId is the slow side of GetSymbolId, out of line so that
// GetSymbolId is within the inline budget.
//
// storetranslate:store-only
// storetranslate:pointer-hash 2d51f4a544629e45 utilities.go:GetSymbolId
//
//go:noinline
func newGetSymbolId(s *Symbol) uint64 {
	id := s.gid.Load()
	if id == 0 {
		// Worst case, we burn a few ids if we have to CAS.
		id = newSymbolId()
		if !s.gid.CompareAndSwap(0, id) {
			id = s.gid.Load()
		}
	}
	return id
}

// IsLocalsContainer reads the kind's reserved slot table: a Store node has
// no LocalsContainerData to test for nil (7c (f) 17).
//
// storetranslate:pointer-hash f34ff2661a2eedb1 ast.go:IsLocalsContainer
func IsLocalsContainer(node Node) bool {
	_, ok := node.LocalsRef()
	return ok
}

// IsExternalModule reads the File: its ExternalModuleIndicator is a NodeRef
// of the file's Store, not a node.
//
// storetranslate:pointer-hash 636ea47f8f4e0323 utilities.go:IsExternalModule
func IsExternalModule(file *File) bool {
	return file.IsExternalModule()
}

// IsExternalOrCommonJSModule reads the File, whose CommonJSModuleIndicator is
// in the Bound.
//
// storetranslate:pointer-hash e239b1588abae7bd utilities.go:IsExternalOrCommonJSModule
func IsExternalOrCommonJSModule(file *File) bool {
	return file.IsExternalOrCommonJSModule()
}

// GetExternalModuleName: core.FirstOrNil over a List has no rule; the first
// argument is read with Len and At.
//
// storetranslate:pointer-hash 6c2540cec4b8481a utilities.go:GetExternalModuleName
func GetExternalModuleName(node Node) Node {
	switch node.Kind() {
	case ast.KindImportDeclaration, ast.KindJSImportDeclaration, ast.KindExportDeclaration:
		return node.ModuleSpecifier()
	case ast.KindImportEqualsDeclaration:
		if node.AsImportEqualsDeclaration().ModuleReference().Kind() == ast.KindExternalModuleReference {
			return node.AsImportEqualsDeclaration().ModuleReference().Expression()
		}
		return Node{}
	case ast.KindImportType:
		return getImportTypeNodeLiteral(node)
	case ast.KindCallExpression:
		if args := node.Arguments(); args.Len() > 0 {
			return args.At(0)
		}
		return Node{}
	case ast.KindModuleDeclaration:
		if IsStringLiteral(node.AsModuleDeclaration().Name()) {
			return node.AsModuleDeclaration().Name()
		}
		return Node{}
	}
	panic("Unhandled case in getExternalModuleName")
}

// NodeHasName: core.Some over a List has no rule; the declarations are read
// with Len and At.
//
// storetranslate:pointer-hash e1fd899ed00c65f6 utilities.go:NodeHasName
func NodeHasName(statement Node, id Node) bool {
	name := statement.Name()
	if !name.IsNil() {
		return IsIdentifier(name) && name.Text() == id.Text()
	}
	if IsVariableStatement(statement) {
		declarations := statement.AsVariableStatement().DeclarationList().AsVariableDeclarationList().Declarations()
		for i := range declarations.Len() {
			if NodeHasName(declarations.At(i), id) {
				return true
			}
		}
	}
	return false
}

// IsPotentiallyExecutableNode: core.Some over a List has no rule, as in
// NodeHasName.
//
// storetranslate:pointer-hash ffe4845962f5b7bd utilities.go:IsPotentiallyExecutableNode
func IsPotentiallyExecutableNode(node Node) bool {
	if ast.KindFirstStatement <= node.Kind() && node.Kind() <= ast.KindLastStatement {
		if IsVariableStatement(node) {
			declarationList := node.AsVariableStatement().DeclarationList()
			if GetCombinedNodeFlags(declarationList)&ast.NodeFlagsBlockScoped != 0 {
				return true
			}
			declarations := declarationList.AsVariableDeclarationList().Declarations()
			for i := range declarations.Len() {
				if !declarations.At(i).Initializer().IsNil() {
					return true
				}
			}
			return false
		}
		return true
	}
	return IsClassDeclaration(node) || IsEnumDeclaration(node) || IsModuleDeclaration(node)
}

// IsAsyncFunction: the Store has no BodyData; Body and AsteriskToken are role
// accessors of the four kinds.
//
// storetranslate:pointer-hash 3a84f556aaad98cd utilities.go:IsAsyncFunction
func IsAsyncFunction(node Node) bool {
	switch node.Kind() {
	case ast.KindFunctionDeclaration, ast.KindFunctionExpression, ast.KindArrowFunction, ast.KindMethodDeclaration:
		return !node.Body().IsNil() && node.AsteriskToken().IsNil() && HasSyntacticModifier(node, ast.ModifierFlagsAsync)
	}
	return false
}

// IsDeclarationNode tests the kind: the Pointer x.DeclarationData() is non-nil
// exactly for the kinds whose definition extends DeclarationBase
// (HasDeclarationData, generated). A lone DeclarationData() has no Store form
// (store-checker-7d2a-implementation-instructions-20260929.md 4.1).
//
// storetranslate:pointer-hash f75ed3ee84a9df81 ast.go:IsDeclarationNode
func IsDeclarationNode(node Node) bool {
	return node.HasDeclarationData()
}

// GetFunctionFlags reads the members of BodyData on the node (HasBodyData,
// AsteriskToken, Body).
//
// storetranslate:pointer-hash 79ef5ff003ff4ddd functionflags.go:GetFunctionFlags
func GetFunctionFlags(node Node) ast.FunctionFlags {
	if node.IsNil() {
		return ast.FunctionFlagsInvalid
	}
	if !node.HasBodyData() {
		return ast.FunctionFlagsInvalid
	}
	flags := ast.FunctionFlagsNormal
	switch node.Kind() {
	case ast.KindFunctionDeclaration, ast.KindFunctionExpression, ast.KindMethodDeclaration:
		if !node.AsteriskToken().IsNil() {
			flags |= ast.FunctionFlagsGenerator
		}
		fallthrough
	case ast.KindArrowFunction:
		if HasSyntacticModifier(node, ast.ModifierFlagsAsync) {
			flags |= ast.FunctionFlagsAsync
		}
	}
	if node.Body().IsNil() {
		flags |= ast.FunctionFlagsInvalid
	}
	return flags
}

// IsUnterminatedLiteral reads TokenFlags and TemplateFlags on the views that
// have them. The Pointer's LiteralLikeNodeBase.TokenFlags of JsxText and of
// a template literal is never set, so it is 0 there, as here.
//
// storetranslate:pointer-hash 4a6f12d112d50c43 utilities.go:IsUnterminatedLiteral
func IsUnterminatedLiteral(node Node) bool {
	return ast.IsLiteralKind(node.Kind()) && literalTokenFlags(node)&ast.TokenFlagsUnterminated != 0 ||
		ast.IsTemplateLiteralKind(node.Kind()) && templateFlags(node)&ast.TokenFlagsUnterminated != 0
}

// GetAllAccessorDeclarationsList is GetAllAccessorDeclarations on a list
// (3.4, List version).
//
// storetranslate:pointer-hash 14c9926de6cb9199 utilities.go:GetAllAccessorDeclarations
func GetAllAccessorDeclarationsList(parentDeclarations List, accessor Node) AllAccessorDeclarations {
	if HasDynamicName(accessor) {
		// dynamic names can only be match up via checker symbol lookup, just return an object with just this accessor
		return GetAllAccessorDeclarationsForDeclaration(accessor, []Node{accessor})
	}

	accessorName := GetPropertyNameForPropertyNameNode(accessor.Name())
	accessorStatic := IsStatic(accessor)
	var matches []Node
	for i := range parentDeclarations.Len() {
		member := parentDeclarations.At(i)
		if !IsAccessor(member) || IsStatic(member) != accessorStatic {
			continue
		}
		memberName := GetPropertyNameForPropertyNameNode(member.Name())
		if memberName == accessorName {
			matches = append(matches, member)
		}
	}
	return GetAllAccessorDeclarationsForDeclaration(accessor, matches)
}

// GetReparsedNodeForNode searches the reparsed clones of the file, which are
// NodeRefs of its Store, by position.
//
// storetranslate:pointer-hash 3d5245962b162d81 utilities.go:GetReparsedNodeForNode
func GetReparsedNodeForNode(node Node) Node {
	if !node.IsNil() && node.Flags()&ast.NodeFlagsJSDoc != 0 && node.Flags()&ast.NodeFlagsReparsed == 0 {
		if file := node.File(); file != nil && len(file.ReparsedClones) != 0 {
			pos, found := slices.BinarySearchFunc(file.ReparsedClones, node, func(ref NodeRef, n Node) int {
				return CompareNodePositions(file.Store.Node(ref), n)
			})
			if !found && pos > 0 {
				pos--
			}
			candidate := file.Store.Node(file.ReparsedClones[pos])
			if node.Loc().ContainedBy(candidate.Loc()) {
				if reparsed := findCloneInNode(candidate, node); !reparsed.IsNil() {
					return reparsed
				}
			}
		}
	}
	return node
}

// IndexOfNodeList is IndexOfNode on a list (3.4, List version).
//
// storetranslate:pointer-hash 625d482165f65d1d utilities.go:IndexOfNode
func IndexOfNodeList(nodes List, node Node) int {
	index, ok := BinarySearchFuncList(nodes, node, CompareNodePositions)
	if ok {
		return index
	}
	return -1
}

// GetSemanticJsxChildrenList is GetSemanticJsxChildren on a list (3.4, List
// version).
//
// storetranslate:pointer-hash cc36c98a569ca60e utilities.go:GetSemanticJsxChildren
func GetSemanticJsxChildrenList(children List) []Node {
	return FilterList(children, func(i Node) bool {
		switch i.Kind() {
		case ast.KindJsxExpression:
			return !i.Expression().IsNil()
		case ast.KindJsxText:
			return !i.AsJsxText().ContainsOnlyTriviaWhiteSpaces()
		default:
			return true
		}
	})
}

// The ancestor walks below are the generated versions reshaped to stay within
// the inline budget (store-checker-7d2c-accessor-tax-report-20261002.md 2):
// the rare side of each is a noinline call, and FindAncestorOrQuit reads the
// header and the parent edge directly.

// storetranslate:pointer-hash 2978672a03f3135f utilities.go:FindAncestorOrQuit
func FindAncestorOrQuit(node Node, callback func(Node) ast.FindAncestorResult) Node {
	for node.h != nil && node.h.kind != ast.KindUnknown { // !node.IsNil()
		switch callback(node) {
		case ast.FindAncestorQuit:
			return Node{}
		case ast.FindAncestorTrue:
			return node
		}
		node = node.s.node(node.h.parent)
	}
	return Node{}
}

// storetranslate:pointer-hash dca65bd012276201 utilities.go:GetRootDeclaration
func GetRootDeclaration(node Node) Node {
	if node.Kind() == ast.KindBindingElement {
		return getRootDeclarationOfBindingElement(node)
	}
	return node
}

// storetranslate:store-only
// storetranslate:pointer-hash dca65bd012276201 utilities.go:GetRootDeclaration
//
//go:noinline
func getRootDeclarationOfBindingElement(node Node) Node {
	for node.Kind() == ast.KindBindingElement {
		node = node.Parent().Parent()
	}
	return node
}

// GetCombinedNodeFlags takes the walk only for the kinds it climbs from; any
// other kind combines its own flags alone.
//
// storetranslate:pointer-hash 83da3d18b6ace2cc utilities.go:GetCombinedNodeFlags
func GetCombinedNodeFlags(node Node) ast.NodeFlags {
	switch node.Kind() {
	case ast.KindBindingElement, ast.KindVariableDeclaration, ast.KindVariableDeclarationList:
		return getCombinedNodeFlags(node)
	}
	return node.Flags()
}

// storetranslate:store-only
// storetranslate:pointer-hash 83da3d18b6ace2cc utilities.go:GetCombinedNodeFlags
//
//go:noinline
func getCombinedNodeFlags(node Node) ast.NodeFlags {
	node = GetRootDeclaration(node)
	flags := node.Flags()
	if node.Kind() == ast.KindVariableDeclaration {
		node = node.Parent()
	}
	if !node.IsNil() && node.Kind() == ast.KindVariableDeclarationList {
		flags |= node.Flags()
		node = node.Parent()
	}
	if !node.IsNil() && node.Kind() == ast.KindVariableStatement {
		flags |= node.Flags()
	}
	return flags
}

// storetranslate:pointer-hash f831f76b089fef80 utilities.go:IsGlobalSourceFile
func IsGlobalSourceFile(node Node) bool {
	return node.Kind() == ast.KindSourceFile && isGlobalSourceFile(node)
}

// storetranslate:store-only
// storetranslate:pointer-hash f831f76b089fef80 utilities.go:IsGlobalSourceFile
//
//go:noinline
func isGlobalSourceFile(node Node) bool {
	return !IsExternalOrCommonJSModule(node.File())
}

// HasQuestionToken reads the kind directly: QuestionToken gives a node of its
// Store or Node{}, and no sentinel is of kind QuestionToken.
//
// storetranslate:pointer-hash 8df070f5dc4fd89b utilities.go:HasQuestionToken
func HasQuestionToken(node Node) bool {
	t := node.QuestionToken()
	return t.h != nil && t.h.kind == ast.KindQuestionToken
}
