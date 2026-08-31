package ast

import (
	"testing"

	"github.com/microsoft/TypeScript/tsc/internal/core"
	"gotest.tools/v3/assert"
)

func TestStorePreservesKindValuesTokenFlagsJSDocAndParents(t *testing.T) {
	t.Parallel()
	f := NewNodeFactory(NodeFactoryHooks{})
	store := NewStore(8)
	f.AttachStore(store)

	literal := f.NewNumericLiteral("0x10", TokenFlagsHexSpecifier)
	template := f.NewTemplateHead("cooked", "raw\\n", TokenFlagsContainsInvalidEscape)
	heritage := f.NewHeritageClause(KindExtendsKeyword, f.NewNodeList([]*Node{literal}))
	jsdocText := f.NewJSDocText([]string{"first", "second"})
	root := f.NewJSDoc(
		f.NewNodeList([]*Node{jsdocText}),
		f.NewNodeList([]*Node{template, heritage}),
	)
	root.Loc = core.NewTextRange(0, 20)
	f.StoreSync(heritage)
	f.StoreSync(root)

	rootHandle := f.HandleOf(root)
	literalHandle := f.HandleOf(literal)
	templateHandle := f.HandleOf(template)
	heritageHandle := f.HandleOf(heritage)
	textHandle := f.HandleOf(jsdocText)
	assert.Equal(t, rootHandle.Ref(), textHandle.Parent().Ref())
	assert.Equal(t, heritageHandle.Ref(), literalHandle.Parent().Ref())
	assert.Equal(t, "0x10", literalHandle.StringValue(valueSlotNumericLiteralText))
	assert.Equal(t, TokenFlagsHexSpecifier, literalHandle.TokenFlags())
	assert.Equal(t, "cooked", templateHandle.StringValue(valueSlotTemplateHeadText))
	assert.Equal(t, "raw\\n", templateHandle.StringValue(valueSlotTemplateHeadRawText))
	assert.Equal(t, TokenFlagsContainsInvalidEscape, templateHandle.TokenFlags())
	assert.Equal(t, uint64(KindExtendsKeyword), heritageHandle.UintValue(valueSlotHeritageClauseToken))
	assert.DeepEqual(t, []string{"first", "second"}, storeObjectValue[[]string](textHandle, valueSlotJSDocTextText))
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

func TestStoreBridgePreservesCrossFileChildrenAsGlobalRefs(t *testing.T) {
	storeA := NewStore(4)
	factoryA := NewNodeFactory(NodeFactoryHooks{})
	factoryA.AttachStore(storeA)
	child := factoryA.NewIdentifier("external")
	opts := SourceFileParseOptions{FileName: "/a.ts", Path: "/a.ts"}
	sourceNode := factoryA.NewSourceFile(opts, "external", factoryA.NewNodeList([]*Node{child}), nil)
	child.Parent = sourceNode
	sourceFile := sourceNode.AsSourceFile()
	sourceFile.SetParseStore(storeA, factoryA.HandleOf(sourceNode))
	sourceFile.SetParseNodeRef(factoryA.TakeNodeRef())
	RegisterFile(sourceFile)

	storeB := NewStore(4)
	factoryB := NewNodeFactory(NodeFactoryHooks{})
	factoryB.AttachStore(storeB)
	synthetic := factoryB.NewSyntheticExpression(nil, false, child)
	syntheticHandle := factoryB.HandleOf(synthetic)

	assert.Equal(t, sourceFile.RefOf(child), syntheticHandle.ExternalChild(slotSyntheticExpressionTupleNameSource))
	assert.Equal(t, sourceNode, child.Parent)

	jsdoc := factoryB.NewJSDoc(factoryB.NewNodeList([]*Node{child}), nil)
	jsdocHandle := factoryB.HandleOf(jsdoc)
	list := jsdocHandle.ListSlot(listSlotJSDocComment)
	assert.Equal(t, sourceFile.RefOf(child), storeB.ExternalListAt(list, 0))
	assert.Equal(t, sourceNode, child.Parent)
}

func TestStoreAwareUpdateReusesEquivalentNodeRefs(t *testing.T) {
	t.Parallel()
	plain := NewNodeFactory(NodeFactoryHooks{})
	left := plain.NewIdentifier("left")
	equivalentLeft := plain.NewIdentifier("left")
	right := plain.NewIdentifier("right")
	original := plain.NewQualifiedName(left, right)

	store := NewStore(4)
	leftHandle := store.Alloc(KindIdentifier, 0, core.UndefinedTextRange(), 0)
	rightHandle := store.Alloc(KindIdentifier, 0, core.UndefinedTextRange(), 0)
	factory := NewNodeFactory(NodeFactoryHooks{})
	factory.AttachStoreMap(store, map[*Node]NodeRef{
		left:           leftHandle.Ref(),
		equivalentLeft: leftHandle.Ref(),
		right:          rightHandle.Ref(),
	})

	updated := factory.UpdateQualifiedName(original.AsQualifiedName(), equivalentLeft, right)
	assert.Equal(t, original, updated)
}
