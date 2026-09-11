# Resolved中継関数の局所化（2026-09-11）

メンテナが元の処理と対照しやすいよう、取得済みsyntaxを使う処理を既存の関数境界へ戻した。実装変更はbinder.goのみ。比較元はcheckpoint `e38ef0b8093`。

- Binaryのdestructuring判定をbindChildrenRefのBinary caseへ展開。Operatorが`=`かつLeftが存在するときだけLeftのKindを読む条件を維持。
- OptionalChain後半の小さなswitchをbindOptionalChainRefへ展開。questionDot、property/index、typeArguments、argumentsの訪問順と、true/false targetの保存・復帰位置を維持。
- 名前・root・childrenの取得だけをするRef中継関数を除去し、その取得を呼び出し側へ移した。長い宣言・Flow処理は元のRef名の共有関数とし、取得済み値を明示的な引数で受け取る。
- computed nameでは通常の名前取得をしない条件、missing nameを再解決しない契約、GetSymbolTable/parentの評価順、BindingElement再帰でのKind取得回数を維持。
- BinderのResolved付きメソッドは18→0、メソッド総数は20減。宣言処理やFlow処理の大きな本体を複製せず、2passとbind順を維持した。生成Accessorやwalker、Store配列の表現は変更していない。

## 検証

- `go test ./internal/ast ./internal/binder ./internal/compiler`: 成功。
- `go vet ./internal/ast ./internal/binder ./internal/compiler`: 成功。
- 新規snapshotから意味監査binaryをbuildし、checker/dom、missing、computed、optional、exports、JS/JSX等20入力を実行。変更前の意味digestと20/20一致。
- 監査対象の生成Accessor呼び出し回数も20/20差分なし。すべてのStore getterを計数したわけではないため、総命令数の不変を意味しない。
- `git diff --check`: 成功。

artifactは`.cursor/skills/verify-tsc/artifacts/20260911-binder-local-accessors/`。選択repoは`/Volumes/SanDisk1TB/worktree/cursor-ast-store-tests`、HEAD `e38ef0b8093`のdirty作業内容。tsgolint_git_revはnull。監査identityは`current`で、終了時binder.go SHAも一致。対照は`20260911-checkpoint-review-fixes/builds/fixed/results`（revision基準では`stale`）。対照binaryのbinder.go source SHAがcheckpointのbinder.goと一致することを確認して意味比較に使った。

候補cloneは直前PMU artifactのworktrees.txtに記録したものと同じで、今回未使用。pointer版の再build/実行はしていない。今回のns/op、B/op、allocs/op、PMU、benchstatによる性能比較は`missing`。直前のPMUは今回の編集前コードの証拠なので、今回も同じ性能と断定しない。

既存CPU分析の広いhot pathはwalk/helpers、割当driverはsymbolIdx/flows、symbolRefs、FlowNode/Symbol/Handleの拡大。今回はそれらの表現最適化ではなく、レビューのための関数構造整理である。性能を採用判断に使う際は、保存済みcheckpointを対照に同条件のwall/PMUとA/Aを測り、benchstatで比較する。全repoテストおよび新規性能測定は今回実行していない。
