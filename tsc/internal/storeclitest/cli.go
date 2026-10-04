package storeclitest

import (
	"context"
	"errors"
	"io"
	"maps"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/microsoft/TypeScript/tsc/internal/bundled"
	"github.com/microsoft/TypeScript/tsc/internal/core"
	"github.com/microsoft/TypeScript/tsc/internal/execute"
	"github.com/microsoft/TypeScript/tsc/internal/execute/tsc"
	"github.com/microsoft/TypeScript/tsc/internal/tsoptions"
	"github.com/microsoft/TypeScript/tsc/internal/vfs"
	"github.com/microsoft/TypeScript/tsc/internal/vfs/vfstest"
)

// CLIResult is what one run of the CLI did: its output, its exit status, and
// the files it read and wrote.
type CLIResult struct {
	Output string
	Status tsc.ExitStatus
	Reads  []string
	Writes []string
	// Written is the last text written to each file.
	Written map[string]string
}

// RunCLI runs execute.CommandLine on a fresh in-memory FS of files (text or
// vfstest.Symlink) with the bundled libs, at cwd, with a clock that does not
// move (so that the times of --diagnostics are 0) and no TTY.
func RunCLI(files map[string]any, caseSensitive bool, cwd string, args []string) CLIResult {
	return runOn(files, caseSensitive, cwd, func(sys tsc.System) tsc.CommandLineResult {
		return execute.CommandLine(context.Background(), sys, args, nil)
	})
}

// RunCheck is execute.RunCheck of config on the same kind of system as
// RunCLI's.
func RunCheck(files map[string]any, caseSensitive bool, cwd string, config *tsoptions.ParsedCommandLine, backend core.ASTBackend) CLIResult {
	return runOn(files, caseSensitive, cwd, func(sys tsc.System) tsc.CommandLineResult {
		return execute.RunCheck(context.Background(), sys, config, backend, nil)
	})
}

func runOn(files map[string]any, caseSensitive bool, cwd string, run func(tsc.System) tsc.CommandLineResult) CLIResult {
	fs := &recordingFS{FS: bundled.WrapFS(vfstest.FromMap(files, caseSensitive))}
	sys := &cliSystem{fs: fs, cwd: cwd}
	result := run(sys)
	fs.mu.Lock()
	defer fs.mu.Unlock()
	return CLIResult{Output: sys.out.String(), Status: result.Status, Reads: slices.Clone(fs.reads), Writes: slices.Clone(fs.writes), Written: maps.Clone(fs.written)}
}

type cliSystem struct {
	fs  vfs.FS
	cwd string
	out strings.Builder
}

var _ tsc.System = (*cliSystem)(nil)

func (s *cliSystem) Writer() io.Writer                            { return &s.out }
func (s *cliSystem) ErrorWriter() io.Writer                       { return &s.out }
func (s *cliSystem) FS() vfs.FS                                   { return s.fs }
func (s *cliSystem) DefaultLibraryPath() string                   { return bundled.LibPath() }
func (s *cliSystem) GetCurrentDirectory() string                  { return s.cwd }
func (s *cliSystem) WriteOutputIsTTY() bool                       { return false }
func (s *cliSystem) GetWidthOfTerminal() int                      { return 0 }
func (s *cliSystem) GetEnvironmentVariable(string) (string, bool) { return "", false }
func (s *cliSystem) Now() time.Time                               { return time.Time{} }
func (s *cliSystem) SinceStart() time.Duration                    { return 0 }
func (s *cliSystem) Spawn([]string, string, io.Writer) (io.ReadWriteCloser, error) {
	return nil, errors.New("storeclitest: no process")
}

// recordingFS records the files read and every write, removal and time
// change.
type recordingFS struct {
	vfs.FS
	mu      sync.Mutex
	reads   []string
	writes  []string
	written map[string]string
}

func (f *recordingFS) note(list *[]string, path string) {
	f.mu.Lock()
	*list = append(*list, path)
	f.mu.Unlock()
}

func (f *recordingFS) ReadFile(path string) (string, bool) {
	f.note(&f.reads, path)
	return f.FS.ReadFile(path)
}

func (f *recordingFS) WriteFile(path string, data string) error {
	f.note(&f.writes, "write "+path)
	f.mu.Lock()
	if f.written == nil {
		f.written = map[string]string{}
	}
	f.written[path] = data
	f.mu.Unlock()
	return f.FS.WriteFile(path, data)
}

func (f *recordingFS) AppendFile(path string, data string) error {
	f.note(&f.writes, "append "+path)
	return f.FS.AppendFile(path, data)
}

func (f *recordingFS) Remove(path string) error {
	f.note(&f.writes, "remove "+path)
	return f.FS.Remove(path)
}

func (f *recordingFS) Chtimes(path string, aTime time.Time, mTime time.Time) error {
	f.note(&f.writes, "chtimes "+path)
	return f.FS.Chtimes(path, aTime, mTime)
}
