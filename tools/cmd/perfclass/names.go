package main

import (
	"regexp"
	"strings"
)

const tscPrefix = "github.com/microsoft/TypeScript/tsc/internal/"

// fn is a symbol name split as the rules need it: generic instances and
// closures are folded into their function.
type fn struct {
	Raw     string
	Pkg     string // tsc packages without tscPrefix ("checker", "ast/store"), others as is ("runtime", "slices")
	Recv    string // the receiver's base type, without * or type arguments; "" for a function
	Name    string
	Closure string // the closure suffix that was folded ("func1", "func1.2"), "" for none
	Generic bool   // the symbol is a generic instance
}

var closurePart = regexp.MustCompile(`^(func|gowrap|deferwrap)\d+$|^\d+$`)

// parseFunc splits a symbol name of a Go stack frame. The suffix " (inline)"
// of pprof's -traces is dropped.
func parseFunc(raw string) fn {
	raw = strings.TrimSuffix(raw, " (inline)")
	f := fn{Raw: raw}
	s, generic := stripBrackets(raw)
	f.Generic = generic
	slash := strings.LastIndex(s, "/")
	dot := strings.Index(s[slash+1:], ".")
	if dot < 0 {
		f.Pkg, f.Name = s, s
		return f
	}
	f.Pkg = strings.TrimPrefix(s[:slash+1+dot], tscPrefix)
	parts := strings.Split(s[slash+1+dot+1:], ".")
	// glob..func1 is a closure of a package-level initializer.
	end := len(parts)
	for end > 1 && (closurePart.MatchString(parts[end-1]) || parts[end-1] == "") {
		end--
	}
	f.Closure = strings.TrimPrefix(strings.Join(parts[end:], "."), ".")
	parts = parts[:end]
	switch len(parts) {
	case 1:
		f.Name = parts[0]
	default:
		f.Recv = strings.TrimSuffix(strings.TrimPrefix(strings.TrimPrefix(parts[0], "(*"), "("), ")")
		f.Name = strings.Join(parts[1:], ".")
	}
	// A range-over-func body (f-range1) and a method value (M-fm) belong
	// to their function.
	if i := strings.Index(f.Name, "-"); i >= 0 {
		f.Name = f.Name[:i]
	}
	return f
}

// stripBrackets removes the bracketed type arguments of a generic instance.
func stripBrackets(s string) (string, bool) {
	if !strings.Contains(s, "[") {
		return s, false
	}
	var b strings.Builder
	depth := 0
	for _, r := range s {
		switch {
		case r == '[':
			depth++
		case r == ']':
			depth--
		case depth == 0:
			b.WriteRune(r)
		}
	}
	return b.String(), true
}

// storePkgs pair each Store package with its Pointer package.
var storePkgs = map[string]string{
	"ast/store":             "ast",
	"storechecker":          "checker",
	"storebinder":           "binder",
	"storeparser":           "parser",
	"storeprinter":          "printer",
	"storecompiler":         "compiler",
	"storeevaluator":        "evaluator",
	"storemodule":           "module",
	"storemodulespecifiers": "modulespecifiers",
	"storenodebuilder":      "nodebuilder",
	"storepseudochecker":    "pseudochecker",
	"storescanner":          "scanner",
}

// normPkg is the Pointer package of a Store package, other packages as is.
func normPkg(pkg string) string {
	if p, ok := storePkgs[pkg]; ok {
		return p
	}
	return pkg
}

// Key is the function with the Store packages named as the Pointer's and the
// closure folded: the same key on both sides is the same function.
func (f fn) Key() string {
	if f.Recv != "" {
		return normPkg(f.Pkg) + ".(" + f.Recv + ")." + f.Name
	}
	return normPkg(f.Pkg) + "." + f.Name
}

// Display is the function with its own package and the closure folded.
func (f fn) Display() string {
	if f.Recv != "" {
		return f.Pkg + ".(" + f.Recv + ")." + f.Name
	}
	return f.Pkg + "." + f.Name
}

func (f fn) isRuntime() bool {
	return f.Pkg == "runtime" || strings.HasPrefix(f.Pkg, "internal/runtime/") || f.Pkg == "internal/bytealg"
}

func (f fn) isTsc() bool { return strings.HasPrefix(f.Raw, tscPrefix) }
