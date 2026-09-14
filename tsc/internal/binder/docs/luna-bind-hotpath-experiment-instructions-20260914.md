# Luna実装指示: bind Identifierの比較実験（2026-09-14）

この文書を渡された実装担当は、下記の初回実験を実装し、再現可能な比較候補と意味検証結果を引き渡す。文章の提案だけで終えない。測定未実施を高速化成功と呼ばない。会話・報告は日本語。

## 0. 到達点と範囲

初回は **B0 / S / L / N / LR の5条件**を作る。前案の4条件に、LRの呼出し経路だけを変える対照Nを追加した。仮説は「Identifierの入口dispatchと、名前位置を知る親からの再判定を省くとCPU費用が減るか」。A全体、C〜G、parser変更は今回実装しない。

完了条件:
- 5条件を凍結sourceから再ビルドできる。
- 同一テストで診断・Flow・flags・Symbol/CFG結果を比較でき、実行して一致を確認した。
- 機械語のCALL、frame、inline、text sizeを比較し、何が消えたかを記録した。
- 寿命検証済みハーネスで測定できるdriver、fixture manifest、実行手順が揃う。
- `docs/luna-bind-hotpath-experiment-results-20260914.md` に実装・検証・未測定項目を記録する。

この依頼文による実装段階ではunit/意味検証/ビルド/機械語確認/ハーネス寿命検証まで実行する。長時間benchmark、PMUの管理者実行、新規Instruments記録は開始せず、測定driverを完成させて渡す。後続のユーザーが測定も明示的に指示した場合はその範囲で実行する。commit/push/PR/mergeはこの文書では指示しない。

## 1. 読む順序と初期保存

1. リポジトリおよびbinderのAGENTS.md。
2. `docs/bind-hotpath-chain-analysis-20260914.md`（仮説集。下記訂正を優先）。
3. `docs/binder-investigation-plan-20260910.md`、`docs/bind-batch-lifetime-fix-20260911.md`。
4. `../ast/docs/bind-ident-contextual-bench.md`、`../ast/docs/bind-store-only-ab.md`（binderからの相対パス）。

開始時のselected repoは `/Volumes/SanDisk1TB/worktree/cursor-ast-store-tests`、想定HEADは `1be4c8a1e64bda712843fe6d940fed3a44e7be78`。違う場合は作業を巻き戻さず、下記関数の制御フローを再確認して差分を記録する。generator/store/binder/parserにはユーザーの説明コメントがある。上書き・restoreしない。

artifact directoryは初回空の `/private/tmp/bind-hotpath-luna-20260914-<実行時刻>/` とし、以降OUTと呼ぶ。開始時に `git status --short`、HEAD、tracked diff、変更対象source、Go/Nodeのversionを保存。B0は開始時の実ファイルをコピーして固定し、HEADのファイルでユーザー変更を消さない。

全variantはOUT/variants/<name>/に変更対象の完全ファイルとGo overlay.jsonを保存する。overlayのReplaceキーはselected repoの絶対パス、値はvariantの絶対パス。S/L/N/LRを段階的に作成し、各段のファイルを保存してから次へ進む。途中のvariantを後から最新ファイルで上書きしない。実行時分岐や環境変数でvariantを切り替えるコードを製品hot pathに入れない。

## 2. 訂正済みの意味契約

- IdentifierはLastToken以下なので、bindKind末尾のgetContainerFlagsRefとbindChildrenRefを通らない。3switch削減と数えない。
- SetFlow(ref,b.currentFlow)は必ず従来と同じ順に実行。直接flows書込みやcurrentFlowID追加は禁止。
- PrivateIdentifier、ThisKeyword、SuperKeyword、他tokenは全て元の経路。ThisKeywordのseenThisKeywordも変更しない。
- Diagnosticsの有無、Ambient/JSDoc、strict mode、AwaitContext/YieldContext、予約語の判定と診断順を維持。
- `ThisNodeHasError`から`ThisNodeOrAnySubNodesHasError`への伝播とseenParseErrorを維持。Gのerror-free shortcutを混ぜない。
- 名前位置の既知情報は初回 **QualifiedName.Rightだけ**。schemaのDeclarationName等から自動推論しない。VariableDeclaration.Name、BindingElement.Name、Parameter.Nameは既知名として扱わない。
- 公開accessorの安全検査、子の走査順、functions-firstの2pass、AST/listの構造を変えない。

## 3. 変更ファイル

本体変更を許すファイル:
- `tsc/internal/binder/binder.go`
- `tools/scripts/tsc/generate-go-ast.ts`
- 生成結果 `tsc/internal/binder/bindwalk_generated.go`

追加可: binderの`*_test.go`、検証用snapshot/overlay driver、今回の結果文書。測定driverはOUTへ置き、再実行に必要な最小source/手順は結果文書に添付する。ast/store.go、parser.go、schema ast.json、既存benchmarkの寿命ループは変更しない。生成ファイルの手編集は禁止。

## 4. S: 構造対照

B0から次だけ変更する。

1. `bindIdentifierEffects(ref ast.NodeRef, knownName bool)` を追加する。本体はSetFlow、その後contextual identifier検査だけ。error epilogueを入れない。
2. 既存`checkContextualIdentifierRef(ref)`の名前と契約を保持し、`checkContextualIdentifierRefWithNameContext(ref, false)`へ委譲する。
3. `checkContextualIdentifierRefWithNameContext(ref, knownName)`へ旧本体を移す。変更は、旧Ambient/JSDoc/isIdentifierNameRefによるreturn条件にknownNameを加えるだけ。順序はdiagnostics→FlagsAt→Ambient/JSDoc→knownName→旧isIdentifierNameRef→TextAt/keyword/diagnostics。false時は旧判定をそのまま実行する。
4. `bindKind`のIdentifier caseのSetFlow+検査を `b.bindIdentifierEffects(id, false)` に置き換える。共通末尾は完全に元のまま。Identifier caseでreturnしない。

Sはコード構造による費用を測る対照。noinlineを付けない。compilerがhelperをinlineして消しても正常な結果として記録する。

## 5. L: Identifier入口短縮

Sから、`bindChildRef`だけを次の構造にする（返り値なし）。

```go
if ref == 0 { return }
kind := b.store.KindAt(ref)
if kind == ast.KindIdentifier {
    b.bindIdentifierEffects(ref, false)
    b.finishIdentifierBinding(ref)
    return
}
b.bindN(bindNode{ref: ref, kind: kind}, parentKind)
```

追加する`finishIdentifierBinding(ref)`は、Identifierで実行される既存の共通末尾を正確に移す:

```go
flags := b.store.FlagsAt(ref)
if flags&ast.NodeFlagsThisNodeHasError != 0 {
    b.store.SetFlagsAt(ref,
        b.store.FlagsAt(ref)|ast.NodeFlagsThisNodeOrAnySubNodesHasError)
    b.seenParseError = true
}
```

SetFlagsAt直前のFlagsAtを最初のflagsへ置き換えない。seenParseErrorが既にtrueならfalseに戻さない。このhelperはSetFlow/予約語検査の**後**に一度だけ呼ぶ。bindKindの末尾をさらに実行しない。bindN/bindNode/bindListRefの別入口は変更しない。これらが元経路を使っても正しい。

Lの短絡到達数を監査して、全Identifierの最適化と誤記しない。PropertyAccess専用経路の変更は初回に含めない。

## 6. NとLR: 同じ呼出し経路で親判定だけを比較

Lから、`bindIdentifierNameChildRef(ref,parentKind)`を追加。nil→KindAt→Identifierならeffects+finish→return、他kindなら従来のbindNとする。nil/他kindを捨てない。

- **N:** このhelper内で `bindIdentifierEffects(ref, false)` を呼ぶ。
- **LR:** Nとの差は同じ箇所の `false` を `true` に変えるだけ。

N/LR両方で、生成walkerのQualifiedName.Rightだけをこのhelperへ接続する。他のchild/listは元のemitを維持。

生成器では`emitBinderChildFromAccessor`が見るnode/memberを確認し、QualifiedNameのRightにだけ一致する明示predicateを作る。全kindを共有するschema nodeなら各kindで述語が成立することを確認する。数値slotの全般的な書換えやDeclarationName型全般の分類をしない。

生成:

```sh
node --experimental-strip-types tools/scripts/tsc/generate-go-ast.ts
```

同じコマンドを2回実行し、2回目の全生成物が不変であることをhash/diffで確認する。予期しない生成物変更は自分の変更と既存差分を分けて調べる。無関係ファイルをrestoreして隠さない。

NとLRはgenerator/generated sourceが一致し、binder.goのknownName literalだけ異なることを検証する。これでN対Lは呼出し経路変更の費用、LR対Nは親判定を省く効果となる。最終的な採否比較はLR対B0。

## 7. 意味検証を具体化する

各variantに同じfixture・比較ロジックをGo overlayで適用する。B0/Sには新helperが無いので共通テストから直接呼ばない。test専用adapterでB0/Sは元のbindKind、LはbindChildRef、N/LRは対象helperへ入る形にして比較する。adapterは入口を選ぶだけで、期待する副作用を代わりに実行してはいけない。新helper単体テストは当該helperを持つvariantだけでコンパイルする。テストのために本体へruntime toggleを追加しない。

### 7a. 末尾処理の検証

初期seenParseError true/false × 対象IdentifierのThisNodeHasError有/無の4条件で、LのfinishとB0のIdentifier bindの最終flags/seenParseErrorを照合。error有時にはaggregate flagが立ち、error無時に既存seenParseErrorを消さないこと。必要なStore/SourceFile準備は既存binder testの方法を再利用する。

### 7b. 名前位置の検証

QualifiedNameを含む型参照（例 `let x: NS.Member;`）、ネストしたQualifiedName、予約語とunicode escapeを含む右側をfixture化。監査用テストでは、生成helperに入るIdentifierで旧`isIdentifierNameRef(ref)`がtrueであることをassertする。監査hookはoverlayまたはtest専用build tagに隔離し、性能binaryに入れない。

helper直接テストでnil/Identifier以外も元処理と一致させる。VariableDeclaration/Parameter/BindingElementの名前、PropertyAccess.Nameなど未対応位置では旧判定が残ることを確認する。

### 7c. 全体比較

正常TS、宣言ファイル、JS、JSX、ambient/JSDoc、await/yieldとstrict mode、#constructor、this/super、構文エラーのfixtureを使う。既存の20入力監査を優先して再利用。`tools/scripts/tsc/binder_audit_test.go.tmpl`も確認する。

B0/S/L/N/LRで同一入力をparseし、別processでcanonical結果を出力してdiffする。診断コード/位置/順序、node flags、symbol宣言・意味情報、Flow/CFG、意味のある訪問順を比較。pointerアドレスやprocess依存IDをdigestに混ぜず、訪問順の安定IDに正規化する。基準B0を候補に合わせて更新しない。

各variantの通常テスト（repo rootから）:

```sh
go -C tsc test -overlay /ABS/variant/overlay.json ./internal/ast ./internal/parser ./internal/binder ./internal/compiler
```

/ABSはdriverが確定した実パスに置き換える。未実行コマンドを成功と書かない。基準にも失敗がある場合はbaseline failureとvariant固有差を分ける。新規失敗は修正して同じ範囲を再検証する。

## 8. ビルド・機械語・寿命の確認

全variantを同じGo/CGO/flagsで最適化ビルド。`-N -l`、`-B`、strip、PGO追加は禁止。測定用binaryは`binderinvestigation`で作る。

```sh
go -C tsc test -overlay /ABS/variant/overlay.json -tags=binderinvestigation -c -o /ABS/variant/binder.test ./internal/binder
```

fixture manifestは実在するchecker.ts/dom.generated.d.tsを探して絶対パスとSHA256を保存。`investigationFixtures`の形式は `[{"name":"checker.ts","path":"...","sha256":"..."}, ...]`。存在しないfixtureをSkipで通さない。

BINDER_INVESTIGATION_MANIFESTを設定し、各binaryで `-test.run '^TestInvestigationBatchLifetime$' -test.v` を実行する。distinct/unbound→bound、最大10、Store登録数の復帰を確認。

`go tool objdump`でbindKind、bindChildRef、追加helper、forEachBindChildGeneratedを保存する。inlineされたhelperが単独symbolとして無い場合はcallerを見る。各variantのtext size、frame、CALL、比較分岐、spillを表にする。静的命令数を動的inst/opとして報告しない。

## 9. 測定driverの仕様（この実装依頼では実行しない）

単一のdriverでB0/S/L/N/LRの通常GC wallを測定できるようにする。固定10round、各variant新規process、各fixture10bind、順序は5条件をroundごとにrotateし後半はreverse。B0の独立A/A対照を各roundに一つ追加。process内の10bindを10独立sampleとして扱わない。

実行引数:
`-test.run '^$' -test.bench '^BenchmarkBindInvestigation$' -test.benchmem -test.benchtime=10x -test.count=1`

通常GCはBINDER_BENCH_GC_OFF=0。GC無効は別出力として1を使う。環境・fixture・Go/CGO・binary/source hashを固定し、結果と混ぜない。主比較はS/B0、L/S、N/L、LR/N、LR/B0。rawを条件別に保存し、benchstatの出力を保存する。異常終了/Benchmark行なしは成功扱いにしない。

PMUは別後続段階。現行kperfと固定batchの寿命が一致するかを監査し、未対応なら`missing`として実装担当へ引き渡す。旧BenchmarkBindHot/BindKPCを無条件で代用しない。管理者認証の迂回や他ユーザーのInstruments停止をしない。

## 10. 報告・停止条件

結果文書に必ず以下を記載:
- selected repo、candidate clones（初回pointer比較なし）、repo_root/typescript_go_git_rev/tsgolint_git_rev、dirty snapshotと全variant SHA。
- 変更ファイル、B0→S→L→N→LRの差、意味検証、生成再現性、機械語の証拠へのリンク。
- ns/op・B/op・allocs/op・inst/op・cycles/opは未測定ならmissing。旧9/11の異なるrevisionはstale。artifactが存在してrequested regexのBenchmark行がない場合はunsupported。currentはrepo_rootと両revision一致が必須で、variant hashも別に照合。
- 広いhot pathはwalk/helpers、今回の狭い対象はIdentifier入口/QualifiedName.Right。割当driverはSymbol/Arena/Flow等であり、今回の変更による割当削減を主張しない。
- 性能の採用条件は意味一致、inst/cycles改善、通常GCのbinder全体3%以上を主目標、parse+bind非退行。Delivery割合低下だけで合格しない。計測前は採否未判定。
- 次のアクションはprepared driverによる固定条件の測定。C以降を勝手に開始しない。

意味不一致は同じカード内で修正する。QualifiedName.Rightへの経路が到達しない場合は0件を隠さず記録し、対象拡大でごまかさない。source driftを検出したら凍結snapshotの結果を保ち、現行への適用差分を報告する。実装完了と性能採用を分け、遅かった候補も再現可能なartifactとして残す。
