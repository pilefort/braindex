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

## 決めたこと

記録日・理由・根拠は `docs/decisions.md` にあった当時の記録のまま。

### 定期実行の登録は `braindex schedule` が行い、`braindex init` はやらない

記録日: 2026-09-03
理由: init は「既存ファイルを上書きしない・再実行しても安全なファイル展開」で通しており、OS のスケジューラへの書き込み(権限・後始末・削除の手間)は性質が違う。登録を別コマンドに分けると、init の再実行が OS の状態を変えないまま保てる。README にコマンド例を書くだけの旧運用は、利用者が手で打つ手間と、hub や braindex のパスを自分で埋める間違いを残していたので、CLI が絶対パスを埋めて登録する形に進めた。OS 通知(toast)を同梱しない決定(2026-09-03)は据え置く — 外したのは通知と原型の `schedule/` 一式であって、登録そのものではない。
根拠: 会話 2026-09-03(ユーザー「定期実行のスクリプト登録って、init 時にできないんだっけ」→「実際に登録して欲しい」)・当時の `work/SPEC-schedule.md`（2026-09-03・削除済み）・PR #5（控え: pr/5.md）0（控え: pr/50.md）（実装 `internal/schedule/`・`cmd/braindex/cmd_schedule.go`）

### 定期実行の時刻は `daily:HH:MM` と `weekly:<曜日>:HH:MM` の 2 形だけ受け、cron 式は受けない

記録日: 2026-09-03
理由: cron 式(`*/15 * * * *` 等)は Windows の schtasks に一般変換できず、受け付けると「Linux でだけ動く設定」が書けてしまう。2 形に絞れば cron の 5 フィールドにも schtasks の `/SC` にも決定的に落ちる。毎分・15 分おきのような細かい周期は braindex の用途(週次レビュー・訂正率の点検・毎朝のニュース取得)に要らない。
根拠: `internal/schedule/when.go`・判定表テスト `when_test.go`(cron 式を渡すと「cron 式は受けない」でエラー)

### 定期実行に登録できるのは braindex 自身のサブコマンドだけ

記録日: 2026-09-03
理由: 任意のコマンド文字列を受けると、設定ファイル `braindex.json` が「OS のスケジューラに任意コードを登録する口」になる。`args` を配列にして先頭要素を登録済みサブコマンド名と照合すれば、生成する行は braindex の実行ファイルと引数だけで閉じ、シェルの分割規則も設定に持ち込まずに済む。
根拠: `internal/schedule/settings.go` の `Validate`・`cmd_schedule_test.go` の「任意のコマンドは登録しない」

### `crontab -l` を読めなければ止める。ただし「まだ crontab が無い」失敗だけは空として続ける

記録日: 2026-09-03（同日に上書き。改訂前の決定は下に残す）
理由: 全消しの防止（読めないまま書き戻さない）は変えず、`crontab` を一度も作っていない利用者が
`schedule install` を手作業なしで使えるようにする。改訂前の決定が却下理由に挙げた「文言が実装と言語設定に依存する」は、
`crontab -l` を `LC_ALL=C` で実行して英語に固定することで潰せる（macOS の `crontab: no crontab for <user>` も
Linux cronie の `no crontab for <user>` も `no crontab` を含む）。文言の違う実装（未確認: busybox）ではエラー側に倒れるだけで、
全消しにはならない。却下: 改訂前の B（install コマンドが「まず手で crontab を作れ」と要求するのは筋が悪い）／現状維持。
根拠: 2026-09-03 に mac-office（Darwin 24.6.0）で実測（crontab 未作成の `crontab -l` は終了コード 1・`crontab: no crontab for <user>`）／
実装は `internal/schedule.IsNoCrontab`・`cmd/braindex.readCrontab`（判定表テストつき）／改訂の合意は会話 2026-09-03（ユーザー判断）

#### 改訂前（2026-09-03・同日に上書き）: 読めなければ常に止める

理由: 失敗を「空」と畳むと、読みが失敗しつつ書きが通る状況で利用者の crontab を全消しする。文言で「no crontab」だけを見分ける案は、文言が実装と言語設定に依存するので採らなかった。**代償として、crontab を一度も作っていない環境では `crontab -e` で空の crontab を作る手間が要る**（エラー文でその手順を案内する）。却下: 出力の文言で見分ける／現状維持。
根拠: 会話 2026-09-03（ユーザー判断・承認待ちフォームの回答）。実装は PR #5（控え: pr/5.md）9（控え: pr/59.md）。指摘の出どころは当時の PR #5（控え: pr/5.md）0（控え: pr/50.md） のレビュー記録 3 節（2026-09-03・除去済み。`readCrontab` の読みで、実測はしていない）
上書きの経緯: 実装後に「初回の利用者が手作業を要求される」ことが具体的に見えたため、実測（macOS の文言）と `LC_ALL=C` による対処を添えてユーザーに再提示し、A'（上）に変更した
