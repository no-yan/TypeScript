package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"gotest.tools/v3/assert"
	"gotest.tools/v3/golden"
)

func TestRewriteSampleFull(t *testing.T) {
	dir := copyFixture(t)
	code := run([]string{filepath.Join(dir, "internal", "sample")})
	assert.Equal(t, 0, code)
	got, err := os.ReadFile(filepath.Join(dir, "internal", "sample", "sample.go"))
	assert.NilError(t, err)
	golden.Assert(t, string(got), "sample.full.go")
	build := exec.Command("go", "test", "./internal/sample")
	build.Dir = dir
	out, err := build.CombinedOutput()
	assert.NilError(t, err, "rewritten sample must compile: %s", out)
}

func TestRewriteSampleTypesOnly(t *testing.T) {
	dir := copyFixture(t)
	code := run([]string{"-types-only", filepath.Join(dir, "internal", "sample")})
	assert.Equal(t, 0, code)
	got, err := os.ReadFile(filepath.Join(dir, "internal", "sample", "sample.go"))
	assert.NilError(t, err)
	golden.Assert(t, string(got), "sample.types.go")
	star := countStar(got, starNode) + countStar(got, starExpr)
	assert.Equal(t, 0, star)
}

func copyFixture(t *testing.T) string {
	t.Helper()
	src := filepath.Join("testdata", "rewrite")
	dst := t.TempDir()
	cmd := exec.Command("cp", "-R", src+"/.", dst)
	out, err := cmd.CombinedOutput()
	assert.NilError(t, err, "%s", out)
	return dst
}
