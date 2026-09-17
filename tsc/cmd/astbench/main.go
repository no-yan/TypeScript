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
