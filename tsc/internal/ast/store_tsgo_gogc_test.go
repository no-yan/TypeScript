package ast_test

import (
	"bytes"
	"flag"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"testing"
	"time"
)

type gogcCase struct {
	name string
	env  []string
}

const gogcRounds = 5

func TestTsgoGOGCBaseline(t *testing.T) {
	if os.Getenv("STORE_TSGO_GOGC") != "1" {
		run := ""
		if f := flag.Lookup("test.run"); f != nil {
			run = f.Value.String()
		}
		if run == "" || !strings.Contains(run, "TestTsgoGOGCBaseline") {
			t.Skip("set STORE_TSGO_GOGC=1 or go test -run TestTsgoGOGCBaseline")
		}
	}

	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	repoRoot := filepath.Clean(filepath.Join(filepath.Dir(file), "..", "..", ".."))
	tscBin := filepath.Join(repoRoot, "built", "local", "tsc")

	// Always rebuild so the measurement cannot silently use a binary from another
	// checkout. Hereby produces the same noembed binary used by CI.
	build := exec.Command("npx", "hereby", "build")
	build.Dir = repoRoot
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("npx hereby build: %v\n%s", err, out)
	}
	if _, err := os.Stat(tscBin); err != nil {
		t.Fatalf("built/local/tsc missing after hereby build: %v", err)
	}

	project := os.Getenv("STORE_TSGO_PROJECT")
	if project == "" {
		project = filepath.Join(repoRoot, "smoke", "typescript-6.0", "src", "compiler")
	}
	if _, err := os.Stat(filepath.Join(project, "tsconfig.json")); err != nil {
		t.Skipf("TypeScript 6.0 smoke checkout missing at %s: %v", project, err)
	}

	cases := []gogcCase{
		{name: "default"},
		{name: "GOGC=off", env: []string{"GOGC=off"}},
		{name: "GOGC=200", env: []string{"GOGC=200"}},
		{name: "GOMEMLIMIT=8GiB", env: []string{"GOMEMLIMIT=8GiB"}},
	}

	_ = runTsgoGOGCCase(t, tscBin, repoRoot, project, cases[0], 0)

	samples := make([][]time.Duration, len(cases))
	for round := range gogcRounds {
		// Rotate the first case each round so cache warming and machine drift do
		// not consistently favor one GC setting.
		for offset := range len(cases) {
			caseIndex := (round + offset) % len(cases)
			c := cases[caseIndex]
			elapsed := runTsgoGOGCCase(t, tscBin, repoRoot, project, c, round+1)
			samples[caseIndex] = append(samples[caseIndex], elapsed)
		}
	}
	medians := make([]time.Duration, len(cases))
	for i, c := range cases {
		medians[i] = medianDuration(samples[i])
		t.Logf("%s median=%s exit=0", c.name, medians[i])
	}

	defaultOverOff := float64(medians[0]) / float64(medians[1])
	memLimitOverOff := float64(medians[3]) / float64(medians[1])
	t.Logf("default/off=%.3f memlimit/off=%.3f", defaultOverOff, memLimitOverOff)
	if defaultOverOff < 1.10 || memLimitOverOff <= 1.05 {
		t.Errorf(
			"FAIL-PERF: require default/off >= 1.10 and memlimit/off > 1.05; got %.3f and %.3f",
			defaultOverOff,
			memLimitOverOff,
		)
	}
}

func runTsgoGOGCCase(
	t *testing.T,
	tscBin string,
	repoRoot string,
	project string,
	c gogcCase,
	round int,
) time.Duration {
	t.Helper()
	cmd := exec.Command(tscBin, "-p", project, "--noEmit")
	cmd.Dir = repoRoot
	cmd.Env = gogcChildEnv(c.env)
	cmd.Stdout = io.Discard
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	start := time.Now()
	err := cmd.Run()
	elapsed := time.Since(start)
	if err != nil {
		t.Fatalf("%s round %d: %v\n%s", c.name, round, err, stderr.Bytes())
	}
	return elapsed
}

func gogcChildEnv(extra []string) []string {
	// Default must not inherit the test process GOGC or GOMEMLIMIT.
	out := make([]string, 0, len(os.Environ())+len(extra))
	for _, e := range os.Environ() {
		if strings.HasPrefix(e, "GOGC=") || strings.HasPrefix(e, "GOMEMLIMIT=") {
			continue
		}
		out = append(out, e)
	}
	return append(out, extra...)
}

func medianDuration(samples []time.Duration) time.Duration {
	sort.Slice(samples, func(i, j int) bool { return samples[i] < samples[j] })
	return samples[len(samples)/2]
}
