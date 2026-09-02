package ls

import (
	"context"
	"strings"
	"testing"

	"github.com/microsoft/TypeScript/tsc/internal/bundled"
	"github.com/microsoft/TypeScript/tsc/internal/compiler"
	"github.com/microsoft/TypeScript/tsc/internal/core"
	"github.com/microsoft/TypeScript/tsc/internal/ls/lsconv"
	"github.com/microsoft/TypeScript/tsc/internal/lsp/lsproto"
	"github.com/microsoft/TypeScript/tsc/internal/tsoptions"
	"github.com/microsoft/TypeScript/tsc/internal/vfs/vfstest"
	"gotest.tools/v3/assert"
)

func TestGetSymbolAtPositionAfterFreeze(t *testing.T) {
	t.Parallel()

	content := "const value = 1;\n"
	fs := vfstest.FromMap(map[string]string{
		"/repro.ts":      content,
		"/tsconfig.json": `{ "compilerOptions": {}, "files": ["repro.ts"] }`,
	}, false /*useCaseSensitiveFileNames*/)
	fs = bundled.WrapFS(fs)

	host := compiler.NewCompilerHost("/", fs, bundled.LibPath(), nil, nil, nil)
	parsed, errors := tsoptions.GetParsedCommandLineOfConfigFile("/tsconfig.json", &core.CompilerOptions{}, nil, host, nil)
	assert.Equal(t, len(errors), 0)
	program := compiler.NewProgram(compiler.ProgramOptions{Config: parsed, Host: host})
	program.BindSourceFiles()
	program.GetSemanticDiagnostics(context.Background(), program.GetSourceFile("/repro.ts"))

	converters := lsconv.NewConverters(lsproto.PositionEncodingKindUTF8, func(_ string) *lsconv.LSPLineMap {
		return lsconv.ComputeLSPLineStarts(content)
	})
	l := &LanguageService{program: program, converters: converters}

	offset := strings.Index(content, "value")
	sym, err := l.GetSymbolAtPosition(context.Background(), "/repro.ts", offset)
	assert.NilError(t, err)
	assert.Assert(t, sym != nil)

	sourceFile := program.GetSourceFile("/repro.ts")
	pos, _ := converters.ToLSPPosition(sourceFile, core.TextPos(offset))
	hover, err := l.ProvideHover(context.Background(), &lsproto.HoverParams{
		TextDocument: lsproto.TextDocumentIdentifier{Uri: "file:///repro.ts"},
		Position:     pos,
	})
	assert.NilError(t, err)
	assert.Assert(t, hover.Hover != nil)
}
