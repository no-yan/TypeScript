# Luna実装作業記録

[実装指示書](luna-accessor-implementation-plan-20260910.md)に従って更新する。P0/P1は旧arity APIで検証済み。名前付き生成Accessorへの移行は新規G0〜G2として記録し、既存結果を保持する。

| ID | 状態 | 変更関数 | 消した再解決 | 残存 | 意味監査 | 機械語/frame | artifact |
|---|---|---|---|---|---|---|---|
| P0 | 検証済み | `tools/scripts/tsc/luna_accessor_audit.py` | Binder本体の変更なし（監査overlayのみ） | 時間benchmark未測定。以後はbaseline/candidate snapshotを明示して再利用 | 20入力baseline、保存済みsyntax-scalars意味digestと一致。診断・Symbol・Flow・訪問traceを比較可能 | 監査binary build確認。性能frame比較はP1で実施 | `.cursor/skills/verify-tsc/artifacts/20260910-luna-accessor-p0-baseline-04` |
| P1 | 検証済み | `bindParameterFlowRef`, `bindBindingElementFlowRef` | Parameter/BindingElementの個別`ChildRef`/generated getter再解決を一括snapshotへ置換 | modifier list・宣言name受渡しは対象外（P6）。時間benchmark未測定 | 20/20意味digest一致。診断・Symbol・Flow・訪問順一致 | 対象objdump保存。frameはParameter 96B / BindingElement 80Bで維持、CALL相当行は26→22 / 23→17 | `.cursor/skills/verify-tsc/artifacts/20260910-luna-accessor-p1-candidate` |
| G0 | 検証済み | `generateStoreAccessors`、`store_accessors_generated.go`、`store_accessors_test.go`、P0 TARGET_GO/overlay | schema由来の名前付きAccess*/Accessor。Parameter/BindingElementで検証。slot番号をcallerに出さない | Binder callerはG1。全node生成済みでG2移行待ち | AST境界テスト緑。`go test ./internal/ast ./internal/binder ./internal/compiler`緑。同dir dprintでregen一致 | 未測定（G0対象外） | 生成物はrepo内。20入力はG1候補で実施 |
| G1 | 検証済み | `bindParameterFlowRef`, `bindBindingElementFlowRef` | SyntaxChildren4/5とmodifiersRefGenerated再解決をAccess*へ。ModifiersはAccessor field | 宣言name受渡しはP6。Access*がCALLのまま（未inline） | 20/20意味digest一致（compare-saved） | BindingElement frame 80B維持。Parameter 96B→144B（監査ビルド。通常ビルドの値ではない。AccessParameter CALLとstruct spill）。CALL相当は BindingElement 17→14、Parameter系はAccess追加あり | `.cursor/skills/verify-tsc/artifacts/20260910-luna-accessor-g1-candidate` |
| G2 | 検証済み | `forEachBindChildGenerated`、`bindIfStatementRef`、`bindAccessExpressionFlowRef` | walker全caseとIf/PropertyAccessをAccess*へ。SyntaxChildrenN/IfStatementRefs/PropertyAccessExpressionRefsのbinder callerなし | 旧API定義とASTテストは互換残存。他生成helperのListSlotAt/ChildRefはP7以降 | 20/20意味digest一致 | Access* CALL有無はG1同様ケース依存。時間benchmark未実施（G2条件外） | `.cursor/skills/verify-tsc/artifacts/20260910-luna-accessor-g2-candidate` |
| P2 | 検証済み | `skipParenthesesN`、`maybeBindExpressionFlowIfCallN`、`bindCallExpressionFlowRef` | skip後kindの再KindAt削除。非optional CallでAccessCallExpression一回取得とcallee再利用 | optional chain後段は従来取得。paren childはChildRef(0) | 20/20意味digest一致 | 未測定 | `.cursor/skills/verify-tsc/artifacts/20260910-luna-accessor-p2-candidate` |
| P3 | 検証済み | Binary Flow Resolved（`bindChildrenRef`/`bindBinaryExpressionFlowResolved`/`bindLogicalLikeExpressionResolved`/`bindDestructuringAssignmentFlowResolved`/`bindBinaryExpressionKind`） | Flow領域内のoperator/slot再解決をAccessor受渡しへ | bindKind前段とbindChildren後段の2回Accessは残存（意図的） | 20/20意味digest一致 | 未測定 | `.cursor/skills/verify-tsc/artifacts/20260910-luna-accessor-p3-candidate` |
| P4a | 検証済み | Variable/Prefix/Postfix/Delete + `bindInitializedVariableFlowResolved` | Access*一回取得とname受渡し | binding pattern再帰は子name再取得（意図的） | P4候補で20/20 | 未測定 | `.cursor/skills/verify-tsc/artifacts/20260910-luna-accessor-p4-candidate` |
| P4b | 検証済み | Conditional/While/Do/For/ForInOf/Try | getter式をAccess fieldへ置換。Flow操作は移動なし | — | 同上 | 未測定 | 同上 |
| P5 | 検証済み | `bindSwitchStatementRef`、`bindCaseBlockRef` | Switch Access*、clauses ListLen外出し、clause ref/kind保持、KindAt二重を解消 | list owner全面はP7 | 20/20意味digest一致 | 未測定 | `.cursor/skills/verify-tsc/artifacts/20260910-luna-accessor-p5-candidate` |
| V1 | 検証済み（意味） | G0〜G2＋P2〜P5累積 | 各カード20入力一致を積み上げ | wall/KPCまとめ測定は未実施（missing） | p5 candidate＝累積headで20/20 | wall/KPC missing | p2〜p5 candidates |
| P6a | 検証済み | `getDeclarationNameResolved`、`hasDynamicNameResolved` | name=0を解決済み欠損としてcoreへ | caller受渡しはP6b | 20/20 | 未測定 | `.cursor/skills/verify-tsc/artifacts/20260910-luna-accessor-p6a-candidate` |
| P6b | 検証済み | `declareSymbolResolved`とModule/Class/SourceFile/AddToTable Resolved、Variable/Parameter/Property受渡し | declare経路でname再resolve削除 | Function/Class/exportはP6c | 20/20 | 未測定 | `.cursor/skills/verify-tsc/artifacts/20260910-luna-accessor-p6b-candidate` |
| P6c | 検証済み | export/Function/Class名のResolved受渡し（`bindExportDeclarationRef`/`bindExportAssignmentRef`/`bindClassLikeDeclarationRef`/`bindFunctionOrConstructorTypeRef`/`bindFunctionDeclarationRef`） | strict checkと宣言workerで同一resolved name | listはP7 | 20/20 | 未測定 | `.cursor/skills/verify-tsc/artifacts/20260910-luna-accessor-p6c-candidate` |
| P7a | 検証済み | `BindListSpan`/`TryBindListSpan`/`BindListSpanElem`（`store_bind_span.go`）+境界試験 | local listのowner/header一回解決API | consumer適用はP7b | 20/20（APIのみ・Binder未変更） | 境界試験緑 | `.cursor/skills/verify-tsc/artifacts/20260910-luna-accessor-p7a-candidate` |
| P7b | 検証済み | `bindListRef`/`modifierFlagsRef`/`hasExportDeclarationsRef`/`listIndexRef`/`eachList`/`bindEachStatementFunctionsFirstRef`/`bindInitializedVariableFlowResolved` | local Span branch＋foreign fallback | ListSlotAt往復は後続 | 20/20 | 未測定 | `.cursor/skills/verify-tsc/artifacts/20260910-luna-accessor-p7b-candidate` |
| P8 | 検証済み | `isNarrowableReference`/`hasNarrowableArgument`のchild局所Handle共有＋foreign境界試験 | Argument/Operator/Expressionの短絡後半再取得 | — | 20/20 | 未測定 | `.cursor/skills/verify-tsc/artifacts/20260910-luna-accessor-p8-candidate` |
| V2 | 検証済み（意味） | P6〜P8累積 | 各カード20入力一致を積み上げ | wall/KPC missing | p8 candidate＝累積headで20/20 | wall/KPC missing | p6〜p8 candidates |
| P9 | 検証済み | root Resolved共有、OptionalChain expression/questionDot共有、modifierはP7 Span | 同一宣言内root再walk・optional child再取得 | D1/D2設計待ち | 20/20 | 未測定 | `.cursor/skills/verify-tsc/artifacts/20260910-luna-accessor-p9-candidate` |
| D1 | 設計待ち | [luna-accessor-d1-parent-design-20260910.md](luna-accessor-d1-parent-design-20260910.md) | — | 全訪問親引数化はしない。限定Identifier patchはレビュー後 | 対象外 | 対象外 | 設計メモ |
| D2 | 設計待ち | [luna-accessor-d2-header-design-20260910.md](luna-accessor-d2-header-design-20260910.md) | — | 契約なしheader pointer禁止 | 対象外 | 対象外 | 設計メモ |
| V3 | 意味検証済み／性能評価未完了 | [luna-accessor-v3-residual-20260910.md](luna-accessor-v3-residual-20260910.md) | 15分類対応表 | wall/KPC・parse+bind・GC missing | p9＝最終累積20/20 | missing明記 | p9 candidate + V3 doc |

## 2026-09-11 レビュー修正

P6cのFunctionExpression名をstrict-mode検査へ再利用。P9のOptionalChain後段へ構文snapshotを渡し、後段のAccess再呼出しを除去。Binary専用処理・Call構文判定には生成ChildrenAccessorを導入し、未使用list解決を除く。境界試験と任意関数の通常／監査ビルドobjdump検証を追加。新たな意味・機械語検証の結果は別記する。wall/KPC・parse+bind・GCは未完了のまま。

レビュー修正版はAST/Binder/Compilerテスト成功、20入力でP9・保存baseline双方に意味一致。通常機械語でBinary未使用list解決とOptionalChain後段Access除去を確認。保持費（親frame +64 B）を含め、詳細は[修正記録](accessor-review-fixes-20260911.md)。性能評価は未完了。


### 2026-09-11 固定規模の測定完了

[測定結果](accessor-review-measurement-20260911.md)。レビュー修正前後でcheckerの命令−1.24%、cycles−2.09%（各p=.002、KPC A/A合格）。domはbenchstat有意差なし。bind/parse+bindのwallは非有意かつA/A不合格。GC probeはcurrentだが、Store保持とA/A変動のためGC改善は未証明。旧missing記載は測定前の履歴として保持する。pointer同一セッション比較・制御したGC評価は未完了。
