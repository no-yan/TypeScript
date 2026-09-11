# 生成Accessorのレビュー修正（2026-09-11）

## 問題・修正

- Binary専用処理はModifiersを使わないのに、全fieldのAccessorがlistのownerを解決していた。生成元に明示的なchild-only projectionを追加し、Binaryの専用callerを`AccessBinaryExpressionChildren`へ移した。生成walkerは全field版を保持する。
- Callのexpression/questionDot判定も`AccessCallExpressionChildren`を使う。引数を訪問するcallerは全field版のまま。
- OptionalChainは後段で必要になる構文参照も最初に取得し、整数snapshotとして後段へ渡す。後段のAccess再呼出しを除く。Flags・Flow・Symbolの値は保持しない。
- FunctionExpressionのnameRef/nameKindをstrict-mode検査へ再利用する。
- objdump driverは通常ビルド／監査ビルド・任意symbol regex・必須関数名を指定できる。binary hash不一致、モード不一致、空出力、対象関数欠落を拒否する。
- G0境界試験を追加（kind/shapeの拒否、ForIn/ForOf alias、foreign child fallback、projectionとfull accessorの一致）。

2pass、診断、bind順は変更しない。残るOptional Callのcallee再取得、親判定、D1/D2は今回の除去対象に含めない。

## 対象と証拠

選択repo: `/Volumes/SanDisk1TB/worktree/cursor-ast-store-tests`、HEAD `32598cba146fa4dd7b6162b838630c90d865ab28`、tsgolint revisionはnull。
pointer対照: `/Volumes/SanDisk1TB/ghq/github.com/no-yan/TypeScript`、HEAD `8ac035a394c79e693a3a7d74cb170448503ee894`、tsgolint revisionはnull。
候補clone一覧はafter artifactの`worktrees.txt`に保存（flownode、land-44-45、lock-design-inv、lock-profile、merged-symbols-guard、nolock97、opt-merge-symbol、profile、putcol-bce、store-nolock-exp、store-pr系、store-redesign）。今回の修正・検証には未使用。

artifact rootは選択repoの`.cursor/skills/verify-tsc/artifacts/`。
- `20260911-accessor-review-before`: 修正前source hashと通常ビルド。identityはcurrent、修正後sourceとは不一致。
- `20260911-accessor-review-after`: 修正後source hash、検証結果の保存先。
- `20260910-luna-accessor-p9-candidate`: 既存の20入力意味対照。選択identityはcurrent、修正後sourceとは不一致。
- 旧arityのwall/KPC値は現在のnamed Accessorの性能根拠に使わない。sourceの相違はidentityのstaleと区別する。

今回の修正版のns/op・B/op・allocs/op、wall/KPC、parse+bind、GCはmissing。新しいbenchstat比較はまだない。
既存profileの広いhot pathはwalk/helpersで、slot再解決は一部。allocation driverはsymbolIdx/flows列、symbolRefs、FlowNode/Symbol/Handleの大型化であり、本修正では残る。現行sourceのprofileとして扱わない。

## 再現と受け入れ条件

Goテストは`tsc`で`go test ./internal/ast ./internal/binder ./internal/compiler`。
生成Accessorの再生成一致とdriverの単体試験を確認する。
`luna_accessor_audit.py snapshot`、`build-normal`、`build`、`audit`、`compare`でsource/binary identityと20入力の意味digestを照合する。
objdumpは`--mode normal --symbols '対象関数regex' --expect 必須関数`を指定する。
Binary child-only accessorにlist owner/map解決がなく、OptionalChain後段にAccess再呼出しがないことを通常ビルドで確認する。
frame増減はビルド種別ごとに報告し、静的CALL箇所数を実行命令数と混同しない。

V3の性能完了条件は未充足。負荷の高い環境で測定回数を増やさず、既定のA/Aとchecker/dom各10 binds×6 rounds、benchstat比較、その後parse+bind/GCで評価する。

## 通常ビルドの機械語結果

`normal-codegen-comparison.json`と`builds/normal/normal-*.objdump.txt`に保存。
Binary child-only accessorではmap検索・list解決がなくなった。Call child-only accessorは判定helperにinlineされ、helperからのAccessCallExpression CALLとmap検索がなくなった。
全field版はwalker用に残るため、全field版そのもののmap検索が消えたという意味ではない。
OptionalChain後段のAccess CALLは3箇所→0箇所。静的CALL箇所数は16→9。実行命令数の測定ではない。

| 通常ビルド関数 | 修正前frame | 修正後frame |
|---|---:|---:|
| bindBinaryExpressionKind | 208 B | 144 B |
| bindOptionalChainRef | 112 B | 176 B |
| bindOptionalChainRestResolved | 64 B | 48 B |
| bindFunctionExpressionRef | 64 B | 64 B |
| bindParameterFlowRef | 112 B | 112 B |
| bindBindingElementFlowRef | 64 B | 64 B |

OptionalChainの親側frameは64 B増えた。再解決削減には保持費が伴うため、wall/KPCを測る前に性能改善とは評価しない。
旧G1のParameter 144 B / BindingElement 80 Bは監査ビルドの値であり、上表と混同しない。

## 機能検証

- `go test ./internal/ast ./internal/binder ./internal/compiler`: 成功（追加境界試験を含む）。
- objdump driverの単体試験: 4件成功。
- 生成Accessorの再生成一致: 成功。
- `git diff --check`: 成功。
- `go test ./...`は今回実行していない。

検証時に別のpointer版ビルドが並行しており、ホスト1分load averageは約25〜29だった。
本修正の実行命令数・wallをこの状態で判定しない。

20入力の監査はP9と保存baselineの両方に一致した（診断・Symbol・Flow・構文・訪問順）。
修正後の全記録対象source hashは現在checkoutに一致。afterの通常／監査build identityはcurrent。
既存監査hookではcheckerのChildRef/KindAt各4回、ID332回の減少を検出した。domの既存hook計数は同じ。
このhookは生成アクセサ内のnil-map検索を数えていないため、上記差だけでlist解決の削減量や全体命令数を推定しない。

次のアクションは、負荷が安定した状態で修正前／修正後の非監査benchmark binaryを固定し、既定A/Aと累積wall/KPCのbenchstat比較を行うこと。
この段階では「レビュー指摘の実装修正・意味検証・通常機械語確認済み／性能評価未完了」とする。

同3パッケージの`go vet`も成功。


### 2026-09-11 固定規模の測定完了

[測定結果](accessor-review-measurement-20260911.md)。レビュー修正前後でcheckerの命令−1.24%、cycles−2.09%（各p=.002、KPC A/A合格）。domはbenchstat有意差なし。bind/parse+bindのwallは非有意かつA/A不合格。GC probeはcurrentだが、Store保持とA/A変動のためGC改善は未証明。旧missing記載は測定前の履歴として保持する。pointer同一セッション比較・制御したGC評価は未完了。
