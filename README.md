# braindex

[![CI](https://github.com/pilefort/braindex/actions/workflows/ci.yml/badge.svg)](https://github.com/pilefort/braindex/actions/workflows/ci.yml)

**Markdown ノートの索引を、LLM なし・依存なし・決定的に作るリポ横断の CLI。**

複数の git リポジトリに散らばった Markdown ノート（`docs/notes/`・`docs/decisions.md`）を、
コピーせずに 1 枚の索引 `catalog.md` にまとめる CLI と、その索引を中心に知識を蓄積・レビューする
フォルダ規約のテンプレート。

- [構成](#構成) — 何がどこに置かれ、どう流れるか
- [何をするか](#何をするか) — 引く・書く・回す・確かめる・振り返る・知る・決める・示す・裏を取る
- [セットアップ](#セットアップ) — 3 手で hub リポが動く
- [コマンド](#コマンド) — 索引／`init`／`review`／`lint`／`retro`／`news`／`approvals`／`answer`／`verify`／`scope`／`schedule`
- [設計](#設計) — 3 原則・やらないこと・LLM wiki 型との対応
- [開発](#開発) — 状態・リポジトリの地図

## 構成

`root`（既定は hub の親ディレクトリ）の直下にリポジトリを並べ、そのうち 1 つを hub にする。
hub が持つのは索引と週次レビューだけで、知識の正本は各リポに残る。

```text
parent/                            ← braindex.json の root（既定 ".."＝hub の親）
├── hub/                           ← 索引を置くリポ。braindex はここで実行する
│   ├── braindex.json              設定（root / notes_dirs / extra / review / retro / news / schedule）
│   ├── index/catalog.md           ■ 索引：1 ノート 1 行（日付・種別・タイトル・要旨・パス）
│   ├── work/review/2026-09-03.md  週次レビューの下書き（braindex review）
│   ├── news/                      ニュースの置き場（ダイジェストと、選別で残した見出し keep/）
│   ├── docs/  work/               hub 自身のノートと作業状態（work/APPROVALS.md は判断待ち）
│   └── .claude/skills/            判断を埋めるスキル（braindex-review・retro・record-lint・contradiction-scan）
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
- **確かめる**: 作業状態（`work/ISSUE-*.md`）が規約の形か、ノートが曖昧でないか（出典なき数字・日付なし・裸のヘッジ…）を
  `braindex lint` で決定論に検査する。複数ノートをまたぐ矛盾を探すときは、`braindex scope` が索引から走査対象を切り出す（判定は人かエージェント）。
- **振り返る**: コーディングエージェントとのセッションで、ユーザーがエージェントの振る舞いを訂正した割合（訂正率）を
  ローカルのログから常時計測し、閾値を超えたらレトロスペクティブ（訂正の型の洗い出しと規約への反映）を促す。
- **知る**: 直近のセッション内容と、最近書いたノートから関心分野を推定し、その人に合ったニュースを選んで提示する。
  「残す／不要」の選別が関心の推定に戻る。
- **決める**: 判断待ち（`work/APPROVALS.md`）をブラウザのフォームで聞き、答えを `docs/decisions.md` に
  3 段（結論 → 理由 → 根拠）で追記する。受け口は 127.0.0.1 だけで、外へは出さない。
- **示す**: 読み返す価値のある回答は Markdown で書き、自己完結の HTML にして既定ブラウザで開く（チャットは流れる）。
- **裏を取る**: ノートに書いた GitHub リポ・arXiv 論文・URL・逐語引用が実在するかを、一次ソースへの GET で照合する。

中核は「引く」と「回す」で、これが索引 CLI。ほかは索引の周りで動く周辺機能で、いずれも main に入っている。

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
hub には判断を埋めるスキルも入る（`.claude/skills/` の `braindex-review`・`retro`・`record-lint`・`contradiction-scan`）。

### 4. エージェントに横断検索させる

hub の `CLAUDE.md` には「索引を grep → 実ファイルを読む」の手順が入るが、hub の外のリポで作業している
セッションからも引かせるには、利用者のグローバル `CLAUDE.md`（Claude Code なら `~/.claude/CLAUDE.md`）に次の 3 行を足す（`<hub>` は hub の場所）:

```md
- 複数リポにまたがる知識を答える前に、`<hub>/index/catalog.md` を `grep -i <語>` で引く（記憶で答えない）
- ヒット行のパスは `root`（hub の `braindex.json`。既定は hub の親ディレクトリ）からの相対。その実ファイルを読む。要旨は 80 字の手がかりであって、内容の代わりではない
- 何も当たらなければ、そう言う。ノートや決定をでっち上げない
```

### 5. 定期実行

判断は人が行うので、自動化するのは下書きの作成だけ。設定の `schedule` 節に書いたジョブを、hub で `braindex schedule install`
と打つと OS のスケジューラ（Windows は schtasks、macOS・Linux は crontab）に登録できる。

```sh
braindex schedule print      # 登録に使うコマンドを出すだけ（何も変えない）
braindex schedule install    # 登録する（再実行しても二重にならない）
braindex schedule list       # 設定のジョブと、OS 側に登録されているか
braindex schedule uninstall  # この hub の登録を消す
```

節を省略すると、週次レビュー（月 09:00）と訂正率の確認（月 09:05）の 2 本になる。hub と braindex 自身の絶対パスを埋め込むので、
定期実行の環境の PATH には依存しない（どちらかを移したら登録し直す）。詳細は [`braindex schedule`](#braindex-schedule--定期実行の登録) の節。
自分で cron や schtasks に書きたいときは `braindex schedule print` の出力をそのまま使える。
同じ日に 2 回動いても、既にある下書きは上書きしない。

## コマンド

| コマンド | 入力 | 出力 |
|---|---|---|
| `braindex` | `<root>/*/docs/notes/**/*.md`・`<root>/*/docs/decisions.md` | `index/catalog.md` |
| `braindex init` | 埋め込みのテンプレ | hub の骨格（`-repo` で各リポの骨格） |
| `braindex review` | 索引の前回コミット・各リポの `git log`・`work/TODO.md` | `work/review/<今日>.md` |
| `braindex lint` | `<root>/*/work/ISSUE-*.md` | 指摘（stdout）と終了コード |
| `braindex retro` | Claude Code のセッションログ（`~/.claude/projects/*/*.jsonl`） | 率の表（stdout）・ダイジェスト（一時ディレクトリ） |
| `braindex news` | `news/feeds.json` のフィード（GET）と関心の出典（索引・セッション・`news/keep/`） | `news/digest_<日付>_<層>.md` と選別 UI の同名 `.html` |
| `braindex approvals` | `work/APPROVALS.md` | ブラウザのフォーム → `docs/decisions.md` への追記 |
| `braindex answer` | Markdown 1 ファイル | 自己完結 HTML（一時置き場・既定ブラウザで開く） |
| `braindex verify` | GitHub リポ・arXiv ID・URL・逐語引用 | 照合の結果（stdout・`-json`） |
| `braindex scope` | `index/catalog.md`（`-dir` ならディレクトリ配下の `*.md`） | 矛盾検査の走査対象（chunk 分割・stdout・`-json`） |
| `braindex schedule` | 設定の `schedule` 節 | OS のスケジューラへの登録 |

終了コードは共通で **0 成功／1 失敗（結果を書かない）／2 警告つき完了（結果は書いたが、飛ばしたものや取りこぼしがある）**。
「2 なら結果は使える」が全コマンドで成り立つので、定期実行から一律に判定できる。

| 2 を返す場面 | コマンド |
|---|---|
| 読めないものを飛ばした | 索引の生成・`review`・`news fetch`（フィード・選別 JSON・統計）・`news profile` |
| 指摘・不一致があった | `lint`（指摘あり）・`verify`（NOT FOUND あり）・`approvals status`（記載漏れ・未反映の回答）・`approvals apply`（反映できなかった項目） |
| 突き合わせる相手がいない | `scope`（対象が 2 件未満） |

3 を使うのは 2 つだけ: `retro check`（閾値超え）と `approvals serve`（時間切れ）。
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
| `news` | ニュースサジェスト（`braindex news`）の節。`dir`（既定 `news`）・`feeds`（既定 `news/feeds.json`）・`seen_days`（既定 90）・`cap_per_layer`（層ごとの 1 フィード表示上限。既定 `{"daily": 15, "weekly": 25}`・表に無い層は 20）・`profile_days`（既定 14）・`sessions_dir`・`show_min_score`（主要表示にする関心度の下限。0〜3・既定 2。**0 は全件を主要表示**で、省略とは別の意味）。省略可。範囲外の値は設定の誤りとしてエラー |
| `schedule` | 定期実行（`braindex schedule`）の節。`jobs` の配列（`name`・`args`・`when`）。省略すると既定の 2 本。省略可 |

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

### braindex lint — ISSUE とノートの検査

`work/ISSUE-<slug>.md`（作業状態。テンプレ `docs/conventions.md` の形）が規約どおりか、ノート（それ以外の `.md`）が
曖昧でないかを決定論で検査する。索引には載せない。`ISSUE-*.md` は形の検査、それ以外はノートの検査になる（`-kind issue|note` で固定できる）。

```sh
braindex lint                         # root 直下の各リポの work/ISSUE-*.md をまとめて検査(root は索引と同じ解決規則)
braindex lint work                    # ディレクトリ直下の ISSUE-*.md
braindex lint work/ISSUE-x.md         # ファイル
braindex lint docs/notes/x.md         # ノートの曖昧さ検査(用語集は同じリポの docs/glossary.md を自動で探す)
braindex lint -kind note docs/notes   # ディレクトリ直下の *.md をノートとして検査
```

指摘は stdout に `パス:行: [種別] 内容` で出す（ファイル全体に掛かる指摘は行番号なし。`-json` なら `path`・`line`・`msg`・`kind`・`severity` の配列）。
終了コード: 0 指摘なし／1 失敗（フラグ・root・パスの誤り）／2 指摘あり。

#### ISSUE の形

| 検査 | 指摘 |
|---|---|
| 見出し | 先頭の見出しが `# ISSUE:` で始まらない |
| 必須の節 | `## 現在の作業` `## 状態` が無い（対象リスト・メモは任意） |
| 現在地 | `← いまここ` が無い、または 2 つ以上 |
| 最終更新 | `最終更新: YYYY-MM-DD` が無い・形式が違う・未来・`-stale-days N` 以上たっている |
| 仕様 | `仕様: SPEC-<slug>.md` の参照先が同じディレクトリに無い |
| HEAD 比較（git 管理下のみ） | HEAD にあったチェック項目（`- [ ]`／`- [x]`）が消えた／内容が変わったのに最終更新が HEAD と同じ |

フラグ: `-config` `-root` `-date YYYY-MM-DD`（基準日）`-stale-days N`（0 で見ない）`-no-git`（HEAD 比較をしない）
`-kind issue|note` `-glossary <用語集>` `-json`。
git が無い・git 管理外のファイルは HEAD 比較を飛ばす（要約の「HEAD 比較 N 件」で分かる）。

なぜ: ISSUE は「次のセッションが 1 枚読んで再開できる状態」を目的にするが、更新のたびに丸ごと書き直すので、既存の項目を落とす事故が起きる。
エージェントの実行状態をランタイムが検証して不正なら差し戻す設計（SKILL.state, arXiv:2608.26263）と同じ形で、機械が形を確かめ、判断は人がする。

#### ノートの曖昧さ

後から読む者（人もエージェントも）が事実をもっともらしく再構成してしまう書き方を拾う。確度は 2 段階で、warn はほぼそのまま直してよく、
candidate（種別名に「(候補)」が付く）は本文の意味で真偽を確かめてから直す。判断と修正案の手順は hub に入るスキル `record-lint`。

| 種別 | 確度 | 指摘 |
|---|---|---|
| 曖昧な数量詞 | warn | 多い・最近・かなり など、数値や日付に置き換えるべき語 |
| 日付なし | warn | 本文に日付（`YYYY-MM-DD`・`YYYY/M/D`・`YYYY 年 M 月`）が一つも無く、ファイル名（`YYYYMMDD`・`YYYY-MM-DD`）にも無い |
| 出典なき数字 | candidate | 単位つきの数や小数がある行に、出典マーカー（出典・根拠・参照・実測・→・URL・ファイル名 など）が無い |
| 裸のヘッジ | candidate | たぶん・かもしれない・〜と思う などが、（推測）・未確認 のタグも出典も無いまま付いている |
| なぜ欠落 | candidate | 決定（`記録日`・`採用日` の語を持つ `##` ブロック。`decisions.md` は全ブロック）に理由の語が無い |
| 根拠欠落 | candidate | 決定に `根拠:` 行が無い |
| 未定義用語 | candidate | 「鉤括弧の語」・`[[link]]`・英大文字始まりの語が用語集に無い（`-glossary` か、同じリポの `docs/glossary.md` があるときだけ） |

コードフェンスの中は見ない（未定義用語だけは本文全体から語を拾う）。表の行は出典なき数字と裸のヘッジで見ない。語彙表は日本語のみで CLI に埋め込む。

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

### braindex news — ニュースサジェスト

hub で `braindex news fetch` を実行すると、`news/feeds.json` のフィードを GET し、既読（`news/.seen.json`）に無い記事を
`news/digest_<日付>_<層>.md`（記録用）と、同名の `.html`（選別 UI・既定ブラウザで開く）に書く。
記事は関心プロファイルで採点し、関心度が `news.show_min_score`（既定 2）以上を主要表示、未満は「関心外と判定」に折りたたむ。
HTML の「選別を書き出す」が保存した JSON を `braindex news apply` が取り込み、「残す」を `news/keep/YYYY-MM.md` に追記する。
keep は次のプロファイルの出典になるので、**選別がそのまま関心の推定に戻る**。
外へ出る通信はフィードの GET だけで、セッション内容もノート本文も送らない。HTML は外部の JS・CSS を参照しない。
フィードのリンクは `http(s)` のものだけを載せる（それ以外は題名だけを出し、選別 JSON にも `news/keep/` にも入れない）。

フィード一覧 `news/feeds.json` は自分で作る（`braindex init` は展開しない）。`name` と `url` を持つオブジェクトの配列:

```json
[
  { "name": "Go Blog", "url": "https://go.dev/blog/feed.atom", "layer": "weekly", "lang": "en" }
]
```

`layer` は自由なラベル（`daily`・`weekly` など）で、`news fetch -layer <層>` の絞り込みと表示上限（`cap_per_layer`）に使う。
空のフィードは `-layer all`（既定）のときだけ取る。`lang`・`category`・`note` は任意。未知のキー・`name` の重複・
`http(s)` でない URL はエラーにする。RSS 2.0・Atom・RSS 1.0 を読み、記事の識別子は追跡パラメータを除いたリンクから作る
（同じ記事が `utm_` 付きで再配信されても既読と一致する）。

サブコマンド:

| サブコマンド | 何をするか |
|---|---|
| `news fetch` | フィードを取得し、新着のダイジェスト（Markdown）と選別 UI（HTML）を書く。冒頭で `apply` と同じ取り込みも動く |
| `news profile` | 関心プロファイル（語 → 重み・出典）を表示する。出典は索引の直近差分・直近のセッション内容・`news/keep/`・`news/interests.md` |
| `news apply` | 選別 JSON を `<news.dir>/inbox` と `-inbox`（既定 `~/Downloads`）から取り込む |

重みは出典ごとに最大を 1 に正規化した値の和で、決定論。LLM は使わない。窓の起点は `retro` と同じローカルの 0 時。
主なフラグ: `-config` `-date YYYY-MM-DD` `-layer` `-out` `-stdout` `-no-open` `-no-score`（採点せず全件を主要表示）
`-replay`（既読を無視して再生成し、既読も更新しない）`-days` `-top` `-json` `-sessions` `-inbox`。
終了コード: 0 成功／1 失敗（**同じ日の出力先が既にある**・全フィードの取得失敗。何も書かない）／2 警告つきで完了（一部のフィードが取れなかった・採点の出典が無かった・選別や統計を取り込めなかった）。
同じ日に 2 回動かすと、既にあるダイジェストは上書きせず終了コード 1 で止まる（`braindex review` と同じ。読み直すだけなら `-out` で別名に、捨ててよければ `-stdout` に出す）。

### braindex approvals — 判断待ちのフォーム

`work/APPROVALS.md` の判断待ち（1 項目 1 判断・5 欄「決めたいこと／なぜ今決めるか／選択肢／私の案／決めないとどうなるか」）を
ブラウザのフォームにして聞き、答えを記録する。受け口は 127.0.0.1 の空きポートだけで、回答を 1 回受けたら終わる（常駐しない）。外へは何も送らない。

```sh
braindex approvals serve -apply   # フォームを開いて回答を待ち、そのまま反映する
braindex approvals status         # 件数・記載漏れ・未反映の回答（書き込みなし）
braindex approvals apply          # 受けた回答を反映する（聞くのと分けたいとき）
```

選んだ項目は `docs/decisions.md` に 3 段（結論 → 理由 → 根拠）で追記して `APPROVALS.md` から消し、保留は項目を残して
「**保留（日付）:**」を付ける。反映した回答 JSON は `.applied.json` に改名するので、2 回反映されない。
フラグ: `-file`（既定 `work/APPROVALS.md`）`-dir`（回答 JSON の置き場。既定は OS の一時ディレクトリの `braindex-approvals`）
`-timeout 秒`（0 で無期限）`-no-open` `-apply` `-decisions` `-date` `-reply`。
終了コード: `serve` 0 回答あり／3 時間切れ、`apply` 0 反映した・回答なし／2 反映できなかった項目がある、`status` 0 ／2 記載漏れか未反映の回答あり。いずれも 1 は失敗。

### braindex answer — 回答の HTML 化

`braindex answer <md>` は Markdown を自己完結の HTML（外部の JS・CSS を参照しない）にして書き、既定ブラウザで開く。
出力先の既定は一時置き場で、実行のたびに `-ttl-days`（既定 14）より古いものを消す。
**HTML は読むための一時物**なので、残す価値のある内容は `.md` を `docs/notes/` に置いてから渡す（置き場所が寿命を表す）。
リンクの `href` に出すのは `http(s)` と、スキームを持たないもの（相対パス・`#見出し`）だけ。`javascript:` などは文字として残す。

```sh
braindex answer note.md            # HTML にして開く
braindex answer -no-open note.md   # 書くだけ
braindex answer -dir               # 一時置き場の場所を表示して終わる
braindex answer -purge             # 一時置き場の中を今すぐ全部消す
```

フラグ: `-out` `-no-open` `-dir` `-purge` `-ttl-days`（0 で消さない）。終了コード: 0 成功／1 失敗。

### braindex verify — 実在の照合

ノートに書いた GitHub リポ・arXiv 論文・URL・逐語引用を、一次ソースへの GET で照合する（こちらから本文は送らない）。

```sh
braindex verify github pilefort/braindex
braindex verify arxiv 2608.26263
braindex verify url https://go.dev/blog/
braindex verify quote https://example.com/a "引用したい文をそのまま書く"
```

出力は 1 件 1 行（種別・対象・判定・実測のタブ区切り。`-json` で配列）。`quote` は空白の揺れだけ許し、24 字未満は照合しない。
`GITHUB_TOKEN` があれば GitHub API の認証に使う（任意・レート制限対策）。
終了コード: 0 全件 FOUND／2 NOT FOUND あり／1 失敗。

### braindex scope — 矛盾検査の走査対象

複数ノートをまたぐ相互矛盾・陳腐化を探すとき、索引から走査対象を列挙・絞り込み・chunk 分割して出す。**矛盾の判定はしない**
（chunk ごとに実ファイルを全文読み比べ、反証で偽陽性を落とす手順は hub に入るスキル `contradiction-scan`）。

```sh
braindex scope -topic 長さ         # タイトル・要旨・パスに語を含む行(大小無視)
braindex scope -repo alpha         # その見出し(リポ名)の行だけ
braindex scope -full -size 20      # 全件を 20 件ずつの chunk に分ける
braindex scope -dir docs/notes     # 索引を使わず、ディレクトリ配下の *.md を列挙する
```

索引を使うときのパスは索引の行と同じ（`root` からの相対）。`-dir` のときは渡したディレクトリからの相対で出る。
フラグ: `-topic` `-repo` `-dir` `-full` `-size N`（既定 12）`-json`（`mode`・`n_entries`・`chunks`）`-catalog` `-config`。
終了コード: 0 ／1 失敗／2 対象が 2 件未満（突き合わせる相手がいない）。

### braindex schedule — 定期実行の登録

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

タグの後（2026-09-03）に main へ入ったもの: ニュースサジェスト `braindex news`（Phase 4）・判断待ちのフォーム `braindex approvals`（Phase 5）・
ノートの曖昧さ検査 `braindex lint -kind note` と走査対象の切り出し `braindex scope`（Phase 6）・回答の HTML 化 `braindex answer` と
実在の照合 `braindex verify`（Phase 7）・定期実行の登録 `braindex schedule`。次のタグで出る。

### リポジトリの地図

| 場所 | 何が入るか |
|---|---|
| `cmd/braindex` | サブコマンドの登録とフラグ解析（`main.go`・`commands.go`・`cmd_*.go`） |
| `internal/` | 索引の実装（`scan` → `extract` → `render` → `catalog`）と `config`・`template`（init）・`lint`・`review`・`sessions`／`retro`・`feed`／`interest`／`news`（ニュース）・`approvals`・`mdhtml`／`verify`（回答の HTML 化と照合）・`scope`・`schedule` |
| `braindex.example.json` | 設定ファイルの雛形 |
| `.github/workflows/ci.yml` | CI。ubuntu と windows で gofmt／vet／test に加え、同じ入力から 2 回生成してバイト一致することを確かめる |
| `CONTRIBUTING.md` | 開発の決まり（テスト・決定性・持ち込まないもの） |
| `docs/` `work/` | 作者の設計メモと作業状態。git 管理外（`.gitignore`。2026-09-02 決定） |

```sh
go test ./...   # 依存なし。CI は gofmt -l . と go vet ./... も回す
```

## ライセンス

MIT（`LICENSE`）。
