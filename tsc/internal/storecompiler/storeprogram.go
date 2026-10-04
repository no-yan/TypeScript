package storecompiler

import (
	"github.com/microsoft/TypeScript/tsc/internal/ast/store"
	"github.com/microsoft/TypeScript/tsc/internal/storechecker"
)

// IDSpace is the program's StoreID namespace (store-cli-design-20261004.md
// 4.3): the checker's region is made in it.
func (p *Program) IDSpace() *store.IDSpace { return p.opts.idSpace }

var _ storechecker.Program = (*Program)(nil)
