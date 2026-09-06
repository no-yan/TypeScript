package ast

import (
	"testing"

	"github.com/microsoft/TypeScript/tsc/internal/core"
	"gotest.tools/v3/assert"
)

func TestFactoryListRefsMatchesList(t *testing.T) {
	t.Parallel()
	f := NewFactory(FactoryHooks{})
	a := f.NewIdentifier("a")
	b := f.NewIdentifier("b")
	loc := core.NewTextRange(3, 9)

	viaHandles := f.List(loc, a, Handle{}, b)
	viaRefs := f.ListRefs(loc, []NodeRef{a.Ref(), 0, b.Ref()})
	s := f.Store()

	assert.Equal(t, s.ListLen(viaRefs), 3)
	assert.Equal(t, s.ListLoc(viaRefs), loc)
	for i := range 3 {
		assert.Equal(t, s.ListRefAt(viaRefs, i), s.ListRefAt(viaHandles, i), "slot %d", i)
		assert.Equal(t, s.ListAt(viaRefs, i), s.ListAt(viaHandles, i), "slot %d", i)
	}
	assert.Equal(t, s.ListRefAt(viaRefs, 1), NodeRef(0))
	assert.Assert(t, s.ListAt(viaRefs, 1).IsNil())

	empty := f.ListRefs(loc, nil)
	assert.Equal(t, s.ListLen(empty), 0)
	assert.Equal(t, s.ListLoc(empty), loc)
	assert.Assert(t, empty != 0, "an empty list is still a list, not a missing list")
	assert.Equal(t, s.ListRefAt(0, 0), NodeRef(0), "missing list reads as absent")
}
