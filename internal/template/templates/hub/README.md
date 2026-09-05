# 知識ハブ

複数リポを横断する索引（`index/catalog.md`）と、どのリポにも属さない知識を置くリポジトリ。
`braindex init` で展開した。ここにあるファイルは自由に書き換えてよい（`braindex init` は既存ファイルを上書きしない）。
この README は全機能の説明を持つ。まだ足していない機能（`docs/`・`work/`・スキル・`braindex.json` の節が無いもの）は
`braindex init -add <機能>` で足す（一覧は `braindex init -list`。`conventions`・`review`・`retro`・`news`・`schedule`・`all`）。

## 日々の使い方

- **引く**: `grep -i <語> index/catalog.md` で当たりを付け、ヒット行のパスの実ファイルを読む。
  要旨の列だけで答えない。要旨は 80 字の手がかりであって、内容の代わりではない。
- **索引を再生成する**: このディレクトリで `braindex` を実行する。走査対象は `braindex.json` で決まる
  （`root` は各リポの親ディレクトリ）。`index/catalog.md` をコミットすると、その `git diff` が前回からの差分になる。
- **知識はそれが属するリポに書く**: 調べて分かった事実はそのリポの `docs/notes/`、決めたことは `docs/decisions.md`。
  どのリポにも属さない知識だけを、この hub の `docs/notes/` に書く。

## 週次レビュー

週に 1 回、このディレクトリで `braindex review` を実行する。`work/review/YYYY-MM-DD.md` に下書きができ、
索引の増減・リポ別の差分ファイル・放置 TODO・アーカイブ候補は埋まっている。
スキル `braindex-review`（`.claude/skills/braindex-review/SKILL.md`）の手順で判断の節（ダイジェスト・アーカイブ・次アクション）を埋め、
`braindex` で索引を再生成して、下書きと一緒にコミットする。最新のファイル名が前回レビューの日付を兼ねる。
閾値は `braindex.json` の `review` 節。

下書きの作成だけなら定期実行に任せられる（判断は人）。このディレクトリで `braindex schedule install` と打つと、
`braindex.json` の `schedule` 節（`braindex init -add schedule` が足す）に書いたジョブが OS のスケジューラ（Windows は schtasks、macOS・Linux は crontab）に登録される。
ジョブは足してある機能の分だけ入る: `review` があれば週次レビュー（月 09:00）、`retro` があれば訂正率の確認（月 09:05）。
`schedule` の後に足した機能のジョブは `braindex update` が足す。既にある下書きは上書きしない。

```sh
braindex schedule print      # 登録に使うコマンドを出すだけ（何も変えない）
braindex schedule install    # 登録する（再実行しても二重にならない）
braindex schedule list       # 設定のジョブと、OS 側に登録されているか
braindex schedule uninstall  # この hub の登録を消す
```

hub と `braindex` 自身の絶対パスを埋め込むので、定期実行の環境の PATH には依存しない。
**この hub か `braindex` を別の場所へ移したら `braindex schedule install` をやり直す。**

## 振り返り（訂正率）

`braindex retro check` が、直近 14 日のセッションログ（Claude Code の `~/.claude/projects`）で「エージェントの振る舞いへの訂正」の割合を
閾値（既定 8%）と比べ、超えていれば 1 行と終了コード 3 で知らせる。判定は辞書照合の決定論で、発話の本文はどこにも書かず送らない。
超えたら、スキル `retro`（`.claude/skills/retro/SKILL.md`）の手順で `braindex retro extract` のダイジェスト（OS の一時ディレクトリに出る）を読み、
所見と規約への反映案を `docs/notes/retro-YYYY-MM-DD.md` に残す。数値は `braindex retro stats -window-days 14 -by project,week,position` の表を貼る（窓を付けないと全期間の集計になる）。

組み込みは 2 通り。Claude Code の hook（`SessionStart`）に `braindex retro check -quiet || true` を置けば、超えたときだけ 1 行がセッションに入る。
定期実行なら `braindex schedule install`（`-add schedule` が足す `retro` ジョブが週 1 回 `braindex retro check` を回す）。窓・閾値・辞書は `braindex.json` の `retro` 節。

## 地図

| 場所 | 何が入るか |
|---|---|
| `index/catalog.md` | 索引。`braindex` が生成する。手で編集しない |
| `braindex.json` | 走査の設定: `root`・`notes_dirs`・`extra`（段 0）。週次レビューの設定（記録の置き場と閾値）: `review`。振り返りの設定（窓・閾値・辞書）: `retro`。判断待ちフォームの置き場: `approvals`（`-add conventions`）。ニュース: `news`。定期実行のジョブ（名前・引数・時刻）: `schedule`。節は `braindex init -add` が足す |
| `docs/` | 蓄積するもの: `overview.md`・`glossary.md`・`decisions.md`・`notes/`・`conventions.md`（`-add conventions`） |
| `work/` | 揮発するもの: `APPROVALS.md`（判断待ち）・`TODO.md`（`-add conventions`）・`review/`（週次レビューの記録。`-add review`） |
| `.claude/skills/` | Claude Code のスキル: `record-lint`（ノート保存前の曖昧さ検査）・`contradiction-scan`（横断の矛盾検査）・`research-distill`（検証優先の調査。`braindex verify` で裏取り、`braindex answer` で HTML 化）は `-add conventions`。`braindex-review`（週次レビューの判断）は `-add review`。`retro`（振り返り）は `-add retro` |
