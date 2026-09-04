package tstransforms

import (
	"github.com/microsoft/TypeScript/tsc/internal/ast"
	"github.com/microsoft/TypeScript/tsc/internal/core"
	"github.com/microsoft/TypeScript/tsc/internal/printer"
	"github.com/microsoft/TypeScript/tsc/internal/transformers"
)

const USE_NEW_TYPE_METADATA_FORMAT = false

type MetadataTransformer struct {
	transformers.Transformer
	legacyDecorators    bool
	resolver            printer.EmitResolver
	serializer          *metadataSerializer
	languageVersion     core.ScriptTarget
	strictNullChecks    bool
	parent              ast.Handle
	currentLexicalScope ast.Handle
}

func NewMetadataTransformer(opt *transformers.TransformOptions) *transformers.Transformer {
	tx := &MetadataTransformer{legacyDecorators: opt.CompilerOptions.ExperimentalDecorators.IsTrue(), resolver: opt.EmitResolver, languageVersion: opt.CompilerOptions.GetEmitScriptTarget(), strictNullChecks: opt.CompilerOptions.GetStrictOptionValue(opt.CompilerOptions.StrictNullChecks)}
	return tx.NewTransformer(tx.visit, opt.Context)
}
func (tx *MetadataTransformer) visit(node ast.Handle) ast.Handle {
	if (node.SubtreeFacts() & ast.SubtreeContainsDecorators) == 0 {
		return node
	}
	switch node.Kind {
	case ast.KindClassDeclaration:
		return tx.visitClassDeclaration(node)
	case ast.KindClassExpression:
		return tx.visitClassExpression(node)
	case ast.KindPropertyDeclaration:
		return tx.visitPropertyDeclaration(node)
	case ast.KindMethodDeclaration:
		return tx.visitMethodDeclaration(node)
	case ast.KindSetAccessor:
		return tx.visitSetAccessor(node)
	case ast.KindGetAccessor:
		return tx.visitGetAccessor(node)
	case ast.KindSourceFile:
		tx.parent = ast.Handle{}
		defer tx.setParent(ast.Handle{})
		tx.currentLexicalScope = node
		defer tx.setCurrentLexicalScope(ast.Handle{})
		tx.serializer = newMetadataSerializer(tx.resolver, tx.Factory(), tx.EmitContext(), tx.languageVersion, tx.strictNullChecks)
		updated := tx.Visitor().VisitEachChild(node)
		tx.EmitContext().AddEmitHelper(updated, tx.EmitContext().ReadEmitHelpers()...)
		return updated
	case ast.KindModuleBlock, ast.KindBlock, ast.KindCaseBlock:
		oldScope := tx.currentLexicalScope
		tx.currentLexicalScope = node
		defer tx.setCurrentLexicalScope(oldScope)
		return tx.Visitor().VisitEachChild(node)
	default:
		return tx.Visitor().VisitEachChild(node)
	}
}
func (tx *MetadataTransformer) setParent(node ast.Handle) {
	tx.parent = node
}
func (tx *MetadataTransformer) setCurrentLexicalScope(node ast.Handle) {
	tx.currentLexicalScope = node
}
func (tx *MetadataTransformer) visitClassExpression(node ast.Handle) ast.Handle {
	oldParent := tx.parent
	tx.parent = node
	defer tx.setParent(oldParent)
	if !ast.ClassOrConstructorParameterIsDecorated(tx.legacyDecorators, node) {
		return tx.Visitor().VisitEachChild(node)
	}
	modifiers := tx.injectClassTypeMetadata(tx.Visitor().VisitModifiers(node.Modifiers()), node)
	return tx.Factory().UpdateClassExpression(node, modifiers, tx.Visitor().VisitNode(node.Name()), tx.Visitor().VisitNodes(node.TypeParameterList()), tx.Visitor().VisitNodes(node.HeritageClauses()), tx.Visitor().VisitNodes(node.MemberList()))
}
func (tx *MetadataTransformer) visitClassDeclaration(node ast.Handle) ast.Handle {
	oldParent := tx.parent
	tx.parent = node
	defer tx.setParent(oldParent)
	if !ast.ClassOrConstructorParameterIsDecorated(tx.legacyDecorators, node) {
		return tx.Visitor().VisitEachChild(node)
	}
	modifiers := tx.injectClassTypeMetadata(tx.Visitor().VisitModifiers(node.Modifiers()), node)
	return tx.Factory().UpdateClassDeclaration(node, modifiers, tx.Visitor().VisitNode(node.Name()), tx.Visitor().VisitNodes(node.TypeParameterList()), tx.Visitor().VisitNodes(node.HeritageClauses()), tx.Visitor().VisitNodes(node.MemberList()))
}
func (tx *MetadataTransformer) visitPropertyDeclaration(node ast.Handle) ast.Handle {
	if !ast.HasDecorators(node) {
		return tx.Visitor().VisitEachChild(node)
	}
	modifiers := tx.injectClassElementTypeMetadata(tx.Visitor().VisitModifiers(node.Modifiers()), node, tx.parent)
	return tx.Factory().UpdatePropertyDeclaration(node, modifiers, tx.Visitor().VisitNode(node.Name()), tx.Visitor().VisitNode(node.PostfixToken()), tx.Visitor().VisitNode(node.Type()), tx.Visitor().VisitNode(node.Initializer()))
}
func (tx *MetadataTransformer) visitMethodDeclaration(node ast.Handle) ast.Handle {
	if !ast.HasDecorators(node) && len(getDecoratorsOfParameters(node)) == 0 {
		return tx.Visitor().VisitEachChild(node)
	}
	modifiers := tx.injectClassElementTypeMetadata(tx.Visitor().VisitModifiers(node.Modifiers()), node, tx.parent)
	return tx.Factory().UpdateMethodDeclaration(node, modifiers, tx.Visitor().VisitNode(node.AsteriskToken()), tx.Visitor().VisitNode(node.Name()), tx.Visitor().VisitNode(node.PostfixToken()), tx.Visitor().VisitNodes(node.TypeParameterList()), tx.Visitor().VisitNodes(node.ParameterList()), tx.Visitor().VisitNode(node.Type()), tx.Visitor().VisitNode(node.FullSignature()), tx.Visitor().VisitNode(node.Body()))
}
func (tx *MetadataTransformer) visitSetAccessor(node ast.Handle) ast.Handle {
	if !ast.HasDecorators(node) && len(getDecoratorsOfParameters(node)) == 0 {
		return tx.Visitor().VisitEachChild(node)
	}
	modifiers := tx.injectClassElementTypeMetadata(tx.Visitor().VisitModifiers(node.Modifiers()), node, tx.parent)
	return tx.Factory().UpdateSetAccessorDeclaration(node, modifiers, tx.Visitor().VisitNode(node.Name()), tx.Visitor().VisitNodes(node.TypeParameterList()), tx.Visitor().VisitNodes(node.ParameterList()), tx.Visitor().VisitNode(node.Type()), tx.Visitor().VisitNode(node.FullSignature()), tx.Visitor().VisitNode(node.Body()))
}
func (tx *MetadataTransformer) visitGetAccessor(node ast.Handle) ast.Handle {
	if !ast.HasDecorators(node) {
		return tx.Visitor().VisitEachChild(node)
	}
	modifiers := tx.injectClassElementTypeMetadata(tx.Visitor().VisitModifiers(node.Modifiers()), node, tx.parent)
	return tx.Factory().UpdateGetAccessorDeclaration(node, modifiers, tx.Visitor().VisitNode(node.Name()), tx.Visitor().VisitNodes(node.TypeParameterList()), tx.Visitor().VisitNodes(node.ParameterList()), tx.Visitor().VisitNode(node.Type()), tx.Visitor().VisitNode(node.FullSignature()), tx.Visitor().VisitNode(node.Body()))
}
func (tx *MetadataTransformer) injectClassTypeMetadata(list ast.ListRef, node ast.Handle) ast.ListRef {
	metadata := tx.getTypeMetadata(node, node)
	if len(metadata) > 0 {
		var originalNodes []ast.Handle
		if list != 0 {
			originalNodes = node.Store().ListSlice(list).Slice()
		}
		if len(originalNodes) == 0 {
			res := tx.Factory().NewModifierList(metadata)
			if list != 0 {
				res = tx.Factory().RelocateList(res, node.Store().ListLoc(list))
			}
			return res
		}
		var modifiersArray []ast.Handle
		if ast.IsModifier(originalNodes[0]) && (originalNodes[0].Kind == ast.KindDefaultKeyword || originalNodes[0].Kind == ast.KindExportKeyword) {
			modifiersArray = append(modifiersArray, originalNodes[0])
			if len(originalNodes) > 1 && (originalNodes[1].Kind == ast.KindDefaultKeyword || originalNodes[1].Kind == ast.KindExportKeyword) {
				modifiersArray = append(modifiersArray, originalNodes[1])
			}
		}
		restStart := len(modifiersArray)
		decos := core.Filter(originalNodes, ast.IsDecorator)
		modifiersArray = append(modifiersArray, decos...)
		modifiersArray = append(modifiersArray, metadata...)
		otherModifiers := core.Filter(originalNodes[restStart:], ast.IsModifier)
		modifiersArray = append(modifiersArray, otherModifiers...)
		res := tx.Factory().NewModifierList(modifiersArray)
		res = tx.Factory().RelocateList(res, node.Store().ListLoc(list))
		return res
	}
	return list
}
func (tx *MetadataTransformer) injectClassElementTypeMetadata(list ast.ListRef, node ast.Handle, container ast.Handle) ast.ListRef {
	if !ast.IsClassLike(container) {
		return list
	}
	if !ast.ClassElementOrClassElementParameterIsDecorated(tx.legacyDecorators, node, container) {
		return list
	}
	metadata := tx.getTypeMetadata(node, container)
	if len(metadata) > 0 {
		var originalNodes []ast.Handle
		if list != 0 {
			originalNodes = node.Store().ListSlice(list).Slice()
		}
		if len(originalNodes) == 0 {
			res := tx.Factory().NewModifierList(metadata)
			if list != 0 {
				res = tx.Factory().RelocateList(res, node.Store().ListLoc(list))
			}
			return res
		}
		var modifiersArray []ast.Handle
		decos := core.Filter(originalNodes, ast.IsDecorator)
		modifiersArray = append(modifiersArray, decos...)
		modifiersArray = append(modifiersArray, metadata...)
		modifiers := core.Filter(originalNodes, ast.IsModifier)
		modifiersArray = append(modifiersArray, modifiers...)
		res := tx.Factory().NewModifierList(modifiersArray)
		res = tx.Factory().RelocateList(res, node.Store().ListLoc(list))
		return res
	}
	return list
}

func (tx *MetadataTransformer) getTypeMetadata(node ast.Handle, container ast.Handle) []ast.Handle {
	if !tx.legacyDecorators {
		return nil
	}
	if USE_NEW_TYPE_METADATA_FORMAT {
		return tx.getNewTypeMetadata(node, container)
	}
	return tx.getOldTypeMetadata(node, container)
}
func (tx *MetadataTransformer) getOldTypeMetadata(node ast.Handle, container ast.Handle) []ast.Handle {
	var decorators []ast.Handle
	if tx.shouldAddTypeMetadata(node) {
		typeMetadata := tx.Factory().NewMetadataHelper("design:type", tx.serializer.SerializeTypeOfNode(metadataSerializerContext{currentLexicalScope: tx.currentLexicalScope, currentNameScope: container}, node, container))
		decorators = append(decorators, tx.Factory().NewDecorator(typeMetadata))
	}
	if tx.shouldAddParamTypesMetadata(node) {
		paramTypesMetadata := tx.Factory().NewMetadataHelper("design:paramtypes", tx.serializer.SerializeParameterTypesOfNode(metadataSerializerContext{currentLexicalScope: tx.currentLexicalScope, currentNameScope: container}, node, container))
		decorators = append(decorators, tx.Factory().NewDecorator(paramTypesMetadata))
	}
	if tx.shouldAddReturnTypeMetadata(node) {
		returnTypeMetadata := tx.Factory().NewMetadataHelper("design:returntype", tx.serializer.SerializeReturnTypeOfNode(metadataSerializerContext{currentLexicalScope: tx.currentLexicalScope, currentNameScope: container}, node))
		decorators = append(decorators, tx.Factory().NewDecorator(returnTypeMetadata))
	}
	return decorators
}
func (tx *MetadataTransformer) getNewTypeMetadata(node ast.Handle, container ast.Handle) []ast.Handle {
	var properties []ast.Handle
	if tx.shouldAddTypeMetadata(node) {
		properties = append(properties, tx.Factory().NewPropertyAssignment(0, tx.Factory().NewIdentifier("type"), ast.Handle{}, ast.Handle{}, tx.Factory().NewArrowFunction(0, 0, tx.Factory().NewList([]ast.Handle{}), ast.Handle{}, ast.Handle{}, tx.Factory().NewToken(ast.KindEqualsGreaterThanToken), tx.serializer.SerializeTypeOfNode(metadataSerializerContext{currentLexicalScope: tx.currentLexicalScope, currentNameScope: container}, node, container))))
	}
	if tx.shouldAddParamTypesMetadata(node) {
		properties = append(properties, tx.Factory().NewPropertyAssignment(0, tx.Factory().NewIdentifier("paramTypes"), ast.Handle{}, ast.Handle{}, tx.Factory().NewArrowFunction(0, 0, tx.Factory().NewList([]ast.Handle{}), ast.Handle{}, ast.Handle{}, tx.Factory().NewToken(ast.KindEqualsGreaterThanToken), tx.serializer.SerializeParameterTypesOfNode(metadataSerializerContext{currentLexicalScope: tx.currentLexicalScope, currentNameScope: container}, node, container))))
	}
	if tx.shouldAddReturnTypeMetadata(node) {
		properties = append(properties, tx.Factory().NewPropertyAssignment(0, tx.Factory().NewIdentifier("returnType"), ast.Handle{}, ast.Handle{}, tx.Factory().NewArrowFunction(0, 0, tx.Factory().NewList([]ast.Handle{}), ast.Handle{}, ast.Handle{}, tx.Factory().NewToken(ast.KindEqualsGreaterThanToken), tx.serializer.SerializeReturnTypeOfNode(metadataSerializerContext{currentLexicalScope: tx.currentLexicalScope, currentNameScope: container}, node))))
	}
	if len(properties) > 0 {
		typeInfoMetadata := tx.Factory().NewMetadataHelper("design:typeinfo", tx.Factory().NewObjectLiteralExpression(tx.Factory().NewList(properties), true))
		return []ast.Handle{tx.Factory().NewDecorator(typeInfoMetadata)}
	}
	return nil
}

func (tx *MetadataTransformer) shouldAddTypeMetadata(node ast.Handle) bool {
	switch node.Kind {
	case ast.KindMethodDeclaration, ast.KindGetAccessor, ast.KindSetAccessor, ast.KindPropertyDeclaration:
		return true
	}
	return false
}

func (tx *MetadataTransformer) shouldAddReturnTypeMetadata(node ast.Handle) bool {
	return node.Kind == ast.KindMethodDeclaration
}

func (tx *MetadataTransformer) shouldAddParamTypesMetadata(node ast.Handle) bool {
	switch node.Kind {
	case ast.KindClassDeclaration, ast.KindClassExpression:
		return !ast.GetFirstConstructorWithBody(node).IsNil()
	case ast.KindMethodDeclaration, ast.KindGetAccessor, ast.KindSetAccessor:
		return true
	}
	return false
}
