package store

import "github.com/microsoft/TypeScript/tsc/internal/ast"

// GetNodeAtPosition is ast.GetNodeAtPosition without the JSDoc pass: from the
// root, it descends into the child whose range holds position.
func GetNodeAtPosition(file *File, position int) Node {
	current := file.Root()
	for {
		var child Node
		found := false
		current.ForEachChild(func(node Node) bool {
			if nodeContainsPosition(node, position) {
				child = node
				found = true
				return true
			}
			return false
		})
		if !found || IsMetaProperty(child) {
			return current
		}
		current = child
	}
}

func nodeContainsPosition(node Node, position int) bool {
	return node.Kind() >= ast.KindFirstNode && int(node.Pos()) <= position && (position < int(node.End()) || position == int(node.End()) && node.Kind() == ast.KindEndOfFile)
}
