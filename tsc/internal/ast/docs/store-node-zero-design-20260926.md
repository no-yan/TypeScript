# Store Node の零値と nil: 直接読みを維持し、nil 判定の追加費用を限定する設計

2026-09-26。対象は [Store checker 設計](store-checker-design-20260923.md) §4 の `Node{}` の未決事項。**設計と限定した試作の結果であり、本番実装は変更していない。** 7d-0 へ取り込む推奨案を Z1 と呼ぶ。FR1、D2、E1、F1 を維持する。

## 1. 結論

**`Node{}` と Store ごとの `Node(0)` を同じ「ノードなし」として扱い、nil を消費する API だけを零値対応にする。通常の header accessor と辺の解決には零値対応の分岐を足さない。**

```go
func (n Node) IsNil() bool {
    return n.h == nil || n.h.kind == ast.KindUnknown
}
```

加えて `Node` を Go の非比較型にする。`==`、`map[Node]`、Node を含む構造体の比較などをコンパイル時に拒否し、比較は `SameNode`、保存用の key は `NodeKey` に統一する。初期化漏れを人間が監視する方式は採らない。

安全にする対象は、零値の保存・受け渡し・存在判定・nil の同一性・書き込み境界である。`Node{}.Kind()` など、存在しないノードの内容を読む操作まで有効にするものではない。Pointer の `IsIdentifier(nil)` も `node.Kind` を直接読み、nil を受け付けない。全 getter に「nil なら 0」を足す必要はない。

## 2. 利用側の形

```go
// D2 の Symbol と N1' の checker field は零値のままでよい。
var valueDeclaration store.Node
if valueDeclaration.IsNil() {
    valueDeclaration = declaration
}

// map の不在値や slice の未設定要素も同じ契約。
if n := declarations[key]; !n.IsNil() {
    use(n.Kind(), n.Flags())
}

// 異なる Store の番兵と Node{} も等しい。
if store.SameNode(oldName, newName) {
    return old
}
links.Get(node.Key())

// 書き込み先は実ノードを要求する。親の nil は許す。
factory.SetParent(child, store.Node{})
```

`Node{}` を使うために現在の file や active Store を渡さない。nil は所属 program、file、region を持たない「不在」であり、どの Store の番兵だったかは意味を持たない。

## 3. 表現と不変条件

```go
type Node struct {
    _ [0]func() // 比較不能にする。末尾に置かない。
    s *Store
    h *NodeHeader
}

// generator は同じ struct 定義を繰り返さず、Node から underlying type を導く。
// alias (=) にはしない。各 view の method と型の区別は維持する。
type Identifier Node
type BinaryExpression Node

func (s *Store) node(ref NodeRef) Node {
    return Node{s: s, h: &s.nodes[ref]}
}
```

意味上の状態は三つだけとする。

| 状態 | 表現 | 意味 |
| --- | --- | --- |
| 零値 | `s == nil && h == nil` | 所有者のない nil |
| 番兵 | `s != nil && h == &s.nodes[0]` | その Store の nil 表現 |
| 実ノード | `s != nil`、`h` はその Store の非零行 | 内容を読めるノード |

`s` だけ、または `h` だけが nil の値は不正な内部状態。field は非公開とし、resolver と Factory が正規の値だけを返す。`IsNil` に全不変条件の検査を詰め込まず、`storechecks` の構築・公開テストで検査する。

`KindUnknown == 0` は番兵専用という現行 `IsNil` の前提を維持する。`KindUnknownKeyword`、`MissingDeclaration`、`OmittedExpression` は別の kind である。非零行に `KindUnknown` を作る API 入力を許可しない。generic な token/Factory 構築の入口で拒否し、Builder の生成経路にも `storechecks` で検査を置く。現在の Builder は `NewToken(KindUnknown, ...)` を拒否していないため、これは実装時に補う条件であり、現状ですでに保証されていると扱わない。

番兵の header は作成時の零値から書き換えない。公開後の header が動かないという既存の寿命契約も維持する。今回の設計は Builder の再確保を越えて `Node` を保持できるようにする変更ではない。

### 比較不能にする理由

二種類の nil を認めると `Node{} == s.Node(0)` は構造比較では false になる。これは将来の手書きコードだけの問題ではなく、現行の [core の generic helper](../../core/core.go) に具体例がある。`FirstNonNil` と `MapNonNil` は `mapped != *new(U)`、`OrElse` は零値比較、`SameMap` は `mapped != value` を内部で使う。比較可能な Node を渡すと、前者は番兵を非 nil と扱い、後者は意味上同じ nil の表現違いを変更と扱う。呼び出し側の `==` / `!=` だけを直す E1 では、core の generic 本体にある比較を拾えない。

現 checkout の checker 配下と `ast/utilities.go` にある呼び出しは `FirstNonNil` 5、`MapNonNil` 12、`SameMap` 20（Node 以外を含む静的件数）。Node を返す具体例は `checker.go` の default declaration 探索、`services.go` の参照ノード収集、`nodebuilderimpl.go` の宣言名探索、`OrElse` による診断位置の選択である。非比較型化はこれらの変換漏れをコンパイルエラーに変える。

`_ [0]func()` を**先頭**に置けば、Node・Node を含む構造体・配列は非比較型になる。`slices.Contains([]Node, ...)` や `core.LinkStore[Node, V]` の comparable 制約もコンパイルエラーになる。修正漏れを通常のビルドで検出でき、実行時に比較のための状態を持たない。map の値、slice の要素、field への代入はそのまま可能。

Go 1.27.0 / darwin arm64 の試作でサイズ 16B、`s` の offset 0、`h` の offset 8、二つの pointer のレジスタ渡し、scalar load を確認した。ゼロサイズ field を末尾へ動かすと padding の条件が変わるので、配置も検証対象とする。既存 view は `{s,h}` の struct なので、Node だけを変えると型変換が壊れる。generator と全 view の変更を同じ実装単位にする。

`any` に箱詰めした値の比較など、Go の静的型が Node を隠す操作までコンパイル時に防げるわけではない。変換器は Node を箱詰めしてこの制約を回避しない。

コンパイルで落とすだけでなく、修復も変換規則にする。Node を比較する `FirstNonNil` / `MapNonNil` / `OrElse` は `IsNil` を使う Node 版、`SameMap` / `SameMapIndex` / `Deduplicate` / `AppendIfUnique` は `SameNode` を使う Node 版へ付け替える。型引数を調べ、Node 以外の比較は変えない。評価順、callback 回数、早期終了、要素順、slice の再利用契約を保つ。

また、`NonExistentPropertyKey.propNode`（`collections.Set` の要素）と `accessibleChainCacheKey.location`（map key）は Node の同一性だけを保存し、field から Node として読み戻していない。こうした key 専用 struct の Node field は `NodeKey`、構築・代入は `node.Key()` に変換する。`go/types` で map / Set の key 型と field の使用箇所を対応づける。Node として読み戻す用途があれば機械的に field を置換せず、key と値を分けるなど別規則が必要な箇所として報告する。詳細は [実装指示書 §5.1](store-checker-7d0-implementation-instructions-20260924.md)。

## 4. API の契約

| 操作 | 零値・各 Store の番兵 | 実ノード |
| --- | --- | --- |
| `IsNil()` | true | false |
| `SameNode(a,b)` | nil 同士は true | 正規 header の同一性 |
| `Key()` | `NodeKey{}` | `{StoreID, NodeRef}` |
| `File()` | nil。Store を読む前に判定 | F1 の規則 |
| `NodeIsMissing`、`FindAncestor` など Pointer が nil を許す helper | Pointer と同じ nil 分岐 | 既存の処理 |
| `JSDoc` / `EagerJSDoc` | nil を返し cache を作らない | J-d の source host 制約 |
| `Kind` / `Flags` / `Pos` / typed field / `Parent` | 公開契約上は対象外 | 従来の直接読み |
| `Ref()` | source 内の番兵は従来どおり 0。零値は対象外 | 所属 Store 内の番号 |
| writer の宛先 | 通常 build でも拒否 | region / seal を検査して書く |
| optional な子・親の入力 | local edge 0 にする | 所有者検査後に local / foreign 化 |

`JSDoc` の nil 受理は本設計で明記する Store API の契約である。Pointer の `Node.JSDoc` 自体が nil-safe だという意味ではない。既存動作設計の nil host の扱いを残し、J-d の host 所属検査より先に処理する。

scalar accessor が番兵の値を読めることは、通常コードで nil に accessor を呼んでよい根拠にしない。零値と番兵は内容読みの定義域外であり、そのときの結果の一致は約束しない。`Ref()` は既存の unsafe pointer 差分を使う raw API なので、零値での例外の形も公開契約にしない。nil-safe な key が必要な場所では `Key()` を使う。

```go
func SameNode(a, b Node) bool {
    // 同じ実ノードの比較は header 読みなしで終わる。
    return a.h == b.h || (a.IsNil() && b.IsNil())
}

func (n Node) Key() NodeKey {
    if n.IsNil() {
        return NodeKey{}
    }
    return NodeKey{store: n.s.id, id: n.Ref()}
}
```

一つの生きた header address は一つの Store にだけ属する。FR1 は foreign entry のアドレスではなく正規の `Node` を返す。この不変条件により、同一実ノードの fast path で `s` をもう一度比べる必要はない。`NodeKey` の有効範囲は引き続き一つの program 内であり、別 program の key の比較を今回の変更で安全化するものではない。

### writer と foreign の入口

```go
func encodeOptionalEdge(dst *Store, value Node) NodeEdge {
    if value.IsNil() {
        return 0 // owner を読まず、foreign 表にも nil を登録しない
    }
    // 同じ program の source/JSDoc、または同じ region かを検査する。
    // 同じ Store は value.Ref()、別 Store は正規 Node を foreign 表へ登録する。
    // 実装は 7d-0 の Factory 側。
}
```

nil 入力には所属の意味がないため、別 program の番兵も edge 0 として扱う。非 nil の入力だけに不変条件 26 の所属検査を適用する。writer の宛先については逆で、`IsNil` に該当したら owner を読む前に必ず拒否する。

Factory の setter だけでなく、現存する `Node.AddFlags` / `ClearFlags` / `SetFlow` / `SetSymbol`、生成 setter も row 0 を書けないようにする。Builder の `adopt(0, parent)` のように optional child を受ける内部経路は、従来どおり何も書かず終了してよい。「row 0 を書かない」と「nil の入力を一切受けない」を混同しない。

この検査は書き込み・構築の境界へ置き、`Kind`、`Parent`、`node()` の読みへ移さない。追加 writer 検査の性能は今回の IsNil-only overlay では測っていない。

## 5. 比較した設計

| 案 | 読みの費用 | 零値の安全性・移行 | 判断 |
| --- | --- | --- | --- |
| Z1: 番兵を残し nil 消費 API だけ対応 | scalar と resolver は不変。任意 Node の nil 判定に分岐 | D2/N1' を維持。比較は型で禁止 | 推奨 |
| `node(0)` が常に `Node{}` を返す | nil 判定は pointer だけ。全 resolver に 0 判定 | 全 nil が一形態。既存の番兵読みとの互換監査が広い | 今回は採らない |
| nullable `NodeSlot{store,ref}` と解決済み Node を分ける | field を読むたびに Resolve が必要 | struct の零値は自然だが保存形式・変換器・D2 を変更。16B のままで縮まらない | 今回の問題には過大 |
| 全 getter で nil なら 0 / nil を返す | 最頻の scalar 読みにも判定 | 不正な内容読みを黙って通す。Pointer 契約より広い | 採らない |
| 全 field を番兵で初期化 | 既存の命令列 | map の不在値、slice の零要素、新 field を構造的に防げない | 採らない |

canonical nil の方が速い場面もあり得る。今回の判断は nil 判定だけの短さではなく、全ての辺へ新しい判定を入れる範囲と移行費用を含める。synthetic な親探索だけで checker 全体の勝敗を決めない。

## 6. 既存証拠と今回の測定

選択 repo は `/Volumes/SanDisk1TB/ghq/github.com/no-yan/TypeScript`、rev は `d8f8adf5d6dbc754d0b428835afe5e1afc56a6fc`。対象 package は統合された `tsc`。候補 clone として `oxc-project/tsgolint`、`tsgolint_1`〜`_5`、`tsgolint_original`、`tsgolint_refactor` の typescript-go も見つかったが、この設計ファイルと Store 実装のある選択 repo だけを使用した。

| artifact / 要求した測定 | 状態 | 扱い |
| --- | --- | --- |
| `_store-binder-results` の過去の ns / KPC / inline / objdump | `stale` | env は古い rev と Go 1.27.1。現 checkout の性能値として再利用しない |
| 過去の bind 出力に対する `BenchmarkParentChain` | `unsupported` | artifact は存在するが該当 Benchmark 行がない |
| 作業開始時の零値対応比較 | `missing` | 過去の raw code と命令列を読んでから小さい probe を作った |
| 今回の probe / IsNil-only overlay の bind | `current` | `manifest.json` の repo/rev、overlay、source hash を対応づけた |
| FR1 + D2 + 非比較 Node + writer 検査を統合した Check / E2E | `missing` | 未実装。今回の数値から合格を宣言しない |

provenance の `typescript_go_git_rev` はこの統合 repo の rev、`tsgolint_git_rev` は null（選択 checkout に TSGolint はない）。別 clone の rev と同一であるという意味ではない。生データは [_store-node-zero-results-20260926/](./_store-node-zero-results-20260926/)。

### 6.1 命令列

Go 1.27.0 / darwin arm64 / Apple M1。32B header と16B Node の独立 probe、および実 `store.go` の `IsNil` だけを変更する overlay を使用した。下表は objdump の実行経路を比較したもので、KPC による動的命令数ではない。wrapper の prologue / epilogue は除く。

| 経路 | 旧 Store と Z1 の差（Pointer 比ではない） |
| --- | --- |
| `Kind()` | どちらも `MOVH` 1 回。nil guard なし |
| `node(ref)` / 通常の `Parent()` | 変更なし |
| 引数で受けた Node の `IsNil` を bool で返す | 非零 h の経路で `CBZ` と `JMP` が増える |
| `if !n.IsNil() { return n.Kind() }` | h の `CBZ` が1命令増える |
| local `node(ref)` → `IsNil` → `Kind` | 追加の h 判定が消え、正規化した命令列は同じ |
| parent chain のループ | 各反復に `CBZ` が1命令残る。ゼロ費用ではない |
| canonical nil の `node(ref)` | 非零 ref の解決にも0判定が1命令増える |
| marker 付き Node の scalar read | 同じ2レジスタ渡し、`MOVH` 1回 |

inline cost は `IsNil` 6 → 11、`Kind` は4のまま。実 binder の `bindChildren` は静的命令行数285で不変、`bind` は843 → 844、`bindContainer` は893 → 895。これは関数の静的なコード量であり、各命令の実行回数・総 inst の増減を表さない。

Pointer を基準にした親ループの終了判定は別に扱う。単純な Pointer の nil 分岐は `CBZ` 1命令、旧 Store は kind の load と零比較分岐、Z1 はさらに h の `CBZ` が加わる。したがって Pointer 比の概算は旧 Store で+1、Z1で+2命令/反復（h 検査が消える形なら+1）。これは同じ最終 binary の動的 KPC 比較ではない。隣接する Kind 読みとの共有などで命令列が変わるので、7d 設計 §2.7 には未計数の反復数に対する +1〜2 として入れ、既存の辺解決の費用と二重計上しない。

`int32(n.h.kind) == 0` へ拡張して比較する小試作も残した。bool を返す形では `UBFX` が消えたが、Kind を続けて返す形では別の `MOVD` が入り命令数が減らなかった。効果が呼び出し形に依存するため、Z1 の必須実装には含めない。

### 6.2 時間測定の限界と採用判断

**現時点の性能に関する根拠は §6.1 の静的な命令列の差だけである。時間の改善・退行の大きさや、Check ゲートの合格は主張しない。** 今回 KPC は取り直していない。7d-0 で GC オフの KPC Session による inst / cycles を取り、後段の Check では同じ binary の Pointer を基準に cycles ≤ 1.10 を検証する。旧 Store との比較は変更の費用を切り分ける補助であり、Pointer 比のゲートを代替しない。

探索的に取得した ns/op、B/op、allocs/op、benchstat は artifact に保存するが、本文の時間表は採用判断に使えないため削除した。bind は通常 GC、load average 3.3〜3.5 で、IsNil のみの変更にもかかわらず checker.ts の B/op 中央値が約0.8MB動いた。pool / GC / 反復数の影響を分離できておらず、時間の −1.79% をこの変更の効果に帰属できない。親64段の probe も load 4.94、dual のばらつき±32%で、+10.84%を再現可能な退行幅として扱えない。いずれもノイズを排除できない探索記録であり、性能上の採用根拠にはしない。生データは `bind-*.txt`、`benchstat-bind.txt`、`probe/bench*.txt`、集計は `medians.json`。親 probe は各 variant が0 B/op・0 allocs/opだったが、これは Check 全体の allocation を示さない。

7d-0 では KPC と同じ source / compiler の inline 診断も保存し、生成 accessor と Builder / Factory constructor の can / cannot inline 件数・cost・変化した関数名を before / after で比較する。`IsNil` の cost 6 → 11 が caller の予算境界を越えないか、実際の呼び出し箇所の inline と objdump でも確認する。独立 probe で inline されたことだけでは十分でない。

最も広い性能評価対象は Check 全体だが、この変更で直接検証できる狭い経路は nil 判定と親探索である。[過去の動的頻度資料](accessor-dynamic-frequency-20260921.md)では親の辺は約49回/nodeであり、親探索に残る分岐を無視しない。この値は現 checkout で再計数した結果ではない。nil 判定の総動的回数も今回計数していない。allocation driver として調べる対象は既存の Symbol、宣言、flow / locals、scratch の構築であるが、今回 allocation profile は採っておらず寄与率を断定しない。設計上、Node ごとの新しい確保や側表は加えない。

親探索が実入力でも重要な費用として残った場合は、Store 内の専用 helper で入口の `IsNil` を一度検査し、その後を「resolver の返り値は h 非 nil」という不変条件で走査する余地がある。これは foreign resolver も零値を返さないことと、番兵を越えて走査しないことを検証してから採る局所最適化である。一般の checker コードへ unchecked API を広げず、今回の未測定の高速化として成果に数えない。

具体的には、親だけを列挙する Store 内部 helper は次の形にできる（本番未実装、命令列・時間は未測定）。

```go
func ancestors(node Node, visit func(Node)) {
    if node.IsNil() {
        return
    }
    for {
        edge := node.h.parent
        if edge == 0 {
            return
        }
        node = node.s.node(edge)
        visit(node)
    }
}
```

入口で零値と番兵を除き、ループ内は親の辺0を終了条件にする。これにより各反復の `h == nil` と `kind == KindUnknown` はソース上不要になる。前提は、非零の辺が必ず実ノードへ解決されること（foreign 表に nil を登録しない）、header の寿命が有効なこと、親関係が木の契約を守ること。callback もこの契約を壊さない。固定の Store で全ての辺を解くと foreign の親へ移った後に誤るため、毎回現在の `node.s` を使う。これは終了検査を除く方式ではなく、終了検査を親の辺へ移す方式である。

過去の頻度では Parent は Bind の0.62回/nodeに対し Check は49.44回/nodeだった。従って Bind の時間だけで Check の親探索の影響を判断しない。ただし Parent の総回数は、この形式の ancestor loop の反復回数ではない。専用 helper の優先度は実際の呼び出し箇所と Check の profile で決める。

## 7. 実装単位と受け入れ条件

最初の単位は Z1 の `IsNil` と nil-safe な `SameNode` / `Key` / `File`、nil の辺エンコードである。次に Node の非比較型化と view の再生成、既存の直接比較と generic comparable 呼び出しの置換をまとめて行う。最後に writer / Builder の番兵保護を加え、7d-0 の等価・性能検証へ進む。

今回済ませたのは、独立 probe の nil 同一性・layout・foreign 解決、実 IsNil overlay での零値 field / map 不在値 / `NodeIsMissing`、既存の限定した Store / bind テスト、非比較 marker の直接比較 / map / composite key / generic 制約の compile-fail 確認である。7d-0 全体の正しさの検証には代えない。

実装時に必須とするもの:

1. `Node{}`、二つの Store の番兵、各 Store の実ノードで IsNil / SameNode の全組合せを検査する。同じ foreign ノードの経路違いも同一とする。
2. 未設定の `Symbol.ValueDeclaration`、checker field、`make([]Node, n)`、map 不在値を初期化なしで扱える。`Key` は全 nil を0、`File` は全 nil をnilにする。
3. optional child/parent に nil を渡しても foreign entry を増やさず0へ戻る。nil を宛先にした writer は通常 build でも拒否し、番兵の全 field が不変である。
4. 非零行の `KindUnknown` を公開しない。typed view の誤用は storechecks で検出する。合法な Pointer の nil helper 呼び出しを panic に変えない。
5. 非比較 marker、全 view の相互変換、サイズ・offset・ABI を検査する。直接比較・Node を含む map key・generic comparable の compile-fail を持つ。比較不能を interface 化で回避しない。
6. parse / bind の既存等価を両 tag で通す。source / synth / JSDoc と容量1〜3を混ぜた操作列は、nil の表現によらず同じ結果になる。
7. 最終形の scalar / resolver / nil loop の命令列と inline を取り直す。生成 accessor と Builder / Factory ctor の inline 件数・cost・変化した関数と実際の call site を before / after で確認する。FR1 の foreign 経路では、local-only probe と同じ消去が起きるとは仮定しない。
8. 7d-0 の Walk / Parse / Bind、後段の Check は GC オフの KPC inst / cycles を raw + benchstat で比べる。Pointer 比を採否の基準とし、旧 Store 比は費用の切り分けに使う。Check cycles ≤ 1.10 × Pointer の既存ゲートを維持する。固定の最小改善率は設けず、追加分岐・writer・allocation・保守費用を評価し、E2E 時間の未解決差を明記する。
9. comparable helper の Node 版への変換を検査する。零値、同一 Store と別 Store の番兵、同一実ノード、異なる実ノードを混ぜ、nil の除外・fallback・同一性・callback 回数・slice 再利用が Pointer の意味と一致する。Node 以外の型引数を誤って変換しない。
10. 二つの既存複合 key を回帰例とし、Node field → NodeKey、全構築・代入 → Key() を検査する。nil の表現によらず同じ key、異なる実ノードは異なる key となること。field を Node として読み戻す入力は未対応として報告し、黙って型を変えない。

## 8. 合成判断と次の作業

独立した二案を比較し、A（dual nil）を基盤にした。B（canonical nil + NodeSlot）から optional/required の境界を明示する考え方を取り込んだが、NodeSlot と全 resolver の変更は採らなかった。別の審査でも A を推奨。A の nil-first SameNode は既存 E1 の header-equality-first に直し、File / JSDoc / nil helper の例外、Unknown 番兵の前提、raw writer の保護を補った。view は marker の定義を反復せず `type View Node` にして表現を一箇所から導く。

次はこの契約を 7d-0 に実装し、上の受け入れ条件を確認する。今回の結果は「全ホットパスの命令増加ゼロ」ではなく、**直接読みの命令列を維持し、追加検査を nil を扱う経路に限定する**という設計の根拠である。
