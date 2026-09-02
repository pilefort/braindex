# 知識ハブ

複数リポを横断する索引（`index/catalog.md`）と、どのリポにも属さない知識を置くリポジトリ。
`braindex init` で展開した。ここにあるファイルは自由に書き換えてよい（`braindex init` は既存ファイルを上書きしない）。

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

下書きの作成だけなら定期実行に任せられる（判断は人）。cron なら `0 9 * * 1 cd <この hub> && braindex review`、
Windows のタスクスケジューラなら `schtasks /Create /SC WEEKLY /D MON /ST 09:00 /TN braindex-review /TR "cmd /c cd /d <この hub> && braindex review"`。
既にある下書きは上書きしない。

## 地図

| 場所 | 何が入るか |
|---|---|
| `index/catalog.md` | 索引。`braindex` が生成する。手で編集しない |
| `braindex.json` | 走査の設定: `root`・`notes_dirs`・`extra`。週次レビューの閾値: `review` |
| `docs/` | 蓄積するもの: `overview.md`・`glossary.md`・`decisions.md`・`notes/`・`conventions.md` |
| `work/` | 揮発するもの: `APPROVALS.md`（判断待ち）・`TODO.md`・`review/`（週次レビューの記録） |
