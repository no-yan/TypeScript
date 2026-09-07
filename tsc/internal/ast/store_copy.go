package ast

// CopySubtree deep-copies the subtree rooted at src into f's Store.
// A zero src returns a zero Handle. Parents are left unset; callers use
// SetParentsInChildren. Named children and every list slot are remapped.
func (f *Factory) CopySubtree(src Handle) Handle {
	return f.copySubtree(src, nil)
}

func (f *Factory) copySubtree(src Handle, onClone func(Handle, Handle)) Handle {
	if src.Ref() == 0 {
		return Handle{}
	}
	if src.Store() == nil {
		panic("ast: invalid Handle")
	}
	c := &subtreeCopier{
		dst:     f,
		src:     src.Store(),
		remap:   make(map[NodeRef]NodeRef),
		lists:   make(map[ListRef]ListRef),
		onClone: onClone,
	}
	result := c.copy(src.Ref())
	for srcRef, dstRef := range c.remap {
		if next := c.src.NextContainer(srcRef); next != 0 {
			if remapped, ok := c.remap[next]; ok {
				c.dst.store.SetNextContainer(dstRef, remapped)
			}
		}
	}
	return result
}

type subtreeCopier struct {
	onClone func(Handle, Handle)
	dst     *Factory
	src     *Store
	remap   map[NodeRef]NodeRef
	lists   map[ListRef]ListRef
}

func (c *subtreeCopier) copy(ref NodeRef) Handle {
	if ref == 0 {
		return Handle{}
	}
	if dstRef, ok := c.remap[ref]; ok {
		return c.dst.store.At(dstRef)
	}
	src := c.src.At(ref)
	n := src.NumChildren()
	listN := src.NumListSlots()
	dst := c.dst.createSlots(src.Kind, src.Flags(), src.Loc(), n, listN)
	dst.SetTokenFlags(src.TokenFlags())
	c.remap[ref] = dst.Ref()
	dst.SetSymbol(src.Symbol())
	dst.SetLocalSymbol(src.LocalSymbol())
	dst.SetFlowNode(src.FlowNode())
	dst.SetEndFlowNode(src.EndFlowNode())
	dst.SetReturnFlowNode(src.ReturnFlowNode())
	dst.SetLocals(src.Locals())
	for key, value := range c.src.scalarValues {
		if NodeRef(key>>32) == ref {
			dst.SetUintValue(int(uint32(key)), value)
		}
	}
	for key, value := range c.src.stringValues {
		if NodeRef(key>>32) == ref {
			dst.SetStringValue(int(uint32(key)), c.src.internText(value))
		}
	}
	for key, value := range c.src.objectValues {
		if NodeRef(key>>32) == ref {
			dst.SetObjectValue(int(uint32(key)), value)
		}
	}
	if text := src.Ident(); text != "" {
		dst.SetIdent(c.dst.store.Intern(text))
	}
	for i := range n {
		child := src.Child(i)
		if !child.IsNil() && child.Store() != c.src {
			dst.SetChild(i, child)
		} else {
			dst.SetChild(i, c.copy(child.Ref()))
		}
	}
	for i := range listN {
		if list := src.ListSlot(i); list != 0 {
			dst.SetListSlot(i, c.copyList(list))
		}
	}
	if c.onClone != nil {
		c.onClone(dst, src)
	}
	return dst
}

func (c *subtreeCopier) copyList(src ListRef) ListRef {
	if src == 0 {
		return 0
	}
	if dst, ok := c.lists[src]; ok {
		return dst
	}
	n := c.src.ListLen(src)
	dst := c.dst.store.AllocList(c.src.ListLoc(src), n)
	c.lists[src] = dst
	for i := range n {
		child := c.src.ListAt(src, i)
		if !child.IsNil() && child.Store() != c.src {
			c.dst.store.SetListAt(dst, i, child)
		} else {
			c.dst.store.SetListAt(dst, i, c.copy(child.Ref()))
		}
	}
	return dst
}
