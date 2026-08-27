package ast

// CopySubtree deep-copies the subtree rooted at src into f's Store.
// A zero src returns a zero Handle. Parents are left unset; callers use
// SetParentsInChildren. Panics if f's Store is sealed.
func (f *Factory) CopySubtree(src Handle) Handle {
	if src.Ref() == 0 {
		return Handle{}
	}
	if f.store.internIdx == nil {
		panic("ast: CopySubtree into sealed Store")
	}
	if src.Store() == nil {
		panic("ast: invalid Handle")
	}
	c := &subtreeCopier{
		dst:   f,
		src:   src.Store(),
		remap: make(map[NodeRef]NodeRef),
	}
	return c.copy(src.Ref())
}

type subtreeCopier struct {
	dst   *Factory
	src   *Store
	remap map[NodeRef]NodeRef
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
	c.remap[ref] = dst.Ref()
	if text := src.Ident(); text != "" {
		dst.SetIdent(c.dst.store.Intern(text))
	}
	for i := range n {
		dst.SetChild(i, c.copy(src.Child(i).Ref()))
	}
	return dst
}
