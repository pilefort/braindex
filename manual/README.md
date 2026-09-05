# braindex の手引き

[← README](../README.md)

コマンドごとの詳しい説明。入口と段階的な取り込みは README にある。

| ページ | コマンド |
|---|---|
| [索引の生成](generate.md) | `braindex`（設定 `braindex.json` の全キー・索引の各行の決め方） |
| [骨格の展開と追従](init-update.md) | `braindex init`・`braindex update`・各リポの骨格・エージェントに横断検索させる設定 |
| [週次レビューと検査](review-lint.md) | `braindex review`・`braindex lint` |
| [振り返り](retro.md) | `braindex retro`（訂正率の計測） |
| [ニュース](news.md) | `braindex news`（fetch・profile・apply・suggest） |
| [学習の提案](learn.md) | `braindex learn` |
| [判断・HTML・照合・走査対象](tools.md) | `braindex approvals`・`answer`・`verify`・`scope` |
| [定期実行](schedule.md) | `braindex schedule` |
| [設計](design.md) | LLM wiki 型との対応・リポジトリの地図・版の状態 |

## 終了コード（共通）

終了コードは共通で **0 成功／1 失敗（結果を書かない）／2 警告つき完了（結果は書いたが、飛ばしたものや取りこぼしがある）**。
「2 なら結果は使える」が全コマンドで成り立つので、定期実行から一律に判定できる。

| 2 を返す場面 | コマンド |
|---|---|
| 読めないものを飛ばした | 索引の生成・`review`・`news fetch`（フィード・選別 JSON・統計）・`news profile` |
| 指摘・不一致があった | `lint`（指摘あり）・`verify`（NOT FOUND あり）・`approvals status`（記載漏れ・未反映の回答）・`approvals apply`（反映できなかった項目） |
| 利用者の編集を残して `.new` を置いた | `update` |
| 突き合わせる相手がいない | `scope`（対象が 2 件未満） |

3 を使うのは 2 つだけ: `retro check`（閾値超え）と `approvals serve`（時間切れ）。
フラグの要約は `braindex -h`、各コマンドは `braindex <コマンド> -h`。
