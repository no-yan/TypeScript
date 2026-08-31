package ast_test

import (
	"bytes"
	"flag"
	"fmt"
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

	workload := writeGOGCWorkload(t)
	cases := []gogcCase{
		{name: "default"},
		{name: "GOGC=off", env: []string{"GOGC=off"}},
		{name: "GOGC=200", env: []string{"GOGC=200"}},
		{name: "GOMEMLIMIT=8GiB", env: []string{"GOMEMLIMIT=8GiB"}},
	}

	samples := make([][]time.Duration, len(cases))
	for round := range gogcRounds {
		// Rotate the first case each round so cache warming and machine drift do
		// not consistently favor one GC setting.
		for offset := range len(cases) {
			caseIndex := (round + offset) % len(cases)
			c := cases[caseIndex]
			cmd := exec.Command(tscBin, "--noEmit", "--strict", workload)
			cmd.Dir = repoRoot
			cmd.Env = gogcChildEnv(c.env)
			cmd.Stdout = io.Discard
			var stderr bytes.Buffer
			cmd.Stderr = &stderr
			start := time.Now()
			err := cmd.Run()
			elapsed := time.Since(start)
			if err != nil {
				t.Fatalf("%s round %d: %v\n%s", c.name, round+1, err, stderr.Bytes())
			}
			samples[caseIndex] = append(samples[caseIndex], elapsed)
		}
	}
	for i, c := range cases {
		t.Logf("%s median=%s exit=0", c.name, medianDuration(samples[i]))
	}
}

func writeGOGCWorkload(t *testing.T) string {
	t.Helper()

	const declarations = 8_000
	var source strings.Builder
	source.Grow(declarations * 400)
	source.WriteString("type Box<T> = { value: T; next?: Box<T> };\n")
	for i := range declarations {
		fmt.Fprintf(&source, "interface Item%d extends Box<{ id: %d; name: string }> { tag: \"item%d\" }\n", i, i, i)
		fmt.Fprintf(&source, "declare const item%d: Item%d;\n", i, i)
		fmt.Fprintf(&source, "type Result%d = Item%d extends Box<infer U> ? Readonly<U> : never;\n", i, i)
		fmt.Fprintf(&source, "const result%d: Result%d = item%d.value;\n", i, i, i)
	}

	file := filepath.Join(t.TempDir(), "checker-workload.ts")
	if err := os.WriteFile(file, []byte(source.String()), 0o600); err != nil {
		t.Fatal(err)
	}
	return file
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
