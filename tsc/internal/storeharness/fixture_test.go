package storeharness

import (
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"testing"

	"github.com/microsoft/TypeScript/tsc/internal/ast"
	"github.com/microsoft/TypeScript/tsc/internal/ast/store"
	"github.com/microsoft/TypeScript/tsc/internal/compiler"
	"github.com/microsoft/TypeScript/tsc/internal/testutil/harnessutil"
)

// The identity of the measurement program (store-checker-7d2c-implementation-
// instructions-20261002.md 3.3): the Store checker on a FixtureProgram records
// what it records on the H1 Program the fixture was recorded from. The
// recording run is the H1 run: the Recorder answers what the H1 Program
// answers. The fixture goes through its JSON, as between the processes.

// fixtureIdentity runs the driver on the H1 Program of pointer through a
// Recorder, then on the FixtureProgram of what it recorded, with the targets
// of the Pointer program on both, and compares the two runs. class is "" when
// the recording run panics: there is nothing to compare.
func fixtureIdentity(pointer *compiler.Program, diagFiles, walkFiles []*ast.SourceFile) (class string, detail *Detail, fixture *Fixture, err error) {
	h1 := newProgram(pointer)
	recorder := NewRecorder(h1)
	recorded := &run{}
	func() {
		defer func() {
			if v := recover(); v != nil {
				recorded.panicked = recovered(v)
			}
		}()
		runStoreOn(recorded, recorder, h1Stores(h1, diagFiles), h1Stores(h1, walkFiles))
	}()
	if recorded.panicked != nil {
		return "", &Detail{Where: "the recording run", Store: recorded.panicked.message + "\n" + recorded.panicked.stack}, nil, nil
	}
	if fixture, err = recorder.Fixture(); err != nil {
		return "", nil, nil, err
	}
	data, err := json.Marshal(fixture)
	if err != nil {
		return "", nil, nil, err
	}
	fixture = &Fixture{}
	if err := json.Unmarshal(data, fixture); err != nil {
		return "", nil, nil, err
	}
	program := NewFixtureProgram(fixture)
	replayed := &run{}
	func() {
		defer func() {
			if v := recover(); v != nil {
				replayed.panicked = recovered(v)
			}
		}()
		runStoreOn(replayed, program, fixtureStores(program, diagFiles), fixtureStores(program, walkFiles))
	}()
	class, detail = compareRecords(recorded, replayed)
	if class == ClassEqual && (recorded.order != replayed.order) {
		class, detail = "symbol order", &Detail{Where: fmt.Sprintf("%+v / %+v", recorded.order, replayed.order)}
	}
	return class, detail, fixture, nil
}

func h1Stores(p *Program, files []*ast.SourceFile) []*store.File {
	out := make([]*store.File, len(files))
	for i, f := range files {
		out[i] = p.Store(f)
	}
	return out
}

func fixtureStores(p *FixtureProgram, files []*ast.SourceFile) []*store.File {
	out := make([]*store.File, len(files))
	for i, f := range files {
		out[i] = p.File(f.Path())
	}
	return out
}

func TestFixtureIdentityCompilerProject(t *testing.T) {
	if testing.Short() {
		t.Skip("the compiler project")
	}
	pointer := newCompilerProjectProgram(t)
	diagFiles, walkFiles := projectTargets(pointer)
	class, detail, fixture, err := fixtureIdentity(pointer, diagFiles, walkFiles)
	if err != nil {
		t.Fatal(err)
	}
	if class != ClassEqual {
		t.Fatalf("class %q: %+v", class, detail)
	}
	t.Logf("files %d, modules %d, recorded questions %d", len(fixture.Files), len(fixture.Modules), fixture.questions())
}

// TestFixtureIdentityConfigurations is the identity on stage 2's cases, the
// selection of TestCheckerConfigurations (STORE_CHECKER_CONFIG_SAMPLE and the
// others). A case whose H1 run panics, or that the runner skips or rejects,
// is counted, not compared: stage 2 decides those.
func TestFixtureIdentityConfigurations(t *testing.T) {
	if testing.Short() {
		t.Skip("the configurations are not run with -short")
	}
	cases, _, _, err := enumerateConfigurations(runnerSuites)
	if err != nil {
		t.Fatal(err)
	}
	selected := selectCases(t, cases).selected
	type outcome struct{ status, detail string }
	outcomes := make([]outcome, len(selected))
	next := make(chan int)
	var wg sync.WaitGroup
	for range envInt(t, "STORE_CHECKER_CONFIG_WORKERS", 4) {
		wg.Go(func() {
			for i := range next {
				outcomes[i] = func() (o outcome) {
					defer func() {
						if v := recover(); v != nil {
							o = outcome{"harness-error", fmt.Sprint(v)}
						}
					}()
					c := selected[i]
					if c.Skipped {
						return outcome{status: "not-run"}
					}
					d, _ := prepareCase(c)
					if d == nil {
						return outcome{status: "not-run"}
					}
					if o := callWithT(func(t *testing.T) { harnessutil.SkipUnsupportedCompilerOptions(t, d.options) }); o.Kind != "" {
						return outcome{status: "not-run"}
					}
					pointer, release, panicked := tryBuild(d.build)
					if panicked != nil {
						return outcome{status: "not-run"}
					}
					defer release()
					diagFiles, walkFiles := d.targets(pointer)
					class, detail, _, err := fixtureIdentity(pointer, diagFiles, walkFiles)
					switch {
					case err != nil:
						return outcome{"harness-error", err.Error()}
					case class == "":
						message, _, _ := strings.Cut(detail.Store, "\n")
						return outcome{"h1-panics", message}
					case class != ClassEqual:
						return outcome{"different", fmt.Sprintf("%s: %+v", class, detail)}
					}
					return outcome{status: "equal"}
				}()
			}
		})
	}
	for i := range selected {
		next <- i
	}
	close(next)
	wg.Wait()
	counts := map[string]int{}
	for i, o := range outcomes {
		counts[o.status]++
		switch o.status {
		case "different", "harness-error":
			t.Errorf("%s: %s: %s", selected[i].ID, o.status, o.detail)
		case "h1-panics":
			t.Logf("%s: %s: %s", selected[i].ID, o.status, o.detail)
		}
	}
	t.Logf("%d cases: %v", len(selected), counts)
}

func (f *Fixture) questions() int {
	return len(f.SourceFile) + len(f.ForResolved) + len(f.FileExists) + len(f.MayBeEmitted) + len(f.EmitFormat) +
		len(f.ImpliedFormat) + len(f.EmitSyntax) + len(f.UsageMode) + len(f.DefaultMode) + len(f.Resolved) +
		len(f.FromSpecifier) + len(f.MetaData) + len(f.DefaultLibrary) + len(f.NilRedirect) + len(f.NilOutputDts) +
		len(f.NilFromSource) + len(f.PackageJson) + len(f.RedirectTarget) + len(f.SourceOfOutput) +
		len(f.NearestPackage) + len(f.JSXReference) + len(f.Scalars.Asked)
}
