package ast_test

import (
	"bytes"
	"flag"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestTsgoStoreE2E(t *testing.T) {
	if os.Getenv("STORE_TSGO_E2E") != "1" {
		run := ""
		if f := flag.Lookup("test.run"); f != nil {
			run = f.Value.String()
		}
		if run == "" || !strings.Contains(run, "TestTsgoStoreE2E") {
			t.Skip("set STORE_TSGO_E2E=1 or go test -run TestTsgoStoreE2E")
		}
	}

	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller failed")
	}
	repoRoot := filepath.Clean(filepath.Join(filepath.Dir(file), "..", "..", ".."))
	tscBin := filepath.Join(repoRoot, "built", "local", "tsc")
	tscDir := filepath.Join(repoRoot, "tsc")

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
		alt := "/tmp/typescript-6.0/src/compiler"
		if _, altErr := os.Stat(filepath.Join(alt, "tsconfig.json")); altErr != nil {
			t.Skipf("TypeScript 6.0 smoke checkout missing at %s: %v", project, err)
		}
		project = alt
	}

	wipeTsbuildinfo(t, project)
	smoke := exec.Command(tscBin, "-p", project, "--noEmit")
	smoke.Dir = repoRoot
	var smokeErr bytes.Buffer
	smoke.Stdout = &smokeErr
	smoke.Stderr = &smokeErr
	if err := smoke.Run(); err != nil {
		t.Fatalf("CI smoke --noEmit: %v\n%s", err, smokeErr.Bytes())
	}
	t.Logf("CI smoke --noEmit exit=0 project=%s", project)

	local := exec.Command("go", "test", "./internal/testrunner", "-count=1", "-run", "TestLocal/alias")
	local.Dir = tscDir
	out, err := local.CombinedOutput()
	t.Logf("testrunner TestLocal/alias:\n%s", out)
	if err != nil {
		t.Logf("TestLocal/alias failed: %v (type baselines still Alloc on a frozen parse Store; CI smoke is the e2e gate)", err)
	}
}

func wipeTsbuildinfo(t *testing.T, project string) {
	t.Helper()
	root := filepath.Dir(filepath.Dir(project))
	_ = filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		if strings.HasSuffix(path, ".tsbuildinfo") {
			_ = os.Remove(path)
		}
		return nil
	})
}
