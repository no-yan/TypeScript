package transformers

import (
	"github.com/microsoft/TypeScript/tsc/internal/ast"
	"github.com/microsoft/TypeScript/tsc/internal/binder"
	"github.com/microsoft/TypeScript/tsc/internal/core"
	"github.com/microsoft/TypeScript/tsc/internal/printer"
)

type chainedTransformer struct {
	Transformer
	components []*Transformer
}

func (ch *chainedTransformer) visit(node ast.Handle) ast.Handle {
	if node.Kind != ast.KindSourceFile {
		panic("Chained transform passed non-sourcefile initial node")
	}
	file := ast.GetSourceFileOfNode(node)
	for _, t := range ch.components {
		file = t.TransformSourceFile(file)
	}
	return file.ParseRoot()
}

type TransformOptions struct {
	Context                   *printer.EmitContext
	CompilerOptions           *core.CompilerOptions
	Resolver                  binder.ReferenceResolver
	EmitResolver              printer.EmitResolver
	GetEmitModuleFormatOfFile func(file ast.HasFileName) core.ModuleKind
}
type TransformerFactory = func(opt *TransformOptions) *Transformer

func Chain(transforms ...TransformerFactory) TransformerFactory {
	if len(transforms) < 2 {
		if len(transforms) == 0 {
			panic("Expected some number of transforms to chain, but got none")
		}
		return transforms[0]
	}
	return func(opt *TransformOptions) *Transformer {
		constructed := make([]*Transformer, 0, len(transforms))
		for _, t := range transforms {
			if result := t(opt); result != nil {
				constructed = append(constructed, result)
			}
		}
		switch len(constructed) {
		case 0:
			return nil
		case 1:
			return constructed[0]
		}
		ch := &chainedTransformer{components: constructed}
		return ch.NewTransformer(ch.visit, opt.Context)
	}
}
