package astbench

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

func gitOutput(repo string, args ...string) (string, error) {
	cmd := exec.Command("git", args...)
	cmd.Dir = repo
	var out bytes.Buffer
	cmd.Stdout = &out
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("git %s: %w: %s", strings.Join(args, " "), err, strings.TrimSpace(stderr.String()))
	}
	return cleanRef(out.String()), nil
}

func gitRaw(repo string, args ...string) (string, error) {
	cmd := exec.Command("git", args...)
	cmd.Dir = repo
	var out bytes.Buffer
	cmd.Stdout = &out
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("git %s: %w: %s", strings.Join(args, " "), err, strings.TrimSpace(stderr.String()))
	}
	return out.String(), nil
}

func gitRevision(repo, ref string) (string, error) {
	return gitOutput(repo, "rev-parse", "--verify", ref+"^{commit}")
}

func gitDirty(repo string) (bool, error) {
	out, err := gitOutput(repo, "status", "--porcelain=v1", "--untracked-files=all")
	return out != "", err
}

func gitPatchHash(repo string) (string, error) {
	cmd := exec.Command("git", "diff", "HEAD", "--binary", "--no-ext-diff")
	cmd.Dir = repo
	var out bytes.Buffer
	cmd.Stdout = &out
	if err := cmd.Run(); err != nil {
		return "", err
	}
	return hashReader(bytes.NewReader(out.Bytes()))
}

func gitPatch(repo string) ([]byte, error) {
	cmd := exec.Command("git", "diff", "HEAD", "--binary", "--no-ext-diff")
	cmd.Dir = repo
	return cmd.Output()
}

func gitUntracked(repo string) (string, error) {
	out, err := gitRaw(repo, "status", "--porcelain=v1", "--untracked-files=all")
	if err != nil {
		return "", err
	}
	var lines []string
	for _, line := range strings.Split(out, "\n") {
		if line == "" {
			continue
		}
		if strings.HasPrefix(line, "??") {
			lines = append(lines, strings.TrimSpace(line[2:]))
		}
	}
	return strings.Join(lines, "\n") + func() string {
		if len(lines) > 0 {
			return "\n"
		}
		return ""
	}(), nil
}

// copyWorktreeSnapshot copies tracked files plus explicit non-ignored
// untracked files. It avoids node_modules, caches, and arbitrary ignored data
// that a broad filesystem walk would accidentally preserve in an artifact.
func copyWorktreeSnapshot(repo, dst string) error {
	out, err := gitRaw(repo, "ls-files", "--cached", "--others", "--exclude-standard", "-z")
	if err != nil {
		return err
	}
	for _, rel := range strings.Split(out, "\x00") {
		if rel == "" {
			continue
		}
		if filepath.IsAbs(rel) || rel == "." || strings.HasPrefix(filepath.Clean(rel), ".."+string(filepath.Separator)) {
			return fmt.Errorf("invalid worktree path %q", rel)
		}
		src := filepath.Join(repo, rel)
		info, err := os.Lstat(src)
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				continue
			}
			return err
		}
		if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
			return fmt.Errorf("unsupported worktree file %q", rel)
		}
		target := filepath.Join(dst, rel)
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return err
		}
		in, err := os.Open(src)
		if err != nil {
			return err
		}
		outf, err := os.OpenFile(target, os.O_CREATE|os.O_EXCL|os.O_WRONLY, info.Mode().Perm())
		if err != nil {
			_ = in.Close()
			return err
		}
		_, copyErr := io.Copy(outf, in)
		closeErr := errors.Join(outf.Close(), in.Close())
		if copyErr != nil {
			return copyErr
		}
		if closeErr != nil {
			return closeErr
		}
	}
	return nil
}

// copyHarnessOverlay places the same harness/workload sources in both frozen
// source snapshots. It preserves the production AST implementation from each ref.
func copyHarnessOverlay(repo, dst string) error {
	paths := []string{"tsc/internal/astbench", "tsc/cmd/astbench", "tsc/internal/ast/testdata/traversal", "tools/scripts/tsc/astbench.sh"}
	for _, rel := range paths {
		src := filepath.Join(repo, rel)
		info, err := os.Lstat(src)
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				continue
			}
			return err
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("harness overlay symlink is unsupported: %s", rel)
		}
		if info.IsDir() {
			if err := copyTree(src, filepath.Join(dst, rel), nil); err != nil {
				return err
			}
			continue
		}
		if err := copyFile(src, filepath.Join(dst, rel), info.Mode().Perm()); err != nil {
			return err
		}
	}
	return nil
}

func copyFile(src, dst string, mode os.FileMode) error {
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, mode)
	if err != nil {
		return err
	}
	_, copyErr := io.Copy(out, in)
	closeErr := out.Close()
	if copyErr != nil {
		return copyErr
	}
	return closeErr
}

func snapshotRef(repo, ref, dst string) error {
	if err := os.MkdirAll(dst, 0o755); err != nil {
		return err
	}
	archive := filepath.Join(filepath.Dir(dst), ".snapshot.tar")
	defer os.Remove(archive)
	cmd := exec.Command("git", "archive", "--format=tar", "-o", archive, ref)
	cmd.Dir = repo
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("git archive %s: %w: %s", ref, err, strings.TrimSpace(string(out)))
	}
	f, err := os.Open(archive)
	if err != nil {
		return err
	}
	defer f.Close()
	return extractTar(f, dst)
}
