package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/microsoft/TypeScript/tsc/internal/astbench"
)

func main() { os.Exit(mainExit()) }

func mainExit() int {
	if len(os.Args) < 2 {
		usage()
		return 2
	}
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	var err error
	switch os.Args[1] {
	case "sweep-plan":
		err = sweepPlan(os.Args[2:])
	case "select-daily":
		err = selectDaily(os.Args[2:])
	case "inspect":
		err = inspect(ctx, os.Args[2:])
	case "prepare":
		err = prepare(ctx, os.Args[2:])
	case "run":
		err = run(ctx, os.Args[2:])
	case "collect":
		err = collect(os.Args[2:])
	case "report":
		err = report(os.Args[2:])
	case "sample":
		err = sample(os.Args[2:])
	case "verify":
		err = verify(os.Args[2:])
	default:
		usage()
		return 2
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	return 0
}

func usage() { fmt.Fprintln(os.Stderr, "usage: astbench inspect|prepare|run|collect|report|sample") }

func prepare(ctx context.Context, args []string) error {
	f := flag.NewFlagSet("prepare", flag.ContinueOnError)
	repo, artifacts, before, after, plan, runID := "", "", "", "", "", ""
	working := false
	f.StringVar(&repo, "repo", "", "repository root")
	f.StringVar(&artifacts, "out", "", "artifact root")
	f.StringVar(&artifacts, "artifacts", "", "artifact root")
	f.StringVar(&before, "before", "", "before git ref")
	f.StringVar(&after, "after", "", "after git ref")
	f.BoolVar(&working, "after-working-tree", false, "snapshot dirty working tree as after")
	f.StringVar(&plan, "plan", "", "plan JSON")
	f.StringVar(&runID, "run-id", "", "run id")
	if err := f.Parse(args); err != nil {
		return err
	}
	dir, err := astbench.Prepare(ctx, astbench.PrepareOptions{Repo: repo, Artifacts: artifacts, Before: before, After: after, AfterWorkingTree: working, PlanPath: plan, RunID: runID})
	if err != nil {
		return err
	}
	fmt.Println(dir)
	return nil
}

func run(ctx context.Context, args []string) error {
	f := flag.NewFlagSet("run", flag.ContinueOnError)
	dir, lane := "", ""
	f.StringVar(&dir, "run", "", "run directory")
	f.StringVar(&lane, "lane", "", "lane name")
	if err := f.Parse(args); err != nil {
		return err
	}
	return astbench.Run(ctx, astbench.RunOptions{RunDir: dir, Lane: lane})
}

func collect(args []string) error {
	f := flag.NewFlagSet("collect", flag.ContinueOnError)
	dir, out := "", ""
	f.StringVar(&dir, "run", "", "run directory")
	f.StringVar(&out, "out", "", "analysis output directory")
	if err := f.Parse(args); err != nil {
		return err
	}
	if out != "" {
		return astbench.WriteAnalysisTo(dir, out)
	}
	c, err := astbench.Collect(dir)
	if err != nil {
		return err
	}
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	return enc.Encode(c)
}

func report(args []string) error {
	f := flag.NewFlagSet("report", flag.ContinueOnError)
	dir := ""
	f.StringVar(&dir, "run", "", "run directory")
	if err := f.Parse(args); err != nil {
		return err
	}
	text, err := astbench.Report(dir)
	if err != nil {
		return err
	}
	fmt.Print(text)
	return nil
}

func sample(args []string) error {
	f := flag.NewFlagSet("sample", flag.ContinueOnError)
	config := ""
	f.StringVar(&config, "config", "", "inline JSON config or file")
	if err := f.Parse(args); err != nil {
		return err
	}
	if config == "" {
		return fmt.Errorf("sample requires --config")
	}
	var raw []byte
	var err error
	if strings.HasPrefix(strings.TrimSpace(config), "{") {
		raw = []byte(config)
	} else {
		raw, err = os.ReadFile(config)
		if err != nil {
			return err
		}
	}
	s, measureErr := astbench.SampleJSONBytes(raw)
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	if err := enc.Encode(s); err != nil {
		return err
	}
	return measureErr
}

func verify(args []string) error {
	f := flag.NewFlagSet("verify", flag.ContinueOnError)
	config := ""
	f.StringVar(&config, "config", "", "inline JSON config or file")
	if err := f.Parse(args); err != nil {
		return err
	}
	if config == "" {
		return fmt.Errorf("verify requires --config")
	}
	var raw []byte
	var err error
	if strings.HasPrefix(strings.TrimSpace(config), "{") {
		raw = []byte(config)
	} else {
		raw, err = os.ReadFile(config)
		if err != nil {
			return err
		}
	}
	v, err := astbench.VerifyJSONBytes(raw)
	encErr := json.NewEncoder(os.Stdout).Encode(v)
	if encErr != nil {
		return encErr
	}
	if err != nil {
		return err
	}
	if !v.Valid {
		return fmt.Errorf("trace verification failed: %s", v.Reason)
	}
	return nil
}

func inspect(ctx context.Context, args []string) error {
	_ = ctx
	f := flag.NewFlagSet("inspect", flag.ContinueOnError)
	repo, artifacts, bench := "", "", ".*"
	f.StringVar(&repo, "repo", "", "repository root")
	f.StringVar(&artifacts, "artifacts", "", "artifact root")
	f.StringVar(&bench, "bench", ".*", "benchmark regex")
	if err := f.Parse(args); err != nil {
		return err
	}
	result, err := astbench.Inspect(repo, artifacts, bench)
	if err != nil {
		return err
	}
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	return enc.Encode(result)
}

func writePlan(path string, p astbench.Plan) error {
	b, e := json.MarshalIndent(p, "", "  ")
	if e != nil {
		return e
	}
	if path == "" {
		fmt.Println(string(b))
		return nil
	}
	f, e := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0644)
	if e != nil {
		return e
	}
	_, e = f.Write(append(b, '\n'))
	ce := f.Close()
	if e != nil {
		return e
	}
	return ce
}
func sweepPlan(args []string) error {
	f := flag.NewFlagSet("sweep-plan", flag.ContinueOnError)
	o := astbench.SweepOptions{}
	out := ""
	f.StringVar(&out, "out", "", "new plan file (stdout if omitted)")
	f.StringVar(&o.Repo, "repo", "", "selected repository")
	f.StringVar(&o.RunID, "run-id", "", "sweep identifier")
	f.StringVar(&o.Visitor, "visitor", "expression", "expression or full-tree")
	f.IntVar(&o.StartSubtrees, "start-subtrees", 32, "first repeated subtree count")
	f.IntVar(&o.MaxCases, "max-cases", 10, "maximum points (2 to 10)")
	f.IntVar(&o.Batch, "batch", 256, "root traversals per process sample")
	f.Uint64Var(&o.MemoryBudgetBytes, "memory-budget-bytes", 256<<20, "conservative planning memory ceiling")
	f.DurationVar(&o.Timeout, "sample-timeout", time.Minute, "timeout per process")
	f.Int64Var(&o.Seed, "seed", 1, "shape and schedule seed")
	if e := f.Parse(args); e != nil {
		return e
	}
	p, e := astbench.NewSweep(o)
	if e != nil {
		return e
	}
	return writePlan(out, p)
}
func selectDaily(args []string) error {
	f := flag.NewFlagSet("select-daily", flag.ContinueOnError)
	run, small, large, reason, out := "", "", "", "", ""
	var l1 uint64
	version := 1
	f.StringVar(&run, "run", "", "completed sweep run")
	f.StringVar(&small, "small", "", "small cell ID")
	f.StringVar(&large, "large", "", "large cell ID")
	f.StringVar(&reason, "reason", "", "selection justification (capacity, time, contamination)")
	f.StringVar(&out, "out", "", "new plan file")
	f.Uint64Var(&l1, "l1d-target-bytes", 0, "explicit conservative L1D capacity from host evidence")
	f.IntVar(&version, "preset-version", 1, "increment after CPU/generator/visitor changes")
	if e := f.Parse(args); e != nil {
		return e
	}
	p, e := astbench.SelectDaily(run, small, large, reason, l1, version)
	if e != nil {
		return e
	}
	return writePlan(out, p)
}
