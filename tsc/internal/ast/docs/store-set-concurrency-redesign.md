# StoreSet の問題と再設計

状態: 初案の採用を撤回。以下の「性能要件による再審査」が優先する。実装・race 検証・性能検証は未実施。2026-09-05。

## 性能要件による再審査

必須要件は「A の Monaco 実行を B 同等以上にし、race をなくす」。初案の分割登録表は安全な lookup の候補に過ぎず、この性能要件を満たす根拠はない。採用決定を撤回し、下記に残す分割表案は比較候補として扱う。単純 no-lock が既に B より約21%遅いため、それに atomic load とページ参照を追加するだけでは目標達成の説明にならない。

### 改訂する実行時アーキテクチャ

checker が高頻度に参照するノードを、登録表経由の GlobalRef から直接参照へ戻す。比較対象 B は Symbol.Declarations が []*Node、ValueDeclaration が *Node。A は []GlobalRef / GlobalRef で、NodeSeq.All/Len、Symbol.IsStatic、checker.mergeSymbol 等が参照のたびに NodeOf を呼ぶ。ここはコード上確認した差である。ただしこれが残差21%のすべてを説明するとの証拠はない。

1. **ホットなノード参照には、所有者とノードを直接指す表現を使う。** Store 形式を残す最小の試作は DeclRef{store *Store; ref NodeRef} を ValueDeclaration と Declarations に使い、GlobalRef の unpack と global lookup をなくす。これは B の *Node と同じコストではない。A の Kind 復元も残る。候補が遅ければ、checker が見るノード表現自体を B の *Node に戻す範囲まで検討し、pointer-free を必須制約にしない。
2. **登録表を checker の内部参照から分離する。** GlobalRef はプロセス境界・汎用 API 等の identity が必要な場所に限定する。それらの解決は従来の同期を保持して安全に行い、境界で解決した参照を owner の有効期間中だけ使用する。hot path の共有 atomic 化を目標にしない。
3. **共有 parse/bind データと checker 私有データを所有権で分ける。** 共有ノードは書き込みフェーズの完了を同期してから公開。checker の synth と診断用 printer Store は checker の所有下に置き、共有 registry を介さず直接参照する。遅延作成を禁止する必要はない。checker 外へ渡す場合だけ明示的な公開と寿命移譲が必要。
4. **寿命は lookup のたびに判定せず、処理単位で保証する。** Program が共有 Store を、checker/EmitContext が私有 Store を保持する。checker と診断処理の終了を待ってから owner を Close する。外部 GlobalRef の解決無効化と、既に取得した直接参照の有効期間を区別する。閉じた Store をプールで再利用して古い参照から別ノードが見える状態は禁止。
5. **削除に対する意味の差を移行条件にする。** 現在 NodeSeq の GlobalRef 解決は Unregister 後の nil を飛ばす。直接参照ではこの自動失効はない。内部の列挙が生きた owner の期間に限られることを全利用箇所で確認し、外部 API が必要とする失効は同期された境界に残す。単なるキャッシュ置換として実装しない。

### 速さと安全性の両方を確定する実装手順

- まず新たな concurrent registry を作らず、現行のロックあり経路を安全性の対照にする。
- ValueDeclaration/Declarations の直接参照を最小の実ワークロード試作とし、共有データの初期化後は不変、checker の変更は私有 Symbol で行う既存契約を検査する。ノード参照解決・Kind 復元・GC・総割り当てを測る。
- この試作で残差が減らなければ、直接参照対象を無差別に広げない。B との差のある memory/GC/Store allocation を次に帰属する。単独プロファイルの比率だけで21%を説明しない。
- *Store を含む DeclRef は通常16 bytes（B の *Node は8 bytes）になり、宣言リストがGC走査対象になる。Kind 込み Handle の大量保存、ノードごとの追加 wrapper 確保、packed AST と B AST の二重保持を無条件に導入しない。ポインタ削減による利得より lookup の損失が大きいなら、B の表現へ戻す判断をする。
- 最終的に残る汎用 registry は、ホットでなければ既存 RWMutex のままでよい。登録表のページ化は、そこが依然支配的だと実測した場合にだけ再検討する。

### 必須の合格条件

- 元の race 再現（並列 NewChecker、診断用 NewEmitContext）、並列登録/参照/Close、機能テストが race 検出なし。公開順序と Store 内部の書き込み契約もレビューする。
- B と同じ診断・ファイル・型等の結果。noEmit だけの特殊な正しさに依存しない。
- **最終受入れは B 同等以上。ロックあり A からの改善だけでは合格しない。** race 無効、同じ Go/対象/引数/環境で交互・無作為順の反復測定を行い、wall と Check を別々に評価する。
- benchstat の「有意差なし」は同等性の証明ではない。中央値が B 以下であることに加え、差の区間推定が実用上許容する劣化幅内に収まるかを確認する。許容幅を決めていない段階で、遅い結果を「同等」と呼ばない。ばらつきが大きければ測定条件を改善し、判定保留とする。
- ns/op、TotalAlloc による B/op、Mallocs による allocs/op と retained heap を保存する。GC/メモリ増加でwallが悪化した候補は棄却する。

現状はこの要件を満たす実装も測定結果もない。以下の旧案の atomic lookup を「高速」「B同等」と呼ばない。B の現行表現は性能比較の基準だが、B 自体の race 検証結果も未取得なので無条件に race-free とは認定しない。

## 問題

StoreSet はプロセス共通の GlobalRef → Store 登録表である。現在の stores は可変 slice、Store は各参照で RWMutex の読み取りロックを取る。多数の checker が NodeOf を呼ぶと同じロックのカウンタを更新し、CPU/cache-line コストを生む。一方、読み取りロックだけを除去すると Add の append と Store の slice header 読み取りが競合する。事前に容量を確保しても header 更新は残る。

書き込みは parse/bind に限定されない。

1. checkerPool.createCheckers が NewChecker を並列に呼ぶ。各 checker が synth Store を登録する間に別 checker は initializeChecker → mergeSymbol → NodeOf を実行する。
2. symbolToString → getNodeBuilderEx → NewEmitContext が診断用 Store を遅延登録する。--noEmit でも通る。
3. Close → UnregisterStore → Remove は既存スロットを nil にする。adopt も既存スロットと表長を書き換える。

追加の公開順序問題: Add は s.id を CAS してから表に append する。RegisterStore は ID != 0 で早期 return するため、別 goroutine に「ID はあるが表への登録は未完了」という状態を見せ得る。現在の lookup のロックは Add 完了を待つが、無ロック lookup への変更ではこの保護も失われる。

既存の並列参照テストは登録・ノード作成後に参照を開始しており、登録と読み取りの重なりを十分に検証していない。Store 登録表の安全性と、Store 内の AST 配列の所有権は別の契約である。

## 証拠と対象

選択 repo: /Volumes/SanDisk1TB/worktree/cursor-ast-store-tests。
typescript_go_git_rev: 8067b4b419b2420236e396b4892f66c97141904b。
tsgolint_git_rev: 該当なし。候補 checkout: 上記、/Volumes/SanDisk1TB/worktree/lock-profile（同じ HEAD）、B /Volumes/SanDisk1TB/ghq/github.com/no-yan/TypeScript（8ac035a394c79e693a3a7d74cb170448503ee894）。

artifact set: /private/tmp/lock-monaco-20260905。メタデータの3項目に対応する旧実験はその記録内で current。現在の実行ファイルとの同一性は再確認していないため、現時点のバイナリの測定値としては stale。ユーザーの race ログは /Users/noyan/.codex/attachments/2267a958-a329-47ce-b611-c3479c3cb3f0/pasted-text.txt。ビルド SHA がないため current なビルド対応付けはできないが、3件の race と上記2つの登録経路を示す。コードの経路も確認した。新設計の性能・race artifact は missing。

旧10回反復測定の中央値（1 op = Monaco コンパイラ1プロセス）:

| variant | ns/op（外側の経過時間） | allocs/op（診断時点の Mallocs） |
|---|---:|---:|
| A HEAD ロックあり | 6,824,968,958 | 11,565,960 |
| A HEAD 単純 no-lock | 4,754,311,417 | 11,565,235 |
| 指定 B | 3,922,912,000 | 11,295,503 |

B/op は missing。生存 heap を B/op と見なさない。benchstat でロック除去は wall -30.34%、Check -45.39%、no-lock は B 比 wall +21.19%。単純 no-lock は安全性を欠く比較用実験で、再設計の達成保証ではない。race 付き実行の秒数を比較に混ぜない。

hot paths: ロックあり対照の StoreSet.Store 配下 RLock/RUnlock は計0.83/11.15 CPU秒。no-lock 後の広い CPU ホットパスは madvise、ゼロクリア、GC scanning。狭く介入可能な登録表のロック経路とは区別する。allocation drivers は Store.AllocSlots、NewStore、slices.Grow、checker 型生成等。設計で新たな全表コピー・GC 負担を増やさないことが重要。

## 旧案: 比較候補として残す分割登録表

writer の直列化を維持し、Store lookup だけを「不変 directory + 固定長 page + atomic slot」に置き換える。File/SetFile は当面 writer mutex で保護し、同時に最適化しない。

概念上の型:

```go
const storePageSize = 256 // 初期値。性能検証で確認する。
type storePage struct {
    slots [storePageSize]atomic.Pointer[Store]
}
type storeDirectory struct {
    pages []*storePage // 公開後は長さ・要素・backing array を変更しない
}
type StoreSet struct {
    mu sync.Mutex                         // 変更と File/liveCount 用
    directory atomic.Pointer[storeDirectory]
    highWater uint64                      // mu 内のみ。採番・範囲検証
    files []*SourceFile                  // mu 内のみ。lookup は触らない
    domain *identityDomain               // 小さい独立 token
}
```

Store には登録時だけ使う registrationMu と所属 domain token を加える。token は StoreSet 本体への参照にしない（Store が登録表全体の寿命を延ばすのを避ける）。既存 id は atomic.Uint32 を維持する。atomic を含む Store/page はコピー禁止。

Store(id) は id=0 を処理し、directory.Load()、ページ範囲確認、page.slots[offset].Load() で返す。highWater や files や可変 slice header を読まない。通常は atomic load 2回で、reader による共有カウンタの書き込みはない。atomic load のコストやページ間接参照は残るので、単純 no-lock と同速とは仮定しない。

## 成長と削除

- 初回は1ページを用意する。directory のページ容量が不足したらページ数を原則2倍にする。
- 新 directory に既存の page **ポインタ**をコピーし、追加容量分の空 page をすべて確保してから公開する。ページ本体や atomic slot はコピーしない。
- 新 directory は atomic Store で公開する。公開済み directory の pages は一切書き換えない。
- 既存 reader は旧 directory と既存 page を安全に使える。未登録 ID の検索が nil を返すことは許容する。公開済み ID を取得してから検索した reader は、ID の公開順序により必要な directory が見える。
- Remove は既存 slot.Store(nil)。ページは縮小・再利用しない。StoreID も再採番しない。
- Store を取得済みの reader は Remove と重なってもそのポインタを保持できる。Go GC が物理的な寿命を保つ。Remove は Store の内部配列を破壊しない。
- Remove 後の所有者による Store の再利用・配列解放は別問題。全利用者の終了を待つ既存フェーズ契約が必要で、lookup の atomic 化では解決しない。

成長時だけ directory をコピーするので、通常の Add は全登録表のコピーを伴わない。directory の総コピー量は幾何成長で O(ページ数)。64 bit では256 slot/page は約2 KiB。容量倍増の空きスロットは通常最大約2倍の範囲で残り、GC の走査対象にもなる。高水位は減らないため、長寿命プロセスの登録・削除反復は必須評価対象。極端に疎な外部 ID の取り込みは認めない。

## 公開手順と API 契約

すべての操作が同じ順序でロックを取る: StoreSet.mu → Store.registrationMu。逆順の呼び出し・登録処理からのコールバックは禁止する。

Add:

1. 両 mutex を取得し、未登録・所属 domain・ID 上限を確認する。二重 Add は既存契約どおり panic。
2. 必要なページと files 容量を確保する。ID の上限を uint64 で検査し、uint32 の0への wrap を許さない。
3. highWater と所属 domain を設定し、対象 slot に Store を atomic 公開する。必要な directory の公開も完了させる。
4. **最後に s.id.Store(id)**。ID != 0 を観測した側は登録完了を観測できる。
5. ロックを解放する。

Store 内の既存データは公開前に必要な初期化を終える。登録後に作るノードは従来どおり owner が管理し、他 checker への公開には別の同期が必要。登録表の公開は将来の AST 書き込みを同期しない。

RegisterStore:

- 同じ domain での繰り返し登録は同じ ID を返す。
- 未登録の判定後は、上記と同じロック内で ID を再確認する。競合時に単純に公開 Add を再呼び出して panic させない。
- 高速な既登録経路を残す場合、atomic ID の取得後に初期化済みの immutable domain を検証する。
- Remove 後は ID を維持し、RegisterStore だけで復活させない（現行挙動を維持）。

adopt/BindFile:

- 同じ Store の同じ ID での登録確認は冪等。生きた別 Store を上書きしない。
- 異なる identity domain の ID を数値だけで輸入することを禁止する。現在の無条件 adopt は衝突を検出しない。global identity に必要なら元の domain での登録をやめ、最初から global domain で登録するよう呼び出し側を変更する。
- 同一 domain の tombstone を BindFile で復帰する現行挙動を維持するなら、Store 本体に残る ID/domain を検証して同じ Store だけを再公開する。異なる Store への ID 再利用は禁止。
- File metadata は従来どおり SetFile の完了後に利用可能。Store の公開と File の設定を一つの原子的なペアとは規定しない。

Remove/SetFile/liveCount/File:

- StoreSet.mu 下で操作する。Remove は引数の domain と対象 Store の一致も検証し、foreign Store で別スロットを消さない。
- Remove が参照を nil にしても ID/domain は不変。繰り返し Remove は無害。
- File は従来の整合性を mutex で維持。liveCount は mutex 下でスロットを数えるか、同じ mutex 下の正確なカウンタを使う。

この domain 検証は public Add の二重登録禁止と整合するが、foreign adopt に依存した呼び出しがあれば移行が必要。現行コードで BindFile の production 呼び出し元は RegisterFile であり、実装時には AST/パーサ全体のテストで確認する。

## 採らない案

- 全表を毎回コピーする snapshot: 実装は簡単だが N 件登録で O(N²) のコピーとなり、既に大きい GC 負荷を増やす。
- checker 開始前に freeze: 診断・printer の遅延登録を扱えない。
- slice の事前確保だけ: header と Remove/adopt の slot 書き込み競合が残る。
- checker ごとの永続 lookup cache: Remove 後の失効と新規登録の追従が必要。まず登録表の契約を確立する。
- GlobalRef にポインタを加える: pointer-free の表現・メモリ設計を変え、今回の問題以上に影響が広がる。

## 実装順序と受入基準

1. 非 no-lock の現行実装を安全性の対照として保持する。no-lock tag は実験専用と明示する。
2. ページ成長・lookup・追加/削除を実装し、Store ID の公開順序・domain・二重登録を一緒に直す。
3. 次の回帰テストを race 有効で実施する。
   - 複数 writer の登録中に、既存 ID と新たに同期して受け取った ID を多数 reader が参照する。
   - ページ境界・directory 成長境界を繰り返し越える。公開済み ID が nil/別 Store に化けない。
   - 同一 Store の並列 RegisterStore は同じ ID、異なる domain への Add は拒否。
   - Remove と lookup の重なりは Store または nil、Remove 完了を同期してからの lookup は nil。取得済み Store の内部を破壊しない。
   - 同一 domain の再 adopt、foreign domain 拒否、SetFile/File、二重 Remove、ID 上限。
   - 診断用 NodeBuilder の遅延登録を伴う Monaco を race 有効で実行する。
4. AST と compiler の既存テスト、診断一致を確認する。race の無報告だけで安全性の証明とせず、公開順序・全 writer 経路のレビューも行う。
5. race 無効で旧ロックあり/新設計/B を同一条件の Monaco で反復測定し、生出力を保存して benchstat で比較する。wall、Check、ns/op、B/op（TotalAlloc）、allocs/op を報告する。
6. microbenchmark は既存 Store lookup と並列追加を最小限に分離する。symbol synthetic は方向確認に限り、Monaco の代用にしない。

旧案の局所検証条件は race 回帰・機能テスト・公開契約・ロックありからの改善だったが、最終受入条件としては撤回する。冒頭の「B 同等以上」を必須とし、残差があれば未達として続ける。

次の行動はこの設計の実装と検証である。設計だけをもって no-lock バイナリを安全と扱わない。
