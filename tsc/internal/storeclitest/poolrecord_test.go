//go:build poolrecord

package storeclitest

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/microsoft/TypeScript/tsc/internal/compiler"
	"github.com/microsoft/TypeScript/tsc/internal/core"
	"github.com/microsoft/TypeScript/tsc/internal/execute"
	"github.com/microsoft/TypeScript/tsc/internal/storecompiler"
	"github.com/microsoft/TypeScript/tsc/internal/storeharness"
	"github.com/microsoft/TypeScript/tsc/internal/tspath"
)

// The file association of the two checker pools
// (store-cli-parallel-instructions-20261007.md 3): with the poolrecord
// overlay (testdata/poolrecord/gen.py), each pool records its association,
// the association's inputs and each checker's visits, in the run of the check
// mode itself.

// poolRecord is the JSON of compiler.PoolRecordJSON and
// storecompiler.PoolRecordJSON.
type poolRecord struct {
	Checkers                   int              `json:"checkers"`
	Files                      []poolRecordFile `json:"files"`
	PrioritizeSourceFiles      bool             `json:"prioritizeSourceFiles"`
	SourceFileWeightMultiplier int              `json:"sourceFileWeightMultiplier"`
	BalancePenaltyMultiplier   int              `json:"balancePenaltyMultiplier"`
	FileWeights                []int            `json:"fileWeights"`
	FileOrder                  []int            `json:"fileOrder"`
	Adjacency                  [][]int          `json:"adjacency"`
	Associations               []int            `json:"associations"`
	CreateNS                   [2]int64         `json:"createNS"`
	Groups                     []struct {
		StartNS    int64   `json:"startNS"`
		EndNS      int64   `json:"endNS"`
		Visits     []int   `json:"visits"`
		VisitEndNS []int64 `json:"visitEndNS"`
	} `json:"groups"`
}

type poolRecordFile struct {
	FileName          string `json:"fileName"`
	NodeCount         int    `json:"nodeCount"`
	WeightNodeCount   int    `json:"weightNodeCount"`
	TextLen           int    `json:"textLen"`
	Imports           int    `json:"imports"`
	IsDeclarationFile bool   `json:"isDeclarationFile"`
}

// takePoolRecord runs check, which makes one program of backend, and returns
// its pool's record: nil when it made no checkers.
func takePoolRecord(backend core.ASTBackend, check func()) *poolRecord {
	reset, take := compiler.PoolRecordReset, compiler.PoolRecordJSON
	if backend == core.ASTBackendStore {
		reset, take = storecompiler.PoolRecordReset, storecompiler.PoolRecordJSON
	}
	reset()
	check()
	var r poolRecord
	if err := json.Unmarshal(take(), &r); err != nil {
		panic(err)
	}
	if r.Checkers == 0 {
		return nil
	}
	return &r
}

// poolComparison is what differs between the two records, by the parts of
// 3.3; NodeCount is the statistics' value and only counted.
type poolComparison struct {
	Checkers         [2]int   `json:"checkers"` // pointer, store: the counts after the clamp
	Files            int      `json:"files"`
	Differences      []string `json:"differences,omitempty"`
	NodeCountDiffer  int      `json:"nodeCountDiffer"` // files
	NodeCountPointer int      `json:"nodeCountPointer"`
	NodeCountStore   int      `json:"nodeCountStore"`
	MaxGroupEndNS    [2]int64 `json:"maxGroupEndNS"`
}

func comparePoolRecords(p, s *poolRecord) poolComparison {
	c := poolComparison{}
	differ := func(format string, args ...any) { c.Differences = append(c.Differences, fmt.Sprintf(format, args...)) }
	switch {
	case p == nil && s == nil:
		return c
	case p == nil || s == nil:
		differ("pool: pointer %v, store %v", p != nil, s != nil)
		return c
	}
	c.Checkers = [2]int{p.Checkers, s.Checkers}
	c.Files = len(p.Files)
	if p.Checkers != s.Checkers {
		differ("checkers: %d, %d", p.Checkers, s.Checkers)
	}
	// The program's file order, by path.
	names := func(r *poolRecord) []string {
		ns := make([]string, len(r.Files))
		for i, f := range r.Files {
			ns[i] = tspath.NormalizePath(f.FileName)
		}
		return ns
	}
	if pn, sn := names(p), names(s); !slices.Equal(pn, sn) {
		differ("file order: %s", firstDifference(pn, sn))
		return c
	}
	// The inputs of the weights.
	for i := range p.Files {
		pf, sf := p.Files[i], s.Files[i]
		if pf.WeightNodeCount != sf.WeightNodeCount || pf.TextLen != sf.TextLen || pf.Imports != sf.Imports || pf.IsDeclarationFile != sf.IsDeclarationFile {
			differ("inputs of %s: %+v, %+v", pf.FileName, pf, sf)
		}
		c.NodeCountPointer += pf.NodeCount
		c.NodeCountStore += sf.NodeCount
		if pf.NodeCount != sf.NodeCount {
			c.NodeCountDiffer++
		}
	}
	if p.PrioritizeSourceFiles != s.PrioritizeSourceFiles || p.SourceFileWeightMultiplier != s.SourceFileWeightMultiplier || p.BalancePenaltyMultiplier != s.BalancePenaltyMultiplier {
		differ("policy: %v %d %d, %v %d %d", p.PrioritizeSourceFiles, p.SourceFileWeightMultiplier, p.BalancePenaltyMultiplier, s.PrioritizeSourceFiles, s.SourceFileWeightMultiplier, s.BalancePenaltyMultiplier)
	}
	if !slices.Equal(p.FileWeights, s.FileWeights) {
		n := 0
		for i := range p.FileWeights {
			if i < len(s.FileWeights) && p.FileWeights[i] != s.FileWeights[i] {
				n++
			}
		}
		differ("weights: %d of %d files", n, len(p.FileWeights))
	}
	if !slices.Equal(p.FileOrder, s.FileOrder) {
		differ("association order")
	}
	// The import graph: each file's edges, with their multiplicity. The order
	// of a file's edges is the order of a map (Program.resolvedModules) in
	// both backends, and the association counts them only.
	if len(p.Adjacency) != len(s.Adjacency) {
		differ("graph: %d, %d files", len(p.Adjacency), len(s.Adjacency))
	} else {
		for i := range p.Adjacency {
			pa, sa := slices.Sorted(slices.Values(p.Adjacency[i])), slices.Sorted(slices.Values(s.Adjacency[i]))
			if !slices.Equal(pa, sa) {
				differ("edges of %s: %v, %v", p.Files[i].FileName, pa, sa)
			}
		}
	}
	if !slices.Equal(p.Associations, s.Associations) {
		n := 0
		for i := range p.Associations {
			if i < len(s.Associations) && p.Associations[i] != s.Associations[i] {
				n++
			}
		}
		differ("associations: %d of %d files", n, len(p.Associations))
	}
	// Each checker's files, in the order it visits them.
	for i := range min(len(p.Groups), len(s.Groups)) {
		if !slices.Equal(p.Groups[i].Visits, s.Groups[i].Visits) {
			differ("visits of checker %d: %d, %d files", i, len(p.Groups[i].Visits), len(s.Groups[i].Visits))
		}
		c.MaxGroupEndNS[0] = max(c.MaxGroupEndNS[0], p.Groups[i].EndNS)
		c.MaxGroupEndNS[1] = max(c.MaxGroupEndNS[1], s.Groups[i].EndNS)
	}
	return c
}

var poolCheckerCounts = []int{2, 4, 8}

// TestPoolAssignments: the CLI fixtures, the configs of
// STORE_CLI_POOLRECORD_CONFIGS (comma-separated tsconfig paths) and, with
// STORE_CLI_POOLRECORD_CORPUS=1, the configurations of
// TestNativeCheckerConfigurations (STORE_CLI_POOLRECORD_FILTER: a regexp of
// the ids), each with 2, 4 and 8 checkers. The runs are one at a time: the
// record is the last pool's.
// STORE_CLI_POOLRECORD_OUT: a directory for the records and the comparisons.
func TestPoolAssignments(t *testing.T) {
	out := os.Getenv("STORE_CLI_POOLRECORD_OUT")
	var summary []map[string]any
	note := func(name string, n int, p, s *poolRecord) {
		c := comparePoolRecords(p, s)
		for _, d := range c.Differences {
			t.Errorf("%s, %d checkers: %s", name, n, d)
		}
		summary = append(summary, map[string]any{"input": name, "checkersAsked": n, "comparison": c})
		if out != "" && !strings.HasPrefix(name, "corpus/") {
			b, _ := json.Marshal(map[string]any{"pointer": p, "store": s})
			file := regexp.MustCompile(`[^A-Za-z0-9.-]+`).ReplaceAllString(name, "_") + "-" + strconv.Itoa(n) + ".json"
			if err := os.WriteFile(filepath.Join(out, file), b, 0o644); err != nil {
				t.Fatal(err)
			}
		}
	}

	fixtures := append(append([]graphFixture{}, diagnosticFixtures...), graphFixtures...)
	for _, fx := range fixtures {
		for _, n := range poolCheckerCounts {
			args := []string{"-p", "/p/tsconfig.json", "--checkers", strconv.Itoa(n), "--astBackend"}
			p := takePoolRecord(core.ASTBackendPointer, func() { RunCLI(fx.files, fx.caseSensitive, "/p", append(args, "pointer")) })
			s := takePoolRecord(core.ASTBackendStore, func() { RunCLI(fx.files, fx.caseSensitive, "/p", append(args, "store")) })
			note("fixture/"+fx.name, n, p, s)
		}
	}

	if configs := os.Getenv("STORE_CLI_POOLRECORD_CONFIGS"); configs != "" {
		for _, config := range strings.Split(configs, ",") {
			cwd := tspath.GetDirectoryPath(tspath.NormalizePath(config))
			for _, n := range poolCheckerCounts {
				args := []string{"-p", config, "--checkers", strconv.Itoa(n), "--astBackend"}
				run := func(backend string) {
					result := execute.CommandLine(context.Background(), newOSSystem(cwd), append(args, backend), nil)
					if result.Status != 0 && result.Status != 2 {
						t.Fatalf("%s %s: status %d", config, backend, result.Status)
					}
				}
				p := takePoolRecord(core.ASTBackendPointer, func() { run("pointer") })
				s := takePoolRecord(core.ASTBackendStore, func() { run("store") })
				if p == nil || s == nil {
					t.Fatalf("%s: no pool", config)
				}
				note(config, n, p, s)
			}
		}
	}

	if os.Getenv("STORE_CLI_POOLRECORD_CORPUS") == "1" {
		cases, _, _, err := storeharness.EnumerateConfigurations()
		if err != nil {
			t.Fatal(err)
		}
		var filter *regexp.Regexp
		if f := os.Getenv("STORE_CLI_POOLRECORD_FILTER"); f != "" {
			filter = regexp.MustCompile(f)
		}
		pools, compared := 0, 0
		for _, c := range cases {
			if c.Skipped || filter != nil && !filter.MatchString(c.ID) {
				continue
			}
			d, outcome := storeharness.PrepareCase(c)
			if outcome.Kind != "" {
				continue
			}
			for _, n := range poolCheckerCounts {
				options := d.Options()
				options.NoEmit, options.SingleThreaded, options.ListFiles = core.TSTrue, core.TSFalse, core.TSTrue
				options.Checkers = &n
				if len(execute.CheckModeUnsupported(d.CommandLine(options.Clone()))) != 0 {
					break
				}
				caseSensitive, cwd := d.UseCaseSensitiveFileNames(), d.CurrentDirectory()
				p := takePoolRecord(core.ASTBackendPointer, func() {
					RunCheck(d.FileMap(), caseSensitive, cwd, d.CommandLine(options.Clone()), core.ASTBackendPointer)
				})
				s := takePoolRecord(core.ASTBackendStore, func() {
					RunCheck(d.FileMap(), caseSensitive, cwd, d.CommandLine(options.Clone()), core.ASTBackendStore)
				})
				compared++
				if p != nil {
					pools++
				}
				note("corpus/"+c.ID, n, p, s)
			}
		}
		t.Logf("corpus: %d runs compared, %d with a pool", compared, pools)
	}

	if out != "" {
		b, _ := json.MarshalIndent(summary, "", " ")
		if err := os.WriteFile(filepath.Join(out, "summary.json"), b, 0o644); err != nil {
			t.Fatal(err)
		}
	}
}
