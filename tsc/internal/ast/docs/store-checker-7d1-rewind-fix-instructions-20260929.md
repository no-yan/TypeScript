# 巻き戻された reparse の修正 (TODO 7d-1 追補) 実装指示書

作成日: 2026-09-29。対象は実装を担当するエージェント。発端は [store-checker-7d1-verification-report-20260929.md](store-checker-7d1-verification-report-20260929.md) (以下「検証報告」) の §3.5 と §8 の 1 で、ユーザーが案 (c) を選んだ。前提は [store-checker-7d1-implementation-instructions-20260928.md](store-checker-7d1-implementation-instructions-20260928.md) (以下「7d-1 指示書」) と、その実装 (未コミットの working tree、2026-09-29 00:23 の source の sha256 は `_store-checker-7d1-results/kpc-source-sha256.txt`)。

設計判断は本書で済ませてある。書かれていない判断が必要になったら、実装を進めずに報告する。

## 0. 直すもの

JS で、巻き戻される speculation の中で `@typedef` / `@callback` / `@import` が reparse されると、Store parser が通常 build で panic する (`store: invalid node edge`、`parseListIndex` の `View`)。最小の入力は `/a.js` の `x = (/** @typedef {number} T */ a, b);`。

- **Pointer の動き**: `(` の後が arrow function かどうか決まらないので、`tryParse` で引数として parse する。引数の JSDoc の `@typedef` を reparse し、JSTypeAliasDeclaration を `reparseList` に積む。arrow でないと分かると rewind するが、`rewind` (parser/parser.go) は `reparseList` を戻さない。そのため typedef は外側の `parseListIndex` で statement list に入り、T は宣言される。`parseDelimitedList` は `reparseList` を区切らず、区切るのは `parseListIndex` だけ。
- **Store の動き**: `rewind` の `Truncate` が speculation の行を消すので、`reparseList` は範囲外か、再利用された別のノードを指す。storeChecks build では `checkNoTruncatedRefs` が panic する。
- **原因**: 7d-1 指示書 §4.2 の「speculation で戻すものは Pointer と同じ」が、Store の行の再利用と両立しない。Pointer は捨てた speculation のノードを heap で生かしておける。

## 1. 直し方 (案 (c))

**`rewind` で、`reparseList` が `state.nodes` 以降の行を指していたら、その rewind では `Truncate` しない。** speculation で作ったノードは dead の行として残り、`reparseList` のノード (とその部分木) はそのまま生きる。Pointer が捨てたノードを heap に残すのと同じ結果になる。

`tsc/internal/storeparser/parser.go` の `rewind` を次の形にする (コメントは英語、既存の文体に合わせる):

```go
func (p *Parser) rewind(state ParserState) {
	...
	p.jsdocInfos = p.jsdocInfos[0:state.jsdocInfosLen]
	p.reparsedClones = p.reparsedClones[0:state.reparsedClonesLen]
	// The Pointer parser keeps the reparse list across a rewind, so a @typedef,
	// @callback or @import reparsed in a discarded speculation still goes into
	// the enclosing statement list. Its rows must outlive the rewind: the
	// speculation's nodes stay as dead rows rather than being truncated.
	if !p.reparseListFrom(state.nodes) {
		p.b.Truncate(state.nodes, state.extra)
	}
	if storeChecks {
		p.checkNoTruncatedRefs(p.b.Len())
	}
	for len(p.missingLists) != 0 && ... { // unchanged
	...
}

// reparseListFrom reports whether the reparse list names a node made at or
// after row nodes, which Truncate(nodes, …) would drop.
func (p *Parser) reparseListFrom(nodes int) bool {
	for _, ref := range p.reparseList {
		if int(ref) >= nodes {
			return true
		}
	}
	return false
}
```

決めてあること:

1. **条件は「`reparseList` に `ref >= state.nodes` がある」。** 「長さが伸びた」ではなく行で判定する。
   - top-level await の reparse (`parseSourceFileWorker` の中) は `state.nodes` を現在の長さに差し替えてから `rewind` するので、この条件は立たない。
   - 入れ子の speculation では、内側で残した typedef が外側の rewind でも同じ条件で残り、外側が成功すれば生き続ける。Pointer と同じ。
2. **`Truncate` しないときは `nodes` と `extra` の両方を残す** (typedef の部分木の list は `extra` にある)。`textBuf` は元から戻していないので変えない。
3. **`jsdocInfos` と `reparsedClones` は今のまま長さで戻す** (Pointer と同じ)。残った typedef は `HasJSDoc` を持つが cache には入らない。Pointer も同じで、等価テストで確かめる。
4. **`missingLists` の pop は今のまま無条件に残す。** pop は消えた block の index の再利用を防ぐためのもの。`Truncate` しなかった block は dead で、`isMissingList` に問われない。pop しても害は無い。
5. **`checkNoTruncatedRefs` の引数は `p.b.Len()`** (rewind の後の行数) にする。`state.nodes` のままだと、`Truncate` しなかったときに発火してしまう。検査の意味 (「3 つの列が存在しない行を指さない」) は変わらない。コメントの「a node it keeps alive would here be a reused row」は直した後の事実に合わせて書き換える。
6. **TS の速い側**: TS の file の `reparseList` は常に空で、`reparseListFrom` は空の slice の range で終わる。inline されることを `-gcflags=-m` で確かめる。されなければ呼び出し側で `len(p.reparseList) != 0 &&` を先に置く。
7. `ParserState` に field は足さない。`mark` も変えない。

変えないもの: `tsc/internal/ast/store` (Builder の API は既存の `Mark` / `Truncate` / `Len` で足りる)、storebinder の本体、生成器、変換器。`unsafe` を増やさない。

## 2. ガード

1. 変更してよいのは `tsc/internal/storeparser/parser.go` (§1)、storeparser と storebinder のテスト (§3)、`tsc/internal/storeparser/testdata/fuzz/FuzzEquivalence/` (§3 の 4)。それ以外 (特に `tsc/internal/ast/store/`、`parser`、`binder`) は変えない。
2. `node tools/scripts/tsc/generate.ts` と `(cd tools && go run ./cmd/storetranslate)` を再実行して、生成物が変わらないこと。
3. 等価テストに除外や緩めを足さない。既知の失敗は `FuzzEquivalence/a708303f611b95c8` (`ExternalModuleIndicator differs`) の 1 件だけ。

## 3. テスト

1. **位置 × tag の行列テスト** (storeparser の parser_test.go に `TestRewoundReparse` として足す): 下の位置 P の `C` に tag を 1 つ入れた JS の入力を、Pointer と Store で parse する。比べるのは `Equivalent`、`EquivalentJSDoc`、`fieldMismatches`、診断 (`TestCorpus` と同じ規則)。
   - tag (3 通り): `/** @typedef {number} T */`、`/** @callback F @returns {string} */`、`/** @import { A } from 'a' */`
   - 位置 P (13 通り。`/a.js`、13 だけ `/a.jsx`):
     1. `x = (C a, b);`
     2. `x = (C a);`
     3. `x = (C a, b) => a;` (speculation が成功する対照)
     4. `x = (C a = 1, b);`
     5. `x = (C {a}, b);`
     6. `x = async (C a, b);`
     7. `x = c ? (C a) : d;`
     8. `x = c ? (C a) : d => e;`
     9. `x = ((C a, b), c);` (入れ子)
     10. `x = (a = class { C m() {} }, b);` (class member の list から外へ押し出される typedef)
     11. `x = (a = { C m() {} }, b);`
     12. `f(C a, b);` (speculation の無い対照)
     13. `x = <div>{(C a, b)}</div>;`
   - **修正前の code で、各入力が tag なし / storeChecks で panic したか、等価が落ちたかを記録し、報告に表で載せる。** panic した入力が、この修正の効く入力。1 つも panic しなければ行列が足りないので、speculation の入口 (`tryParse` と、parse まで進む `lookAhead`) を読み、JS で JSDoc の host を含みうる位置を足す。
   - 修正後は 39 入力とも両 build で一致すること。
2. **`TestEquivalence` の入力**に、`/rewound-typedef.js`: `x = (/** @typedef {number} T */ a, b);\n/** @type {T} */\nvar y;` を足す (残った typedef と、それを使う側)。
3. **storebinder**: `smallSources` に 2 と同じ入力を `/rewound-typedef.js` で足す。bind の等価 (symbol と宣言) を確かめる。
4. **fuzz の seed**: 修正前に panic した入力のうち tag ごとに 1 つ (3 本) を、`testdata/fuzz/FuzzEquivalence/rewound-reparse-{typedef,callback,import}` として置く。形式は次のとおり:
   ```
   go test fuzz v1
   string(".js")
   string("x = (/** @typedef {number} T */ a, b);")
   bool(false)
   bool(false)
   ```
5. **`TestDeadNodes`**: 位置 1 の `@typedef` の入力を足し、残った dead node の数を固定する (speculation で作った引数とその JSDoc。数は実測して、内訳をコメントに書く)。
6. `-tags storechecks` で corpus 全体が通ること。`checkNoTruncatedRefs` は発火しない。

## 4. 計測

- `go test -gcflags=-m` で `reparseListFrom` (か呼び出し側の早期判定) と `rewind` の inline の判定を記録する。
- **KPC はユーザーが取る** (root が要る)。実装者は binary と実行スクリプトを用意する。
  - before: 検証で使った `_store-checker-7d1-results/storeparser.after.kperf.test` (sha256 は同じディレクトリの `kpc-binary-sha256.txt`)。
  - after: 修正後の source から `GOWORK=off go test -tags kperf -c` で作る。
  - `StoreParseKPCV1` を before1 → after → before2 の順で `-test.benchtime 20x -test.count 5`。検証の `run-kpc.sh` と同じく、source と binary の sha256 を実行前後で照合し、load < 1.5 を待つ。
  - 線: Parse cycles S/P が checker.ts ≤ 1.00、dom ≤ 1.00 (7d-1 の after は 0.826 / 0.824)。Store の inst の before からの差を報告する。見積りは rewind 1 回あたり 2〜3 命令で、+0.3% 以内。
  - Walk と Bind の binary は storeparser の変更で変わるが、測る経路は変わらないので取らない。

## 5. 完了条件

1. 生成物が再現し、`git status` の変更が §2 の範囲だけ。
2. `go build ./...`、`go vet ./internal/ast/... ./internal/storeparser/... ./internal/storebinder/...` が通る。
3. store / storeparser / storebinder のテストが tag なしと `-tags storechecks` で通る (既知の fuzz seed 1 件を除く)。`go test ./internal/ast/ ./internal/parser/ ./internal/binder/` も通る。
4. §3 の 1 の行列が修正後 0 不一致。修正前に panic した入力の表がある。
5. parse の等価 17,415 file と bind の等価 17,415 file が mismatch 0 のまま。宣言の Store の検査は 717,627 件から変わらないこと (corpus にこの形は無いので)。
6. §4 の KPC の binary とスクリプトがあり、ユーザーの計測の結果が線の内側。

## 6. 報告

7d-1 実装報告 ([store-checker-7d1-implementation-report-20260929.md](store-checker-7d1-implementation-report-20260929.md)) に「§10 巻き戻された reparse の修正 (2026-09-29)」を足し、次を書く:

- (a) `rewind` の diff。
- (b) 行列の修正前 / 後の表。
- (c) dead node の数。
- (d) inline の判定。
- (e) KPC (ユーザーの計測の後に追記)。
- (f) 本書に無い判断をした箇所 (無いはず)。

7d-1 指示書 §4.2 の「戻すものは Pointer と同じ」の直後に、「ただし `reparseList` が speculation の行を指す rewind では `Truncate` しない (本書)」と 1 行追記する。

## 7. 本書で決めたこと

| 項目 | 決めたこと | 根拠 |
| --- | --- | --- |
| 直し方 | 案 (c): 該当する rewind だけ `Truncate` しない | Pointer と同じ結果を 1 条件で出せる。(a) `reparseList` も戻すと T が宣言されず等価が壊れる。(b) 部分木の移し替えは参照の付け替えが要る |
| 判定 | `reparseList` に `ref >= state.nodes` があるか | 長さの差だと、top-level await の reparse と入れ子の speculation で誤る |
| 戻さないもの | `nodes`、`extra` (該当する rewind だけ)。`jsdocInfos` / `reparsedClones` / `missingLists` は今のまま | Pointer の `rewind` と同じものを戻す。`missingLists` の pop は dead の block に無害 |
| storeChecks の検査 | 引数を `p.b.Len()` にして残す | 直した後の不変条件「存在しない行を指さない」を検査する |
| テスト | 位置 13 × tag 3 の行列、修正前の panic を記録、fuzz seed 3 本 | 検証報告 §9: 検証者が見つけた 1 形で、網羅していない |
