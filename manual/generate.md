# braindex — 索引の生成

[← README](../README.md) ／ [手引きの目次](README.md)

索引はコピーではなくパスを持つ。生成・参照・レビューはこう回る。

```text
生成:  各リポの docs/notes/**/*.md ＋ docs/decisions.md
         └─▶ scan（走査）─▶ extract（日付・種別・タイトル・要旨）─▶ render（1 行に整形）
               └─▶ hub の index/catalog.md（LF 固定。同じ入力なら常にバイト一致）

引く:  人 / エージェント ─▶ catalog.md を grep ─▶ ヒット行のパスの実ファイルを読む
         索引はコピーを持たない。要旨 80 字は手がかりであって、内容の代わりではない

回す:  catalog.md をコミット ─▶ 再生成すると git diff ＝ 前回からの差分
         └─▶ braindex review が索引の増減・差分ファイル・放置 TODO を集計し、週次レビューの下書きへ
```

## 設定 `braindex.json`

`braindex.json`（カレントディレクトリ。`-config` で変更可。無くてもよく、そのときは `-root` が必須）:

| キー | 意味 |
|---|---|
| `root` | 走査のルート。直下の各ディレクトリを 1 リポとみなす。相対パスは設定ファイルのディレクトリ基準。`-root` が無ければ必須 |
| `notes_dirs` | 各リポのノート置き場。既定 `["docs/notes"]`。`["wiki"]` や、移行中の `["docs/notes", "wiki"]` も可。種別ラベルは末尾セグメント。リポ内の相対パスに限る（`..` を含むパスと絶対パスは設定の誤りとして終了コード 1） |
| `extra` | 規約外の置き場を個別に足す配列。各要素は `repo`（root 直下のリポ名）・`path`（リポ内の起点。`"."` はリポ直下。`notes_dirs` と同じくリポ内の相対パスに限る）・`recursive`（`true` でサブディレクトリも走査）・`kind`（種別ラベル）・`exclude`（グロブの配列。`/` を含むパターンは起点からの相対パス、含まなければファイル名とディレクトリ名に掛ける。ディレクトリに当たるとその枝ごと除外する。大文字小文字は区別する） |
| `review` | 週次レビュー（`braindex review`・[review-lint.md](review-lint.md)）の節。`dir`（記録の置き場。既定 `work/review`）・`since_days`（前回の記録が無いときに遡る日数。既定 14）・`stale_todo_weeks`（TODO を放置とみなす週数。既定 4）・`archive_months`（何か月より前をアーカイブ候補にするか。既定 6）。省略可 |
| `retro` | 振り返り（`braindex retro`）の節。`sessions_dir`・`window_days`・`threshold`・`position_bins`・`dictionary`・`dictionary_extra`。省略可。詳細は [retro.md](retro.md) |
| `news` | ニュースサジェスト（`braindex news`・[news.md](news.md)）の節。`dir`（既定 `news`）・`feeds`（既定 `news/feeds.json`）・`seen_days`（既定 90）・`cap_per_layer`（層ごとの 1 フィード表示上限。既定 `{"daily": 15, "weekly": 25}`・表に無い層は 20）・`profile_days`（既定 14）・`sessions_dir`・`show_min_score`（主要表示にする関心度の下限。0〜3・既定 2。**0 は全件を主要表示**で、省略とは別の意味）・`llm`（`off`（既定）か `claude-cli`。LLM 補助の opt-in）・`llm_model`・`llm_timeout_sec`（既定 120）。省略可。範囲外の値は設定の誤りとしてエラー |
| `approvals` | 判断待ちのフォーム（`braindex approvals`・[tools.md](tools.md)）の節。`file`（既定 `work/APPROVALS.md`）・`decisions`（既定 `docs/decisions.md`）・`timeout_sec`（既定 0＝無期限）。省略可 |
| `schedule` | 定期実行（`braindex schedule`・[schedule.md](schedule.md)）の節。`jobs` の配列（`name`・`args`・`when`）。省略すると既定の 2 本。省略可 |

未知のキーはエラーにする（`notes_dir` のような打ち間違いを無言で無視しない）。

例（`alpha` リポの `research/` を種別 `research` で載せ、README と下書きを除く）:

```json
{
  "root": "..",
  "notes_dirs": ["docs/notes"],
  "extra": [
    { "repo": "alpha", "path": "research", "recursive": true, "kind": "research", "exclude": ["README.md", "*.draft.md"] }
  ]
}
```

フラグ: `-config` `-root` `-out`（既定は設定ファイルと同じディレクトリの `index/catalog.md`）`-date YYYY-MM-DD`（生成日の固定。テスト・CI 用）。
終了コード: 0 成功／1 失敗（フラグの誤り・設定・root が読めない。索引は書かない）／2 警告つき完了（読めないファイルや存在しない `extra` を stderr に出して飛ばし、索引は書く）。

## 索引の各行の決め方

- リポ: `root` 直下の各ディレクトリ（`.` で始まるものは除く）。root 相対パスに `archive` セグメントを含むファイルは除外
- 種別: ノート置き場の直下はその末尾セグメント（既定 `notes`）、サブディレクトリ配下は `notes/<サブディレクトリ>`、`docs/decisions.md` は `decisions`、`extra` は指定した `kind`（`recursive` ならサブディレクトリ配下は `kind/<サブディレクトリ>`）
- 日付: ファイル名の `YYYYMMDD` か `YYYY-MM-DD` → 本文先頭 10 行の ISO 日付か `YYYY年M月D日` → 無ければ空欄（mtime には頼らない）
- タイトル: 最初の `# ` 行。無ければファイル名（`.md` を除く）
- 要旨: タイトル直後の、見出し・表・コードフェンス・日付だけの行（`記録日:` `日付:` `更新日:` `作成日:` `Date:`）でない最初の本文行を 80 字で切る
- `decisions.md` だけ別扱い: 日付は全文の `記録日:` のうち最も新しいもの（追記式なので「最後に何か決めた日」）、タイトルは `<H1>（N 件）`、要旨は末尾の H2 見出し 1 件
- 並び: リポ名昇順 → 日付降順 → パス昇順。改行は LF 固定。入力の BOM と CRLF は正規化する
