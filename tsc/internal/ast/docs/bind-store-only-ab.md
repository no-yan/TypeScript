# Store-only Bind micro-A/Bs (2026-09-09)

Adoption rule: land only opts pointer Bind cannot mirror. Shared binder/parse wins (MayBeReserved, parent-thread name check) are out of scope even when BindHot moves.

## Results

| Overlay | Store-specific? | BindHot GOGC=off | BindKPC inst | Verdict |
| --- | --- | --- | ---: | --- |
| `SetFlow` full no-op (earlier) | yes (column write) | ~−4% | **−5.2%** | ceiling only; cannot ship |
| `flowIDBind` + skip `mustMutate` + direct `flows[ref]` | yes | ~ (p=0.798) | **−1.4%** | reject |
| `mustMutate` no-op globally | yes | ~ (p=0.959) | — | reject |
| PropertyAccess skip `HandleOf`+`isNarrowableReference` | yes (Handle tax) | ~ (p=0.382) | **−1.9%** | reject as sole Feature |
| NodeRef narrowing in `createFlowCondition` + property SetFlow | yes | ~ (p=0.161) | **−2.1%** (cycles −2.8%) | reject as sole Feature |
| `MayBeReserved` (reverted) | **no** | −12% | −7.7% | rejected by adoption rule |

## Reading

Guards around SetFlow (`mustMutate`, arena address check in `flowID`) are not the Bind wall. The earlier −5% SetFlow no-op is mostly **doing the column store / not doing it**, i.e. the fact that Store maps flow through `flows []uint32` instead of a pointer field — that tax is real but inseparable from the Store flow-index design (already accepted for GC).

Single-site HandleOf on property access is similarly small versus the +70% gap to `8ac035a`.

## Next Store-only directions

1. **Done:** Paired Instruments vs `8ac035a` — `bind-paired-cpu-profiler-8ac035a.md`. Bind weight ~1.73×; Store-only API leaves ≤3% each; mass is walk currency.
2. **Do not** chase more local HandleOf / flowID / MayBeReserved / packed Kind micro-opts.
3. Next Feature: redesign **how Binder loads children/lists** (walk currency), measured with BindKPC vs this Store baseline — not shared binder algorithm skips.
