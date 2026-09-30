package storechecker

import (
	"github.com/microsoft/TypeScript/tsc/internal/ast/store"
	"github.com/microsoft/TypeScript/tsc/internal/core"
)

// The P2 declarations of checker/inference.go:
// internal/ast/docs/store-checker-7d2a-implementation-instructions-20260929.md
// 3.4.

// createReverseMappedType maps the TupleElementInfos with core.SameMap, which
// compares with ==: TupleElementInfo holds a node, which is not comparable in
// the Store (Z1), so the loop of SameMap is written out with SameNode (R6).
//
// storetranslate:pointer-hash 623486b18b22b15f inference.go:Checker.createReverseMappedType
func (c *Checker) createReverseMappedType(source *Type, target *Type, constraint *Type) *Type {
	// We consider a source type reverse mappable if it has a string index signature or if
	// it has one or more properties and is of a partially inferable type.
	if !(c.getIndexInfoOfType(source, c.stringType) != nil || len(c.getPropertiesOfType(source)) != 0 && c.isPartiallyInferableType(source)) {
		return nil
	}
	// For arrays and tuples we infer new arrays and tuples where the reverse mapping has been
	// applied to the element type(s).
	if c.isArrayType(source) {
		elementType := c.inferReverseMappedType(c.getTypeArguments(source)[0], target, constraint)
		if elementType == nil {
			return nil
		}
		return c.createArrayTypeEx(elementType, c.isReadonlyArrayType(source))
	}
	if isTupleType(source) {
		elementTypes := core.Map(c.getElementTypes(source), func(t *Type) *Type {
			return c.inferReverseMappedType(t, target, constraint)
		})
		if !core.Every(elementTypes, func(t *Type) bool { return t != nil }) {
			return nil
		}
		elementInfos := source.TargetTupleType().elementInfos
		if getMappedTypeModifiers(target)&MappedTypeModifiersIncludeOptional != 0 {
			mapInfo := func(info TupleElementInfo) TupleElementInfo {
				if info.flags&ElementFlagsOptional != 0 {
					return TupleElementInfo{flags: ElementFlagsRequired, labeledDeclaration: info.labeledDeclaration}
				}
				return info
			}
			// core.SameMap, with the struct's == written out: its node is
			// compared with SameNode.
			for i, info := range elementInfos {
				mapped := mapInfo(info)
				if mapped.flags != info.flags || !store.SameNode(mapped.labeledDeclaration, info.labeledDeclaration) {
					result := make([]TupleElementInfo, len(elementInfos))
					copy(result, elementInfos[:i])
					result[i] = mapped
					for j := i + 1; j < len(elementInfos); j++ {
						result[j] = mapInfo(elementInfos[j])
					}
					elementInfos = result
					break
				}
			}
		}
		return c.createTupleTypeEx(elementTypes, elementInfos, source.TargetTupleType().readonly)
	}
	// For all other object types we infer a new object type where the reverse mapping has been
	// applied to the type of each property.
	reversed := c.newObjectType(ObjectFlagsReverseMapped|ObjectFlagsAnonymous, nil /*symbol*/)
	reversed.AsReverseMappedType().source = source
	reversed.AsReverseMappedType().mappedType = target
	reversed.AsReverseMappedType().constraintType = constraint
	return reversed
}
