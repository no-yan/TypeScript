package ast_test

import (
	"errors"
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
	fixture := filepath.Join("tsc", "testdata", "fixtures", "compiler", "checker.ts")

	if _, err := os.Stat(tscBin); err != nil {
		// Cold hereby is a full noembed go build.
		build := exec.Command("npx", "hereby", "build")
		build.Dir = repoRoot
		if out, err := build.CombinedOutput(); err != nil {
			t.Fatalf("npx hereby build: %v\n%s", err, out)
		}
		if _, err := os.Stat(tscBin); err != nil {
			t.Fatalf("built/local/tsc missing after hereby build: %v", err)
		}
	}

	cases := []gogcCase{
		{name: "default"},
		{name: "GOGC=off", env: []string{"GOGC=off"}},
		{name: "GOGC=200", env: []string{"GOGC=200"}},
		{name: "GOMEMLIMIT=8GiB", env: []string{"GOMEMLIMIT=8GiB"}},
	}

	const rounds = 5
	for _, c := range cases {
		samples := make([]time.Duration, 0, rounds)
		lastExit := -1
		for range rounds {
			cmd := exec.Command(tscBin, "--noEmit", fixture)
			cmd.Dir = repoRoot
			cmd.Env = gogcChildEnv(c.env)
			cmd.Stdout = io.Discard
			cmd.Stderr = io.Discard
			start := time.Now()
			err := cmd.Run()
			elapsed := time.Since(start)
			if err != nil {
				var exitErr *exec.ExitError
				// Exit 2 is TS diagnostics on this fixture. The process finished, so wall time still counts.
				if !errors.As(err, &exitErr) || !exitErr.Exited() {
					t.Fatalf("%s: %v", c.name, err)
				}
			}
			if cmd.ProcessState == nil {
				t.Fatalf("%s: process did not start", c.name)
			}
			lastExit = cmd.ProcessState.ExitCode()
			samples = append(samples, elapsed)
		}
		t.Logf("%s median=%s exit=%d", c.name, medianDuration(samples), lastExit)
	}
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
