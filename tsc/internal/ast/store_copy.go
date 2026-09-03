package ast

// CopySubtree deep-copies the subtree rooted at src into f's Store.
// A zero src returns a zero Handle. Parents are left unset; callers use
// SetParentsInChildren. Named children and every list slot are remapped.
func (f *Factory) CopySubtree(src Handle) Handle {
	if src.Ref() == 0 {
		return Handle{}
	}
	if src.Store() == nil {
		panic("ast: invalid Handle")
	}
	c := &subtreeCopier{
		dst:   f,
		src:   src.Store(),
		remap: make(map[NodeRef]NodeRef),
		lists: make(map[ListRef]ListRef),
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
	dst   *Factory
	src   *Store
	remap map[NodeRef]NodeRef
	lists map[ListRef]ListRef
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
		if external := src.ExternalChild(i); external != 0 {
			dst.SetExternalChild(i, external)
		} else {
			dst.SetChild(i, c.copy(src.Child(i).Ref()))
		}
	}
	for i := range listN {
		if list := src.ListSlot(i); list != 0 {
			dst.SetListSlot(i, c.copyList(list))
		}
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
		if external := c.src.ExternalListAt(src, i); external != 0 {
			c.dst.store.SetExternalListAt(dst, i, external)
		} else {
			c.dst.store.SetListAt(dst, i, c.copy(c.src.ListAt(src, i).Ref()))
		}
	}
	return dst
}
