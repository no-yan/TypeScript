package storeharness

import (
	"bufio"
	"cmp"
	"context"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/microsoft/TypeScript/tsc/internal/core"
	"github.com/microsoft/TypeScript/tsc/internal/repo"
	"github.com/microsoft/TypeScript/tsc/internal/testrunner"
	"github.com/microsoft/TypeScript/tsc/internal/tspath"
	"golang.org/x/sync/semaphore"
)

// The corpus of 2a (7.1): storebinder/binder_test.go's corpus, copied, with
// where each file comes from.

type corpusFile struct {
	name, text string
	origin     string // the file under testdata, and the unit of a test
}

func corpusUnder(tb testing.TB, testCases bool) []corpusFile {
	tb.Helper()
	var supported []string
	for _, es := range tspath.AllSupportedExtensionsWithJson {
		supported = append(supported, es...)
	}
	var files []corpusFile
	add := func(origin, name, text string) {
		if ext := tspath.TryGetExtensionFromPath(name); slices.Contains(supported, ext) {
			files = append(files, corpusFile{"/index" + ext, text, origin})
		}
	}
	each := func(root string, visit func(path string, text string)) bool {
		if _, err := os.Stat(root); err != nil {
			return false
		}
		err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() || tspath.TryGetExtensionFromPath(path) == "" {
				return nil
			}
			text, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			visit(path, string(text))
			return nil
		})
		if err != nil {
			tb.Fatal(err)
		}
		return true
	}
	relative := func(path string) string {
		rel, err := filepath.Rel(repo.TestDataPath(), path)
		if err != nil {
			return path
		}
		return filepath.ToSlash(rel)
	}
	if !each(filepath.Join(repo.TestDataPath(), "fixtures"), func(path, text string) { add(relative(path), path, text) }) {
		tb.Fatal("testdata/fixtures is missing")
	}
	if !testCases {
		return files
	}
	for _, dir := range []string{"tests/cases/compiler", "tests/cases/conformance"} {
		found := each(filepath.Join(repo.TestDataPath(), dir), func(path, text string) {
			units, _, _, _, err := testrunner.ParseTestFilesAndSymlinks(text, path,
				func(name, content string, _ map[string]string) (corpusFile, error) {
					return corpusFile{name: name, text: content}, nil
				})
			if err != nil {
				tb.Fatal(err)
			}
			for _, unit := range units {
				add(relative(path)+"#"+unit.name, unit.name, unit.text)
			}
		})
		if !found {
			tb.Logf("skipped: testdata/%s is missing", dir)
		}
	}
	return files
}

// stage1Options are the options of stage 1 (5.1): the defaults, with the JS
// files in the program and checked.
func stage1Options() *core.CompilerOptions {
	return &core.CompilerOptions{AllowJs: core.TSTrue, CheckJs: core.TSTrue}
}

// envInt is a positive integer from the environment, or def.
func envInt(tb testing.TB, name string, def int) int {
	v := os.Getenv(name)
	if v == "" {
		return def
	}
	n, err := strconv.Atoi(v)
	if err != nil || n <= 0 {
		tb.Fatalf("%s=%q is not a positive integer", name, v)
	}
	return n
}

type corpusResult struct {
	Result
	file    corpusFile
	elapsed time.Duration
	harness string // a panic of the harness itself
}

// TestCheckerCorpus runs the driver on every file of the corpus as a program
// of its own (7.1) and classifies each by its first difference (7.2). It
// reports: a difference does not fail it (the strict mode is 2b-2's).
//
//	STORE_CHECKER_CORPUS_SAMPLE=n   n files of the corpus, at a fixed interval
//	STORE_CHECKER_CORPUS_WORKERS=n  the workers (4)
//	STORE_CHECKER_CORPUS_OUT=path   a JSONL line for each file
//	STORE_CHECKER_CORPUS_TRACE=path each file as it starts and ends
//	STORE_CHECKER_CORPUS_FILTER=s   the files whose origin contains s
func TestCheckerCorpus(t *testing.T) {
	if testing.Short() {
		t.Skip("the checker corpus is not run with -short")
	}
	files := corpusUnder(t, true)
	total := len(files)
	if n := envInt(t, "STORE_CHECKER_CORPUS_SAMPLE", 0); n > 0 && n < len(files) {
		sampled := make([]corpusFile, 0, n)
		for i := range n {
			sampled = append(sampled, files[i*len(files)/n])
		}
		files = sampled
	}
	if filter := os.Getenv("STORE_CHECKER_CORPUS_FILTER"); filter != "" {
		files = slices.DeleteFunc(files, func(f corpusFile) bool { return !strings.Contains(f.origin, filter) })
	}
	workers := envInt(t, "STORE_CHECKER_CORPUS_WORKERS", 4)

	results := make([]corpusResult, len(files))
	var (
		mu      sync.Mutex
		running = map[int]time.Time{}
	)
	stopWatch := make(chan struct{})
	go func() {
		// The files that run long: a hang shows where it is.
		ticker := time.NewTicker(time.Minute)
		defer ticker.Stop()
		for {
			select {
			case <-stopWatch:
				return
			case now := <-ticker.C:
				mu.Lock()
				for i, start := range running {
					if d := now.Sub(start); d > time.Minute {
						t.Logf("running for %v: %s", d.Round(time.Second), files[i].origin)
					}
				}
				mu.Unlock()
			}
		}
	}()
	// STORE_CHECKER_CORPUS_TRACE=path writes each file as it starts and ends,
	// unbuffered, so that a run that dies shows the files it was on.
	var trace *os.File
	if path := os.Getenv("STORE_CHECKER_CORPUS_TRACE"); path != "" {
		var err error
		if trace, err = os.Create(path); err != nil {
			t.Fatal(err)
		}
		defer trace.Close()
	}
	traceLine := func(format string, args ...any) {
		if trace != nil {
			mu.Lock()
			fmt.Fprintf(trace, format, args...)
			mu.Unlock()
		}
	}
	// A file of 1MB or more (the compiler's checker.ts, dom.generated.d.ts)
	// takes 2GB alone: it runs with no other file, so that the run fits the
	// machine's 8GB.
	budget := semaphore.NewWeighted(int64(workers))
	weight := func(f corpusFile) int64 {
		if len(f.text) >= 1<<20 {
			return int64(workers)
		}
		return 1
	}
	start := time.Now()
	next := make(chan int)
	var wg sync.WaitGroup
	for range workers {
		wg.Go(func() {
			for i := range next {
				w := weight(files[i])
				if err := budget.Acquire(context.Background(), w); err != nil {
					panic(err)
				}
				traceLine("start %d %s\n", i, files[i].origin)
				mu.Lock()
				running[i] = time.Now()
				mu.Unlock()
				results[i] = compareCorpusFile(files[i])
				mu.Lock()
				delete(running, i)
				mu.Unlock()
				traceLine("end %d\n", i)
				budget.Release(w)
			}
		})
	}
	for i := range files {
		next <- i
	}
	close(next)
	wg.Wait()
	close(stopWatch)
	wall := time.Since(start)

	var rusage syscall.Rusage
	_ = syscall.Getrusage(syscall.RUSAGE_SELF, &rusage)
	t.Logf("%d of %d files, %d workers: wall %v, %.1f ms a file of wall, max RSS %.2f GB",
		len(files), total, workers, wall.Round(time.Second), float64(wall.Milliseconds())/float64(len(files)), float64(maxRSSBytes(rusage))/(1<<30))
	var perFile []time.Duration
	for _, r := range results {
		perFile = append(perFile, r.elapsed)
	}
	slices.Sort(perFile)
	if len(perFile) > 0 {
		t.Logf("time of a file: median %v, p99 %v, max %v", perFile[len(perFile)/2].Round(time.Millisecond), perFile[len(perFile)*99/100].Round(time.Millisecond), perFile[len(perFile)-1].Round(time.Millisecond))
	}

	logCorpusResults(t, results)
	if out := os.Getenv("STORE_CHECKER_CORPUS_OUT"); out != "" {
		if err := writeCorpusJSONL(out, results); err != nil {
			t.Error(err)
		}
	}
}

// compareCorpusFile runs Compare; a panic out of it is the harness's own.
func compareCorpusFile(f corpusFile) (r corpusResult) {
	r.file = f
	start := time.Now()
	defer func() {
		r.elapsed = time.Since(start)
		if v := recover(); v != nil {
			r.harness = fmt.Sprint(v)
		}
	}()
	r.Result = Compare(map[string]string{f.name: f.text}, stage1Options())
	return r
}

// maxRSSBytes is ru_maxrss in bytes: it is in bytes on darwin, in
// kilobytes elsewhere.
func maxRSSBytes(r syscall.Rusage) int64 {
	if runtime.GOOS == "darwin" {
		return r.Maxrss
	}
	return r.Maxrss * 1024
}

func logCorpusResults(t *testing.T, results []corpusResult) {
	t.Helper()
	type bucket struct {
		key      string
		count    int
		examples []corpusResult
	}
	classes := map[string]*bucket{}
	excluded := map[string]*bucket{}
	var harness []corpusResult
	compared := 0
	for _, r := range results {
		if r.harness != "" {
			harness = append(harness, r)
			continue
		}
		table, key := classes, r.Class
		if r.Excluded != "" {
			table, key = excluded, r.Excluded
		} else {
			compared++
		}
		b := table[key]
		if b == nil {
			b = &bucket{key: key}
			table[key] = b
		}
		b.count++
		if len(b.examples) < 3 {
			b.examples = append(b.examples, r)
		}
	}
	sorted := func(m map[string]*bucket) []*bucket {
		var out []*bucket
		for _, b := range m {
			out = append(out, b)
		}
		slices.SortFunc(out, func(a, b *bucket) int {
			return cmp.Or(cmp.Compare(b.count, a.count), cmp.Compare(a.key, b.key))
		})
		return out
	}
	for _, reason := range []string{ExcludedMissingRoot, ExcludedPointerPanics} {
		if b := excluded[reason]; b != nil {
			t.Logf("excluded (%s): %d, first %s", reason, b.count, b.examples[0].file.origin)
		} else {
			t.Logf("excluded (%s): 0", reason)
		}
	}
	var equal int
	if b := classes[ClassEqual]; b != nil {
		equal = b.count
	}
	t.Logf("compared %d, equal %d (%.1f%%), classes %d", compared, equal, 100*float64(equal)/float64(max(compared, 1)), len(classes))
	for i, b := range sorted(classes) {
		t.Logf("%6d  %s", b.count, b.key)
		if i >= 20 || b.key == ClassEqual {
			continue
		}
		for _, r := range b.examples {
			if r.Detail != nil {
				t.Logf("          %s: %s\n            Pointer: %s\n            Store:   %s", r.file.origin, r.Detail.Where, clip(r.Detail.Pointer), clip(r.Detail.Store))
			}
		}
	}
	for _, r := range harness {
		t.Errorf("harness error on %s: %s", r.file.origin, r.harness)
	}
}

func clip(s string) string {
	if len(s) > 400 {
		return s[:400] + "…"
	}
	return s
}

func writeCorpusJSONL(path string, results []corpusResult) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	w := bufio.NewWriter(f)
	enc := json.NewEncoder(w)
	for _, r := range results {
		line := struct {
			File     string  `json:"file"`
			Class    string  `json:"class,omitempty"`
			Excluded string  `json:"excluded,omitempty"`
			Harness  string  `json:"harness,omitempty"`
			Detail   *Detail `json:"detail,omitempty"`
			Millis   int64   `json:"ms"`
		}{r.file.origin, r.Class, r.Excluded, r.harness, r.Detail, r.elapsed.Milliseconds()}
		if err := enc.Encode(line); err != nil {
			f.Close()
			return err
		}
	}
	if err := w.Flush(); err != nil {
		f.Close()
		return err
	}
	return f.Close()
}
