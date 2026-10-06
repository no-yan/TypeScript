package storecompiler

import (
	"github.com/microsoft/TypeScript/tsc/internal/ast/store"
	"github.com/microsoft/TypeScript/tsc/internal/core"
	"github.com/microsoft/TypeScript/tsc/internal/storechecker"
)

// The P2 declarations of compiler/checkerpool.go.

// createCheckers weighs a file by the nodes the Pointer parser makes
// (store.File.CreatedNodeCount): the Pointer's NodeCount, which counts every
// node of its factory. The Store's NodeCount, the rows the file keeps, is
// smaller, and a different weight can move a file to another checker
// (store-cli-parallel-instructions-20261007.md 3).
//
// storetranslate:pointer-hash f7f4030504088c24 checkerpool.go:checkerPool.createCheckers
func (p *checkerPool) createCheckers() {
	p.createCheckersOnce.Do(func() {
		checkerCount := len(p.checkers)
		wg := core.NewWorkGroup(p.program.SingleThreaded())
		for i := range checkerCount {
			wg.Queue(func() {
				var tracer *storechecker.Tracer
				if p.tracing != nil {
					tracer = storechecker.NewTracer(p.tracing, i)
				}
				p.checkers[i], p.locks[i] = storechecker.NewChecker(p.program, tracer)
			})
		}

		wg.RunAndWait()

		associations := make([]int, len(p.program.files))
		if checkerCount > 1 {
			baseWeights := make([]int, len(p.program.files))
			importCounts := make([]int, len(p.program.files))
			isDeclarationFile := make([]bool, len(p.program.files))
			totalBaseWeight := 0
			declarationBaseWeight := 0
			for i, file := range p.program.files {
				baseWeight := getCheckerAssociationBaseWeight(file.CreatedNodeCount, len(file.Text()))
				totalBaseWeight += baseWeight
				if file.IsDeclarationFile {
					declarationBaseWeight += baseWeight
				}
				baseWeights[i] = baseWeight
				importCounts[i] = len(file.Imports())
				isDeclarationFile[i] = file.IsDeclarationFile
			}
			policy := getCheckerAssociationPolicy(totalBaseWeight, declarationBaseWeight, checkerCount)
			if policy.sourceFileWeightMultiplier != 1 {
				// Apply this before import normalization. The policy intentionally
				// increases both source-file work and the normalized import unit.
				for i, declaration := range isDeclarationFile {
					if !declaration {
						baseWeights[i] *= policy.sourceFileWeightMultiplier
					}
				}
			}
			fileWeights := getCheckerAssociationWeights(baseWeights, importCounts)
			adjacentFiles := p.getImportAdjacency()
			fileOrder := getCheckerAssociationOrder(fileWeights, isDeclarationFile, policy.prioritizeSourceFiles)
			associations = getCheckerAssociationsInOrder(fileWeights, adjacentFiles, fileOrder, checkerCount, policy.balancePenaltyMultiplier)
		}
		p.fileAssociations = make(map[*store.File]*storechecker.Checker, len(p.program.files))
		for i, file := range p.program.files {
			p.fileAssociations[file] = p.checkers[associations[i]]
		}
	})
}
