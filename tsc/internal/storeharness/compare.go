package storeharness

import (
	"fmt"
	"regexp"
	"slices"
	"strings"
)

// The comparison (6.2, 6.3, 6.4) and the class of a program (7.2): the first
// difference in the driver's order decides it, and its key is built from what
// differs, with no table of names.

// Classes other than a difference's.
const (
	ClassEqual            = "equal"
	ClassSyntheticImport  = "synthetic import (stage 2)"
	ClassInternalIDToken  = "internal id token"
	classCorrespondence   = "symbol correspondence"
	classWalkNodeSet      = "walk: node set"
	storePanicClassPrefix = "store panic: "
)

// internalIDToken are the numbers that the two sides may give differently:
// the class symbol's id of a private name (#<id>@) and a unique symbol's id
// (__@name@<id>, or with the internal prefix before escaping).
var (
	privateNameIDRe  = regexp.MustCompile(`#[0-9]+@`)
	uniqueSymbolIDRe = regexp.MustCompile(`(__|\xFE)@([^@]*)@[0-9]+`)
)

func withoutIDs(s string) string {
	s = privateNameIDRe.ReplaceAllString(s, "#@")
	return uniqueSymbolIDRe.ReplaceAllString(s, "$1@$2@")
}

type comparison struct {
	found   *Detail
	class   string
	idToken *Detail // the first difference that is only in the ids
}

func (c *comparison) fail(class string, d Detail) {
	c.class, c.found = class, &d
}

func compareRuns(p, s *run) (string, *Detail) {
	if s.panicked != nil {
		if s.panicked.message == syntheticImportPanic {
			return ClassSyntheticImport, &Detail{Where: s.panicked.frame, Store: s.panicked.message}
		}
		return storePanicClassPrefix + first60(s.panicked.message) + " @ " + s.panicked.frame, &Detail{Where: s.panicked.frame, Store: s.panicked.message + "\n" + s.panicked.stack}
	}
	c := &comparison{}
	for i := range p.diags {
		c.diagList("file", i, p.diags[i], s.diags[i])
		c.diagList("suggestion", i, p.suggestions[i], s.suggestions[i])
	}
	c.diagList("global", -1, p.global, s.global)
	c.walk("type", p.types, s.types)
	c.walk("symbol", p.symbols, s.symbols)
	c.correspondence(p.symbols, s.symbols)
	switch {
	case c.found != nil:
		return c.class, c.found
	case c.idToken != nil:
		return ClassInternalIDToken, c.idToken
	}
	return ClassEqual, nil
}

func first60(s string) string {
	r := []rune(s)
	if len(r) > 60 {
		r = r[:60]
	}
	return string(r)
}

// diagList compares a column of diagnostics: its count, then each
// diagnostic's fields in order.
func (c *comparison) diagList(column string, file int, p, s []diag) {
	if c.found != nil {
		return
	}
	where := column
	if file >= 0 {
		where = fmt.Sprintf("%s of root %d", column, file)
	}
	if len(p) != len(s) {
		c.fail("diag "+column+": count", Detail{Where: where, Pointer: diagsString(p), Store: diagsString(s)})
		return
	}
	for i := range p {
		field, pv, sv := diagDiff(p[i], s[i], false)
		if field == "" {
			continue
		}
		d := Detail{Where: fmt.Sprintf("%s #%d %s", where, i, field), Pointer: pv + " | " + p[i].String(), Store: sv + " | " + s[i].String()}
		if other, _, _ := diagDiff(p[i], s[i], true); other == "" {
			if c.idToken == nil {
				c.idToken = &d
			}
			continue
		}
		c.fail("diag "+column+": "+field, d)
		return
	}
}

// diagDiff is the first field that differs, with the two values; the
// strings of the message are compared without their ids when ids is set.
func diagDiff(p, s diag, ids bool) (field, pv, sv string) {
	str := func(v string) string {
		if ids {
			return withoutIDs(v)
		}
		return v
	}
	type value struct {
		name string
		p, s any
	}
	values := []value{
		{"File", p.file, s.file},
		{"Pos", p.pos, s.pos},
		{"End", p.end, s.end},
		{"Code", p.code, s.code},
		{"Category", p.category, s.category},
		{"MessageKey", p.messageKey, s.messageKey},
		{"MessageArgs", strings.Join(mapStrings(p.messageArgs, str), "\x00"), strings.Join(mapStrings(s.messageArgs, str), "\x00")},
		{"MessageText", str(p.messageText), str(s.messageText)},
		{"Source", p.source, s.source},
		{"ReportsUnnecessary", p.reportsUnnecessary, s.reportsUnnecessary},
		{"ReportsDeprecated", p.reportsDeprecated, s.reportsDeprecated},
		{"SkippedOnNoEmit", p.skippedOnNoEmit, s.skippedOnNoEmit},
		{"RepopulateInfo", p.repopulate != nil, s.repopulate != nil},
	}
	for _, v := range values {
		if v.p != v.s {
			if v.name == "MessageArgs" {
				return v.name, fmt.Sprintf("%q", p.messageArgs), fmt.Sprintf("%q", s.messageArgs)
			}
			return v.name, fmt.Sprint(v.p), fmt.Sprint(v.s)
		}
	}
	if p.repopulate != nil {
		pr, sr := *p.repopulate, *s.repopulate
		for _, v := range []value{
			{"RepopulateInfo.Kind", pr.kind, sr.kind},
			{"RepopulateInfo.ModuleReference", pr.moduleReference, sr.moduleReference},
			{"RepopulateInfo.Mode", pr.mode, sr.mode},
			{"RepopulateInfo.PackageName", pr.packageName, sr.packageName},
		} {
			if v.p != v.s {
				return v.name, fmt.Sprint(v.p), fmt.Sprint(v.s)
			}
		}
	}
	for _, nested := range []struct {
		name string
		p, s []diag
	}{
		{"MessageChain", p.messageChain, s.messageChain},
		{"RelatedInformation", p.relatedInformation, s.relatedInformation},
	} {
		if len(nested.p) != len(nested.s) {
			return nested.name + ".count", fmt.Sprint(len(nested.p)), fmt.Sprint(len(nested.s))
		}
		for i := range nested.p {
			if field, pv, sv := diagDiff(nested.p[i], nested.s[i], ids); field != "" {
				return nested.name + "." + field, pv, sv
			}
		}
	}
	return "", "", ""
}

func mapStrings(ss []string, f func(string) string) []string {
	out := make([]string, len(ss))
	for i, s := range ss {
		out[i] = f(s)
	}
	return out
}

func (d diag) String() string {
	return fmt.Sprintf("%s %d-%d TS%d %s %q", d.file, d.pos, d.end, d.code, d.messageKey, d.messageArgs)
}

func diagsString(ds []diag) string {
	var b strings.Builder
	fmt.Fprintf(&b, "%d:", len(ds))
	for _, d := range ds {
		b.WriteString("\n  ")
		b.WriteString(d.String())
	}
	return b.String()
}

// walk compares the records of a walk: the same nodes, then what the walker
// gives for each.
func (c *comparison) walk(which string, p, s []walkRecord) {
	if c.found != nil {
		return
	}
	for i := range min(len(p), len(s)) {
		pr, sr := p[i], s[i]
		where := fmt.Sprintf("%s walk #%d at %d %v %q", which, i, pr.pos, pr.kind, pr.text)
		if pr.pos != sr.pos || pr.kind != sr.kind || pr.text != sr.text {
			c.fail(classWalkNodeSet, Detail{Where: fmt.Sprintf("%s walk #%d", which, i), Pointer: pr.node(), Store: sr.node()})
			return
		}
		if pr.present == sr.present && pr.value == sr.value {
			continue
		}
		d := Detail{Where: where, Pointer: pr.shown(), Store: sr.shown()}
		if pr.present && sr.present && withoutIDs(pr.value) == withoutIDs(sr.value) {
			if c.idToken == nil {
				c.idToken = &d
			}
			continue
		}
		c.fail("walk: "+which, d)
		return
	}
	if len(p) != len(s) {
		c.fail(classWalkNodeSet, Detail{Where: fmt.Sprintf("%s walk: %d records, %d", which, len(p), len(s)), Pointer: recordAt(p, len(s)), Store: recordAt(s, len(p))})
	}
}

func (r walkRecord) node() string { return fmt.Sprintf("%d %v %q", r.pos, r.kind, r.text) }

func (r walkRecord) shown() string {
	if !r.present {
		return "<none>"
	}
	return r.value
}

func recordAt(rs []walkRecord, i int) string {
	if i < len(rs) {
		return rs[i].node() + " : " + rs[i].shown()
	}
	return "<end>"
}

// correspondence pairs the symbols of the same records of the symbol walk
// (6.4): one to one, in both directions, and a symbol's declarations with its
// counterpart's (file, range, kind, in order). The ids are neither compared
// nor asked for.
func (c *comparison) correspondence(p, s []walkRecord) {
	if c.found != nil {
		return
	}
	toStore := map[any]any{}
	toPointer := map[any]any{}
	for i := range p {
		pr, sr := p[i], s[i]
		if !pr.present || !sr.present {
			continue
		}
		where := fmt.Sprintf("symbol walk #%d at %d %v %q", i, pr.pos, pr.kind, pr.text)
		if paired, ok := toStore[pr.symbol]; ok {
			if paired != sr.symbol {
				c.fail(classCorrespondence, Detail{Where: where + ": the Pointer symbol has two Store symbols", Pointer: pr.value, Store: sr.value})
				return
			}
			continue
		}
		if _, ok := toPointer[sr.symbol]; ok {
			c.fail(classCorrespondence, Detail{Where: where + ": the Store symbol has two Pointer symbols", Pointer: pr.value, Store: sr.value})
			return
		}
		toStore[pr.symbol] = sr.symbol
		toPointer[sr.symbol] = pr.symbol
		if !slices.Equal(pr.decls, sr.decls) {
			c.fail(classCorrespondence, Detail{Where: where + ": the declarations differ", Pointer: fmt.Sprint(pr.decls), Store: fmt.Sprint(sr.decls)})
			return
		}
	}
}
