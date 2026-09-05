package checker_test

import (
	"testing"

	"github.com/microsoft/TypeScript/tsc/internal/ast"
	"github.com/microsoft/TypeScript/tsc/internal/bundled"
	"github.com/microsoft/TypeScript/tsc/internal/compiler"
	"github.com/microsoft/TypeScript/tsc/internal/core"
	"github.com/microsoft/TypeScript/tsc/internal/tsoptions"
	"github.com/microsoft/TypeScript/tsc/internal/vfs/vfstest"
	"gotest.tools/v3/assert"
)

func TestGetSymbolAtLocation(t *testing.T) {
	t.Parallel()

	content := `interface Foo {
  bar: string;
}
declare const foo: Foo;
foo.bar;`
	fs := vfstest.FromMap(map[string]string{
		"/foo.ts": content,
		"/tsconfig.json": `
				{
					"compilerOptions": {},
					"files": ["foo.ts"]
				}
			`,
	}, false /*useCaseSensitiveFileNames*/)
	fs = bundled.WrapFS(fs)

	cd := "/"
	host := compiler.NewCompilerHost(cd, fs, bundled.LibPath(), nil, nil, nil)

	parsed, errors := tsoptions.GetParsedCommandLineOfConfigFile("/tsconfig.json", &core.CompilerOptions{}, nil, host, nil)
	assert.Equal(t, len(errors), 0, "Expected no errors in parsed command line")

	p := compiler.NewProgram(compiler.ProgramOptions{
		Config: parsed,
		Host:   host,
	})
	p.BindSourceFiles()
	c, done := p.GetTypeChecker(t.Context())
	defer done()
	file := p.GetSourceFile("/foo.ts")
	interfaceId := file.ParseRoot().Statements()[0].Name()
	varId := (file.ParseRoot().Statements()[1].VariableStatementDeclarationList()).Store().ListSlice(file.ParseRoot().Statements()[1].VariableStatementDeclarationList().VariableDeclarationListDeclarations())[0].Name()
	propAccess := file.ParseRoot().Statements()[2].Expression()
	nodes := []ast.Handle{interfaceId, varId, propAccess}
	for _, node := range nodes {
		symbol := c.GetSymbolAtLocation(node)
		if symbol == nil {
			t.Fatalf("Expected symbol to be non-nil")
		}
	}
}

func TestDeclareGlobalIsVisibleInOtherFile(t *testing.T) {
	t.Parallel()
	fs := bundled.WrapFS(vfstest.FromMap(map[string]string{
		"/aug.d.ts": "export {};\ndeclare global { var foo: number; }\n",
		"/use.ts":   "foo;\n",
		"/tsconfig.json": `{
			"compilerOptions": { "noEmit": true, "skipLibCheck": true },
			"files": ["aug.d.ts", "use.ts"]
		}`,
	}, false))
	host := compiler.NewCompilerHost("/", fs, bundled.LibPath(), nil, nil, nil)
	parsed, errors := tsoptions.GetParsedCommandLineOfConfigFile("/tsconfig.json", &core.CompilerOptions{}, nil, host, nil)
	assert.Equal(t, len(errors), 0)
	p := compiler.NewProgram(compiler.ProgramOptions{Config: parsed, Host: host})
	file := p.GetSourceFile("/use.ts")
	diags := p.GetSemanticDiagnostics(t.Context(), file)
	if len(diags) != 0 {
		t.Fatalf("expected foo from declare global, got %v", diags)
	}
}

func TestNestedAmbientGlobalIsVisible(t *testing.T) {
	t.Parallel()
	fs := bundled.WrapFS(vfstest.FromMap(map[string]string{
		"/buffer.d.ts": "declare module \"node:buffer\" { global { var Buffer: { from(s: string): object }; } }\n",
		"/use.ts":      "Buffer.from(\"x\");\n",
		"/tsconfig.json": `{
			"compilerOptions": { "noEmit": true, "skipLibCheck": true },
			"files": ["buffer.d.ts", "use.ts"]
		}`,
	}, false))
	host := compiler.NewCompilerHost("/", fs, bundled.LibPath(), nil, nil, nil)
	parsed, errors := tsoptions.GetParsedCommandLineOfConfigFile("/tsconfig.json", &core.CompilerOptions{}, nil, host, nil)
	assert.Equal(t, len(errors), 0)
	p := compiler.NewProgram(compiler.ProgramOptions{Config: parsed, Host: host})
	file := p.GetSourceFile("/use.ts")
	diags := p.GetSemanticDiagnostics(t.Context(), file)
	if len(diags) != 0 {
		t.Fatalf("expected Buffer from nested global, got %v", diags)
	}
}

func TestOptionalParentNarrowsInDefaultParam(t *testing.T) {
	t.Parallel()
	const fullIface = `export interface S {
    flags: number;
    escapedName: string;
    declarations?: object[];
    valueDeclaration?: object;
    members?: Map<string, S>;
    exports?: Map<string, S>;
    globalExports?: Map<string, S>;
    id: number;
    mergeId: number;
    parent?: S;
    exportSymbol?: S;
    constEnumOnlyModule: boolean | undefined;
    isReferenced?: number;
    lastAssignmentPos?: number;
    isReplaceableByMethod?: boolean;
    assignmentDeclarationMembers?: Map<number, object>;
}
declare const emptySymbols: Map<string, S>;
function getMembersOfSymbol(symbol: S) {
    return symbol.members || emptySymbols;
}
`
	cases := []struct {
		name string
		src  string
	}{
		{
			name: "simpleTakeWithReturn",
			src: `export interface S { parent?: S }
function take(s: S) { return s; }
export function f(x: S, y = x.parent ? take(x.parent) : undefined) {
    return y;
}
`,
		},
		{
			name: "fullTake",
			src: fullIface + `function take(s: S) { return s; }
export function f(x: S, y = x.parent ? take(x.parent) : undefined) {}
`,
		},
		{
			name: "fullGetMembers",
			src: fullIface + `export function f(x: S, y = x.parent ? getMembersOfSymbol(x.parent) : undefined) {}
`,
		},
		{
			name: "fullArrayFrom",
			src: fullIface + `export function f(x: S, y: S[] | undefined = x.parent ? Array.from(getMembersOfSymbol(x.parent).values()) : undefined) {
    return y;
}
`,
		},
		{
			name: "fullArrayFromInBody",
			src: fullIface + `export function f(x: S) {
    const y = x.parent ? Array.from(getMembersOfSymbol(x.parent).values()) : undefined;
    return y;
}
`,
		},
		{
			name: "simpleArrayFrom",
			src: `export interface S { members?: Map<string, S>; parent?: S }
declare const emptySymbols: Map<string, S>;
function getMembersOfSymbol(symbol: S) {
    return symbol.members || emptySymbols;
}
export function f(x: S, y: S[] | undefined = x.parent ? Array.from(getMembersOfSymbol(x.parent).values()) : undefined) {}
`,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			fs := bundled.WrapFS(vfstest.FromMap(map[string]string{
				"/foo.ts": tc.src,
				"/tsconfig.json": `{
					"compilerOptions": { "noEmit": true, "strict": true, "skipLibCheck": true, "lib": ["es2020"] },
					"files": ["foo.ts"]
				}`,
			}, false))
			host := compiler.NewCompilerHost("/", fs, bundled.LibPath(), nil, nil, nil)
			parsed, errors := tsoptions.GetParsedCommandLineOfConfigFile("/tsconfig.json", &core.CompilerOptions{}, nil, host, nil)
			assert.Equal(t, len(errors), 0)
			p := compiler.NewProgram(compiler.ProgramOptions{Config: parsed, Host: host})
			file := p.GetSourceFile("/foo.ts")
			diags := p.GetSemanticDiagnostics(t.Context(), file)
			if len(diags) != 0 {
				t.Fatalf("%s: unexpected diagnostics: %v", tc.name, diags)
			}
		})
	}
}

func TestScriptAmbientVarIsGlobal(t *testing.T) {
	t.Parallel()
	fs := bundled.WrapFS(vfstest.FromMap(map[string]string{
		"/globals.d.ts": "declare var process: { pid: number };\n",
		"/use.ts":       "process.pid;\n",
		"/tsconfig.json": `{
			"compilerOptions": { "noEmit": true, "skipLibCheck": true },
			"files": ["globals.d.ts", "use.ts"]
		}`,
	}, false))
	host := compiler.NewCompilerHost("/", fs, bundled.LibPath(), nil, nil, nil)
	parsed, errors := tsoptions.GetParsedCommandLineOfConfigFile("/tsconfig.json", &core.CompilerOptions{}, nil, host, nil)
	assert.Equal(t, len(errors), 0)
	p := compiler.NewProgram(compiler.ProgramOptions{Config: parsed, Host: host})
	file := p.GetSourceFile("/use.ts")
	diags := p.GetSemanticDiagnostics(t.Context(), file)
	if len(diags) != 0 {
		t.Fatalf("expected process from script .d.ts, got %v", diags)
	}
}

func TestCrossFileTypeQueryTypeArgumentsDoNotPanic(t *testing.T) {
	t.Parallel()
	fs := bundled.WrapFS(vfstest.FromMap(map[string]string{
		"/decl.ts": "export class Foo<T> { value!: T }\nexport function take<T extends typeof Foo<number>>(ctor: T) { return ctor }\n",
		"/use.ts":  "import { take, Foo } from \"./decl\";\ntake(Foo);\n",
		"/tsconfig.json": `{
			"compilerOptions": { "noEmit": true, "skipLibCheck": true, "strict": true },
			"files": ["decl.ts", "use.ts"]
		}`,
	}, true))
	host := compiler.NewCompilerHost("/", fs, bundled.LibPath(), nil, nil, nil)
	parsed, errors := tsoptions.GetParsedCommandLineOfConfigFile("/tsconfig.json", &core.CompilerOptions{}, nil, host, nil)
	assert.Equal(t, 0, len(errors))
	p := compiler.NewProgram(compiler.ProgramOptions{Config: parsed, Host: host})
	_ = p.GetSemanticDiagnostics(t.Context(), p.GetSourceFile("/use.ts"))
	_ = p.GetSemanticDiagnostics(t.Context(), p.GetSourceFile("/decl.ts"))
}
