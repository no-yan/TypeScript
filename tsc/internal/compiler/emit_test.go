package compiler_test

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"testing"

	"github.com/microsoft/TypeScript/tsc/internal/bundled"
	"github.com/microsoft/TypeScript/tsc/internal/compiler"
	"github.com/microsoft/TypeScript/tsc/internal/core"
	"github.com/microsoft/TypeScript/tsc/internal/tsoptions"
	"github.com/microsoft/TypeScript/tsc/internal/vfs/vfstest"
	"gotest.tools/v3/assert"
)

type emitFiles struct {
	mu sync.Mutex
	m  map[string]string
}

func (e *emitFiles) write(fileName string, text string, data *compiler.WriteFileData) error {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.m == nil {
		e.m = map[string]string{}
	}
	e.m[fileName] = text
	return nil
}

// generateLongLineTS generates TypeScript source code that produces a single very long line.
// This simulates generated code (e.g., from code generators) that has no line breaks,
// which triggers O(n²) behavior in source map generation due to
// GetECMALineAndUTF16CharacterOfPosition scanning from line start for each position.
func generateLongLineTS(numProperties int) string {
	// Build a large object literal all on one line, with no line breaks.
	var b strings.Builder
	b.WriteString("export const data: Record<string, number> = {")
	for i := range numProperties {
		if i > 0 {
			b.WriteString(", ")
		}
		fmt.Fprintf(&b, "prop_%d: %d", i, i)
	}
	b.WriteString("};")
	return b.String()
}

func BenchmarkEmitLongLines(b *testing.B) {
	if !bundled.Embedded {
		b.Skip("bundled files are not embedded")
	}

	for _, numProps := range []int{1000, 5000, 10000} {
		b.Run(fmt.Sprintf("props_%d", numProps), func(b *testing.B) {
			source := generateLongLineTS(numProps)

			fs := vfstest.FromMap(map[string]string{
				"/dev/src/index.ts": source,
			}, true /*useCaseSensitiveFileNames*/)
			fs = bundled.WrapFS(fs)

			opts := core.CompilerOptions{
				Target:    core.ScriptTargetES2015,
				SourceMap: core.TSTrue,
				OutDir:    "/dev/out",
			}

			host := compiler.NewCompilerHost("/dev/src", fs, bundled.LibPath(), nil, nil, nil)

			p := compiler.NewProgram(compiler.ProgramOptions{
				Config: &tsoptions.ParsedCommandLine{
					ParsedConfig: &tsoptions.ParsedOptions{
						FileNames:       []string{"/dev/src/index.ts"},
						CompilerOptions: &opts,
					},
				},
				Host: host,
			})

			// Discard written files — we only care about emit performance.
			nopWriteFile := func(fileName string, text string, data *compiler.WriteFileData) error {
				return nil
			}

			b.ResetTimer()
			b.ReportAllocs()

			for b.Loop() {
				p.Emit(context.Background(), compiler.EmitOptions{
					WriteFile: nopWriteFile,
				})
			}
		})
	}
}

func BenchmarkEmitManyFiles(b *testing.B) {
	if !bundled.Embedded {
		b.Skip("bundled files are not embedded")
	}

	// Simulate many files with moderately long single-line content.
	numFiles := 200
	numPropsPerFile := 500

	files := make(map[string]string, numFiles)
	fileNames := make([]string, 0, numFiles)
	for i := range numFiles {
		name := fmt.Sprintf("/dev/src/file_%d.ts", i)
		files[name] = generateLongLineTS(numPropsPerFile)
		fileNames = append(fileNames, name)
	}

	fs := vfstest.FromMap(files, true)
	fs = bundled.WrapFS(fs)

	opts := core.CompilerOptions{
		Target:    core.ScriptTargetES2015,
		SourceMap: core.TSTrue,
		OutDir:    "/dev/out",
	}

	host := compiler.NewCompilerHost("/dev/src", fs, bundled.LibPath(), nil, nil, nil)

	p := compiler.NewProgram(compiler.ProgramOptions{
		Config: &tsoptions.ParsedCommandLine{
			ParsedConfig: &tsoptions.ParsedOptions{
				FileNames:       fileNames,
				CompilerOptions: &opts,
			},
		},
		Host: host,
	})

	nopWriteFile := func(fileName string, text string, data *compiler.WriteFileData) error {
		return nil
	}

	b.ResetTimer()
	b.ReportAllocs()

	for b.Loop() {
		p.Emit(context.Background(), compiler.EmitOptions{
			WriteFile: nopWriteFile,
		})
	}
}

// BenchmarkEmitLongLinesWithLineBreaks is a control benchmark that emits the same amount
// of code but WITH line breaks, showing that the issue is specific to long lines.
func BenchmarkEmitLongLinesWithLineBreaks(b *testing.B) {
	if !bundled.Embedded {
		b.Skip("bundled files are not embedded")
	}

	numProperties := 10000

	// Same content but with newlines between each property.
	var sb strings.Builder
	sb.WriteString("export const data: Record<string, number> = {\n")
	for i := range numProperties {
		if i > 0 {
			sb.WriteString(",\n")
		}
		fmt.Fprintf(&sb, "  prop_%d: %d", i, i)
	}
	sb.WriteString("\n};\n")
	source := sb.String()

	fs := vfstest.FromMap(map[string]string{
		"/dev/src/index.ts": source,
	}, true)
	fs = bundled.WrapFS(fs)

	opts := core.CompilerOptions{
		Target:    core.ScriptTargetES2015,
		SourceMap: core.TSTrue,
		OutDir:    "/dev/out",
	}

	host := compiler.NewCompilerHost("/dev/src", fs, bundled.LibPath(), nil, nil, nil)

	p := compiler.NewProgram(compiler.ProgramOptions{
		Config: &tsoptions.ParsedCommandLine{
			ParsedConfig: &tsoptions.ParsedOptions{
				FileNames:       []string{"/dev/src/index.ts"},
				CompilerOptions: &opts,
			},
		},
		Host: host,
	})

	nopWriteFile := func(fileName string, text string, data *compiler.WriteFileData) error {
		return nil
	}

	b.ResetTimer()
	b.ReportAllocs()

	for b.Loop() {
		p.Emit(context.Background(), compiler.EmitOptions{
			WriteFile: nopWriteFile,
		})
	}
}

func TestGetDeclarationDiagnosticsDoesNotMarkProgramFileAsDeclaration(t *testing.T) {
	t.Parallel()
	if !bundled.Embedded {
		t.Skip("bundled files are not embedded")
	}

	fs := bundled.WrapFS(vfstest.FromMap(map[string]string{
		"/index.ts": "export const x = 1;\n",
		"/tsconfig.json": `{
			"compilerOptions": { "declaration": true, "skipLibCheck": true },
			"files": ["index.ts"]
		}`,
	}, true))
	host := compiler.NewCompilerHost("/", fs, bundled.LibPath(), nil, nil, nil)
	parsed, errors := tsoptions.GetParsedCommandLineOfConfigFile("/tsconfig.json", &core.CompilerOptions{}, nil, host, nil)
	assert.Equal(t, 0, len(errors))

	p := compiler.NewProgram(compiler.ProgramOptions{Config: parsed, Host: host})
	file := p.GetSourceFile("/index.ts")
	assert.Assert(t, file != nil)
	_ = p.GetDeclarationDiagnostics(t.Context(), file)

	assert.Equal(t, false, file.IsDeclarationFile)
	assert.Equal(t, file, file.ParseStore().SourceFile())
	assert.Equal(t, false, file.ParseStore().SourceFile().IsDeclarationFile)
}

func TestReactJsxDefaultExportUsesRuntimeImport(t *testing.T) {
	t.Parallel()
	if !bundled.Embedded {
		t.Skip("bundled files are not embedded")
	}

	fs := bundled.WrapFS(vfstest.FromMap(map[string]string{
		"/index.tsx": "export default <div/>;\n",
		"/tsconfig.json": `{
			"compilerOptions": {
				"jsx": "react-jsx",
				"module": "commonjs",
				"declaration": true,
				"skipLibCheck": true
			},
			"files": ["index.tsx"]
		}`,
	}, true))
	host := compiler.NewCompilerHost("/", fs, bundled.LibPath(), nil, nil, nil)
	parsed, errors := tsoptions.GetParsedCommandLineOfConfigFile("/tsconfig.json", &core.CompilerOptions{}, nil, host, nil)
	assert.Equal(t, 0, len(errors))

	p := compiler.NewProgram(compiler.ProgramOptions{Config: parsed, Host: host})
	out := &emitFiles{}
	_ = p.Emit(context.Background(), compiler.EmitOptions{
		WriteFile: out.write,
	})

	js, ok := out.m["/index.js"]
	assert.Assert(t, ok, "expected /index.js, got %v", out.m)
	assert.Assert(t, strings.Contains(js, "jsx-runtime"), "js should import jsx-runtime, got:\n%s", js)
	assert.Assert(t, strings.Contains(js, "jsx_runtime_1.jsx") || strings.Contains(js, "jsx_runtime_1[\"jsx\"]"), "js should call through the runtime import, got:\n%s", js)
	assert.Assert(t, !strings.Contains(js, "_jsx("), "js should not call the generated jsx binding directly, got:\n%s", js)

	dts, ok := out.m["/index.d.ts"]
	assert.Assert(t, ok, "expected /index.d.ts, got %v", out.m)
	assert.Assert(t, strings.Contains(dts, "declare const _default"), "dts should use _default, got:\n%s", dts)
	assert.Assert(t, !strings.Contains(dts, "_default_1"), "dts should not uniquify _default, got:\n%s", dts)
}

func TestCjsReexportHelperNameMatchesRequire(t *testing.T) {
	t.Parallel()
	if !bundled.Embedded {
		t.Skip("bundled files are not embedded")
	}

	fs := bundled.WrapFS(vfstest.FromMap(map[string]string{
		"/dog.ts":    "export function createDog() { return 1; }\n",
		"/index.ts":  "import { createDog } from './dog';\nexport { createDog };\n",
		"/tsconfig.json": `{
			"compilerOptions": {
				"module": "commonjs",
				"skipLibCheck": true
			},
			"files": ["index.ts", "dog.ts"]
		}`,
	}, true))
	host := compiler.NewCompilerHost("/", fs, bundled.LibPath(), nil, nil, nil)
	parsed, errors := tsoptions.GetParsedCommandLineOfConfigFile("/tsconfig.json", &core.CompilerOptions{}, nil, host, nil)
	assert.Equal(t, 0, len(errors))

	p := compiler.NewProgram(compiler.ProgramOptions{Config: parsed, Host: host})
	out := &emitFiles{}
	_ = p.Emit(context.Background(), compiler.EmitOptions{
		WriteFile: out.write,
	})

	js, ok := out.m["/index.js"]
	assert.Assert(t, ok, "expected /index.js, got %v", out.m)
	assert.Assert(t, strings.Contains(js, "const dog_1 = require(\"./dog\")"), "js should require dog_1, got:\n%s", js)
	assert.Assert(t, strings.Contains(js, "return dog_1.createDog"), "export getter should use dog_1, got:\n%s", js)
	assert.Assert(t, !strings.Contains(js, "dog_2"), "js should not mint a second dog helper, got:\n%s", js)
}

func TestJsTypedefCommentPreservedInDts(t *testing.T) {
	t.Parallel()
	if !bundled.Embedded {
		t.Skip("bundled files are not embedded")
	}

	fs := bundled.WrapFS(vfstest.FromMap(map[string]string{
		"/common.js": "/**\n * @template T, Name\n * @typedef {T & {x: Name}} Nominal\n */\nmodule.exports = {};\n",
		"/index.js":  "import { Nominal } from './common';\n\n/**\n * @typedef {Nominal<string, 'MyNominal'>} MyNominal\n */\n",
		"/tsconfig.json": `{
			"compilerOptions": {
				"allowJs": true,
				"checkJs": true,
				"declaration": true,
				"module": "commonjs",
				"skipLibCheck": true
			},
			"files": ["index.js", "common.js"]
		}`,
	}, true))
	host := compiler.NewCompilerHost("/", fs, bundled.LibPath(), nil, nil, nil)
	parsed, errors := tsoptions.GetParsedCommandLineOfConfigFile("/tsconfig.json", &core.CompilerOptions{}, nil, host, nil)
	assert.Equal(t, 0, len(errors))

	p := compiler.NewProgram(compiler.ProgramOptions{Config: parsed, Host: host})
	out := &emitFiles{}
	_ = p.Emit(context.Background(), compiler.EmitOptions{
		WriteFile: out.write,
	})

	dts, ok := out.m["/index.d.ts"]
	assert.Assert(t, ok, "expected /index.d.ts, got %v", out.m)
	assert.Assert(t, strings.Contains(dts, "export type MyNominal"), "dts should emit the typedef alias, got:\n%s", dts)
	assert.Assert(t, strings.Contains(dts, "@typedef"), "dts should keep the source @typedef comment, got:\n%s", dts)
}
