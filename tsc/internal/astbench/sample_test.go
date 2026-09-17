package astbench

import "testing"

func TestParseSampleConfigRequiresExactFields(t *testing.T) {
	if _, err := parseSampleConfig([]byte(`{"case":"full-tree","shape":"mixed","nodes":4,"batch":1}`)); err == nil {
		t.Fatal("accepted incomplete sample config")
	}
	if _, err := parseSampleConfig([]byte(`{"case":"full-tree","shape":"mixed","nodes":4,"seed":0,"layout_seed":0,"layout":"construction","representation":"store","batch":1} trailing`)); err == nil {
		t.Fatal("accepted trailing sample config")
	}
}

func TestZeroSeedIsPassedThrough(t *testing.T) {
	c, err := parseSampleConfig([]byte(`{"case":"full-tree","shape":"mixed","nodes":4,"seed":0,"layout_seed":0,"layout":"construction","representation":"store","batch":1}`))
	if err != nil {
		t.Fatal(err)
	}
	if got := c.workload().Seed; got != 0 {
		t.Fatalf("seed changed: %d", got)
	}
}
