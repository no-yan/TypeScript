package storepseudochecker

import "github.com/microsoft/TypeScript/tsc/internal/ast/store"

// The P2 declarations of pseudochecker/lookup.go:
// internal/ast/docs/store-checker-7d2a-implementation-instructions-20260929.md
// 3.4.

// createReturnFromSignature reads the member of FunctionLikeData on the node:
// a lone FunctionLikeData() has no Store form (4.1).
//
// storetranslate:pointer-hash ddfe892849aa7cd3 lookup.go:PseudoChecker.createReturnFromSignature
func (ch *PseudoChecker) createReturnFromSignature(fn store.Node) *PseudoType {
	if store.IsFunctionLike(fn) {
		// !!! TODO: support ripping return type off of .FullSignature
		r := fn.Type()
		if !r.IsNil() {
			return NewPseudoTypeDirect(r)
		}
	}
	if isValueSignatureDeclaration(fn) {
		return ch.typeFromSingleReturnExpression(fn)
	}
	return NewPseudoTypeNoResult(fn)
}

// lastRequiredParamIndexList is lastRequiredParamIndex on a list (3.4, List
// version): slices.Backward over the list is a loop down from Len()-1.
//
// storetranslate:pointer-hash b3f068aab09d0906 lookup.go:lastRequiredParamIndex
func lastRequiredParamIndexList(params store.List) int {
	for i := params.Len() - 1; i >= 0; i-- {
		param := params.At(i)
		if !isOptionalInitializedOrRestParameter(param) {
			return i + 1
		}
	}
	return 0
}
