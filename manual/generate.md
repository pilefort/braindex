# braindex — 索引の生成

[← README](../README.md) ／ [手引きの目次](README.md)

索引はコピーではなくパスを持つ。生成・参照・レビューはこう回る。

```text
生成:  各リポの docs/notes/**/*.md ＋ docs/decisions.md
         └─▶ scan（走査）─▶ extract（日付・種別・タイトル・要旨）─▶ render（1 行に整形）
               └─▶ hub の index/catalog.md（LF 固定。同じ入力なら常にバイト一致）
               └─▶ hub の index/changes.json（本文のハッシュと観測日。索引の行が変わらない本文だけの更新を見分ける）

引く:  人 / エージェント ─▶ catalog.md を grep ─▶ ヒット行のパスの実ファイルを読む
         索引はコピーを持たない。要旨 80 字は手がかりであって、内容の代わりではない

回す:  catalog.md をコミット ─▶ 再生成すると git diff ＝ 前回からの差分
         └─▶ braindex review が索引の増減・差分ファイル・放置 TODO を集計し、週次レビューの下書きへ
```

## 設定 `braindex.json`

`braindex.json`（カレントディレクトリ。`-config` で変更可。無くてもよく、そのときは `-root` が必須）:

| キー | 意味 |
|---|---|
| `root` | 走査のルート。直下の各ディレクトリを 1 リポとみなす（`repo_depth` で段数を変えられる）。相対パスは設定ファイルのディレクトリ基準。`-root` が無ければ必須 |
| `repo_depth` | root から何段下のディレクトリをリポとみなすか。既定 1（root 直下）。`2` なら `root/<group>/<name>` がリポで、リポ名（索引の H2 見出し・`extra.repo`・`search -repo` など）は `group/name`。段数は root 全体で 1 つ（1 段と 2 段の配置は混ぜられない。2 段のときは 1 段目の `docs/notes` は見ない）。0 または省略で 1、負の値は設定の誤り。列挙できなかった group は「読めなかった範囲」として索引の先頭に残る |
| `notes_dirs` | 各リポのノート置き場。既定 `["docs/notes"]`。`["wiki"]` や、移行中の `["docs/notes", "wiki"]` も可。種別ラベルは末尾セグメント。リポ内の相対パスに限る（`..` を含むパスと絶対パスは設定の誤りとして終了コード 1） |
| `extra` | 規約外の置き場を個別に足す配列。各要素は `repo`（リポ名。`repo_depth` が 2 なら `group/name`）・`path`（リポ内の起点。`"."` はリポ直下。`notes_dirs` と同じくリポ内の相対パスに限る）・`recursive`（`true` でサブディレクトリも走査）・`kind`（種別ラベル）・`exclude`（グロブの配列。`/` を含むパターンは起点からの相対パス、含まなければファイル名とディレクトリ名に掛ける。ディレクトリに当たるとその枝ごと除外する。大文字小文字は区別する） |
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

例（`~/projects/<group>/<name>` のように 1 段はさんで整理している root。索引の見出しは `## work/alpha` になる）:

```json
{
  "root": "..",
  "repo_depth": 2,
  "extra": [
    { "repo": "work/alpha", "path": "research", "recursive": true, "kind": "research" }
  ]
}
```

フラグ: `-config` `-root` `-out`（既定は設定ファイルと同じディレクトリの `index/catalog.md`）`-date YYYY-MM-DD`（生成日の固定。テスト・CI 用）`-version`（入っている版を 1 行出して終わる）。
終了コード: 0 成功／1 失敗（フラグの誤り・設定・root が読めない。索引は書かない）／2 警告つき完了（読めないファイルや存在しない `extra` を stderr に出して飛ばし、索引は書く。本文の変更の記録を読めない・書けないときも同じ）。

## 索引の各行の決め方

- リポ: `root` の `repo_depth` 段下の各ディレクトリ（既定 1 で直下。どの段でも `.` で始まるものは除く）。リポ名は root 相対のスラッシュ区切り（`alpha`・2 段なら `work/alpha`）。root 相対パスに `archive` セグメントを含むファイルは除外
- 種別: ノート置き場の直下はその末尾セグメント（既定 `notes`）、サブディレクトリ配下は `notes/<サブディレクトリ>`、`docs/decisions.md` は `decisions`、`extra` は指定した `kind`（`recursive` ならサブディレクトリ配下は `kind/<サブディレクトリ>`）
- 日付: ファイル名の `YYYYMMDD` か `YYYY-MM-DD` → 本文先頭 10 行の ISO 日付か `YYYY年M月D日` → 無ければ空欄（mtime には頼らない）
- タイトル: 最初の `# ` 行。無ければファイル名（`.md` を除く）
- 要旨: タイトル直後の、見出し・表・コードフェンス・日付だけの行（`記録日:` `日付:` `更新日:` `作成日:` `Date:`）でない最初の本文行を 80 字で切る
- `decisions.md` だけ別扱い: 日付は全文の `記録日:` のうち最も新しいもの（追記式なので「最後に何か決めた日」）、タイトルは `<H1>（N 件）`、要旨は末尾の H2 見出し 1 件
- 並び: リポ名昇順 → 日付降順 → パス昇順。改行は LF 固定。入力の BOM と CRLF は正規化する

## 本文の変更の記録 `index/changes.json`

索引の行は日付・種別・タイトル・要旨・パスだけなので、本文の後半だけを直した更新では行が変わらず、`git diff` にも出ない。
そこで生成のたびに、本文の内容ハッシュと「その内容を最初に見た日」を索引と同じディレクトリの `changes.json` に書き、
次の生成でハッシュが違えば「本文が変わった」と分かるようにする。索引の日付（記録日）は変えない——いつ書いた知識か
という情報を失わないため。mtime は使わない（git が保存しないので clone ごとに変わる）。

```json
{
  "version": 1,
  "notes": [
    { "path": "alpha/docs/notes/a.md", "hash": "<sha256>" },
    { "path": "alpha/docs/notes/b.md", "hash": "<sha256>", "observed": "2026-09-07" },
    { "path": "beta/docs/notes/old.md", "hash": "<sha256>", "observed": "2026-08-01", "missing": "2026-09-01" }
  ]
}
```

- `hash`: BOM を除き、CRLF・CR を LF に揃えた本文の SHA-256（16 進）。checkout の改行変換や BOM の付け外しは変更に数えない。
  内容が変わったことを見分ける値であって、意味のある変更かどうかは判定しない
- `observed`: その内容を braindex が最初に見た日（生成日。`-date` で固定できる）。無いのは「記録を始めた時点で既にあった」＝
  いつ変わったかは不明。初回の生成では全件が観測日なしになり、「今日変わった」とは記録しない
- `missing`: 走査で見当たらなくなった日。同じ内容で戻れば消え、`observed` は元のまま。違う内容で戻れば `observed` が戻った日になる
- 読めなかった範囲（索引先頭の「走査:」の行）にある記録は前回のまま据え置く。読めなかっただけで、消えたとも変わったとも言わない
- 並びは `path` 昇順・LF・インデント 2。同じ材料と同じ生成日からは同じバイト列になるので、`catalog.md` と一緒にコミットしてよい
  （記録日は動かないまま、本文の変更が diff に出る）
- 壊れていて読めない（手で編集した・別の版が書いた）ときは、警告して記録を触らず索引だけ書く（終了コード 2）。
  直すか、ファイルごと消せば次の生成が初回として観測をやり直す
- 書けなかったときも警告にとどめる（索引は書けている。観測は次の生成で追いつく）。同じ hub で生成が重なると、Windows では
  置き換えの瞬間に片方が拒まれてこの警告になることがある。記録が半端な内容になることはない
- stdout の 1 行: 初回は `本文の観測を開始: N 件を記録(いつ変わったかは不明)`、以後は `本文の変更: 変更 a・新規 b・見当たらない c`
  （再出現があれば `・再出現 d`、据え置きがあれば `(読めなかった範囲の e 件は前回のまま)`）か `本文の変更: なし`

関心プロファイル（`braindex news profile`）がこの記録を読んで、記録日が古くても `observed` が窓の中にあるノートを数えるようになるのは、後続の変更から。
