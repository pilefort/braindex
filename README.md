# braindex

**Markdown ノートの索引を、LLM なし・依存なし・決定的に作るリポ横断の CLI。**

複数の git リポジトリに散らばった Markdown ノート（`docs/notes/`・`docs/decisions.md`）を、
コピーせずに 1 枚の索引 `catalog.md` にまとめる CLI と、その索引を中心に知識を蓄積・レビューする
フォルダ規約のテンプレート。

## 状態

v0（2026-09-02）: 索引 CLI（Phase 1）を原型から移植して可搬化し、セットアップ `braindex init`（フォルダ規約のテンプレ同梱）と
週次レビューの集計 `braindex review`（Phase 2）を足した。
原型は作者の私用「第二の脳」で 2026-08-07 から運用しているもの（非公開・20 リポ 307 ノートを索引中）。

予定: 訂正率トリガのレトロスペクティブ → ニュースサジェスト（この順。2026-09-02 決定）。

## 使い方

```
go install github.com/pilefort/braindex/cmd/braindex@latest
```

Go 1.26 以降。依存は標準ライブラリのみ。

1. 複数のリポと hub リポ（索引を置くリポ）を同じ親ディレクトリの直下に並べる
2. hub リポで `braindex.example.json` を `braindex.json` としてコピーする（`"root": ".."` が親ディレクトリを指す）
3. hub リポで `braindex` を実行すると `index/catalog.md` ができる。索引を読むときは grep → パスの先の実ファイルへ
4. `index/catalog.md` をコミットする。以後、索引の `git diff` が「前回からの差分」になる

フラグ・終了コード・設定キーは下の「設定とフラグ」、索引の各行の意味は「索引の中身」の節（要約は `braindex -h`）。

## 設定とフラグ

`braindex.json`（カレントディレクトリ。`-config` で変更可。無くてもよく、そのときは `-root` が必須）:

| キー | 意味 |
|---|---|
| `root` | 走査のルート。直下の各ディレクトリを 1 リポとみなす。相対パスは設定ファイルのディレクトリ基準。`-root` が無ければ必須 |
| `notes_dirs` | 各リポのノート置き場。既定 `["docs/notes"]`。`["wiki"]` や、移行中の `["docs/notes", "wiki"]` も可。種別ラベルは末尾セグメント |
| `extra` | 規約外の置き場を個別に足す配列。各要素は `repo`（root 直下のリポ名）・`path`（リポ内の起点。`"."` はリポ直下）・`recursive`（`true` でサブディレクトリも走査）・`kind`（種別ラベル）・`exclude`（グロブの配列。`/` を含むパターンは起点からの相対パス、含まなければファイル名に掛ける。大文字小文字は区別する） |
| `review` | 週次レビュー（`braindex review`）の節。`dir`（記録の置き場。既定 `work/review`）・`since_days`（前回の記録が無いときに遡る日数。既定 14）・`stale_todo_weeks`（TODO を放置とみなす週数。既定 4）・`archive_months`（何か月より前をアーカイブ候補にするか。既定 6）。省略可 |

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

## 週次レビュー（braindex review）

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

## 索引の中身

- リポ: `root` 直下の各ディレクトリ（`.` で始まるものは除く）。root 相対パスに `archive` セグメントを含むファイルは除外
- 種別: ノート置き場の直下はその末尾セグメント（既定 `notes`）、サブディレクトリ配下は `notes/<サブディレクトリ>`、`docs/decisions.md` は `decisions`、`extra` は指定した `kind`（`recursive` ならサブディレクトリ配下は `kind/<サブディレクトリ>`）
- 日付: ファイル名の `YYYYMMDD` か `YYYY-MM-DD` → 本文先頭 10 行の ISO 日付か `YYYY年M月D日` → 無ければ空欄（mtime には頼らない）
- タイトル: 最初の `# ` 行。無ければファイル名（`.md` を除く）
- 要旨: タイトル直後の、見出し・表・コードフェンスでない最初の本文行を 80 字で切る。`decisions.md` は末尾の H2 見出し 3 件
- 並び: リポ名昇順 → 日付降順 → パス昇順。改行は LF 固定。入力の BOM と CRLF は正規化する

## 何をするか

- **引く**: `braindex` を実行すると `<root>/*/docs/notes/**/*.md` と `<root>/*/docs/decisions.md` を走査し、
  `index/catalog.md` に 1 ノート 1 行（日付・種別・タイトル・要旨・パス）を書く。
  索引を grep して当たりを付け、パスの先の実ファイルを読む。索引の要旨だけで答えない。
- **書く**: 知識は各リポの `docs/notes/` に書く。索引側への転記はしない。
- **回す**: 索引を再生成してコミットすると、`git diff` がそのまま「前回からの差分」になる。
  週次レビューはこれを材料にする。
- **振り返る**: コーディングエージェントとのセッションで、ユーザーがエージェントの振る舞いを訂正した割合（訂正率）を
  ローカルのログから常時計測し、閾値を超えたらレトロスペクティブ（訂正の型の洗い出しと規約への反映）を促す。
- **知る**: 直近のセッション内容と、最近書いたノートから関心分野を推定し、その人に合ったニュースを選んで提示する。
  「残す／不要」の選別が関心の推定に戻る。

中核は「引く」と「回す」で、これが索引 CLI。「振り返る」「知る」は索引の周りで動く周辺機能として順次足す。

## 3 原則

1. **指す、コピーしない。** 知識の正本は各リポにある。索引はパスで指すだけなので、正が 2 つにならない。
2. **索引は決定的。LLM を使わない。** 同じ入力からは常にバイト一致の索引が出る。だから索引をコミットでき、diff が意味を持つ。
   周辺機能（振り返る・知る）は LLM を採点や要約の補助に使ってよいが、取得と計測は決定論で行い、判断は人に残す。
3. **寿命で分ける。** 蓄積するもの（`docs/`）と揮発するもの（`work/`）を混ぜない。索引は前者だけを見る。

理由と却下案は作者の設計メモ（`docs/`・git 管理外）にある。

## LLM wiki 型との対応

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

## やらないこと

- 意味検索・ベクトル DB（recall 失敗の実例が出るまで入れない）
- LLM による索引の要旨生成・索引更新
- ノート本文・セッション内容の外部送信（ニュースの取得は GET のみ）
- ネタ帳（アイデア帳）。索引・レビュー・ニュースとは独立した機能で、テンプレの核ではない

## 地図

| 場所 | 何が入るか |
|---|---|
| `cmd/braindex` `internal/` | 索引 CLI の実装（scan → extract → render → catalog） |
| `braindex.example.json` | 設定ファイルの雛形 |
| `CONTRIBUTING.md` | 開発の決まり（テスト・決定性・持ち込まないもの） |
| `docs/` `work/` | 作者の設計メモと作業状態。git 管理外（`.gitignore`。2026-09-02 決定） |

## セットアップ

3 手で hub リポが動く。

```sh
go install github.com/pilefort/braindex/cmd/braindex@latest   # CLI を入れる
mkdir hub && cd hub && braindex init                           # hub リポの骨格を展開(既存ファイルは上書きしない)
braindex                                                       # 索引 index/catalog.md を生成
braindex review                                                # 週に 1 回: レビューの下書き work/review/<今日>.md
```

`braindex init -repo <dir>` は各プロジェクトのリポに `docs/notes/{common,project}/`・`docs/decisions.md`・`work/{APPROVALS,TODO}.md` の骨格を置く。
どちらも既存ファイルは上書きしないので、再実行しても安全。hub には週次レビューのスキル（`.claude/skills/braindex-review/SKILL.md`）も入る。

**エージェントに横断検索させる**: hub の `CLAUDE.md` には「索引を grep → 実ファイルを読む」の手順が入るが、hub の外のリポで作業している
セッションからも引かせるには、利用者のグローバル `CLAUDE.md`（Claude Code なら `~/.claude/CLAUDE.md`）に次の 3 行を足す（`<hub>` は hub の場所）:

```md
- 複数リポにまたがる知識を答える前に、`<hub>/index/catalog.md` を `grep -i <語>` で引く（記憶で答えない）
- ヒット行のパス（root 相対）の実ファイルを読む。要旨は 80 字の手がかりであって、内容の代わりではない
- 何も当たらなければ、そう言う。ノートや決定をでっち上げない
```

**定期実行**: 判断は人が行うので自動化するのは下書きの作成だけ。週に 1 回、hub で `braindex review` を動かす。
cron なら `0 9 * * 1 cd <hub> && braindex review`、Windows のタスクスケジューラなら
`schtasks /Create /SC WEEKLY /D MON /ST 09:00 /TN braindex-review /TR "cmd /c cd /d <hub> && braindex review"`。
同じ日に 2 回動いても、既にある下書きは上書きしない。

## ライセンス

MIT（`LICENSE`）。
