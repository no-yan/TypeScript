package storeharness

import (
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/microsoft/TypeScript/tsc/internal/core"
	"github.com/microsoft/TypeScript/tsc/internal/repo"
	"github.com/microsoft/TypeScript/tsc/internal/testrunner"
	"github.com/microsoft/TypeScript/tsc/internal/testutil/harnessutil"
	"github.com/microsoft/TypeScript/tsc/internal/tspath"
	"github.com/microsoft/TypeScript/tsc/internal/vfs/osvfs"
)

// TestPointerWalkerBaselines holds the Pointer walker (pointerwalker.go, a
// copy of tsbaseline's) to the committed reference baselines
// (store-checker-7d2b2-implementation-instructions-20261001.md 7): every test
// of tests/cases/compiler with one unit and a reference .types and .symbols
// whose compiler options, set on testrunner's defaults as harnessutil sets
// them, make one configuration and no harness option. Each is put at
// /.src/<name> in a program of those options, the driver takes the
// diagnostics, and the walker's records, written as tsbaseline writes a
// result line, are the reference's > lines. Strict.
//
// The walker's hadErrorBaseline is whether the reference has a .errors.txt,
// here only: tsbaseline's is the program's diagnostics after its comment
// directives (testrunner's len(c.result.Diagnostics) > 0), and an error
// baseline is written when there is one (tsbaseline's error baseline has
// NoContent otherwise). The corpus keeps the driver's rule (H1).
func TestPointerWalkerBaselines(t *testing.T) {
	if testing.Short() {
		t.Skip("the baselines are not compared with -short")
	}
	dir := filepath.Join(repo.TestDataPath(), "tests", "cases", "compiler")
	referenceDir := filepath.Join(repo.TestDataPath(), "baselines", "reference", "compiler")
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Skipf("testdata/tests/cases/compiler is missing: %v", err)
	}
	type test struct {
		name, text string
		options    *core.CompilerOptions
	}
	var tests []test
	skippedOptions := 0
	for _, e := range entries {
		if e.IsDir() || !tsExtension.MatchString(e.Name()) {
			continue
		}
		// As testrunner reads a test (compiler_runner.go): osvfs decodes a BOM.
		text, ok := osvfs.FS().ReadFile(tspath.RootedFilePathFromAbsolute(filepath.Join(dir, e.Name())))
		if !ok {
			t.Fatalf("cannot read %s", e.Name())
		}
		units, symlinks, currentDir, options, err := testrunner.ParseTestFilesAndSymlinks(text, e.Name(),
			func(name, content string, fileOptions map[string]string) (corpusFile, error) {
				return corpusFile{name: name, text: content, origin: fmt.Sprint(len(fileOptions))}, nil
			})
		if err != nil {
			t.Fatal(err)
		}
		// harnessutil adds the test libs of /.lib to the program's files when a
		// unit names them (includeLibDir): a harness condition the driver's
		// program does not have.
		if len(units) != 1 || units[0].name != e.Name() || units[0].origin != "0" || len(symlinks) != 0 || currentDir != "" || strings.Contains(units[0].text, "/.lib/") {
			continue
		}
		base := tsExtension.ReplaceAllString(e.Name(), "")
		if !fileExists(filepath.Join(referenceDir, base+".types")) || !fileExists(filepath.Join(referenceDir, base+".symbols")) {
			continue
		}
		compilerOptions := testrunnerDefaultOptions()
		if len(options) != 0 && !setOptions(t, options, compilerOptions) {
			skippedOptions++
			continue
		}
		tests = append(tests, test{e.Name(), units[0].text, compilerOptions})
	}

	type outcome struct {
		mismatch string
	}
	outcomes := make([]outcome, len(tests))
	next := make(chan int)
	var wg sync.WaitGroup
	for range envInt(t, "STORE_CHECKER_CORPUS_WORKERS", 4) {
		wg.Go(func() {
			for i := range next {
				outcomes[i].mismatch = compareWithBaselines(tests[i].name, tests[i].text, tests[i].options, referenceDir)
			}
		})
	}
	for i := range tests {
		next <- i
	}
	close(next)
	wg.Wait()

	failed := 0
	for i, o := range outcomes {
		if o.mismatch != "" {
			failed++
			if failed <= 200 {
				t.Errorf("%s: %s", tests[i].name, o.mismatch)
			}
		}
	}
	t.Logf("%d tests, %d differ from their baselines", len(tests), failed)
	t.Logf("%d tests with options not taken (several configurations or a harness option)", skippedOptions)
}

// testrunnerDefaultOptions are harnessutil.CompileFiles's defaults
// (harnessutil.go): CRLF, the default libs not checked, no truncation.
func testrunnerDefaultOptions() *core.CompilerOptions {
	return &core.CompilerOptions{NewLine: core.NewLineKindCRLF, SkipDefaultLibCheck: core.TSTrue, NoErrorTruncation: core.TSTrue}
}

// setOptions sets a test's options on the defaults as
// harnessutil.CompileFiles does (the defaults first), when they make one
// configuration (no value lists several) and set no harness option.
func setOptions(t *testing.T, options map[string]string, compilerOptions *core.CompilerOptions) bool {
	for _, v := range options {
		if strings.ContainsAny(v, ",*") {
			return false
		}
	}
	harnessOptions := harnessutil.HarnessOptions{CaseSensitivity: tspath.CaseSensitive, CurrentDirectory: "/.src"}
	harnessutil.SetOptionsFromTestConfig(t, options, compilerOptions, &harnessOptions, "/.src", false /*allowUnknownOptions*/)
	return reflect.DeepEqual(harnessOptions, harnessutil.HarnessOptions{CaseSensitivity: tspath.CaseSensitive, CurrentDirectory: "/.src"})
}

// compareWithBaselines is "" when the walker's type and symbol lines are the
// reference's, or the first difference.
func compareWithBaselines(name, text string, options *core.CompilerOptions, referenceDir string) string {
	unit := "/.src/" + name
	files := map[string]string{unit: text}
	pointer := newPointerProgram(files, options)
	roots, missing := Roots(pointer, files)
	if len(missing) > 0 {
		return "not a source file of its program"
	}
	base := tsExtension.ReplaceAllString(name, "")
	hadErrorBaseline := fileExists(filepath.Join(referenceDir, base+".errors.txt"))
	r := runPointer(pointer, roots, roots, &hadErrorBaseline)
	if r.panicked != nil {
		return "the Pointer panics: " + r.panicked.message
	}
	codeLines := codeLinesRegexp.Split(text, -1)
	for _, walk := range []struct {
		extension string
		records   []walkRecord
	}{{".types", r.types}, {".symbols", r.symbols}} {
		reference, err := os.ReadFile(filepath.Join(referenceDir, base+walk.extension))
		if err != nil {
			return err.Error()
		}
		want := resultLines(string(reference), codeLines)
		var got []string
		for _, record := range walk.records {
			if record.present {
				// A value may hold a line break (a symbol name from the
				// source): its first line is the baseline's > line.
				line := testPathPrefixReplacer.Replace(">" + lineDelimiter.ReplaceAllString(record.text, "") + " : " + record.value)
				got = append(got, strings.TrimSuffix(strings.SplitN(line, "\n", 2)[0], "\r"))
			}
		}
		for i := range min(len(got), len(want)) {
			if got[i] != want[i] {
				return fmt.Sprintf("%s line %d of %d:\n  walker:    %s\n  reference: %s", walk.extension, i, len(want), got[i], want[i])
			}
		}
		if len(got) != len(want) {
			return fmt.Sprintf("%s: %d lines, the reference %d (next: walker %q, reference %q)", walk.extension, len(got), len(want), at(got, len(want)), at(want, len(got)))
		}
	}
	return ""
}

// resultLines are the > lines of a baseline, less the lines of the source
// that begin with > (tsbaseline writes the source lines as they are).
func resultLines(baseline string, codeLines []string) []string {
	var out []string
	for line := range strings.SplitSeq(baseline, "\n") {
		line = strings.TrimSuffix(line, "\r")
		if strings.HasPrefix(line, ">") && !slices.Contains(codeLines, line) {
			out = append(out, line)
		}
	}
	return out
}

func at(lines []string, i int) string {
	if i < len(lines) {
		return lines[i]
	}
	return ""
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

// tsbaseline's (type_symbol_baseline.go, util.go).
var (
	codeLinesRegexp        = regexp.MustCompile("[\r\u2028\u2029]|\r?\n")
	lineDelimiter          = regexp.MustCompile("\r?\n")
	tsExtension            = regexp.MustCompile(`\.tsx?$`)
	testPathPrefixReplacer = strings.NewReplacer(
		"/.ts/", "",
		"/.lib/", "",
		"/.src/", "",
		"bundled:///libs/", "",
		"file:///./ts/", "file:///",
		"file:///./lib/", "file:///",
		"file:///./src/", "file:///",
	)
)
