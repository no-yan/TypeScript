package store

import (
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"unsafe"

	"github.com/microsoft/TypeScript/tsc/internal/ast"
)

// The representation of the nil node (Z1, internal/ast/docs/
// store-node-zero-design-20260926.md 3 and 7): the marker makes Node and the
// views not comparable, adds no size, and keeps s and h where the ABI passes
// them.

func TestNodeLayout(t *testing.T) {
	var n Node
	if unsafe.Sizeof(n) != 16 || unsafe.Offsetof(n.s) != 0 || unsafe.Offsetof(n.h) != 8 {
		t.Errorf("Node is %d bytes with s at %d and h at %d, want 16, 0, 8", unsafe.Sizeof(n), unsafe.Offsetof(n.s), unsafe.Offsetof(n.h))
	}
	var id Identifier
	if unsafe.Sizeof(id) != 16 {
		t.Errorf("a view is %d bytes, want 16", unsafe.Sizeof(id))
	}
}

// Every view converts to Node and back without a copy of anything but the
// two words.
func TestViewConversion(t *testing.T) {
	b := NewBuilder("x", nil, 0)
	b.NewIdentifier(0, 0, 1, "x")
	s := b.Finish()
	n := s.node(1)
	id := n.AsIdentifier()
	if back := Node(id); !SameNode(back, n) || back.s != n.s || back.h != n.h {
		t.Error("Node(view) is not the node")
	}
	if Identifier(n).Text() != "x" {
		t.Error("Identifier(node) does not read the node")
	}
}

// The comparisons that must not compile: ==, a map key, a struct holding a
// Node as a map key, and a comparable type argument. Each is built as its own
// package through an overlay and must fail with the compiler's
// comparability error.
func TestNodeNotComparable(t *testing.T) {
	if testing.Short() {
		t.Skip("builds packages")
	}
	dir, err := filepath.Abs(".")
	if err != nil {
		t.Fatal(err)
	}
	for name, body := range map[string]string{
		"equal":          "func f(a, b store.Node) bool { return a == b }",
		"view equal":     "func f(a, b store.Identifier) bool { return a == b }",
		"map key":        "var m map[store.Node]int",
		"struct key":     "type k struct{ n store.Node }\nvar m map[k]int",
		"array key":      "var m map[[2]store.Node]int",
		"comparable arg": "func g[T comparable](T) {}\nfunc f(n store.Node) { g(n) }",
		"slices.Index":   "func f(ns []store.Node, n store.Node) int { return slices.Index(ns, n) }",
	} {
		t.Run(name, func(t *testing.T) {
			pkgDir := filepath.Join(t.TempDir(), "x")
			virtual := filepath.Join(dir, "zcompilefail", "x.go")
			src := "package zcompilefail\n\nimport (\n\t\"slices\"\n\n\t\"github.com/microsoft/TypeScript/tsc/internal/ast/store\"\n)\n\nvar _ = slices.Index[[]int]\n\n" + body + "\n"
			overlay, err := json.Marshal(map[string]map[string]string{"Replace": {virtual: filepath.Join(pkgDir, "x.go")}})
			if err != nil {
				t.Fatal(err)
			}
			if err := os.MkdirAll(pkgDir, 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(pkgDir, "x.go"), []byte(src), 0o644); err != nil {
				t.Fatal(err)
			}
			overlayPath := filepath.Join(pkgDir, "overlay.json")
			if err := os.WriteFile(overlayPath, overlay, 0o644); err != nil {
				t.Fatal(err)
			}
			cmd := exec.Command("go", "build", "-overlay", overlayPath, "./zcompilefail")
			cmd.Dir = dir
			out, err := cmd.CombinedOutput()
			if err == nil {
				t.Fatalf("%s compiles", name)
			}
			text := string(out)
			if !strings.Contains(text, "cannot be compared") && !strings.Contains(text, "invalid map key") && !strings.Contains(text, "does not satisfy comparable") && !strings.Contains(text, "incomparable") {
				t.Fatalf("%s fails for another reason:\n%s", name, text)
			}
			t.Logf("%s", strings.TrimSpace(text))
		})
	}
}

// A real row of kind Unknown is refused: KindUnknown is the sentinel's.
func TestNoUnknownRows(t *testing.T) {
	space := NewIDSpace(0)
	f := NewFactory(NewNodeRegion(space), 2, FactoryHooks{})
	defer func() {
		if recover() == nil {
			t.Error("the Factory made a node of kind Unknown")
		}
	}()
	f.NewToken(ast.KindUnknown)
}
