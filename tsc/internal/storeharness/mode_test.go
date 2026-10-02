package storeharness

import (
	"cmp"
	"fmt"
	"slices"
	"sync"
	"testing"

	"github.com/microsoft/TypeScript/tsc/internal/ast"
	"github.com/microsoft/TypeScript/tsc/internal/ast/store"
	"github.com/microsoft/TypeScript/tsc/internal/ast/store/storetest"
	"github.com/microsoft/TypeScript/tsc/internal/compiler"
	"github.com/microsoft/TypeScript/tsc/internal/core"
)

// TestModeCorpus is the unit equivalence of the adapter's C methods (8.1):
// on the program of every file of the corpus, at every module specifier
// position, the Pointer program's GetModeForUsageLocation,
// GetEmitSyntaxForUsageLocation and GetResolvedModuleFromModuleSpecifier and
// the adapter's give the same. Strict.
func TestModeCorpus(t *testing.T) {
	if testing.Short() {
		t.Skip("the mode corpus is not run with -short")
	}
	files := corpusUnder(t, true)
	type fileStats struct {
		positions     map[string]int // by the specifier's place
		modes         map[core.ResolutionMode]int
		overridden    int // the mode is not the emit syntax: a resolution-mode attribute
		mismatches    []string
		missing       bool
		pointerPanics bool // the Pointer program panics
	}
	stats := make([]fileStats, len(files))
	next := make(chan int)
	var wg sync.WaitGroup
	for range envInt(t, "STORE_CHECKER_CORPUS_WORKERS", 4) {
		wg.Go(func() {
			for i := range next {
				st := fileStats{positions: map[string]int{}, modes: map[core.ResolutionMode]int{}}
				f := files[i]
				fileMap := map[string]string{f.name: f.text}
				pointer, panicked := tryPointerProgram(fileMap, stage1Options())
				if panicked != nil {
					st.pointerPanics = true
					stats[i] = st
					continue
				}
				pf := pointer.GetSourceFile(f.name)
				if pf == nil {
					st.missing = true
					stats[i] = st
					continue
				}
				program := newProgram(pointer)
				sf := program.Store(pf)
				for _, pair := range specifierPairs(t, f.origin, pf, sf) {
					place := specifierPlace(pair.P)
					st.positions[place]++
					m := compareSpecifier(pointer, program, pf, sf, pair)
					st.modes[m.mode]++
					if m.overridden {
						st.overridden++
					}
					for _, mismatch := range m.mismatches {
						st.mismatches = append(st.mismatches, fmt.Sprintf("%s %s at %d (%s %q): %s", mismatch.method, f.origin, pair.P.Pos(), place, pair.P.Text(), mismatch.values))
					}
				}
				stats[i] = st
			}
		})
	}
	for i := range files {
		next <- i
	}
	close(next)
	wg.Wait()

	positions := map[string]int{}
	modes := map[core.ResolutionMode]int{}
	var total, missing, pointerPanics, mismatches, overridden int
	for _, st := range stats {
		overridden += st.overridden
		if st.missing {
			missing++
		}
		if st.pointerPanics {
			pointerPanics++
		}
		for k, v := range st.positions {
			positions[k] += v
			total += v
		}
		for k, v := range st.modes {
			modes[k] += v
		}
		for _, m := range st.mismatches {
			mismatches++
			if mismatches <= 50 {
				t.Error(m)
			}
		}
	}
	t.Logf("%d files (%d not in their program, %d whose Pointer program panics), %d module specifier positions, %d mismatches", len(files), missing, pointerPanics, total, mismatches)
	for _, k := range slices.Sorted(func(yield func(string) bool) {
		for k := range positions {
			if !yield(k) {
				return
			}
		}
	}) {
		t.Logf("  %-12s %d", k, positions[k])
	}
	t.Logf("  %d positions whose mode is not their emit syntax (a resolution-mode override)", overridden)
	var byMode []core.ResolutionMode
	for m := range modes {
		byMode = append(byMode, m)
	}
	slices.SortFunc(byMode, func(a, b core.ResolutionMode) int { return cmp.Compare(a, b) })
	for _, m := range byMode {
		t.Logf("  mode %-10v %d", m, modes[m])
	}
}

// modeComparison is compareSpecifier's: the Pointer's mode, whether it is
// not the emit syntax (a resolution-mode override), and the methods whose
// results differ.
type modeComparison struct {
	mode       core.ResolutionMode
	overridden bool
	mismatches []modeMismatch
}

type modeMismatch struct {
	method string
	values string // the Pointer's and the Store's
}

// compareSpecifier compares the Pointer program's GetModeForUsageLocation,
// GetEmitSyntaxForUsageLocation and GetResolvedModuleFromModuleSpecifier with
// the adapter's at a pair of module specifiers.
func compareSpecifier(pointer *compiler.Program, program *Program, pf *ast.SourceFile, sf *store.File, pair storetest.Pair) modeComparison {
	pm, sm := pointer.GetModeForUsageLocation(pf, pair.P), program.GetModeForUsageLocation(sf, pair.N)
	pe, se := pointer.GetEmitSyntaxForUsageLocation(pf, pair.P), program.GetEmitSyntaxForUsageLocation(sf, pair.N)
	pr, sr := pointer.GetResolvedModuleFromModuleSpecifier(pf, pair.P), program.GetResolvedModuleFromModuleSpecifier(sf, pair.N)
	m := modeComparison{mode: pm, overridden: pm != pe}
	if pm != sm {
		m.mismatches = append(m.mismatches, modeMismatch{"GetModeForUsageLocation", fmt.Sprintf("Pointer %v, Store %v", pm, sm)})
	}
	if pe != se {
		m.mismatches = append(m.mismatches, modeMismatch{"GetEmitSyntaxForUsageLocation", fmt.Sprintf("Pointer %v, Store %v", pe, se)})
	}
	if pr != sr {
		m.mismatches = append(m.mismatches, modeMismatch{"GetResolvedModuleFromModuleSpecifier", fmt.Sprintf("Pointer %p, Store %p", pr, sr)})
	}
	return m
}

// specifierPairs are the pairs of filePairs whose Pointer node is a module
// specifier.
func specifierPairs(t *testing.T, origin string, pf *ast.SourceFile, sf *store.File) []storetest.Pair {
	pairs, _ := filePairs(t, origin, pf, sf)
	return slices.DeleteFunc(pairs, func(pair storetest.Pair) bool { return specifierPlace(pair.P) == "" })
}

// filePairs is pairNodes, with its problems the test's errors.
func filePairs(t *testing.T, origin string, pf *ast.SourceFile, sf *store.File) ([]storetest.Pair, int) {
	pairs, jsdoc, problems := pairNodes(pf, sf)
	for _, problem := range problems {
		t.Errorf("%s: %s", origin, problem)
	}
	return pairs, jsdoc
}

// pairNodes are the pairs of the file's tree (storetest.Pairs), each host
// followed by the pairs of its JSDoc, how many of them are JSDoc's, and what
// does not pair.
func pairNodes(pf *ast.SourceFile, sf *store.File) (out []storetest.Pair, jsdoc int, problems []string) {
	pairs, m := storetest.Pairs(pf, sf.Store)
	if pairs == nil {
		return nil, 0, []string{fmt.Sprintf("the trees do not pair: %v", m)}
	}
	for _, pair := range pairs {
		out = append(out, pair)
		if pair.P.Flags&ast.NodeFlagsHasJSDoc == 0 {
			continue
		}
		ps, ns := pair.P.JSDoc(pf), pair.N.JSDoc(sf)
		if len(ps) != len(ns) {
			problems = append(problems, fmt.Sprintf("%d JSDoc on the Pointer host at %d, %d on the Store's", len(ps), pair.P.Pos(), len(ns)))
			continue
		}
		for i, p := range ps {
			pointers, nodes := preorderPointer(p), storetest.Preorder(ns[i])
			if len(pointers) != len(nodes) {
				problems = append(problems, fmt.Sprintf("the JSDoc at %d do not pair: %d nodes, %d", p.Pos(), len(pointers), len(nodes)))
				continue
			}
			for j, pn := range pointers {
				out = append(out, storetest.Pair{P: pn, N: nodes[j]})
			}
			jsdoc += len(pointers)
		}
	}
	return out, jsdoc, problems
}

func preorderPointer(root *ast.Node) []*ast.Node {
	var nodes []*ast.Node
	var visit ast.Visitor
	visit = func(n *ast.Node) bool {
		nodes = append(nodes, n)
		n.ForEachChild(visit)
		return false
	}
	visit(root)
	return nodes
}

// specifierPlace is where a Pointer node is a module specifier, or "": an
// import, export or import-equals external reference, a require, an import
// call, an import type, a JSDoc import.
func specifierPlace(n *ast.Node) string {
	if !ast.IsStringLiteralLike(n) || n.Parent == nil {
		return ""
	}
	p := n.Parent
	switch p.Kind {
	case ast.KindImportDeclaration, ast.KindJSImportDeclaration:
		if p.AsImportDeclaration().ModuleSpecifier == n {
			return "import"
		}
	case ast.KindExportDeclaration:
		if p.AsExportDeclaration().ModuleSpecifier == n {
			return "export"
		}
	case ast.KindExternalModuleReference:
		if p.AsExternalModuleReference().Expression == n {
			return "import="
		}
	case ast.KindCallExpression:
		if args := p.Arguments(); len(args) > 0 && args[0] == n {
			switch {
			case ast.IsRequireCall(p, true /*requireStringLiteralLikeArgument*/):
				return "require"
			case ast.IsImportCall(p):
				return "import()"
			}
		}
	case ast.KindLiteralType:
		if ast.IsImportTypeNode(p.Parent) && p.Parent.AsImportTypeNode().Argument == p {
			return "import type"
		}
	case ast.KindJSDocImportTag:
		if p.AsJSDocImportTag().ModuleSpecifier == n {
			return "JSDoc import"
		}
	}
	return ""
}
