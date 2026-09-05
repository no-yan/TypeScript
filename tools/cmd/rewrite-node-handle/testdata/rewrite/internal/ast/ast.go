package ast

type Kind int
type NodeFlags int
type SymbolFlags int

type TextRange struct{}

type Node struct {
	Kind   Kind
	Flags  NodeFlags
	Loc    TextRange
	Parent *Node
}

type IdentifierNode = Handle
type Expression = Handle

type NodeList struct{}
type SourceFile struct{}
type ParameterDeclaration struct{}

func (n *Node) AsParameterDeclaration() *ParameterDeclaration { return nil }
func (n *Node) IsJSDoc() bool                                 { return false }

type Handle struct{}

func (h Handle) Kind() Kind         { return 0 }
func (h Handle) Flags() NodeFlags   { return 0 }
func (h Handle) Loc() TextRange     { return TextRange{} }
func (h Handle) Parent() Handle     { return Handle{} }
func (h Handle) IsNil() bool        { return false }
func (h Handle) SetFlags(NodeFlags) {}
func (h Handle) SetParent(Handle)   {}
func (h Handle) SetLoc(TextRange)   {}
func (h Handle) AsParameterDeclaration() *ParameterDeclaration {
	return nil
}
func (h Handle) IsJSDoc() bool { return false }

type Symbol struct {
	Flags  SymbolFlags
	Parent *Symbol
}
