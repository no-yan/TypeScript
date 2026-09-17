package workload

import (
	"testing"

	"github.com/microsoft/TypeScript/tsc/internal/ast"
	"github.com/microsoft/TypeScript/tsc/internal/core"
)

func TestTraceMatchesMeasuredVisitorForEverySupportedCase(t *testing.T) {
	for _, tc := range []Config{
		{Case: CaseFullTree, Shape: ShapeMixed, Nodes: 64, Seed: 1, LayoutSeed: 1, Layout: LayoutConstruction, Representation: RepresentationStore},
		{Case: CaseExpression, Shape: ShapeMixed, Nodes: 64, Seed: 1, LayoutSeed: 1, Layout: LayoutConstruction, Representation: RepresentationStore},
		{Case: CaseFullTree, Shape: ShapeMixed, Nodes: 64, Seed: 1, LayoutSeed: 1, Layout: LayoutConstruction, Representation: RepresentationPointer},
		{Case: CaseExpression, Shape: ShapeMixed, Nodes: 64, Seed: 1, LayoutSeed: 1, Layout: LayoutConstruction, Representation: RepresentationPointer},
		{Case: CaseFullTree, Shape: ShapeFixture, Layout: LayoutConstruction, Representation: RepresentationStore},
		{Case: CaseExpression, Shape: ShapeFixture, Layout: LayoutConstruction, Representation: RepresentationStore},
	} {
		trace, result, err := trace(tc)
		if err != nil {
			t.Fatal(err)
		}
		sample, err := Measure(tc, 2)
		if err != nil {
			t.Fatal(err)
		}
		if uint64(len(trace)) != result.Visits || sample.Visits != result.Visits || sample.Checksum != result.Checksum || sample.EdgeReads != result.EdgeReads || sample.AttributeReads != result.AttributeReads {
			t.Fatalf("%+v: trace=%d result=%+v sample=%+v", tc, len(trace), result, sample)
		}
	}
}

func TestTraceKeepsExactIdentifierText(t *testing.T) {
	c := DefaultConfig()
	c.Shape = ShapeFixture
	c.Nodes = 0
	entries, err := Trace(c)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]bool{"alpha": false, "beta": false, "invoke": false, "gamma": false, "delta": false}
	for _, entry := range entries {
		if _, ok := want[entry.Text]; ok {
			want[entry.Text] = true
		}
	}
	for text, seen := range want {
		if !seen {
			t.Fatalf("missing exact fixture text %q", text)
		}
	}
}

func TestExpressionWalkCallVisitsCalleeAndArgumentsOnce(t *testing.T) {
	ptrFactory := ast.NewNodeFactory(ast.NodeFactoryHooks{})
	callee := ptrFactory.NewIdentifier("f")
	arg1, arg2 := ptrFactory.NewIdentifier("a"), ptrFactory.NewIdentifier("b")
	ptrCall := ptrFactory.NewCallExpression(callee, nil, nil, ptrFactory.NewNodeList([]*ast.Node{arg1, arg2}), 0)
	ptrResult := (&walker{expr: true}).run(&tree{pointer: ptrCall, count: 4})
	if ptrResult.Visits != 4 || ptrResult.EdgeReads != 3 {
		t.Fatalf("pointer call traversal duplicated child: %+v", ptrResult)
	}

	storeFactory := ast.NewFactory(ast.FactoryHooks{})
	storeCallee := storeFactory.NewIdentifier("f")
	storeArgs := []ast.Handle{storeFactory.NewIdentifier("a"), storeFactory.NewIdentifier("b")}
	storeCall := storeFactory.NewCallExpression(storeCallee, ast.Handle{}, 0, storeFactory.List(core.UndefinedTextRange(), storeArgs...), 0)
	storeResult := (&walker{expr: true}).run(&tree{store: storeCall, owner: storeFactory.Store(), count: 4})
	if storeResult.Visits != 4 || storeResult.EdgeReads != 3 {
		t.Fatalf("store call traversal duplicated child: %+v", storeResult)
	}
}

func TestExpressionTraceExcludesBinaryOperator(t *testing.T) {
	c := DefaultConfig()
	c.Case = CaseExpression
	c.Shape = ShapeMixed
	c.Nodes = 64
	entries, err := Trace(c)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if entry.Kind == ast.KindPlusToken {
			t.Fatalf("expression trace visited operator token: %+v", entry)
		}
	}
}

func TestLogicalCountMatchesReachableNodesAtCampaignSizes(t *testing.T) {
	for seed := int64(0); seed < 4; seed++ {
		for _, size := range []int{4, 8, 32, 256, 16384} {
			c := DefaultConfig()
			c.Seed = seed
			c.Nodes = size
			entries, result, err := trace(c)
			if err != nil {
				t.Fatal(err)
			}
			if len(entries) != size || result.LogicalNodes != uint64(size) {
				t.Fatalf("seed=%d size=%d reachable=%d logical=%d", seed, size, len(entries), result.LogicalNodes)
			}
		}
	}
}
