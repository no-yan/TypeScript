package store

import "github.com/microsoft/TypeScript/tsc/internal/ast"

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

// GetNodeAtPosition has no includeJSDoc: the Store has no JSDoc yet (7d-1).
// From the root, it descends into the child whose range holds position.
//
// storetranslate:pointer-hash f2ca68b56aa87bec utilities.go:GetNodeAtPosition
func GetNodeAtPosition(file *File, position int) Node {
	current := file.Root()
	for {
		var child Node
		current.ForEachChild(func(node Node) bool {
			if nodeContainsPosition(node, position) {
				child = node
				return true
			}
			return false
		})
		if child.IsNil() || IsMetaProperty(child) {
			return current
		}
		current = child
	}
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
