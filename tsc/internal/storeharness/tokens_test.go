package storeharness

import "testing"

// TestInternalIDTokenRe: the three forms of 5.4 match, the raw byte 0xFE
// included, and strings like them without an id do not.
func TestInternalIDTokenRe(t *testing.T) {
	for _, s := range []string{
		"#12@",
		"__#12@#x",
		"__@iterator@34",
		"Symbol(__@iterator@34)",
		"\xFE@iterator@34", // the internal prefix, not escaped
		"\xFE@@1",
	} {
		if !internalIDTokenRe.MatchString(s) {
			t.Errorf("%q: not found", s)
		}
	}
	for _, s := range []string{
		"#x@", // a private name
		"#@",
		"#12",
		"__@iterator",
		"__@iterator@",
		"__@iterator@x", // a name where the id is
		"_@iterator@1",
		"@iterator@1",
		"\xFEiterator@1",
		"\u00fe@x@1", // U+00FE, which the regexp \xFE matches (2b-1 verification 7.1)
	} {
		if internalIDTokenRe.MatchString(s) {
			t.Errorf("%q: found", s)
		}
	}
}

// TestIDTokens counts the tokens of every string of a run.
func TestIDTokens(t *testing.T) {
	r := &run{
		diags:       [][]diag{{{messageText: "a", messageArgs: []string{"__@x@1", "b"}, messageChain: []diag{{messageText: "#2@"}}}}},
		suggestions: [][]diag{{{relatedInformation: []diag{{messageText: "\xFE@y@3"}}}}},
		global:      []diag{{messageText: "__@z@4 __@z@5"}},
		types:       []walkRecord{{value: "number"}, {value: "#6@"}},
		symbols:     []walkRecord{{value: "Symbol(__@w@7)"}},
	}
	if n, where, value := idTokens(r); n != 7 || where != "file of target 0 #0 MessageArgs[0]" || value != "__@x@1" {
		t.Errorf("idTokens = %d, %q, %q; want 7, at file of target 0 #0 MessageArgs[0], __@x@1", n, where, value)
	}
}
