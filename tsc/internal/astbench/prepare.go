package astbench

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

type PrepareOptions struct {
	Repo             string
	Artifacts        string
	Before           string
	After            string
	AfterWorkingTree bool
	PlanPath         string
	RunID            string
}

func Prepare(ctx context.Context, opts PrepareOptions) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	if opts.Repo == "" || opts.Artifacts == "" {
		return "", errors.New("prepare requires --repo and --out/--artifacts")
	}
	repo, err := filepath.Abs(opts.Repo)
	if err != nil {
		return "", err
	}
	if opts.AfterWorkingTree && opts.After != "" {
		return "", errors.New("--after and --after-working-tree are mutually exclusive")
	}
	if opts.Before == "" {
		return "", errors.New("prepare requires --before")
	}
	if !opts.AfterWorkingTree && opts.After == "" {
		return "", errors.New("prepare requires --after or --after-working-tree")
	}
	if opts.AfterWorkingTree {
		// This is deliberately a required opt-in. A dirty tree must never be
		// silently treated as the named after ref.
		opts.After = "working-tree"
	}
	if opts.RunID == "" {
		opts.RunID = time.Now().UTC().Format("20060102T150405.000000000Z")
	}
	artifacts, err := filepath.Abs(opts.Artifacts)
	if err != nil {
		return "", err
	}
	if err := validateRunID(opts.RunID); err != nil {
		return "", err
	}
	// Resolve and validate the plan before creating snapshots or starting a
	// build. User supplied binaries are intentionally unsupported: both sides
	// must be built from the frozen source snapshots by this function.
	plan, err := loadPreparePlan(opts)
	if err != nil {
		return "", err
	}
	if len(plan.Binaries) != 0 {
		return "", errors.New("prepare does not accept supplied binaries")
	}
	if err := EnsureOrder(&plan); err != nil {
		return "", err
	}
	runDir := filepath.Join(artifacts, opts.RunID)
	if err := os.MkdirAll(artifacts, 0o755); err != nil {
		return "", err
	}
	if err := os.Mkdir(runDir, 0o755); err != nil {
		if errors.Is(err, os.ErrExist) {
			return "", fmt.Errorf("run directory already exists: %s", runDir)
		}
		return "", err
	}
	// Leave a newly created run directory recoverable if a cancellation arrives;
	// no existing run can be overwritten.
	beforeRev, err := gitRevision(repo, opts.Before)
	if err != nil {
		return "", err
	}
	dirty, err := gitDirty(repo)
	if err != nil {
		return "", err
	}
	// A dirty checkout is allowed when both refs are explicit; only an explicit
	// working-tree after snapshot includes those changes in the measured input.
	afterRev := ""
	if !opts.AfterWorkingTree {
		afterRev, err = gitRevision(repo, opts.After)
		if err != nil {
			return "", err
		}
	}
	patchHash, err := gitPatchHash(repo)
	if err != nil {
		return "", err
	}
	patch, err := gitPatch(repo)
	if err != nil {
		return "", err
	}
	untracked, err := gitUntracked(repo)
	if err != nil {
		return "", err
	}
	snapshots := filepath.Join(runDir, "snapshots")
	if err := os.MkdirAll(snapshots, 0o755); err != nil {
		return "", err
	}
	if err := snapshotRef(repo, opts.Before, filepath.Join(snapshots, "before")); err != nil {
		return "", err
	}
	if opts.AfterWorkingTree {
		if err := copyWorktreeSnapshot(repo, filepath.Join(snapshots, "after")); err != nil {
			return "", err
		}
	} else if err := snapshotRef(repo, opts.After, filepath.Join(snapshots, "after")); err != nil {
		return "", err
	}
	// Historical refs do not contain this harness yet. Overlay exactly the
	// harness/workload paths in both snapshots so both variants can be built
	// with the same runner. This never overlays tsc/internal/ast itself.
	if err := copyHarnessOverlay(repo, filepath.Join(snapshots, "before")); err != nil {
		return "", err
	}
	if err := copyHarnessOverlay(repo, filepath.Join(snapshots, "after")); err != nil {
		return "", err
	}
	beforeHash, err := hashTree(filepath.Join(snapshots, "before"))
	if err != nil {
		return "", err
	}
	afterHash, err := hashTree(filepath.Join(snapshots, "after"))
	if err != nil {
		return "", err
	}
	if opts.AfterWorkingTree {
		afterRev, err = gitRevision(repo, "HEAD")
		if err != nil {
			return "", err
		}
	}
	identity := Identity{
		SchemaVersion:      SchemaVersion,
		RepoRoot:           repo,
		BeforeRef:          opts.Before,
		AfterRef:           opts.After,
		BeforeRevision:     beforeRev,
		AfterRevision:      afterRev,
		TypescriptGoGitRev: afterRev,
		TSGolintGitRev:     nil,
		Dirty:              dirty,
		AfterWorkingTree:   opts.AfterWorkingTree,
		SourceHash:         afterHash,
		FixtureHash:        beforeHash,
		DirtyPatchHash:     patchHash,
		SourceSnapshot:     filepath.Join(snapshots, "after"),
		FixtureSnapshot:    filepath.Join(snapshots, "before"),
		Binary:             map[string]Binary{},
		CreatedAt:          time.Now().UTC(),
	}
	plan.RunID = opts.RunID
	plan.RepoRoot = repo
	plan.Identity = IdentityRef{BeforeRevision: beforeRev, AfterRevision: afterRev, SourceHash: afterHash, FixtureHash: beforeHash}
	buildDir := filepath.Join(runDir, "build")
	if err := os.MkdirAll(buildDir, 0o755); err != nil {
		return "", err
	}
	if err := buildSnapshot(ctx, filepath.Join(snapshots, "before"), "before", buildDir, &identity); err != nil {
		return "", err
	}
	if err := buildSnapshot(ctx, filepath.Join(snapshots, "after"), "after", buildDir, &identity); err != nil {
		return "", err
	}
	if err := os.WriteFile(filepath.Join(runDir, "dirty.patch"), patch, 0o644); err != nil {
		return "", err
	}
	if err := os.WriteFile(filepath.Join(runDir, "untracked.txt"), []byte(untracked), 0o644); err != nil {
		return "", err
	}
	if err := os.MkdirAll(filepath.Join(runDir, "attempts"), 0o755); err != nil {
		return "", err
	}
	if err := writeJSON(filepath.Join(runDir, "identity.json"), identity); err != nil {
		return "", err
	}
	if err := writeJSON(filepath.Join(runDir, "plan.json"), plan); err != nil {
		return "", err
	}
	return runDir, nil
}

func validateRunID(id string) error {
	if id == "" || id == "." || id == ".." || filepath.Base(id) != id || strings.ContainsAny(id, `/\\`) {
		return fmt.Errorf("invalid run id %q", id)
	}
	return nil
}

func loadPreparePlan(opts PrepareOptions) (Plan, error) {
	if opts.PlanPath == "" {
		return defaultPlan(opts.RunID, opts.Repo, "", "", "", ""), nil
	}
	var p Plan
	if err := readJSON(opts.PlanPath, &p); err != nil {
		return p, err
	}
	if err := validatePlan(p); err != nil {
		return p, err
	}
	return p, nil
}

func buildSnapshot(ctx context.Context, snapshot, label, buildDir string, identity *Identity) error {
	if _, err := os.Stat(filepath.Join(snapshot, "tsc", "go.mod")); err != nil {
		return fmt.Errorf("%s snapshot has no tsc module: %w", label, err)
	}
	path := filepath.Join(buildDir, "astbench-"+label)
	stdoutPath := filepath.Join(buildDir, label+"-build.stdout.txt")
	stderrPath := filepath.Join(buildDir, label+"-build.stderr.txt")
	cmd := exec.CommandContext(ctx, "go", "build", "-trimpath", "-pgo=off", "-o", path, "./cmd/astbench")
	cmd.Dir = filepath.Join(snapshot, "tsc")
	cmd.Env = append(os.Environ(), "GOWORK=off")
	stdout, err := os.Create(stdoutPath)
	if err != nil {
		return err
	}
	stderr, err := os.Create(stderrPath)
	if err != nil {
		_ = stdout.Close()
		return err
	}
	cmd.Stdout, cmd.Stderr = stdout, stderr
	runErr := cmd.Run()
	closeErr := errors.Join(stdout.Close(), stderr.Close())
	if runErr != nil {
		return fmt.Errorf("build %s: %w", label, runErr)
	}
	if closeErr != nil {
		return closeErr
	}
	h, err := hashFile(path)
	if err != nil {
		return err
	}
	identity.Binary[label] = Binary{Label: label, Path: path, Snapshot: path, SHA256: h, BuildHash: h}
	infoCmd := exec.CommandContext(ctx, "go", "version", "-m", path)
	infoCmd.Dir = cmd.Dir
	info, infoErr := infoCmd.CombinedOutput()
	if infoErr != nil {
		return fmt.Errorf("binary build info: %w", infoErr)
	}
	if err := os.WriteFile(filepath.Join(buildDir, label+"-build-info.txt"), info, 0644); err != nil {
		return err
	}
	versionCmd := exec.CommandContext(ctx, "go", "version")
	versionCmd.Dir = filepath.Join(snapshot, "tsc")
	version, versionErr := versionCmd.Output()
	if versionErr != nil {
		return fmt.Errorf("go version %s: %w", label, versionErr)
	}
	versionPath := filepath.Join(buildDir, "go-version.txt")
	if previous, readErr := os.ReadFile(versionPath); readErr == nil && string(previous) != string(version) {
		return fmt.Errorf("build toolchain mismatch: %s=%q %s=%q", label, strings.TrimSpace(string(version)), filepath.Base(versionPath), strings.TrimSpace(string(previous)))
	}
	if err := os.WriteFile(versionPath, version, 0o644); err != nil {
		return err
	}
	env := []string{"GOWORK=off"}
	for _, key := range []string{"GOFLAGS", "GOTOOLCHAIN", "GOOS", "GOARCH", "GOARM64", "CGO_ENABLED", "GODEBUG", "GOEXPERIMENT"} {
		if v, ok := os.LookupEnv(key); ok {
			env = append(env, key+"="+v)
		}
	}
	if err := writeJSON(filepath.Join(buildDir, label+"-build.json"), map[string]any{"argv": []string{"go", "build", "-trimpath", "-pgo=off", "-o", path, "./cmd/astbench"}, "env": env, "go_version": strings.TrimSpace(string(version)), "binary_sha256": h}); err != nil {
		return err
	}
	return nil
}
