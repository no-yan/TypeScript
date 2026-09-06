package ast

import (
	"testing"

	"github.com/microsoft/TypeScript/tsc/internal/core"
)

func TestFlowSlotRoundTrip(t *testing.T) {
	var chunk, off uint32
	for i := uint32(0); i < 5000; i++ {
		c, o := flowSlot(i)
		if i == 0 {
			chunk, off = 0, 0
		} else if o == 0 {
			if int(off+1) != flowChunkCap(chunk) || c != chunk+1 {
				t.Fatalf("i=%d: chunk %d ended at off %d (cap %d), next chunk %d", i, chunk, off, flowChunkCap(chunk), c)
			}
			chunk, off = c, 0
		} else if c != chunk || o != off+1 {
			t.Fatalf("i=%d: got (%d,%d) want (%d,%d)", i, c, o, chunk, off+1)
		} else {
			off = o
		}
	}
}

// Arena flows must keep their address across chunk growth, and the column
// must hand back the same *FlowNode it was given, whether the flow came from
// this Store's arena, another Store's arena, or a plain composite literal.
func TestFlowColumnIdentity(t *testing.T) {
	a, b := NewStore(16), NewStore(16)
	var refs []NodeRef
	for i := 0; i < 600; i++ {
		refs = append(refs, a.Alloc(KindIdentifier, NodeFlagsNone, core.NewTextRange(i, i+1), 0).Ref())
	}
	a.PrepareBindTables()
	b.PrepareBindTables()
	flows := make([]*FlowNode, len(refs))
	for i, ref := range refs {
		flows[i] = a.NewFlow(FlowFlagsStart)
		a.SetFlow(ref, flows[i])
	}
	for i, ref := range refs {
		if got := a.Flow(ref); got != flows[i] {
			t.Fatalf("flow %d: got %p want %p", i, got, flows[i])
		}
	}
	foreignRef := b.Alloc(KindIdentifier, NodeFlagsNone, core.NewTextRange(0, 1), 0).Ref()
	b.PrepareBindTables()
	fromA, literal := flows[300], &FlowNode{Flags: FlowFlagsTrueCondition}
	b.SetFlow(foreignRef, fromA)
	if got := b.Flow(foreignRef); got != fromA {
		t.Fatalf("cross-store flow: got %p want %p", got, fromA)
	}
	b.SetFlow(foreignRef, literal)
	if got := b.Flow(foreignRef); got != literal {
		t.Fatalf("literal flow: got %p want %p", got, literal)
	}
	b.SetFlow(foreignRef, nil)
	if got := b.Flow(foreignRef); got != nil {
		t.Fatalf("nil flow: got %p", got)
	}
	if a.FlowCount() != len(refs) || b.FlowCount() != 0 {
		t.Fatalf("FlowCount: a=%d b=%d", a.FlowCount(), b.FlowCount())
	}
}
