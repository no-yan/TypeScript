package ast_test

import (
	"fmt"
	"os"
	"runtime"
	"sort"
	"testing"
	"time"
)

// TestE2ELayoutReport prints heap and forced-GC cost for checker.ts under
// the Factory AST vs a Flattened Store. Run with:
//
//	go test ./internal/ast -run TestE2ELayoutReport -v
//	GOGC=off go test ./internal/ast -run TestE2ELayoutReport -v
func TestE2ELayoutReport(t *testing.T) {
	sf := parseBenchFixture(t, "checker.ts")
	root := sf.ParseRoot()
	s := sf.ParseStore()
	n := countAstNodes(root)
	t.Logf("nodes=%d GOGC=%s", n, gogcEnv())

	runtime.GC()
	var ms runtime.MemStats
	runtime.ReadMemStats(&ms)
	t.Logf("factory live: HeapInuse=%s HeapObjects=%d", humanBytes(ms.HeapInuse), ms.HeapObjects)
	t.Logf("factory forced GC median=%s", timeForcedGCs(8, sf))

	sf = nil
	runtime.GC()
	runtime.ReadMemStats(&ms)
	t.Logf("store live:   HeapInuse=%s HeapObjects=%d Len=%d", humanBytes(ms.HeapInuse), ms.HeapObjects, s.Len())
	t.Logf("store forced GC median=%s", timeForcedGCs(8, s, root))
}

func gogcEnv() string {
	if v := os.Getenv("GOGC"); v != "" {
		return v
	}
	return "default"
}

func humanBytes(n uint64) string {
	const unit = 1024
	if n < unit {
		return fmt.Sprintf("%d B", n)
	}
	div, exp := uint64(unit), 0
	for v := n / unit; v >= unit; v /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %ciB", float64(n)/float64(div), "KMGTPE"[exp])
}

func timeForcedGCs(rounds int, keep ...any) time.Duration {
	samples := make([]time.Duration, 0, rounds)
	for range rounds {
		start := time.Now()
		runtime.GC()
		samples = append(samples, time.Since(start))
		for _, k := range keep {
			runtime.KeepAlive(k)
		}
	}
	sort.Slice(samples, func(i, j int) bool { return samples[i] < samples[j] })
	return samples[len(samples)/2]
}
