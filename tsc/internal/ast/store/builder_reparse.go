package store

import "github.com/microsoft/TypeScript/tsc/internal/ast"

// The Builder API of the JS reparse (B1 in internal/ast/docs/
// store-checker-design-20260923.md 2.2, "reparse の Builder API", and
// store-checker-7d1-implementation-instructions-20260928.md 3.1). The Pointer
// reparser (internal/parser/reparser.go) rewrites nodes it has already made;
// these are the same writes on the scratch, before Finish only: a published
// Store is never written. The generated setters (SetType, SetModifiers, ...)
// and the walk of DeepCloneReparse are in builder_generated.go.
//
// A node made here comes after its host, so "a child's id is below its
// parent's" does not hold for the reparse and the JSDoc nodes (invariant 20).
// Only "the root is the last node" is relied on (Store.Root); FinishFile
// checks it in a storeChecks build.

// SetFlags replaces the flags: finishReparsedNode's node.Flags = ...
func (b *Builder) SetFlags(id NodeRef, flags ast.NodeFlags) { b.s.nodes[id].flags = flags }

// SetRange is node.Loc = core.NewTextRange(pos, end).
func (b *Builder) SetRange(id NodeRef, pos, end int32) {
	b.s.nodes[id].pos, b.s.nodes[id].end = pos, end
}

// SetParent is child.Parent = parent, for the parents that the Pointer parser
// writes outside a constructor: a JSDoc comment's parent is its host, which
// does not hold it in a slot.
func (b *Builder) SetParent(child, parent NodeRef) { b.adopt(child, parent) }

// ReparentChildren is finishMutatedNode (overrideParentInImmediateChildren):
// the parent of every child that ForEachChild visits becomes id.
func (b *Builder) ReparentChildren(id NodeRef) {
	b.View(id).ForEachChild(func(child Node) bool {
		b.s.nodes[child.Ref()].parent = id
		return false
	})
}

// DeepCloneReparse is ast.NodeFactory.DeepCloneReparse: the subtree is
// copied in post-order with the ranges and flags of the original (the JSDoc
// flag included); the parent of each copy is its copied parent, and only the
// root gains NodeFlagsReparsed. A list is a new block with the range of the
// original; a text points at the original's bytes.
func (b *Builder) DeepCloneReparse(id NodeRef) NodeRef {
	if id == NoNodeRef {
		return NoNodeRef
	}
	clone := b.deepCloneReparse(id)
	b.s.nodes[clone].flags |= ast.NodeFlagsReparsed
	return clone
}

// DeepCloneReparseModifiers is ast.NodeFactory.DeepCloneReparseModifiers: the
// block and its elements, none of them flagged Reparsed.
func (b *Builder) DeepCloneReparseModifiers(at ListRef) ListRef { return b.cloneListReparse(at) }

func (b *Builder) cloneChildReparse(id NodeRef) NodeRef {
	if id == NoNodeRef {
		return NoNodeRef
	}
	return b.deepCloneReparse(id)
}

func (b *Builder) cloneListReparse(at ListRef) ListRef {
	if at == NoListRef {
		return NoListRef
	}
	elems := make([]NodeRef, b.s.extra[at])
	for i := range elems {
		// Read by index: a clone appends to extra, which may move.
		elems[i] = b.deepCloneReparse(NodeRef(b.s.extra[int(at)+3+i]))
	}
	return b.List(int32(b.s.extra[at+1]), int32(b.s.extra[at+2]), elems)
}

// cloneRow appends a copy of h with the payload words w (already holding the
// cloned children), or, for a kind without a payload, h's data word as is.
func (b *Builder) cloneRow(h NodeHeader, w []uint32) NodeRef {
	data := h.data
	if w != nil {
		data = uint32(len(b.s.extra))
		b.s.extra = append(b.s.extra, w...)
	}
	return b.header(h.kind, h.flags, ast.ModifierFlags(h.modifierFlags), h.pos, h.end, data)
}
