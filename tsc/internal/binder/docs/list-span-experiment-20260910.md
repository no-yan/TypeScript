# list span の限定実験（2026-09-10）

## 問題・仮説

共通walk/helpersは広いhot pathであり、今回の介入はその一部であるbindListRefだけ。
各ListElemで同じownerとlist headerを解決し直している。走査開始時に必要な情報だけを
取得し、走査中に再利用すれば、永続ASTのindex表現を保って追加命令を減らせると考えた。
以前失敗したgenerated walkerのnamed-child slice hoistとは対象・機構が異なる。

## 比較条件と状態

選択repo: `/Volumes/SanDisk1TB/worktree/cursor-ast-store-tests`、revision
`32598cba146fa4dd7b6162b838630c90d865ab28`。pointer候補:
`/Volumes/SanDisk1TB/ghq/github.com/no-yan/TypeScript`、revision
`8ac035a394c79e693a3a7d74cb170448503ee894`。TSGolint revisionはnull。
identityの3項目が一致するためcurrent。今回はStore baseline対candidateだけを測り、
pointerとの新規比較はmissing。全候補checkout（flownode、store-redesign、store-nolock-exp、
lock-profile、profile、store-pr-*を含む）はartifactのworktrees.txtに保存し、対照に混ぜない。

artifact set: `.cursor/skills/verify-tsc/artifacts/20260910-binder-investigation-02/list-span-experiment/`。
overlay、dirty差、binary hash、fixture参照、実行順、raw、benchstatを保存。
currentというrevision判定とdirty variantの同一性は別管理。ユーザーの本体変更は保持。
Go 1.26.0、CGO=1、GOMAXPROCS=8、GOMEMLIMIT=off。通常wallはGOGC=100、
KPCはGOGC=offの別セッション。各2入力、10 bind × 6 rounds、順序反転。
parseと強制GCはtimer外。KPCの両binaryで新規processの自己検証を各2回実施。
過去の30 bind A/Aの精度を今回の10 bind探索へそのまま適用しない。

## 提案修正と正しさ

TryBindListSpanでlocal ownerを一度確認し、start/lengthの8 byte値を得る。
local loopは同じStoreのchildrenをindexで読む。foreign listは元のloopへ戻る。
永続pointer cacheや全nodeの展開は追加しない。保持するのはlistメタデータだけであり、
children配列の拡張後にも現在の配列を読む。利用条件は走査中のlist start/length不変。
Flags・Symbol・Flowの更新は妨げないが、構造変換へ無条件に一般化しない。
本体への採用はせず、測定用overlayに限定した。

8実入力＋9境界入力で診断・Symbol・CFG・構文・訪問順等の監査がbaselineと一致。
AST/binder/compiler単体テストも成功。追加テストでnil、空、foreign、qualified/unqualified、
範囲外panic、children配列の拡張、既存要素更新を確認した。
コンパイラ全回帰試験とpointerとの完全な意味一致は今回の結果に含めない。

## 証拠

通常GCの中央値。差の検定は保存rawに対するbenchstat。

| 入力 | ns/op baseline → candidate | wall差 / p | B/op baseline → candidate | allocs/op |
| --- | ---: | ---: | ---: | ---: |
| checker | 16,261,064.5 → 16,219,170.5 | −0.26% / .699 | 12,799,498.5 → 12,799,524 | 14,165 → 14,165 |
| dom | 5,742,694 → 5,671,556.5 | −1.24% / .310 | 7,870,383.5 → 7,870,409 | 16,684 → 16,684 |

paired mean-log-ratio bootstrap95%区間はchecker [−0.76%, +5.86%]、dom [−3.14%, +0.25%]。
中央値比とpaired推定量は異なる。checkerは非悪化すら確認できていない。

| 入力 | EL0 inst/op baseline → candidate | 差 / p | EL0 cycles/op baseline → candidate | 差 / p |
| --- | ---: | ---: | ---: | ---: |
| checker | 185,003,267 → 182,373,904.5 | −1.42% / .002 | 88,528,871.5 → 88,331,204 | −0.22% / .937 |
| dom | 77,778,473 → 75,691,115.5 | −2.68% / .002 | 32,908,251.5 → 32,042,753.5 | −2.63% / .015 |

EL0 branchは−2.33% / −4.02%。L1D missはchecker非有意、dom−2.37% (p=.026)。
fixed EL0+EL1は別列としてraw/benchstatに保存。KPCのwallを通常GC性能に代用しない。

instrumented監査ではlistOwner呼出が83,520 / 53,596回、ID確認が50,646 / 35,424回減る。
ListLen 32,874 / 18,172回とListElem 50,646 / 35,424回を置換。
TryBindListSpanのowner確認自体はlistごとに残る。これらは重なる経路の計数なので寄与を足さない。
逆アセンブルでもlocal loopからListLen/ListElem CALLが消え、header読出がloop外へ移る。
foreign fallbackには元のCALLが残る。stack frameは64→96 byteに増えた。
静的CALL削減ではなく、上記の実binder動的命令削減を効果の根拠とする。

## 診断・汎化できること

「同じ解決結果の有効期間を明示し、その期間だけ使う」は一経路で機構が成立した。
各getterの機械的inline化や大きな共通cacheとは違い、再解決する仕事自体を除ける。
ただしcheckerでは命令削減がcycles/wall改善につながっていない。局所的な再利用で
Store退行全体を解決したとは言えず、今回だけでowner・header・inlineの個別寄与も分離できない。
従来のowner fastpathとは重複するため、両者の改善率を加算できない。

allocation driverは既存調査のsymbolIdx/flows列、symbolRefs、FlowNode32→48 B、
Symbol/DeclarationsのHandle拡大が残る。このspanは永続表現を縮めず、割当削減も示していない。
新しいInstruments profile、GC CPU/assist/scan/live、parse+bind比較はmissing。
pprofは使用していない。過去のstale profileで今回の寄与を確定しない。

## 再現・採用条件・次の行動

生成は `tools/scripts/tsc/binder_list_span_experiment.py`。保存済みoverlayとbinaryで再実行する場合、
artifact内campaign.pyのenvironment・manifest・実行引数を使い、新しい出力先を指定する。
KPCは管理者権限と独立した測定セッションが必要。比較は
`benchstat baseline.txt candidate.txt` をwall-6とkpc/kpc-6でそれぞれ実行する。
保存先の再利用や既存rawの上書きはしない。

本候補はwall 3%の採用条件未達、保留。非有意を効果ゼロとは判定しない。
次は既にdomで初回wall 3.32%を得たowner＋name候補の独立確認を優先する。
spanを広げる場合は、他のlist loopで不変条件と残存解決回数を先に監査し、
既存owner fastpathとの同一セッション対照で追加価値を確認してから対象を増やす。
採用には事前対象の有意なwall ≥3%、inst/cycles低下、独立再現、8入力の非悪化区間、
全回帰、phase間コスト移動なし、通常GC全体の検証が必要。pointer同等とGC高速化は別判定。
