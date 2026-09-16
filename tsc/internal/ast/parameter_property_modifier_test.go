package ast_test

import (
	"testing"

	"github.com/microsoft/TypeScript/tsc/internal/ast"
	"github.com/microsoft/TypeScript/tsc/internal/core"
	"github.com/microsoft/TypeScript/tsc/internal/parser"
)

func TestParameterPropertyModifierFlagTracksConstructionAndReplacement(t *testing.T) {
	t.Parallel()
	factory := ast.NewFactory(ast.FactoryHooks{})
	name := factory.NewIdentifier("value")
	parameter := factory.NewParameterDeclaration(0, ast.Handle{}, name, ast.Handle{}, ast.Handle{}, ast.Handle{})
	if parameter.Flags()&ast.NodeFlagsHasParameterPropertyModifier != 0 {
		t.Fatal("plain parameter unexpectedly has the parameter-property modifier flag")
	}

	modifiers := factory.NewModifierList([]ast.Handle{factory.NewModifier(ast.KindReadonlyKeyword)})
	updated := factory.UpdateParameterDeclaration(parameter, modifiers, ast.Handle{}, name, ast.Handle{}, ast.Handle{}, ast.Handle{})
	if updated.Flags()&ast.NodeFlagsHasParameterPropertyModifier == 0 {
		t.Fatal("updated parameter is missing the parameter-property modifier flag")
	}
	cloned := factory.DeepCloneNode(updated)
	if cloned.Flags()&ast.NodeFlagsHasParameterPropertyModifier == 0 {
		t.Fatal("deep-cloned parameter lost the parameter-property modifier flag")
	}

	updated.SetParameterDeclarationModifiers(0)
	if updated.Flags()&ast.NodeFlagsHasParameterPropertyModifier != 0 {
		t.Fatal("replacing modifiers did not clear the parameter-property modifier flag")
	}
}

func TestParsedParameterPropertyModifierFlag(t *testing.T) {
	t.Parallel()
	file := parser.ParseSourceFile(ast.SourceFileParseOptions{FileName: "/parameter.ts", Path: "/parameter.ts"}, `
class C {
  constructor(public value: number, readonly other: string, plain: boolean) {}
}
`, core.ScriptKindTS)
	var got []ast.NodeFlags
	ast.Walk(file.ParseRoot(), func(node ast.Handle) bool {
		if node.Kind == ast.KindParameter {
			got = append(got, node.Flags())
		}
		return false
	})
	if len(got) != 3 {
		t.Fatalf("got %d parameters, want 3", len(got))
	}
	for i := 0; i < 2; i++ {
		if got[i]&ast.NodeFlagsHasParameterPropertyModifier == 0 {
			t.Errorf("parameter %d is missing the modifier fact", i)
		}
	}
	if got[2]&ast.NodeFlagsHasParameterPropertyModifier != 0 {
		t.Error("plain parameter unexpectedly has the parameter-property modifier flag")
	}
}
