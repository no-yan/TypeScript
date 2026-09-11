package binder

import (
	"testing"

	"github.com/microsoft/TypeScript/tsc/internal/ast"
	"github.com/microsoft/TypeScript/tsc/internal/core"
)

// Foreign children are not produced by the parser; Handle getters still resolve them.
func TestNarrowableReferenceForeignChildren(t *testing.T) {
	owner := ast.NewStore(8)
	defer ast.UnregisterStore(owner)
	foreign := ast.NewStore(8)
	defer ast.UnregisterStore(foreign)
	loc := core.UndefinedTextRange()

	base := owner.Alloc(ast.KindIdentifier, 0, loc, 0)
	arg := foreign.Alloc(ast.KindStringLiteral, 0, loc, 0)
	elem := owner.AllocSlots(ast.KindElementAccessExpression, 0, loc, 3, 0)
	elem.SetChild(0, base)
	elem.SetChild(2, arg)
	if !isNarrowableReference(elem) {
		t.Fatal("element access with foreign string literal arg should be narrowable")
	}

	left := foreign.Alloc(ast.KindIdentifier, 0, loc, 0)
	op := owner.Alloc(ast.KindEqualsToken, 0, loc, 0)
	right := owner.Alloc(ast.KindNumericLiteral, 0, loc, 0)
	bin := owner.AllocSlots(ast.KindBinaryExpression, 0, loc, 4, 0)
	bin.SetChild(0, left)
	bin.SetChild(2, op)
	bin.SetChild(3, right)
	if !isNarrowableReference(bin) {
		t.Fatal("assignment with foreign left should be narrowable via Handle")
	}

	calleeExpr := foreign.Alloc(ast.KindIdentifier, 0, loc, 0)
	prop := owner.Alloc(ast.KindIdentifier, 0, loc, 0)
	propAccess := owner.AllocSlots(ast.KindPropertyAccessExpression, 0, loc, 3, 0)
	propAccess.SetChild(0, calleeExpr)
	propAccess.SetChild(2, prop)
	call := owner.AllocSlots(ast.KindCallExpression, 0, loc, 2, 2)
	call.SetChild(0, propAccess)
	if !hasNarrowableArgument(call) {
		t.Fatal("call whose property-access object is foreign should see narrowable callee object")
	}
}
