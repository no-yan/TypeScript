# Store accessor の高速化設計

- 日付: 2026-09-03
- 対象: `exp/handle-kind-field` (HEAD `e3604c0978` + 未コミットの `Handle.Kind` キャッシュ実験)
- 環境: go1.26.6 darwin/arm64 (Apple Silicon)
- 位置づけ: `STORE.md` の「Current code」「How to re-check the layout bet」を補う設計メモ。決定ではなく、測定つきの候補と優先順。

## 結論

外部ドキュメント(Carbon / Zig / Oxc 比較)の主張「多相 accessor の switch を hot path の外へ出し、一度だけ行う」は方針として正しい。ただしこのコードベースでは **switch は第 2 の問題** である。第 1 の問題は葉の accessor `Handle.Child` 自体が重く、kind-specific accessor に切り替えても残ることにある。

1 アクセスあたりのコストは、多相 switch、`Child` の防御コード、子 kind の先読み、の三つにほぼ均等に分かれる(下表)。外部ドキュメントは最初の一つしか扱っていない。

## 現状の計測

### アセンブリ (`go build -gcflags=-S ./internal/ast`)

| 関数 | サイズ | 所見 |
|---|---|---|
| `Handle.Child` | 288 B | inline されない。`mustLive` 2 分岐、`nodes[id]` bounds check、`childLen` に対する index 検査、`children` bounds check、子 id が 0 なら `ExternalChild`(map lookup)→ `NodeOf` 呼び出し、最後に `handleOf` で子 header の kind を再ロード |
| `Handle.IfStatementExpression` | 96 B | `CALL Handle.Child` 1 回。slot 定数は引数として渡され、load に畳み込まれない |
| `Handle.Expression` | 1328 B | 49 個の `CMPW` による比較木(jump table ではない)。全 37 case が `CALL Handle.Child` |
| `(*Store).handleOf` | 144 B | `Child` / `ListAt` / `At` に inline される。子 header への dependent load を 1 つ追加する |

`nodeHeader` は 44 バイト、`Handle` は 16 バイト、`listHeader` は 16 バイト(`unsafe.Sizeof`)。

### マイクロベンチ

64K ノード(8 kind を混在、`expression` は `DoStatement` のみ slot 1、他は slot 0)を疑似乱数順で走査し、`expression` 子を 1 つ読む。`count=3` の中央値。ベンチファイルは計測後に削除した。再現用コードは末尾。

| 方式 | 1 アクセス | 削られた要素 |
|---|---|---|
| `At(id).Expression()` 多相 switch | 13.4 ns | (基準) |
| `Child(0)` kind-specific | 10.0 ns | switch: −3.4 ns |
| 生の列読み + 子 kind 再ロード | 7.0 ns | `Child` の防御コードと call: −3.0 ns |
| 生の列読みのみ(NodeRef を返す) | 5.4 ns | 子 header への dependent load: −1.6 ns |
| `exprSlot[kind]` テーブル + 生の列読み | 5.0 ns | switch の代替。テーブルの方が僅かに速い |

ランダム順なのでメモリレイテンシ支配であり、絶対値より差分を見る。

### 使用頻度(参考)

`checker.go` での多相 accessor 呼び出し: `Name()` 224、`Expression()` 202、`Type()` 109、`Initializer()` 80。kind-specific accessor 呼び出しは 387、多相は 604。`binder.go` の `bind()` と `checker.go` の `checkExpressionWorker` は既に `switch node.Kind` の外側 dispatch を持ち、case 内で accessor を呼ぶ形になっている。

## 提案(優先順)

### 1. 生成 accessor を生の列読みに落とし、`Child` を hot path から外す

最優先。`generate-go-ast.ts` が出す `XxxField()` を次の形にする。

```go
func (h Handle) IfStatementExpression() Handle {
    n := &h.s.nodes[h.id]
    return h.s.childAt(n.childStart + slotIfStatementExpression)
}
```

- `childLen` に対する index 検査は kind が保証するので落とす(debug build の assert に移す)。
- `mustLive` は生成 accessor から外し、`At` / 構築側で一度だけ行う。
- cross-store の子は「id が 0 のときだけ map を引く」現状の設計のため、**nil の optional 子を読むたびに map lookup が走っている**。`externalChild != nil` を先に見る guard を入れるか、`NodeRef` の上位 bit を external 印にして nil と区別する。
- 目標は kind-specific accessor が 1〜2 load で inline されること。

### 2. 多相 accessor は switch ではなく kind → slot テーブルにする

`buildPolymorphicIndex` が持つ情報から `[KindCount]uint8` を機械生成する。該当 kind に field がなければ番兵値(`0xFF`)で `Handle{}` を返す。setter も同じテーブルで済む。

```go
var exprSlot = [KindCount]uint8{ /* 0xFF 初期化、該当 kind だけ slot */ }

func (h Handle) Expression() Handle {
    slot := exprSlot[h.Kind]
    if slot == 0xFF { return Handle{} }
    return h.s.childAt(h.s.nodes[h.id].childStart + uint32(slot))
}
```

外部ドキュメントは「dependent load chain が増え、Apple Silicon では switch に負けうる」と推測しているが、上の計測では逆でテーブルの方が僅かに速く、分岐予測の状態にも依存しない。

### 3. schema 生成時に hot な field を slot 0 に寄せる

`store_schema_generated.go` を見ると `expression` は大半の kind で slot 0 だが、`DoStatement`、`ExportAssignment`、`YieldExpression`、`TypeAssertion`、`JsxExpression`、JSDoc 系が slot 1〜2 に散っている。generator で「同名 field は同 slot」を制約にすれば hot 群はテーブルすら不要で `child0` 一発になり、long tail だけ 2 のテーブルを使う二段構えにできる。

pointer AST の field 順序互換は捨てることになるが、parser は `SetChild(slot, ...)` で書くので構築側は影響を受けない。

### 4. `Handle.Kind` の事前キャッシュは条件付きで見直す

このブランチの実験(`Handle{s, id, Kind}` + `handleOf`)は `Child()` / `ListAt()` ごとに子 header への dependent load を強制する。子を続けて `switch` する場所では元が取れるが、親から子へ渡すだけの経路では純損(計測で 1.6 ns/access)。

代替案: children 列を `{id uint32, kind int16}` の 8 バイト要素にして **親側に子の kind を複製する**。兄弟の kind が同じ cache line に並び、header への飛びが消える。Oxc の AST node ID RFC にある「共通 field を同 offset に置く」に近い。`SetChild` 時に kind を書き、`SetKind` 相当の再書き込みがある経路(現状は無いはず)は要確認。これは実装して E2E で測る価値がある。

### 5. `nodeHeader` を 44 バイトから 32 バイトへ縮める

`childLen` と `listLen` は kind から決まるので schema テーブルへ追い出せる。`listStart` は `childStart` に隣接配置で統合できる。`STORE.md` が column-per-field SoA を棄却した判断は維持し、packed header を保ったまま細くする。

## 外部ドキュメントへの反論

- **Carbon 型の typed ref(`BinaryRef` など)は速度には効かない。** `bind()` も `checkExpressionWorker` も既に外側 dispatch 済みで、case 内では kind-specific accessor を呼んでいる。typed ref が買うのは型安全であって命令数ではなく、命令数は提案 1 で決まる。後回しでよい。
- **Zig 型の full view は限定的。** 単発 accessor が 1 load になった後では amortization の効果が薄い。`Name` / `TypeParameters` / `Parameters` / `Type` / `Body` を続けて読む signature 系宣言に限って検討すれば十分。
- **「`Handle` に kind を足しても switch は消えない」は正しいが不十分。** 足すことで load が増える(提案 4)ことを見落としている。
- **kind → slot テーブルが switch より遅いという推測は、この測定では成立しない**(提案 2)。

## 実験計画

順番は 1 → 2 と 3 の併用 → 4 → 5。各段で次を測る。

```bash
cd tsc
go test ./internal/ast -run '^$' -bench 'E2EWalk|AdvWalkStore' -count 5
go test ./internal/binder -run '^$' -bench . -count 5   # 存在すれば
```

判定は `benchstat` で行い、E2E で有意差が出ない段は取り込まない。提案 1 は generator の変更で完結し、公開 API(`Handle.XxxField()`)は変えない。

### 提案 1 の実装結果 (2026-09-03、attempt 015)

`childAt` / `childAtSlow` を追加し、生成 getter を `return h.childAt(slot)` に変更（291 件）。`Child` は bounds 付きで `childAt` に委譲。

同一プロセス A/B（`BenchmarkAccIfExpr` vs `BenchmarkAccIfExprChild`）: med **−29%**。accessor 本体への列読み展開は inline budget 超過で逆に悪化したため不採用。詳細は `tsc/.audit/attempts/015-childAt.md`。

## 再現用マイクロベンチ

`internal/ast` に package `ast` の `_test.go` として置き、計測後に削除する。

```go
package ast

import (
	"testing"

	"github.com/microsoft/TypeScript/tsc/internal/core"
)

var accKinds = []Kind{KindIfStatement, KindExpressionStatement, KindParenthesizedExpression, KindReturnStatement, KindAwaitExpression, KindDoStatement, KindCallExpression, KindWhileStatement}

func buildAccStore(n int) (*Store, []NodeRef) {
	s := NewStore(n * 2)
	refs := make([]NodeRef, 0, n)
	loc := core.NewTextRange(0, 1)
	for i := 0; i < n; i++ {
		leaf := s.Alloc(KindIdentifier, 0, loc, 0)
		k := accKinds[i%len(accKinds)]
		h := s.Alloc(k, 0, loc, 3)
		slot := 0
		if k == KindDoStatement {
			slot = slotDoStatementExpression
		}
		h.SetChild(slot, leaf)
		refs = append(refs, h.id)
	}
	order := make([]NodeRef, n)
	x := uint32(12345)
	for i := range order {
		x = x*1664525 + 1013904223
		order[i] = refs[int(x>>8)%n]
	}
	return s, order
}

const accN = 1 << 16

func BenchmarkAccPolyExpression(b *testing.B) {
	s, order := buildAccStore(accN)
	var sink NodeRef
	for b.Loop() {
		for _, id := range order {
			sink += s.At(id).Expression().id
		}
	}
	_ = sink
}

func BenchmarkAccKindSpecificChild(b *testing.B) {
	s, order := buildAccStore(accN)
	var sink NodeRef
	for b.Loop() {
		for _, id := range order {
			h := s.At(id)
			if h.Kind == KindDoStatement {
				sink += h.DoStatementExpression().id
			} else {
				sink += h.Child(0).id
			}
		}
	}
	_ = sink
}

func BenchmarkAccRawChildWithKind(b *testing.B) {
	s, order := buildAccStore(accN)
	var sink NodeRef
	for b.Loop() {
		for _, id := range order {
			n := &s.nodes[id]
			c := s.children[n.childStart]
			if n.kind == KindDoStatement {
				c = s.children[n.childStart+1]
			}
			sink += c + NodeRef(s.nodes[c].kind)
		}
	}
	_ = sink
}

func BenchmarkAccRawChildRefOnly(b *testing.B) {
	s, order := buildAccStore(accN)
	var sink NodeRef
	for b.Loop() {
		for _, id := range order {
			n := &s.nodes[id]
			c := s.children[n.childStart]
			if n.kind == KindDoStatement {
				c = s.children[n.childStart+1]
			}
			sink += c
		}
	}
	_ = sink
}

var accExprSlot [KindCount]uint8

func init() { accExprSlot[KindDoStatement] = uint8(slotDoStatementExpression) }

func BenchmarkAccSlotTable(b *testing.B) {
	s, order := buildAccStore(accN)
	var sink NodeRef
	for b.Loop() {
		for _, id := range order {
			n := &s.nodes[id]
			sink += s.children[n.childStart+uint32(accExprSlot[n.kind])]
		}
	}
	_ = sink
}
```

```bash
go test ./internal/ast -run '^$' -bench 'BenchmarkAcc' -benchmem -count 3
```
