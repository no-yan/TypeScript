package compiler_test

import (
	"testing"

	"github.com/microsoft/TypeScript/tsc/internal/ast"
	"github.com/microsoft/TypeScript/tsc/internal/bundled"
	"github.com/microsoft/TypeScript/tsc/internal/checker"
	"github.com/microsoft/TypeScript/tsc/internal/compiler"
	"github.com/microsoft/TypeScript/tsc/internal/core"
	"github.com/microsoft/TypeScript/tsc/internal/tsoptions"
	"github.com/microsoft/TypeScript/tsc/internal/vfs/vfstest"
	"gotest.tools/v3/assert"
)

func newOwnershipProgram(t *testing.T, files map[string]string, checkers int) *compiler.Program {
	t.Helper()
	if !bundled.Embedded {
		t.Skip("bundled files are not embedded")
	}
	fs := vfstest.FromMap[any](nil, false)
	fs = bundled.WrapFS(fs)
	names := make([]string, 0, len(files))
	for name, contents := range files {
		_ = fs.WriteFile(name, contents)
		names = append(names, name)
	}
	opts := core.CompilerOptions{
		SkipLibCheck:                 core.TSTrue,
		SkipDefaultLibCheck:          core.TSTrue,
		NoEmit:                       core.TSTrue,
		Strict:                       core.TSTrue,
		StrictNullChecks:             core.TSTrue,
		StrictPropertyInitialization: core.TSTrue,
	}
	if checkers > 0 {
		opts.Checkers = &checkers
	}
	return compiler.NewProgram(compiler.ProgramOptions{
		Config: &tsoptions.ParsedCommandLine{
			ParsedConfig: &tsoptions.ParsedOptions{
				FileNames:       names,
				CompilerOptions: &opts,
			},
		},
		Host: compiler.NewCompilerHost("/", fs, bundled.LibPath(), nil, nil, nil),
	})
}

func TestPrivateStoreNoRaceOnSyntheticElementAccess(t *testing.T) {
	t.Parallel()
	p := newOwnershipProgram(t, map[string]string{
		"/a.ts": `export class A {
  x: number;
  constructor() { this.x = 1; }
}
`,
		"/b.ts": `export function f(xs: [number, ...number[]]) {
  const [head, ...rest] = xs;
  return rest;
}
`,
		"/c.ts": `export class C {
  y: string;
  constructor() { this.y = ""; }
}
`,
	}, 2)
	t.Cleanup(p.Close)
	_ = p.GetSemanticDiagnostics(t.Context(), nil)
}

func TestNewCallSignatureWithoutCheckSourceFile(t *testing.T) {
	t.Parallel()
	p := newOwnershipProgram(t, map[string]string{
		"/a.ts": `export class C { get x() { return 1 } }
`,
	}, 1)
	t.Cleanup(p.Close)
	file := p.GetSourceFile("/a.ts")
	c, done := p.GetTypeCheckerForFileExclusive(t.Context(), file)
	defer done()
	assert.Assert(t, c.FactoryStore() != nil, "factory Store must be live at NewChecker")
	name := file.ParseRoot().Statements()[0].Members()[0].Name()
	sym := c.GetSymbolAtLocation(name)
	assert.Assert(t, sym != nil)
	typ := c.GetTypeOfSymbol(sym)
	assert.Assert(t, typ != nil)
	_ = c.GetSignaturesOfType(typ, checker.SignatureKindCall)
}

func TestBinderSymbolsDoNotHoldSynthStoreID(t *testing.T) {
	t.Parallel()
	p := newOwnershipProgram(t, map[string]string{
		"/a.ts": `export class A {
  x: number;
  constructor() { this.x = 1; }
}
export function f(xs: [number, ...number[]]) {
  const [head, ...rest] = xs;
  return rest;
}
`,
		"/b.ts": `export class B { y = 1; }
`,
	}, 2)
	t.Cleanup(p.Close)
	_ = p.GetSemanticDiagnostics(t.Context(), nil)
	p.ForEachCheckerParallel(func(idx int, c *checker.Checker) {
		c.AssertBinderSymbolsStayOnParseStores()
	})
}

func TestProgramCloseUnregistersSynthStores(t *testing.T) {
	t.Parallel()
	p := newOwnershipProgram(t, map[string]string{
		"/a.ts": `export const x = 1;
`,
	}, 1)
	file := p.GetSourceFile("/a.ts")
	c, done := p.GetTypeCheckerForFileExclusive(t.Context(), file)
	id := c.FactoryStore().ID()
	assert.Assert(t, id != 0)
	done()
	p.Close()
	assert.Equal(t, ast.Handle{}, ast.NodeOf(ast.MakeGlobalRef(id, 1)))
}

func TestParallelEmitPreservesParseStores(t *testing.T) {
	files := map[string]string{
		"/a.ts": `export class A { value: number = 1; constructor() {} }`,
		"/b.ts": `import { A } from "./a"; export class B extends A { get twice(): number { return this.value * 2; } }`,
		"/c.ts": `import { B } from "./b"; export const read = (b?: B): number => b?.twice ?? 0;`,
	}
	p := newOwnershipProgram(t, files, 4)
	defer p.Close()
	p.Options().NoEmit = core.TSFalse
	p.Options().Declaration = core.TSTrue
	p.Options().SourceMap = core.TSTrue
	p.Options().Target = core.ScriptTargetES2015
	p.Options().OutDir = "/out"
	_ = p.GetSemanticDiagnostics(t.Context(), nil)
	roots := make(map[*ast.SourceFile]ast.Handle)
	sizes := make(map[*ast.Store]int)
	for name := range files {
		file := p.GetSourceFile(name)
		roots[file] = file.ParseRoot()
		sizes[file.ParseStore()] = file.ParseStore().Len()
	}
	// Keep reading all parse trees while transforms allocate in parallel.
	stop := make(chan struct{})
	done := make(chan struct{})
	go func() {
		defer close(done)
		for {
			select {
			case <-stop:
				return
			default:
			}
			for _, root := range roots {
				ast.Walk(root, func(n ast.Handle) bool { _ = n.Flags(); _ = n.Parent(); return false })
			}
		}
	}()
	var parallel emitFiles
	result := p.Emit(t.Context(), compiler.EmitOptions{WriteFile: parallel.write})
	close(stop)
	<-done
	assert.Assert(t, !result.EmitSkipped)
	assert.Assert(t, len(parallel.m) >= len(files)*2)
	for file, root := range roots {
		assert.Equal(t, root, file.ParseRoot())
		assert.Equal(t, sizes[root.Store()], root.Store().Len())
		assert.Equal(t, file, root.Store().SourceFile())
	}
	p.Options().SingleThreaded = core.TSTrue
	var serial emitFiles
	p.Emit(t.Context(), compiler.EmitOptions{WriteFile: serial.write})
	assert.DeepEqual(t, parallel.m, serial.m)
}
