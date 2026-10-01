package storeharness

import "fmt"

// The properties of CompareSymbols on each side (review contracts 2.4,
// store-checker-7d2b2-implementation-instructions-20261001.md 6). Its last
// resort is the symbols' ids, which each side numbers lazily, so the two
// sides' signs are not compared: each side is checked alone, after the
// driver has recorded everything, as a comparison may number a symbol.
//
//   - identity: cmp(s, s) == 0 for every symbol of the symbol walk;
//   - antisymmetry: sign(cmp(a, b)) == -sign(cmp(b, a)) for the distinct
//     symbols next to each other in the walk, and for every pair of symbols
//     without declarations of the same name (they reach the ids);
//   - stability: each of those pairs compares the same a second time, after
//     all the others.
//
// The order the public output shows (walks, diagnostics) is compared, as it
// is, by the comparison of the runs.

// symbolOrder is what checkSymbolOrder found on one side: the pairs checked,
// those of them that reach the id fallback (the same name, and no
// declarations or first declarations at the same place, which
// compareNodes does not order), and the first violation.
type symbolOrder struct {
	pairs, fallback int
	property        string // "" when none is violated
	violation       string
}

func checkSymbolOrder(records []walkRecord, compare func(a, b any) int, name func(any) string) symbolOrder {
	var o symbolOrder
	fail := func(property, format string, args ...any) {
		if o.property == "" {
			o.property, o.violation = property, fmt.Sprintf(format, args...)
		}
	}
	var walk, symbols []any // the walk's symbols, and each once in the walk's order
	decls := map[any][]declKey{}
	for _, r := range records {
		if !r.present || r.symbol == nil {
			continue
		}
		walk = append(walk, r.symbol)
		if _, ok := decls[r.symbol]; !ok {
			decls[r.symbol] = r.decls
			symbols = append(symbols, r.symbol)
		}
	}
	for _, s := range symbols {
		if r := compare(s, s); r != 0 {
			fail("identity", "cmp(%q, itself) = %d", name(s), r)
		}
	}

	type pair struct {
		a, b any
		ab   int
	}
	var pairs []pair
	seen := map[[2]any]bool{}
	check := func(a, b any) {
		if seen[[2]any{a, b}] || seen[[2]any{b, a}] {
			return
		}
		seen[[2]any{a, b}] = true
		ab, ba := compare(a, b), compare(b, a)
		if sign(ab) != -sign(ba) {
			fail("antisymmetry", "cmp(%q, %q) = %d, cmp(%q, %q) = %d", name(a), name(b), ab, name(b), name(a), ba)
		}
		pairs = append(pairs, pair{a, b, ab})
		if reachesIDFallback(name(a), name(b), decls[a], decls[b]) {
			o.fallback++
		}
	}
	for i := 1; i < len(walk); i++ {
		if walk[i-1] != walk[i] {
			check(walk[i-1], walk[i])
		}
	}
	var names []string
	byName := map[string][]any{}
	for _, s := range symbols {
		if len(decls[s]) == 0 {
			n := name(s)
			if _, ok := byName[n]; !ok {
				names = append(names, n)
			}
			byName[n] = append(byName[n], s)
		}
	}
	for _, n := range names {
		group := byName[n]
		for i := range group {
			for _, b := range group[i+1:] {
				check(group[i], b)
			}
		}
	}
	for _, p := range pairs {
		if r := compare(p.a, p.b); r != p.ab {
			fail("stability", "cmp(%q, %q) = %d, then %d", name(p.a), name(p.b), p.ab, r)
		}
	}
	o.pairs = len(pairs)
	return o
}

func reachesIDFallback(nameA, nameB string, a, b []declKey) bool {
	if nameA != nameB {
		return false
	}
	if len(a) == 0 || len(b) == 0 {
		return len(a) == len(b)
	}
	return a[0].path == b[0].path && a[0].pos == b[0].pos
}

func sign(x int) int {
	switch {
	case x < 0:
		return -1
	case x > 0:
		return 1
	}
	return 0
}
