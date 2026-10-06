#!/usr/bin/env python3
"""The poolrecord overlay (store-cli-parallel-instructions-20261007.md 3 and
5.4): the checker pools of compiler and storecompiler record their file
association, its inputs and the order in which each checker visits its files.

usage: gen.py OUTDIR
writes OUTDIR/overlay.json and the files it names; then
  go test -overlay OUTDIR/overlay.json -tags poolrecord ./internal/storeclitest -run TestPoolAssignments

The pool files of the two packages are copied with the calls inserted, each
at a line that must occur exactly once among them; the record itself
(zz_poolrecord.go) is the same text in both packages but for the node count
that createCheckers weighs.
"""
import hashlib, json, os, sys

TSC = os.path.abspath(os.path.join(os.path.dirname(__file__), "..", "..", "..", ".."))

INSERTS = [
    # after the association, inside createCheckers' checkerCount > 1 branch
    ("\t\t\tassociations = getCheckerAssociationsInOrder(fileWeights, adjacentFiles, fileOrder, checkerCount, policy.balancePenaltyMultiplier)\n",
     "\t\t\tpoolRecordWeights(policy.prioritizeSourceFiles, policy.sourceFileWeightMultiplier, policy.balancePenaltyMultiplier, fileWeights, adjacentFiles, fileOrder)\n"),
    # after the association map
    ("\t\t\tp.fileAssociations[file] = p.checkers[associations[i]]\n\t\t}\n",
     "\t\tpoolRecordAssociations(p, associations)\n"),
    # the start of createCheckers
    ("\tp.createCheckersOnce.Do(func() {\n",
     "\t\tpoolRecordCreateStart()\n"),
    # a checker's group: its start and end, and each file after its callback
    ("\t\t\tp.locks[checkerIdx].Lock()\n\t\t\tdefer p.locks[checkerIdx].Unlock()\n",
     "\t\t\tpoolRecordGroupStart(checkerIdx)\n\t\t\tdefer poolRecordGroupEnd(checkerIdx)\n"),
    ("\t\t\t\t\tcb(checker, i, file)\n",
     "\t\t\t\t\tpoolRecordVisit(checkerIdx, i)\n"),
]

# The field that each createCheckers weighs (compiler/checkerpool.go and
# storecompiler/checkerpool_manual.go).
WEIGHT_NODES = {"compiler": "NodeCount", "storecompiler": "CreatedNodeCount"}

RECORD = '''//go:build poolrecord

package %(pkg)s

import (
	"encoding/json"
	"slices"
	"time"
)

// The record of the poolrecord overlay
// (internal/storeclitest/testdata/poolrecord/gen.py): what the checker pool
// that last made its checkers did. One pool at a time.

type poolRecordData struct {
	Checkers                   int               `json:"checkers"`
	Files                      []poolRecordFile  `json:"files"`
	PrioritizeSourceFiles      bool              `json:"prioritizeSourceFiles"`
	SourceFileWeightMultiplier int               `json:"sourceFileWeightMultiplier"`
	BalancePenaltyMultiplier   int               `json:"balancePenaltyMultiplier"`
	FileWeights                []int             `json:"fileWeights"`
	FileOrder                  []int             `json:"fileOrder"`
	Adjacency                  [][]int           `json:"adjacency"`
	Associations               []int             `json:"associations"`
	CreateNS                   [2]int64          `json:"createNS"` // createCheckers' start and end
	Groups                     []poolRecordGroup `json:"groups"`
}

type poolRecordFile struct {
	FileName          string `json:"fileName"`
	NodeCount         int    `json:"nodeCount"`
	WeightNodeCount   int    `json:"weightNodeCount"` // what createCheckers weighs
	TextLen           int    `json:"textLen"`
	Imports           int    `json:"imports"`
	IsDeclarationFile bool   `json:"isDeclarationFile"`
}

// poolRecordGroup is a checker's task of forEachCheckerGroupDo: the indices
// of the files it visited, in order, and the times (ns since createCheckers'
// start) after each.
type poolRecordGroup struct {
	StartNS    int64   `json:"startNS"`
	EndNS      int64   `json:"endNS"`
	Visits     []int   `json:"visits"`
	VisitEndNS []int64 `json:"visitEndNS"`
}

var (
	poolRecord   poolRecordData
	poolRecordT0 time.Time
)

func poolRecordSince() int64 { return time.Since(poolRecordT0).Nanoseconds() }

func poolRecordCreateStart() {
	poolRecord = poolRecordData{}
	poolRecordT0 = time.Now()
}

func poolRecordWeights(prioritizeSourceFiles bool, sourceFileWeightMultiplier int, balancePenaltyMultiplier int, fileWeights []int, adjacentFiles [][]int, fileOrder []int) {
	poolRecord.PrioritizeSourceFiles = prioritizeSourceFiles
	poolRecord.SourceFileWeightMultiplier = sourceFileWeightMultiplier
	poolRecord.BalancePenaltyMultiplier = balancePenaltyMultiplier
	poolRecord.FileWeights = slices.Clone(fileWeights)
	poolRecord.FileOrder = slices.Clone(fileOrder)
	poolRecord.Adjacency = make([][]int, len(adjacentFiles))
	for i, adjacent := range adjacentFiles {
		poolRecord.Adjacency[i] = slices.Clone(adjacent)
	}
}

func poolRecordAssociations(p *checkerPool, associations []int) {
	poolRecord.Checkers = len(p.checkers)
	poolRecord.Associations = slices.Clone(associations)
	for _, file := range p.program.files {
		poolRecord.Files = append(poolRecord.Files, poolRecordFile{
			FileName:          file.FileName(),
			NodeCount:         file.NodeCount,
			WeightNodeCount:   file.%(weightNodes)s,
			TextLen:           len(file.Text()),
			Imports:           len(file.Imports()),
			IsDeclarationFile: file.IsDeclarationFile,
		})
	}
	poolRecord.Groups = make([]poolRecordGroup, len(p.checkers))
	poolRecord.CreateNS[1] = poolRecordSince()
}

func poolRecordGroupStart(checkerIdx int) {
	poolRecord.Groups[checkerIdx].StartNS = poolRecordSince()
}

func poolRecordVisit(checkerIdx int, fileIndex int) {
	g := &poolRecord.Groups[checkerIdx]
	g.Visits = append(g.Visits, fileIndex)
	g.VisitEndNS = append(g.VisitEndNS, poolRecordSince())
}

func poolRecordGroupEnd(checkerIdx int) {
	poolRecord.Groups[checkerIdx].EndNS = poolRecordSince()
}

// PoolRecordReset forgets the record, so that a run that makes no checkers
// leaves none.
func PoolRecordReset() { poolRecord = poolRecordData{} }

// PoolRecordJSON is the record of the pool that last made its checkers.
func PoolRecordJSON() []byte {
	b, err := json.Marshal(&poolRecord)
	if err != nil {
		panic(err)
	}
	return b
}
'''

# The files of each pool: createCheckers is P2 in storecompiler.
SOURCES = {
    "compiler": ["checkerpool.go"],
    "storecompiler": ["checkerpool.go", "checkerpool_manual.go"],
}

# The checkers' counts of the count runs (5.4): the nodes the checker made
# (its factory) and the entries of its type cache and relations, the same
# fields in both checkers.
STATS = '''//go:build poolrecord

package %(pkg)s

// PoolRecordStats are the counts of a checker for the count runs of the
// poolrecord overlay (internal/storeclitest/testdata/poolrecord/gen.py).
func (c *Checker) PoolRecordStats() map[string]int {
	return map[string]int{
		"factoryNodes":          c.factory.NodeCount(),
		"cachedTypes":           len(c.cachedTypes),
		"subtypeRelation":       len(c.subtypeRelation.results),
		"strictSubtypeRelation": len(c.strictSubtypeRelation.results),
		"assignableRelation":    len(c.assignableRelation.results),
		"comparableRelation":    len(c.comparableRelation.results),
		"identityRelation":      len(c.identityRelation.results),
	}
}
'''

def instrument(pkg):
    """The package's pool files with the calls inserted; each anchor must
    occur exactly once among them."""
    texts = {}
    for name in SOURCES[pkg]:
        texts[name] = open(os.path.join(TSC, "internal", pkg, name)).read()
    for anchor, insert in INSERTS:
        hits = [name for name, text in texts.items() for _ in range(text.count(anchor))]
        if len(hits) != 1:
            sys.exit(f"{pkg}: the anchor occurs {len(hits)} times: {anchor!r}")
        texts[hits[0]] = texts[hits[0]].replace(anchor, anchor + insert)
    return texts

def main():
    out = os.path.abspath(sys.argv[1])
    os.makedirs(out, exist_ok=True)
    replace = {}
    hashes = {}
    for pkg in SOURCES:
        for name, text in instrument(pkg).items():
            src = os.path.join(TSC, "internal", pkg, name)
            dst = os.path.join(out, pkg + "_" + name)
            open(dst, "w").write(text)
            replace[src] = dst
            hashes[src] = hashlib.sha256(open(src, "rb").read()).hexdigest()
        record = os.path.join(out, pkg + "_zz_poolrecord.go")
        open(record, "w").write(RECORD % {"pkg": pkg, "weightNodes": WEIGHT_NODES[pkg]})
        replace[os.path.join(TSC, "internal", pkg, "zz_poolrecord.go")] = record
    for pkg in ["checker", "storechecker"]:
        stats = os.path.join(out, pkg + "_zz_poolrecord.go")
        open(stats, "w").write(STATS % {"pkg": pkg})
        replace[os.path.join(TSC, "internal", pkg, "zz_poolrecord.go")] = stats
    json.dump({"Replace": replace}, open(os.path.join(out, "overlay.json"), "w"), indent=1)
    json.dump(hashes, open(os.path.join(out, "sources.sha256.json"), "w"), indent=1)

main()
