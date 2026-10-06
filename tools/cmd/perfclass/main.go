// Command perfclass classifies the samples of
// store-checker-perf-analysis-instructions-20261005.md: stage 3a's xctrace
// samples by mechanism (5.2) and stage 3b's allocation stacks by category
// (6.1).
//
//	perfclass 3a -src tsc/internal -k 10 -samples out.jsonl -json out.json pointer=a.jsonl,b.jsonl store=c.jsonl,d.jsonl
//	perfclass 3b -json out.json pointer=pointer.traces store=store.traces
//
// 3a reads the JSONL of join.py, one sample a line: {"frames": [leaf first],
// "inst": n, "cycles": n, "clean": bool}. The interval is the samples with
// xctraceCheckPointer or xctraceCheckStore of storeclitest on their stack.
// 3b reads `go tool pprof -traces -unit=B -sample_index=...`.
package main

import (
	"bufio"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"math"
	"os"
	"sort"
	"strconv"
	"strings"
)

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, "usage: perfclass 3a|3b ...")
		os.Exit(2)
	}
	var err error
	switch os.Args[1] {
	case "3a":
		err = run3a(os.Args[2:], os.Stdout)
	case "3b":
		err = run3b(os.Args[2:], os.Stdout)
	default:
		err = fmt.Errorf("unknown mode %q", os.Args[1])
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "perfclass:", err)
		os.Exit(1)
	}
}

// sides parses side=path[,path] arguments in order.
func sides(args []string) ([]string, map[string][]string, error) {
	var order []string
	paths := map[string][]string{}
	for _, a := range args {
		side, list, ok := strings.Cut(a, "=")
		if !ok {
			return nil, nil, fmt.Errorf("argument %q is not side=path[,path]", a)
		}
		if _, dup := paths[side]; !dup {
			order = append(order, side)
		}
		paths[side] = append(paths[side], strings.Split(list, ",")...)
	}
	return order, paths, nil
}

var regionFuncs = map[string]bool{"xctraceCheckPointer": true, "xctraceCheckStore": true}

func isRegion(f fn) bool { return f.Pkg == "storeclitest" && f.Recv == "" && regionFuncs[f.Name] }

type joined struct {
	Frames []string `json:"frames"`
	Inst   float64  `json:"inst"`
	Cycles float64  `json:"cycles"`
	Clean  bool     `json:"clean"`
}

type cost struct{ Inst, Cycles float64 }

func (c *cost) add(inst, cycles float64) { c.Inst += inst; c.Cycles += cycles }

type funcRow struct {
	Mechanism string `json:"mechanism"`
	Function  string `json:"function"`
	Origin    string `json:"origin"`
	Cost      cost   `json:"cost"`
}

type side3a struct {
	Runs       int                 `json:"runs"`
	Samples    int                 `json:"samples"`
	Clean      int                 `json:"clean"`
	Total      cost                `json:"total"` // in the interval, per K per run
	Outside    cost                `json:"outside"`
	Mechanisms map[string]cost     `json:"mechanisms"`
	Functions  map[string]*funcRow `json:"functions"` // by mechanism + "\x00" + function
}

func run3a(args []string, w io.Writer) error {
	flags := flag.NewFlagSet("3a", flag.ContinueOnError)
	src := flags.String("src", "tsc/internal", "the tsc/internal directory")
	k := flags.Int("k", 10, "intervals per run")
	samplesOut := flags.String("samples", "", "write the classified samples (JSONL)")
	jsonOut := flags.String("json", "", "write the summary (JSON)")
	if err := flags.Parse(args); err != nil {
		return err
	}
	order, paths, err := sides(flags.Args())
	if err != nil {
		return err
	}
	s, err := loadSources(*src)
	if err != nil {
		return err
	}
	var samples *bufio.Writer
	if *samplesOut != "" {
		f, err := os.Create(*samplesOut)
		if err != nil {
			return err
		}
		defer f.Close()
		samples = bufio.NewWriter(f)
		defer samples.Flush()
	}
	result := map[string]*side3a{}
	for _, side := range order {
		r := &side3a{Runs: len(paths[side]), Mechanisms: map[string]cost{}, Functions: map[string]*funcRow{}}
		result[side] = r
		scale := 1 / float64(*k*r.Runs)
		for _, path := range paths[side] {
			err := readLines(path, func(line []byte) error {
				var j joined
				if err := json.Unmarshal(line, &j); err != nil {
					return err
				}
				c := s.classify3a(j.Frames, isRegion)
				if samples != nil {
					out, _ := json.Marshal(struct {
						Side string `json:"side"`
						Run  string `json:"run"`
						joined
						sample3a
					}{side, path, j, c})
					samples.Write(out)
					samples.WriteByte('\n')
				}
				inst, cycles := j.Inst*scale, j.Cycles*scale
				if c.Mechanism == mechOutside {
					r.Outside.add(inst, cycles)
					return nil
				}
				r.Samples++
				if j.Clean {
					r.Clean++
				}
				r.Total.add(inst, cycles)
				m := r.Mechanisms[c.Mechanism]
				m.add(inst, cycles)
				r.Mechanisms[c.Mechanism] = m
				key := c.Mechanism + "\x00" + c.Function
				row := r.Functions[key]
				if row == nil {
					row = &funcRow{Mechanism: c.Mechanism, Function: c.Function, Origin: c.Origin}
					r.Functions[key] = row
				}
				row.Cost.add(inst, cycles)
				return nil
			})
			if err != nil {
				return fmt.Errorf("%s: %w", path, err)
			}
		}
	}
	if *jsonOut != "" {
		if err := writeJSON(*jsonOut, result); err != nil {
			return err
		}
	}
	if len(order) == 2 {
		report3a(w, order[0], order[1], result[order[0]], result[order[1]])
	}
	return nil
}

func report3a(w io.Writer, pName, sName string, p, s *side3a) {
	const M = 1e6
	d := cost{s.Total.Inst - p.Total.Inst, s.Total.Cycles - p.Total.Cycles}
	fmt.Fprintf(w, "per interval: %s inst %.1fM cycles %.1fM (%d runs, %d samples, clean %.1f%%, outside inst %.1fM); %s inst %.1fM cycles %.1fM (%d runs, %d samples, clean %.1f%%, outside inst %.1fM); diff inst %+.1fM cycles %+.1fM\n\n",
		pName, p.Total.Inst/M, p.Total.Cycles/M, p.Runs, p.Samples, 100*float64(p.Clean)/float64(p.Samples), p.Outside.Inst/M,
		sName, s.Total.Inst/M, s.Total.Cycles/M, s.Runs, s.Samples, 100*float64(s.Clean)/float64(s.Samples), s.Outside.Inst/M,
		d.Inst/M, d.Cycles/M)
	fmt.Fprintf(w, "| mechanism | %s inst M | %s inst M | diff inst M | share | %s cyc M | %s cyc M | diff cyc M | share |\n|---|---:|---:|---:|---:|---:|---:|---:|---:|\n", pName, sName, pName, sName)
	for _, m := range mechanisms {
		a, b := p.Mechanisms[m], s.Mechanisms[m]
		fmt.Fprintf(w, "| %s | %.1f | %.1f | %+.1f | %s | %.1f | %.1f | %+.1f | %s |\n", m, a.Inst/M, b.Inst/M, (b.Inst-a.Inst)/M, share(b.Inst-a.Inst, d.Inst), a.Cycles/M, b.Cycles/M, (b.Cycles-a.Cycles)/M, share(b.Cycles-a.Cycles, d.Cycles))
	}
	// Functions by Key across the sides: a function may be in a different
	// mechanism on each side (an accessor inlined on one side only).
	type both struct {
		key, pMech, sMech, origin string
		p, s                      cost
	}
	rows := map[string]*both{}
	get := func(key string) *both {
		if rows[key] == nil {
			rows[key] = &both{key: key}
		}
		return rows[key]
	}
	for _, f := range p.Functions {
		b := get(f.Function)
		b.pMech, b.origin = f.Mechanism, f.Origin
		b.p.add(f.Cost.Inst, f.Cost.Cycles)
	}
	for _, f := range s.Functions {
		b := get(f.Function)
		b.sMech, b.origin = f.Mechanism, f.Origin
		b.s.add(f.Cost.Inst, f.Cost.Cycles)
	}
	list := make([]*both, 0, len(rows))
	for _, b := range rows {
		list = append(list, b)
	}
	for _, unit := range []string{"inst", "cycles"} {
		val := func(c cost) float64 {
			if unit == "inst" {
				return c.Inst
			}
			return c.Cycles
		}
		total := val(d)
		sort.Slice(list, func(i, j int) bool {
			di, dj := val(list[i].s)-val(list[i].p), val(list[j].s)-val(list[j].p)
			if di != dj {
				return di > dj
			}
			return list[i].key < list[j].key
		})
		fmt.Fprintf(w, "\n### functions by diff %s, down to 80%% of the diff (%+.1fM)\n\n| function | mechanism (%s / %s) | origin | %s M | %s M | diff M | cumulative |\n|---|---|---|---:|---:|---:|---:|\n", unit, total/M, pName, sName, pName, sName)
		cum := 0.0
		for _, b := range list {
			diff := val(b.s) - val(b.p)
			if diff <= 0 || cum >= 0.8*total {
				break
			}
			cum += diff
			fmt.Fprintf(w, "| `%s` | %s / %s | %s | %.1f | %.1f | %+.1f | %s |\n", b.key, b.pMech, b.sMech, b.origin, val(b.p)/M, val(b.s)/M, diff/M, share(cum, total))
		}
	}
	for _, m := range []string{mechSelf, mechRuntime, mechStdlib} {
		fmt.Fprintf(w, "\n### the last bins: %s, top 20 by %s inst\n\n| function | %s inst M | %s inst M | diff M |\n|---|---:|---:|---:|\n", m, sName, pName, sName)
		var in []*both
		for _, b := range list {
			if b.sMech == m || b.pMech == m {
				in = append(in, b)
			}
		}
		sort.Slice(in, func(i, j int) bool {
			return math.Max(in[i].s.Inst, in[i].p.Inst) > math.Max(in[j].s.Inst, in[j].p.Inst)
		})
		for i, b := range in {
			if i == 20 {
				break
			}
			fmt.Fprintf(w, "| `%s` | %.1f | %.1f | %+.1f |\n", b.key, b.p.Inst/M, b.s.Inst/M, (b.s.Inst-b.p.Inst)/M)
		}
	}
}

func share(x, total float64) string {
	if total == 0 {
		return "-"
	}
	return fmt.Sprintf("%.0f%%", 100*x/total)
}

func readLines(path string, f func([]byte) error) error {
	file, err := os.Open(path)
	if err != nil {
		return err
	}
	defer file.Close()
	sc := bufio.NewScanner(file)
	sc.Buffer(make([]byte, 1<<20), 1<<26)
	for sc.Scan() {
		if len(strings.TrimSpace(sc.Text())) == 0 {
			continue
		}
		if err := f(sc.Bytes()); err != nil {
			return err
		}
	}
	return sc.Err()
}

func writeJSON(path string, v any) error {
	b, err := json.MarshalIndent(v, "", " ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, b, 0o644)
}

// trace is one stack of pprof -traces with its value.
type trace struct {
	Frames []string
	Value  int64
	Labels []string
}

// parseTraces reads the output of `go tool pprof -traces -unit=B`.
func parseTraces(r io.Reader) ([]trace, error) {
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 1<<20), 1<<26)
	var out []trace
	var cur *trace
	for sc.Scan() {
		line := sc.Text()
		if strings.HasPrefix(line, "-----------+") {
			if cur != nil && len(cur.Frames) > 0 {
				out = append(out, *cur)
			}
			cur = &trace{}
			continue
		}
		if cur == nil {
			continue // the header
		}
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}
		if len(cur.Frames) == 0 {
			first, rest, _ := strings.Cut(trimmed, " ")
			if strings.HasSuffix(first, ":") {
				cur.Labels = append(cur.Labels, trimmed)
				continue
			}
			v, err := strconv.ParseInt(strings.TrimSuffix(first, "B"), 10, 64)
			if err != nil {
				return nil, fmt.Errorf("value %q: %w", line, err)
			}
			cur.Value = v
			cur.Frames = append(cur.Frames, strings.TrimSpace(rest))
			continue
		}
		cur.Frames = append(cur.Frames, trimmed)
	}
	if cur != nil && len(cur.Frames) > 0 {
		out = append(out, *cur)
	}
	return out, sc.Err()
}

type siteRow struct {
	site3b
	Bytes  int64 `json:"bytes"`
	Traces int   `json:"traces"`
}

type side3b struct {
	Total      int64            `json:"total"`
	Categories map[string]int64 `json:"categories"`
	Sites      []*siteRow       `json:"sites"` // by bytes, descending
}

func run3b(args []string, w io.Writer) error {
	flags := flag.NewFlagSet("3b", flag.ContinueOnError)
	jsonOut := flags.String("json", "", "write the summary (JSON)")
	top := flags.Int("top", 40, "sites to print per side")
	if err := flags.Parse(args); err != nil {
		return err
	}
	order, paths, err := sides(flags.Args())
	if err != nil {
		return err
	}
	result := map[string]*side3b{}
	for _, side := range order {
		r := &side3b{Categories: map[string]int64{}}
		result[side] = r
		bySite := map[site3b]*siteRow{}
		for _, path := range paths[side] {
			f, err := os.Open(path)
			if err != nil {
				return err
			}
			traces, err := parseTraces(f)
			f.Close()
			if err != nil {
				return fmt.Errorf("%s: %w", path, err)
			}
			for _, t := range traces {
				c := classify3b(t.Frames)
				r.Total += t.Value
				r.Categories[c.Category] += t.Value
				row := bySite[c]
				if row == nil {
					row = &siteRow{site3b: c}
					bySite[c] = row
				}
				row.Bytes += t.Value
				row.Traces++
			}
		}
		for _, row := range bySite {
			r.Sites = append(r.Sites, row)
		}
		sort.Slice(r.Sites, func(i, j int) bool {
			if r.Sites[i].Bytes != r.Sites[j].Bytes {
				return r.Sites[i].Bytes > r.Sites[j].Bytes
			}
			return r.Sites[i].Site < r.Sites[j].Site
		})
	}
	if *jsonOut != "" {
		if err := writeJSON(*jsonOut, result); err != nil {
			return err
		}
	}
	const MB = 1e6
	fmt.Fprintf(w, "| category |")
	for _, side := range order {
		fmt.Fprintf(w, " %s MB | share |", side)
	}
	fmt.Fprintf(w, "\n|---|")
	for range order {
		fmt.Fprintf(w, "---:|---:|")
	}
	fmt.Fprintln(w)
	for _, c := range append(categories, "total") {
		fmt.Fprintf(w, "| %s |", c)
		for _, side := range order {
			r := result[side]
			v := r.Categories[c]
			if c == "total" {
				v = r.Total
			}
			fmt.Fprintf(w, " %.2f | %s |", float64(v)/MB, share(float64(v), float64(r.Total)))
		}
		fmt.Fprintln(w)
	}
	for _, side := range order {
		r := result[side]
		fmt.Fprintf(w, "\n### %s: top %d sites\n\n| site | category | owner | MB | cumulative |\n|---|---|---|---:|---:|\n", side, *top)
		var cum int64
		for i, row := range r.Sites {
			if i == *top {
				break
			}
			cum += row.Bytes
			fmt.Fprintf(w, "| `%s` | %s | `%s` | %.2f | %s |\n", row.Site, row.Category, row.Owner, float64(row.Bytes)/MB, share(float64(cum), float64(r.Total)))
		}
	}
	return nil
}
