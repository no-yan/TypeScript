package scanner

import (
	"strings"
	"testing"

	"github.com/microsoft/TypeScript/tsc/internal/ast"
	"github.com/microsoft/TypeScript/tsc/internal/core"
	"github.com/microsoft/TypeScript/tsc/internal/stringutil"
	"gotest.tools/v3/assert"
)

func TestScanStringPreservesLoneSurrogates(t *testing.T) {
	t.Parallel()
	s := NewScanner()
	s.SetText(`"🦀\ud7ff\ud800\ud801\uD83E\uDD80"`)
	assert.Equal(t, s.Scan(), ast.KindStringLiteral)
	assert.Equal(t, s.TokenValue(), "🦀"+
		stringutil.EncodeJSStringRune(0xD7FF)+
		stringutil.EncodeJSStringRune(0xD800)+
		stringutil.EncodeJSStringRune(0xD801)+
		"🦀")
}

func TestNormalizeJSDocTypeSourceText(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name          string
		text          string
		expectedLines []string
	}{
		{name: "single line", text: " \t* \tFoo", expectedLines: []string{"Foo"}},
		{name: "ECMAScript line breaks", text: "Foo\r\n * Bar\r\t* Baz\u2028 * Qux\u2029* Quux", expectedLines: []string{"Foo", "Bar", "Baz", "Qux", "Quux"}},
		{name: "blank and trailing lines", text: "Foo\r\n *\r\n", expectedLines: []string{"Foo", "", ""}},
		{name: "line without marker", text: "Foo\n  Bar", expectedLines: []string{"Foo", "Bar"}},
		{name: "only leading marker", text: "**Foo", expectedLines: []string{"*Foo"}},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			expected := strings.Join(test.expectedLines, core.NewLineKindLF.GetNewLineCharacter())
			assert.Equal(t, normalizeJSDocTypeSourceText(test.text), expected)
		})
	}
}

func TestIsJSDocTypeExpressionOrChild(t *testing.T) {
	t.Parallel()

	f := ast.NewFactory(ast.FactoryHooks{})
	jsDocType := f.NewTypeReferenceNode(ast.Handle{}, 0)
	jsDocType.SetFlags(ast.NodeFlagsJSDoc)
	jsDocTypeChild := f.NewIdentifier("")
	jsDocTypeChild.SetFlags(ast.NodeFlagsJSDoc)
	jsDocTypeChild.SetParent(jsDocType)
	reparsedType := f.NewTypeLiteralNode(0)
	reparsedType.SetFlags(ast.NodeFlagsReparsed)
	reparsedTypeChild := f.NewIdentifier("")
	reparsedTypeChild.SetFlags(ast.NodeFlagsReparsed)
	reparsedTypeChild.SetParent(reparsedType)
	ordinaryType := f.NewTypeReferenceNode(ast.Handle{}, 0)
	jsDocTag := f.NewJSDocParameterOrPropertyTag(ast.KindJSDocParameterTag, ast.Handle{}, ast.Handle{}, false, ast.Handle{}, false, 0)
	jsDocTag.SetFlags(ast.NodeFlagsJSDoc)
	jsDocTagChild := f.NewIdentifier("")
	jsDocTagChild.SetFlags(ast.NodeFlagsJSDoc)
	jsDocTagChild.SetParent(jsDocTag)

	tests := []struct {
		name     string
		node     ast.Handle
		expected bool
	}{
		{name: "type expression", node: f.NewJSDocTypeExpression(ast.Handle{}), expected: true},
		{name: "JSDoc type", node: jsDocType, expected: true},
		{name: "JSDoc type child", node: jsDocTypeChild, expected: true},
		{name: "reparsed type", node: reparsedType, expected: true},
		{name: "reparsed type child", node: reparsedTypeChild, expected: true},
		{name: "ordinary type", node: ordinaryType, expected: false},
		{name: "other JSDoc child", node: jsDocTagChild, expected: false},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, isJSDocTypeExpressionOrChild(test.node), test.expected)
		})
	}
}

func TestGetTextOfNodeFromJSDocTypePreservesAsteriskType(t *testing.T) {
	t.Parallel()

	sourceText := strings.Join([]string{"", " * *"}, core.NewLineKindLF.GetNewLineCharacter())
	node := ast.NewFactory(ast.FactoryHooks{}).NewJSDocAllType()
	node.SetFlags(ast.NodeFlagsJSDoc)
	node.SetLoc(core.NewTextRange(0, len(sourceText)))

	assert.Equal(t, GetTextOfNodeFromSourceText(sourceText, node, false /*includeTrivia*/), "*")
}
