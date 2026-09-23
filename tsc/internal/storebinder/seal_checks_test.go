//go:build storechecks

package storebinder_test

import (
	"testing"

	"github.com/microsoft/TypeScript/tsc/internal/ast"
	"github.com/microsoft/TypeScript/tsc/internal/storebinder"
	"github.com/microsoft/TypeScript/tsc/internal/storeparser"
)

// After the bind, every write API of the Store panics (the binder sealed it).
func TestSealPanics(t *testing.T) {
	file := storeparser.ParseSourceFile(sourceInput("/a.ts", "function f() { let x = 1; return x }"))
	storebinder.BindSourceFile(file)
	root := file.Root()
	fn := root.AsSourceFile().Statements().At(0)
	for name, write := range map[string]func(){
		"AddFlags":      func() { root.AddFlags(ast.NodeFlagsAmbient) },
		"ClearFlags":    func() { root.ClearFlags(ast.NodeFlagsAmbient) },
		"SetFlow":       func() { root.SetFlow(1) },
		"SetSymbol":     func() { root.SetSymbol(1) },
		"SetLocalsSlot": func() { fn.SetLocalsSlot(1) },
		"typed setter":  func() { fn.AsFunctionDeclaration().SetEndFlowNode(1) },
	} {
		t.Run(name, func(t *testing.T) {
			defer func() {
				if recover() == nil {
					t.Errorf("%s after the bind did not panic", name)
				}
			}()
			write()
		})
	}
}
