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
		if sym := n.Symbol(); sym != nil {
			h.SetSymbol(sym)
		}
		if loc := n.LocalsContainerData(); loc != nil {
			if loc.Locals != nil {
				h.SetLocals(loc.Locals)
			}
			if loc.NextContainer != nil {
				if next := file.HandleOf(loc.NextContainer); next.Ref() != 0 {
					h.SetNextContainer(next)
				}
			}
		}
		if flow := n.FlowNodeData(); flow != nil && flow.FlowNode != nil {
			h.SetFlowNode(flow.FlowNode)
		}
		return false
	})
}
