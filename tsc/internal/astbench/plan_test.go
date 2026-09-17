package astbench

import "testing"

func TestEnsureOrderBalancedAndDeterministic(t *testing.T) {
	p := defaultPlan("r", "/repo", "a", "b", "x", "y")
	if err := EnsureOrder(&p); err != nil {
		t.Fatal(err)
	}
	counts := map[string]map[string]int{}
	for i := 0; i < len(p.Order); i += 2 {
		a, b := p.Order[i], p.Order[i+1]
		if a.CellID != b.CellID || a.Pair != b.Pair || a.Seed != b.Seed {
			t.Fatal("split pair or different input")
		}
		if counts[a.CellID] == nil {
			counts[a.CellID] = map[string]int{}
		}
		counts[a.CellID][a.Label+"/"+b.Label]++
	}
	for _, c := range counts {
		if c["before/after"] != 2 || c["after/before"] != 2 || c["control-before/control-after"] != 4 {
			t.Fatalf("unbalanced %v", c)
		}
	}
	q := defaultPlan("r", "/repo", "a", "b", "x", "y")
	if err := EnsureOrder(&q); err != nil {
		t.Fatal(err)
	}
	for i := range p.Order {
		if p.Order[i] != q.Order[i] {
			t.Fatal("nondeterministic schedule")
		}
	}
	p.Order = p.Order[:len(p.Order)-1]
	if validateOrder(p) == nil {
		t.Fatal("missing arm accepted")
	}
}
func TestRejectsUnsupportedPlanConditions(t *testing.T) {
	for _, mutate := range []func(*Plan){func(p *Plan) { p.Cells[0].GC = "100" }, func(p *Plan) { p.Cells[0].P = 2 }, func(p *Plan) { p.Cells[0].Backend = "kpc" }, func(p *Plan) { p.Cells[0].Batch = 0 }, func(p *Plan) { p.Cells[0].ID = "../bad" }, func(p *Plan) { l := p.Lanes["daily"]; l.Directions = []string{"AB", "AB", "AA"}; p.Lanes["daily"] = l }} {
		p := defaultPlan("r", "/repo", "a", "b", "x", "y")
		mutate(&p)
		if validatePlan(p) == nil {
			t.Fatal("unsupported plan accepted")
		}
	}
}
