# related — いまの作業に関連するノート

索引のタイトル・要旨・ファイル名に含まれる語と、ノート間のつながりから候補を並べます。
意味検索ではありません。語が違う同じ話題は引けません。LLM・形態素解析・埋め込み・DB は使わず、ネットワークにも出ません。

```sh
braindex related -config ../hub/braindex.json -repo alpha
braindex related -config ../hub/braindex.json -repo alpha -issue work/ISSUE-example.md links 索引
braindex related -config ../hub/braindex.json -repo alpha -session session-id -json
```

## 入力

入口は次の 4 つです。`git diff` は材料にしません。

| 入力 | 指定方法と扱い |
| --- | --- |
| リポ | `-repo`。省略時はカレントを設定の root 相対にして、`repo_depth` に従って決めます。root の外では指定が必要です。リポ名の各セグメントは語から除きます。 |
| ISSUE | `-issue work/ISSUE-*.md` で 1 枚。指定パスはカレント基準です。省略時はそのリポの `work/ISSUE-*.md` 全部を名前順に読みます。無ければ警告して続けます。 |
| セッション | `-session ID` で 1 つ。省略時はそのリポのディレクトリ配下で、直近 `-days` 日（既定 7）に更新されたセッションを使います。人間の発話だけを読み、定型と判定された発話は除きます。 |
| 引数の語 | フラグの後ろに追加する語です。 |

設定は `-config`（既定はカレントの `braindex.json`）で指定します。設定が無ければ失敗します。
索引は設定と同じディレクトリの `index/catalog.md`、つながりはその隣の `links.tsv` を読みます。
セッションの置き場は `retro.sessions_dir`、未設定なら `~/.claude/projects` です。置き場が無ければ警告して続けます。
`-session` 指定時は日数とリポの絞り込みを外し、指定 ID の発話だけを材料にします。

ISSUE・セッション・引数は、それぞれ URL を除いてから `interest.Words` と同じ規則で語を切り出します。
ISSUE と引数の重みは 2、セッションは 1。同じ語が複数の出典にあれば最大値を使い、繰り返しの回数では増やしません。

## 点数と並び順

- 語の点：タイトルに含む語の重みの和 × 2 ＋ 要旨に含む語の重みの和 ＋ ファイル名に含む語の重みの和。ASCII の大小を無視した部分一致です。
- つながりの点：語の点が正のノートと、方向を問わず 1 段でつながる辺ごとに加点します。link・wiki は 1、mention は 0.5、合計の上限は 3。2 段先はたどりません。`-no-mention` で mention を除けます。
- 合計点の降順。同点なら日付の降順、さらに同じならパスの昇順です。日付を点には足しません。合計 0 の行は出しません。

索引のリポ見出しが一致する「このリポ」と、それ以外の「他のリポ」に分けます。
`-limit` は各節の上限（既定 10）。`-days` と `-limit` は 1 以上を指定します。
入力の語は重みの降順・語の昇順で上位 20 語まで表示し、出典ごとの語数も示します。
候補には語の重みと一致した欄、つながりの点を表示します。
links.tsv が無ければ「つながり: 無し（braindex で生成する）」と表示し、語の点だけで並べます。

`-json` は `repo`, `terms`, `this_repo`, `other_repos`, `warnings` を持つオブジェクトです。
`terms` は `word`, `weight`, `sources`（ISSUE・セッション・引数）を持ちます。
各候補は `path`, `repo`, `title`, `date`, `score`, `words`, `links` を持ちます。
`words` の `where` は一致した欄（title・summary・path）の配列です。
`links` の link・wiki・mention は加点に使った辺の本数で、点の上限 3 を適用する前の内訳です。

## 終了コード

| コード | 意味 |
| --- | --- |
| 0 | 成功 |
| 1 | 設定・索引・フラグが不正、入力を読めない、links.tsv が壊れているなどの失敗 |
| 2 | ISSUE・セッションの置き場・links.tsv が無い、語が 0 個、セッション読み取りの警告など。残った材料で出力します。 |
