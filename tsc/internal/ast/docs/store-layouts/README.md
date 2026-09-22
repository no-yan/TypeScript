# Store AST の layout 候補: 共通前提と比較

作成日: 2026-09-20。設計ドキュメントのみ。コードは変更しない。

> 2026-09-22 追記: 主案は B1 から「24B header + 一様配置の `extra` 列」に変わった。決定事項は [../store-ast-design-20260922.md](../store-ast-design-20260922.md)。この README の分布実測と比較表は引き続き有効。

Store AST の行の形を 3 案に分けて文書化する。各案の文書は同じ章立てにしてあり、この README は 3 案に共通する前提 (実測した分布、Pointer AST の実サイズ、walk の契約) と比較表だけを持つ。

| 文書 | 案 | 一言 |
| --- | --- | --- |
| [a-children-column.md](a-children-column.md) | A: 元の Store | 24B header + 共有 `children` 列 + `lists` 列。`cursor/ast-store-tests` の実装そのもの |
| [b-fixed-arity.md](b-fixed-arity.md) | B: kind 別固定 arity | 子 slot を kind ごとの固定形に置く。B1 = 型付き slab (主案)、B2 = word-packed 可変長行 (上限測定用) |
| [c-fixed-row-extra.md](c-fixed-row-extra.md) | C: 固定長行 + extra | Zig `std.zig.Ast` 型。全ノード同じ長さの行に 2 word だけ inline し、あふれと list を `extra` 列へ逃がす |

## 1. 判定に使う計測

`tsc/internal/ast/docs/traversal-benchmark-design.md` の Walk 契約に従う。要点だけ再掲する。

- 入力は実ファイル (`checker.ts`、`dom.generated.d.ts`)。合成木は判定に使わない (cycles を約半分に過小評価する。メモリ `store-ast-walk-baseline`)。
- 走査は現行 `ForEachChild` が列挙する子の preorder DFS。計測中は訪問数の加算だけ。operator token などを片側だけ省略しない。
- Store は Pointer AST を変換して作る。parse と変換は計測外。
- 正しさは順序付き fingerprint で別 process で確認する。
- Pointer 基準値 (M1、kpc): checker.ts full walk 110 inst/visit・IPC 3.52・約 31 cycles/visit、run 間の inst 差 ≤ 0.12%。ただしこの値は checksum 付き walker のもので、契約どおりの「訪問数のみ」walker では測り直しが要る。

この walk が判定できるのは不採用だけである。元の Store でも walk 単体は問題にならず、bind の +79% inst は accessor ごとの税・side map・宣言経路の Handle 化から来ていた (メモリ `bind-childkinds-column-rejected`、`rewrite-regression-attribution`)。walk で Pointer に負ける layout は安く捨てられるが、勝った layout が bind / check で速い保証はない。

## 2. 実測した分布

2026-09-20、`store-ast` (main `f29aeb9f82`) で `ast.json` の member 定義と実ファイルの kind ヒストグラムを結合した。slot は `noFactory` を除いた child member の数 (list slot を含む)、payload は slot + 非 child の data member (string は 2 word 換算)。

| | checker.ts | dom.generated.d.ts |
| --- | --- | --- |
| ノード数 | 298,054 | 109,605 |
| 子 slot 0 の葉 | 51.4% (Identifier 41.5%) | 53.6% (Identifier 33.1%) |
| slot ≤ 2 / ≤ 4 / ≤ 6 | 70.9% / 90.4% / 98.9% | 77.1% / 79.9% / 99.8% |
| 最大 slot | 9 (MethodDeclaration) | 8 |
| 平均の宣言 slot / node | 1.47 | 1.58 |
| 宣言 slot の占有率 | 約 61% | — |
| payload 0 (header だけで済む) | 8.9% | 16.9% |
| payload > 2 | 30.5% | 26.4% |
| 非 nil の NodeList + ModifierList | 42,709 (0.143 / node) | 18,175 (0.166 / node) |
| list 要素の総数 | 76,868 (全ノードの 25.8%) | 38,343 (35.0%) |
| NodeList の長さ 1 / ≤2 / ≤4 / >16 | 59% / 85% / 96% / 0.2% | 37% / 81% / 94% / 0.8% |

頻度の上位 (checker.ts、括弧内は slot 数 / うち list slot): Identifier 41.5% (0/0)、PropertyAccessExpression 8.4% (3/0)、CallExpression 6.2% (4/2)、BinaryExpression 5.5% (5/1)、Block 3.2% (1/1)、TypeReference 2.9% (2/1)、VariableDeclaration 2.3% (4/0)。

読み取れること:

- ノードの半分は葉で、その大半が Identifier である。行の形を決めるのは header の大きさと識別子 text の置き方であり、list の表現は二次的である。
- list は作る時点で長さが確定している (parser は要素を scratch に集めてから `NewNodeList` を呼ぶ)。可変長だが成長はしないので、どの案でも「要素を連続領域へ 1 回で書く」で足りる。
- 宣言 slot の 4 割は nil である (`BinaryExpression.modifiers` / `Type`、`PropertyAccessExpression.QuestionDotToken` など)。固定 arity はこれを無駄にするが、1 node あたり約 2.3B にすぎない。

## 3. Pointer AST の実サイズ

同じ日に `reflect` で data struct の大きさを測った。`Node` header は 48B (Kind、Flags、Loc、id、Parent、`data` interface) で、各 kind の struct は `NodeDefault` 経由でこの header を先頭に埋め込む。つまり header と field は同じ確保に入っている。

| | checker.ts | dom.generated.d.ts |
| --- | --- | --- |
| struct + NodeList (32B) + 要素 slice の合計 | 25.2 MB (84.6 B/node) | 9.0 MB (82.4 B/node) |
| 代表的な struct | Identifier 72B、PropertyAccessExpression 88B、CallExpression 96B、BinaryExpression 104B、Parameter 112B | |

size class の切り上げと文字列本体は含まない。parser は pool され、`NodeFactory` の kind 別 arena (`core.Arena[T]`、46 kind) はファイルをまたいで使い回される。

## 4. 比較表

footprint は §2 の分布からの試算で、side table (symbol、flow、links) と文字列本体を含まない。依存 load は「親の行を読んだあと、子の header に届くまでに直列に待つ load の数」。

| | Pointer | A | B1 型付き slab | B2 word-packed | C 固定長行 + extra |
| --- | --- | --- | --- | --- | --- |
| B/node (checker.ts) | 84.6 | 33.2 (※) | 37.5 | 33.5 | 35.1 |
| Pointer 比 | 100% | 39% | 44% | 40% | 42% |
| 名前付きの子 1 つ | 1 | 2 | 2 | 1 | 1 (payload ≤ 2 と hot slot)、2 (それ以外) |
| list の先頭要素 | 3 | 3 | 3 | 2 | 2〜3 |
| `NodeRef` が密な連番か | — | はい | はい | いいえ (word offset) | はい |
| 行の型 | Go struct | header は struct、slot は番号 | 全部 Go struct | 全部 `[]uint32` | 行は struct、extra は `[]uint32` |
| GC が scan する列 | 全部 | なし | なし (text を offset にすれば) | なし | なし |
| 線形走査 (全ノード列挙) | 不可 | 可 | header 列は可 | kind 表を引けば可 | 可 |

※ A は非 child の data member を side map (`scalarValues` など) に置くので、この数字に含まれていない。識別子 text だけは `childStart` に intern id を同居させており追加 0B。

3 案の footprint は Pointer の 39〜44% に収まり、案の間の差は 1 割程度しかない。メモリ量では案を選べない。選ぶ軸は次の 3 つである。

1. 子への到達が何段か、そのとき accessor に分岐が入るか。
2. `NodeRef` が密か。密でないと symbol / flow / checker links の列が引けない。
3. 上流のメンテナが読めて直せる形か。

## 5. 推奨する順序

1. B1 (型付き slab) を先に walk に接続する。型の安全性が上流と同じ水準で、`NodeRef` が密である。期待は「Pointer と同等」で、勝つ理由は無い。採否の根拠は GC mark 時間と RSS に置く。
2. B1 が Pointer に大きく負けた場合だけ、C を足して「index ベースで 1 段にできるか」を見る。C は行が typed で `NodeRef` も密なので、B2 より提案に耐える。
3. A は対照として 1 本だけ接続する。すでに実装があり、再現用である。
4. B2 は上限の測定が必要になるまで作らない。提案には使わない。
