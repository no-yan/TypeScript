package storeclitest

import (
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/microsoft/TypeScript/tsc/internal/ast"
	"github.com/microsoft/TypeScript/tsc/internal/ast/store"
	"github.com/microsoft/TypeScript/tsc/internal/bundled"
	"github.com/microsoft/TypeScript/tsc/internal/checker"
	"github.com/microsoft/TypeScript/tsc/internal/compiler"
	"github.com/microsoft/TypeScript/tsc/internal/core"
	"github.com/microsoft/TypeScript/tsc/internal/execute"
	"github.com/microsoft/TypeScript/tsc/internal/execute/tsc"
	"github.com/microsoft/TypeScript/tsc/internal/repo"
	"github.com/microsoft/TypeScript/tsc/internal/storechecker"
	"github.com/microsoft/TypeScript/tsc/internal/storecompiler"
	"github.com/microsoft/TypeScript/tsc/internal/testutil/kperf"
	"github.com/microsoft/TypeScript/tsc/internal/tsoptions"
	"github.com/microsoft/TypeScript/tsc/internal/tspath"
	"github.com/microsoft/TypeScript/tsc/internal/vfs"
	"github.com/microsoft/TypeScript/tsc/internal/vfs/cachedvfs"
	"github.com/microsoft/TypeScript/tsc/internal/vfs/osvfs"
)

// The benchmarks of store-cli-design-20261004.md 7.2 on real projects.
//
// STORE_CLI_BENCH_INPUTS names the inputs: "name=tsconfig,name=tsconfig";
// the default is C01, the compiler fixture's semantic-check config.

type benchInput struct{ name, config string }

func benchInputs(tb testing.TB) []benchInput {
	spec := os.Getenv("STORE_CLI_BENCH_INPUTS")
	if spec == "" {
		return []benchInput{{"C01", filepath.Join(repo.TestDataPath(), "fixtures", "compiler", "tsconfig.store-cli-c01.json")}}
	}
	var out []benchInput
	for _, part := range strings.Split(spec, ",") {
		name, config, ok := strings.Cut(part, "=")
		if !ok {
			tb.Fatalf("STORE_CLI_BENCH_INPUTS: %q", part)
		}
		out = append(out, benchInput{name, config})
	}
	return out
}

var backends = []core.ASTBackend{core.ASTBackendPointer, core.ASTBackendStore}

func backendName(b core.ASTBackend) string {
	if b == core.ASTBackendStore {
		return "store"
	}
	return "pointer"
}

// osSystem is the CLI's system on the real file system with a fresh cache,
// the bundled libs, and the output discarded.
type osSystem struct {
	fs  vfs.FS
	cwd string
	out io.Writer
}

func newOSSystem(cwd string) *osSystem {
	return &osSystem{fs: bundled.WrapFS(cachedvfs.From(osvfs.FS())), cwd: cwd, out: io.Discard}
}

var start = time.Now()

func (s *osSystem) Writer() io.Writer                              { return s.out }
func (s *osSystem) ErrorWriter() io.Writer                         { return s.out }
func (s *osSystem) FS() vfs.FS                                     { return s.fs }
func (s *osSystem) DefaultLibraryPath() tspath.RootedDirectoryPath { return bundled.LibPath() }
func (s *osSystem) GetCurrentDirectory() tspath.RootedDirectoryPath {
	return tspath.RootedDirectoryPathFromAbsolute(s.cwd)
}
func (s *osSystem) WriteOutputIsTTY() bool                       { return false }
func (s *osSystem) GetWidthOfTerminal() int                      { return 0 }
func (s *osSystem) GetEnvironmentVariable(string) (string, bool) { return "", false }
func (s *osSystem) Now() time.Time                               { return time.Now() }
func (s *osSystem) SinceStart() time.Duration                    { return time.Since(start) }
func (s *osSystem) Spawn([]string, string, io.Writer) (io.ReadWriteCloser, error) {
	return nil, errors.New("storeclitest: no process")
}

// nativeProgram is one backend's program of a config: the config is read and
// the program built with a host of its own each time.
type nativeProgram struct {
	pointer *compiler.Program
	store   *storecompiler.Program
}

func parseConfig(tb testing.TB, fs vfs.FS, configFile string) *tsoptions.ParsedCommandLine {
	config, diags := tsoptions.GetParsedCommandLineOfConfigFile(tspath.RootedFilePathFromAbsolute(configFile), &core.CompilerOptions{}, nil, fs, &tsc.ExtendedConfigCache{})
	if len(diags) != 0 {
		tb.Fatalf("%s: %d config diagnostics", configFile, len(diags))
	}
	return config
}

func buildProgram(tb testing.TB, backend core.ASTBackend, configFile string) nativeProgram {
	cwd := tspath.GetDirectoryPath(tspath.NormalizePath(configFile))
	sys := newOSSystem(cwd)
	if backend == core.ASTBackendStore {
		host := storecompiler.NewCachedFSCompilerHost(sys.FS(), sys.DefaultLibraryPath(), nil, nil)
		return nativeProgram{store: storecompiler.NewProgram(storecompiler.ProgramOptions{Config: parseConfig(tb, host.FS(), configFile), Host: host})}
	}
	host := compiler.NewCachedFSCompilerHost(sys.FS(), sys.DefaultLibraryPath(), nil, nil, nil)
	return nativeProgram{pointer: compiler.NewProgram(compiler.ProgramOptions{Config: parseConfig(tb, host.FS(), configFile), Host: host})}
}

// BenchmarkNativeCheckKPC is the old Check (storeharness/measure_test.go
// checkPointer and checkStore) on the native programs: outside the interval
// the config, the program, the bind of every file and the targets (the
// files but the default libs); inside it NewChecker, GetDiagnostics of each
// target in order and GetGlobalDiagnostics. Each iteration has a new program
// and checker; the GC is off (kperf.Session). Run with -benchtime=1x in a
// process of its own for each sample.
func BenchmarkNativeCheckKPC(b *testing.B) {
	for _, input := range benchInputs(b) {
		for _, backend := range backends {
			b.Run(input.name+"/backend="+backendName(backend), func(b *testing.B) {
				session := kperf.Start(b)
				defer session.Stop()
				b.ReportAllocs()
				b.ResetTimer()
				diagnostics := 0
				for range b.N {
					b.StopTimer()
					p := buildProgram(b, backend, input.config)
					ctx := context.Background()
					if backend == core.ASTBackendStore {
						p.store.BindSourceFiles()
						var targets []*store.File
						for _, f := range p.store.SourceFiles() {
							if !p.store.IsSourceFileDefaultLibrary(f.PathKey()) {
								targets = append(targets, f)
							}
						}
						var ds [][]*store.Diagnostic
						b.StartTimer()
						session.Measure(func() {
							c, _ := storechecker.NewChecker(p.store, nil)
							for _, f := range targets {
								ds = append(ds, c.GetDiagnostics(ctx, f))
							}
							ds = append(ds, c.GetGlobalDiagnostics())
						})
						b.StopTimer()
						diagnostics = countAll(ds)
					} else {
						p.pointer.BindSourceFiles()
						var targets []*ast.SourceFile
						for _, f := range p.pointer.SourceFiles() {
							if !p.pointer.IsSourceFileDefaultLibrary(f.PathKey()) {
								targets = append(targets, f)
							}
						}
						var ds [][]*ast.Diagnostic
						b.StartTimer()
						session.Measure(func() {
							c, _ := checker.NewChecker(p.pointer, nil)
							for _, f := range targets {
								ds = append(ds, c.GetDiagnostics(ctx, f))
							}
							ds = append(ds, c.GetGlobalDiagnostics())
						})
						b.StopTimer()
						diagnostics = countAll(ds)
					}
				}
				session.Report()
				b.ReportMetric(float64(diagnostics), "diagnostics")
			})
		}
	}
}

func countAll[D any](ds [][]D) int {
	n := 0
	for _, d := range ds {
		n += len(d)
	}
	return n
}

// BenchmarkNativeCommandLine is execute.CommandLine of the check mode, each
// iteration with a new system and file system cache: no process start, the
// output discarded. Its allocations are the compiler's.
func BenchmarkNativeCommandLine(b *testing.B) {
	for _, input := range benchInputs(b) {
		for _, backend := range backends {
			b.Run(input.name+"/backend="+backendName(backend), func(b *testing.B) {
				cwd := tspath.GetDirectoryPath(tspath.NormalizePath(input.config))
				args := []string{"--noEmit", "--singleThreaded", "--pretty", "false", "--astBackend", backendName(backend), "-p", input.config}
				b.ReportAllocs()
				for range b.N {
					result := execute.CommandLine(context.Background(), newOSSystem(cwd), args, nil)
					if result.Status != tsc.ExitStatusSuccess && result.Status != tsc.ExitStatusDiagnosticsPresent_OutputsGenerated {
						b.Fatalf("status %d", result.Status)
					}
				}
			})
		}
	}
}

// BenchmarkNativeProgram times the stages of the check through the program
// apart: build (config to graph: I/O, parse, resolution, include), bind (every
// file), check (the program, global and semantic diagnostics and the global
// ones again, with the checker made in it: the collector's stages after the
// bind, the program's skip, directive and filter included). The other stages
// run with the timer stopped.
func BenchmarkNativeProgram(b *testing.B) {
	for _, input := range benchInputs(b) {
		for _, backend := range backends {
			for _, stage := range []string{"build", "bind", "check"} {
				b.Run(input.name+"/backend="+backendName(backend)+"/stage="+stage, func(b *testing.B) {
					ctx := context.Background()
					b.ReportAllocs()
					b.ResetTimer()
					for range b.N {
						b.StopTimer()
						if stage == "build" {
							b.StartTimer()
						}
						p := buildProgram(b, backend, input.config)
						b.StopTimer()
						if backend == core.ASTBackendStore {
							stages := execute.StoreCheckStages(p.store)
							runStages(b, ctx, stage, stages.Bind, func() int {
								return len(stages.Program()) + len(stages.Global(ctx)) + len(stages.Semantic(ctx)) + len(stages.Global(ctx))
							})
						} else {
							stages := execute.PointerCheckStages(p.pointer)
							runStages(b, ctx, stage, stages.Bind, func() int {
								return len(stages.Program()) + len(stages.Global(ctx)) + len(stages.Semantic(ctx)) + len(stages.Global(ctx))
							})
						}
					}
				})
			}
		}
	}
}

func runStages(b *testing.B, ctx context.Context, stage string, bind func(context.Context), check func() int) {
	if stage == "bind" {
		b.StartTimer()
	}
	bind(ctx)
	b.StopTimer()
	if stage == "check" {
		b.StartTimer()
		check()
		b.StopTimer()
	}
}
