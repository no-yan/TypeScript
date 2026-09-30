package storebinder_test

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/microsoft/TypeScript/tsc/internal/ast"
	"github.com/microsoft/TypeScript/tsc/internal/ast/store"
	"github.com/microsoft/TypeScript/tsc/internal/ast/store/storetest"
	"github.com/microsoft/TypeScript/tsc/internal/binder"
	"github.com/microsoft/TypeScript/tsc/internal/core"
	"github.com/microsoft/TypeScript/tsc/internal/diagnostics"
	"github.com/microsoft/TypeScript/tsc/internal/parser"
	"github.com/microsoft/TypeScript/tsc/internal/storebinder"
	"github.com/microsoft/TypeScript/tsc/internal/storeparser"
)

// The unit equivalence of the name resolver
// (internal/ast/docs/store-checker-7d2a-implementation-instructions-20260929.md
// 6.2): on every identifier of every corpus file, JSDoc included, the Pointer
// binder.NameResolver and the translated storebinder.NameResolver resolve the
// identifier's text from it for Value, Type and Namespace, with isUse false
// and true, and without and with a not-found message. The two resolvers have
// the same simple callbacks (not the checker's): the symbols of a declaration,
// an empty or a fixture's Globals, a Lookup by flags, sentinels for arguments
// and require, and callbacks that only record. The results must be paired by
// the bind equivalence's symbol pairing (bindComparer.symbols, made from the
// bind, not from Resolve), and the recorded sequences of callback calls must
// be the same. This compares how the resolvers walk; the checker's lookup is
// compared in 2b.

// resolverSide is one side's identities: a node is its index in the paired
// walk, a symbol the index of its pair.
type resolverSide struct {
	name   func(n any) string // a node's id
	symbol func(s any) string // a symbol's id
	log    []string
	// lastLocations counts the kinds of the lastLocation of the successful
	// resolutions (the Pointer side's is reported).
	lastLocations map[ast.Kind]int
}

func (r *resolverSide) record(format string, args ...any) {
	r.log = append(r.log, fmt.Sprintf(format, args...))
}

// describeArgs is the arguments of a diagnostic: nodes and symbols by id,
// anything else as printed.
func (r *resolverSide) describeArgs(args []any) string {
	var parts []string
	for _, a := range args {
		switch v := a.(type) {
		case *ast.Node, store.Node:
			parts = append(parts, r.name(v))
		case *ast.Symbol, *store.Symbol:
			parts = append(parts, r.symbol(v))
		default:
			parts = append(parts, fmt.Sprint(v))
		}
	}
	return strings.Join(parts, ",")
}

// resolverPair is the two resolvers of a file, and their identities.
type resolverPair struct {
	pointer     *binder.NameResolver
	store       *storebinder.NameResolver
	p, s        *resolverSide
	nodeIndex   map[*ast.Node]int
	keyIndex    map[store.NodeKey]int
	symbolIndex map[*ast.Symbol]int
	storeIndex  map[*store.Symbol]int
}

// lookupAlias: the fixtures' Lookup also returns an alias (3).
func newResolverPair(m *bindComparer, pairs []storetest.Pair, pGlobals ast.SymbolTable, sGlobals store.SymbolTable, lookupAlias bool) *resolverPair {
	rp := &resolverPair{
		nodeIndex:   map[*ast.Node]int{},
		keyIndex:    map[store.NodeKey]int{},
		symbolIndex: map[*ast.Symbol]int{},
		storeIndex:  map[*store.Symbol]int{},
	}
	for i, pair := range pairs {
		rp.nodeIndex[pair.P] = i
		rp.keyIndex[pair.N.Key()] = i
	}
	i := 0
	for p, s := range m.symbols {
		rp.symbolIndex[p] = i
		rp.storeIndex[s] = i
		i++
	}
	pArguments, pRequire := &ast.Symbol{Name: "arguments"}, &ast.Symbol{Name: "require"}
	sArguments, sRequire := &store.Symbol{Name: "arguments"}, &store.Symbol{Name: "require"}
	rp.symbolIndex[pArguments], rp.storeIndex[sArguments] = -1, -1
	rp.symbolIndex[pRequire], rp.storeIndex[sRequire] = -2, -2
	for name, p := range pGlobals {
		rp.symbolIndex[p] = -10 - len(name)
	}
	for name, s := range sGlobals {
		rp.storeIndex[s] = -10 - len(name)
	}
	rp.p = &resolverSide{
		name: func(n any) string {
			node := n.(*ast.Node)
			if node == nil {
				return "nil"
			}
			if i, ok := rp.nodeIndex[node]; ok {
				return fmt.Sprint("n", i)
			}
			return "n?" + node.Kind.String()
		},
		symbol: func(s any) string {
			symbol := s.(*ast.Symbol)
			if symbol == nil {
				return "nil"
			}
			if i, ok := rp.symbolIndex[symbol]; ok {
				return fmt.Sprint("s", i)
			}
			return "s?" + normalizeName(symbol.Name)
		},
	}
	rp.s = &resolverSide{
		name: func(n any) string {
			node := n.(store.Node)
			if node.IsNil() {
				return "nil"
			}
			if i, ok := rp.keyIndex[node.Key()]; ok {
				return fmt.Sprint("n", i)
			}
			return "n?" + node.Kind().String()
		},
		symbol: func(s any) string {
			symbol := s.(*store.Symbol)
			if symbol == nil {
				return "nil"
			}
			if i, ok := rp.storeIndex[symbol]; ok {
				return fmt.Sprint("s", i)
			}
			return "s?" + normalizeName(symbol.Name)
		},
	}
	options := &core.CompilerOptions{}
	pCache := map[*ast.Node]core.Tristate{}
	sCache := map[store.NodeKey]core.Tristate{}
	ps, ss := rp.p, rp.s
	rp.pointer = &binder.NameResolver{
		CompilerOptions: options,
		GetSymbolOfDeclaration: func(node *ast.Node) *ast.Symbol {
			ps.record("GetSymbolOfDeclaration %s", ps.name(node))
			return node.Symbol()
		},
		Error: func(location *ast.Node, message *diagnostics.Message, args ...any) *ast.Diagnostic {
			ps.record("Error %s %d %s", ps.name(location), message.Code(), ps.describeArgs(args))
			return nil
		},
		Globals:         pGlobals,
		ArgumentsSymbol: pArguments,
		RequireSymbol:   pRequire,
		Lookup: func(symbols ast.SymbolTable, name string, meaning ast.SymbolFlags) *ast.Symbol {
			symbol := symbols[name]
			if symbol != nil && (symbol.Flags&meaning != 0 || lookupAlias && symbol.Flags&ast.SymbolFlagsAlias != 0) {
				ps.record("Lookup %q %d %s", name, meaning, ps.symbol(symbol))
				return symbol
			}
			ps.record("Lookup %q %d nil", name, meaning)
			return nil
		},
		SymbolReferenced: func(symbol *ast.Symbol, meaning ast.SymbolFlags) {
			ps.record("SymbolReferenced %s %d", ps.symbol(symbol), meaning)
		},
		SetRequiresScopeChangeCache: func(node *ast.Node, value core.Tristate) {
			ps.record("SetRequiresScopeChangeCache %s %d", ps.name(node), value)
			pCache[node] = value
		},
		GetRequiresScopeChangeCache: func(node *ast.Node) core.Tristate {
			ps.record("GetRequiresScopeChangeCache %s %d", ps.name(node), pCache[node])
			return pCache[node]
		},
		OnPropertyWithInvalidInitializer: func(location *ast.Node, name string, declaration *ast.Node, result *ast.Symbol) bool {
			ps.record("OnPropertyWithInvalidInitializer %s %q %s %s", ps.name(location), name, ps.name(declaration), ps.symbol(result))
			return false
		},
		OnFailedToResolveSymbol: func(location *ast.Node, name string, meaning ast.SymbolFlags, nameNotFoundMessage *diagnostics.Message) {
			ps.record("OnFailedToResolveSymbol %s %q %d %d", ps.name(location), name, meaning, nameNotFoundMessage.Code())
		},
		OnSuccessfullyResolvedSymbol: func(location *ast.Node, result *ast.Symbol, meaning ast.SymbolFlags, lastLocation *ast.Node, associated *ast.Node, withinDeferredContext bool) {
			if lastLocation != nil && ps.lastLocations != nil {
				ps.lastLocations[lastLocation.Kind]++
			}
			ps.record("OnSuccessfullyResolvedSymbol %s %s %d %s %s %v", ps.name(location), ps.symbol(result), meaning, ps.name(lastLocation), ps.name(associated), withinDeferredContext)
		},
	}
	rp.store = &storebinder.NameResolver{
		CompilerOptions: options,
		GetSymbolOfDeclaration: func(node store.Node) *store.Symbol {
			ss.record("GetSymbolOfDeclaration %s", ss.name(node))
			return node.Symbol()
		},
		Error: func(location store.Node, message *diagnostics.Message, args ...any) *store.Diagnostic {
			ss.record("Error %s %d %s", ss.name(location), message.Code(), ss.describeArgs(args))
			return nil
		},
		Globals:         sGlobals,
		ArgumentsSymbol: sArguments,
		RequireSymbol:   sRequire,
		Lookup: func(symbols store.SymbolTable, name string, meaning ast.SymbolFlags) *store.Symbol {
			symbol := symbols[name]
			if symbol != nil && (symbol.Flags&meaning != 0 || lookupAlias && symbol.Flags&ast.SymbolFlagsAlias != 0) {
				ss.record("Lookup %q %d %s", name, meaning, ss.symbol(symbol))
				return symbol
			}
			ss.record("Lookup %q %d nil", name, meaning)
			return nil
		},
		SymbolReferenced: func(symbol *store.Symbol, meaning ast.SymbolFlags) {
			ss.record("SymbolReferenced %s %d", ss.symbol(symbol), meaning)
		},
		SetRequiresScopeChangeCache: func(node store.Node, value core.Tristate) {
			ss.record("SetRequiresScopeChangeCache %s %d", ss.name(node), value)
			sCache[node.Key()] = value
		},
		GetRequiresScopeChangeCache: func(node store.Node) core.Tristate {
			ss.record("GetRequiresScopeChangeCache %s %d", ss.name(node), sCache[node.Key()])
			return sCache[node.Key()]
		},
		OnPropertyWithInvalidInitializer: func(location store.Node, name string, declaration store.Node, result *store.Symbol) bool {
			ss.record("OnPropertyWithInvalidInitializer %s %q %s %s", ss.name(location), name, ss.name(declaration), ss.symbol(result))
			return false
		},
		OnFailedToResolveSymbol: func(location store.Node, name string, meaning ast.SymbolFlags, nameNotFoundMessage *diagnostics.Message) {
			ss.record("OnFailedToResolveSymbol %s %q %d %d", ss.name(location), name, meaning, nameNotFoundMessage.Code())
		},
		OnSuccessfullyResolvedSymbol: func(location store.Node, result *store.Symbol, meaning ast.SymbolFlags, lastLocation store.Node, associated store.Node, withinDeferredContext bool) {
			ss.record("OnSuccessfullyResolvedSymbol %s %s %d %s %s %v", ss.name(location), ss.symbol(result), meaning, ss.name(lastLocation), ss.name(associated), withinDeferredContext)
		},
	}
	return rp
}

// resolveStats counts what the comparison did.
type resolveStats struct {
	files, calls, found int
	foundBy             map[ast.SymbolFlags]int
	throughJSDoc        map[ast.Kind]int // identifiers whose chain reaches a function-like through a node of the kind
	callbacks           map[string]int
	jsdocLast           map[ast.Kind]int // Pointer calls whose lastLocation was a JSDoc node of these kinds
	mismatches          storetest.Mismatches
	example             map[string]string
}

func newResolveStats() *resolveStats {
	return &resolveStats{throughJSDoc: map[ast.Kind]int{}, foundBy: map[ast.SymbolFlags]int{}, callbacks: map[string]int{}, jsdocLast: map[ast.Kind]int{}, mismatches: storetest.Mismatches{}, example: map[string]string{}}
}

func (st *resolveStats) mismatch(label, where string) {
	if st.mismatches[label] == 0 {
		st.example[label] = where
	}
	st.mismatches[label]++
}

var resolveMeanings = []ast.SymbolFlags{ast.SymbolFlagsValue, ast.SymbolFlagsType, ast.SymbolFlagsNamespace}

// resolveOne resolves name at the pair's node on both sides in every
// configuration and compares.
func (st *resolveStats) resolveOne(rp *resolverPair, p *ast.Node, n store.Node, name string, excludeGlobals bool, where string) {
	for _, isUse := range []bool{false, true} {
		for _, message := range []*diagnostics.Message{nil, diagnostics.Cannot_find_name_0} {
			for _, meaning := range resolveMeanings {
				rp.p.log, rp.s.log = rp.p.log[:0], rp.s.log[:0]
				pr := rp.pointer.Resolve(p, name, meaning, message, isUse, excludeGlobals)
				sr := rp.store.Resolve(n, name, meaning, message, isUse, excludeGlobals)
				st.calls++
				if pr != nil {
					st.found++
					st.foundBy[meaning]++
				}
				if pid, sid := rp.p.symbol(pr), rp.s.symbol(sr); pid != sid {
					st.mismatch(fmt.Sprintf("result %s, want %s", sidKind(sid), sidKind(pid)), where+" "+name)
				} else if strings.HasPrefix(pid, "s?") {
					st.mismatch("result unpaired", where+" "+name)
				}
				if len(rp.p.log) != len(rp.s.log) {
					st.mismatch("callbacks: count", where+" "+name)
				} else {
					for i := range rp.p.log {
						if rp.p.log[i] != rp.s.log[i] {
							st.mismatch("callbacks: "+strings.Fields(rp.p.log[i])[0], where+" "+name+": "+rp.s.log[i]+", want "+rp.p.log[i])
							break
						}
					}
				}
				for _, entry := range rp.p.log {
					st.callbacks[strings.Fields(entry)[0]]++
				}
			}
		}
	}
}

func sidKind(id string) string {
	switch {
	case id == "nil":
		return "nil"
	case strings.HasPrefix(id, "s?"):
		return "an unpaired symbol"
	}
	return "a symbol"
}

// jsdocPairs pairs the JSDoc of each host of the file (the Pointer's parsed
// on the Pointer file, the Store's on the Store file; a TS file's lazy JSDoc
// both), in preorder, as storetest.EquivalentJSDoc does. A host whose JSDoc
// differs in shape (the number of comments or the length of a preorder) is
// left out and reported with false.
func jsdocPairs(pointer *ast.SourceFile, file *store.File, pairs []storetest.Pair) ([]storetest.Pair, bool) {
	var out []storetest.Pair
	same := true
	for _, host := range pairs {
		if host.P.Flags&ast.NodeFlagsHasJSDoc == 0 {
			continue
		}
		ps, ns := host.P.JSDoc(pointer), host.N.JSDoc(file)
		if len(ps) != len(ns) {
			same = false
			continue
		}
		var hostPairs []storetest.Pair
		for i := range ps {
			pw, nw := preorderPointerNodes(ps[i]), storetest.Preorder(ns[i])
			if len(pw) != len(nw) {
				same = false
				hostPairs = nil
				break
			}
			for j := range pw {
				hostPairs = append(hostPairs, storetest.Pair{P: pw[j], N: nw[j]})
			}
		}
		out = append(out, hostPairs...)
	}
	return out, same
}

func preorderPointerNodes(root *ast.Node) []*ast.Node {
	var nodes []*ast.Node
	var visit ast.Visitor
	visit = func(n *ast.Node) bool {
		nodes = append(nodes, n)
		n.ForEachChild(visit)
		return false
	}
	visit(root)
	return nodes
}

// resolveFile compares every identifier of the file, JSDoc included.
func (st *resolveStats) resolveFile(pointer *ast.SourceFile, file *store.File, where string) {
	pairs, _ := storetest.Pairs(pointer, file.Store)
	if pairs == nil {
		st.mismatch("parse mismatch", where)
		return
	}
	m := bindComparerFor(pointer, file, pairs)
	jsdoc, same := jsdocPairs(pointer, file, pairs)
	if !same {
		st.mismatch("jsdoc shape", where)
	}
	all := append(pairs, jsdoc...)
	rp := newResolverPair(m, all, ast.SymbolTable{}, store.SymbolTable{}, false)
	rp.p.lastLocations = st.jsdocLast
	st.files++
	for _, pair := range all {
		if pair.P.Kind != ast.KindIdentifier {
			continue
		}
		if pair.N.Kind() != ast.KindIdentifier {
			st.mismatch("pair kind", where)
			continue
		}
		for n := pair.P; n.Parent != nil; n = n.Parent {
			if ast.IsFunctionLike(n.Parent) {
				switch n.Kind {
				case ast.KindJSDoc, ast.KindJSDocParameterTag, ast.KindJSDocReturnTag:
					st.throughJSDoc[n.Kind]++
				}
			}
		}
		st.resolveOne(rp, pair.P, pair.N, pair.P.Text(), false, fmt.Sprintf("%s@%d", where, pair.P.Pos()))
	}
}

// bindBothIn is bindBoth with the Store file in a program of one file: the
// lazy JSDoc Stores take StoreIDs of their own, so the NodeKeys that name the
// nodes of the calls do not collide with the file's (outside a program every
// Store is StoreID 0).
func bindBothIn(opts ast.SourceFileParseOptions, text string, kind core.ScriptKind) (*ast.SourceFile, *store.File) {
	pointer := parser.ParseSourceFile(opts, text, kind)
	binder.BindSourceFile(pointer)
	file := storeparser.ParseSourceFileIn(store.NewIDSpace(1), 0, opts, text, kind)
	storebinder.BindSourceFile(file)
	return pointer, file
}

// bindComparerFor is BindEquivalent's comparer after it has paired the file's
// symbols: the pairing of the results.
func bindComparerFor(pointer *ast.SourceFile, file *store.File, pairs []storetest.Pair) *bindComparer {
	m := &bindComparer{
		Mismatches: storetest.Mismatches{},
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
	return m
}

func TestNameResolverCorpus(t *testing.T) {
	files := corpus(t)
	st := newResolveStats()
	start := time.Now()
	for i, f := range files {
		opts, text, kind := sourceInput(f.name, f.text)
		pointer, file := bindBothIn(opts, text, kind)
		st.resolveFile(pointer, file, fmt.Sprintf("%s #%d", f.name, i))
	}
	t.Logf("%d files in %v: %d Resolve calls on each side, %d found a symbol", st.files, time.Since(start).Round(time.Second), st.calls, st.found)
	for name, count := range st.callbacks {
		t.Logf("callback %s: %d", name, count)
	}
	for _, meaning := range resolveMeanings {
		t.Logf("found for meaning %d: %d", meaning, st.foundBy[meaning])
	}
	// nameresolver.go's JSDoc branches (KindJSDoc, KindJSDocParameterTag,
	// KindJSDocReturnTag as lastLocation at a function-like location) are
	// taken only from inside a JSDoc. The final lastLocation of the
	// successful resolutions, and the identifiers whose parent chain reaches a
	// function-like through one of these kinds, are counted.
	for _, kind := range []ast.Kind{ast.KindJSDoc, ast.KindJSDocParameterTag, ast.KindJSDocReturnTag} {
		t.Logf("successful resolutions with the final lastLocation %v: %d; identifiers that reach a function-like through %v: %d", kind, st.jsdocLast[kind], kind, st.throughJSDoc[kind])
	}
	for label, count := range st.mismatches {
		t.Errorf("%s: %d (first at %s)", label, count, st.example[label])
	}
}

// The fixtures of 6.2: each takes a path that the corpus with the empty
// Globals may not, and must show its callback in the Pointer sequence.
func TestNameResolverFixtures(t *testing.T) {
	type fixture struct {
		name     string
		source   string
		ident    string // the identifier to resolve: its first occurrence at or after after
		after    string
		globals  bool // a Globals table with the identifier's name
		exclude  bool // excludeGlobals
		alias    bool // Lookup also returns an alias
		callback string
		want     string // the Pointer result: "symbol", "nil"
	}
	fixtures := []fixture{
		{name: "globals", source: "g;", ident: "g", globals: true, callback: "Lookup", want: "symbol"},
		// The same name as "globals", not found once the globals are excluded.
		{name: "excludeGlobals", source: "g;", ident: "g", globals: true, exclude: true, callback: "OnFailedToResolveSymbol", want: "nil"},
		{name: "alias import", source: `import { x } from "m"; x;`, ident: "x", after: "; ", alias: true, callback: "OnSuccessfullyResolvedSymbol", want: "symbol"},
		{name: "alias export", source: `const y = 1; export { y as z }; z;`, ident: "y", after: "export", alias: true, callback: "Lookup", want: "symbol"},
		{name: "isUse", source: "function f() { let a = 1; a; }", ident: "a", after: "; ", callback: "SymbolReferenced", want: "symbol"},
		{name: "not found", source: "unknownName;", ident: "unknownName", callback: "OnFailedToResolveSymbol", want: "nil"},
		// A class type parameter from a static member, with a message.
		{name: "error", source: "class C<T> { static x: T; }", ident: "T", after: "static", callback: "Error", want: "nil"},
		{name: "invalid initializer", source: "class C { p = x; constructor(x: number) {} }", ident: "x", callback: "OnPropertyWithInvalidInitializer"},
		{name: "scope change cache", source: "function f(a = () => b) { var b = 1; }", ident: "b", callback: "SetRequiresScopeChangeCache"},
	}
	for _, fx := range fixtures {
		t.Run(fx.name, func(t *testing.T) {
			opts, text, kind := sourceInput("/fixture.ts", fx.source)
			pointer, file := bindBothIn(opts, text, kind)
			pairs, _ := storetest.Pairs(pointer, file.Store)
			if pairs == nil {
				t.Fatal("parse mismatch")
			}
			m := bindComparerFor(pointer, file, pairs)
			pGlobals, sGlobals := ast.SymbolTable{}, store.SymbolTable{}
			if fx.globals {
				pGlobals[fx.ident] = &ast.Symbol{Name: fx.ident, Flags: ast.SymbolFlagsVariable}
				sGlobals[fx.ident] = &store.Symbol{Name: fx.ident, Flags: ast.SymbolFlagsVariable}
			}
			rp := newResolverPair(m, pairs, pGlobals, sGlobals, fx.alias)
			from := strings.Index(fx.source, fx.after)
			var pair *storetest.Pair
			for i := range pairs {
				if pairs[i].P.Kind == ast.KindIdentifier && pairs[i].P.Text() == fx.ident && pairs[i].P.Pos() >= from {
					pair = &pairs[i]
					break
				}
			}
			if pair == nil {
				t.Fatalf("no identifier %s", fx.ident)
			}
			st := newResolveStats()
			st.resolveOne(rp, pair.P, pair.N, fx.ident, fx.exclude, fx.name)
			for label, count := range st.mismatches {
				t.Errorf("%s: %d (%s)", label, count, st.example[label])
			}
			if st.callbacks[fx.callback] == 0 {
				t.Errorf("the Pointer resolver called no %s: the fixture does not take its path (callbacks %v)", fx.callback, st.callbacks)
			}
			// The result of the first configuration (isUse false, no message,
			// Value).
			result := rp.pointer.Resolve(pair.P, fx.ident, ast.SymbolFlagsValue, nil, false, fx.exclude)
			got := "nil"
			if result != nil {
				got = "symbol"
			}
			if fx.want != "" && got != fx.want {
				t.Errorf("the Pointer result is %s, want %s", got, fx.want)
			}
			t.Logf("%s: %d calls, callbacks %v", fx.name, st.calls, st.callbacks)
		})
	}
}
