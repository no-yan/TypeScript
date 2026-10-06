//go:build poolrecord

package storeclitest

import (
	"encoding/json"

	"github.com/microsoft/TypeScript/tsc/internal/checker"
	"github.com/microsoft/TypeScript/tsc/internal/compiler"
	"github.com/microsoft/TypeScript/tsc/internal/core"
	"github.com/microsoft/TypeScript/tsc/internal/storechecker"
	"github.com/microsoft/TypeScript/tsc/internal/storecompiler"
)

// The count runs of store-cli-parallel-instructions-20261007.md 5.4: with
// the poolrecord overlay, TestNativeStages writes each pool's record and
// each checker's counts, and the pool's checkers after the bind.
func init() {
	pointerCheckerStats = (*checker.Checker).PoolRecordStats
	storeCheckerStats = (*storechecker.Checker).PoolRecordStats
	poolRecordOf = func(backend core.ASTBackend) json.RawMessage {
		if backend == core.ASTBackendStore {
			return storecompiler.PoolRecordJSON()
		}
		return compiler.PoolRecordJSON()
	}
}
