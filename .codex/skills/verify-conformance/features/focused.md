# Focused reproduction

suiteの結果に現れた特定ケースを、同じcommit・ハーネスで再実行する。

## Sub-features

- TestLocal配下のファイル×設定をregexで指定。
- type/symbol/error等の子検証も選択可能。
- 再現ログと生成内容を別snapshotへ保存。

## How to get to it (user POV)

comparison.jsonまたはsnapshot.jsonから正確なテスト名を選び、Goの`-run`形式に合わせて正規表現をescapeする。

## Driving it with conformance.py

SKILL.mdのfocusedコマンドで`indexSignatureTypeInference.ts`を実行する。snapshot.jsonの対象ケースがそのファイルだけで、0件ではないことを確認する。子検証まで絞った場合は、期待する完全なtest名が存在して終了statusを持つことも確認する。比較する両版には同一filterを使う。cleanup後もfocused snapshotを読めることを確認する。

## Gotchas

Goは`/`で各階層のregexを区切る。設定名の括弧・ドット等をescapeする。親だけ走って子が0件の結果を成功の根拠にしない。focused結果をsuite全体のsnapshotと比較しない。
