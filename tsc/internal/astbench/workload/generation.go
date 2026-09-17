package workload

import (
	"crypto/sha256"
	"fmt"
	"strconv"
	"unsafe"

	"github.com/microsoft/TypeScript/tsc/internal/ast"
)

const RepeatedSubtreeNodes = 7
const GeneratorVersion = "ast-child-edge-v2"

type Generation struct {
	Version             string         `json:"version"`
	SubtreeSpec         string         `json:"subtree_spec"`
	Subtrees            int            `json:"subtrees"`
	RequestedNodes      int            `json:"requested_nodes"`
	ActualNodes         int            `json:"actual_nodes"`
	MaxDepth            int            `json:"max_depth"`
	KindCounts          map[string]int `json:"kind_counts"`
	ConstructionOrder   string         `json:"construction_order"`
	VisitorContractHash string         `json:"visitor_contract_hash"`
}

type ByteEstimate struct {
	Bytes  *uint64 `json:"bytes"`
	Method string  `json:"method"`
	Reason string  `json:"reason,omitempty"`
}
type Memory struct {
	RepresentationUsedBytes     ByteEstimate `json:"representation_used_bytes"`
	RepresentationCapacityBytes ByteEstimate `json:"representation_capacity_bytes"`
	ReachableHeapBytesEstimate  ByteEstimate `json:"reachable_heap_bytes_estimate"`
	ProcessPeakRSS              ByteEstimate `json:"process_peak_rss"`
	AccessFootprintEstimate     ByteEstimate `json:"access_footprint_estimate"`
}

func resolveSubtrees(c Config) (int, error) {
	if c.Nodes < 0 || c.Nodes > 1000000 || c.Subtrees < 0 {
		return 0, fmt.Errorf("workload: repeated Nodes must be in [0,1000000] and Subtrees nonnegative")
	}
	n := c.Subtrees
	if n == 0 {
		if c.Nodes < 2 {
			return 0, fmt.Errorf("workload: repeated requires positive Subtrees or Nodes >= 2")
		}
		n = (c.Nodes - 1 + RepeatedSubtreeNodes - 1) / RepeatedSubtreeNodes
	}
	if n > (1000000-1)/RepeatedSubtreeNodes {
		return 0, fmt.Errorf("workload: repeated actual node count exceeds 1000000")
	}
	return n, nil
}
func logicalText(n *logical) string {
	if n.text != "" {
		return n.text
	}
	return strconv.FormatUint(uint64(n.id), 10)
}

// Describe runs outside the measurement interval. Depth counts the root as one.
func Describe(c Config) (Generation, error) {
	g := Generation{Version: GeneratorVersion, RequestedNodes: c.Nodes, KindCounts: map[string]int{}, ConstructionOrder: "factory child-before-parent, left-to-right; no address sorting"}
	if err := Validate(c); err != nil {
		return g, err
	}
	contract := "root-child-edges/v2;order=left-to-right;attributes=kind,flags,pos,end,text;case=" + c.Case + ";expression=binary-left-right-without-operator,paren-expression,array-elements,call-callee-arguments,property-expression-name;fallback=ForEachChild"
	g.VisitorContractHash = fmt.Sprintf("%x", sha256.Sum256([]byte(contract)))
	record := func(k ast.Kind, depth int) {
		g.ActualNodes++
		g.KindCounts[fmt.Sprint(k)]++
		if depth > g.MaxDepth {
			g.MaxDepth = depth
		}
	}
	if c.Shape == ShapeFixture {
		t, err := fixture()
		if err != nil {
			return g, err
		}
		defer release(t)
		var walk func(ast.Handle, int)
		walk = func(h ast.Handle, d int) {
			record(h.Kind, d)
			h.ForEachChild(func(k ast.Handle) bool { walk(k, d+1); return false })
		}
		walk(t.store, 1)
		g.ConstructionOrder = "parser factory construction order; no address sorting"
		return g, nil
	}
	root, _ := recipe(c)
	var visit func(*logical, int)
	visit = func(n *logical, d int) {
		record(n.kind, d)
		for _, k := range n.kids {
			visit(k, d+1)
		}
	}
	visit(root, 1)
	if c.Shape == ShapeRepeated {
		g.Subtrees = len(root.kids)
		g.SubtreeSpec = "v1: root array list of independent paren(binary(paren(numeric),plus,paren(numeric))); 7 nodes/subtree; root list length scales; SplitMix64 seed payload; prefix stable"
	}
	return g, nil
}

func describeMemory(c Config, t *tree) Memory {
	// Storage census is restricted to the repeated generator; unsupported
	// backing structures remain explicitly unavailable.
	unavailable := func(reason string) ByteEstimate { return ByteEstimate{Method: "unavailable", Reason: reason} }
	w := walker{expr: c.Case == CaseExpression, tracing: true}
	w.run(t)
	// A lower bound on explicitly read logical field storage, not cache lines.
	// It omits dispatch, parent/list metadata, intern lookups, padding, and tables.
	edgeBytes := uint64(unsafe.Sizeof((*ast.Node)(nil)))
	locBytes := uint64(2 * unsafe.Sizeof(int(0)))
	if c.Representation == RepresentationStore {
		edgeBytes = uint64(unsafe.Sizeof(ast.NodeRef(0)))
		locBytes = 8
	}
	footprint := w.r.Visits*(uint64(unsafe.Sizeof(ast.Kind(0)))+uint64(unsafe.Sizeof(ast.NodeFlags(0)))+locBytes) + w.r.EdgeReads*edgeBytes
	for _, entry := range w.entries {
		if len(entry.Text) > 0 {
			footprint++
			if len(entry.Text) > 1 {
				footprint++
			}
		}
	}
	used, capacity := repeatedStorage(c, t)
	return Memory{

		RepresentationUsedBytes:     used,
		RepresentationCapacityBytes: capacity,
		ReachableHeapBytesEstimate:  unavailable("no retained-object graph census; allocator rounding and shared backing storage are not inferred"),
		ProcessPeakRSS:              peakRSS(),
		AccessFootprintEstimate:     ByteEstimate{Bytes: &footprint, Method: "lower bound: visited kind+flags+positions, traversed pointer/NodeRef edges, and first/last text bytes; excludes metadata, lookup tables, padding and cache-line effects; not residency"},
	}
}
