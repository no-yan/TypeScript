# Binder 基盤移行の実装・検証結果

2026-09-15。[設計](upstream-to-store-translation.md)・[実装指示書](luna-foundation-migration-instructions-20260915.md)・[署名台帳](luna-foundation-signatures-20260915.md)に対応する。

## 実装範囲

- Binder の構文参照・保存状態を NodeRef、構文列を ListRef に移した。公開 query と Symbol.Declarations / ValueDeclaration は Handle を保つ。
- 167関数の署名を Go parser で台帳と照合。setFlowNode / setReturnFlowNode を削除し、newFlowData / getLocals を追加。基本入口は `bind(node ast.NodeRef) bool`。
- Flow を Store から生成し、通常 AST の Node と合成 payload の Data を分離。EndFlow / ReturnFlow / FallthroughFlow をそれぞれ元の所有ノードに保存する。
- SourceFile の Symbol を root 側の列と同期し、JSON の一時宣言後も両方を復元する。Locals の読み取りを能力判定で省略せず、読み取り時には map を作らない。
- 生成 walker を直接 bind / ListRef 走査へ移し、JSDoc の IsNameFirst 順序と childless kind を明示した。未知 kind を物理的な child 配置で走査する fallback は削除した。
- AST → Flow 対応・Flags・NextContainer・Symbol・診断・訪問順を比較する実入力監査 driver と、保存先・Locals・再 bind の単体テストを追加した。

Luna high に区間ごとの移行と検査を割り当て、主担当が宣言処理・container 処理を実装し、差分レビューと実行検証を行った。型変換だけでは、kind 集合の欠落、表示名と復号後の名前の違い、Body child と BodyBase の違いを見落とし得る。Luna への作業分担は可能だが、今回の移行を無監査で任せられるという評価ではない。

## 固定した比較元

| 対象 | revision |
| --- | --- |
| 意味の対照 upstream | `879f9867ac455404e75759dd1739281cf6aa7f85` |
| 周辺 Store と旧 Binder | `85506e8b7d51f2b05f84f9ba1a8c3309b2f30ce4` |
| 検証用 candidate commit | `d7505ce59277cb0117f900e46580d12a58d7810e` |

candidate commit は独立した検証用 repo にのみ存在する。検証時点では作業ブランチは未commitだった。作業木と検証用コピーの5127個の Go source / module 関連ファイルの hash を照合した。参照 baseline を承認・変更していない。

## 検証

| 検証 | 結果 |
| --- | --- |
| 全シグネチャ | 167 / 167 一致、余分な関数なし |
| 意味監査 | 11入力で固定 upstream と完全一致。構文形状、診断、訪問順、Symbol、Flow、全 AST 保存対応の差0 |
| 監査の変異検出 | Flow / EndFlow / ReturnFlow / FallthroughFlow、EndFlow の誤った所有先、Flags、NextContainer、file.Symbol の8種類を検出 |
| package test | 最終コードで ast / binder / checker / compiler の4 package がすべて成功 |
| 正規生成の再現性 | 作業木と独立コピーの両方で全18出力の再生成一致。独立コピーの tracked diff 0 |
| conformance | 旧 Store と全件の結果・内容が一致。pass 6429 / 既存 fail 116 / skip 1165。candidate の A/A も内容まで一致 |

監査入力は TS / JS / JSON / TSX、export・class・signature・関数と IIFE・loop / try / switch・binding pattern・JSDoc・strict mode と escaped spelling・parse error を含む。監査を全 TypeScript 入力への同等性証明とは扱わず、conformance と消費側 package test を別に実行する。

旧 Store Binder との監査差は15 field。JSON root Symbol の同期、static block の余分な EndFlow / HasImplicitReturn、case の EndFlow / FallthroughFlow の混同、匿名関数の Symbol 名が変わり、今回の結果は固定 upstream 側に一致した。pointer アドレスや Store の数値 ref を同一視せず、構造パスと共通の Flow / Symbol ID 空間で比較した。

固定 upstream との全 conformance 比較は入力契約とテスト構成が異なるため `incomparable`。旧 Store と candidate の入力は同じ5930ファイルであり、こちらを全件の退行確認に用いた。upstream との11入力の意味監査とは区別する。

## 制約と次の実装

- 固定 upstream の ModuleDeclaration.Attributes は現行 Store schema / parser にない。この属性専用の名前生成・診断の2分岐は対象木に到達しないため含めない。構文対応は schema / parser / checker を合わせた別変更が必要。
- static block は Body child を持つが upstream BodyBase を持たない。EndFlow / HasImplicitReturn は保存せず、ReturnFlow は保存する。
- FlowNode.Data の `*ast.Node` は残す。pointer-node-removal-plan の Step 1 は今回の互換基準版の後に独立して行う。
- 実装検証後の[性能比較](foundation-migration-performance-20260915.md)で、命令数とcheckerの割り当ての退行を確認した。`bind(node, kind)` の個別効果は未測定。引数列の直接走査による修正候補は取り込んでいない。

## 証拠と再実行

artifact root:

`/Volumes/SanDisk1TB/Library/Caches/binder-foundation-20260915-164851`

主要な記録:

- `candidate-final-source-identity.json`、`signatures.json`、`upstream-to-candidate.patch`、`audit-implementation/`
- `audit-{upstream,head,candidate}-verified/{identity.json,build.log,test.log,snapshots/}`
- `audit-upstream-to-candidate-verified/summary.json`、`audit-head-to-candidate-verified/*.json`
- `packages-head.log`、`packages-candidate-final.log`
- `before.sha256`、`after.sha256`、`generate-second.log`、`candidate-generation/`
- `conformance-{upstream,head,candidate}-{a,b}/`、`conformance-{upstream,head,candidate}-aa/`
- `conformance-head-to-candidate/`、`conformance-upstream-to-candidate/`、`conformance-candidate-source-check.json`

意味監査は repo root で以下の形で再実行する。OUT は毎回新しい空ディレクトリ名にする。比較元の source を先に固定する。

```sh
python3 tools/scripts/tsc/binder_semantic_audit.py selftest
python3 tools/scripts/tsc/binder_semantic_audit.py run --repo SOURCE --representation pointer --out OLD
python3 tools/scripts/tsc/binder_semantic_audit.py run --repo . --representation store --out NEW
python3 tools/scripts/tsc/binder_semantic_audit.py compare --old OLD --new NEW --out DIFF
```

conformance の再実行は [verify-conformance](../../../../.codex/skills/verify-conformance/SKILL.md) に従う。dirty な作業木を HEAD として検証しない。
