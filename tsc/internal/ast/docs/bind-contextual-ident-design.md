# Bind contextual identifier redesign

Date: 2026-09-09. Updated same day with adoption criterion.

## Adoption criterion (project goal)

The goal is to show **Store Bind faster than pointer-tree Bind**. Only land changes that fix **Store-specific** cost (layout, columns, Handle/NodeRef tax, arena, Freeze, etc.).

**Do not land** optimizations that the pointer AST can take the same way (shared binder algorithm, parse-time flags either tree can stamp, parent threading in the walk). Those shrink both sides and do not prove Store.

## Problem

`checkContextualIdentifierRef` is a real Bind wall lever (full skip −15.5% BindHot). Half is `TextAt`+`GetIdentifierToken` (−7.8%). That cost exists on pointer Bind too (`node.Text()` + `GetIdentifierToken`). A parse-time `MayBeReserved` flag is the same idea on either representation.

## Verdict

**Rejected for landing.** Phase 1 (`IdentifierMayBeReserved`) was implemented and measured (BindKPC inst −7.7%, cycles −6.0%; BindHot GOGC=off −12.3%) then **reverted**. It is a shared-algorithm win, not a Store-vs-pointer win.

Phase 2 (parent-threaded `isIdentifierNameRef`) is likewise pointer-applicable → do not pursue under this criterion.

Keep the measurement notes in `bind-ident-contextual-bench.md` as a ceiling / distractor record only.

## What remains in scope for P1-shaped Bind cost

Only if the Store path does **extra** work the pointer path does not, for example:

- Extra `ParentRef` / `KindAt` / `HandleOf` round-trips that pointer Bind folds into one `*Node`
- Column / intern indirection unique to Store `TextAt`
- Flags loads that are cheaper as a field on `*Node`

Those need an A/B that attributes the delta to Store representation, not to a binder rewrite both trees could share.

## Synthesis decision

Arena reconstructed after runner OOM. Base design (flag bit) **fails the adoption criterion** above. Code reverted 2026-09-09.

## Alternatives considered

| Shape | Why not land |
| --- | --- |
| Parse-time MayBeReserved | Pointer can stamp the same flag |
| Parent-thread name position | Pointer walk can pass parent too |
| Dense keyword column | Store-specific but heavy; kwcache-class; not pursued |
| Full skip | Breaks diagnostics; upper bound only |

## Next implementation step

Return to Store-only Bind taxes ranked in `why-store-bind-is-slower.md` (packed layout rejects, SetFlow/flowID, Handle leftovers, column touch) — compare against `8ac035a` pointer Bind, not against an optimized shared binder.
