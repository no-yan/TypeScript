package ast

import (
	"testing"

	"github.com/microsoft/TypeScript/tsc/internal/core"
	"gotest.tools/v3/assert"
)

func TestStorePreservesKindValuesTokenFlagsJSDocAndParents(t *testing.T) {
	t.Parallel()
	f := NewFactory(FactoryHooks{})

	literal := f.NewNumericLiteral("0x10", TokenFlagsHexSpecifier)
	template := f.NewTemplateHead("cooked", "raw\\n", TokenFlagsContainsInvalidEscape)
	heritage := f.NewHeritageClause(KindExtendsKeyword, f.List(core.UndefinedTextRange(), literal))
	jsdocText := f.NewJSDocText([]string{"first", "second"})
	root := f.NewJSDoc(
		f.List(core.UndefinedTextRange(), jsdocText),
		f.List(core.UndefinedTextRange(), template, heritage),
	)
	root.SetLoc(core.NewTextRange(0, 20))
	root.SetParentsInChildren()

	assert.Equal(t, root.Ref(), jsdocText.Parent().Ref())
	assert.Equal(t, heritage.Ref(), literal.Parent().Ref())
	assert.Equal(t, "0x10", literal.StringValue(valueSlotNumericLiteralText))
	assert.Equal(t, TokenFlagsHexSpecifier, literal.TokenFlags())
	assert.Equal(t, "cooked", template.StringValue(valueSlotTemplateHeadText))
	assert.Equal(t, "raw\\n", template.StringValue(valueSlotTemplateHeadRawText))
	assert.Equal(t, TokenFlagsContainsInvalidEscape, template.TokenFlags())
	assert.Equal(t, uint64(KindExtendsKeyword), heritage.UintValue(valueSlotHeritageClauseToken))
	assert.DeepEqual(t, []string{"first", "second"}, storeObjectValue[[]string](jsdocText, valueSlotJSDocTextText))
}

func TestStoreRetainsSourceFileMetadataOwner(t *testing.T) {
	t.Parallel()
	f := NewFactory(FactoryHooks{})
	opts := SourceFileParseOptions{FileName: "/index.ts", Path: "/index.ts"}
	eof := f.NewToken(KindEndOfFile)
	root := f.NewSourceFile(f.List(core.UndefinedTextRange()), eof)
	file := NewSourceFileMetadata(opts, "const x = 1")
	file.ScriptKind = core.ScriptKindTS
	file.IdentifierCount = 17
	file.SetParseStore(f.Store(), root)

	assert.Equal(t, file, f.Store().SourceFile())
	assert.Equal(t, "/index.ts", f.Store().SourceFile().FileName())
	assert.Equal(t, core.ScriptKindTS, f.Store().SourceFile().ScriptKind)
	assert.Equal(t, 17, f.Store().SourceFile().IdentifierCount)
}

func TestCloneWrapperDoesNotRebindStoreOrProgramFlags(t *testing.T) {
	t.Parallel()
	f := NewFactory(FactoryHooks{})
	opts := SourceFileParseOptions{FileName: "/index.ts", Path: "/index.ts"}
	eof := f.NewToken(KindEndOfFile)
	root := f.NewSourceFile(f.List(core.UndefinedTextRange()), eof)
	file := NewSourceFileMetadata(opts, "export const x = 1")
	file.SetParseStore(f.Store(), root)
	file.ReferencedFiles = []*FileReference{{FileName: "keep.ts"}}

	view := file.CloneWrapper()
	view.IsDeclarationFile = true
	view.ReferencedFiles = nil
	view.SetParseRoot(root)

	assert.Equal(t, file, f.Store().SourceFile())
	assert.Equal(t, false, file.IsDeclarationFile)
	assert.Equal(t, 1, len(file.ReferencedFiles))
	assert.Equal(t, file.ParseStore(), view.ParseStore())
	assert.Equal(t, file.ParseRoot(), view.ParseRoot())
}

func TestCloneWrapperKeepsParseIdentifierSet(t *testing.T) {
	t.Parallel()
	f := NewFactory(FactoryHooks{})
	opts := SourceFileParseOptions{FileName: "/index.ts", Path: "/index.ts"}
	foo := f.NewIdentifier("foo")
	stmt := f.NewExpressionStatement(foo)
	eof := f.NewToken(KindEndOfFile)
	root := f.NewSourceFile(f.List(core.UndefinedTextRange(), stmt), eof)
	root.SetParentsInChildren()
	file := NewSourceFileMetadata(opts, "foo")
	file.SetParseStore(f.Store(), root)
	file.RecordParseIdentifiers()

	assert.Equal(t, true, file.HasIdentifier("foo"))
	assert.Equal(t, false, file.HasIdentifier("_jsx"))

	emitFactory := NewFactoryOn(f.Store(), FactoryHooks{OnCreate: func(h Handle) {
		h.SetFlags(h.Flags() | NodeFlagsSynthesized)
	}})
	jsx := emitFactory.NewIdentifier("_jsx")
	emitRoot := emitFactory.NewSourceFile(emitFactory.List(core.UndefinedTextRange(), emitFactory.NewExpressionStatement(jsx)), eof)
	view := file.CloneWrapper()
	view.SetParseRoot(emitRoot)

	assert.Equal(t, true, view.HasIdentifier("foo"))
	assert.Equal(t, false, view.HasIdentifier("_jsx"))
	assert.Equal(t, false, file.HasIdentifier("_jsx"))
}

func TestCallExpressionSubtreeContainsIdentifier(t *testing.T) {
	t.Parallel()
	f := NewFactory(FactoryHooks{OnCreate: func(h Handle) {
		h.SetFlags(h.Flags() | NodeFlagsSynthesized)
	}})
	RegisterStore(f.Store())
	call := f.NewCallExpression(f.NewIdentifier("_jsx"), Handle{}, 0, f.List(core.UndefinedTextRange(), f.NewStringLiteral("div", TokenFlagsNone)), NodeFlagsNone)
	assert.Assert(t, call.SubtreeFacts()&SubtreeContainsIdentifier != 0, "call facts=%#x", call.SubtreeFacts())
}

func TestPrimaryStringValueUsesCanonicalTextColumn(t *testing.T) {
	t.Parallel()
	store := NewStore(2)
	identifier := store.Alloc(KindIdentifier, 0, core.UndefinedTextRange(), 0)

	identifier.SetStringValue(valueSlotIdentifierText, "first")
	assert.Equal(t, "first", identifier.StringValue(valueSlotIdentifierText))
	assert.Equal(t, "first", identifier.Ident())

	identifier.SetStringValue(valueSlotIdentifierText, "second")
	assert.Equal(t, "second", identifier.StringValue(valueSlotIdentifierText))
	identifier.SetStringValue(valueSlotIdentifierText, "")
	assert.Equal(t, "", identifier.StringValue(valueSlotIdentifierText))
	assert.Equal(t, "", identifier.Ident())
}
