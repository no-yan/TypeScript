package ast

type FunctionFlags uint32

const (
	FunctionFlagsNormal         FunctionFlags = 0
	FunctionFlagsGenerator      FunctionFlags = 1 << 0
	FunctionFlagsAsync          FunctionFlags = 1 << 1
	FunctionFlagsInvalid        FunctionFlags = 1 << 2
	FunctionFlagsAsyncGenerator FunctionFlags = FunctionFlagsAsync | FunctionFlagsGenerator
)

func GetFunctionFlags(node Handle) FunctionFlags {
	if node.IsNil() {
		return FunctionFlagsInvalid
	}
	flags := FunctionFlagsNormal
	switch node.Kind {
	case KindFunctionDeclaration, KindFunctionExpression, KindMethodDeclaration:
		if !node.AsteriskToken().IsNil() {
			flags |= FunctionFlagsGenerator
		}
		fallthrough
	case KindArrowFunction:
		if node.ModifierFlags()&ModifierFlagsAsync != 0 {
			flags |= FunctionFlagsAsync
		}
	default:
		return FunctionFlagsInvalid
	}
	if node.Body().IsNil() {
		flags |= FunctionFlagsInvalid
	}
	return flags
}
