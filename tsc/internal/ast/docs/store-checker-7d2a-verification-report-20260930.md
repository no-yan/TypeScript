# Store checker 7d-2a 独立検証報告

検証日: 2026-09-30 (JST)。検証指示書、実装指示書、Pointer/Store の実コードを照合した。実装報告の成功の記述を証拠として代用していない。

## 1. 結論

**不合格。実行で再現する正しさの問題が 2 件ある。** 生成再現・build・vet と既存 corpus 等価の成功は確認したが、全体の正しさは認定しない。実装は修正していない。

1. `storeparser.ParseIsolatedEntityName` の有効な戻り値で `Identifier.Text()` が panic する。
2. 新設した List helper は、ゼロ値 List を nil slice と同じように扱わず panic する。

さらに、要求された nameresolver の JSDocParameterTag / JSDocReturnTag の実際の分岐は今回 0 回であり、報告の祖先件数は分岐実行の証明にならない。`TestHasDataCorpus` は parser corpus では有効だが、`HasFlowNodeData` が全 Factory 入力で Pointer と等価であることは反例により否定される。KPC の実測線は全て満たした。ただし Parse の時間 / cycles 増加があり、性能不変は認定しない (§8)。

| 検証線 | 判定 | 独立した根拠 |
|---|---|---|
| 再生成と source 不変 | 合格 | 対象 146 file の SHA256 一致、Go/TS source の原 checkout / snapshot 差 0 |
| build / vet / 変換器試験 | 合格 | 全 build、指定 vet、変換器両タグの再実行成功 |
| production 依存・unsafe・Pointer ガード | 合格 | store への production 依存 0、unsafe 増加 0、指定 Pointer 範囲 diff 空 |
| Parse / Bind corpus | 合格 (既知 seed を除く) | 各 17,415 file、不一致 0、宣言 Store 検査 717,627、dead 22,793 |
| nameresolver 戻り値 / callback | 合格した範囲あり | 10,235,856 呼出し / 側、不一致 0、fixture 9 本、破壊検出 |
| nameresolver 指定 JSDoc 分岐 | 不合格 | ParameterTag / ReturnTag の function restriction 分岐実行各 0 |
| printer 単体等価 | 合格した範囲あり | 55 cell、不一致 0、panic 原因の独立採取、nil fixture 破壊検出 |
| ParseIsolatedEntityName 境界 | 不合格 | 正常入力 3 件とも Text() panic |
| List helper の nil 契約 | 不合格 | SomeList / FilterList の zero List 入力で panic |
| HasData corpus / 一般等価 | corpus 合格 / 一般等価不成立 | 2,638,926 node 一致、Factory NewToken 6 kind の反例 |
| 既存 hot path 性能 | 実測線は合格 / 不変は未確認 | cycles 比は全線内、Parse に有意な時間 / cycles 増加 (§8) |

## 2. 対象と証拠の保存

選択 repo は `/Volumes/SanDisk1TB/ghq/github.com/no-yan/TypeScript`。HEAD / before は `2341effa515a6fc7b5788da7f09cd08967377da5`。after はその HEAD 上の未コミット実装。

別の検証処理が同じ checkout で動いていたため、全 source の独立コピーを `/private/tmp/7d2a-independent-20260930` に作成して実行した。testdata と node_modules は元 checkout のものを読む。検証前後と一時変異の復元後に対象 source 146 file を SHA256 で照合し、元 checkout と snapshot のどちらも差 0。Pointer を含む Go/TS source も再生成後に原 checkout と照合して差 0 を確認した。

この報告が依拠する実行出力は [_store-checker-7d2a-results/independent](_store-checker-7d2a-results/independent/metadata.json)。同じ結果ディレクトリの他の検証者のログを、この独立実行の成功として引用していない。source 抽出・型解決グラフ・before 比較は別担当が作成し、親担当も関連 diff・経路・実コードを確認した。

環境は Go `go1.27.1 darwin/arm64`、Node `v24.3.0`、Apple M1。途中は高負荷で、重複した初回コンパイルを停止して以後の実行を順番にした。初回 tools 試験の timeout は `test-storetranslate-initial-timeout.txt` に保存し、負荷整理後に `-count=1 -timeout=30m` で再実行した結果を採用した。開始時 source、途中環境、終了環境は `snapshot-sha256.json`、`env-checkpoint.txt`、`env-end.txt`、最終照合は `source-final-check.json`。

## 3. 再現・build・既存試験

`node tools/scripts/tsc/generate.ts` と `GOWORK=off STORETRANSLATE_REACH=1 go run ./cmd/storetranslate` を snapshot で実行。対象 146 file の生成前後差 0、Go/TS source の原 checkout との差 0。生成器の初回 dprint cache 書込み拒否は記録し、権限付き再実行で完了した。生成物の header inventory は `headers.txt` (production 110 file、generated header 62)。手書き API / P2 と生成物を区別した。

- `GOWORK=off go build ./...`: 成功。
- tsc の ast と全 store package (storemodule も含む)、tools の storetranslate の vet: 成功。
- tools `go test -count=1 ./cmd/storetranslate/`: 通常タグと storechecks の両方で成功。
- tsc `go test -count=1 -v ./internal/ast/store/... ./internal/store...`: 両タグとも唯一の失敗は `FuzzEquivalence/a708303f611b95c8`、理由も `ExternalModuleIndicator differs from the Pointer file`。別の失敗はない。
- Parse: 17,415 file (JS 1,456)、訪問 2,584,467 node、JSDoc 比較 54,459 node、診断差 0、dead 22,793。Bind: 17,415 compared、parse mismatch 0、宣言 Store 717,627 検査。
- `var _ ast.HasFileName = (*store.File)(nil)` を境界 probe に置き、コンパイル成功。

生出力は `test-notag.txt`、`test-storechecks.txt`、`test-storetranslate*.txt`、`build.txt`、`vet-*.txt`、`regen-*.txt`。exit は `rest-exits.json` と `additional-exits.json`。無音の build/vet 成功を stdout の空だけで判定せず exit 0 を確認した。

## 4. 機械性・到達・P2

詳細と source graph は [mechanical-audit.md](_store-checker-7d2a-results/mechanical-audit.md) と同ディレクトリの raw diff / selection / reach chain。

- 型 / store API に基づく規則を確認。R8 の ownerFactory は receiver / 引数の struct field の NodeFactory 型を調べ、候補 0 / 複数は problem。変数名 c/b/f で選ばない。対応 API の名前表と core helper 表は存在するため、「名前表が全く無い」とは述べない。特定 checker 関数に依存する表は見つからない。
- R8 の元 27 箇所を確認し、追加を含む generated writer 35 と manual writer 7 を点検。`nodecopy.go` の NodeList Loc は SetListRange。nodebuilderimpl の ImportType は Factory の NewImportTypeNode から来ることを関数の戻り値分岐で追い、comment を根拠にしなかった。source への直接書込みは静的追跡で見つからないが、全 writer の実行は 2b で未検証。
- checker / printer の `.Nodes()` は 14 箇所 (生成 12、manual 2)。原 checker 一覧 11 と、printer 2、manual closure 1 の差は報告 §2.3 が説明している。`getEffectiveCallArguments` は generated。読みのまとめ (g) は入っていない。
- 到達数は再生成出力で一致。checker.go は 1,490 / 1,509、services 3 / 66、exports 2 / 92、emitresolver 27 / 64、printer.go 374 / 378。残る interface method には CHA の型実装による理由がある。
- 刈った 20 関数を seed 20260930 で選択し、型解決した逆参照の closure を再帰点検。到達 caller 0。services / exports / emitresolver の survivors 全 32 に root への経路を保存し、独立した source graph の 248 辺で照合。SymbolTracker の全 interface method の残存を確認。Program の実装は 2b の adapter の範囲であり、ここで実装済とは認定しない。
- P2 全 73 関数 + Program 型 1 の元宣言 hash を独立照合し、全 74 一致。関数を Pointer と並べて読んだ。NewChecker は規則置換の差と region / Factory の 2 行。hash を 000... に改変して CLI が停止することも確認し、復元後 SHA256 差 0。
- P2 関数数 / 宣言行: store 20 / 270、checker 36 / 1,333 (+ Program 型 26)、printer 11 / 232、scanner 3 / 16、pseudochecker 2 / 22、module 1 / 54。実装報告と一致。
- 生成物 20 関数 (checker 10、flow / relater / nodebuilderimpl / jsx 計 10) を層別乱択で比較。nil、node ==、list range、map key、writer を含め、規則外の制御・短絡評価・副作用順の変更は見つからない。API の意味の正しさは別途下記の反例で判定した。

## 5. 単体等価と試験の感度

### 5.1 nameresolver

Pointer parser/binder と Store parser/binder は独立。symbol 対は Resolve より前に bindComparer が作る。戻り値を使って対応を後付けしていない。callback の意味、順序・引数の比較と globals / excludeGlobals / alias / isUse / message / invalid initializer / cache の fixture をコードで照合した。

17,415 file、10,235,856 Resolve / 側、nil 以外 1,803,200。Value 1,412,216、Type 355,476、Namespace 35,508。戻り値 / callback 不一致 0。callback の全件数は `test-notag.txt` と `test-instrumented.txt`。fixture 9 本は各 12 呼出し、指定 Pointer callback が現れることを assert する。

JSDoc 対応 helper が配列長 / 部分木長の差を continue で除外する弱点は実在する。独立 overlay でその 2 箇所を panic に替えて全 corpus の resolver と HasDataCorpus を再実行した。今回は panic せず PASS したため、**今回の対象 corpus でその silent skip は 0**。将来の不一致を検出する元試験の保証は弱いまま。

| 必須の場所 / 名前 | 独立計数した Resolve 呼出し |
|---|---:|
| 関数の祖先あり | 5,476,488 |
| 入れ子関数 (関数の祖先が 2 個以上) | 2,944,296 |
| class | 966,888 |
| namespace | 1,072,488 |
| enum | 101,616 |
| with | 1,344 |
| catch | 8,856 |
| type parameter 内 | 276,000 |
| arguments | 7,908 |
| JS require | 1,692 |

祖先 / 名前を持つ identifier に対する 12 設定の呼出しを計数した。name が require という条件は実際の require fallback 成功件数とは区別する。

**報告の JSDoc 分岐保証は成立しない。** Pointer resolver の function-like lookup restriction の本体に counter を置いた結果、lastLocation は KindJSDoc 7,752 回、KindJSDocParameterTag 0 回、KindJSDocReturnTag 0 回。祖先件数 7,295 / 329 / 126 は再現するが、その ancestor が存在しても locals lookup で結果が見つからなければ当該分岐に入らない。最後の成功 callback の kind を数える方法も分岐そのものの計数ではない。後 2 kind に関する指示 §3.1.3 は未充足。

### 5.2 printer

相手は Pointer parser + Pointer printer。5 options は checker/printer.go と一致。source と clone は同一 program の IDSpace / region に置き、clone は実際に印字する EmitContext の Factory が作る。

17,415 file、55 cell、25,163,865 印字、不一致 0、Pointer 出力合計 687,570,949 bytes。全 cell で出力は十分あり、最少でも 3,878,555 bytes。双方 panic 14,110、最大 cell 比率は 0.2472% で、どの cell も panic で埋まっていない。55 cell の全表は [printer-cells.md](_store-checker-7d2a-results/independent/printer-cells.md)。

元試験は panic 値を捨てるため、独立 overlay で file / corpus index / setting / node kind / 両 panic 値を保存した。双方 panic の内容が違うものは 0。95 corpus 入力に存在し、理由は Pointer 自体の emitType / emitStatement の既知の default panic に一致する:

- `unhandled TypeNode: KindTemplateLiteralTypeSpan`: 13,640 回。
- `unhandled statement: KindJSImportDeclaration`: 470 回。

全発生キーは `test-instrumented.txt`、元 testdata の物理ファイル対応は `panic-corpus-origins.txt`。仮想ファイル名 /index.ts だけで元ファイルを推定していない。

nil fixture の元 TypeReference は、source file の引数だけを替えて Pointer が `\\u0061` / `a`、Store が各々に一致。clone は両方 `a`。これは canUseSourceFile の currentSourceFile 項を確実に突く。ほかの nil 固有分岐 (comment、trailing comma、line formatting、literal text 等) 全てをこの fixture が突くとは認定しない。source 有無による corpus 出力差は cell 表に記録した。

EmitContext の textSource / original / synthetic comment は Pointer の on/off 出力が変わり、それぞれ Store と一致。emit flags の入力は on/off とも `"あ"` で、試験 PASS は flags の読取り感度を証明しない。報告にも同じ出力の記載があるため、この項目を出力差のある試験として扱わない。Store textSource fixture は private map への設定であり、production の setter 経路を検証したとはしない。

### 5.3 意図的な破壊

全て隔離 snapshot の overlay 又は復元を伴う hash 変更。失敗理由がコンパイルエラーではなく期待の mismatch / hash 拒否であることを raw output で確認した。

| 変更 | 検出 |
|---|---|
| locals を nil 表にする | resolver 戻り値 / callback mismatch |
| globals lookup を消す | globals fixture 失敗 |
| SymbolReferenced 呼出しを消す | isUse fixture の callback 差で失敗 |
| canUseSourceFile を false にする | nil fixture で Store `a` / Pointer escape の差 |
| HasFlowNodeData の FalseKeyword を false に戻す | corpus #3 の KindFalseKeyword で失敗 |
| P2 hash を 000... にする | 変換器 CLI が GetLocals hash mismatch で停止 |

出力は `mutation-*.txt`、exit は全て期待どおり非 0。復元後 source 差 0 (`after-mutations-sha256.txt`)。

## 6. 再現した正しさの問題

### 6.1 ParseIsolatedEntityName の識別子 text を壊す

**問題 / 証拠:** `FinishSynthetic` (store/checker_api.go) は全 node の pos/end を -1 にする。一方 `Node.identifierText` (store/store.go) の通常 identifier は `s.src[data:end]` を読む。実行結果 `boundary.txt` で `React.createElement`、`h`、`a.b.c` の全 identifier の Text() が `slice bounds out of range [:-1]`。Pointer は同じ tree を markAsSynthetic した後も text を読める。

Pointer の公開 parser.ParseIsolatedEntityName は元の位置を保持する。位置を -1 にするのは checker/jsx.go の wrapper の markAsSynthetic。指示書の位置要求を評価する際は、その wrapper と同じ処理を Pointer に明示的に適用して比較した。parser 自体が最初から -1 を返すという説明は source と一致しない。

不正な `a.`、`1`、空は双方 nil。StoreID は program counter の 7 / 8 / 9、File は nil、sealed=true、Factory 書込み拒否、region 無し EmitContext の拒否は実行確認できた。これらの成功は text の不具合を相殺しない。

**再現:** snapshot の tsc で `GOWORK=off go run ./internal/ast/docs/_verification/probe.go`。probe 原文は `independent/boundary-probe.go.txt`。本番 API だけでも `n := storeparser.ParseIsolatedEntityName(store.NewIDSpace(0), "h"); n.Text()` が panic する。

**原因の仮説 / 提案:** identifier text が range の end に依存したまま synthetic 化されている。range を壊す前に text を range 非依存の保存形式へ移すなど、通常・escaped identifier とその複製で text を保存する必要がある。具体的な表現の選択は性能・容量への影響も考慮する。

**受入条件:** 指定 6 入力で kind / text / nil が Pointer checker wrapper と一致、全 valid node の位置 -1、sealed / StoreID / File nil / writer 拒否も維持。plain / escaped / qualified identifier の Text と印字で panic しない。

### 6.2 zero List の nil helper 契約不一致

**問題 / 証拠:** node_helpers.go は「nil List を nil slice として扱う」と記述するが、SomeList / FilterList 等は IsNil を見ず Len を呼ぶ。List.Len は owner が nil の `List{}` を読めず panic。`zero-list-probe.txt` で Pointer core.Some(nil)=false、core.Filter(nil) の長さ=0 に対し、Store の両 helper は nil pointer panic。

**再現:** `GOWORK=off go run ./probes/zero-list`。原文 `independent/zero-list-probe.go.txt`。MapList / Nodes は IsNil guard があるので同じ問題を回避する。source の所有者を持つ block 0 sentinel と zero List は同じ IsNil=true でも Len の安全性が違い、通常 parser corpus は後者を網羅しない。

**原因の仮説 / 提案:** owned sentinel の Len=0 を zero List も共有すると誤認した helper。nil を受ける helper で IsNil を扱う、又は List API の zero 契約を定める。全 Len 呼出しのコストを増やす案と helper guard 案は区別して評価する。

**受入条件:** zero / owned nil / 空の live list / foreign edge を持つ live list で、各 Pointer helper と戻り値・順序・early exit・callback 回数が一致。List helper と P2 List 版の nil 入力を試験する。実際の Check corpus の全経路まで再現した主張はしない。

## 7. 今回追加した対象

### 7.1 TestHasDataCorpus / HasFlowNodeData 修正 (§5.5)

17,415 file、2,638,926 node、272 kind、9 述語全て一致。true 件数は Declaration 576,627、Exportable 141,694、LocalsContainer 171,866、FunctionLike 67,998、ClassLike 10,950、Body 52,075、FlowNode 1,329,039、LiteralLike 169,109、TemplateLiteralLike 5,716。JSDoc silent skip を禁止した再実行でも同じ成功。

生成器の内部 schema を独立に読み、旧 kindDefinition 1 定義の base と全 definition の base union を差分計算した (`hasdata-base-delta.json`)。増えるのは HasFlowNodeData の Null / True / False / This / Super / Import の 6 kind だけ、他 8 述語に増減なし。False を故意に戻すと corpus 試験が落ちるので、追加試験がこの修正を検出する感度はある。

ただし `hasdata-factory-counterexample.txt` で、Factory.NewToken の同 6 kind は Pointer の FlowNodeData()!=nil が false、Store HasFlowNodeData が true。Pointer は具象 data の型で dispatch し、Store は kind のみで判定することが原因。報告に書かれた限界は実在し、生成コメントの無条件の「Pointer と同じ述語」という説明は厳密には誤り。一般等価を必要とするなら生成経路の区別を保存するか、用途を限定した述語として契約と caller を検証する必要がある。checker が該当 Factory Token を決して受け取らないという全経路証明は今回得ていない。

### 7.2 後から了承された 4 判断 (§7.2 の 1–4)

本依頼のユーザーが後から了承したと述べているため、了承はその直接の情報を根拠に扱う。実装報告だけを了承の証拠にせず、了承済であることを正しさの証拠にも使わない。

| 判断 | source から確認したこと | 留保 / 訂正 |
|---|---|---|
| referenceresolver を変換 | emitresolver の NewReferenceResolver 等の使用、node を含む型、15 関数の出力を確認 | 差分は機械規則と対応。実行の全経路は 2b |
| storemodule を追加 | P2 本体は Pointer と対応、File 型の変更、ResolvedModule は元 module 型を保持 | GetResolutionDiagnostic の返り値は *diagnostics.Message。報告の「*ast.Diagnostic を含む struct を返す」はこの関数の説明として誤り。診断を含むのは引数側 ResolvedModule |
| Has<X>Data 述語を生成 | schema / generated code / corpus / mutation / Factory 反例を確認 | parser corpus には適切だが一般等価ではない (§7.1) |
| List 版を 5 つ追加 | 5 本全て Pointer と条件・順序が対応、型による List 版選択と caller を確認 | 「どれも slice と List 両方の caller」は誤り。appendTypeParameters / getInstantiatedConstructorsForTypeArguments の slice 版には caller が無い。保持自体は意味の bug ではない |

## 8. 既存 hot path と KPC

before の関数を receiver 型 + 名前 + file で対にして comment を除いた Go AST 本体を比較し、共通不変 2,832、変更 16、削除 0、新規 646。store/utilities.go の既存関数は全て不変。全一覧は [performance-guard-audit.md](_store-checker-7d2a-results/performance-guard-audit.md) と `performance-audit/function-body-diff.txt`。

変更は SyntheticExpression 関連の Builder / Factory 5 関数、Node.ForEachChild、File 移行による parser / binder の 10 関数。File 移行全 hunk を getter / constructor / setter の置換として確認した。生成 accessor 等 874 件の inline 判定 / cost は before と同じ (node 38、Parent 45、List.At 78、FlowNode 48、Symbol 40)。

Walk は walker → ForEachChild、Parse は parseSourceFileWorker → finishSourceFile、Bind は source file bind と ForEachChild の経路で変更した関数を呼ぶ。SyntheticExpression が parse corpus に無いことだけで ForEachChild の命令列不変は認定できず、**Walk も含め 3 stage 全ての KPC が必要**。

測定結果 `performance-audit/kpc-*-20260930-212434.txt` を受領し、9 本全てに対象 Benchmark 行と PASS を確認した。選択 repo / git rev は環境ログと現在の checkout で一致。解析時に source-before / source-after / binaries の 3 SHA256 manifest を全件照合して成功。after は未コミット実装なので rev だけでなく source hash も根拠にした。今回の artifact set は **`current`**、7d-1 の既存結果は引き続き `stale`。今回の 3 stage に `missing` / `unsupported` はない。

`benchstat before1.txt after.txt before2.txt` と `benchstat before2.txt after.txt` を stage ごとに実行し、raw と比較出力を保存した。比較は手作業の目視で代用していない。raw は各 variant 5 標本、20x。benchstat は標本 6 未満のため 95% CI を出せないと警告する。非有意な差を同等性の証明として扱わない。

| 線 (同一 binary の Store / Pointer cycles 中央値比) | before1 | after | before2 | 線 | 判定 |
|---|---:|---:|---:|---:|---|
| Walk (checker.ts) | 1.0960 | 1.0922 | 1.0934 | ≤1.15 | 実測線内 |
| Parse (checker.ts) | 0.8241 | 0.8321 | 0.8239 | ≤1.00 | 実測線内 |
| Parse (dom) | 0.8259 | 0.8252 | 0.8264 | ≤1.00 | 実測線内 |
| Bind (checker.ts、cycles/node) | 1.0215 | 1.0026 | 1.0205 | ≤1.10 | 実測線内 |

Walk の Benchmark は両側の訪問数が同じ baseline に一致することを毎回 assert しているので、cycles/op 比は cycles/visit 比と一致する。比の表は集約の補助であり、前後の統計比較は benchstat を根拠にした。

after の raw 中央値 (ns/op は wall time、cycles / instructions とは別の指標):

| workload | 側 | ns/op | B/op | allocs/op | cycles/op | inst/op | IPC |
|---|---|---:|---:|---:|---:|---:|---:|
| walk checker.ts | Pointer | 2,411,608 | 0 | 0 | 7,635,538 | 21,582,694 | 2.827 |
| walk checker.ts | Store | 2,633,156 | 0 | 0 | 8,339,570 | 27,973,563 | 3.353 |
| parse checker.ts | Pointer | 21,310,358 | 26,137,522 | 11,931 | 67,165,288 | 261,143,521 | 3.887 |
| parse checker.ts | Store | 17,721,898 | 15,997,514 | 639 | 55,886,794 | 217,393,468 | 3.890 |
| parse dom | Pointer | 9,630,442 | 9,906,383 | 2,790 | 30,434,857 | 124,224,460 | 4.080 |
| parse dom | Store | 7,954,342 | 6,038,936 | 1,534 | 25,113,587 | 112,411,923 | 4.477 |
| bind checker.ts | Pointer | 10,837,440 | 7,423,983 | 13,952 | 34,007,874 | 110,717,067 | 3.255 |
| bind checker.ts | Store | 11,013,319 | 6,953,859 | 12,632 | 34,093,708 | 126,507,171 | 3.713 |

**性能不変とは言えない。** Store Parse checker.ts は before1 に対し時間 +2.56%、cycles +2.37%、before2 に対し時間 +2.12%、cycles +2.61% (各 p=0.008)。Store Parse dom も時間 +1.10% / +0.83%、cycles +0.69% / +1.03% (before1 / before2、各 p=0.008)。これはこの測定セットで実証された増加であり、無視しない。

Pointer Parse も時間が checker.ts +1.74% / +1.58%、dom +1.02% / +0.72% 増えている。Pointer 側の source は不変なので、測定環境又は binary / compiler layout 等の影響も候補になる。Pointer の同方向の増加は Store の変化を全て説明する証拠にはならず、checker.ts の Store / Pointer 比も before 両側より約 1% 上がった。現状はコード変更による固有の回帰かを切り分けられていない。

Walk の時間 / cycles / inst の差は benchstat で有意と判定されなかった。Bind Store の時間は before1 に対し +1.79% (p=0.008)、before2 に対する +1.05% は未解決 (p=0.056)。Store Bind cycles はどちらの before との差も未解決、inst は before2 に対し +0.41% (p=0.032)。単に「全段不変」とはまとめない。Bind 比が良くなったのは Pointer cycles の増加も含み、Store の改善と断定しない。

割当回数は全行で before と同じ。Store Parse の B/op は checker.ts / dom とも **+160 B/op**、Bind は両側とも **+16 B/op**、Walk は 0 のまま。File の実 struct サイズを別 probe で確認すると 464→576 bytes (+112、`file-size-20260930-212434.txt`)。cache field の追加による File の増大は割当費用の driver の一つだが、sizeof と allocator B/op は同じではなく、+160 全体の内訳と Bind 両側 +16 の原因は未確認。profile を採っていないので allocation hot path の割合は判定しない。

hot paths は walker / ForEachChild、parseSourceFileWorker / finishSourceFile、BindSourceFile / ForEachChild。ステージ全体は File 生成より広く、+160 B/op のような狭い費用は File の allocation を分離した方が追いやすい。synthetic microbenchmark の結果による実 workload のボトルネック認定は行っていない。

測定は 21:24–21:25 JST、開始 load 1.48 / 1.87 / 3.18、終了 4.92 / 2.64 / 3.38。load <1.5 は開始時に満たすが、全測定中の低負荷を記録したログはない。終了 load だけから外部負荷が原因と断定もしない。

生出力、3 列比較、before2 対 after 比較、全中央値、metadata は `performance-audit/kpc-*-20260930-212434.txt`、`benchstat-three-*-20260930-212434.txt`、`benchstat-before2-after-*-20260930-212434.txt`、`kpc-medians-20260930-212434.json`、`metadata-20260930-212434.json`。大きな before / binary は引き続き `/private/tmp/store-7d2a-perf` に置く。

次の行動は正しさの修正を優先し、その後の現実入力 Parse で同じ before1→after→before2 比較を行って、共通の変動と Store 固有の増加を区別すること。CI を求めるなら少なくとも 6 標本以上とする。今回の線は満たすため、この結果だけを理由に直ちに同じ測定を繰り返さない。修正が入れば現在の artifact は source hash 不一致となり `stale` にする。小さな変更に一律 3% の採用条件は適用しない。

## 9. 次の行動と再現

正しさの 2 不具合を直し、境界と nil の小さい対照試験を置くことが先。JSDocParameterTag / ReturnTag の実分岐を通す fixture、silent skip をエラーにする保証、panic 内容の保持、emit flags の感度と nil 固有分岐の不足も試験で明確にする。HasFlowNodeData の一般契約 / 使用制限を決める。了承済の判断 4 件について改めて了承を求める必要はない。

再現する場合は、保存した `snapshot-sha256.json` と対象 source の一致を先に確認し、上記 snapshot の tsc / tools で実行する。全試験は `GOWORK=off GOMAXPROCS=2 go test -p 1 -count=1 -timeout=30m -v ...`、タグ版は `-tags storechecks`。再生成コマンド、overlay の追加計数と 6 破壊試験の実行順・復元処理は `independent/run-additional.py.txt`、probe 原文も同じディレクトリに保存した。大きな before / binary / compiler 生出力は scratchpad に置き、repo 内に追加していない。

KPC の実測線は満たしたが、正しさの反例 2 件により現時点の不合格は変わらない。checker 本体を corpus で動かす 2b / end-to-end 性能の 2c は、この検証によって完了したとは扱わない。
