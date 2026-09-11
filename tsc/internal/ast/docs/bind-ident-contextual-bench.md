# Bind contextual identifier cost (P1)

Date: 2026-09-09. Apple M1. Overlay scratch: `/tmp/bind-p1-verify/`.

Instruments put `checkContextualIdentifierRef` at ~3–5% exclusive of bind. This note is the BindHot / BindKPC upper bound and the micro-opts that failed to capture it.

## Upper bound (skip entire check)

Throwaway: `checkContextualIdentifierRef` returns immediately. Semantics broken; measures ceiling only.

| Clock | base | skip | delta |
| --- | ---: | ---: | ---: |
| BindHot checker.ts (n=6, 20x) | 17.72 ms ± 4% | 14.97 ms ± 2% | **−15.50%** (p=0.002) |
| BindHot `GOGC=off` | 19.46 ms ± 28% | 16.47 ms ± 11% | **−15.35%** (p=0.009) |
| BindKPC inst/op (n=3, 30x, median) | 189.6M | 165.8M | **−12.5%** |
| BindKPC cycles/op | 96.63M | 84.85M | **−12.2%** |

This is a real Bind wall lever, unlike packed child Kind or KindAt hoist.

## Split

| Overlay | What it skips | BindHot vs base |
| --- | --- | --- |
| `notext` | `TextAt` + `GetIdentifierToken` after `isIdentifierNameRef` | **−7.81%** (p=0.026) |
| full skip | entire function | **−15.5%** |
| implied | `FlagsAt` + `isIdentifierNameRef` + diagnostics check | ~7.7% of BindHot |

## Micro-opts that did not move wall

| Overlay | Idea | BindHot |
| --- | --- | --- |
| `peek` | `IdentCouldBeKeyword` (len/first-byte) before `TextAt` | ~ (p=0.240) |
| `kwcache` | cache `GetIdentifierToken` by intern id in Binder | ~ (p=0.240), B/op **+1.15%** |
| `fastname` | `isIdentifierNameRef` parent-kind only, no `nameRefGenerated` | ~ (p=0.240) |
| `oneflags` | one `FlagsAt` for Identifier, early return | ~ (p=0.937) |

Most binding names that reach `TextAt` are already short lowercase (peek does not reject them). Per-bind intern maps add bytes without wall win.

## What to build next (semantic-preserving)

1. **Parse-time reserved-ident mark** (preferred for the TextAt half). When creating an Identifier whose text is a keyword (`GetIdentifierToken != KindIdentifier`), set a NodeFlags alias bit on that identifier (same pattern as `NodeFlagsIdentifierHasExtendedUnicodeEscape`). `checkContextualIdentifierRef` already has `FlagsAt`: if the bit is clear, return after `isIdentifierNameRef` without `TextAt`. Targets the measured **−7.8%** half. Rare keywords only pay `TextAt`.

2. **Parent-threaded `isIdentifierNameRef`** for the other half. Pass `(parent, parentKind)` from the bind walk so Store does not `ParentRef`+`KindAt` again. `fastname` showed `nameRefGenerated` equality is not the wall; try parent threading before inventing more Store layout.

Do not land the full skip. Do not land `kwcache`.

## Adoption criterion

Project goal: Store Bind faster than pointer Bind. **Do not land** binder/parse opts the pointer tree can mirror (e.g. reserved-ident flag, parent-threaded name check). Those are valid speedups but out of scope for proving Store.

Design note (rejected for land): `bind-contextual-ident-design.md`.

## Measured then reverted (2026-09-09)

`IdentifierMayBeReserved` + Bind TextAt gate: BindHot `GOGC=off` **−12.3%** (p=0.038); BindKPC inst **−7.7%**, cycles **−6.0%**. Reverted — not Store-specific.
## Reproduce

```sh
# upper bound
go test -C ./tsc -c -o /tmp/bind-p1-verify/base.test ./internal/binder
go test -C ./tsc -overlay /tmp/bind-p1-verify/overlay.json -c -o /tmp/bind-p1-verify/skip.test ./internal/binder
# interleave BenchmarkBindHot/checker.ts; benchstat
# BindKPC: osascript admin driver per kperf-bind-measurement.md
```
