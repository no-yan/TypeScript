package binder

import (
	"strconv"
	"strings"
	"testing"

	"github.com/microsoft/TypeScript/tsc/internal/ast"
	"github.com/microsoft/TypeScript/tsc/internal/core"
	"github.com/microsoft/TypeScript/tsc/internal/parser"
)

var parameterPropertyChecks uint64

// BenchmarkBinderParameterPropertyModifierCheck compares the current repeated
// modifier scan with the per-Parameter syntax fact on a declaration-shaped
// input containing ordinary and parameter-property constructor arguments.
func BenchmarkBinderParameterPropertyModifierCheck(b *testing.B) {
	var source strings.Builder
	for classIndex := range 128 {
		source.WriteString("class C")
		source.WriteString(strconv.Itoa(classIndex))
		source.WriteString(" { constructor(public id: number, private readonly name: string, protected active: boolean, plain: number, optional?: string, rest: unknown) {} }\n")
	}
	file := parser.ParseSourceFile(ast.SourceFileParseOptions{FileName: "/parameter-properties.ts", Path: "/parameter-properties.ts"}, source.String(), core.ScriptKindTS)
	store := file.ParseStore()
	type parameterSample struct {
		parameter ast.NodeRef
		parent    ast.NodeRef
	}
	samples := make([]parameterSample, 0, 128*6)
	ast.Walk(file.ParseRoot(), func(node ast.Handle) bool {
		if node.Kind == ast.KindParameter {
			samples = append(samples, parameterSample{parameter: node.Ref(), parent: store.ParentRef(node.Ref())})
		}
		return false
	})

	b.Run("modifier-scan", func(b *testing.B) {
		var matched uint64
		b.ResetTimer()
		for range b.N {
			for _, sample := range samples {
				if ast.IsParameterPropertyDeclaration(store.At(sample.parameter), store.At(sample.parent)) {
					matched++
				}
			}
		}
		parameterPropertyChecks = matched
	})
	b.Run("parameter-flag", func(b *testing.B) {
		var matched uint64
		b.ResetTimer()
		for range b.N {
			for _, sample := range samples {
				if store.KindAt(sample.parent) == ast.KindConstructor && store.FlagsAt(sample.parameter)&ast.NodeFlagsHasParameterPropertyModifier != 0 {
					matched++
				}
			}
		}
		parameterPropertyChecks = matched
	})
}
