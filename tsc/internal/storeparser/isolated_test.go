package storeparser

import (
	"fmt"
	"strings"
	"testing"

	"github.com/microsoft/TypeScript/tsc/internal/ast"
	"github.com/microsoft/TypeScript/tsc/internal/ast/store"
	"github.com/microsoft/TypeScript/tsc/internal/parser"
)

// ParseIsolatedEntityName against the Pointer's (the inputs of
// store-checker-7d2a-verification-instructions-20260929.md 3.3): the same tree
// of kinds and identifier texts, or nil for both. The positions are not
// compared: the Pointer's function keeps them and its checker wrapper's
// markAsSynthetic clears them, where the Store's clears them itself (R16).
func TestParseIsolatedEntityName(t *testing.T) {
	space := store.NewIDSpace(3)
	inputs := []string{"React.createElement", "h", "a.b.c", "a.", "1", "", " a ", "a . b", "this", "a.#b", "a/*c*/.b", "React.Fragment", "a b", "a.b.", "\\u0061", "await", "yield", "let"}
	for _, in := range inputs {
		want := describeIsolatedPointer(parser.ParseIsolatedEntityName(in))
		s := ParseIsolatedEntityName(space, in)
		var positions []string
		if got := describeIsolatedStore(s, &positions); got != want {
			t.Errorf("%q: got %s, want %s", in, got, want)
		}
		if s.IsNil() {
			continue
		}
		for _, pos := range positions {
			if pos != "-1,-1" {
				t.Errorf("%q: positions %v, want all -1,-1", in, positions)
				break
			}
		}
		if s.File() != nil {
			t.Errorf("%q: File() is not nil", in)
		}
		func() {
			defer func() {
				if recover() == nil {
					t.Errorf("%q: a write from another Factory was not refused", in)
				}
			}()
			store.NewFactory(store.NewNodeRegion(space), 16, store.FactoryHooks{}).SetRange(s, 0, 0)
		}()
	}
}

func describeIsolatedPointer(n *ast.Node) string {
	if n == nil {
		return "nil"
	}
	var b strings.Builder
	var walk ast.Visitor
	walk = func(n *ast.Node) bool {
		fmt.Fprintf(&b, "(%v", n.Kind)
		if n.Kind == ast.KindIdentifier || n.Kind == ast.KindPrivateIdentifier {
			fmt.Fprintf(&b, " %q", n.Text())
		}
		n.ForEachChild(walk)
		b.WriteString(")")
		return false
	}
	walk(n)
	return b.String()
}

func describeIsolatedStore(n store.Node, positions *[]string) string {
	if n.IsNil() {
		return "nil"
	}
	var b strings.Builder
	var walk store.Visitor
	walk = func(n store.Node) bool {
		fmt.Fprintf(&b, "(%v", n.Kind())
		if n.Kind() == ast.KindIdentifier || n.Kind() == ast.KindPrivateIdentifier {
			fmt.Fprintf(&b, " %q", n.Text())
		}
		*positions = append(*positions, fmt.Sprintf("%d,%d", n.Pos(), n.End()))
		n.ForEachChild(walk)
		b.WriteString(")")
		return false
	}
	walk(n)
	return b.String()
}
