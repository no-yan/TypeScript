package main

import "strings"

// The rules of store-checker-perf-analysis-instructions-20261005.md 5.2. The
// lists that the source cannot give are the tables below; the walks'
// callbacks and the origins come from the source (source.go).

// Mechanisms of stage 3a, in report order.
const (
	mechLinks      = "1 links"
	mechAdapter    = "2 adapter"
	mechWalk       = "3 ancestor walk/utility (+callbacks)"
	mechAccessor   = "4 node accessor"
	mechOtherMap   = "5 other map"
	mechSelf       = "6 checker/binder/other tsc self"
	mechAlloc      = "7a runtime: allocation"
	mechMemmove    = "7b runtime: memmove"
	mechRuntime    = "7c runtime: other"
	mechStdlib     = "8 stdlib/other"
	mechOutside    = "outside the interval"
	mechRegionSelf = "9 interval function self"
)

var mechanisms = []string{mechLinks, mechAdapter, mechWalk, mechAccessor, mechOtherMap, mechSelf, mechAlloc, mechMemmove, mechRuntime, mechStdlib, mechRegionSelf}

// linkTypes are the links tables: their methods, and the maps and
// allocations under them, are links.
var linkTypes = map[string]bool{
	"core.LinkStore":                    true,
	"core.PagedLinkStore":               true,
	"checker.nodeLinkStore":             true,
	"checker.symbolArenaLinkStore":      true,
	"storechecker.nodeLinkStore":        true,
	"storechecker.symbolArenaLinkStore": true,
}

func (f fn) isLinks() bool { return f.Recv != "" && linkTypes[f.Pkg+"."+f.Recv] }

// walks are the ancestor walks and the utilities of 7d-2c (by their Pointer
// name; Node.File is the Store's GetSourceFileOfNode): the ast functions with
// a loop over the parents at daa33e7d88 (both packages), the utilities of the
// 7d-2c list (GetSymbolId, HasQuestionToken, IsGlobalSourceFile,
// IsExpressionOfOptionalChainRoot, GetEnclosingBlockScopeContainer,
// GetCombinedNodeFlags) and the slow paths that 7d-2c B split out of them.
var walks = map[string]bool{
	"ast.FindAncestor": true, "ast.FindAncestorOrQuit": true, "ast.FindAncestorKind": true, "ast.FindManyAncestors": true,
	"ast.GetCombinedNodeFlags": true, "ast.getCombinedNodeFlags": true,
	"ast.GetRootDeclaration": true, "ast.getRootDeclarationOfBindingElement": true,
	"ast.IsGlobalSourceFile": true, "ast.isGlobalSourceFile": true,
	"ast.GetSymbolId": true, "ast.newGetSymbolId": true,
	"ast.HasQuestionToken":                                        true,
	"ast.GetThisContainer":                                        true,
	"ast.GetSuperContainer":                                       true,
	"ast.GetAssignmentTarget":                                     true,
	"ast.GetEnclosingBlockScopeContainer":                         true,
	"ast.GetSourceFileOfNode":                                     true,
	"ast.(Node).File":                                             true,
	"ast.IsExpressionOfOptionalChainRoot":                         true,
	"ast.accessKind":                                              true,
	"ast.GetImmediatelyInvokedFunctionExpression":                 true,
	"ast.IsNodeDescendantOf":                                      true,
	"ast.IsPartOfTypeQuery":                                       true,
	"ast.IsThisInTypeQuery":                                       true,
	"ast.WalkUpBindingElementsAndPatterns":                        true,
	"ast.WalkUpParenthesizedExpressions":                          true,
	"ast.WalkUpParenthesizedTypes":                                true,
	"ast.IsExpressionNode":                                        true,
	"ast.IsDeclarationName":                                       true,
	"ast.GetAssignedName":                                         true,
	"ast.IsArrayLiteralOrObjectLiteralDestructuringPattern":       true,
	"ast.IsDeprecatedDeclarationWithCachedFlags":                  true,
	"ast.IsNameOfHeritageClauseTypeReference":                     true,
	"ast.isIdentifierInNonEmittingHeritageClause":                 true,
	"ast.isPartOfPossiblyValidTypeOrAbstractComputedPropertyName": true,
}

// notAccessors are the receivers of ast and ast/store that are not nodes,
// lists, flows or their views.
var notAccessors = map[string]bool{
	"Diagnostic": true, "DiagnosticsCollection": true, "NodeFactory": true, "Factory": true, "Builder": true,
	"NodeVisitor": true, "Bound": true, "Symbol": true, "IDSpace": true, "PositionMap": true,
	"PragmaSpecification": true, "Kind": true, "NodeSet": true, "OrderedNodeSet": true,
}

func (f fn) isAccessor() bool {
	return (f.Pkg == "ast" || f.Pkg == "ast/store") && f.Recv != "" && !notAccessors[f.Recv]
}

// transparent functions are skipped up the stack to the function that
// decides: the runtime and the generic helpers (slices, maps, core's and
// collections' generic types and functions but the links tables).
func (f fn) transparent() bool {
	if f.isRuntime() {
		return true
	}
	switch f.Pkg {
	case "slices", "maps":
		return true
	case "core", "collections":
		return f.Generic && !f.isLinks()
	}
	return false
}

func (f fn) isMap() bool {
	if f.Pkg == "internal/runtime/maps" {
		return true
	}
	if f.Pkg != "runtime" {
		return false
	}
	for _, p := range []string{"map", "memhash", "strhash", "aeshash", "nilinterhash", "interhash", "typehash", "f32hash", "f64hash", "c64hash", "c128hash", "int32Hash", "int64Hash"} {
		if strings.HasPrefix(f.Name, p) {
			return true
		}
	}
	return false
}

// isAlloc are the runtime's allocation paths (mallocgc and its entries, the
// caches and the heap under it).
func (f fn) isAlloc() bool {
	if f.Pkg != "runtime" {
		return false
	}
	switch f.Recv {
	case "mcache", "mcentral", "mheap", "pageAlloc", "fixalloc", "mspan", "gcBits":
		return true
	}
	if strings.HasPrefix(f.Name, "malloc") {
		return true
	}
	switch f.Name {
	case "newobject", "newarray", "makeslice", "makeslicecopy", "growslice", "makemap", "makemap_small", "makechan",
		"rawstring", "rawstringtmp", "rawbyteslice", "rawruneslice", "concatstrings", "slicebytetostring",
		"stringtoslicebyte", "stringtoslicerune", "intstring", "convT", "convTnoptr", "convTstring", "convTslice",
		"convT16", "convT32", "convT64", "nextFreeFast", "heapSetType", "heapSetTypeNoHeader", "heapSetTypeSmallHeader",
		"heapSetTypeLarge", "newUserArenaChunk", "persistentalloc", "sysAlloc":
		return true
	}
	return false
}

func (f fn) isMemmove() bool {
	if f.Pkg != "runtime" {
		return false
	}
	switch f.Name {
	case "memmove", "typedmemmove", "typedslicecopy", "wbMove", "bulkBarrierPreWrite":
		return true
	}
	return false
}

// sample3a is the classification of one sample of stage 3a.
type sample3a struct {
	Mechanism string `json:"mechanism"`
	Function  string `json:"function"` // the function that decided (Key), "" outside the interval
	Display   string `json:"display"`
	Origin    string `json:"origin"`
}

// classify3a classifies a stack (leaf first) by the rules of 5.2; region
// says whether a frame is the interval's function.
func (s *sources) classify3a(frames []string, region func(fn) bool) sample3a {
	fs := make([]fn, len(frames))
	top := -1
	for i, raw := range frames {
		fs[i] = parseFunc(raw)
		if top < 0 && region(fs[i]) {
			top = i
		}
	}
	if top < 0 {
		return sample3a{Mechanism: mechOutside}
	}
	if top == 0 {
		return sample3a{Mechanism: mechRegionSelf, Function: fs[0].Key(), Display: fs[0].Display()}
	}
	for _, f := range fs[:top] {
		if f.Pkg == "storeharness" {
			return s.decided(mechAdapter, fs[0])
		}
	}
	eff := top
	for i := range top {
		if !fs[i].transparent() {
			eff = i
			break
		}
	}
	decider := fs[eff]
	chain := fs[:eff]
	if fs[0].isRuntime() {
		for _, f := range chain {
			if f.isMap() {
				if decider.isLinks() {
					return s.decided(mechLinks, decider)
				}
				return s.decided(mechOtherMap, decider)
			}
		}
		for _, f := range chain {
			if f.isAlloc() {
				return s.decided(mechAlloc, decider)
			}
		}
		if fs[0].isMemmove() {
			return s.decided(mechMemmove, decider)
		}
		return s.decided(mechRuntime, decider)
	}
	if eff == top {
		return s.decided(mechRegionSelf, decider)
	}
	return s.decided(s.mechanismOf(decider), decider)
}

// mechanismOf is the mechanism of a function that is not transparent.
func (s *sources) mechanismOf(f fn) string {
	switch {
	case f.isLinks():
		return mechLinks
	case walks[f.Key()] || s.isWalkCallback(f):
		return mechWalk
	case f.isAccessor():
		return mechAccessor
	case f.isTsc():
		return mechSelf
	default:
		return mechStdlib
	}
}

func (s *sources) decided(mechanism string, f fn) sample3a {
	return sample3a{Mechanism: mechanism, Function: f.Key(), Display: f.Display(), Origin: s.origin(f)}
}

// Categories of stage 3b (6.1). A heap profile drops the runtime's frames at
// the leaf (runtime/pprof hides the allocator and the map code), so a map's
// allocation is the function that asked for it and is not told apart here.
const (
	catParseBind = "parse/bind products"
	catChecker   = "checker"
	catLinks     = "links"
	catOther     = "map/other"
)

var categories = []string{catParseBind, catChecker, catLinks, catOther}

// owners are the packages that decide the category of an allocation: the
// first frame up the stack in one of them, past the transparent functions,
// the frames outside tsc (strings.Clone, os.ReadFile) and the ast, printer
// and node builder frames that both phases call.
var owners = map[string]string{
	"parser": catParseBind, "storeparser": catParseBind, "binder": catParseBind, "storebinder": catParseBind,
	"scanner": catParseBind, "storescanner": catParseBind,
	"checker": catChecker, "storechecker": catChecker, "evaluator": catChecker, "storeevaluator": catChecker,
	"pseudochecker": catChecker, "storepseudochecker": catChecker,
}

var ownerTransparent = map[string]bool{
	"ast": true, "ast/store": true, "printer": true, "storeprinter": true, "nodebuilder": true, "storenodebuilder": true,
}

// site3b is the classification of one allocation stack of stage 3b.
type site3b struct {
	Category string `json:"category"`
	Site     string `json:"site"`  // the first function that is not transparent (Display)
	Owner    string `json:"owner"` // the function that decided the category (Display), "" when none did
}

// classify3b classifies an allocation stack (leaf first).
func classify3b(frames []string) site3b {
	fs := make([]fn, len(frames))
	for i, raw := range frames {
		fs[i] = parseFunc(raw)
	}
	eff := len(fs)
	for i, f := range fs {
		if !f.transparent() {
			eff = i
			break
		}
	}
	var site fn
	if eff < len(fs) {
		site = fs[eff]
	}
	out := site3b{Site: site.Display(), Category: catOther}
	for _, f := range fs[eff:] {
		if f.isLinks() {
			out.Category, out.Owner = catLinks, f.Display()
			return out
		}
		if c, ok := owners[f.Pkg]; ok {
			out.Category, out.Owner = c, f.Display()
			return out
		}
		if f.isTsc() && !f.transparent() && !ownerTransparent[f.Pkg] {
			out.Owner = f.Display()
			return out
		}
	}
	return out
}
