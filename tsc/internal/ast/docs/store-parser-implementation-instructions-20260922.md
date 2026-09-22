# Store parser (TODO 7b) 実装指示書

作成日: 2026-09-22。対象は実装を担当するエージェント。設計の根拠は [store-ast-design-20260922.md](store-ast-design-20260922.md) (以下「設計文書」) §2.7 (構築) と §6 の 7。前段の成果は `tsc/internal/ast/store/` (TODO 7a、[store-generator-verification-report-20260922.md](store-generator-verification-report-20260922.md))。検証の手順と判定は [store-parser-verification-instructions-20260922.md](store-parser-verification-instructions-20260922.md)。

設計判断はこの文書で済ませてある。書かれていない判断が必要になったら、実装を進めずに報告する。

## 0. 作るものの性格

現行の parser (`tsc/internal/parser`、6,854 行) を **機械的に移植**して、Pointer AST を経由せずに Store を直接構築する parser を作る。目的は 2 つ:

1. 設計文書 §2.7 の構築規則 (id で書く、post-order、scratch、Compact、speculation の truncate) が本物の parser で成り立つことを示す。
2. **GC と memory の効果を初めて実測する。** Walk (7a) は命令数で Store が負けることを示した。設計の賭けは「header が noscan」「footprint 39%」「割り当てが file あたり数本」にあり、それが測れる最初の段階が parse である。

| | Pointer parser | 7b |
| --- | --- | --- |
| 出力 | `*ast.SourceFile` | `*store.SourceFile` (§3.4) |
| ノードの生成 | `p.factory.NewXxx` + `finishNode` | 生成 constructor `p.b.NewXxx(flags, pos, end, …)` |
| 親の設定 | `finishNode` が `ForEachChild` で子を辿って書く | constructor が書く (7a で実装済み) |
| list | `[]*ast.Node` を arena に clone、`NodeList` を alloc | parser 側の LIFO scratch → `Builder.List` で block 1 つ |
| speculation | `rewind` は scanner と診断だけ戻す (作ったノードは残る) | `nodes` / `extra` を truncate |
| JSDoc | TS は flag だけ、JS は eager parse + reparse | **flag だけ (全 file)**。JS の JSDoc 由来ノードは作らない (§5) |

まだ作らないもの (7c 以降): JSDoc ノードと reparse (`jsdoc.go`、`reparser.go`)、`ExternalModuleIndicator` と module reference の収集 (`references.go`、`ast.SetExternalModuleIndicator`)、binder との結合、program への組み込み、incremental parse、`api` / LSP。

## 1. ガード

1. package は `tsc/internal/storeparser`、package 名 `storeparser`。import してよいのは `internal/ast` (Kind、flags、Diagnostic、Pragma 等の値型)、`internal/ast/store`、`internal/scanner`、`internal/core`、`internal/diagnostics`、`internal/tspath`、`internal/stringutil`。**`internal/parser` は import しない** (テストからは比較のために import してよい)。
2. **`internal/parser`、`internal/scanner`、`internal/ast` (store 以外) を変更しない。** scanner に足したい API が出たら、足さずに報告する。
3. `internal/ast/store` に足してよいのは §3 に列挙したものだけ。7a の accessor、`ForEachChild`、shape 表、typed view の形は変えない。generator (`generate-go-store.ts`) の変更は §3.5 (storetest の移動) と、§3.1 の constructor の変更に伴う出力だけ。
4. 移植は**機械的**に行う。関数名、関数の順序、コメント、ローカル変数名を Pointer parser と同じに保つ (検証で `diff` を取る)。挙動を「改善」しない。Pointer parser のバグに気づいたら直さず報告する。
5. `doc.go` の package comment (英語) に: 設計文書へのパス、`internal/parser` の機械的な移植であること、7b の範囲 (JSDoc / reparse / references 未移植)、移植規則 (§4) の要点。

## 2. ファイル

```
tsc/internal/ast/store/
  store.go                 §3.1〜3.3 の追加 (Builder の scratch / View / 書き換え / truncate)
  sourcefile.go            §3.4 store.SourceFile (手書き)
  storetest/               §3.5 等価テストの driver と生成の比較 (package storetest、7a から移動)
    equivalence.go
    equivalence_generated.go
tsc/internal/storeparser/
  doc.go
  parser.go                internal/parser/parser.go の移植 (JSDoc / reparse の呼び出しを除く)
  types.go                 internal/parser/types.go の移植
  utilities.go             internal/parser/utilities.go の移植 (必要な分)
  pragmas.go               getCommentPragmas / extractPragmas / processPragmasIntoFields (parser.go 末尾の移植。分けてよい)
  parser_test.go           §6
  bench_test.go            §7
tools/scripts/tsc/generate-go-store.ts   §3.5 の出力先変更
```

`jsdoc.go`、`reparser.go`、`references.go` は移植しない。

## 3. `store` package への追加

### 3.1 Builder の scratch と Compact (設計文書 §2.7)

```go
type Builder struct {
    nodes []NodeHeader
    extra []uint32
    texts strings.Builder     // []byte から変更。String() がゼロコピーなので View の Text() が読める
    src   string
    file  uint32
    view  Store               // View() が返す Node の s。nodes / extra / src / texts を b と同期する
}

func (b *Builder) Reset(src string, file uint32)   // len を nodes[:1]、extra[:3] に戻す。cap は保つ。texts は Reset()
func (b *Builder) Finish() *Store                  // 正確なサイズへ copy (Compact)。texts は strings.Clone。番号は振り直さない
```

- parser は Builder を 1 つ持ち (`p.b`)、file ごとに `Reset` する。`sync.Pool` の `putParser` が `*p = Parser{…}` で状態を消す形なので、**`scanner` と同じく `b` を残す**。
- `Finish` の後に `b` の scratch を触ってはいけない。`Finish` は copy なので `b` を次の file で上書きしても Store は壊れない。
- **`texts` を parser 間で共有しない。** `Finish` が `strings.Clone` で正確な長さの string にするのはそのためである (ゼロコピーのまま使い回すと次の file で上書きされる。2026-09-16 に再現済み、設計文書 §2.4)。
- `NewBuilder(src, file)` は残す (`convert` が使う)。

constructor の `text string` 引数と `textWords` / `identifierData` の compare は 7a のまま。理由: 識別子の `scanner.TokenValue()` は escape が無ければ source の部分文字列そのもの (同じ backing array) なので、`b.src[start:end] == text` は長さ比較と `memequal` の pointer 一致で終わる。literal の text (cooked) は quote が外れるので compare が失敗して `texts` に入る。**それでよい** (checker.ts の `texts` は 11 KB)。literal を source 参照にする最適化は 7b の後で、計測してから決める。

### 3.2 構築中の読み: `View`

parser は子の kind、pos、end、member を読む (`ast.IsIdentifier(name)`、`typeNode.Type().Pos()`、`modifiers.Nodes` の走査、`opening.TagName()` など、約 30 箇所)。

```go
// View は id の Node を返す。append 中の nodes を指すので、次の constructor / List / NewXxx の呼び出しまでしか有効でない。
func (b *Builder) View(id NodeRef) Node
```

- `b.view` は `Store{nodes: b.nodes, extra: b.extra, src: b.src, texts: b.texts.String()}` を `View` のたびに詰め直す (slice header と string header の copy だけ。O(1))。
- 7a の typed view、役割 accessor、`List.Refs()`、`Node.Ref()` がそのまま使える。**新しい読み取り API は作らない。**
- 規則: `View` の戻り値を、parse 関数の呼び出し (= append) をまたいで保持しない。ループ内で `View` して `AddFlags` するのは append ではないので可。検証で `View(` の使用箇所を全部目視する。
- `View` で得た `Text()` (texts 側) を file をまたいで保持しない (§3.1)。

### 3.3 id で書く

Pointer parser がノード生成後に書き換える箇所は 3 種類 (§4.3)。Builder に次を足す:

```go
func (b *Builder) AddFlags(id NodeRef, flags ast.NodeFlags)
func (b *Builder) SetFlags(id NodeRef, flags ast.NodeFlags)
func (b *Builder) SetLoc(id NodeRef, pos, end int32)
func (b *Builder) Mark() (nodes, extra int)          // speculation 用
func (b *Builder) Truncate(nodes, extra int)         // Mark の値に戻す。texts は戻さない (§4.4)
func (b *Builder) Len() int                          // len(nodes)。dead node の計測用
```

`SetParent` は作らない (親は constructor が書く。§4.3 で全部消えることを確認済み)。

### 3.4 `store.SourceFile`

parser の結果。`ast.SourceFile` の「parser が設定する field」のうち、木の走査を要しないものだけ。

```go
type SourceFile struct {
    Store             *Store
    FileName          string
    Path              tspath.Path
    Text              string            // = Store.src
    ScriptKind        core.ScriptKind
    LanguageVariant   core.LanguageVariant
    IsDeclarationFile bool
    Flags             ast.NodeFlags     // root の flags (sourceFlags を含む)。Root().Flags() と同じ値
    IdentifierCount   int
    NodeCount         int               // len(nodes) - 1 (dead node を含む)
    diagnostics       []*ast.Diagnostic // file は nil のまま
    jsDiagnostics     []*ast.Diagnostic
    CommentDirectives []ast.CommentDirective
    Pragmas           []ast.Pragma
    ReferencedFiles, TypeReferenceDirectives, LibReferenceDirectives []*ast.FileReference
    CheckJsDirective  *ast.CheckJsDirective
}
func (f *SourceFile) Root() Node
func (f *SourceFile) Diagnostics() []*ast.Diagnostic
func (f *SourceFile) JSDiagnostics() []*ast.Diagnostic
```

無いもの (7c 以降): `ExternalModuleIndicator`、`CommonJSModuleIndicator`、`imports` / `ModuleAugmentations` / `AmbientModuleNames` / `UsesUriStyleNodeCoreModules`、`jsdocCache` / `ReparsedClones` / `jsdocDiagnostics`、`TextCount`、binder の field。`ast.Diagnostic.file` は nil のまま (`attachFileToDiagnostics` 相当は program 結合時)。

### 3.5 等価テストの共有 (`storetest`)

7a の `store_test.go` にある driver (`mismatches`、`equivalent`、`preorderStore` / `preorderPointer`) と生成の `compareMembers` / `compareRoles` を、`internal/ast/store/storetest` (package `storetest`、非 test package) に移す。generator の出力先を `storetest/equivalence_generated.go` に変える。

```go
package storetest
type Options struct {
    SkipReparsed bool   // Pointer 側の NodeFlagsReparsed ノードを nil / 不在として扱う (§5)
    MaskFlags    ast.NodeFlags   // Flags の比較から除く bit (§5)
}
func Equivalent(file *ast.SourceFile, s *store.Store, opts Options) (Mismatches, int)
func Preorder(root store.Node) []store.Node
```

7a の `TestEquivalence` / `TestCorpus` はこれを呼ぶ形に書き換える (`Options{}` で従来どおり)。`storetest` は `internal/parser` を import しない (入力は `*ast.SourceFile`)。

## 4. 移植規則

`internal/parser/parser.go` を `storeparser/parser.go` に copy し、次の置換を上から順に当てる。**規則に無い書き換えはしない。**

### 4.1 型

| Pointer | Store |
| --- | --- |
| `*ast.Node` (子、戻り値、引数) | `store.NodeRef` (`NoNodeRef` = nil) |
| `*ast.NodeList`、`*ast.ModifierList` | `store.ListRef` (block index。`NoListRef` = nil) |
| `[]*ast.Node` (list の材料) | `[]store.NodeRef` (parser の scratch、§4.2) |
| `ast.NodeFactory` | 無し。`p.b *store.Builder` |
| `node == nil` / `node != nil` | `node == store.NoNodeRef` / `node != store.NoNodeRef` (素の `0` と比べない) |
| `list == nil` | `list == store.NoListRef` |
| `p.nodeSliceArena`、`p.stringSliceArena` | 無し |
| `p.currentParent`、`p.setParentFromContext`、`overrideParentInImmediateChildren` | 無し |
| `p.jsdocInfos`、`p.jsdocComments*Space`、`p.jsdocTagComments*Space`、`p.reparseList`、`p.reparsedClones`、`p.hasDeprecatedTag` | 無し (§5) |

### 4.2 ノードと list の生成

```go
// Pointer
result := p.finishNode(p.factory.NewIfStatement(expression, thenStatement, elseStatement), pos)
// Store
result := p.b.NewIfStatement(p.flags(), int32(pos), int32(p.nodePos()), expression, thenStatement, elseStatement)
```

- `p.flags()` は `finishNodeWithEnd` の flags の部分: `contextFlags` に `hasParseError` なら `ThisNodeHasError` を OR して `hasParseError = false`。
- Pointer の constructor が自分で立てる flag (`NewCallExpression` の `flags & OptionalChain` 等。`ast_generated.go` を読んで列挙する) は `p.flags() | …` で渡す。
- `finishNodeWithEnd(node, pos, end)` の形は `end` をそのまま渡す。
- **引数の評価順**: Go は左から評価するので、`p.nodePos()` より前に子の parse が済んでいなければならない。Pointer が constructor の引数の中で `p.parse…` を呼んでいる箇所 (33 箇所) は、**子を先にローカル変数に受ける** 形に直す。これが規則 4 (機械的) の唯一の例外で、直した箇所は報告に列挙する。
- 多 kind の定義 (`ForInOrOfStatement`、`CaseOrDefaultClause`、`BindingPattern`、`TypeAliasDeclaration`、`ImportDeclaration`) は constructor の先頭に kind を渡す。
- token / keyword は `p.b.NewToken(kind, flags, pos, end)`。
- Identifier: `p.b.NewIdentifier(flags, pos, end, text)`。`p.identifierCount++` と `await` の判定は `newIdentifier` に残す。
- **list**: parser に `elems []store.NodeRef` の LIFO scratch を持つ。`parseListIndex` / `parseDelimitedList` は `mark := len(p.elems)` を取り、要素を `p.elems = append(p.elems, elt)` で積み、終わりに `at := p.b.List(pos, end, p.elems[mark:])` を作って `p.elems = p.elems[:mark]` に戻す。入れ子の list は上に積むので壊れない。`parseListIndex` の戻り値 `[]*ast.Node` は、呼び側が `newNodeList` するまでの一時値なので、`(mark int)` を返して呼び側で `List` を作る形にする (`parseSourceFileWorker` の `statements` がこれに当たる。`reparseList` の append は §5 で消える)。
- `p.newNodeList(loc, nodes)` / `p.newModifierList` → `p.b.List(int32(loc.Pos()), int32(loc.End()), nodes)`。
- `parseEmptyNodeList` → 空 block。**`createMissingList`**: Pointer は番兵の backing array で「missing」を区別し、parser 内 (`typeHasArrowFunctionBlockingParseError`) だけが `isMissingNodeList` を見る。Store では空 block を作って index を `p.missingLists collections.Set[store.ListRef]` に入れ、`isMissingNodeList(at)` はその membership にする。file ごとに clear。
- `ast.NodeIsMissing(node)` / `NodeIsPresent` → `View` で pos == end を見る同名の関数を `utilities.go` に書く。

### 4.3 生成後の書き換え

Pointer parser がノード生成後に `Flags` / `Loc` / `Parent` を書く箇所は次の 9 行 (群) で、全部読んで確認済み。

| 行 (parser.go) | 内容 | Store |
| --- | --- | --- |
| 475 | `result.Flags \|= p.sourceFlags` (SourceFile) | `p.b.AddFlags(root, p.sourceFlags)`。`store.SourceFile.Flags` にも同じ値を置く |
| 1139、1928 | `m.Flags \|= NodeFlagsAmbient` (modifiers の各要素) | `for _, m := range p.b.View(modifiers).Refs() { p.b.AddFlags(m, Ambient) }` (ループ内に append なし) |
| 2269〜2270 | `implicitExport.Loc = …; .Flags = Reparsed` (`namespace a.b` の暗黙 export modifier) | `NewToken(KindExportKeyword, NodeFlagsReparsed, pos, pos)` で最初から作る (書き換え不要) |
| 3706〜3708 | `NewOptionalTypeNode(typeNode.Type())` に `typeNode` の Flags / Loc を写し、`Parent` を付け替え | `p.b.NewOptionalTypeNode(v.Flags(), v.Pos(), v.End(), inner)`。`Parent` は constructor が書く。元の `typeNode` は dead node になる |
| 4818〜4825、4841 | JSX の閉じタグ欠落時、捨てる parse 結果の子を新しい親に付け替え | constructor が書くので不要。捨てた `lastChild` は dead node |
| 4866 | `operatorToken.Loc = (pos, pos)` | `NewToken(KindCommaToken, p.flags(), pos, pos)` |
| 5484 | `node.Flags \|= OptionalChain` (NonNull の連鎖を下る) | `p.b.AddFlags(node, OptionalChain)`、`node = p.b.View(node).Expression().Ref()` |
| 5794〜5798 | `unparseExpressionWithTypeArguments` (Parent の付け替え) | 関数ごと削除。呼び側の constructor が書く |
| 608 | reparse 後の statement の `Parent` | constructor が書く |

**`SetParent` が要る箇所は無い。** 移植中に新たに出たら報告する。

### 4.4 speculation

`ParserState` に `nodes, extra int` (= `p.b.Mark()`) を足し、`rewind` で `p.b.Truncate` する。`lookAhead` は常に rewind するので、callback 内で作ったノードは消える。`tryParse…` で成功したときは残る。

- `texts` は truncate しない (`strings.Builder` に truncate が無い。escape 付きの text を speculation 中に作るのは稀で、残るのは dead bytes。検証で数える)。
- rewind を伴わずに捨てられるノード (§4.3 の JSX と `parseTupleElementType`、`reparseTopLevelAwait` が作り直す statement) は **dead node として残す**。`Root()` は最後のノードなので影響しない。数は `NodeCount − 訪問数` で検証が数える。Pointer の `NodeCount` (`factory.nodeCount`) も speculation の分を含むので、比較のときは両方報告する。

### 4.5 SourceFile

- `parseSourceFileWorker` / `parseJSONText` の最後: `root := p.b.NewSourceFile(…)`、`p.b.AddFlags(root, p.sourceFlags)`、`s := p.b.Finish()`、`store.SourceFile{Store: s, …}` を返す。`finishSourceFile` の移植は §3.4 の field だけ。`createJSDocCache`、`SetHasLazyJSDoc`、`ReparsedClones`、`SetExternalModuleIndicator`、`collectExternalModuleReferences` は呼ばない (コメントで 7c と書く)。
- `reparseTopLevelAwait` は移植する (statement の再 parse。JSDoc ではない)。
- `getCommentPragmas` は `scanner.GetLeadingCommentRanges(f *ast.NodeFactory, …)` に factory を渡す。CommentRange の値を作るだけなので、parser に `pragmaFactory ast.NodeFactory` を 1 つ持って渡す。これは scanner の API の都合であり、`doc.go` に書く。
- `checkJSSyntax` (JS の構文診断、86 行) は `View` で移植する。`jsDiagnostics` に入る。

### 4.6 ast の述語

parser.go が `*ast.Node` に対して呼ぶ `ast` の関数は、kind だけを見るもの (`IsModifierKind`、`IsKeyword`、`GetBinaryOperatorPrecedence`、`IsAssignmentOperator`、`ModifierToFlag`、`CanHaveDecorators` 等) はそのまま `View(id).Kind()` に対して使う。ノードの中身を見るものは `utilities.go` に `View` 版を書く: `tagNamesAreEquivalent` (text と PropertyAccess の再帰)、`typeHasArrowFunctionBlockingParseError`、`nodeIsMissing` / `nodeIsPresent`、`isOptionalChain`、`isLeftHandSideExpression` (kind のみ)、`getTextOfNodeFromSourceText` (pos / end)。`ast.ModifiersToFlags` は constructor が `modifierFlags` で計算するので不要。

## 5. JSDoc と JS

7b は JSDoc ノードを作らない。

- **flag**: `withJSDoc(node, info)` の代わりに、`jsdocScannerInfo()` の値から `NodeFlagsHasJSDoc` と `NodeFlagsPossiblyContainsDeprecatedTag` を **全 file** で立てる (Pointer の TS 経路と同じ)。`withJSDoc` の 56 箇所は `p.b.AddFlags(result, p.jsdocFlags(info))` に置き換える (`info == 0` なら何もしない)。
- **TS / TSX / D.TS**: Pointer も flag しか立てない (`@see` / `@link` の eager parse は `jsdocInfos` に入るだけで木を変えない) ので、木と `diagnostics` は**完全一致**する。
- **JS / JSX**: Pointer は JSDoc を eager parse し、`reparseTags` で `@typedef` → `JSTypeAliasDeclaration` 等を statement に足し、`@type` 等を宣言の `Type` に入れる (`NodeFlagsReparsed`)。7b はこれを作らない。等価テストは `SkipReparsed: true` で Pointer 側の Reparsed ノードを不在として比べ、`MaskFlags: HasJSDoc | PossiblyContainsDeprecatedTag` で JS の flag 差を除く (Pointer の JS 経路は parse 結果が空なら flag を立てない)。
- **JSON**: `parseJSONText` を移植する (JSDoc なし)。
- `jsdocDiagnostics` (JS の JSDoc parse 由来) は作らない。JS の `diagnostics` は Pointer の部分集合になりうる (検証指示 §1.3)。

## 6. テスト (`storeparser/parser_test.go`)

1. **fingerprint**: `fixtures.ASTBenchFixtures` を直接 parse し、preorder の `kind|pos|end` の sha256 と訪問数が `astBenchmarkBaselines` (7a の `walkBaselines` と同じリテラル) と一致する。
2. **等価 (fixture)**: 2 fixture と 7a の `smallSource` について `storetest.Equivalent(pointerFile, storeFile.Store, Options{})` が空。Pointer 側は `parser.ParseSourceFile` の既定。
3. **等価 (corpus)**: 7a の `corpus` (`testdata/fixtures` + `tests/cases/{compiler,conformance}`、`-short` で fixtures だけ) を、file の種類ごとの Options で流す: TS 系と JSON は `Options{}`、JS 系は `SkipReparsed` + `MaskFlags`。kind と label で集計して 1 回で全部出す。
4. **診断**: 同じ corpus で `Diagnostics()` を比べる。比較の単位は `(Pos, Len, Code, MessageText)` の列。TS 系と JSON は列が完全一致。JS 系は Store の列が Pointer の列の部分列で、差分の件数と message を `t.Log` する。
5. **dead node**: fixture ごとに `NodeCount − 訪問数` を `t.Log`。speculation の truncate を確かめる小さな source (`(a, b) => c` の arrow 判定、`a < b > c` の type argument の試行、`x!!.y` の optional chain) で `NodeCount == 訪問数` になる。
6. **scratch の再利用**: 同じ parser (pool から取る) で同じ file を 2 回 parse し、2 回目の `testing.AllocsPerRun` が 1 回目より小さく、`Finish` の copy 分 (`nodes` / `extra` / `texts` / `SourceFile` / diagnostics) を除いて 0 に近いこと。数値は報告。
7. **`View` の規則**: `grep -n 'View(' parser.go` の全行を目視し、append をまたぐ保持が無いことを報告に書く (機械検査は無い)。
8. **`texts`**: 7a の `TestTexts` と同じ source を直接 parse し、identifier の escape と literal の `Text()`、`texts` の長さが同じ。

全テストを tag なしと `-tags storechecks` で通す。7a のテスト (`store`、`storetest` に移した後) も両 tag で通ること。

## 7. ベンチ (`storeparser/bench_test.go`)

`internal/parser/ast_benchmark_test.go` の形を移す。

- `BenchmarkStoreParseV1/<fixture>/{pointer,store}`: `pointer` は `parser.ParseSourceFile`、`store` は `storeparser.ParseSourceFile`。`b.ReportAllocs()`。ns/op、B/op、allocs/op。
- `BenchmarkStoreParseKPCV1/{pointer,store}`: checker.ts、`kperf.Session` (GC オフ)。inst / cycles / IPC。
- `BenchmarkStoreParseRetainedV1/{pointer,store}`: corpus の `fixtures` 部分 (`-short` と同じ集合) を **全部 parse して結果を保持**し、`runtime.GC()` を 1 回呼んで、(a) `HeapAlloc` の増分 / 総ノード数 [B/node]、(b) 保持した状態での `runtime.GC()` 1 回の時間 [ms] を `ReportMetric` する。これが「GC が辿る pointer」の最初の実測になる。Pointer 側は `*ast.SourceFile` を保持する。
- Pointer 側と Store 側で JSDoc の仕事量が違う点: TS fixture では両方 flag だけ (Pointer は `@see` / `@link` があると eager parse する。fixture にいくつあるかは検証が数える)。

## 8. 完了条件

1. `node tools/scripts/tsc/generate.ts` で生成物が再現し、`git status` の変更が `generate-go-store.ts`、`internal/ast/store/` (§3)、`internal/storeparser/` だけ。`internal/parser`、`internal/scanner`、`internal/ast` (store 以外) に差分なし。
2. `go build ./... && go vet ./internal/ast/... ./internal/storeparser/...` が通る。production から `storeparser` / `store` の import なし。
3. §6 の全テストが両 tag で通る。7a のテストも通る。
4. §7 の 3 ベンチが完走。
5. 報告に: (a) 規則 4 の例外 (子をローカルに受け直した 33 箇所前後の一覧)、(b) 移植中に新たに出た書き換え箇所 (§4.3 の表に無いもの)、(c) `View(` の全使用箇所と保持していないことの確認、(d) JS 系の診断の差分の内訳、(e) dead node の数 (fixture ごと)、(f) `store` に足した API の一覧と、§3 に無いものを足したならその理由、(g) 移植しなかった関数の一覧 (JSDoc / reparse / references 以外にあれば)。
