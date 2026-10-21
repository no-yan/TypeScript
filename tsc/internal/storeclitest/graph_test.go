package storeclitest

import (
	"fmt"
	"strings"
	"testing"

	"github.com/microsoft/TypeScript/tsc/internal/bundled"
	"github.com/microsoft/TypeScript/tsc/internal/compiler"
	"github.com/microsoft/TypeScript/tsc/internal/core"
	"github.com/microsoft/TypeScript/tsc/internal/storecompiler"
	"github.com/microsoft/TypeScript/tsc/internal/tsoptions"
	"github.com/microsoft/TypeScript/tsc/internal/tspath"
	"github.com/microsoft/TypeScript/tsc/internal/vfs"
	"github.com/microsoft/TypeScript/tsc/internal/vfs/vfstest"
)

// graphFixture is the input of both programs: the files (text or
// vfstest.Symlink), the case sensitivity, the cwd and the tsconfig. expect are
// lines (or line prefixes) that the graph must have, so that the two programs
// cannot agree on a wrong graph.
type graphFixture struct {
	name          string
	files         map[string]any
	caseSensitive bool
	cwd, config   string
	expect        []string
}

// buildGraphs parses the config and builds each program from a fresh FS and
// host of its own: they share the input values only.
func buildGraphs(t *testing.T, fx graphFixture) (pointer, store []string) {
	t.Helper()
	newFS := func() vfs.FS {
		return bundled.WrapFS(vfstest.FromMap(fx.files, core.IfElse(fx.caseSensitive, tspath.CaseSensitive, tspath.CaseInsensitive)))
	}
	parse := func(fs vfs.FS) *tsoptions.ParsedCommandLine {
		config, diags := tsoptions.GetParsedCommandLineOfConfigFile(tspath.RootedFilePathFromAbsolute(fx.config), &core.CompilerOptions{}, nil, fs, nil)
		if len(diags) != 0 {
			t.Fatalf("config diagnostics: %v", diags)
		}
		return config
	}
	ph := compiler.NewCompilerHost(newFS(), bundled.LibPath(), nil, nil, nil)
	p := compiler.NewProgram(compiler.ProgramOptions{Config: parse(ph.FS()), Host: ph, SingleThreaded: core.TSTrue})
	sh := storecompiler.NewCompilerHost(newFS(), bundled.LibPath(), nil, nil)
	s := storecompiler.NewProgram(storecompiler.ProgramOptions{Config: parse(sh.FS()), Host: sh, SingleThreaded: core.TSTrue})
	return PointerGraph(p), StoreGraph(s)
}

// firstDifference is the first line where the graphs differ, or "".
func firstDifference(pointer, store []string) string {
	for i := range max(len(pointer), len(store)) {
		var p, s string
		if i < len(pointer) {
			p = pointer[i]
		}
		if i < len(store) {
			s = store[i]
		}
		if p != s {
			return fmt.Sprintf("line %d:\npointer %s\nstore   %s", i, p, s)
		}
	}
	return ""
}

func checkGraph(t *testing.T, fx graphFixture) []string {
	t.Helper()
	pointer, store := buildGraphs(t, fx)
	if d := firstDifference(pointer, store); d != "" {
		t.Errorf("the graphs differ at %s", d)
	}
	for _, want := range fx.expect {
		found := false
		for _, line := range store {
			found = found || strings.HasPrefix(strings.TrimSpace(line), want)
		}
		if !found {
			t.Errorf("no line %q in the graph:\n%s", want, strings.Join(store, "\n"))
		}
	}
	return store
}

const lib = `"lib": ["es2020"], "types": []`

var graphFixtures = []graphFixture{
	{
		name: "cycle",
		files: map[string]any{
			"/p/tsconfig.json": `{"compilerOptions": {"noEmit": true, "module": "commonjs", ` + lib + `}, "files": ["a.ts"]}`,
			"/p/a.ts":          "import { b } from \"./b\";\nexport const a = 1 + b;\n",
			"/p/b.ts":          "import { a } from \"./a\";\nexport const b = 2;\nexport const c = a;\n",
		},
		expect: []string{`module /p/a.ts "./b" mode=CommonJS -> "/p/b.ts"`, `module /p/b.ts "./a" mode=CommonJS -> "/p/a.ts"`},
	},
	{
		name: "nodenext",
		files: map[string]any{
			"/p/tsconfig.json":                 `{"compilerOptions": {"noEmit": true, "module": "nodenext", ` + lib + `}, "include": ["src"]}`,
			"/p/package.json":                  `{"name": "p", "type": "module", "imports": {"#util": "./src/util.js"}}`,
			"/p/src/main.ts":                   "import { u } from \"#util\";\nimport { e } from \"esm\";\nimport cjs = require(\"cjs\");\nexport const m = u + e + cjs.c;\n",
			"/p/src/util.ts":                   "export const u = 1;\n",
			"/p/src/old.cts":                   "import { e } from \"esm\";\nexport const o = e;\n",
			"/p/node_modules/esm/package.json": `{"name": "esm", "version": "1.0.0", "type": "module", "exports": {".": {"import": "./index.js", "require": "./index.cjs"}}}`,
			"/p/node_modules/esm/index.d.ts":   "export declare const e: number;\n",
			"/p/node_modules/esm/index.d.cts":  "export declare const e: string;\n",
			"/p/node_modules/cjs/package.json": `{"name": "cjs", "version": "2.0.0", "main": "index.js"}`,
			"/p/node_modules/cjs/index.d.ts":   "export declare const c: number;\n",
		},
		expect: []string{
			`module /p/src/main.ts "#util" mode=ESNext -> "/p/src/util.ts"`,
			`module /p/src/main.ts "esm" mode=ESNext -> "/p/node_modules/esm/index.d.ts"`,
			`module /p/src/main.ts "cjs" mode=CommonJS -> "/p/node_modules/cjs/index.d.ts"`,
			`module /p/src/old.cts "esm" mode=CommonJS -> "/p/node_modules/esm/index.d.cts"`,
		},
	},
	{
		name: "bundler-paths",
		files: map[string]any{
			"/p/tsconfig.json":                 `{"compilerOptions": {"noEmit": true, "module": "esnext", "moduleResolution": "bundler", "baseUrl": ".", "paths": {"@lib/*": ["lib/*"]}, ` + lib + `}, "files": ["src/a.ts"]}`,
			"/p/src/a.ts":                      "import { x } from \"@lib/x\";\nimport { y } from \"src/y\";\nimport { z } from \"pkg/sub\";\nexport const a = x + y + z;\n",
			"/p/lib/x.ts":                      "export const x = 1;\n",
			"/p/src/y.ts":                      "export const y = 1;\n",
			"/p/node_modules/pkg/package.json": `{"name": "pkg", "exports": {"./sub": {"types": "./sub.d.ts", "default": "./sub.js"}}}`,
			"/p/node_modules/pkg/sub.d.ts":     "export declare const z: number;\n",
		},
		// baseUrl alone does not resolve (tsgo resolves through paths only).
		expect: []string{`module /p/src/a.ts "@lib/x" mode=ESNext -> "/p/lib/x.ts"`, `module /p/src/a.ts "src/y" mode=ESNext -> ""`, `module /p/src/a.ts "pkg/sub" mode=ESNext -> "/p/node_modules/pkg/sub.d.ts"`},
	},
	{
		name: "json-js",
		files: map[string]any{
			"/p/tsconfig.json":                  `{"compilerOptions": {"noEmit": true, "module": "commonjs", "resolveJsonModule": true, "esModuleInterop": true, "allowJs": true, "maxNodeModuleJsDepth": 1, ` + lib + `}, "files": ["a.ts"]}`,
			"/p/a.ts":                           "import data from \"./data.json\";\nimport { j } from \"./j\";\nimport { d } from \"deep\";\nexport const a = data.n + j + d;\n",
			"/p/data.json":                      `{"n": 1}`,
			"/p/j.js":                           "/** @import { T } from \"./t\" */\nexports.j = 1;\n",
			"/p/t.ts":                           "export type T = number;\n",
			"/p/node_modules/deep/package.json": `{"name": "deep", "main": "index.js"}`,
			"/p/node_modules/deep/index.js":     "const more = require(\"more\");\nexports.d = 1;\n",
			"/p/node_modules/more/package.json": `{"name": "more", "main": "index.js"}`,
			"/p/node_modules/more/index.js":     "exports.m = 1;\n",
		},
		expect: []string{`module /p/a.ts "./data.json" mode=CommonJS -> "/p/data.json"`, `module /p/a.ts "./j" mode=CommonJS -> "/p/j.js"`, `module /p/j.js "./t" mode=CommonJS -> "/p/t.ts"`},
	},
	{
		name: "types-references",
		files: map[string]any{
			"/p/tsconfig.json": `{"compilerOptions": {"noEmit": true, "module": "commonjs", "lib": ["es2020"], "types": ["auto"]}, "files": ["a.ts"]}`,
			"/p/a.ts":          "/// <reference path=\"./ref.d.ts\" />\n/// <reference types=\"manual\" />\n/// <reference lib=\"es2021.string\" />\nexport const a = r + m + au;\n",
			"/p/ref.d.ts":      "declare const r: number;\n",
			"/p/node_modules/@types/manual/index.d.ts": "declare const m: number;\n",
			"/p/node_modules/@types/auto/index.d.ts":   "declare const au: number;\n",
			"/p/node_modules/@types/unused/index.d.ts": "declare const unused: number;\n",
		},
		expect: []string{`type /p/a.ts "manual"`, `type /p/__inferred type names__.ts "auto"`},
	},
	{
		name: "jsx-helpers",
		files: map[string]any{
			"/p/tsconfig.json":                       `{"compilerOptions": {"noEmit": true, "module": "commonjs", "jsx": "react-jsx", "importHelpers": true, ` + lib + `}, "files": ["c.tsx", "d.ts"]}`,
			"/p/c.tsx":                               "export const c = <div/>;\n",
			"/p/d.ts":                                "export const d = 1;\n",
			"/p/node_modules/react/package.json":     `{"name": "react", "exports": {"./jsx-runtime": {"types": "./jsx-runtime.d.ts"}}}`,
			"/p/node_modules/react/jsx-runtime.d.ts": "export {};\n",
			"/p/node_modules/tslib/package.json":     `{"name": "tslib", "types": "tslib.d.ts"}`,
			"/p/node_modules/tslib/tslib.d.ts":       "export {};\n",
		},
		expect: []string{`helpers "tslib" KindStringLiteral parent=KindImportDeclaration root=true statement=false pos=-1,-1`, `jsx react/jsx-runtime "react/jsx-runtime"`},
	},
	{
		name: "symlink-dedup",
		files: map[string]any{
			"/p/tsconfig.json":                                  `{"compilerOptions": {"noEmit": true, "module": "commonjs", "preserveSymlinks": false, ` + lib + `}, "files": ["a.ts"]}`,
			"/p/a.ts":                                           "import { x } from \"linked\";\nimport { y } from \"one\";\nimport { z } from \"two\";\nexport const a = x + y + z;\n",
			"/p/node_modules/linked":                            vfstest.Symlink("/shared/linked"),
			"/shared/linked/package.json":                       `{"name": "linked", "version": "1.0.0", "types": "index.d.ts"}`,
			"/shared/linked/index.d.ts":                         "export declare const x: number;\n",
			"/p/node_modules/one/package.json":                  `{"name": "one", "version": "1.0.0", "types": "index.d.ts"}`,
			"/p/node_modules/one/index.d.ts":                    "export { y } from \"dup\";\n",
			"/p/node_modules/one/node_modules/dup/package.json": `{"name": "dup", "version": "3.0.0", "types": "index.d.ts"}`,
			"/p/node_modules/one/node_modules/dup/index.d.ts":   "export declare const y: number;\nexport declare const z: number;\n",
			"/p/node_modules/two/package.json":                  `{"name": "two", "version": "1.0.0", "types": "index.d.ts"}`,
			"/p/node_modules/two/index.d.ts":                    "export { z } from \"dup\";\n",
			"/p/node_modules/two/node_modules/dup/package.json": `{"name": "dup", "version": "3.0.0", "types": "index.d.ts"}`,
			"/p/node_modules/two/node_modules/dup/index.d.ts":   "export declare const y: number;\nexport declare const z: number;\n",
		},
		caseSensitive: true,
		expect:        []string{`module /p/a.ts "linked" mode=CommonJS -> "/shared/linked/index.d.ts"`, `redirects [/p/node_modules/two/node_modules/dup/index.d.ts]`},
	},
	{
		name: "casing",
		files: map[string]any{
			"/p/tsconfig.json": `{"compilerOptions": {"noEmit": true, "module": "commonjs", "forceConsistentCasingInFileNames": true, ` + lib + `}, "files": ["a.ts"]}`,
			"/p/a.ts":          "import { b } from \"./B\";\nimport { b as b2 } from \"./b\";\nexport const a = b + b2;\n",
			"/p/b.ts":          "export const b = 1;\n",
		},
		expect: []string{`reasons /p/b.ts`},
	},
	{
		name: "missing",
		files: map[string]any{
			"/p/tsconfig.json": `{"compilerOptions": {"noEmit": true, "module": "commonjs", "lib": ["es2020"], "types": ["nope"]}, "files": ["a.ts", "gone.ts"]}`,
			"/p/a.ts":          "/// <reference path=\"./absent.d.ts\" />\n/// <reference types=\"nothing\" />\n/// <reference lib=\"notalib\" />\nimport { x } from \"./nowhere\";\nexport const a = x;\n",
		},
		expect: []string{`module /p/a.ts "./nowhere" mode=CommonJS -> ""`, `type /p/a.ts "nothing"`, `reasons /p/gone.ts nofile missing=true`},
	},
	{
		name: "empty-roots",
		files: map[string]any{
			"/p/tsconfig.json": `{"compilerOptions": {"noEmit": true, "module": "commonjs", ` + lib + `}, "files": []}`,
		},
	},
}

func TestNativeGraph(t *testing.T) {
	for _, fx := range graphFixtures {
		t.Run(fx.name, func(t *testing.T) {
			if fx.cwd == "" {
				fx.cwd = "/p"
			}
			if fx.config == "" {
				fx.config = "/p/tsconfig.json"
			}
			checkGraph(t, fx)
		})
	}
}
