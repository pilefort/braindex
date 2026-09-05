# braindex schedule — 定期実行の登録

[← README](../README.md) ／ [手引きの目次](README.md)

判断は人が行うので、自動化するのは下書きの作成だけ。設定の `schedule` 節（`braindex init` が既定で足す。ジョブは足してある
review・retro・news の分）に書いたジョブを、hub で `braindex schedule install` と打つと OS のスケジューラ（Windows は schtasks、macOS・Linux は crontab）に登録できる。

```sh
braindex schedule print      # 登録に使うコマンドを出すだけ（何も変えない）
braindex schedule install    # 登録する（再実行しても二重にならない）
braindex schedule list       # 設定のジョブと、OS 側に登録されているか
braindex schedule uninstall  # この hub の登録を消す
```

`braindex init` が置く節は、訂正率の確認（月 09:05）・ニュースの取得（毎日 07:30・`-layer daily -no-open`）と、review を足していれば週次レビュー（月 09:00）。
節を省略すると、週次レビューと訂正率の確認の 2 本になる。hub と braindex 自身の絶対パスを埋め込むので、
定期実行の環境の PATH には依存しない（どちらかを移したら登録し直す）。
自分で cron や schtasks に書きたいときは `braindex schedule print` の出力をそのまま使える。
同じ日に 2 回動いても、既にある下書きは上書きしない。

## 設定と登録の仕組み

設定 `braindex.json` の `schedule` 節に書いたジョブを、この OS のスケジューラに登録する。
Windows は `schtasks`（`/F` で上書きするので再実行しても二重にならない。タスク名は `braindex-<hub のフォルダ名>-<ジョブ名>`）、
macOS・Linux は `crontab`（`# BEGIN braindex <hub>` 〜 `# END braindex <hub>` で囲んだブロックだけを書き換え、
ブロックの外の行と別 hub のブロックには触らない）。
**登録できるのは braindex 自身のサブコマンドだけ**で、設定ファイルを任意コード実行の口にしない。

```json
"schedule": {
  "jobs": [
    { "name": "review", "args": ["review"],         "when": "weekly:mon:09:00" },
    { "name": "retro",  "args": ["retro", "check"], "when": "weekly:mon:09:05" }
  ]
}
```

- `name`: 英小文字・数字・ハイフンの 1〜32 文字。タスク名と cron 行の目印になる
- `args`: braindex に渡す引数。`args[0]` は登録済みのサブコマンド名でなければならない。文字列 1 本にしないのは、シェルの分割規則を設定ファイルに持ち込まないため
- `when`: `daily:HH:MM` か `weekly:<曜日>:HH:MM`（曜日は `mon`〜`sun`）の 2 形だけ。cron 式は schtasks に一般変換できない（`*/15` など）ので受けない

節を省略すると上の 2 本になる。hub と braindex 自身の絶対パスを埋め込むので（定期実行の環境は PATH が違う）、
**どちらかを移したら登録し直す**。`braindex init` は自動では登録しない（init は「既存を上書きしないファイル展開」で、OS への副作用は性質が違う）。

サブコマンド: `list`（設定のジョブと OS 側の登録状態）・`print`（登録に使うコマンドを出すだけ）・`install`（登録する）・`uninstall`（消す）。

crontab 側では、`crontab -l` が読めなければ**何もせず終了コード 1** で止まる（読めないまま書き戻すと既にある行を消してしまうため）。
ただし「まだ crontab が無い」ことを示す失敗（出力が `no crontab for <利用者>` の 1 行だけ。BSD cron の `crontab: ` 接頭辞も可）だけは空の crontab として扱うので、
`crontab` を一度も作っていない環境でもそのまま `braindex schedule install` できる。
文言の違う cron 実装ではこの判別が効かず終了コード 1 で止まるので、その場合は `crontab -e` で空の crontab を作ってから実行する。
フラグ: `-config` `-job 名前`（1 本だけを対象にする）`-dry-run`（`install`・`uninstall`。実行せずコマンドを出す）。
終了コード: 0 ／1 フラグ・設定の誤り、またはスケジューラ側が失敗した（登録できていないので失敗）。
