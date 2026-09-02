# braindex

**Markdown ノートの索引を、LLM なし・依存なし・決定的に作るリポ横断の CLI。**

複数の git リポジトリに散らばった Markdown ノート（`docs/notes/`・`docs/decisions.md`）を、
コピーせずに 1 枚の索引 `catalog.md` にまとめる CLI と、その索引を中心に知識を蓄積・レビューする
フォルダ規約のテンプレート。

## 状態

v0（2026-09-02）: 索引 CLI（Phase 1）を原型から移植し、可搬化した。フォルダ規約のテンプレはこれから。
原型は作者の私用「第二の脳」で 2026-08-07 から運用しているもの（非公開・20 リポ 307 ノートを索引中）。

予定: セットアップ機能（`braindex init`）とフォルダ規約のテンプレ同梱 → 訂正率トリガのレトロスペクティブ → ニュースサジェスト（この順。2026-09-02 決定）。

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
| lint（矛盾・陳腐化の検出） | 週次レビュー（規約は `braindex init` のテンプレに同梱予定） | 検出の機械部分は CLI、判断は人 |

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
```

`braindex init -repo <dir>` は各プロジェクトのリポに `docs/notes/{common,project}/`・`docs/decisions.md`・`work/{APPROVALS,TODO}.md` の骨格を置く。
どちらも既存ファイルは上書きしないので、再実行しても安全。hub には週次レビューのスキル（`.claude/skills/braindex-review/SKILL.md`）も入る。

## ライセンス

MIT（`LICENSE`）。
