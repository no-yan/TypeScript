package storeclitest

import (
	"os"
	"runtime"
	"strings"
	"testing"

	"github.com/microsoft/TypeScript/tsc/internal/bundled"
	"github.com/microsoft/TypeScript/tsc/internal/compiler"
	"github.com/microsoft/TypeScript/tsc/internal/core"
	"github.com/microsoft/TypeScript/tsc/internal/storecompiler"
	"github.com/microsoft/TypeScript/tsc/internal/tsoptions"
	"github.com/microsoft/TypeScript/tsc/internal/tspath"
	"github.com/microsoft/TypeScript/tsc/internal/vfs"
	"github.com/microsoft/TypeScript/tsc/internal/vfs/osvfs"
)

// TestNativeGraphProject compares the graphs of a real project, one program
// at a time: STORE_CLI_GRAPH_CONFIG is the tsconfig, STORE_CLI_GRAPH_OUT the
// directory of pointer.txt and store.txt. Each program has its own FS and
// host, and the first is dropped before the second is built.
func TestNativeGraphProject(t *testing.T) {
	configFile, out := os.Getenv("STORE_CLI_GRAPH_CONFIG"), os.Getenv("STORE_CLI_GRAPH_OUT")
	if configFile == "" || out == "" {
		t.Skip("STORE_CLI_GRAPH_CONFIG and STORE_CLI_GRAPH_OUT are not set")
	}
	parse := func(fs vfs.FS) *tsoptions.ParsedCommandLine {
		config, diags := tsoptions.GetParsedCommandLineOfConfigFile(tspath.RootedFilePathFromAbsolute(configFile), &core.CompilerOptions{}, nil, fs, nil)
		if len(diags) != 0 {
			t.Fatalf("config diagnostics: %v", diags)
		}
		return config
	}
	graph := func(backend string) []string {
		fs := bundled.WrapFS(osvfs.FS())
		if backend == "store" {
			h := storecompiler.NewCachedFSCompilerHost(fs, bundled.LibPath(), nil, nil)
			return StoreGraph(storecompiler.NewProgram(storecompiler.ProgramOptions{Config: parse(h.FS()), Host: h, SingleThreaded: core.TSTrue}))
		}
		h := compiler.NewCachedFSCompilerHost(fs, bundled.LibPath(), nil, nil, nil)
		return PointerGraph(compiler.NewProgram(compiler.ProgramOptions{Config: parse(h.FS()), Host: h, SingleThreaded: core.TSTrue}))
	}
	graphs := map[string][]string{}
	for _, backend := range []string{"store", "pointer"} {
		lines := graph(backend)
		runtime.GC()
		if err := os.WriteFile(out+"/"+backend+".txt", []byte(strings.Join(lines, "\n")+"\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		graphs[backend] = lines
	}
	files := 0
	for _, l := range graphs["store"] {
		if strings.HasPrefix(l, "file ") {
			files++
		}
	}
	t.Logf("%d graph lines, %d files", len(graphs["store"]), files)
	if d := firstDifference(graphs["pointer"], graphs["store"]); d != "" {
		t.Errorf("the graphs differ at %s", d)
	}
}
