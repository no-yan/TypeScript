---
name: nodeseq-hotpath
description: >-
  Phase 2 only of check-phase alloc hillclimb: remove remaining Handle Foo()
  materializers after struct NodeSeq lands. Use when Phase 1 is kept and the
  operator asks to swarm Arguments/Parameters/TypeParameters Foo() call sites.
  Do not use for NodeSeq struct redesign (see tsc/.audit/check-phase-alloc-plan.md).
disable-model-invocation: true
---

# NodeSeq hot-path (Phase 2 only)

**Prerequisite.** Phase 1 (struct `NodeSeq`) is measured and kept per
`tsc/.audit/check-phase-alloc-plan.md`. Len/First call-site rewrites (old A7/B4)
are cancelled.

## Read

1. `tsc/.audit/check-phase-alloc-plan.md` Phase 2 section
2. [tasks.md](tasks.md) Wave A/B checkboxes (A7/B4 struck)

## Contract

Same as the alloc plan. Prefer `ListLen`/`ListAt`/`ListIndexOf` and `*Seq().All()`
ranges. `Foo()` only at true `[]Handle` ownership boundaries.

## Fan-out

One exclusive owner for `checker.go`. Supporting packages may parallelize.
Spawn only after Phase 1 keep.
