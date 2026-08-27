package ast

// FlattenNode copies an existing *Node tree into s. It is for layout
// experiments: the parser still builds the pointer AST; this rewrites the
// shape so GC and walk costs can be compared without a Store-backed parser.
func FlattenNode(s *Store, n *Node) Handle {
	if s == nil {
		panic("ast: FlattenNode on nil Store")
	}
	if n == nil {
		return Handle{}
	}
	var kids []*Node
	n.ForEachChild(func(c *Node) bool {
		kids = append(kids, c)
		return false
	})
	h := s.Alloc(n.Kind, n.Flags, n.Loc, len(kids))
	switch n.Kind {
	case KindIdentifier, KindPrivateIdentifier, KindStringLiteral,
		KindNoSubstitutionTemplateLiteral, KindNumericLiteral:
		if text := n.Text(); text != "" {
			h.SetIdent(s.Intern(text))
		}
	}
	for i, c := range kids {
		ch := FlattenNode(s, c)
		h.SetChild(i, ch)
		ch.SetParent(h)
	}
	return h
}
