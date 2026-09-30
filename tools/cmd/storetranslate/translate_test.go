package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/tools/go/packages"
)

// The tests of the rules (7d-0 instructions 5.1): a fixture package, made in
// the tsc module by an overlay, is translated; the output must hold the
// rewritten forms and type-check against package store, and the constructs
// without a rule must be reported rather than rewritten.

func testRoot(t *testing.T) string {
	t.Helper()
	root, err := filepath.Abs("../../..")
	if err != nil {
		t.Fatal(err)
	}
	return root
}

// fixtureWorlds caches the world of each fixture: a load and a reach take
// seconds.
var fixtureWorlds = map[string]*world{}

// translateFixture translates testdata/name as the package
// storetranslatefixture, in a world with the reach computed, so that the
// types of the translated packages (ast.Diagnostic) are the Store's.
func translateFixture(t *testing.T, name string) (string, []problem, *world) {
	t.Helper()
	root := testRoot(t)
	src, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(root, "tsc/internal/storetranslatefixture", strings.TrimSuffix(name, ".txt"))
	w, ok := fixtureWorlds[name]
	if !ok {
		w, err = loadWorldOverlay(root, nil, map[string][]byte{path: src}, "./internal/storetranslatefixture")
		if err != nil {
			t.Fatal(err)
		}
		if err := readManuals(w); err != nil {
			t.Fatal(err)
		}
		res, err := computeReach(w)
		if err != nil {
			t.Fatal(err)
		}
		w.u.res = res
		fixtureWorlds[name] = w
	}
	pkg := w.byPath[tscModule+"/internal/storetranslatefixture"]
	if len(pkg.Errors) > 0 {
		t.Fatalf("the fixture does not type-check: %v", pkg.Errors)
	}
	out := &target{name: "storetranslatefixture", path: tscModule + "/internal/storetranslatefixtureout", local: true, imports: map[string]string{}}
	text, problems, err := translateFile(w, pkg, pkg.Syntax[0], out)
	if err != nil {
		t.Fatalf("the output does not format: %v\n%s", err, text)
	}
	return string(text), problems, w
}

// typeChecks loads the output as a package of the tsc module.
func typeChecks(t *testing.T, out string) {
	t.Helper()
	root := testRoot(t)
	path := filepath.Join(root, "tsc/internal/storetranslatefixtureout/out.go")
	cfg := &packages.Config{Dir: filepath.Join(root, "tsc"), Mode: packages.LoadSyntax, Overlay: map[string][]byte{path: []byte(out)}}
	pkgs, err := packages.Load(cfg, "./internal/storetranslatefixtureout")
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range pkgs[0].Errors {
		t.Errorf("the output does not type-check: %v", e)
	}
}

func TestRules(t *testing.T) {
	out, problems, _ := translateFixture(t, "rules.go.txt")
	for _, pr := range problems {
		t.Errorf("unexpected problem: %s: %s", pr.pos, pr.msg)
	}
	for _, want := range []string{
		// FirstNonNil, MapNonNil and OrElse by the type argument that is
		// compared with its zero value; core for anything else.
		"return store.FirstNonNil(nodes, func(n store.Node) store.Node { return n.Name() })",
		"return core.FirstNonNil(nodes, func(n store.Node) *store.Symbol { return n.Symbol() })",
		"return store.MapNonNil(nodes, func(n store.Node) store.Node { return n.Name() })",
		"return store.MapNonNil(symbols, func(s *store.Symbol) store.Node { return s.ValueDeclaration })",
		"return core.MapNonNil(nodes, func(n store.Node) *store.Symbol { return n.Symbol() })",
		"return store.OrElse(node.Name(), node)",
		"return core.OrElse(local.ExportSymbol, local)",
		// SameMap, SameMapIndex, Deduplicate, AppendIfUnique on nodes.
		"return store.SameMap(nodes, func(n store.Node) store.Node { return n.Parent() })",
		"return store.SameMapIndex(nodes, func(n store.Node, i int) store.Node { return n.Parent() })",
		"return store.Deduplicate(nodes)",
		"return core.Deduplicate(kinds)",
		"return store.AppendIfUnique(declarations, node)",
		"return store.Contains(nodes, node) && store.Index(nodes, node) >= 0",
		"return slices.Contains(kinds, kind)",
		// The composite keys.
		"propNode       store.NodeKey",
		"location                store.NodeKey",
		"key := nonExistentPropertyKey{propNode.Key(), containingType, isUncheckedJS}",
		"keyed := nonExistentPropertyKey{propNode: store.NodeKey{}, containingType: containingType}",
		"keyed.propNode = propNode.Key()",
		"linkKey := accessibleChainCacheKey{useOnlyExternalAliasing, location.Key(), meaning}",
		"c.accessibleChainCache = make(map[accessibleChainCacheKey][]*store.Symbol)",
		"c.accessibleChainCache[accessibleChainCacheKey{meaning: meaning}] = nil",
		// A map keyed by a node.
		"counts := map[store.NodeKey]int{}",
		"counts[n.Key()]++",
		"delete(counts, n.Parent().Key())",
		// Accessors, nil, lists.
		"arguments := node.Arguments()",
		"for i := range arguments.Len() {",
		"arg := arguments.At(i)",
		"if !arg.IsNil() && arg.Kind() == ast.KindIdentifier && !store.SameNode(node.Parent(), arg) {",
		"if node.AsCallExpression().Arguments().Len() > 0 && node.AsCallExpression().Arguments().At(0).Pos() < node.End() {",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("the output lacks %q", want)
		}
	}
	if t.Failed() {
		t.Logf("output:\n%s", out)
	}
	// The output type-checks against package store.
	typeChecks(t, out)
}

func TestRulesUnsupported(t *testing.T) {
	_, problems, _ := translateFixture(t, "unsupported.go.txt")
	var got []string
	for _, pr := range problems {
		got = append(got, pr.msg)
	}
	all := strings.Join(got, "\n")
	for _, want := range []string{
		"core.FirstNonZero with a node type argument needs a comparable Node: no rule",
		"the node field node of the key type",
		"is not a key only (read as a node at",
		"a list passed where the Pointer takes []*Node (no List version of count)",
		"FunctionLikeData of a node is read alone, not as FunctionLikeData().F",
		"the keys of a map keyed by a node are read back",
		// R8: no receiver or parameter holds a NodeFactory, and two do.
		"a write to a node, and no receiver or parameter holds a NodeFactory",
		"a write to a node, and 2 receivers or parameters hold a NodeFactory",
	} {
		if !strings.Contains(all, want) {
			t.Errorf("no problem %q among:\n%s", want, all)
		}
	}
}

// The rules of 7d-2a (3.3), each with the form it gives.
func TestRules7d2(t *testing.T) {
	out, problems, _ := translateFixture(t, "rules7d2.go.txt")
	for _, pr := range problems {
		t.Errorf("unexpected problem: %s: %s", pr.pos, pr.msg)
	}
	for _, want := range []string{
		// R2
		"n := node.TypeArgumentList().Len() + node.ModifierNodes().Len()",
		"node.Parameters()",
		// R3
		"root := sf.Root()",
		"sf.Store.Node(sf.ExternalModuleIndicator).IsNil()",
		"for _, augmentationRef := range sf.ModuleAugmentations {",
		"augmentation := sf.Store.Node(augmentationRef)",
		"sf.Statements().Len()",
		// R4
		"store.SomeList(node.Arguments(), func(n store.Node) bool",
		"store.IndexList(node.Arguments(), arg) >= 0",
		// R4'
		"args = node.Arguments().Nodes()",
		"return args[:i]",
		"return node.AsCallExpression().Arguments().Nodes()",
		// R5
		"nodeLinks core.LinkStore[store.NodeKey, int]",
		"seen      store.NodeSet",
		"*l.nodeLinks.Get(node.Key()) = 1",
		// R6
		"store.Compact(nodes)",
		"store.Coalesce(a, b)",
		// R7
		"factory *store.Factory",
		"return c.factory.Clone(node)",
		// R8
		"c.factory.SetParent(node, parent)",
		"c.factory.SetRange(node, int32(-1), int32(-1))",
		"c.factory.AddFlags(node, ast.NodeFlagsSynthesized)",
		"c.factory.SetListRange(list, int32(loc.Pos()), int32(loc.End()))",
		"c.factory.SetListElement(list, i, node)",
		"c.factory.SetIsTypeOf(store.Node(node.AsImportTypeNode()), true)",
		// R11: the parse diagnostics of a file are *ast.Diagnostic.
		"core.Some(sf.Diagnostics(), func(d *ast.Diagnostic) bool",
		// NodeId
		"return uint64(node.Key().Uint64())",
		// the switches
		"case store.SameNode(node, a):",
		"case store.Node:",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("the output lacks %q", want)
		}
	}
	if t.Failed() {
		t.Logf("output:\n%s", out)
	}
	typeChecks(t, out)
}

// The reach (3.2): an implementation of an interface that reached code names
// is reached, whether or not a call names its method; a method that no
// reached code names is not.
func TestReach(t *testing.T) {
	_, _, w := translateFixture(t, "rules7d2.go.txt")
	res := w.u.res
	for _, key := range []string{
		// nodebuilder.SymbolTracker, implemented by SymbolTrackerImpl.
		"checker.SymbolTrackerImpl.ReportInaccessibleThisError",
		// printer.EmitResolver and binder.ReferenceResolver, by EmitResolver.
		"checker.EmitResolver.GetReferencedExportContainer",
	} {
		if !res.output[internal+key] {
			t.Errorf("%s is not reached", key)
		}
		if from := res.from[internal+key]; !strings.HasPrefix(from, "interface ") {
			t.Errorf("%s is reached from %q, not by an interface", key, from)
		}
	}
	for _, key := range []string{
		// services.go: the language service's, not reached from the roots.
		"checker.Checker.GetSymbolsInScope",
		"checker.Checker.GetExportsOfModule",
	} {
		if res.output[internal+key] {
			t.Errorf("%s is reached (from %s)", key, res.from[internal+key])
		}
	}
}

// A P2 declaration whose recorded hash is not the Pointer declaration's
// stops the translation.
func TestManualHash(t *testing.T) {
	root := testRoot(t)
	w, err := loadWorld(root, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := readManuals(w); err != nil {
		t.Fatalf("the committed P2 files do not check: %v", err)
	}
	store := w.outs[astPath]
	manual, err := os.ReadFile(filepath.Join(store.dir, "utilities_manual.go"))
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "utilities_manual.go")
	if err := os.WriteFile(path, []byte(hashRe16(string(manual))), 0o644); err != nil {
		t.Fatal(err)
	}
	errs, err := readManualFile(w, store, path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(strings.Join(errs, "\n"), "changed (hash") {
		t.Fatalf("a wrong hash did not stop the translation: %v", errs)
	}
}

// hashRe16 replaces the first recorded hash with sixteen zeros.
func hashRe16(s string) string {
	const marker = "storetranslate:pointer-hash "
	i := strings.Index(s, marker) + len(marker)
	return s[:i] + "0000000000000000" + s[i+16:]
}
