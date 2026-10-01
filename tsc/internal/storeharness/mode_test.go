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
					if place == "" {
						continue
					}
					st.positions[place]++
					pm, sm := pointer.GetModeForUsageLocation(pf, pair.P), program.GetModeForUsageLocation(sf, pair.N)
					st.modes[pm]++
					pe, se := pointer.GetEmitSyntaxForUsageLocation(pf, pair.P), program.GetEmitSyntaxForUsageLocation(sf, pair.N)
					if pm != pe {
						st.overridden++
					}
					pr, sr := pointer.GetResolvedModuleFromModuleSpecifier(pf, pair.P), program.GetResolvedModuleFromModuleSpecifier(sf, pair.N)
					where := fmt.Sprintf("%s at %d (%s %q)", f.origin, pair.P.Pos(), place, pair.P.Text())
					if pm != sm {
						st.mismatches = append(st.mismatches, fmt.Sprintf("GetModeForUsageLocation %s: Pointer %v, Store %v", where, pm, sm))
					}
					if pe != se {
						st.mismatches = append(st.mismatches, fmt.Sprintf("GetEmitSyntaxForUsageLocation %s: Pointer %v, Store %v", where, pe, se))
					}
					if pr != sr {
						st.mismatches = append(st.mismatches, fmt.Sprintf("GetResolvedModuleFromModuleSpecifier %s: Pointer %p, Store %p", where, pr, sr))
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

// specifierPairs are the pairs of the file's tree (storetest.Pairs), and of
// the JSDoc of each host, whose Pointer node is a module specifier.
func specifierPairs(t *testing.T, origin string, pf *ast.SourceFile, sf *store.File) []storetest.Pair {
	pairs, m := storetest.Pairs(pf, sf.Store)
	if pairs == nil {
		t.Errorf("%s: the trees do not pair: %v", origin, m)
		return nil
	}
	var out []storetest.Pair
	for _, pair := range pairs {
		if specifierPlace(pair.P) != "" {
			out = append(out, pair)
		}
		if pair.P.Flags&ast.NodeFlagsHasJSDoc == 0 {
			continue
		}
		ps, ns := pair.P.JSDoc(pf), pair.N.JSDoc(sf)
		if len(ps) != len(ns) {
			t.Errorf("%s: %d JSDoc on the Pointer host at %d, %d on the Store's", origin, len(ps), pair.P.Pos(), len(ns))
			continue
		}
		for i, p := range ps {
			pointers, nodes := preorderPointer(p), storetest.Preorder(ns[i])
			if len(pointers) != len(nodes) {
				t.Errorf("%s: the JSDoc at %d do not pair: %d nodes, %d", origin, p.Pos(), len(pointers), len(nodes))
				continue
			}
			for j, pn := range pointers {
				if specifierPlace(pn) != "" {
					out = append(out, storetest.Pair{P: pn, N: nodes[j]})
				}
			}
		}
	}
	return out
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
