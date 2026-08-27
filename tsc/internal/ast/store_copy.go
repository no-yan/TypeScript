package ast

// CopySubtree deep-copies the subtree rooted at src into f's Store.
// A zero src returns a zero Handle. Parents are left unset; callers use
// SetParentsInChildren. Named children and list0 elements are remapped.
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
	return c.copy(src.Ref())
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
	dst := c.dst.create(src.Kind(), src.Flags(), src.Loc(), n)
	dst.SetTokenFlags(src.TokenFlags())
	c.remap[ref] = dst.Ref()
	if text := src.Ident(); text != "" {
		dst.SetIdent(c.dst.store.Intern(text))
	}
	for i := range n {
		dst.SetChild(i, c.copy(src.Child(i).Ref()))
	}
	if list := src.List(); list != 0 {
		dst.SetList(c.copyList(list))
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
		c.dst.store.SetListAt(dst, i, c.copy(c.src.ListAt(src, i).Ref()))
	}
	return dst
}
