# Conformance：コアロジックを優先した50件の分析と修正計画

作成日：2026-09-15。対象：`/Volumes/SanDisk1TB/worktree/cursor-ast-store-tests`。
調査基準：`dcbf75cb38a0ede46a02999d42c355ae970dd5fe`。入力：`tsc/conformance.log`。

## 結論と調査範囲

50件を個別の例外処理で直すべきではない。主な問題は、Store移行で **ノードの意味、親とスコープ、複製の独立性、構文復旧の位置、変換段階の契約** が維持されなくなったことにある。名前解決やAST共通処理から直し、同じ契約を使うケースをまとめて検証する。

ログの297件は「ファイル×設定」の失敗構成数であり、297個の独立したバグではない。本書の50件も同じ単位で数える。子の error/output/type は重複加算しない。内訳は全panic 44構成と、誤った型判定3構成、export欠落1構成、変数宣言欠落2構成。コメントや識別子の綴りだけの差分より、コンパイル停止・誤診断・実行時挙動の破壊を優先した。ログにはconformanceとcompiler/regressionの両方が含まれる。

**証拠の限界：** ログには実行時の完全なcommit識別情報がなく、現HEADで50件全部を再実行した結果ではない。修正作業中に代表ケースを再現した事実と、既存ログ・ソース読解による分析を以下で区別する。「コード整合」は経路の静的確認であり、修正成功の宣言ではない。

- 代表再現済み：`dynamicImportDefer`、`nestedDestructuringOfRequire`、`awaitUsingDeclarationsInForAwaitOf.3`、`jsxFactoryIdentifier`、`usingDeclarationsWithESClassDecorators.4`。
- 先に修正済みの `invalidTryStatements` は今回の50件に含めない。コミット `dcbf75cb38a` で欠落ブロックの空リスト位置を保持し、対象テスト成功を確認した。
- 方針変更後は追加の修正・テスト実行を停止した。未検証だった `Handle.Text` の試作変更は戻した。テストコード・参照baselineは変更していない。

## 優先順位と原因の対応

|群|構成数|問題|証拠の強さ|着手順の理由|
|---|---:|---|---|---|
|C|1|binderとcheckerのrequire別名判定が不一致|代表再現＋コード・履歴整合|意味解析そのものの破壊。狭い境界で契約を戻せる|
|A|7|MetaPropertyのText契約喪失|代表再現＋旧実装との対応|parser・checker・import分類を横断する共通機能|
|B|19|変換ASTの親設定が名前解決用スコープを上書き|代表再現。親破壊機序はコード上の有力仮説|広範なJSX利用で停止。個別nil回避は不適切|
|D|6|欠落リストが位置を失い不正な診断を作る|代表再現＋コード・履歴整合|構文エラーを診断できない|
|E|3|JSX名前空間名が空文字になる|ログ＋旧Text実装・利用箇所整合|正常な型照合と誤り検出の両方を壊す|
|F|9|JSDoc importが通常の宣言へ変換されない|ログ＋コード・移行前実装整合|宣言ファイル生成が停止|
|G|2|推論されたプロパティ名を変数束縛名に流用|ログ＋生成経路・upstream比較整合|不正な中間ASTを作り出す|
|H|1|decoratorのexport消失と名前共有|代表再現。共有機序あり、因果は未実測|モジュールの公開値が消える|
|I|2|enum/namespaceのローカルvarが消える|ログで欠落確認。消失境界は未確定|実行時の参照先が壊れる可能性|

AとE、BとHは共通基盤に関係するが、同じ修正で全部直ると断定しない。Bは影響範囲が広いので、C/Aのように境界が確定した変更を先行し、Bの不変条件を測定してから実装する。

## 原因別の設計評価と修正方法

### C：requireの別名判定をbinderとcheckerで一致させる

**問題・証拠：** `binder/binder.go:1412` の `rootDeclarationRef` は入れ子の分割代入をすべて遡り、外側のrequireを根拠に内側の `grey` にAliasを付ける。一方 `ast/utilities.go:2855` はBindingElementの親を2階層だけ遡る。checkerはAliasに対応する宣言を見つけられず `resolveAlias` で停止する。NodeRef化した `cf31f05f34c5` の前後で判定の違いを確認した。

**なぜ悪いか：** スコープを求める「最外宣言」と、直接requireから導入された別名を判定する「直近の宣言」は異なる概念。同じrootを使い回すと、binderが作るSymbolの種類とcheckerの解釈が矛盾する。

**理想：** require別名の定義を一つに集約し、Handle/NodeRef版が同じ規則に従う。スコープ探索は独立させる。

**現実的な手順：** require判定へ元のref/kindを渡し、BindingElementの場合だけ親を2階層遡る。block scopeとparameter判定のroot探索は維持する。`resolveAlias` にnilガードを追加して逃げない。

**受入条件：** 入れ子ケースが通り、通常のrequire・一段分割代入・ブロックスコープの既存ケースに診断、型、symbol、emitの退行がない。

### A：MetaPropertyの意味を文字列格納方法から切り離す

**問題・証拠：** `ast/store.go:1388` のHandle.TextはIdentを返すだけだが、旧 `ast/ast.go:284` のNode.TextはMetaPropertyの子Nameを返していた。`ast/utilities.go:2129`、`parser/parser.go:5447`、`checker/checker.go:27027` がその契約を使う。`import.defer()` がimport呼び出しと認識されず通常の式として型検査され、assertionに達する。

**なぜ悪いか：** 格納列の読み取りを、構文種類ごとの意味を返すAPIの代替にしてしまった。呼び出し判定だけを直してもparserのsource flagsやシンボル取得に不一致が残る。

**理想：** Identのような生のアクセスと、Textの意味を明示して分離し、移行前後で種類別の契約を照合する。

**現実的な手順：** Handle.TextにMetaPropertyの子名を返す処理を復元する。Eの名前空間名は別の検証単位として同じ契約表に含める。AST各種への場当たり的なfallbackや呼び出し側3箇所の個別補正は避ける。

**受入条件：** 7構成が通り、既存のimport.meta/new.target/不正なimport.defer/通常dynamic importでも診断・source flags・symbol・emitが整合する。

### B：構造上の親と意味解析用の親を混同しない

**問題・証拠：** 19構成のスタックはいずれも `binder/NameResolver.Resolve` に集約する。`jsxtransforms/jsx.go:488` はfactory識別子を元のparse JSXへ明示的に結びつける。しかし `ast/store.go:374` のlinkChild、:398のlinkList、:1523のattachSameStoreは同Storeの子の親を再設定する。`NewCallExpression` もこの経路を使う。名前解決が変換後classに到達すると、未bindのSymbolからMembersを読む `binder/nameresolver.go:128` で停止し得る。

**なぜ悪いか：** emit ASTは元の意味情報を参照しながらノードを再利用する。木の組み立てが意味解析用の親を破壊すると、後段が元のスコープに戻れない。「同じStoreにある」は「親を変えてよい」の根拠ではない。

**理想：** parse ASTは不変。emit側の構造・original対応・名前解決の文脈を別の契約にする。再利用ノードへの接続がsemantic parentを変更しない。

**現実的な手順：** まず代表ケースで識別子生成直後、call接続後、resolver入口のidentity・parent・original・scopeを記録し、破壊境界を確定する。確認後、emit Storeに自動親設定を抑制する明示ポリシーを導入する案を評価する。linkChild/linkList/attachSameStoreを漏れなく対象にし、Parse専用linkChildRefの扱いも整理する。parserの既定動作と明示的SetParentは維持する。checker Storeまで一律変更しない。

**受入条件：** 19構成の通過に加え、ローカルfactory・引数によるshadowing・qualified name・不在factoryの正しい診断が保たれる。panicが消えただけでは不十分。親の上書き仮説が反証されたら、この設計変更は採用しない。

### D：missingとemptyを位置付きデータとして表す

**問題・証拠：** `parser/parser.go:936` のcreateMissingListが0を返し、`Store.ListLoc(0)` は[-1,-1]。for-ofの空宣言を作る :1797 から `checker/grammarchecks.go:1365` に渡り、位置-1のTS1123を生成する。scannerはソース位置として扱うため停止する。

**なぜ悪いか：** 「存在しない」「構文復旧で欠落した」「存在する空リスト」を0に潰すと、後段に必要な位置と状態が消える。位置だけ復元してmissing判定を失うのも誤り。`isMissingNodeList` はarrow functionの構文復旧にも使われる。

**理想：** リストは位置とmissing状態を独立して保持し、無いoptional listとは区別する。

**現実的な手順：** missingリストを現在位置付きで確保し、missingフラグまたは同等の明示状態を導入する。createMissingListとisMissingNodeListを一緒に移行する。for-ofだけparseEmptyListに差し替える案は狭い暫定策であり、共通契約の完成形ではない。scannerで-1を0に丸めるのは誤診断を隠すため不可。

**受入条件：** 6構成で正しいゼロ幅診断が出る。arrow/function parameterの復旧、欠落enum/module/blockの既存ケースも保たれる。先行コミットのブロック局所修正との重複は、この契約復元後に整理する。

### E：JSX名前空間名を正しいプロパティキーへ戻す

**問題・証拠：** ログで `Property ''` が増え、属性型の本来のエラーが失われている。旧Node.TextはJsxNamespacedNameを `namespace:name` に合成していた。現在のHandle.Textでは空のIdentを読み、`checker/jsx.go:1097` のプロパティ検索に空文字が渡る。

**なぜ悪いか：** 複合構文を単一の格納文字列と同一視すると、検索キーが変わる。これは表示だけの問題ではなく型判定の誤り。

**理想／現実策：** Aと同じText契約で名前空間名の合成を復元する。checkerに空文字の特例は入れない。正しい名前空間、似た通常属性名、非一致index signatureの各ケースを分けて確認する。

**受入条件：** 3構成のerror/typeが参照と一致し、単にエラー件数が減るだけではなく本来検出すべき属性エラーを保持する。

### F：JSDoc importを宣言出力のASTへ正規化する

**問題・証拠：** `declarations/transform.go:1232–1236` はJSImportDeclarationをDeepCloneするだけ。移行前には通常ImportDeclarationへkindを変える処理があった。現在のUpdateImportDeclarationも元のkindを保持するため、printerにJS専用kindが流れて停止する。

**なぜ悪いか：** 複製と構文種類の変換は別操作。内部専用ノードを後段に渡し、printerに補完を期待するのは段階の契約違反。Handle側のkindだけを書き換えるとStoreとの不一致も起こり得る。

**理想：** declaration変換境界で出力可能なASTの集合を保証する専用の正規化処理を持つ。

**現実的な手順：** NewImportDeclarationで通常ノードを構築し、clause/specifier/attributes/modifiersとoriginal・位置の必要な情報を引き継ぐ。printerがJSImportDeclarationも受け入れる変更で穴を塞がない。

**受入条件：** 9構成の宣言出力が一致し、不要なJSDoc importを再利用しないこと、type-only情報とmodule設定を保持することも確認する。

### G：束縛識別子と実行時の推論名を分離する

**問題・証拠：** `printer/factory.go:345` のgetNameがGetNameOfDeclarationの結果を無条件にcloneする。匿名classの親PropertyAssignmentから文字列／computed nameが返ると、`estransforms/esdecorator.go:635` で変数名として使われ `printer.go:838` で停止する。ローカルupstream TypeScriptのgetNameは識別子かどうかを検査している。

**なぜ悪いか：** `.name` に使えるプロパティ名は、変数宣言に使えるBindingNameとは限らない。汎用Handleだけでは境界違反を見落としやすい。

**理想：** local binding identifierとruntime inferred nameを別API・返り値契約にする。

**現実的な手順：** getNameで再利用可能な通常Identifierのみcloneし、他は生成識別子にする。生成済み識別子の扱いはupstreamと既存生成名メタデータの規則を照合する。class.nameの推論処理は別に維持する。printerで文字列を識別子として出力しない。

**受入条件：** 2ケースが通り、computed nameの評価回数・class.name・decoratorの初期化順を維持する。回帰commitは本調査では未確定。

### H：DeepCloneとemitフラグの独立性を回復する

**問題・証拠：** CommonJS出力で `exports.default =` が消える。`ast/store_factory.go:438` の同Store DeepCloneNodeはVisitEachChildの結果を返すため、変更のないleaf identifierでは同じidentityを返す。`printer/factory.go:355` はcloneのつもりでそのidentifierにEFLocalNameを付ける。元へのフラグ漏洩はコード上起こり得る。内側名がdefault_1でなく_defaultになる現象にはBの親接続も関係し得るが、因果は未実測。

**なぜ悪いか：** DeepCloneという名前が保証する独立性がなく、片方のemit状態変更が別の用途に及ぶ。getName側でフラグを消すだけでは他の利用者に問題が残る。

**理想：** cloneは独立したidentityを持ち、originalへの対応と必要なメタデータだけを定義済み規則で引き継ぐ。共有を返す処理はcloneとは別APIにする。

**現実的な手順：** clone前後のidentityとEFLocalNameを測定する。同Storeでもleafを含めて複製する共通実装へ統一できるか、既存copySubtreeのメタデータ・親処理を確認する。Bと分離して比較し、必要なら順序付きの修正にする。

**受入条件：** export代入と内部名の両方が正しくなり、元のidentifierのemit flagsがclone操作で変化しない。ESNext側の対応ケースも追加の検証対象とする（本書の50件には非計上）。

### I：var宣言が失われる境界を特定してから直す

**問題・証拠：** ログでenumの `var MouseButton;`、namespaceの `var m;` が欠落する。runtime syntaxのaddVarForDeclarationはGetLocalNameExで名前を作り、`moduletransforms/commonjsmodule.go:631` はIsLocalNameならvarを保持する。初期化子なし・そのフラグなしなら宣言が消える経路がある。

**未確定事項：** 生成側の早期returnなのか、変換途中のmetadata/identityなのかは未計測。DeepCloneの問題と同一原因だと断定できない。

**理想：** enum/namespaceの局所束縛の必要性を変換後も保持する明示契約を持ち、偶発的なノード共有やフラグ消失に依存させない。

**現実的な手順：** addVarForDeclarationの生成有無、生成時・CommonJS入口のidentity/EFLocalName、最終出力を記録する。消失した境界だけを直し、他群の修正で偶然改善した場合にも機序を確認する。無条件でvarを追加すると宣言マージや再宣言を壊し得るので避ける。

**受入条件：** 2ケースで必要なvarだけが復元され、enum/namespaceの宣言マージ、export、名前衝突の既存ケースが維持される。

## 50件の個別対応表

各行はログ上の正確なテスト識別子。原因・修正・受入条件は上の群IDに対応する。ログ行へのリンクは元の失敗証拠を指す。設定違いは実際に別のコンパイル経路なので省略しないが、独立原因としては数えない。

|番号|群|ケース（ファイル×設定）|このケースで観測された失敗|
|---:|---|---|---|
|1|C|[nestedDestructuringOfRequire.ts](/Volumes/SanDisk1TB/worktree/cursor-ast-store-tests/tsc/conformance.log:139)|greyにAliasを付けたが対応宣言を解決できずpanic|
|2|A|[dynamicImportDefer.ts_module=es2020](/Volumes/SanDisk1TB/worktree/cursor-ast-store-tests/tsc/conformance.log:3003)|import.deferを通常式として型検査しassertion|
|3|A|[dynamicImportDefer.ts_module=nodenext](/Volumes/SanDisk1TB/worktree/cursor-ast-store-tests/tsc/conformance.log:3117)|import.deferを通常式として型検査しassertion|
|4|A|[dynamicImportDefer.ts_module=esnext](/Volumes/SanDisk1TB/worktree/cursor-ast-store-tests/tsc/conformance.log:3231)|import.deferを通常式として型検査しassertion|
|5|A|[dynamicImportDefer.ts_module=preserve](/Volumes/SanDisk1TB/worktree/cursor-ast-store-tests/tsc/conformance.log:3345)|import.deferを通常式として型検査しassertion|
|6|A|[dynamicImportDefer.ts_module=commonjs](/Volumes/SanDisk1TB/worktree/cursor-ast-store-tests/tsc/conformance.log:3459)|import.deferを通常式として型検査しassertion|
|7|A|[dynamicImportDefer.ts_module=es2015](/Volumes/SanDisk1TB/worktree/cursor-ast-store-tests/tsc/conformance.log:3573)|import.deferを通常式として型検査しassertion|
|8|A|[importDeferCallCommonJS.ts](/Volumes/SanDisk1TB/worktree/cursor-ast-store-tests/tsc/conformance.log:10421)|import.deferを通常式として型検査しassertion|
|9|B|[tsxUnionTypeComponent1.tsx](/Volumes/SanDisk1TB/worktree/cursor-ast-store-tests/tsc/conformance.log:1620)|名前解決がSymbol未定義のコンテナを参照しpanic|
|10|B|[inlineJsxFactoryDeclarationsLocalTypes.tsx](/Volumes/SanDisk1TB/worktree/cursor-ast-store-tests/tsc/conformance.log:1962)|名前解決がSymbol未定義のコンテナを参照しpanic|
|11|B|[reactHOCSpreadprops.tsx](/Volumes/SanDisk1TB/worktree/cursor-ast-store-tests/tsc/conformance.log:6072)|名前解決がSymbol未定義のコンテナを参照しpanic|
|12|B|[reactDefaultPropsInferenceSuccess.tsx](/Volumes/SanDisk1TB/worktree/cursor-ast-store-tests/tsc/conformance.log:6262)|名前解決がSymbol未定義のコンテナを参照しpanic|
|13|B|[jsxFactoryQualifiedNameWithEs5.ts_target=es2015](/Volumes/SanDisk1TB/worktree/cursor-ast-store-tests/tsc/conformance.log:6914)|名前解決がSymbol未定義のコンテナを参照しpanic|
|14|B|[jsxFactoryQualifiedNameWithEs5.ts_target=es5](/Volumes/SanDisk1TB/worktree/cursor-ast-store-tests/tsc/conformance.log:7076)|名前解決がSymbol未定義のコンテナを参照しpanic|
|15|B|[jsxFactoryQualifiedNameResolutionError.ts](/Volumes/SanDisk1TB/worktree/cursor-ast-store-tests/tsc/conformance.log:7238)|名前解決がSymbol未定義のコンテナを参照しpanic|
|16|B|[jsxFactoryQualifiedName.ts](/Volumes/SanDisk1TB/worktree/cursor-ast-store-tests/tsc/conformance.log:7396)|名前解決がSymbol未定義のコンテナを参照しpanic|
|17|B|[jsxFactoryNotIdentifierOrQualifiedName2.ts](/Volumes/SanDisk1TB/worktree/cursor-ast-store-tests/tsc/conformance.log:7570)|名前解決がSymbol未定義のコンテナを参照しpanic|
|18|B|[jsxFactoryNotIdentifierOrQualifiedName.ts](/Volumes/SanDisk1TB/worktree/cursor-ast-store-tests/tsc/conformance.log:7744)|名前解決がSymbol未定義のコンテナを参照しpanic|
|19|B|[jsxFactoryMissingErrorInsideAClass.ts](/Volumes/SanDisk1TB/worktree/cursor-ast-store-tests/tsc/conformance.log:7918)|名前解決がSymbol未定義のコンテナを参照しpanic|
|20|B|[jsxFactoryIdentifierWithAbsentParameter.ts](/Volumes/SanDisk1TB/worktree/cursor-ast-store-tests/tsc/conformance.log:8076)|名前解決がSymbol未定義のコンテナを参照しpanic|
|21|B|[jsxFactoryIdentifierAsParameter.ts](/Volumes/SanDisk1TB/worktree/cursor-ast-store-tests/tsc/conformance.log:8208)|名前解決がSymbol未定義のコンテナを参照しpanic|
|22|B|[jsxFactoryIdentifier.ts](/Volumes/SanDisk1TB/worktree/cursor-ast-store-tests/tsc/conformance.log:8340)|名前解決がSymbol未定義のコンテナを参照しpanic|
|23|B|[jsxFactoryAndReactNamespace.ts](/Volumes/SanDisk1TB/worktree/cursor-ast-store-tests/tsc/conformance.log:8488)|名前解決がSymbol未定義のコンテナを参照しpanic|
|24|B|[jsxEmitWithAttributes.ts](/Volumes/SanDisk1TB/worktree/cursor-ast-store-tests/tsc/conformance.log:8760)|名前解決がSymbol未定義のコンテナを参照しpanic|
|25|B|[jsxChildrenSingleChildConfusableWithMultipleChildrenNoError.tsx](/Volumes/SanDisk1TB/worktree/cursor-ast-store-tests/tsc/conformance.log:8934)|名前解決がSymbol未定義のコンテナを参照しpanic|
|26|B|[commentsOnJSXExpressionsArePreserved.tsx_jsx=react,module=commonjs,moduledetection=force](/Volumes/SanDisk1TB/worktree/cursor-ast-store-tests/tsc/conformance.log:11176)|名前解決がSymbol未定義のコンテナを参照しpanic|
|27|B|[commentsOnJSXExpressionsArePreserved.tsx_jsx=react,module=system,moduledetection=force](/Volumes/SanDisk1TB/worktree/cursor-ast-store-tests/tsc/conformance.log:11338)|名前解決がSymbol未定義のコンテナを参照しpanic|
|28|D|[awaitUsingDeclarationsInForAwaitOf.3.ts_target=esnext](/Volumes/SanDisk1TB/worktree/cursor-ast-store-tests/tsc/conformance.log:77)|空宣言リストの診断位置-1でpanic|
|29|D|[awaitUsingDeclarationsInForOf.5.ts_target=esnext](/Volumes/SanDisk1TB/worktree/cursor-ast-store-tests/tsc/conformance.log:108)|空宣言リストの診断位置-1でpanic|
|30|D|[parserForOfStatement21.ts](/Volumes/SanDisk1TB/worktree/cursor-ast-store-tests/tsc/conformance.log:283)|空宣言リストの診断位置-1でpanic|
|31|D|[parserForOfStatement2.ts](/Volumes/SanDisk1TB/worktree/cursor-ast-store-tests/tsc/conformance.log:314)|空宣言リストの診断位置-1でpanic|
|32|D|[parserES5ForOfStatement21.ts_target=es2015](/Volumes/SanDisk1TB/worktree/cursor-ast-store-tests/tsc/conformance.log:398)|空宣言リストの診断位置-1でpanic|
|33|D|[parserES5ForOfStatement2.ts_target=es2015](/Volumes/SanDisk1TB/worktree/cursor-ast-store-tests/tsc/conformance.log:429)|空宣言リストの診断位置-1でpanic|
|34|E|[checkJsxNamespaceNamesQuestionableForms.tsx](/Volumes/SanDisk1TB/worktree/cursor-ast-store-tests/tsc/conformance.log:2326)|空文字のJSX名による誤診断／型差分|
|35|E|[jsxNamespacePrefixIntrinsics.tsx](/Volumes/SanDisk1TB/worktree/cursor-ast-store-tests/tsc/conformance.log:9585)|空文字のJSX名による誤診断／型差分|
|36|E|[jsxNamespacedNameNotComparedToNonMatchingIndexSignature.tsx](/Volumes/SanDisk1TB/worktree/cursor-ast-store-tests/tsc/conformance.log:9805)|空文字のJSX名による誤診断／型差分|
|37|F|[importTag5.ts](/Volumes/SanDisk1TB/worktree/cursor-ast-store-tests/tsc/conformance.log:2378)|宣言出力へJSImportDeclarationが残りpanic|
|38|F|[importTag20.ts](/Volumes/SanDisk1TB/worktree/cursor-ast-store-tests/tsc/conformance.log:2426)|宣言出力へJSImportDeclarationが残りpanic|
|39|F|[importTag19.ts](/Volumes/SanDisk1TB/worktree/cursor-ast-store-tests/tsc/conformance.log:2474)|宣言出力へJSImportDeclarationが残りpanic|
|40|F|[importTag16.ts](/Volumes/SanDisk1TB/worktree/cursor-ast-store-tests/tsc/conformance.log:2522)|宣言出力へJSImportDeclarationが残りpanic|
|41|F|[importTag15.ts_module=es2015](/Volumes/SanDisk1TB/worktree/cursor-ast-store-tests/tsc/conformance.log:2570)|宣言出力へJSImportDeclarationが残りpanic|
|42|F|[importTag18.ts](/Volumes/SanDisk1TB/worktree/cursor-ast-store-tests/tsc/conformance.log:2618)|宣言出力へJSImportDeclarationが残りpanic|
|43|F|[importTag15.ts_module=esnext](/Volumes/SanDisk1TB/worktree/cursor-ast-store-tests/tsc/conformance.log:2666)|宣言出力へJSImportDeclarationが残りpanic|
|44|F|[typedefHoisting.ts](/Volumes/SanDisk1TB/worktree/cursor-ast-store-tests/tsc/conformance.log:5068)|宣言出力へJSImportDeclarationが残りpanic|
|45|F|[jsDeclarationEmitDoesNotReuseUnrelatedJSDocImport.ts](/Volumes/SanDisk1TB/worktree/cursor-ast-store-tests/tsc/conformance.log:9972)|宣言出力へJSImportDeclarationが残りpanic|
|46|G|[esDecorators-classExpression-namedEvaluation.2.ts](/Volumes/SanDisk1TB/worktree/cursor-ast-store-tests/tsc/conformance.log:3872)|変数束縛名に非Identifierが入りpanic|
|47|G|[esDecorators-classExpression-missingEmitHelpers-classDecorator.14.ts](/Volumes/SanDisk1TB/worktree/cursor-ast-store-tests/tsc/conformance.log:4285)|変数束縛名に非Identifierが入りpanic|
|48|H|[usingDeclarationsWithESClassDecorators.4.ts_module=commonjs,target=es2015](/Volumes/SanDisk1TB/worktree/cursor-ast-store-tests/tsc/conformance.log:44)|exports.default代入が消失、内部名も変化|
|49|I|[enumKeysQuotedAsObjectPropertiesInDeclarationEmit.ts](/Volumes/SanDisk1TB/worktree/cursor-ast-store-tests/tsc/conformance.log:11533)|enum/namespaceのローカルvar宣言が消失|
|50|I|[chainedImportAlias.ts](/Volumes/SanDisk1TB/worktree/cursor-ast-store-tests/tsc/conformance.log:11547)|enum/namespaceのローカルvar宣言が消失|

## 実施順と検証の方法

1. C → Aを独立した修正単位にする。コード経路が確定していて影響を切り分けやすい。
2. Bは先に観測してparent契約を確定し、D/E/F/Gはそれぞれの境界契約として直す。小さい修正を積み上げても共通基盤の問題を隠さない。
3. Hのclone独立性を確認・修正し、Iの宣言消失境界を測定する。B/H/Iを一度に変更して「件数が減った」だけで原因を判断しない。
4. 各修正で元のケースと近縁の成功ケースを検証し、通過確認後にコミットする。共通原因で複数設定が一緒に直った場合は一つの修正コミットに対象構成を記録し、空コミットに分割しない。

対象を絞る例（リポジトリのtscディレクトリで実行）：

```sh
go test ./internal/testrunner -run '^TestLocal$/^nestedDestructuringOfRequire\.ts$' -count=1
```

設定付きケースはファイル名の後ろの設定まで選ぶか、当該ファイルの全設定を実行する。対象0件・skip・build成功をpassと数えない。テストコードと参照baselineは編集せず、diagnostic/output/type/symbol/source mapの既存検証を維持する。

コミット間の広範な退行確認には `.codex/skills/verify-conformance/SKILL.md` の固定commit snapshotを使う。同helperは未コミット変更を検証しない点に注意し、直接実行による修正後確認と分ける。今回選んだcompiler/regressionケースはconformance専用helperだけでは網羅できないため、TestLocalで別途確認する。最終的には全TestLocalも実行し、既存失敗・新規退行・改善・skip・未完了を区別する。

本調査では追加の全体実行を行っていない。50件すべての修正完了、あるいは未選択247件への効果は主張しない。

## 証拠の所在

- 元ログ：`tsc/conformance.log`（個別表のリンク）。
- 代表再現：`/tmp/conformance-core-before.log`、`/tmp/conformance-defer-before.log`、`/tmp/conformance-jsx-before.log`。
- 先行修正の前後：`/tmp/conformance-invalid-before.log`、`/tmp/conformance-invalid-after.log`。
- 移行前契約：`6d6930385e98^` のparser/AST/declaration transformer。requireの変更：`cf31f05f34c5`。
- upstream比較はローカル `node_modules/typescript/lib/typescript.js` を参照した。これは最新の外部リリースとの比較ではない。
- 本書の機械可読一覧：`priority-50.json`。ログSHA256、基準commit、設定付き識別子、原因群を保存する。
