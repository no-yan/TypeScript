package storeclitest

import (
	"regexp"
	"strings"
	"testing"

	"github.com/microsoft/TypeScript/tsc/internal/execute/tsc"
)

// The diagnostics fixtures of store-cli-design-20261004.md 8.2 ("欠落と診断"):
// each a tsconfig.json at /p with noEmit, and the files.
var diagnosticFixtures = []graphFixture{
	{name: "syntax", files: map[string]any{
		"/p/tsconfig.json": `{"compilerOptions": {"noEmit": true, "strict": true, "types": []}, "files": ["a.ts", "b.ts"]}`,
		"/p/a.ts":          "export const x = (;\n",
		"/p/b.ts":          "let q: number = \"s\";\n",
	}},
	{name: "global", files: map[string]any{
		"/p/tsconfig.json": `{"compilerOptions": {"noEmit": true, "noLib": true, "types": []}, "files": ["a.ts"]}`,
		"/p/a.ts":          "export const x = [1];\n",
	}},
	{name: "semantic-related", files: map[string]any{
		"/p/tsconfig.json": `{"compilerOptions": {"noEmit": true, "strict": true, "types": [], "lib": ["es2020"]}, "files": ["a.ts", "b.ts"]}`,
		"/p/a.ts":          "import { y } from \"./b\";\nexport const x: string = y;\nlet d = 1;\nlet d = 2;\nclass C { m(): number { return \"s\"; } }\nexport function f(a: { p: string }) {}\nf({ p: 1 });\n",
		"/p/b.ts":          "export const y = 1;\nlet z: number = \"s\";\n",
	}},
	{name: "directives", files: map[string]any{
		"/p/tsconfig.json": `{"compilerOptions": {"noEmit": true, "strict": true, "types": [], "lib": ["es2020"]}, "files": ["a.ts"]}`,
		"/p/a.ts":          "// @ts-ignore\nconst a: number = \"s\";\n// @ts-expect-error\nconst b: number = \"s\";\n// @ts-expect-error\nconst c: number = 1;\n\n// a comment\n// @ts-ignore\n\nconst d: number = \"s\";\nexport {};\n",
	}},
	{name: "js-checkjs-jsdoc", files: map[string]any{
		"/p/tsconfig.json": `{"compilerOptions": {"noEmit": true, "allowJs": true, "checkJs": true, "types": [], "lib": ["es2020"]}, "files": ["a.js", "b.js", "c.js"]}`,
		"/p/a.js":          "/** @type {number} */\nconst a = \"s\";\n/** @param {strin} x */\nfunction f(x) {}\n",
		"/p/b.js":          "// @ts-nocheck\nconst b = undefined.x;\n",
		"/p/c.js":          "/** @typedef {{ a: number }} T */\n/** @type {T} */\nconst t = { a: \"s\" };\nclass K { m(@dec p) {} }\n",
	}},
	{name: "plain-js", files: map[string]any{
		"/p/tsconfig.json": `{"compilerOptions": {"noEmit": true, "allowJs": true, "types": [], "lib": ["es2020"]}, "files": ["a.js", "b.ts"]}`,
		"/p/a.js":          "const a = 1;\nconst a = 2;\nlet n = undefined.x;\nclass K { m(@dec p) {} }\n",
		"/p/b.ts":          "import \"./a\";\nexport const b: number = \"s\";\n",
	}},
	{name: "skiplibcheck", files: map[string]any{
		"/p/tsconfig.json": `{"compilerOptions": {"noEmit": true, "skipLibCheck": true, "types": [], "lib": ["es2020"]}, "files": ["a.ts", "d.d.ts"]}`,
		"/p/a.ts":          "export const a: number = \"s\";\n",
		"/p/d.d.ts":        "declare const d: number = \"s\";\n",
	}},
	{name: "skipdefaultlibcheck", files: map[string]any{
		"/p/tsconfig.json": `{"compilerOptions": {"noEmit": true, "skipDefaultLibCheck": true, "types": [], "lib": ["es2020"]}, "files": ["a.ts", "d.d.ts"]}`,
		"/p/a.ts":          "export const a: number = \"s\";\n",
		"/p/d.d.ts":        "declare const d: number = \"s\";\n",
	}},
	{name: "config-error", files: map[string]any{
		"/p/tsconfig.json": `{"compilerOptions": {"noEmit": true, "notAnOption": true, "types": [], "lib": ["es2020"]}, "files": ["a.ts"]}`,
		"/p/a.ts":          "export const a: number = \"s\";\n",
	}},
	{name: "options-diagnostic", files: map[string]any{
		"/p/tsconfig.json": `{"compilerOptions": {"noEmit": true, "module": "nodenext", "moduleResolution": "bundler", "types": [], "lib": ["es2020"]}, "files": ["a.ts"]}`,
		"/p/a.ts":          "export const a = 1;\n",
	}},
}

// memoryRows are the --diagnostics rows whose values depend on the process,
// not the program: they are compared by name only.
var memoryRows = regexp.MustCompile(`(?m)^(Memory used|Memory allocs|.*(?:メモリ使用量|メモリ割り当て)):(\s*)\S+$`)

// statisticsRows are the rows of the --diagnostics table, right-aligned to
// the widest value: the memory values change the alignment.
var statisticsRows = regexp.MustCompile(`(?m)^([^:\n]+):[ ]+(\S+)$`)

func normalizeOutput(out string) string {
	return statisticsRows.ReplaceAllString(memoryRows.ReplaceAllString(out, "$1:$2X"), "$1: $2")
}

func runBackends(t *testing.T, fx graphFixture, flags []string) map[string]CLIResult {
	t.Helper()
	results := map[string]CLIResult{}
	for _, backend := range []string{"none", "pointer", "store"} {
		args := append([]string{"--singleThreaded", "-p", "/p/tsconfig.json"}, flags...)
		if backend != "none" {
			args = append(args, "--astBackend", backend)
		}
		results[backend] = RunCLI(fx.files, fx.caseSensitive, "/p", args)
	}
	return results
}

var outputFlags = [][]string{
	{"--pretty", "false"},
	{"--pretty", "true"},
	{"--pretty", "false", "--quiet"},
	{"--pretty", "true", "--quiet"},
	{"--pretty", "false", "--listFiles"},
	{"--pretty", "false", "--diagnostics"},
	{"--pretty", "false", "--extendedDiagnostics", "--locale", "ja"},
}

// TestNativeCLIEquivalence: with the same input values, the Store check, the
// Pointer check and the compile path print the same output and give the same
// status, and none of them writes.
func TestNativeCLIEquivalence(t *testing.T) {
	fixtures := append(append([]graphFixture{}, diagnosticFixtures...), graphFixtures...)
	statuses := map[tsc.ExitStatus]int{}
	for _, fx := range fixtures {
		for _, flags := range outputFlags {
			t.Run(fx.name+" "+strings.Join(flags, " "), func(t *testing.T) {
				results := runBackends(t, fx, flags)
				for _, backend := range []string{"pointer", "store"} {
					got, want := results[backend], results["none"]
					if normalizeOutput(got.Output) != normalizeOutput(want.Output) || got.Status != want.Status {
						t.Errorf("%s: status %d, compile path %d\n%s", backend, got.Status, want.Status, firstDifference(strings.Split(normalizeOutput(want.Output), "\n"), strings.Split(normalizeOutput(got.Output), "\n")))
					}
				}
				for backend, r := range results {
					if len(r.Writes) != 0 {
						t.Errorf("%s writes %v", backend, r.Writes)
					}
				}
				statuses[results["store"].Status]++
			})
		}
	}
	// The fixtures reach both statuses of a check (when they all ran).
	ran := 0
	for _, n := range statuses {
		ran += n
	}
	if ran == len(fixtures)*len(outputFlags) && (statuses[tsc.ExitStatusSuccess] == 0 || statuses[tsc.ExitStatusDiagnosticsPresent_OutputsGenerated] == 0) {
		t.Errorf("statuses %v", statuses)
	}
}

// TestNativeCLIRejectsUnsupported: a config outside the check mode is
// rejected with its reasons and ExitStatusNotImplemented before any source
// is read, and nothing is written.
func TestNativeCLIRejectsUnsupported(t *testing.T) {
	files := map[string]any{
		"/p/tsconfig.json":  `{"compilerOptions": {"types": [], "lib": ["es2020"]}, "files": ["a.ts"]}`,
		"/p/a.ts":           "export const a = 1;\n",
		"/q/tsconfig.json":  `{"compilerOptions": {"composite": true}, "files": []}`,
		"/p/refs.json":      `{"compilerOptions": {"noEmit": true, "types": []}, "files": ["a.ts"], "references": [{"path": "../q"}]}`,
		"/p/composite.json": `{"compilerOptions": {"noEmit": true, "composite": true, "types": []}, "files": ["a.ts"]}`,
	}
	base := []string{"-p", "/p/tsconfig.json", "--astBackend", "store"}
	for _, tc := range []struct {
		args   []string
		reason string
	}{
		{nil, "requires '--noEmit'"},
		{[]string{"--noEmit", "--declaration"}, "does not support 'declaration'"},
		{[]string{"--noEmit", "--declaration", "--emitDeclarationOnly"}, "does not support 'emitDeclarationOnly'"},
		{[]string{"--noEmit", "--declaration", "--declarationMap"}, "does not support 'declarationMap'"},
		{[]string{"--noEmit", "--declaration", "--isolatedDeclarations"}, "does not support 'isolatedDeclarations'"},
		{[]string{"-p", "/p/composite.json"}, "does not support 'composite'"},
		{[]string{"--noEmit", "--incremental"}, "does not support 'incremental'"},
		{[]string{"--noEmit", "--watch"}, "does not support 'watch'"},
		{[]string{"--noEmit", "--noCheck"}, "does not support 'noCheck'"},
		{[]string{"--noEmit", "--listFilesOnly"}, "does not support 'listFilesOnly'"},
		{[]string{"--noEmit", "--explainFiles"}, "does not support 'explainFiles'"},
		{[]string{"--noEmit", "--generateTrace", "/p/trace"}, "does not support 'generateTrace'"},
		{[]string{"-p", "/p/refs.json"}, "does not support 'references'"},
	} {
		t.Run(strings.Join(tc.args, " "), func(t *testing.T) {
			args := append(append([]string{}, base...), tc.args...)
			r := RunCLI(files, true, "/p", args)
			if r.Status != tsc.ExitStatusNotImplemented || !strings.Contains(r.Output, tc.reason) {
				t.Errorf("status %d, output:\n%s", r.Status, r.Output)
			}
			for _, read := range r.Reads {
				if strings.HasSuffix(read, ".ts") {
					t.Errorf("read the source %s", read)
				}
			}
			if len(r.Writes) != 0 {
				t.Errorf("writes %v", r.Writes)
			}
		})
	}
}

// TestNativeCLIEntry: the parse of --astBackend, the help, --init,
// --showConfig, a response file and --build.
func TestNativeCLIEntry(t *testing.T) {
	files := map[string]any{
		"/p/tsconfig.json": `{"compilerOptions": {"noEmit": true, "types": [], "lib": ["es2020"]}, "files": ["a.ts"]}`,
		"/p/a.ts":          "export const a: number = \"s\";\n",
		"/p/args.txt":      "--noEmit --singleThreaded --astBackend store -p /p/tsconfig.json",
		"/c/tsconfig.json": `{"compilerOptions": {"noEmit": true, "astBackend": "store", "types": []}, "files": []}`,
	}
	run := func(args ...string) CLIResult { return RunCLI(files, true, "/p", args) }

	if r := run("--astBackend", "foo", "-p", "/p/tsconfig.json"); r.Status != tsc.ExitStatusDiagnosticsPresent_OutputsSkipped || !strings.Contains(r.Output, "--astBackend") {
		t.Errorf("an invalid value: %d\n%s", r.Status, r.Output)
	}
	if r := run("-p", "/p/tsconfig.json", "--astBackend"); r.Status != tsc.ExitStatusDiagnosticsPresent_OutputsSkipped || !strings.Contains(r.Output, "astBackend") {
		t.Errorf("a missing value: %d\n%s", r.Status, r.Output)
	}
	for _, backend := range []string{"pointer", "store"} {
		r := run("--singleThreaded", "--astBackend", backend, "-p", "/c/tsconfig.json")
		if !strings.Contains(r.Output, "Option 'astBackend' can only be specified on command line.") {
			t.Errorf("%s: astBackend in a tsconfig: %d\n%s", backend, r.Status, r.Output)
		}
	}

	if r := run("--help"); strings.Contains(r.Output, "astBackend") {
		t.Error("the simplified help shows astBackend")
	}
	if r := run("--all"); !strings.Contains(r.Output, "--astBackend\nSelect the AST of the compiler: 'pointer' (the default) or 'store'. Experimental") {
		t.Errorf("--all:\n%s", r.Output)
	}

	// --init writes the same tsconfig.json whatever the backend, without
	// astBackend.
	var inits []string
	for _, args := range [][]string{{"--init"}, {"--init", "--astBackend", "pointer"}, {"--init", "--astBackend", "store"}} {
		emptyFiles := map[string]any{"/i/.keep": ""}
		r := RunCLI(emptyFiles, true, "/i", args)
		if r.Status != tsc.ExitStatusSuccess || len(r.Writes) != 1 || r.Writes[0] != "write /i/tsconfig.json" {
			t.Errorf("%v: %d %v\n%s", args, r.Status, r.Writes, r.Output)
		}
		inits = append(inits, r.Output+"\n"+r.Written["/i/tsconfig.json"])
		if strings.Contains(r.Written["/i/tsconfig.json"], "astBackend") {
			t.Errorf("%v: the tsconfig.json has astBackend", args)
		}
		// The written tsconfig.json reads back without diagnostics.
		reread := RunCLI(map[string]any{"/i/tsconfig.json": r.Written["/i/tsconfig.json"], "/i/a.ts": "export {};\n"}, true, "/i", []string{"--showConfig"})
		if reread.Status != tsc.ExitStatusSuccess {
			t.Errorf("%v: the tsconfig.json does not read back: %d\n%s", args, reread.Status, reread.Output)
		}
	}
	if inits[0] != inits[1] || inits[0] != inits[2] {
		t.Errorf("--init differs by backend:\n%s", strings.Join(inits, "\n---\n"))
	}

	if r := run("--showConfig", "--astBackend", "store", "-p", "/p/tsconfig.json"); r.Status != tsc.ExitStatusSuccess || strings.Contains(r.Output, "astBackend") {
		t.Errorf("--showConfig: %d\n%s", r.Status, r.Output)
	}

	direct := run("--noEmit", "--singleThreaded", "--astBackend", "store", "-p", "/p/tsconfig.json")
	response := run("@/p/args.txt")
	if direct.Output != response.Output || direct.Status != response.Status || direct.Status != tsc.ExitStatusDiagnosticsPresent_OutputsGenerated {
		t.Errorf("response file: %d %q, direct %d %q", response.Status, response.Output, direct.Status, direct.Output)
	}

	// The build parser rejects --astBackend (TS5094): the build mode has the
	// Pointer program only.
	for _, extra := range [][]string{nil, {"--quiet"}} {
		r := run(append([]string{"--build", "/p/tsconfig.json", "--astBackend", "store"}, extra...)...)
		if r.Status != tsc.ExitStatusDiagnosticsPresent_OutputsSkipped || len(r.Writes) != 0 || (extra == nil && !strings.Contains(r.Output, "TS5094: Compiler option '--astBackend' may not be used with '--build'.")) {
			t.Errorf("--build %v: %d %v\n%s", extra, r.Status, r.Writes, r.Output)
		}
	}
}
