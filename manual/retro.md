# braindex retro — 訂正率の計測

[← README](../README.md) ／ [手引きの目次](README.md)

Claude Code のセッションログ（既定 `~/.claude/projects/<slug>/*.jsonl`）から「人間の発話のうち、エージェントの振る舞いへの訂正の割合」（訂正率）を
規則ベース（LLM を使わず規則と閾値だけ）で測り、閾値を超えたら振り返り（レトロスペクティブ）を促す。計測は CLI、振り返り本体の判断は人か、hub に入るスキル `retro`。

**`check` が鳴る条件は 2 つ重なっている**（2026-09-06 変更）: 直近の窓の訂正率が閾値（既定 8%）を超え、**かつ**その直前の
`baseline_weeks` 週（既定 8 週）の水準からも上振れていること。上振れの判定は「基準の率 + 2SE」で、SE は直近の窓の発話数で計る
二項分布の標準誤差。閾値だけだと、その人の平常運転が閾値の上にある間は毎回鳴り続けて合図の意味が消える。閾値は「床」として残す
（決定 2026-09-03 の 0.08 は覆していない）。基準期間の発話が 50 件に満たなければ材料不足として閾値だけで判定する。

出力は 3 つの数と判定を並べた 1 行:

```
braindex retro check: 直近 14 日の訂正率 9.1%(発話 230・訂正 21) / 基準 7.9%(発話 1204・8 週) / 閾値 8.0% → 閾値は超えたが基準と同水準(鳴らさない)
```
発話の本文はどこにも書かず送らない（リポに残るのは数値と所見だけ）。

```sh
braindex retro stats [-since YYYY-MM-DD | -window-days N] [-by project,week,position]   # 発話数・訂正数・率の表
braindex retro check [-window-days N] [-threshold 0.1] [-quiet]                          # 窓の率を閾値と比べて 1 行。超えたら終了コード 3
braindex retro extract [-since YYYY-MM-DD | -window-days N] [-out DIR]                  # セッションごとの md ダイジェストと index.tsv を一時ディレクトリへ
```

- 分母（人間の発話）: `type: user` で本文がある行から、サブエージェント（`isSidechain`）・tool_result だけの行・スラッシュコマンド・継続要約・中断・
  `<system-reminder>` を除くと空の行・`isMeta`（Skill 起動の文脈など、人が打っていない行）・`<task-notification>`（サブエージェントの完了通知）を除いたもの
- 分子（訂正）: 訂正辞書に当たった発話。辞書は 1 行 1 正規表現の平文（`#` はコメント）で、既定を CLI に埋め込む。設定 `retro.dictionary` で差し替え、
  `retro.dictionary_extra` で追加。不満・好例の語（`sentiment` 辞書）は率に入れず、ダイジェストの印に使う。判定の精度より「同じ基準で継続して測れる」を優先する
- 窓: 既定は直近 14 日（その日の 0 時起点。同じ日の間は何度実行しても同じ結果）。週の境界と 0 時は実行環境のタイムゾーン
- 位置: 各発話にセッション内の通し番号（何番目の人間の発話か）を持ち、`-by position` で区間（既定 `1-3,4-10,11-30,31-`。最初の 3 発話を分ける）別の率を出す。長いセッションで訂正が増えるかを見るため
- ダイジェスト（`extract`）: 窓の中の人間の発話ごとに「直前のアシスタント本文 300 字 → 発話（2000 字まで）」。訂正辞書のヒットは `★`、感情辞書は `☆` を見出しに付ける。
  出力は `sessions/<プロジェクト>/<開始日時>_<ID>.md` と `index.tsv`。既定の出力先は OS の一時ディレクトリの `braindex-retro`。
  出力先の `sessions/` と `index.tsv` は実行のたびに書き直す（前回の分は消える。出力先の他のファイルは触らない）。
  セッションログには機微が含まれるので、`-out` でリポの中に向けるのは自己責任で

設定（`braindex.json` の `retro` 節。設定ファイルが無くても動き、`root`（hub）も要らない）:

| キー | 意味 |
|---|---|
| `sessions_dir` | セッションログの置き場。既定 `~/.claude/projects`（`~` は展開する。相対パスは設定ファイルのディレクトリ基準） |
| `all_projects` | `true` で `root` の外で交わしたセッションも数える。既定 `false`（`root` 配下だけ） |
| `window_days` | `check` の窓（直近何日か）。既定 14 |
| `threshold` | 訂正率の閾値（0〜1）。既定 0.08（試用後に見直す前提の暫定値） |
| `baseline_weeks` | 窓の直前の何週を「ふだんの水準」として比べるか。既定 8。`0` で基準を使わず閾値だけにする |
| `position_bins` | 位置の区間。既定 `"1-3,4-10,11-30,31-"`（`下限-上限` か `下限-` をコンマ区切り） |
| `dictionary` / `dictionary_extra` | 訂正辞書のファイル（差し替え／追加）。省略で埋め込みの既定辞書 |

フラグ: 共通 `-config` `-sessions DIR`（設定より優先）`-date YYYY-MM-DD`（今日の固定）。`stats`／`extract` は `-since` か `-window-days`（同時は不可）。
`check` は `-window-days`・`-threshold`（明示したものだけが設定を上書き）・`-quiet`（超えたときだけ出力。警告も出さない）。
共通 `-all-projects`（設定 `retro.all_projects` と同じ。`news profile`・`news fetch`・`news suggest`・`learn` にもある）。
終了コード: `stats`／`extract` は 0 成功／1 失敗／2 警告つき（読めないログを飛ばした）。`check` は 0 鳴らさない（閾値以下、または閾値超えだが基準と同水準）／1 失敗／2 鳴らさないが警告つき／3 鳴らす（閾値超えかつ基準からも上振れ。警告があっても 3）。

組み込みの例。Claude Code の hook（`~/.claude/settings.json`）の `SessionStart` に置くと、超えたときだけ 1 行がセッションに入る（`|| true` は、hook が終了コード 0 のときだけ標準出力をセッションに入れるため）:

```json
{ "hooks": { "SessionStart": [ { "hooks": [ { "type": "command", "command": "braindex retro check -quiet || true" } ] } ] } }
```

定期実行なら週 1 回。cron: `0 9 * * 1 braindex retro check; [ $? -eq 3 ] && <通知コマンド>`。Windows のタスクスケジューラなら、
`braindex retro check` を回して終了コード 3 のときだけ通知する `.cmd` を登録する。`braindex` が定期実行の環境の PATH に無ければフルパスで書く。

**鳴らしたのに所見ノートが無ければ 1 行に添える**（2026-09-06 追加）。`docs/notes/` の下に、ファイル名の日付が窓の中にある
`retro-YYYY-MM-DD.md` が 1 つも無ければ、`（所見ノート docs/notes/retro-YYYY-MM-DD.md が窓の中に無い）` を末尾に足す。
状態ファイルは持たず、ノートの有無そのものを見る。`docs/notes/` ごと無い hub（その規約を採っていない）には言わない。

閾値超えの後は、hub のスキル `retro`（`braindex init -add retro` が展開する `.claude/skills/retro/SKILL.md`）の手順で `braindex retro extract` のダイジェストを読み、
所見（訂正の型・繰り返し指示・うまくいった協働）と規約への反映案を hub の `docs/notes/retro-YYYY-MM-DD.md` に残す。規約の書き換えは承認の後。

なぜ: 原型（作者の 2026-07〜08 のログ 530 セッション）を人手と LLM で分類したら、訂正の多くは「規約が無い」のではなく「規約があるのに出力時に効いていない」型だった。
だから訂正率を同じ基準で測り続け、上がったときに振り返る回路を置く。

## 確認済みの版

セッションログの形は Claude Code の版ごとに変わりうる。この読み取り層の除外規則（`<system-reminder>` だけの行・スラッシュコマンド・
継続要約・中断・サブエージェント（`isSidechain`）・人が打っていない行（`isMeta`））は、`2.1.258`〜`2.1.263` の実ログで確かめてある。
範囲の外の版のログを読んだら、`retro` も `news` も `learn` も次の警告を出す（古い側・新しい側で 1 行ずつ・終了コード 2）:

```
セッションログに確認済み(2.1.258〜2.1.263)より新しい版が 1 種・3 ファイルある(最も新しい 2.1.270)。除外規則を確かめて MaxKnownVersion を上げる
```

**規則そのものは変えない。** 合わない証拠が無いうちに挙動を変えると、確かめた版での結果まで動いて、率の変化が版の変化か自分の変化か分からなくなる。

**上げる手順**（新しい版のログが溜まってから）:

1. `braindex retro stats -by project` を新しい版のログで回し、発話数が急に増減していないかを見る（除外規則が効かなくなると、
   人が打っていない行まで数えて発話数が跳ねる）
2. `internal/sessions/sessions_test.go` の `TestExcludeReason` の判定表を、新しい版のログに出てくる行の形と突き合わせる
3. 合っていれば `internal/sessions/version.go` の `MaxKnownVersion` を上げ、行末のコメントに確かめた日付と対象を書く
4. `internal/sessions/testdata/projects/-work-repo-c/cccc0001.jsonl`（範囲外の版の fixture）の version が
   まだ範囲外であることを確かめる。範囲に入ってしまったら fixture の version を上げる

## 数えるセッションの範囲

既定では、**`root` の配下で交わしたセッションだけ**を数える（2026-09-06 変更）。`root` は `braindex.json` の `root`（索引の走査対象と同じ）。

`~/.claude/projects` には、索引に載らないリポジトリでの会話も、OS のシステムディレクトリで動かしたときの会話も混ざっている。
それを全部数えると、訂正率も関心プロファイルも「その人が braindex で扱っている範囲」の外の影響を受ける。

- 範囲の外のセッションは数えず、除いた件数を 1 行の警告にまとめる（`<root> の外のセッション N 件を除いた`）
- ログに作業ディレクトリが無いセッションも除く（置き場のディレクトリ名しか分からず、配下かどうか判定できないため）。件数は別の 1 行で伝える
- 全部数えたいときは設定 `retro.all_projects` を `true` にするか、`-all-projects` を付ける
- `braindex.json` に `root` が無いときは絞らない（絞りようがないので、その旨を警告に出す）。設定ファイルを使わず `-sessions` だけで回すときも絞らない
