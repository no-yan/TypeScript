package workload

import (
	"reflect"
	"testing"
)

func TestRepeatedShapePrefixAndPayload(t *testing.T) {
	c := DefaultConfig()
	c.Shape = ShapeRepeated
	c.Nodes = 0
	c.Subtrees = 4
	a, err := Trace(c)
	if err != nil {
		t.Fatal(err)
	}
	g, err := Describe(c)
	if err != nil {
		t.Fatal(err)
	}
	if g.ActualNodes != 29 || g.MaxDepth != 5 || g.Subtrees != 4 || len(a) != 29 {
		t.Fatalf("bad description: %+v", g)
	}
	c.Subtrees = 8
	b, err := Trace(c)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(a[1:], b[1:len(a)]) {
		t.Fatal("larger tree does not preserve subtree prefix")
	}
	c.Subtrees = 4
	c.Seed++
	seeded, err := Trace(c)
	if err != nil {
		t.Fatal(err)
	}
	if reflect.DeepEqual(a, seeded) {
		t.Fatal("seed did not change payload")
	}
	for i := range a {
		if a[i].Kind != seeded[i].Kind || a[i].Children != seeded[i].Children {
			t.Fatal("seed changed shape")
		}
	}
	root, _ := recipe(c)
	seen := map[*logical]bool{}
	var visit func(*logical)
	visit = func(n *logical) {
		if seen[n] {
			t.Fatal("subtrees share nodes")
		}
		seen[n] = true
		for _, kid := range n.kids {
			visit(kid)
		}
	}
	visit(root)
	for _, rep := range []string{RepresentationPointer, RepresentationStore} {
		c.Representation = rep
		if err := Verify(c); err != nil {
			t.Fatal(err)
		}
		sample, err := Measure(c, 4)
		if err != nil {
			t.Fatal(err)
		}
		if sample.Allocations != 0 || sample.GCCycles != 0 || sample.Visits == 0 {
			t.Fatalf("bad timed contract: %+v", sample)
		}
		if sample.Generation.ActualNodes != len(a) || sample.Memory.AccessFootprintEstimate.Bytes == nil {
			t.Fatalf("missing metadata: %+v", sample)
		}
	}
}

func TestRepeatedResolutionAndMetadata(t *testing.T) {
	c := DefaultConfig()
	c.Shape = ShapeRepeated
	c.Nodes = 16
	g, err := Describe(c)
	if err != nil {
		t.Fatal(err)
	}
	if g.Subtrees != 3 || g.ActualNodes != 22 || g.RequestedNodes != 16 {
		t.Fatalf("rounding hidden: %+v", g)
	}
	total := 0
	for _, n := range g.KindCounts {
		total += n
	}
	if total != g.ActualNodes {
		t.Fatal("kind census mismatch")
	}
	c.Subtrees = 2
	explicit, err := Describe(c)
	if err != nil {
		t.Fatal(err)
	}
	if explicit.ActualNodes != 15 || explicit.RequestedNodes != 16 {
		t.Fatal("explicit subtree resolution")
	}
	c.Case = CaseExpression
	expr, err := Describe(c)
	if err != nil {
		t.Fatal(err)
	}
	if expr.VisitorContractHash == g.VisitorContractHash {
		t.Fatal("visitor contracts not distinct")
	}
	for _, bad := range []Config{
		{Case: CaseFullTree, Shape: ShapeRepeated, Nodes: 0, Layout: LayoutConstruction, Representation: RepresentationStore},
		{Case: CaseFullTree, Shape: ShapeRepeated, Subtrees: 1000000, Layout: LayoutConstruction, Representation: RepresentationStore},
	} {
		if Validate(bad) == nil {
			t.Fatalf("accepted invalid config: %+v", bad)
		}
	}
}

func TestRepeatedStorageCensus(t *testing.T) {
	c := DefaultConfig()
	c.Shape = ShapeRepeated
	c.Nodes = 0
	c.Subtrees = 4
	for _, rep := range []string{RepresentationStore, RepresentationPointer} {
		c.Representation = rep
		tree, err := build(c)
		if err != nil {
			t.Fatal(err)
		}
		used, capacity := repeatedStorage(c, tree)
		release(tree)
		if used.Bytes == nil || *used.Bytes == 0 {
			t.Fatalf("%s missing used census: %+v", rep, used)
		}
		if rep == RepresentationStore && (capacity.Bytes == nil || *capacity.Bytes < *used.Bytes) {
			t.Fatalf("invalid store capacity: %+v used=%+v", capacity, used)
		}
		if rep == RepresentationPointer && (capacity.Bytes != nil || capacity.Reason == "") {
			t.Fatal("pointer unknown capacity was guessed")
		}
	}
}

func TestTimerOverheadIsResolved(t *testing.T) {
	for range 5 {
		if n := timerOverhead(); n <= 0 {
			t.Fatalf("batched timer calibration unresolved: %v", n)
		}
	}
}
