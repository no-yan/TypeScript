# 解決済み情報を共有する内部APIの再設計・初稿

調査全体の根拠と到達点は [現時点のinsight](binder-investigation-insights-20260910.md) を参照。

## 根拠と範囲

Parameter経路の3者対照で、domの命令数が元実装比1.46%、同じ関数構成の再取得対照比1.62%減った。
これを根拠に設計段階へ進む。checkerの有意差、cycles/wallの改善、全経路への一般化は未確認。
詳細: [呼び出し経路の対照](threaded-name-experiment-20260910.md)。

目的は、処理が既に知っている構文情報をhelper境界で捨てないこと。
永続ASTはindex表現を保ち、解決済み情報は短い呼び出し・走査範囲でだけ保持する。
公開API・binderの診断/判定/走査順は維持する。全ASTのpointer化や大きなNodeViewは目的にしない。

## 入口と内部処理を分ける

汎用入口は既存のRef/Kindを受け付ける。取得済み情報がある呼び出し側は、同じ意味を持つ内部coreへ
その情報を渡せるようにする。coreの診断・merge等のアルゴリズムは一つだけにする。
実験用に複製したdeclareSymbolRef等を製品コードへ持ち込まない。

最初の対象は構文上の宣言名のみ。name ref/kindの小さな値を再利用する。
Storeは既にBinderが持ち、node kindも既存引数にあるため、それを別のReaderへ包むだけでは改善にならない。
最初からStore/全header/Flags/text/Flowをまとめたcontextにはしない。

既存getDeclarationNameRef/hasDynamicNameRefは「構文名の取得」と「取得した名前の処理」を分離する。
既存入口の振る舞いを残しつつ、取得済みの呼び出し元は後者へ直接到達する。
ExportAssignmentの早期分岐、computed/default exportによる名前取得省略、欠損・assigned-name fallbackは
元の条件で動かす。取得済みであることを理由にkeyword/診断判定を省略しない。

## lazyの単位と寿命

| 情報 | 取得契機 | 有効期間 | 禁止事項 |
| --- | --- | --- | --- |
| 構文名ref/kind | 既存処理が最初に名前を必要とするとき | 同じ宣言の同期処理中 | 別nodeへの流用、構造変更後の再利用 |
| list owner/start/length | list走査を開始するとき | 同じlistの構造が不変の走査中 | 別Storeでの使用、長さ変更をまたぐ保持 |
| 可変Flags/Symbol/Flow | 当面は元のアクセサ | 個別の不変性を証明するまで共有しない | 構文情報と同じ寿命とみなすこと |

入口に名前がある場合は追加lookup・cache-hit判定なしで渡せる形を優先する。
まだない場合は必要になる分岐で初めて取得する。未取得と「取得したが名前なし」は区別する。
optionalなcontextが必要なら、固定サイズのlocal値と明示的なknown状態に限定し、
node identityを検索するmapや全Binder cacheにはしない。ready分岐の利益とコストを別に測る。
assigned-name fallbackは、今回実証した固定的な構文名より強い不変性を仮定しない。

## 実装順序と判定

1. 名前を処理する末端helperを共通coreへ分離し、今回のParameter経路だけを接続する。
   旧入口は互換wrapperとして残し、他経路の先読みを増やさない。
2. 実験の複製版と意味を比較し、名前解決回数、実命令数、stack/escape、code sizeを確認する。
   wrapper化やready分岐で削減が消えるなら、抽象化を小さくする。
3. 既存計数から次の安定情報の利用経路を一つ選ぶ。node kind順には拡張しない。
   Parameterだけの特殊化から一般化できるか、別経路で独立に確認する。
4. 方針が維持できた段階で反復数・8入力・独立セッションを増やし、wall/cyclesを評価する。

stackは単純なfield数で予測せず生成コードを見る。局所structにpointerがないことと、
プログラム全体のGCが速いことは別である。allocation/scan/GC改善は別の保持条件で評価する。

## 成功と中止の条件

設計の機構確認: 意味の一致、consumerで再解決が消えること、保持コスト込みで実binder命令数が減ること。
一般化の確認: 異なる経路でも成立し、共通coreに診断ロジックの複製が不要なこと。
採用: 既定のwall3%、inst/cycles、非悪化区間、全回帰、phase/GCゲートを満たすこと。

getter数だけが減る、stack/分岐で命令数が増える、取得を不要な分岐に前倒しする、
可変状態の無効化規則が複雑になる場合は、その抽象化を縮小または保留する。
今回の一経路は設計開始の根拠であり、全面置換を正当化する証拠ではない。

## ancestor案からの設計更新

[ancestorの3者対照](ancestor-experiment-20260910.md)では、親取得の置換自体はchecker命令を
維持対照比0.90%減らしたが、全nodeのslice維持費に負け、元実装比1.91%増となった。
今回の再設計では全nodeに汎用ancestor sliceを要求しない。
直近親しか必要ないhelperには、既に渡しているparentKindにparentRefを添える等の
小さな呼び出し文脈を先に評価する。深い祖先列は複数の実需要と回収可能量を確認してから検討する。
再帰走査を残して別スタックを重ねるコストと、走査自体を明示stackへ置換する案は別実験である。

## 親ref引数案からの設計更新

[親ref引数の対照](parent-argument-experiment-20260910.md)では、再取得対照よりchecker命令1.14%減。
しかしbaselineとの差は非有意で、dom命令は0.51%増。配列をなくすだけでは採用に届かなかった。
Go compiler JSON診断で、引数追加によるwalkerのbig caller化とChildRefのinline制限を確認。
内部APIの再設計では、渡すfield数だけでなく巨大generated関数のIR規模も制約に含める。
次の対照は親情報を追加せず、walkerの分割等によるcodegen変化とCALL費用を単独評価する。
全getterのinline化を目的にせず、動的命令・cycles・wallが改善する構成を選ぶ。
