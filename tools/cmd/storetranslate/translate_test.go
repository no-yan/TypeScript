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

// translateFixture translates testdata/name as the package storetranslatefixture.
func translateFixture(t *testing.T, name string) (string, []problem, *program) {
	t.Helper()
	root := testRoot(t)
	src, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(root, "tsc/internal/storetranslatefixture", strings.TrimSuffix(name, ".txt"))
	p, err := loadWith(root, map[string][]byte{path: src}, "./internal/storetranslatefixture")
	if err != nil {
		t.Fatal(err)
	}
	pkg := p.extra[0]
	if len(pkg.Errors) > 0 {
		t.Fatalf("the fixture does not type-check: %v", pkg.Errors)
	}
	u := &universe{ast: p.ast.Types, store: p.store.Types}
	out := &target{name: "storetranslatefixture", funcs: map[string]bool{}, local: true, imports: map[string]string{}}
	text, problems, err := translateFile(p, u, pkg, pkg.Syntax[0], out)
	if err != nil {
		t.Fatalf("the output does not format: %v\n%s", err, text)
	}
	return string(text), problems, p
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
	root := testRoot(t)
	path := filepath.Join(root, "tsc/internal/storetranslatefixtureout/rules.go")
	cfg := &packages.Config{Dir: filepath.Join(root, "tsc"), Mode: packages.LoadSyntax, Overlay: map[string][]byte{path: []byte(out)}}
	pkgs, err := packages.Load(cfg, "./internal/storetranslatefixtureout")
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range pkgs[0].Errors {
		t.Errorf("the output does not type-check: %v", e)
	}
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
		"a list passed where the Pointer takes []*Node",
	} {
		if !strings.Contains(all, want) {
			t.Errorf("no problem %q among:\n%s", want, all)
		}
	}
}

// A P2 function whose recorded hash is not the Pointer function's stops the
// translation.
func TestManualHash(t *testing.T) {
	root := testRoot(t)
	p, err := load(root)
	if err != nil {
		t.Fatal(err)
	}
	manual, err := os.ReadFile(filepath.Join(root, storeDir, manualFile))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := readManual(p, filepath.Join(root, storeDir, manualFile)); err != nil {
		t.Fatalf("the committed P2 file does not check: %v", err)
	}
	broken := hashRe16(string(manual))
	path := filepath.Join(t.TempDir(), manualFile)
	if err := os.WriteFile(path, []byte(broken), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := readManual(p, path); err == nil || !strings.Contains(err.Error(), "the Pointer function changed") {
		t.Fatalf("a wrong hash did not stop the translation: %v", err)
	}
}

// hashRe16 replaces the first recorded hash with sixteen zeros.
func hashRe16(s string) string {
	const marker = "storetranslate:pointer-hash "
	i := strings.Index(s, marker) + len(marker)
	return s[:i] + "0000000000000000" + s[i+16:]
}
