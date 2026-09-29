package store

import (
	"github.com/microsoft/TypeScript/tsc/internal/ast"
)

// The JSDoc of a file (J-d in internal/ast/docs/store-checker-design-20260923.md
// 2.2, invariants 24, 25 and 27 of its section 3, and
// store-checker-7d1-implementation-instructions-20260928.md 3.2 and 3.3).
// The JSDoc of a JS file, and of a TS comment with @see or @link, is parsed
// with the file into its Store (eager); the rest of a TS file's JSDoc is
// parsed on the first access, each host once, into a Store of its own that
// is sealed before any reader sees it (lazy). Both are found through the
// file's cache, keyed by the host's NodeRef in the file's Store.

// parseJSDocForNode is the lazy JSDoc parser that internal/storeparser
// registers in its init (SetParseJSDocForNode, utilities_manual.go).
var parseJSDocForNode func(f *File, host Node) []Node

// JSDoc is (*ast.Node).JSDoc: with a nil file, the node's own file.
func (n Node) JSDoc(file *File) []Node {
	if n.h.flags&ast.NodeFlagsHasJSDoc == 0 {
		return nil
	}
	if file == nil {
		file = n.File()
		if file == nil {
			return nil
		}
	}
	file.checkHost(n)
	if file.hasLazyJSDoc {
		return file.resolveJSDoc(n)
	}
	return file.jsdocCache[n.Ref()]
}

// EagerJSDoc is (*ast.Node).EagerJSDoc: the JSDoc already parsed and cached,
// without a lazy parse.
func (n Node) EagerJSDoc(file *File) []Node {
	if n.h.flags&ast.NodeFlagsHasJSDoc == 0 {
		return nil
	}
	if file == nil {
		file = n.File()
		if file == nil {
			return nil
		}
	}
	file.checkHost(n)
	if file.hasLazyJSDoc {
		file.jsdocMu.RLock()
		jsdocs := file.jsdocCache[n.Ref()]
		file.jsdocMu.RUnlock()
		return jsdocs
	}
	return file.jsdocCache[n.Ref()]
}

// checkHost is invariant 27, in every build: the key is a NodeRef of the
// file's source Store, so a host of any other Store (a synthetic node, a
// clone, a node of another file or of a lazy JSDoc Store) would silently name
// another node's JSDoc, and a synthetic host would put a checker's private
// node in the shared cache. A path that reaches here with such a host needs
// its own design (7d design 2.2), not this cache.
func (f *File) checkHost(n Node) {
	if n.s != f.Store {
		panic("store: the JSDoc host is not a node of the file's source Store (invariant 27)")
	}
}

// resolveJSDoc is ast.SourceFile.resolveJSDoc: a read under the read lock,
// then the parse under the write lock with the check repeated. An empty
// result is recorded too, so a host is parsed once (invariant 24).
func (f *File) resolveJSDoc(n Node) []Node {
	if parseJSDocForNode == nil {
		panic("resolveJSDoc called but parseJSDocForNode is not registered; ensure the storeparser package is imported")
	}
	key := n.Ref()
	// Fast path: check cache under read lock
	f.jsdocMu.RLock()
	if jsdocs, ok := f.jsdocCache[key]; ok {
		f.jsdocMu.RUnlock()
		return jsdocs
	}
	f.jsdocMu.RUnlock()

	// Slow path: parse and cache under write lock
	f.jsdocMu.Lock()
	defer f.jsdocMu.Unlock()
	// Double-check after acquiring write lock
	if jsdocs, ok := f.jsdocCache[key]; ok {
		return jsdocs
	}
	jsdocs := parseJSDocForNode(f, n)
	if f.jsdocCache == nil {
		f.jsdocCache = make(map[NodeRef][]Node)
	}
	f.jsdocCache[key] = jsdocs
	return jsdocs
}

func (f *File) JSDocDiagnostics() []*ast.Diagnostic     { return f.jsdocDiagnostics }
func (f *File) SetJSDocDiagnostics(d []*ast.Diagnostic) { f.jsdocDiagnostics = d }
func (f *File) SetJSDocCache(m map[NodeRef][]Node)      { f.jsdocCache = m }
func (f *File) SetHasLazyJSDoc(v bool)                  { f.hasLazyJSDoc = v }

// FinishLazyJSDoc finishes b as the lazy JSDoc Store of host's file: the
// Store takes the next StoreID of the program, the file of host, a foreign
// parent edge from each of roots to host, and is sealed. An empty result
// makes no Store and takes no StoreID. Outside a program (host's Store has
// no IDSpace) the Store is outside one too: StoreID 0 and no IDSpace, so no
// Factory takes its nodes.
func FinishLazyJSDoc(b *Builder, host Node, roots []NodeRef) []Node {
	if len(roots) == 0 {
		return nil
	}
	s := b.Finish()
	hs := host.s
	s.file = hs.file
	s.space = hs.space
	s.id = 0
	if hs.space != nil {
		s.id = hs.space.Next()
	}
	// One foreign entry, the canonical host, for every root.
	s.foreignNodes = []Node{host}
	jsdocs := make([]Node, len(roots))
	for i, root := range roots {
		s.nodes[root].parent = foreignBit
		jsdocs[i] = s.node(root)
	}
	s.sealed = true
	return jsdocs
}
