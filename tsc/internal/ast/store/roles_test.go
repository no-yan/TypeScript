package store

import (
	"testing"

	"github.com/microsoft/TypeScript/tsc/internal/ast"
)

// TestTypeRoleSlot checks that the init of store.go ran after the generated
// one: typeRoleSlot is typeSlot but on the two JSDoc tags.
func TestTypeRoleSlot(t *testing.T) {
	for k, want := range typeSlot {
		if ast.Kind(k) == ast.KindJSDocParameterTag || ast.Kind(k) == ast.KindJSDocPropertyTag {
			want = jsdocParameterOrPropertyTagTypeExpressionSlot
		}
		if typeRoleSlot[k] != want {
			t.Errorf("typeRoleSlot[%v] = %d, want %d", ast.Kind(k), typeRoleSlot[k], want)
		}
	}
}
