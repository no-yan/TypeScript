package storeprinter

import (
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/microsoft/TypeScript/tsc/internal/ast"
	"github.com/microsoft/TypeScript/tsc/internal/ast/store"
	"github.com/microsoft/TypeScript/tsc/internal/ast/store/storetest"
	"github.com/microsoft/TypeScript/tsc/internal/core"
	"github.com/microsoft/TypeScript/tsc/internal/parser"
	"github.com/microsoft/TypeScript/tsc/internal/printer"
	"github.com/microsoft/TypeScript/tsc/internal/repo"
	"github.com/microsoft/TypeScript/tsc/internal/storeparser"
	"github.com/microsoft/TypeScript/tsc/internal/testrunner"
	"github.com/microsoft/TypeScript/tsc/internal/tspath"
)

// The unit equivalence of the printer
// (internal/ast/docs/store-checker-7d2a-implementation-instructions-20260929.md
// 6.3): every corpus file parsed by the Pointer parser and printed by package
// printer, and parsed by the Store parser and printed by this package, with
// the five printer settings of checker/printer.go, must give the same text:
// the file's root; every statement, type node and entity name, with the
// source file and with nil; and a deep clone of every type node and entity
// name by the printing EmitContext's Factory, with the file and nil. The Store
// file and the synthetic nodes are in one program (IDSpace), as the checker's.

// The settings of checker/printer.go (createPrinterWithDefaults, ...).
type setting struct {
	name string
	p    printer.PrinterOptions
	s    PrinterOptions
}

var settings = []setting{
	{"Defaults", printer.PrinterOptions{}, PrinterOptions{}},
	{"RemoveComments", printer.PrinterOptions{RemoveComments: true}, PrinterOptions{RemoveComments: true}},
	{"RemoveCommentsOmitTrailingSemicolon", printer.PrinterOptions{RemoveComments: true, OmitTrailingSemicolon: true}, PrinterOptions{RemoveComments: true, OmitTrailingSemicolon: true}},
	{"RemoveCommentsOmitTrailingSemicolonNeverAsciiEscape", printer.PrinterOptions{RemoveComments: true, OmitTrailingSemicolon: true, NeverAsciiEscape: true}, PrinterOptions{RemoveComments: true, OmitTrailingSemicolon: true, NeverAsciiEscape: true}},
	// No caller in the checker; a setting it defines.
	{"RemoveCommentsNeverAsciiEscape", printer.PrinterOptions{RemoveComments: true, NeverAsciiEscape: true}, PrinterOptions{RemoveComments: true, NeverAsciiEscape: true}},
}

// printPointer prints node with the Pointer printer, as typeToStringEx does:
// a text writer with the checker's new line, no source map.
func printPointer(opts printer.PrinterOptions, ctx *printer.EmitContext, node *ast.Node, file *ast.SourceFile) (text string, panicked bool) {
	defer func() {
		if recover() != nil {
			text, panicked = "", true
		}
	}()
	w := printer.NewTextWriter("\n", 0)
	printer.NewPrinter(opts, printer.PrintHandlers{}, ctx).Write(node, file, w, nil)
	return w.String(), false
}

func printStore(opts PrinterOptions, ctx *EmitContext, node store.Node, file *store.File) (text string, panicked bool) {
	defer func() {
		if recover() != nil {
			text, panicked = "", true
		}
	}()
	w := NewTextWriter("\n", 0)
	NewPrinter(opts, PrintHandlers{}, ctx).Write(node, file, w, nil)
	return w.String(), false
}

// cell counts the prints of one setting, source file (with or nil) and kind.
type cell struct {
	prints, mismatches, bothPanicked, onePanicked int
	fileNilDiffer                                 int // the Pointer text differs between the file and nil
	length                                        int // the Pointer text's length, summed
	example                                       string
}

type printStats struct {
	cells map[string]*cell
	files int
}

func (st *printStats) cell(setting, file, kind string) *cell {
	key := setting + " | " + file + " | " + kind
	c := st.cells[key]
	if c == nil {
		c = &cell{}
		st.cells[key] = c
	}
	return c
}

// compare prints the pair with one setting, with the file and with nil (when
// withNil), and counts.
func (st *printStats) compare(s setting, kind string, pctx *printer.EmitContext, sctx *EmitContext, p *ast.Node, pf *ast.SourceFile, n store.Node, sf *store.File, withNil bool, where string) {
	var withFile string
	for _, file := range []bool{true, false} {
		if !file && !withNil {
			continue
		}
		label := "file"
		var pfile *ast.SourceFile
		var sfile *store.File
		if file {
			pfile, sfile = pf, sf
		} else {
			label = "nil"
		}
		c := st.cell(s.name, label, kind)
		c.prints++
		pt, pp := printPointer(s.p, pctx, p, pfile)
		stext, sp := printStore(s.s, sctx, n, sfile)
		switch {
		case pp && sp:
			c.bothPanicked++
		case pp != sp:
			c.onePanicked++
			c.mismatches++
			if c.example == "" {
				c.example = fmt.Sprintf("%s: Pointer panicked %v, Store panicked %v", where, pp, sp)
			}
		case pt != stext:
			c.mismatches++
			if c.example == "" {
				c.example = fmt.Sprintf("%s: %q, want %q", where, stext, pt)
			}
		}
		c.length += len(pt)
		if file {
			withFile = pt
		} else if pt != withFile {
			c.fileNilDiffer++
		}
	}
}

// printFile compares one source file.
func (st *printStats) printFile(t *testing.T, name, text string, where string) {
	opts, source, kind := sourceInput(name, text)
	pf := parser.ParseSourceFile(opts, source, kind)
	space := store.NewIDSpace(1)
	sf := storeparser.ParseSourceFileIn(space, 0, opts, source, kind)
	pairs, _ := storetest.Pairs(pf, sf.Store)
	if pairs == nil {
		t.Errorf("%s: parse mismatch", where)
		return
	}
	st.files++
	region := store.NewNodeRegion(space)
	pctx := printer.NewEmitContext()
	sctx := NewEmitContextIn(region)
	// The clones, once per file: the EmitContext of the print makes them.
	type clonePair struct {
		kind string
		p    *ast.Node
		n    store.Node
	}
	var clones []clonePair
	for _, pair := range pairs {
		kind := pairKind(pair.P)
		if kind == "type" || kind == "entity name" {
			clones = append(clones, clonePair{"clone of " + kind, pctx.Factory.DeepCloneNode(pair.P), sctx.Factory.DeepCloneNode(pair.N)})
		}
	}
	for _, s := range settings {
		st.compare(s, "root", pctx, sctx, pf.AsNode(), pf, sf.Root(), sf, false, where)
		for _, pair := range pairs {
			if kind := pairKind(pair.P); kind != "" {
				st.compare(s, kind, pctx, sctx, pair.P, pf, pair.N, sf, true, fmt.Sprintf("%s@%d", where, pair.P.Pos()))
			}
		}
		for _, c := range clones {
			st.compare(s, c.kind, pctx, sctx, c.p, pf, c.n, sf, true, where)
		}
	}
}

// pairKind is the kind of print a node gets: a statement, a type node, an
// entity name, or none.
func pairKind(p *ast.Node) string {
	switch {
	case ast.IsTypeNode(p):
		return "type"
	case ast.IsEntityName(p):
		return "entity name"
	case ast.IsStatement(p):
		return "statement"
	}
	return ""
}

func TestPrinterCorpus(t *testing.T) {
	files := corpus(t)
	st := &printStats{cells: map[string]*cell{}}
	start := time.Now()
	for i, f := range files {
		st.printFile(t, f.name, f.text, fmt.Sprintf("%s #%d", f.name, i))
	}
	t.Logf("%d files in %v", st.files, time.Since(start).Round(time.Second))
	var keys []string
	for key := range st.cells {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		c := st.cells[key]
		t.Logf("%s: %d prints, %d mismatches, %d both panicked, %d Pointer file/nil differ, %d bytes", key, c.prints, c.mismatches, c.bothPanicked, c.fileNilDiffer, c.length)
		if c.mismatches > 0 {
			t.Errorf("%s: %d mismatches (first: %s)", key, c.mismatches, c.example)
		}
	}
}

// The source file nil (6.3): the escape of an identifier is kept with the
// file (canUseSourceFile) and not without; a deep clone is synthetic, so
// neither keeps it.
func TestPrinterNilSourceFile(t *testing.T) {
	const source = "type T = \\u0061;"
	opts, text, kind := sourceInput("/fixture.ts", source)
	pf := parser.ParseSourceFile(opts, text, kind)
	space := store.NewIDSpace(1)
	sf := storeparser.ParseSourceFileIn(space, 0, opts, text, kind)
	pairs, _ := storetest.Pairs(pf, sf.Store)
	if pairs == nil {
		t.Fatal("parse mismatch")
	}
	pctx := printer.NewEmitContext()
	sctx := NewEmitContextIn(store.NewNodeRegion(space))
	var ref storetest.Pair
	for _, pair := range pairs {
		if pair.P.Kind == ast.KindTypeReference {
			ref = pair
		}
	}
	s := settings[0]
	check := func(label string, p *ast.Node, pfile *ast.SourceFile, n store.Node, sfile *store.File, want string) {
		t.Helper()
		pt, _ := printPointer(s.p, pctx, p, pfile)
		stext, _ := printStore(s.s, sctx, n, sfile)
		if pt != want {
			t.Errorf("%s: the Pointer prints %q, want %q", label, pt, want)
		}
		if stext != pt {
			t.Errorf("%s: the Store prints %q, the Pointer %q", label, stext, pt)
		}
	}
	check("the node with the file", ref.P, pf, ref.N, sf, "\\u0061")
	check("the node with nil", ref.P, nil, ref.N, nil, "a")
	pc, sc := pctx.Factory.DeepCloneNode(ref.P), sctx.Factory.DeepCloneNode(ref.N)
	check("the clone with the file", pc, pf, sc, sf, "a")
	check("the clone with nil", pc, nil, sc, nil, "a")
}

// The information of the EmitContext (6.3) that the node builder writes: for
// each, an input whose Pointer text changes with it, the text with and
// without it fixed, and the Store's the same.
func TestPrinterEmitContext(t *testing.T) {
	const source = "/*leading*/ let x = 'a\\u0062'; if /*c*/ (x) {}"
	opts, text, kind := sourceInput("/fixture.ts", source)
	pf := parser.ParseSourceFile(opts, text, kind)
	space := store.NewIDSpace(1)
	sf := storeparser.ParseSourceFileIn(space, 0, opts, text, kind)
	pairs, _ := storetest.Pairs(pf, sf.Store)
	if pairs == nil {
		t.Fatal("parse mismatch")
	}
	var literal, ifs storetest.Pair
	for _, pair := range pairs {
		switch pair.P.Kind {
		case ast.KindStringLiteral:
			literal = pair
		case ast.KindIfStatement:
			ifs = pair
		}
	}
	type item struct {
		name      string
		setting   setting
		build     func(on bool) (*ast.Node, store.Node)
		with, out string // the Pointer text with the information and without it
	}
	var pctx *printer.EmitContext
	var sctx *EmitContext
	items := []item{
		{
			name:    "emit flags: EFNoAsciiEscaping",
			setting: settings[0],
			build: func(on bool) (*ast.Node, store.Node) {
				p := pctx.Factory.NewStringLiteral("あ", ast.TokenFlagsNone)
				n := sctx.Factory.NewStringLiteral("あ", ast.TokenFlagsNone)
				if on {
					pctx.AddEmitFlags(p, printer.EFNoAsciiEscaping)
					sctx.AddEmitFlags(n, EFNoAsciiEscaping)
				}
				return p, n
			},
			with: "\"あ\"", out: `"\u3042"`,
		},
		{
			// The Store has no NewStringLiteralFromNode: no code in the
			// checker's reach writes textSource. It is set here as that
			// function sets it, so that the printer's read is compared.
			name:    "textSource",
			setting: settings[0],
			build: func(on bool) (*ast.Node, store.Node) {
				if !on {
					return pctx.Factory.NewStringLiteral(literal.P.Text(), ast.TokenFlagsNone), sctx.Factory.NewStringLiteral(literal.N.Text(), ast.TokenFlagsNone)
				}
				p := pctx.Factory.NewStringLiteralFromNode(literal.P)
				n := sctx.Factory.NewStringLiteral(literal.N.Text(), ast.TokenFlagsNone)
				if sctx.textSource == nil {
					sctx.textSource = make(map[store.NodeKey]store.Node)
				}
				sctx.textSource[n.Key()] = literal.N
				return p, n
			},
			with: "'a\\u0062'", out: `"ab"`,
		},
		{
			// A synthetic if statement at the source statement's range: with
			// the source statement as its original, the printer takes it for
			// its parse node and emits the comment between the keyword and the
			// parenthesis (emitToken, enterToken), as for a node that the node
			// builder copies with its range.
			name:    "original",
			setting: settings[0],
			build: func(on bool) (*ast.Node, store.Node) {
				pf := pctx.Factory
				p := pf.NewIfStatement(pf.NewIdentifier("x"), pf.NewBlock(pf.NewNodeList(nil), false), nil)
				sfac := sctx.Factory
				n := sfac.NewIfStatement(sfac.NewIdentifier("x"), sfac.NewBlock(sfac.NewNodeList(nil), false), store.Node{})
				p.Loc = ifs.P.Loc
				sfac.SetRange(n, ifs.N.Pos(), ifs.N.End())
				if on {
					pctx.SetOriginal(p, ifs.P)
					sctx.SetOriginal(n, ifs.N)
				}
				return p, n
			},
			with: "if /*c*/ (x) { }", out: "if (x) { }",
		},
		{
			name:    "synthetic comment",
			setting: settings[0],
			build: func(on bool) (*ast.Node, store.Node) {
				p := pctx.Factory.NewIdentifier("z")
				n := sctx.Factory.NewIdentifier("z")
				if on {
					pctx.AddSyntheticLeadingComment(p, ast.KindMultiLineCommentTrivia, "c", false)
					sctx.AddSyntheticLeadingComment(n, ast.KindMultiLineCommentTrivia, "c", false)
				}
				return p, n
			},
			with: "/*c*/ z", out: "z",
		},
	}
	for _, it := range items {
		t.Run(it.name, func(t *testing.T) {
			texts := map[bool]string{}
			for _, on := range []bool{true, false} {
				pctx = printer.NewEmitContext()
				sctx = NewEmitContextIn(store.NewNodeRegion(space))
				p, n := it.build(on)
				pt, pp := printPointer(it.setting.p, pctx, p, pf)
				stext, sp := printStore(it.setting.s, sctx, n, sf)
				if pp || sp {
					t.Fatalf("panicked: Pointer %v, Store %v", pp, sp)
				}
				if stext != pt {
					t.Errorf("on %v: the Store prints %q, the Pointer %q", on, stext, pt)
				}
				texts[on] = pt
			}
			t.Logf("with %q, without %q", texts[true], texts[false])
			if texts[true] == texts[false] {
				t.Logf("the Pointer text does not change with %s for this input", it.name)
			}
			if it.with != "" && (texts[true] != it.with || texts[false] != it.out) {
				t.Errorf("the Pointer prints %q with and %q without, want %q and %q", texts[true], texts[false], it.with, it.out)
			}
		})
	}
}

// ── The corpus: the seed corpus of internal/parser's FuzzParser, as
// storeparser's TestCorpus reads it ──

type corpusFile struct {
	name, text string
}

func sourceInput(name, source string) (ast.SourceFileParseOptions, string, core.ScriptKind) {
	return ast.SourceFileParseOptions{FileName: name, Path: tspath.Path(name)}, source, core.GetScriptKindFromFileName(name)
}

func corpus(tb testing.TB) []corpusFile {
	tb.Helper()
	var supported []string
	for _, es := range tspath.AllSupportedExtensionsWithJson {
		supported = append(supported, es...)
	}
	var files []corpusFile
	add := func(name, text string) {
		if ext := tspath.TryGetExtensionFromPath(name); slices.Contains(supported, ext) {
			files = append(files, corpusFile{"/index" + ext, text})
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
	if !each(filepath.Join(repo.TestDataPath(), "fixtures"), add) {
		tb.Fatal("testdata/fixtures is missing")
	}
	if testing.Short() {
		return files
	}
	for _, dir := range []string{"tests/cases/compiler", "tests/cases/conformance"} {
		found := each(filepath.Join(repo.TestDataPath(), dir), func(path, text string) {
			units, _, _, _, err := testrunner.ParseTestFilesAndSymlinks(text, path,
				func(name, content string, _ map[string]string) (corpusFile, error) {
					return corpusFile{name, content}, nil
				})
			if err != nil {
				tb.Fatal(err)
			}
			for _, unit := range units {
				add(unit.name, unit.text)
			}
		})
		if !found {
			tb.Logf("skipped: testdata/%s is missing", dir)
		}
	}
	return files
}

var _ = strings.Contains
