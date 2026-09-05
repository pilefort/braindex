# braindex approvals / answer / verify / scope — 判断・HTML・照合・走査対象

[← README](../README.md) ／ [手引きの目次](README.md)

## braindex approvals — 判断待ちのフォーム

`work/APPROVALS.md` の判断待ち（1 項目 1 判断・5 欄「決めたいこと／なぜ今決めるか／選択肢／私の案／決めないとどうなるか」）を
ブラウザのフォームにして聞き、答えを記録する。受け口は 127.0.0.1 の空きポートだけで、回答を 1 回受けたら終わる（常駐しない）。外へは何も送らない。

```sh
braindex approvals serve -apply   # フォームを開いて回答を待ち、そのまま反映する
braindex approvals status         # 件数・記載漏れ・未反映の回答（書き込みなし）
braindex approvals apply          # 受けた回答を反映する（聞くのと分けたいとき）
```

選んだ項目は `docs/decisions.md` に 3 段（結論 → 理由 → 根拠）で追記して `APPROVALS.md` から消し、保留は項目を残して
「**保留（日付）:**」を付ける。反映した回答 JSON は `.applied.json` に改名するので、2 回反映されない。
フラグ: `-file`（判断待ちのファイル）`-dir`（回答 JSON の置き場。既定は OS の一時ディレクトリの `braindex-approvals`）
`-config`（設定ファイル。既定はカレントの `braindex.json`。無くてもよい）`-timeout 秒`（0 で無期限）`-no-open` `-apply` `-decisions` `-date` `-reply`。

設定（`braindex.json` の `approvals` 節。節ごと省略してよく、設定ファイルが無くても動く）:

```json
{ "approvals": { "file": "work/APPROVALS.md", "decisions": "docs/decisions.md", "timeout_sec": 0 } }
```

- `file` — 判断待ちのファイル。既定 `work/APPROVALS.md`
- `decisions` — 決定の追記先。既定 `docs/decisions.md`
- `timeout_sec` — `serve` が回答を待つ秒数。既定 `0`（無期限）。負の値は設定の誤りとして止める

相対パスは **`braindex.json` のある場所**からの相対で解く（コマンドを打ったカレントからではない）。
優先順位は **フラグ > 設定 > 既定** で、`-file` などを明示したときはフラグが勝つ。
設定ファイルは全体を読むので、`news` など**別の節にタイプミスがあると `approvals` も終了コード 1 で止まる**（他のコマンドと同じ挙動）。
設定ファイルが既定の置き場に無いのは正常で、そのときはフラグと既定だけで動く。`-config` で指定したのに無いときだけ失敗する。
終了コード: `serve` 0 回答あり／3 時間切れ、`apply` 0 反映した・回答なし／2 反映できなかった項目がある、`status` 0 ／2 記載漏れか未反映の回答あり。いずれも 1 は失敗。

## braindex answer — 回答の HTML 化

`braindex answer <md>` は Markdown を自己完結の HTML（外部の JS・CSS を参照しない）にして書き、既定ブラウザで開く。
出力先の既定は一時置き場で、実行のたびに `-ttl-days`（既定 14）より古いものを消す。
**HTML は読むための一時物**なので、残す価値のある内容は `.md` を `docs/notes/` に置いてから渡す（置き場所が寿命を表す）。
リンクの `href` に出すのは `http(s)` と、スキームを持たないもの（相対パス・`#見出し`）だけ。`javascript:` などは文字として残す。

```sh
braindex answer note.md                  # HTML にして開く
braindex answer -no-open note.md         # 書くだけ
braindex answer -out out.html note.md    # 出力先を指定する
braindex answer -dir                     # 一時置き場の場所を表示して終わる
braindex answer -purge                   # 一時置き場の中を今すぐ全部消す
```

フラグ: `-out` `-no-open` `-dir` `-purge` `-ttl-days`（0 で消さない）。フラグは `<md>` より前に置く。終了コード: 0 成功／1 失敗。

## braindex verify — 実在の照合

ノートに書いた GitHub リポ・arXiv 論文・URL・逐語引用を、一次ソースへの GET で照合する（こちらから本文は送らない）。

```sh
braindex verify github pilefort/braindex
braindex verify arxiv 2608.26263
braindex verify url https://go.dev/blog/
braindex verify quote https://example.com/a "引用したい文を二十四字以上そのまま書く"
braindex verify -json github pilefort/braindex   # フラグは種別より前に置く
```

出力は 1 件 1 行（種別・対象・判定・実測のタブ区切り。判定は `FOUND`／`NOT FOUND`／`ERROR`。`-json` で配列）。
`github` は実在・スター数・作成日、`arxiv` は ID の実在（実測の列にタイトル）、`url` は HTTP 200 か、`quote` は本文（タグ除去・空白正規化）に引用が実在するか。
`quote` は空白の揺れだけ許し、24 字未満の引用は ERROR になる。`GITHUB_TOKEN` があれば GitHub API の認証に使う（任意・レート制限対策）。
終了コード: 0 全件 FOUND／2 NOT FOUND あり／1 失敗（ERROR あり・引数の誤り）。

## braindex scope — 矛盾検査の走査対象

複数ノートをまたぐ相互矛盾・陳腐化を探すとき、索引から走査対象を列挙・絞り込み・chunk 分割して出す。**矛盾の判定はしない**
（chunk ごとに実ファイルを全文読み比べ、反証で偽陽性を落とす手順は hub に入るスキル `contradiction-scan`）。

```sh
braindex scope -topic 長さ         # タイトル・要旨・パスに語を含む行(大小無視)
braindex scope -repo alpha         # その見出し(リポ名)の行だけ
braindex scope -full -size 20      # 全件を 20 件ずつの chunk に分ける
braindex scope -dir docs/notes     # 索引を使わず、ディレクトリ配下の *.md を列挙する
```

出力のパスは、どちらのモードでもそのまま開ける形で出る——索引を使うときは索引の行と同じ `root` 相対、
`-dir` のときは渡したディレクトリと結合した形。`-dir` の列挙は `archive` セグメントと `.` で始まるディレクトリの
配下を対象にしない（起点として直接渡したときだけは中を見る）。`archive` の扱いは索引と同じで、
`.` で始まるディレクトリは索引より広く除く（索引が `.` を見るのは `root` 直下のリポ名だけ）。
フラグ: `-topic` `-repo` `-dir` `-full` `-size N`（既定 12）`-json`（`mode`・`n_entries`・`chunks`）`-catalog` `-config`。
終了コード: 0 ／1 失敗／2 対象が 2 件未満（突き合わせる相手がいない）。
