package main

import (
	"bytes"
	"fmt"
	"go/ast"
	"go/parser"
	"go/printer"
	"go/token"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

// The comparison of the binder.go trial with the 7c verbatim storebinder
// (store-binder-implementation-report-20260923.md 1: binder-verbatim.diff,
// Pointer -> verbatim, 64 hunks). The verbatim version is written against the
// 7c API, so its names are first mapped to the 7d-0 names by one table
// (names only: nothing is restructured). A hunk matches when every line it
// adds is a line of the trial's function (or functions) that the hunk falls
// in; a function matches when its lines are the trial's, in order. Lines are
// compared without comments and indentation.

// renames7c maps the 7c API names of the verbatim binder to 7d-0 names. Each
// entry is a name the 7d-0 instructions renamed (3.1, 3.5, 4); nothing else
// is rewritten before the comparison.
var renameTable = [][2]string{
	{`\.SetFlow\(`, `.SetFlowRef(`},
	{`\.SetSymbol\(`, `.SetSymbolId(`},
	{`\.SetLocalsSlot\(`, `.SetLocalsRef(`},
	{`\.LocalsSlot\(\)`, `.LocalsRef()`},
	{`\.SetLocalSymbol\(`, `.SetLocalSymbolId(`},
	{`\.SetEndFlowNode\(`, `.SetEndFlowRef(`},
	{`\.SetReturnFlowNode\(`, `.SetReturnFlowRef(`},
	{`\.SetFallthroughFlowNode\(`, `.SetFallthroughFlowRef(`},
	{`\.EndFlowNode\(\)`, `.EndFlowRef()`},
	{`\.ReturnFlowNode\(\)`, `.ReturnFlowRef()`},
	{`\.FallthroughFlowNode\(\)`, `.FallthroughFlowRef()`},
	{`\.LocalSymbol\(\)`, `.LocalSymbolId()`},
	{`\.FlowNode\(\)`, `.FlowRef()`},
	// Node.Symbol() was the id layer in 7c; File.Symbol() stays the symbol.
	{`([^e]|[^l]e|[^i]le|[^f]ile)\.Symbol\(\)`, `${1}.SymbolId()`},
	{`\bstore\.Ref\b`, `store.Node`},
	{`\.FileRef\(\)`, `.Key()`},
	{`\bstore\.FlowList\b`, `store.FlowListEntry`},
}

func renameVerbatim(src []byte) []byte {
	out := string(src)
	for _, r := range renameTable {
		out = regexp.MustCompile(r[0]).ReplaceAllString(out, r[1])
	}
	return []byte(out)
}

// applyUnifiedDiff applies a unified diff with one file to src.
func applyUnifiedDiff(src, diff []byte) ([]byte, error) {
	in := strings.SplitAfter(string(src), "\n")
	var out []string
	next := 0 // index in in
	hunkRe := regexp.MustCompile(`^@@ -(\d+)(?:,(\d+))? \+(\d+)(?:,(\d+))? @@`)
	lines := strings.SplitAfter(string(diff), "\n")
	for i := 0; i < len(lines); i++ {
		m := hunkRe.FindStringSubmatch(lines[i])
		if m == nil {
			continue
		}
		start, _ := strconv.Atoi(m[1])
		if start > 0 {
			start--
		}
		out = append(out, in[next:start]...)
		next = start
		for i+1 < len(lines) && !strings.HasPrefix(lines[i+1], "@@") && !strings.HasPrefix(lines[i+1], "--- ") {
			i++
			l := lines[i]
			if l == "" {
				continue
			}
			switch l[0] {
			case ' ':
				if next >= len(in) || in[next] != l[1:] && strings.TrimRight(in[next], "\n") != strings.TrimRight(l[1:], "\n") {
					return nil, fmt.Errorf("context mismatch at line %d", next+1)
				}
				out = append(out, in[next])
				next++
			case '-':
				next++
			case '+':
				out = append(out, l[1:])
			case '\\':
			}
		}
	}
	out = append(out, in[next:]...)
	return []byte(strings.Join(out, "")), nil
}

type hunk struct {
	header      string
	plusStart   int // first line in the verbatim file
	plusLen     int
	added       []string
	removed     []string
	category    string
	matched     bool
	missing     []string // added lines not found in the trial
	functions   []string
	designDelta bool // a line that differs because 7d-0 changed the design, not a name (D2, V1)
}

func parseHunks(diff []byte) []*hunk {
	hunkRe := regexp.MustCompile(`^@@ -(\d+)(?:,(\d+))? \+(\d+)(?:,(\d+))? @@`)
	var hunks []*hunk
	var h *hunk
	for _, l := range strings.Split(string(diff), "\n") {
		if m := hunkRe.FindStringSubmatch(l); m != nil {
			start, _ := strconv.Atoi(m[3])
			n := 1
			if m[4] != "" {
				n, _ = strconv.Atoi(m[4])
			}
			h = &hunk{header: l, plusStart: start, plusLen: n}
			hunks = append(hunks, h)
			continue
		}
		if h == nil || l == "" {
			continue
		}
		switch l[0] {
		case '+':
			h.added = append(h.added, l[1:])
		case '-':
			h.removed = append(h.removed, l[1:])
		}
	}
	return hunks
}

// categorize classifies a hunk by its main component, as the 7c report does:
// (d) the flow index, (b) the output target, (c) re-reading the children,
// else (a) the types.
func categorize(h *hunk) string {
	text := strings.Join(h.added, "\n")
	switch {
	case regexp.MustCompile(`FlowRef|FlowListRef|newFlowData|NewFlow|bound\.Flow\(|bound\.FlowList\(|bound\.FlowData\(`).MatchString(text):
		return "d"
	case regexp.MustCompile(`SetFlow|SetSymbol|AddFlags|ClearFlags|SetEndFlow|SetReturnFlow|SetFallthroughFlow|SetLocals|SetLocalSymbol|GetLocals\(b\.bound|symbolOf|b\.bound\.|FileRef\(\)|\.Key\(\)`).MatchString(text):
		return "b"
	case regexp.MustCompile(`\.Refs\(\)|\.At\(|b\.s\.Node\(|\(\)\.[A-Z]|\w\(\) ==|\w\(\)\)`).MatchString(text):
		return "c"
	}
	return "a"
}

// normLine is a line without its indentation and trailing comment; "" when
// nothing is left or only a brace or parenthesis.
func normLine(l string) string {
	l = strings.TrimSpace(l)
	if strings.HasPrefix(l, "//") || strings.HasPrefix(l, "/*") || strings.HasPrefix(l, "*") {
		return ""
	}
	if i := strings.Index(l, " //"); i >= 0 && !strings.Contains(l[:i], "\"") {
		l = strings.TrimSpace(l[:i])
	}
	switch l {
	case "", "{", "}", ")", "(", "})", "},", "} else {":
		return ""
	}
	return l
}

type funcLines struct {
	name  string
	from  int // lines in the file
	to    int
	lines []string
}

// functionsOf prints each declaration without comments and splits it into
// normalized lines.
func functionsOf(src []byte) ([]*funcLines, error) {
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "x.go", src, 0)
	if err != nil {
		return nil, err
	}
	var out []*funcLines
	for _, d := range file.Decls {
		name := declName(d)
		var buf bytes.Buffer
		if err := printer.Fprint(&buf, fset, d); err != nil {
			return nil, err
		}
		fl := &funcLines{name: name, from: fset.Position(d.Pos()).Line, to: fset.Position(d.End()).Line}
		for _, l := range strings.Split(buf.String(), "\n") {
			if n := normLine(l); n != "" {
				fl.lines = append(fl.lines, n)
			}
		}
		out = append(out, fl)
	}
	return out, nil
}

func declName(d ast.Decl) string {
	switch d := d.(type) {
	case *ast.FuncDecl:
		if d.Recv != nil && len(d.Recv.List) > 0 {
			var buf bytes.Buffer
			printer.Fprint(&buf, token.NewFileSet(), d.Recv.List[0].Type)
			return "(" + buf.String() + ")." + d.Name.Name
		}
		return d.Name.Name
	case *ast.GenDecl:
		for _, s := range d.Specs {
			switch s := s.(type) {
			case *ast.TypeSpec:
				return "type " + s.Name.Name
			case *ast.ValueSpec:
				return d.Tok.String() + " " + s.Names[0].Name
			}
		}
	}
	return "?"
}

// designDelta lines are the ones that differ because the design changed
// after 7c, beyond a name: the declarations are Nodes (D2: Ref, FileRef,
// b.s.Node(ref.Id())), the nil is the zero Node (Z1: nilNode()), and code
// outside parse and bind reads a list with Len and At, not Refs (K3, 7d-0
// instructions 3.3).
var designDeltaRe = regexp.MustCompile(`\.Id\(\)|b\.s\.Node\(|\.Key\(\)|store\.Node\{\}|SetValueDeclaration\(b\.s|SymbolName\(b\.s|IsStatic\(|nilNode\(\)|\.Refs\(\)|\[\]store\.NodeRef`)

func compareTrial(trial, verbatim, diff []byte, problems []problem) (string, error) {
	verbatim = renameVerbatim(verbatim)
	trialFuncs, err := functionsOf(trial)
	if err != nil {
		return "", fmt.Errorf("the trial does not parse: %w", err)
	}
	verbatimFuncs, err := functionsOf(verbatim)
	if err != nil {
		return "", fmt.Errorf("the verbatim binder does not parse: %w", err)
	}
	byName := map[string]*funcLines{}
	for _, f := range trialFuncs {
		byName[f.name] = f
	}
	all := map[string]bool{}
	for _, f := range trialFuncs {
		for _, l := range f.lines {
			all[l] = true
		}
	}
	hunks := parseHunks(renameVerbatim(diff))
	for _, h := range hunks {
		h.category = categorize(h)
		lines := map[string]bool{}
		for _, vf := range verbatimFuncs {
			if vf.from <= h.plusStart+h.plusLen-1 && h.plusStart <= vf.to {
				h.functions = append(h.functions, vf.name)
				if tf := byName[vf.name]; tf != nil {
					for _, l := range tf.lines {
						lines[l] = true
					}
				}
			}
		}
		if len(h.functions) == 0 {
			lines = all
		}
		for _, l := range h.added {
			n := normLine(l)
			if n == "" || strings.HasPrefix(n, "package ") {
				continue
			}
			if !lines[n] {
				h.missing = append(h.missing, n)
				if designDeltaRe.MatchString(n) {
					h.designDelta = true
				}
			}
		}
		h.matched = len(h.missing) == 0
	}
	var b strings.Builder
	fmt.Fprintf(&b, "binder.go trial against the 7c verbatim storebinder (names mapped by renameTable)\n\n")
	fmt.Fprintf(&b, "hunks by category (7c report: a 24, b 14, c 15, d 8, f 3; this classification is by keywords):\n")
	cats := []string{"a", "b", "c", "d"}
	totalMatched, total := 0, 0
	for _, c := range cats {
		n, m, dd := 0, 0, 0
		for _, h := range hunks {
			if h.category == c {
				n++
				if h.matched {
					m++
				} else if h.designDelta {
					dd++
				}
			}
		}
		total += n
		totalMatched += m
		fmt.Fprintf(&b, "  (%s) %2d hunks, %2d match (%.0f%%); of the rest, %d differ by a design change after 7c (D2, Z1, K3)\n", c, n, m, pct(m, n), dd)
	}
	fmt.Fprintf(&b, "  all %d hunks, %d match (%.0f%%)\n\n", total, totalMatched, pct(totalMatched, total))
	funcMatch, funcs := 0, 0
	var differing []string
	for _, vf := range verbatimFuncs {
		if strings.HasPrefix(vf.name, "type ") || strings.HasPrefix(vf.name, "var ") || strings.HasPrefix(vf.name, "const ") {
			continue
		}
		funcs++
		tf := byName[vf.name]
		if tf != nil && strings.Join(tf.lines, "\n") == strings.Join(vf.lines, "\n") {
			funcMatch++
		} else {
			differing = append(differing, vf.name)
		}
	}
	fmt.Fprintf(&b, "functions: %d of %d identical after the renames (%.0f%%)\n", funcMatch, funcs, pct(funcMatch, funcs))
	fmt.Fprintf(&b, "translation problems (constructs no rule covers): %d\n\n", len(problems))
	byMsg := map[string]int{}
	for _, p := range problems {
		byMsg[p.msg]++
	}
	var msgs []string
	for m, n := range byMsg {
		msgs = append(msgs, fmt.Sprintf("%4d  %s", n, m))
	}
	sort.Strings(msgs)
	for _, m := range msgs {
		fmt.Fprintln(&b, m)
	}
	fmt.Fprintf(&b, "\nhunks that do not match:\n")
	for _, h := range hunks {
		if h.matched {
			continue
		}
		fmt.Fprintf(&b, "\n(%s) %s in %s%s\n", h.category, h.header, strings.Join(h.functions, ", "), map[bool]string{true: " [design change]", false: ""}[h.designDelta])
		for _, l := range h.missing {
			fmt.Fprintf(&b, "    want: %s\n", l)
		}
	}
	return b.String(), nil
}

func pct(m, n int) float64 {
	if n == 0 {
		return 0
	}
	return 100 * float64(m) / float64(n)
}
