package storeclitest

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"os"
	"runtime"
	"runtime/debug"
	"slices"
	"testing"

	"github.com/microsoft/TypeScript/tsc/internal/ast"
	"github.com/microsoft/TypeScript/tsc/internal/ast/store"
	"github.com/microsoft/TypeScript/tsc/internal/core"
	"github.com/microsoft/TypeScript/tsc/internal/testutil/kperf"
)

// The replay of store-checker-perf-analysis-instructions-20261005.md 7: the
// nodes that an operation received during the C01 check (recorded by the
// count overlay of _store-checker-perf-20261005/s4/count as little-endian
// int32 file, pos, end, kind records; file -1 is a synthetic Store, -2 the
// nil Node), found again in a program of either backend after the same check,
// and given to the operation alone, in the same order.
//
// STORE_CLI_REPLAY_REC is the record prefix (<prefix>-<op>.bin),
// STORE_CLI_REPLAY_OP the operation (parent, isnil, kind, file, links; isnil
// and kind replay the nodes that Parent received, kind as the baseline loop). With
// STORE_CLI_REPLAY_MAP (an output path) the test writes which records the
// backend finds; with STORE_CLI_REPLAY_MASK (the records both backends find,
// one byte a record) the replay takes only those.

type replayRecord struct{ file, pos, end, kind int32 }

func readReplayRecords(tb testing.TB, path string) []replayRecord {
	b, err := os.ReadFile(path)
	if err != nil {
		tb.Fatal(err)
	}
	out := make([]replayRecord, len(b)/16)
	for i := range out {
		r := b[16*i:]
		out[i] = replayRecord{int32(binary.LittleEndian.Uint32(r)), int32(binary.LittleEndian.Uint32(r[4:])), int32(binary.LittleEndian.Uint32(r[8:])), int32(binary.LittleEndian.Uint32(r[12:]))}
	}
	return out
}

// replayNodes are the nodes of the records in one backend, nil where the
// backend has no node for a record (or, with a mask, where the mask is 0),
// and the hash of the file name, pos and kind of the nodes found.
type replayNodes[N any] struct {
	nodes  []N
	found  []bool
	hash   string
	dups   int
	fileNs []string
}

type replayKey [4]int32

func checkPointerProgram(ctx context.Context, p nativeProgram) {
	p.pointer.BindSourceFiles()
	var targets []*ast.SourceFile
	for _, f := range p.pointer.SourceFiles() {
		if !p.pointer.IsSourceFileDefaultLibrary(f.Path()) {
			targets = append(targets, f)
		}
	}
	xctraceCheckPointer(ctx, p.pointer, targets)
}

func checkStoreProgram(ctx context.Context, p nativeProgram) {
	p.store.BindSourceFiles()
	var targets []*store.File
	for _, f := range p.store.SourceFiles() {
		if !p.store.IsSourceFileDefaultLibrary(f.Path()) {
			targets = append(targets, f)
		}
	}
	xctraceCheckStore(ctx, p.store, targets)
}

func pointerReplayNodes(tb testing.TB, p nativeProgram, recs []replayRecord, mask []byte) replayNodes[*ast.Node] {
	var r replayNodes[*ast.Node]
	idx := map[replayKey]*ast.Node{}
	for i, f := range p.pointer.SourceFiles() {
		r.fileNs = append(r.fileNs, f.FileName())
		var visit func(n *ast.Node) bool
		visit = func(n *ast.Node) bool {
			k := replayKey{int32(i), int32(n.Pos()), int32(n.End()), int32(n.Kind)}
			if _, ok := idx[k]; ok {
				r.dups++
			} else {
				idx[k] = n
			}
			for _, j := range n.JSDoc(f) {
				visit(j)
			}
			n.ForEachChild(visit)
			return false
		}
		visit(f.AsNode())
	}
	r.resolve(recs, mask, func(k replayKey) (*ast.Node, bool) { n, ok := idx[k]; return n, ok }, func(n *ast.Node) (int32, int32) { return int32(n.Pos()), int32(n.Kind) })
	return r
}

func storeReplayNodes(tb testing.TB, p nativeProgram, recs []replayRecord, mask []byte) replayNodes[store.Node] {
	var r replayNodes[store.Node]
	idx := map[replayKey]store.Node{}
	for i, f := range p.store.SourceFiles() {
		r.fileNs = append(r.fileNs, f.FileName())
		var visit func(n store.Node) bool
		visit = func(n store.Node) bool {
			k := replayKey{int32(i), int32(n.Pos()), int32(n.End()), int32(n.Kind())}
			if _, ok := idx[k]; ok {
				r.dups++
			} else {
				idx[k] = n
			}
			for _, j := range n.JSDoc(f) {
				visit(j)
			}
			n.ForEachChild(visit)
			return false
		}
		visit(f.Root())
	}
	r.resolve(recs, mask, func(k replayKey) (store.Node, bool) { n, ok := idx[k]; return n, ok }, func(n store.Node) (int32, int32) { return int32(n.Pos()), int32(n.Kind()) })
	return r
}

func (r *replayNodes[N]) resolve(recs []replayRecord, mask []byte, find func(replayKey) (N, bool), posKind func(N) (int32, int32)) {
	h := sha256.New()
	r.found = make([]bool, len(recs))
	for i, rec := range recs {
		if rec.file < 0 || (mask != nil && mask[i] == 0) {
			continue
		}
		n, ok := find(replayKey{rec.file, rec.pos, rec.end, rec.kind})
		if !ok {
			continue
		}
		r.found[i] = true
		r.nodes = append(r.nodes, n)
		pos, kind := posKind(n)
		h.Write([]byte(r.fileNs[rec.file]))
		binary.Write(h, binary.LittleEndian, [2]int32{pos, kind})
	}
	r.hash = hex.EncodeToString(h.Sum(nil))
}

// replayOp is one replay: the setup outside the counters, and the operation
// over every node.
type replayOp struct {
	n    int
	hash string
	run  func() int
}

func prepareReplay(tb testing.TB, backend core.ASTBackend) replayOp {
	rec, op := os.Getenv("STORE_CLI_REPLAY_REC"), os.Getenv("STORE_CLI_REPLAY_OP")
	if rec == "" || op == "" {
		tb.Skip("STORE_CLI_REPLAY_REC and STORE_CLI_REPLAY_OP are not set")
	}
	file := op
	if op == "isnil" || op == "kind" {
		file = "parent" // IsNil over the nodes that Parent received
	}
	recs := readReplayRecords(tb, rec+"-"+file+".bin")
	var mask []byte
	if path := os.Getenv("STORE_CLI_REPLAY_MASK"); path != "" {
		var err error
		if mask, err = os.ReadFile(path); err != nil {
			tb.Fatal(err)
		}
		if len(mask) != len(recs) {
			tb.Fatalf("mask %d, records %d", len(mask), len(recs))
		}
	}
	ctx := context.Background()
	p := buildProgram(tb, backend, benchInputs(tb)[0].config)
	var found []bool
	var out replayOp
	if backend == core.ASTBackendStore {
		checkStoreProgram(ctx, p)
		r := storeReplayNodes(tb, p, recs, mask)
		found, out.n, out.hash = r.found, len(r.nodes), r.hash
		nodes := r.nodes
		switch op {
		case "parent":
			out.run = func() int { return replayStoreParent(nodes) }
		case "file":
			out.run = func() int { return replayStoreFile(nodes) }
		case "links":
			out.run = func() int { return replayStoreLinks(nodes) }
		case "isnil":
			out.run = func() int { return replayStoreIsNil(nodes) }
		case "kind":
			out.run = func() int { return replayStoreKind(nodes) }
		}
	} else {
		checkPointerProgram(ctx, p)
		r := pointerReplayNodes(tb, p, recs, mask)
		found, out.n, out.hash = r.found, len(r.nodes), r.hash
		nodes := r.nodes
		switch op {
		case "parent":
			out.run = func() int { return replayPointerParent(nodes) }
		case "file":
			out.run = func() int { return replayPointerFile(nodes) }
		case "links":
			out.run = func() int { return replayPointerLinks(nodes) }
		case "isnil":
			out.run = func() int { return replayPointerIsNil(nodes) }
		case "kind":
			out.run = func() int { return replayPointerKind(nodes) }
		}
	}
	if out.run == nil {
		tb.Fatalf("STORE_CLI_REPLAY_OP %q", op)
	}
	if path := os.Getenv("STORE_CLI_REPLAY_MAP"); path != "" {
		m := make([]byte, len(found))
		for i, ok := range found {
			if ok {
				m[i] = 1
			}
		}
		if err := os.WriteFile(path, m, 0o644); err != nil {
			tb.Fatal(err)
		}
	}
	runtime.KeepAlive(p)
	return out
}

//go:noinline
func replayStoreParent(nodes []store.Node) int {
	n := 0
	for _, node := range nodes {
		if p := node.Parent(); !p.IsNil() {
			n++
		}
	}
	return n
}

//go:noinline
func replayPointerParent(nodes []*ast.Node) int {
	n := 0
	for _, node := range nodes {
		if p := node.Parent; p != nil {
			n++
		}
	}
	return n
}

// replayStoreKind and replayPointerKind are the baseline: Kind is one load
// on both sides, so their difference is the loop over 2-word elements.
//
//go:noinline
func replayStoreKind(nodes []store.Node) int {
	n := 0
	for _, node := range nodes {
		n += int(node.Kind())
	}
	return n
}

//go:noinline
func replayPointerKind(nodes []*ast.Node) int {
	n := 0
	for _, node := range nodes {
		n += int(node.Kind)
	}
	return n
}

//go:noinline
func replayStoreIsNil(nodes []store.Node) int {
	n := 0
	for _, node := range nodes {
		if !node.IsNil() {
			n++
		}
	}
	return n
}

//go:noinline
func replayPointerIsNil(nodes []*ast.Node) int {
	n := 0
	for _, node := range nodes {
		if node != nil {
			n++
		}
	}
	return n
}

//go:noinline
func replayStoreFile(nodes []store.Node) int {
	n := 0
	for _, node := range nodes {
		if f := node.File(); f != nil {
			n++
		}
	}
	return n
}

//go:noinline
func replayPointerFile(nodes []*ast.Node) int {
	n := 0
	for _, node := range nodes {
		if f := ast.GetSourceFileOfNode(node); f != nil {
			n++
		}
	}
	return n
}

// replayStoreLinks and replayPointerLinks are symbolNodeLinks.Get over a new
// store: the copies below of storechecker's and checker's nodeLinkStore (the
// originals are not exported), on the same value type (one pointer).

type replaySymbolNodeLinks struct{ resolvedSymbol *store.Symbol }

const replayPageShift, replayPageSize, replayPageMask = 8, 1 << 8, 1<<8 - 1

type replayStoreLinkStore struct {
	stores [][]*[replayPageSize]replaySymbolNodeLinks
}

func (s *replayStoreLinkStore) Get(node store.Node) *replaySymbolNodeLinks {
	k := node.Key()
	if id := int(k.StoreID()); id < len(s.stores) {
		pages := s.stores[id]
		if i := int(k.ID() >> replayPageShift); i < len(pages) {
			if page := pages[i]; page != nil {
				return &page[k.ID()&replayPageMask]
			}
		}
	}
	return s.newPage(k)
}

//go:noinline
func (s *replayStoreLinkStore) newPage(k store.NodeKey) *replaySymbolNodeLinks {
	id := int(k.StoreID())
	if id >= len(s.stores) {
		s.stores = slices.Grow(s.stores, id-len(s.stores)+1)[:id+1]
	}
	pages := s.stores[id]
	i := int(k.ID() >> replayPageShift)
	if i >= len(pages) {
		pages = slices.Grow(pages, i-len(pages)+1)[:i+1]
		s.stores[id] = pages
	}
	page := pages[i]
	if page == nil {
		page = new([replayPageSize]replaySymbolNodeLinks)
		pages[i] = page
	}
	return &page[k.ID()&replayPageMask]
}

//go:noinline
func replayStoreLinks(nodes []store.Node) int {
	var s replayStoreLinkStore
	n := 0
	for _, node := range nodes {
		if s.Get(node).resolvedSymbol == nil {
			n++
		}
	}
	return n
}

type replayPointerLinkStore struct {
	store core.PagedLinkStore[replaySymbolNodeLinks]
}

func (s *replayPointerLinkStore) Get(node *ast.Node) *replaySymbolNodeLinks {
	return s.store.Get(uint64(ast.GetNodeId(node)))
}

//go:noinline
func replayPointerLinks(nodes []*ast.Node) int {
	var s replayPointerLinkStore
	n := 0
	for _, node := range nodes {
		if s.Get(node).resolvedSymbol == nil {
			n++
		}
	}
	return n
}

// TestReplayOp is the replay on the process's counters (proc_pid_rusage), the
// GC off and the thread locked: the iteration of stage 4 without root. Each of
// STORE_CLI_REPLAY_REPS (5) passes is counted apart; the first warms the pages.
func TestReplayOp(t *testing.T) {
	backend := core.ASTBackendPointer
	if os.Getenv("STORE_CLI_REPLAY_BACKEND") == "store" {
		backend = core.ASTBackendStore
	}
	op := prepareReplay(t, backend)
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	defer debug.SetGCPercent(debug.SetGCPercent(-1))
	runtime.GC()
	fmt.Printf("replay %s n %d hash %s\n", backendName(backend), op.n, op.hash)
	for i := range 5 {
		_, _, i0, c0 := procRusage()
		op.run()
		_, _, i1, c1 := procRusage()
		fmt.Printf("pass %d inst/op %.2f cycles/op %.2f\n", i, float64(i1-i0)/float64(op.n), float64(c1-c0)/float64(op.n))
	}
}

// BenchmarkReplayOpKPC is the replay on the KPC counters (-tags kperf, root):
// one pass per iteration, -benchtime=1x in a process of its own per sample.
func BenchmarkReplayOpKPC(b *testing.B) {
	for _, backend := range backends {
		b.Run("backend="+backendName(backend), func(b *testing.B) {
			if name := os.Getenv("STORE_CLI_REPLAY_BACKEND"); name != "" && name != backendName(backend) {
				b.Skip("other backend")
			}
			session := kperf.Start(b)
			defer session.Stop()
			b.StopTimer()
			op := prepareReplay(b, backend)
			b.StartTimer()
			for range b.N {
				session.Measure(func() { op.run() })
			}
			session.Report()
			b.ReportMetric(float64(op.n), "nodes")
			t := session.Totals()
			b.ReportMetric(float64(t.Instructions)/float64(op.n)/float64(b.N), "inst/node")
			b.ReportMetric(float64(t.Cycles)/float64(op.n)/float64(b.N), "cycles/node")
		})
	}
}
