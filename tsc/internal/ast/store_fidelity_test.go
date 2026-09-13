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

func TestStoreForEachChildUsesSchemaOrder(t *testing.T) {
	t.Parallel()
	f := NewFactory(FactoryHooks{})
	modifier := f.NewIdentifier("modifier")
	parameter := f.NewIdentifier("parameter")
	typeNode := f.NewIdentifier("type")
	node := f.NewIndexSignatureDeclaration(
		f.List(core.UndefinedTextRange(), modifier),
		f.List(core.UndefinedTextRange(), parameter),
		typeNode,
	)

	assertForEachChildOrder(t, node, modifier, parameter, typeNode)
}

func TestStoreForEachChildIncludesSourceFileInSchemaOrder(t *testing.T) {
	t.Parallel()
	f := NewFactory(FactoryHooks{})
	statement := f.NewIdentifier("statement")
	eof := f.NewToken(KindEndOfFile)
	node := f.NewSourceFile(f.List(core.UndefinedTextRange(), statement), eof)

	assertForEachChildOrder(t, node, statement, eof)
}

func TestStoreForEachChildUsesJSDocRuntimeOrder(t *testing.T) {
	t.Parallel()
	f := NewFactory(FactoryHooks{})
	tagName := f.NewIdentifier("tag")
	name := f.NewIdentifier("name")
	typeExpression := f.NewIdentifier("type")
	comment := f.NewIdentifier("comment")
	comments := f.List(core.UndefinedTextRange(), comment)

	nameFirst := f.NewJSDocParameterOrPropertyTag(KindJSDocParameterTag, tagName, name, false, typeExpression, true, comments)
	typeFirst := f.NewJSDocParameterOrPropertyTag(KindJSDocPropertyTag, tagName, name, false, typeExpression, false, comments)

	assertForEachChildOrder(t, nameFirst, tagName, name, typeExpression, comment)
	assertForEachChildOrder(t, typeFirst, tagName, typeExpression, name, comment)
}

func TestStoreForEachChildStopsEarly(t *testing.T) {
	t.Parallel()
	f := NewFactory(FactoryHooks{})
	first := f.NewIdentifier("first")
	second := f.NewIdentifier("second")
	third := f.NewIdentifier("third")
	node := f.NewIndexSignatureDeclaration(
		f.List(core.UndefinedTextRange(), first),
		f.List(core.UndefinedTextRange(), second),
		third,
	)
	visited := make([]Handle, 0, 2)
	stopped := node.ForEachChild(func(child Handle) bool {
		visited = append(visited, child)
		return child == second
	})

	assert.Assert(t, stopped)
	assert.Equal(t, len(visited), 2)
	assert.Equal(t, visited[0].Ref(), first.Ref())
	assert.Equal(t, visited[1].Ref(), second.Ref())
}

func TestStoreForEachChildResolvesForeignLists(t *testing.T) {
	owner := NewStore(4)
	foreign := NewStore(4)
	RegisterStore(owner)
	RegisterStore(foreign)
	defer UnregisterStore(owner)
	defer UnregisterStore(foreign)

	loc := core.UndefinedTextRange()
	foreignChild := foreign.Alloc(KindIdentifier, 0, loc, 0)
	foreignList := foreign.AllocList(loc, 1)
	foreign.SetListAt(foreignList, 0, foreignChild)
	typeNode := owner.Alloc(KindIdentifier, 0, loc, 0)
	node := owner.AllocSlots(KindIndexSignature, 0, loc, slotIndexSignatureDeclarationCount, listSlotIndexSignatureDeclarationCount)
	node.SetListSlot(listSlotIndexSignatureDeclarationModifiers, foreignList)
	node.SetChild(slotIndexSignatureDeclarationType, typeNode)

	assertForEachChildOrder(t, node, foreignChild, typeNode)
}

func assertForEachChildOrder(t *testing.T, node Handle, want ...Handle) {
	t.Helper()
	got := make([]Handle, 0, len(want))
	stopped := node.ForEachChild(func(child Handle) bool {
		got = append(got, child)
		return false
	})
	assert.Assert(t, !stopped)
	assert.Equal(t, len(got), len(want))
	for i := range want {
		assert.Assert(t, got[i] == want[i], "child %d: got %v want %v", i, got[i], want[i])
	}
}
