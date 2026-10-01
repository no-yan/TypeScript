package storeharness

import (
	"fmt"
	"regexp"
)

// The internal id tokens (store-checker-7d2b2-implementation-instructions-20261001.md
// 5.4): numbers that the two sides may give differently, as each numbers its
// symbols lazily (review contracts 2.3). They are found, not taken out: the
// strings are compared as they are, and a file whose records hold a token on
// either side fails as internalIDTokenPresent, equal or not, until the
// tokens are matched to the corresponding symbols (contracts 2.3), a design
// that waits for the first token. The reference baselines and the corpus of
// 2b-1 have none.
//
//   - #<id>@: the class symbol's id in a private name.
//   - __@<name>@<id>: a unique symbol's id, escaped.
//   - <prefix>@<name>@<id>, the prefix the byte 0xFE before escaping: Go's
//     regexp reads an invalid byte as U+FFFD, so \x{FFFD} matches it (and a
//     U+FFFD character, which is found too).
var internalIDTokenRe = regexp.MustCompile(`#[0-9]+@|__@[^@]*@[0-9]+|\x{FFFD}@[^@]*@[0-9]+`)

// idTokens counts the tokens in the strings of a run: each diagnostic's
// message, arguments, chain and related information, and the values of the
// type and symbol walks. where and value are the first string with one.
func idTokens(r *run) (count int, where, value string) {
	found := func(at, s string) {
		n := len(internalIDTokenRe.FindAllStringIndex(s, -1))
		if n > 0 && count == 0 {
			where, value = at, s
		}
		count += n
	}
	var diags func(column string, ds []diag)
	diags = func(column string, ds []diag) {
		for i, d := range ds {
			at := fmt.Sprintf("%s #%d", column, i)
			found(at+" MessageText", d.messageText)
			for j, arg := range d.messageArgs {
				found(fmt.Sprintf("%s MessageArgs[%d]", at, j), arg)
			}
			diags(at+" MessageChain", d.messageChain)
			diags(at+" RelatedInformation", d.relatedInformation)
		}
	}
	for i := range r.diags {
		diags(fmt.Sprintf("file of root %d", i), r.diags[i])
		diags(fmt.Sprintf("suggestion of root %d", i), r.suggestions[i])
	}
	diags("global", r.global)
	for _, w := range []struct {
		which   string
		records []walkRecord
	}{{"type", r.types}, {"symbol", r.symbols}} {
		for i, record := range w.records {
			found(fmt.Sprintf("%s walk #%d at %d", w.which, i, record.pos), record.value)
		}
	}
	return count, where, value
}
