# Builder への Store 埋め込み (S1) 実装指示書

作成日: 2026-09-22。対象は実装を担当するエージェント。前段は Store parser (TODO 7b、[store-parser-verification-report-20260922.md](store-parser-verification-report-20260922.md))。設計の根拠は [store-ast-design-20260922.md](store-ast-design-20260922.md) §2.7 (構築)。

設計判断はこの文書で済ませてある。書かれていない判断が必要になったら、実装を進めずに報告する。

## 0. 直すもの

`store.Builder.View` が、呼ばれるたびに `Store` 構造体を丸ごと組み立て直している:

```go
func (b *Builder) View(id NodeRef) Node {
	b.view = Store{nodes: b.nodes, extra: b.extra, src: b.src, texts: b.texts.String(), file: b.file}
	return b.view.node(id)
}
```

checker.ts の parse 1 回 (node 298,884・Scan 362,421) で View は 338,615 回呼ばれる。この代入 1 回の単価は 6.1ns で、`b.nodes[id].kind` を直接読む 0.40ns の 15 倍。objdump で見た中身は、88B の stack temp のゼロ化、フィールド転記、`runtime.writeBarrier` の判定、`runtime.wbMove` 付きの typed copy、その直後の slice header 再ロードと bounds check で、約 40 命令と store→load の往復になる。合計 ≈ 2.1ms、parse 20.8ms の約 10% (pprof の flat は 16.9% でやや過大だが同じ桁)。

Pointer parser には対応物がない (`node.Kind` は pointer deref 1 つ)。これは Store が自分で作った負担なので、取り除いても比較を Store に有利に歪めない。

呼び出し元の内訳 (parse 1 回): parseAssignmentExpressionOrHigherWorker 135k、tryReparseOptionalChain 87k、checkJSSyntax 43k、parsePropertyAccessExpressionRest 25k、parseCallExpressionRest 18k。**parser 側で View の回数を減らす変更はしない** (§1)。直した後の View は 0.4ns で、回数を減らしても測れる差にならない。

## 1. ガード

1. 変更するのは `tsc/internal/ast/store/store.go` と generator `tools/scripts/tsc/generate-go-store.ts` の 2 行 (§3)、その再生成結果 `builder_generated.go` だけ。`tsc/internal/storeparser` は **1 行も変えない**。View / ViewList の signature と意味は変えない。
2. `Store` の型、`Node`、`List`、accessor、`Finish` の契約 (exact-size copy、texts の clone、scratch の再利用) を変えない。Compact を外す誘惑に乗らない (RSS のための採用済みの設計判断、Store scratch + exact Compact の実測に基づく)。
3. Builder に `Root` / `NodeCount` / `Footprint` が昇格しない形にする (§2 の理由)。
4. 実装後、7b の検証 (`storeparser` の Pointer parser との等価テスト) が **変更なしで**通ること。

## 2. 設計

Builder が持つ列 (`nodes`、`extra`、`src`、`file`) を、View が返す `Store` の列そのものにする。View は同期を一切せず `b.s.node(id)` を返すだけになる。

```go
// Builder writes by id and never holds a *NodeHeader, because append moves the
// headers. The columns under construction are s, the Store that View reads;
// nothing is copied or re-synced on a View. Builder is used only by pointer:
// Node values from View point into s.
type Builder struct {
	s       Store           // nodes, extra, src, texts, file: the columns under construction
	textBuf strings.Builder // s.texts is textBuf.String(), re-set after every write (textWords)
}
```

判断:

- **名前付きフィールド `s`、匿名埋め込みではない。** 匿名埋め込みなら生成コードの `b.extra` がそのまま通るが、`Root()`・`NodeCount()`・`Footprint()` が構築途中の Builder に昇格する (Root は「最後の行」で、途中では根ではない)。生成コードの変更は 2 行で済む (§3.2)。
- **`texts` は `textBuf` に改名。** `Store.texts string` と `strings.Builder` の同名を避ける。`strings.Builder.String()` はゼロコピーだが、書き込みで buffer が伸びると別配列になるので、`s.texts` は**書き込みのたびに**取り直す。書くのは `textWords` だけ。
- **`view` フィールドは削除。**
- `Node.s` は `&b.s` を指す。Builder は `NewBuilder` の pointer と `parserPool` でしか使われず、値コピーされない。この前提をコメントに書く (上の型コメント)。

## 3. 変更

### 3.1 `store.go`

`Builder` の全メソッドで `b.nodes` → `b.s.nodes`、`b.extra` → `b.s.extra`、`b.src` → `b.s.src`、`b.file` → `b.s.file`、`b.texts` → `b.textBuf`。個別に:

```go
func (b *Builder) Reset(src string, file uint32) {
	if cap(b.s.nodes) == 0 {
		b.s.nodes = make([]NodeHeader, 1, 1024)
		b.s.extra = make([]uint32, 3, 1024)
	} else {
		b.s.nodes = b.s.nodes[:1]
		b.s.extra = b.s.extra[:3]
		b.s.nodes[0] = NodeHeader{}
		clear(b.s.extra)
	}
	b.textBuf.Reset()
	b.s.texts = ""
	b.s.src = src
	b.s.file = file
}

// View is the Node of id in the scratch. It points into s.nodes, so it is valid
// only until the next constructor, List or NewXxx call.
func (b *Builder) View(id NodeRef) Node { return b.s.node(id) }

// ViewList is View for a list block.
func (b *Builder) ViewList(at ListRef) List { return List{&b.s, at} }
```

`textWords` の texts 側:

```go
	off = textInTexts | uint32(b.textBuf.Len())
	b.textBuf.WriteString(text)
	b.s.texts = b.textBuf.String() // the buffer may have moved
	return off, uint32(len(text))
```

`Finish` は `b.s.nodes` / `b.s.extra` / `b.s.src` / `b.s.file` を読み、`texts: strings.Clone(b.textBuf.String())` のまま。Mark / Truncate / Len / AddFlags / SetFlags / SetLoc / List / header / identifierData / adopt / adoptList / modifierFlags は列名の置き換えだけ。

`Truncate` のコメント「texts is not truncated」はそのまま正しい (`textBuf` も `s.texts` も戻さない)。

### 3.2 generator

`tools/scripts/tsc/generate-go-store.ts` の constructor 出力 2 行:

```
w.write("\tdata := uint32(len(b.s.extra))");
w.write(`\tb.s.extra = append(b.s.extra, ${words.join(", ")})`);
```

`b.header`、`b.identifierData`、`b.adopt`、`b.adoptList`、`b.modifierFlags` は Builder のメソッドのまま。再生成:

```
node tools/scripts/tsc/generate-go-store.ts
```

`builder_generated.go` の diff が上の 2 パターンの置換だけであることを `git diff --stat` と `git diff | grep '^[-+]' | grep -v 'b\.s\.extra\|b\.extra' ` で確認する (header 行以外に残りが無いこと)。

### 3.3 変えないもの

`views_generated.go`、`foreach_generated.go`、`shapes_generated.go`、`file.go`、`inspect.go`、`convert/`、`storetest/`、`storeparser/` 全部。

## 4. 検証

### 4.1 正しさ

```
go build ./tsc/... && go vet ./tsc/internal/ast/store/... ./tsc/internal/storeparser/...
go test ./tsc/internal/ast/store/... ./tsc/internal/storeparser/...
go test -tags storechecks ./tsc/internal/ast/store/... ./tsc/internal/storeparser/...
```

全部通ること。storeparser の等価テストが Pointer parser と同じ木・同じ診断を出すことが、View の意味が変わっていない証拠になる。

### 4.2 コードの形

1. View が inline されている: `go build -gcflags='-m' ./tsc/internal/ast/store 2>&1 | grep 'inline.*View'` に `can inline (*Builder).View` と `can inline (*Builder).ViewList`。これは変更前から真なので、変更の効果ではなく非退行の確認。
2. 書き込み barrier が消えている: storeparser のテストバイナリを `go test -c -o /tmp/sp.test ./tsc/internal/storeparser` で作り、`go tool objdump -s 'storeparser\.\(\*Parser\)\.tryReparseOptionalChain$' /tmp/sp.test | grep -c 'wbMove\|typedmemmove'` が **0** (変更前は 5: View の呼び出し箇所ごとに 1 つの `CALL runtime.wbMove`、同関数内で store.go:198 に帰属する命令が 200)。

### 4.3 速さ

変更前 (2026-09-22、Apple M1、`--count 5`) の基準:

```
BenchmarkStoreParseV1/checker.ts/store-8   57   20.77〜20.89 ms/op   10.93 MB/op   579 allocs/op
```

計測:

```
go test -run '^$' -bench '^BenchmarkStoreParseV1/checker.ts/' -count 10 ./tsc/internal/storeparser
```

を変更前 (未 commit なら `git stash` か HEAD のビルド、commit 後なら HEAD~) と変更後で取り、`benchstat` で比較する。pointer 側は対照 (触っていないので不変のはず)。

判定:

- 合否は 4.2 (wbMove 0) で決める。時間は benchstat で store が有意に (p < 0.05) 下がり、pointer が動かないことを確認し、幅は見積 −1.9ms (338,615 × 5.7ns、−9%) と比べて報告する。B/op と allocs/op は不変 (10.9MB / 579)。
- 有意に下がらない、または pointer 側も同じだけ動く場合は、機械のノイズなので `--count 20` で取り直す。それでも有意差が無ければ、View が inline されていないか、`b.s.node(id)` の bounds check が二重になっている疑いがあるので、4.2 の objdump で View の展開先を見て報告する。

KPC (命令数と cycles) は sudo が要るので、ユーザーが別ターミナルで取る:

```
sudo go test -tags kperf -run '^$' -bench '^BenchmarkStoreParseKPCV1/store$' -count 5 ./tsc/internal/storeparser 2>&1 | tee /tmp/kpc-after.txt
```

期待: inst と cycles の両方が下がり、IPC が 3.46 から Pointer parser の 3.90 に近づく。

### 4.4 任意: View の単価

以下を `tsc/internal/ast/store/` に一時的に置いて `ns/view` を見る (commit しない)。変更前 6.1ns。0.40ns は `b.nodes[id].kind` の素の読みの単価で、`Node{s, h}` を経由する View + accessor はそれより上 (1ns 前後) に出る。

```go
package store

import (
	"strings"
	"testing"

	"github.com/microsoft/TypeScript/tsc/internal/ast"
)

func BenchmarkViewUnit(b *testing.B) {
	const n = 300000
	bld := NewBuilder(strings.Repeat("x", 4<<20), 0)
	for i := 0; i < n; i++ {
		bld.NewToken(ast.KindIdentifier, ast.NodeFlags(i&7), int32(i), int32(i+1))
	}
	var sink int
	for b.Loop() {
		for id := NodeRef(1); id <= n; id++ {
			sink += int(bld.View(id).Kind()) + int(bld.View(id).Flags())
		}
	}
	b.ReportMetric(float64(b.Elapsed().Nanoseconds())/float64(b.N)/n/2, "ns/view")
	if sink == 0 {
		b.Fatal("sink")
	}
}
```

## 5. 報告

1. `git diff --stat` と、`builder_generated.go` の diff が §3.2 の置換だけであることの確認結果。
2. 4.1 の 3 コマンドの結果 (通ったかどうかだけでよい)。
3. 4.2 の 2 つ (inline の行、wbMove の個数)。
4. 4.3 の benchstat 出力 (pointer と store の両方、before / after)。KPC はユーザーが取るので、その依頼だけ書く。
5. 判定に届かなかった場合は、原因の仮説と objdump の抜粋。直さずに報告する。
