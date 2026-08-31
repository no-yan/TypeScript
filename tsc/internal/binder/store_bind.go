package binder

import "github.com/microsoft/TypeScript/tsc/internal/ast"

// bindStore walks the parse Store and writes binder side data onto Handles.
// Parent and child slots stay as the parser left them.
func bindStore(file *ast.SourceFile) {
	root := file.ParseRoot()
	if root.Ref() == 0 {
		return
	}
	ast.Walk(root, func(h ast.Handle) bool {
		n := file.NodeFor(h.Ref())
		if n == nil {
			return false
		}
		h.SetFlags(n.Flags)
		h.SetSymbol(n.Symbol())
		if exportable := n.ExportableData(); exportable != nil {
			h.SetLocalSymbol(exportable.LocalSymbol)
		}
		if loc := n.LocalsContainerData(); loc != nil {
			h.SetLocals(loc.Locals)
			h.SetNextContainer(ast.Handle{})
			if loc.NextContainer != nil {
				if next := file.HandleOf(loc.NextContainer); next.Ref() != 0 {
					h.SetNextContainer(next)
				}
			}
		}
		if flow := n.FlowNodeData(); flow != nil {
			h.SetFlowNode(flow.FlowNode)
		}
		if body := n.BodyData(); body != nil {
			h.SetEndFlowNode(body.EndFlowNode)
		}
		switch n.Kind {
		case ast.KindConstructor:
			h.SetReturnFlowNode(n.AsConstructorDeclaration().ReturnFlowNode)
		case ast.KindFunctionDeclaration:
			h.SetReturnFlowNode(n.AsFunctionDeclaration().ReturnFlowNode)
		case ast.KindFunctionExpression:
			h.SetReturnFlowNode(n.AsFunctionExpression().ReturnFlowNode)
		case ast.KindClassStaticBlockDeclaration:
			h.SetReturnFlowNode(n.AsClassStaticBlockDeclaration().ReturnFlowNode)
		}
		return false
	})
}
