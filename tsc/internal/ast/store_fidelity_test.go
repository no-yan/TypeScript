package ast

import (
	"testing"

	"github.com/microsoft/TypeScript/tsc/internal/core"
	"gotest.tools/v3/assert"
)

func TestExpandStorePreservesKindValuesTokenFlagsAndParents(t *testing.T) {
	t.Parallel()
	f := NewNodeFactory(NodeFactoryHooks{})
	store := NewStore(8)
	f.AttachStore(store)

	literal := f.NewNumericLiteral("0x10", TokenFlagsHexSpecifier)
	template := f.NewTemplateHead("cooked", "raw\\n", TokenFlagsContainsInvalidEscape)
	heritage := f.NewHeritageClause(KindExtendsKeyword, f.NewNodeList([]*Node{literal}))
	root := f.NewJSDoc(
		f.NewNodeList([]*Node{f.NewJSDocText([]string{"first", "second"})}),
		f.NewNodeList([]*Node{template, heritage}),
	)
	root.Loc = core.NewTextRange(0, 20)
	f.StoreSync(root)
	assert.Equal(t, f.HandleOf(root).Ref(), f.HandleOf(root.AsJSDoc().Comment.Nodes[0]).Parent().Ref())

	expanded := ExpandStore(f.HandleOf(root), SourceFileParseOptions{}, "")
	assert.Equal(t, KindJSDoc, expanded.Kind)
	assert.Equal(t, expanded, expanded.AsJSDoc().Comment.Nodes[0].Parent)
	assert.DeepEqual(t, []string{"first", "second"}, expanded.AsJSDoc().Comment.Nodes[0].data.(*JSDocText).text)

	gotTemplate := expanded.AsJSDoc().Tags.Nodes[0].AsTemplateHead()
	assert.Equal(t, "cooked", gotTemplate.Text)
	assert.Equal(t, "raw\\n", gotTemplate.RawText)
	assert.Equal(t, TokenFlagsContainsInvalidEscape, gotTemplate.TemplateFlags)

	gotHeritage := expanded.AsJSDoc().Tags.Nodes[1].AsHeritageClause()
	assert.Equal(t, KindExtendsKeyword, gotHeritage.Token)
	gotLiteral := gotHeritage.Types.Nodes[0].AsNumericLiteral()
	assert.Equal(t, "0x10", gotLiteral.Text)
	assert.Equal(t, TokenFlagsHexSpecifier, gotLiteral.TokenFlags)
}

func TestStoreRetainsSourceFileMetadataOwner(t *testing.T) {
	t.Parallel()
	f := NewNodeFactory(NodeFactoryHooks{})
	store := NewStore(2)
	f.AttachStore(store)
	opts := SourceFileParseOptions{FileName: "/index.ts", Path: "/index.ts"}
	node := f.NewSourceFile(opts, "const x = 1", nil, nil)
	file := node.AsSourceFile()
	file.ScriptKind = core.ScriptKindTS
	file.IdentifierCount = 17
	f.StoreSync(node)
	file.SetParseStore(store, f.HandleOf(node))

	assert.Equal(t, file, store.SourceFile())
	assert.Equal(t, "/index.ts", store.SourceFile().FileName())
	assert.Equal(t, core.ScriptKindTS, store.SourceFile().ScriptKind)
	assert.Equal(t, 17, store.SourceFile().IdentifierCount)
}
