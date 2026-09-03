# braindex

[![CI](https://github.com/pilefort/braindex/actions/workflows/ci.yml/badge.svg)](https://github.com/pilefort/braindex/actions/workflows/ci.yml)

**Markdown ノートの索引を、LLM なし・依存なし・決定的に作るリポ横断の CLI。**

複数の git リポジトリに散らばった Markdown ノート（`docs/notes/`・`docs/decisions.md`）を、
コピーせずに 1 枚の索引 `catalog.md` にまとめる CLI と、その索引を中心に知識を蓄積・レビューする
フォルダ規約のテンプレート。

- [構成](#構成) — 何がどこに置かれ、どう流れるか
- [何をするか](#何をするか) — 引く・書く・回す・確かめる・振り返る・知る
- [セットアップ](#セットアップ) — 3 手で hub リポが動く
- [コマンド](#コマンド) — `braindex`／`init`／`review`／`lint`／`retro`
- [設計](#設計) — 3 原則・やらないこと・LLM wiki 型との対応
- [開発](#開発) — 状態・リポジトリの地図

## 構成

`root`（既定は hub の親ディレクトリ）の直下にリポジトリを並べ、そのうち 1 つを hub にする。
hub が持つのは索引と週次レビューだけで、知識の正本は各リポに残る。

```text
parent/                            ← braindex.json の root（既定 ".."＝hub の親）
├── hub/                           ← 索引を置くリポ。braindex はここで実行する
│   ├── braindex.json              設定（root / notes_dirs / extra / review / retro）
│   ├── index/catalog.md           ■ 索引：1 ノート 1 行（日付・種別・タイトル・要旨・パス）
│   ├── work/review/2026-09-03.md  週次レビューの下書き（braindex review）
│   ├── docs/  work/               hub 自身のノートと作業状態
│   └── .claude/skills/            判断を埋めるスキル（braindex-review・retro）
├── alpha/                         ← 各プロジェクトのリポ。知識の正本はこちら
│   ├── docs/notes/**/*.md         索引に載る（種別 notes・notes/<サブディレクトリ>）
│   ├── docs/decisions.md          索引に載る（種別 decisions・末尾の H2 見出し 3 件）
│   └── work/ISSUE-*.md            braindex lint が検査する（索引には載せない）
└── beta/
    └── ...
```

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

## 何をするか

- **引く**: `braindex` を実行すると `<root>/*/docs/notes/**/*.md` と `<root>/*/docs/decisions.md` を走査し、
  `index/catalog.md` に 1 ノート 1 行（日付・種別・タイトル・要旨・パス）を書く。
  索引を grep して当たりを付け、パスの先の実ファイルを読む。索引の要旨だけで答えない。
- **書く**: 知識は各リポの `docs/notes/` に書く。索引側への転記はしない。
- **回す**: 索引を再生成してコミットすると、`git diff` がそのまま「前回からの差分」になる。
  週次レビューはこれを材料にする。
- **確かめる**: 作業状態（`work/ISSUE-*.md`）が規約の形かを `braindex lint` で決定論に検査する。
- **振り返る**: コーディングエージェントとのセッションで、ユーザーがエージェントの振る舞いを訂正した割合（訂正率）を
  ローカルのログから常時計測し、閾値を超えたらレトロスペクティブ（訂正の型の洗い出しと規約への反映）を促す。
- **知る**: 直近のセッション内容と、最近書いたノートから関心分野を推定し、その人に合ったニュースを選んで提示する。
  「残す／不要」の選別が関心の推定に戻る。

中核は「引く」と「回す」で、これが索引 CLI。「確かめる」「振り返る」は実装済み。「知る」は索引の周りで動く周辺機能として順次足す。

## セットアップ

### 1. 入れる

```sh
go install github.com/pilefort/braindex/cmd/braindex@latest
```

Go 1.26 以降。依存は標準ライブラリのみ。clone してあるなら `go install ./cmd/braindex` でもよい。

### 2. hub を作る

索引を置く hub リポと、各プロジェクトのリポを、同じ親ディレクトリの直下に並べる。

```sh
mkdir hub && cd hub
braindex init                                  # hub の骨格を展開(既存ファイルは上書きしない)
git init && git add . && git commit -m "hub"   # 索引の diff を「前回からの差分」にするため git 管理下に置く
braindex                                       # 索引 index/catalog.md を生成
braindex review                                # 週に 1 回: レビューの下書き work/review/<今日>.md
```

`braindex init` は設定 `braindex.json`（`"root": ".."`）・フォルダ規約のテンプレ・スキルを展開する。
テンプレを使わずに始めるなら、`braindex.example.json` を `braindex.json` としてコピーするだけでもよい。

**索引はコミットする。** hub が git 管理下にないと `braindex review` は索引の増減を常に 0 件と報告し、終了コード 2 で終わる。

### 3. 各リポに骨格を置く（任意）

```sh
braindex init -repo ../alpha   # docs/notes/{common,project}/・docs/decisions.md・work/{APPROVALS,TODO}.md
```

hub 側・リポ側とも既存ファイルは上書きしないので、再実行しても安全。
hub には週次レビューのスキル（`.claude/skills/braindex-review/SKILL.md`）と振り返りのスキル（`.claude/skills/retro/SKILL.md`）も入る。

### 4. エージェントに横断検索させる

hub の `CLAUDE.md` には「索引を grep → 実ファイルを読む」の手順が入るが、hub の外のリポで作業している
セッションからも引かせるには、利用者のグローバル `CLAUDE.md`（Claude Code なら `~/.claude/CLAUDE.md`）に次の 3 行を足す（`<hub>` は hub の場所）:

```md
- 複数リポにまたがる知識を答える前に、`<hub>/index/catalog.md` を `grep -i <語>` で引く（記憶で答えない）
- ヒット行のパスは `root`（hub の `braindex.json`。既定は hub の親ディレクトリ）からの相対。その実ファイルを読む。要旨は 80 字の手がかりであって、内容の代わりではない
- 何も当たらなければ、そう言う。ノートや決定をでっち上げない
```

### 5. 定期実行

判断は人が行うので、自動化するのは下書きの作成だけ。週に 1 回、hub で `braindex review` を動かす。
cron なら `0 9 * * 1 cd <hub> && braindex review`、Windows のタスクスケジューラなら
`schtasks /Create /SC WEEKLY /D MON /ST 09:00 /TN braindex-review /TR "cmd /c cd /d <hub> && braindex review"`。
`braindex` が定期実行の環境の PATH に無ければ、`go install` の出力先（`GOBIN`。無ければ `GOPATH/bin`、既定はホームの `go/bin`）のフルパスで書く。
同じ日に 2 回動いても、既にある下書きは上書きしない。

## コマンド

| コマンド | 入力 | 出力 |
|---|---|---|
| `braindex` | `<root>/*/docs/notes/**/*.md`・`<root>/*/docs/decisions.md` | `index/catalog.md` |
| `braindex init` | 埋め込みのテンプレ | hub の骨格（`-repo` で各リポの骨格） |
| `braindex review` | 索引の前回コミット・各リポの `git log`・`work/TODO.md` | `work/review/<今日>.md` |
| `braindex lint` | `<root>/*/work/ISSUE-*.md` | 指摘（stdout）と終了コード |
| `braindex retro` | Claude Code のセッションログ（`~/.claude/projects/*/*.jsonl`） | 率の表（stdout）・ダイジェスト（一時ディレクトリ） |

終了コードは共通で 0 成功／1 失敗（何も書かない）／2 警告つき完了。`retro check` だけ 3（閾値超え）を足す。
フラグの要約は `braindex -h`、各コマンドは `braindex <コマンド> -h`。

### braindex — 索引の生成

`braindex.json`（カレントディレクトリ。`-config` で変更可。無くてもよく、そのときは `-root` が必須）:

| キー | 意味 |
|---|---|
| `root` | 走査のルート。直下の各ディレクトリを 1 リポとみなす。相対パスは設定ファイルのディレクトリ基準。`-root` が無ければ必須 |
| `notes_dirs` | 各リポのノート置き場。既定 `["docs/notes"]`。`["wiki"]` や、移行中の `["docs/notes", "wiki"]` も可。種別ラベルは末尾セグメント。リポ内の相対パスに限る（`..` を含むパスと絶対パスは設定の誤りとして終了コード 1） |
| `extra` | 規約外の置き場を個別に足す配列。各要素は `repo`（root 直下のリポ名）・`path`（リポ内の起点。`"."` はリポ直下。`notes_dirs` と同じくリポ内の相対パスに限る）・`recursive`（`true` でサブディレクトリも走査）・`kind`（種別ラベル）・`exclude`（グロブの配列。`/` を含むパターンは起点からの相対パス、含まなければファイル名に掛ける。大文字小文字は区別する） |
| `review` | 週次レビュー（`braindex review`）の節。`dir`（記録の置き場。既定 `work/review`）・`since_days`（前回の記録が無いときに遡る日数。既定 14）・`stale_todo_weeks`（TODO を放置とみなす週数。既定 4）・`archive_months`（何か月より前をアーカイブ候補にするか。既定 6）。省略可 |
| `retro` | 振り返り（`braindex retro`）の節。`sessions_dir`・`window_days`・`threshold`・`position_bins`・`dictionary`・`dictionary_extra`。省略可。詳細は `braindex retro` の節 |

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

索引の各行の決め方:

- リポ: `root` 直下の各ディレクトリ（`.` で始まるものは除く）。root 相対パスに `archive` セグメントを含むファイルは除外
- 種別: ノート置き場の直下はその末尾セグメント（既定 `notes`）、サブディレクトリ配下は `notes/<サブディレクトリ>`、`docs/decisions.md` は `decisions`、`extra` は指定した `kind`（`recursive` ならサブディレクトリ配下は `kind/<サブディレクトリ>`）
- 日付: ファイル名の `YYYYMMDD` か `YYYY-MM-DD` → 本文先頭 10 行の ISO 日付か `YYYY年M月D日` → 無ければ空欄（mtime には頼らない）
- タイトル: 最初の `# ` 行。無ければファイル名（`.md` を除く）
- 要旨: タイトル直後の、見出し・表・コードフェンスでない最初の本文行を 80 字で切る。`decisions.md` は末尾の H2 見出し 3 件
- 並び: リポ名昇順 → 日付降順 → パス昇順。改行は LF 固定。入力の BOM と CRLF は正規化する

### braindex init — 骨格の展開

hub リポ（引数なし）か各プロジェクトのリポ（`-repo <dir>`）に、フォルダ規約の骨格を展開する。
既存ファイルは上書きしない。展開されるものは「セットアップ」の節。

### braindex review — 週次レビューの下書き

hub で `braindex review` を実行すると、`work/review/<今日>.md` に週次レビューの下書きができる。集計は CLI が決定論で行い、
判断（差分の要約・アーカイブの可否・次アクション）は人か、人が使うエージェント（hub に入るスキル `braindex-review`）が埋める。
索引 `index/catalog.md` は読むだけで書き換えない（再生成は `braindex`）。

下書きの節（この順・固定）:

| 節 | 中身 | 埋めるのは |
|---|---|---|
| 索引（件数と増減） | 前回レビュー時点の索引（hub が git 管理下ならそのコミット、無ければディスクの索引）と、いま走査した結果の差。リポ別に 追加／変更（変わった列名つき）／削除 | CLI |
| 差分ファイル（リポ別） | 各リポで前回以降のコミットが `notes_dirs` と `docs/decisions.md` に触れたファイル（`git log --since --name-status`）。git 管理外のリポは飛ばして警告 | CLI |
| 放置 TODO | 各リポの `work/TODO.md` の未完了項目のうち、行が最後に変わった日（`git blame`）が `stale_todo_weeks` 週より前のもの。git で追えなければ mtime に `~` | CLI |
| アーカイブ候補（機械条件のみ） | `archive_months` か月より前で、今回の差分に無いノート。`decisions` は含めない | CLI |
| 今週の差分ダイジェスト／アーカイブ（実施・見送りと理由）／次アクション | 見出しだけ | 人 |

フラグ: `-config`（設定ファイル＝hub の位置。既定はカレントの `braindex.json`。無ければ失敗）`-date YYYY-MM-DD`（今日の固定）`-since YYYY-MM-DD`（前回日。既定は記録の置き場にある最新の `YYYY-MM-DD.md`、無ければ `since_days` 日前）
`-out`（出力先。既にあれば書かない）`-stdout`（標準出力へ）。
終了コード: 0 成功／1 失敗（フラグの誤り・設定が無い・出力先が既にある。何も書かない）／2 警告つき完了（git 不在・git 管理外のリポを飛ばした）。
git はあれば使う。無い環境でも索引の増減（ディスクの索引との比較）・放置 TODO（日付は mtime で `~` つき）・アーカイブ候補は出る。

### braindex lint — ISSUE の検査

`work/ISSUE-<slug>.md`（作業状態。テンプレ `docs/conventions.md` の形）が規約どおりかを決定論で検査する。索引には載せない。

```sh
braindex lint                    # root 直下の各リポの work/ISSUE-*.md をまとめて検査(root は索引と同じ解決規則)
braindex lint work               # ディレクトリ直下の ISSUE-*.md
braindex lint work/ISSUE-x.md    # ファイル
```

指摘は stdout に `パス:行: 内容` で出す。終了コード: 0 指摘なし／1 失敗（フラグ・root・パスの誤り）／2 指摘あり。

| 検査 | 指摘 |
|---|---|
| 見出し | 先頭の見出しが `# ISSUE:` で始まらない |
| 必須の節 | `## 現在の作業` `## 状態` が無い（対象リスト・メモは任意） |
| 現在地 | `← いまここ` が無い、または 2 つ以上 |
| 最終更新 | `最終更新: YYYY-MM-DD` が無い・形式が違う・未来・`-stale-days N` 以上たっている |
| 仕様 | `仕様: SPEC-<slug>.md` の参照先が同じディレクトリに無い |
| HEAD 比較（git 管理下のみ） | HEAD にあったチェック項目（`- [ ]`／`- [x]`）が消えた／内容が変わったのに最終更新が HEAD と同じ |

フラグ: `-config` `-root` `-date YYYY-MM-DD`（基準日）`-stale-days N`（0 で見ない）`-no-git`（HEAD 比較をしない）。
git が無い・git 管理外のファイルは HEAD 比較を飛ばす（要約の「HEAD 比較 N 件」で分かる）。

なぜ: ISSUE は「次のセッションが 1 枚読んで再開できる状態」を目的にするが、更新のたびに丸ごと書き直すので、既存の項目を落とす事故が起きる。
エージェントの実行状態をランタイムが検証して不正なら差し戻す設計（SKILL.state, arXiv:2608.26263）と同じ形で、機械が形を確かめ、判断は人がする。

### braindex retro — 訂正率の計測

Claude Code のセッションログ（既定 `~/.claude/projects/<slug>/*.jsonl`）から「人間の発話のうち、エージェントの振る舞いへの訂正の割合」（訂正率）を
決定論で測り、閾値を超えたら振り返り（レトロスペクティブ）を促す。計測は CLI、振り返り本体の判断は人か、hub に入るスキル `retro`。
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
| `window_days` | `check` の窓（直近何日か）。既定 14 |
| `threshold` | 訂正率の閾値（0〜1）。既定 0.08（試用後に見直す前提の暫定値） |
| `position_bins` | 位置の区間。既定 `"1-3,4-10,11-30,31-"`（`下限-上限` か `下限-` をコンマ区切り） |
| `dictionary` / `dictionary_extra` | 訂正辞書のファイル（差し替え／追加）。省略で埋め込みの既定辞書 |

フラグ: 共通 `-config` `-sessions DIR`（設定より優先）`-date YYYY-MM-DD`（今日の固定）。`stats`／`extract` は `-since` か `-window-days`（同時は不可）。
`check` は `-window-days`・`-threshold`（明示したものだけが設定を上書き）・`-quiet`（超えたときだけ出力。警告も出さない）。
終了コード: `stats`／`extract` は 0 成功／1 失敗／2 警告つき（読めないログを飛ばした）。`check` は 0 閾値以下／1 失敗／2 閾値以下だが警告つき／3 閾値超え（警告があっても 3）。

組み込みの例。Claude Code の hook（`~/.claude/settings.json`）の `SessionStart` に置くと、超えたときだけ 1 行がセッションに入る（`|| true` は、hook が終了コード 0 のときだけ標準出力をセッションに入れるため）:

```json
{ "hooks": { "SessionStart": [ { "hooks": [ { "type": "command", "command": "braindex retro check -quiet || true" } ] } ] } }
```

定期実行なら週 1 回。cron: `0 9 * * 1 braindex retro check; [ $? -eq 3 ] && <通知コマンド>`。Windows のタスクスケジューラなら、
`braindex retro check` を回して終了コード 3 のときだけ通知する `.cmd` を登録する。`braindex` が定期実行の環境の PATH に無ければフルパスで書く。

閾値超えの後は、hub のスキル `retro`（`braindex init` が展開する `.claude/skills/retro/SKILL.md`）の手順で `braindex retro extract` のダイジェストを読み、
所見（訂正の型・繰り返し指示・うまくいった協働）と規約への反映案を hub の `docs/notes/retro-YYYY-MM-DD.md` に残す。規約の書き換えは承認の後。

なぜ: 原型（作者の 2026-07〜08 のログ 530 セッション）を人手と LLM で分類したら、訂正の多くは「規約が無い」のではなく「規約があるのに出力時に効いていない」型だった。
だから訂正率を同じ基準で測り続け、上がったときに振り返る回路を置く。

## 設計

### 3 原則

1. **指す、コピーしない。** 知識の正本は各リポにある。索引はパスで指すだけなので、正が 2 つにならない。
2. **索引は決定的。LLM を使わない。** 同じ入力からは常にバイト一致の索引が出る。だから索引をコミットでき、diff が意味を持つ。
   周辺機能（振り返る・知る）は LLM を採点や要約の補助に使ってよいが、取得と計測は決定論で行い、判断は人に残す。
3. **寿命で分ける。** 蓄積するもの（`docs/`）と揮発するもの（`work/`）を混ぜない。索引は前者だけを見る。

理由と却下案は作者の設計メモ（`docs/`・git 管理外）にある。

### やらないこと

- 意味検索・ベクトル DB（recall 失敗の実例が出るまで入れない）
- LLM による索引の要旨生成・索引更新
- ノート本文・セッション内容の外部送信（ニュースの取得は GET のみ）
- ネタ帳（アイデア帳）。索引・レビュー・ニュースとは独立した機能で、テンプレの核ではない

### LLM wiki 型との対応

Karpathy の LLM wiki 型（2026-04・`raw/` の素材から LLM が `wiki/` のページを編纂し `index.md` と `log.md` を維持する）と
似た部品を持つが、役割の置き方が違う。ノート置き場は設定 `notes_dirs`（配列・既定 `["docs/notes"]`）で変えたり足したりできるので、
`wiki/` を使う運用でも、`docs/notes` と `wiki` を並走させる移行中でも、そのまま走査できる。

| LLM wiki 型 | braindex | 違い |
|---|---|---|
| `raw/`（素材） | 各リポの作業そのもの（コード・調査・会話） | 素材を 1 か所に集めない |
| `wiki/`（LLM が編纂したページ） | 各リポの `docs/notes/`（`notes_dirs` で変更・追加可） | 人かエージェントが出典つきで書く。LLM が編纂・書き換えはしない |
| `index.md`（LLM が更新する目次） | hub リポの `index/catalog.md` | CLI が決定的に再生成する。LLM は触らない |
| `log.md`（追記式の履歴） | `git log` と `catalog.md` の diff | 専用ファイルを持たない |
| lint（矛盾・陳腐化の検出） | 週次レビュー（`braindex review` が索引の増減・差分ファイル・放置 TODO・アーカイブ候補を集計し、スキル `braindex-review` が判断を埋める） | 集計は CLI、判断は人 |

## 開発

### 状態

v0.1.0（2026-09-03）: 索引 CLI（Phase 1）を原型から移植して可搬化し、セットアップ `braindex init`（フォルダ規約のテンプレ同梱）・
週次レビューの集計 `braindex review`（Phase 2）・ISSUE の検査 `braindex lint`・訂正率トリガのレトロスペクティブ `braindex retro`（Phase 3）を足した。
原型は作者の私用「第二の脳」で 2026-08-07 から運用しているもの（非公開・20 リポ 307 ノートを索引中）。

予定: ニュースサジェスト（`braindex news`）・判断待ちのフォーム（`braindex approvals`）・回答の HTML 化と裏取り（順は 2026-09-02 決定）。

### リポジトリの地図

| 場所 | 何が入るか |
|---|---|
| `cmd/braindex` | サブコマンドの登録とフラグ解析（`main.go`・`commands.go`・`cmd_*.go`） |
| `internal/` | 索引の実装（`scan` → `extract` → `render` → `catalog`）と `config`・`template`（init）・`lint`・`review`・`sessions`／`retro` |
| `braindex.example.json` | 設定ファイルの雛形 |
| `.github/workflows/ci.yml` | CI。ubuntu と windows で gofmt／vet／test に加え、同じ入力から 2 回生成してバイト一致することを確かめる |
| `CONTRIBUTING.md` | 開発の決まり（テスト・決定性・持ち込まないもの） |
| `docs/` `work/` | 作者の設計メモと作業状態。git 管理外（`.gitignore`。2026-09-02 決定） |

```sh
go test ./...   # 依存なし。CI は gofmt -l . と go vet ./... も回す
```

## ライセンス

MIT（`LICENSE`）。
