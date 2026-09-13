# braindex の手引き

[← README](../README.md)

コマンドごとの詳しい説明。入口と段階的な取り込みは README にある。

| ページ | コマンド |
|---|---|
| [索引の生成](generate.md) | `braindex`（設定 `braindex.json` の全キー・索引の各行の決め方・走査の記録・本文の変更の記録 `index/changes.json`） |
| [本文の検索](search.md) | `braindex search`（語を含む行と出典位置。索引に無い語を本文で引く） |
| [走査状態の診断](diagnose.md) | `braindex diagnose`（対象・読めなかった範囲・索引との差。「索引に無い＝存在しない」と読む前に） |
| [骨格の展開と追従](init-update.md) | `braindex init`・`braindex update`・各リポの骨格・エージェントに横断検索させる設定 |
| [週次レビューと検査](review-lint.md) | `braindex review`・`braindex lint` |
| [振り返り](retro.md) | `braindex retro`（訂正率の計測） |
| [ニュース](news.md) | `braindex news`（fetch・profile・apply・suggest。途中で止まったときの立て直し） |
| [学習の提案](learn.md) | `braindex learn`（候補・本文照合）・`learn answer`／`learn answers`（候補への回答） |
| [関連するノート](related.md) | `braindex related`（いまの作業に関連するノートを索引と `index/links.tsv` から並べる） |
| [判断・HTML・照合・走査対象](tools.md) | `braindex approvals`・`answer`・`explain`・`verify`・`scope` |
| [定期実行](schedule.md) | `braindex schedule` |
| [設計](design.md) | LLM wiki 型との対応・リポジトリの地図・版の状態 |
| [索引データと Markdown の互換性](catalog-format.md) | 索引の共通型と読み取り（開発者向け） |

## 安定度

「安定」は既定値・出力の形・終了コードを変えない前提のもの。「試用中」は既定値・辞書・閾値を試用後に見直す前提のもので
（決定 2026-09-03「v1 にしない」）、版が上がると既定値や出力の文言が変わりうる。どちらも索引の決定性（同じ入力からバイト一致）と
本文を外へ送らないことは変わらない。

| 段 | コマンド |
|---|---|
| 安定 | 索引の生成（`braindex`）・`init`・`update`・`review`・`approvals`・`answer` |
| 試用中 | `retro`・`news`・`learn`・`schedule`・`verify`・`scope`・`search`・`diagnose`・`lint -kind note` |

`lint`（ISSUE の形の検査）は規約のテンプレ（`-add conventions`）と対で、規約の形が変われば指摘も変わる。

## 終了コード（共通）

終了コードは共通で **0 成功／1 失敗（結果を書かない）／2 警告つき完了（結果は書いたが、飛ばしたものや取りこぼしがある）**。
「2 なら結果は使える」が全コマンドで成り立つので、定期実行から一律に判定できる。

| 2 を返す場面 | コマンド |
|---|---|
| 読めないものを飛ばした | 索引の生成（読めないファイル・ディレクトリ。本文の変更の記録を読めない・書けない）・`review`・`retro`（読めないログ）・`news fetch`（フィード・選別 JSON・統計）・`news profile`／`learn`（索引やセッションの置き場が無い・本文の変更の記録を読めない） |
| 確認できなかった範囲がある | `search`（読めなかった範囲は結果に列挙）・`learn`（本文照合が確認不能。本文照合を飛ばした・回答ファイルを読めないときも） |
| 読めなかった範囲・警告・索引の欠落や不一致がある。設定の値の誤りで走査できない | `diagnose` |
| 指摘・不一致があった | `lint`（指摘あり）・`verify`（NOT FOUND あり）・`approvals status`（記載漏れ・未反映の回答）・`approvals apply`（反映できなかった項目）・`approvals wait`（届いた回答が未反映のまま） |
| 利用者の編集を残して `.new` を置いた | `update` |
| 突き合わせる相手がいない | `scope`（対象が 2 件未満） |
| 前回の判断の節が空のまま | `review` |
| 残留したロックを外した・別の日の未完了が残っている | `news fetch`・`news apply` |

3 を使うのは 3 つだけ: `retro check`（閾値超え）と、`approvals serve`・`approvals wait`（時間切れ）。
フラグの要約は `braindex -h`、各コマンドは `braindex <コマンド> -h`。
