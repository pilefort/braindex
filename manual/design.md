# 設計の補足

[← README](../README.md) ／ [手引きの目次](README.md)

3 原則とやらないことは README「設計」。ここはその補足。

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
| （外の事実の取り込みは raw への投入） | 調査（スキル `research-distill` がサブエージェントに一次ソースで裏を取らせ、`braindex verify` で実在を再照合し、`braindex answer` で HTML にする） | 照合は CLI（GET のみ）、真偽の判断は人かエージェント。結果は出典つきで `docs/notes/` に置く |

## 版の状態

v0.1.0（2026-09-03）: 索引 CLI（Phase 1）を原型から移植して可搬化し、セットアップ `braindex init`（フォルダ規約のテンプレ同梱）・
週次レビューの集計 `braindex review`（Phase 2）・ISSUE の検査 `braindex lint`・訂正率トリガのレトロスペクティブ `braindex retro`（Phase 3）を足した。
原型は作者の私用「第二の脳」で 2026-08-07 から運用しているもの（非公開・20 リポ 307 ノートを索引中）。

タグの後（2026-09-03）に main へ入ったもの: ニュースサジェスト `braindex news`（Phase 4）・判断待ちのフォーム `braindex approvals`（Phase 5）・
ノートの曖昧さ検査 `braindex lint -kind note` と走査対象の切り出し `braindex scope`（Phase 6）・回答の HTML 化 `braindex answer` と
実在の照合 `braindex verify`（Phase 7）・定期実行の登録 `braindex schedule`。次のタグで出る。

## リポジトリの地図

| 場所 | 何が入るか |
|---|---|
| `cmd/braindex` | サブコマンドの登録とフラグ解析（`main.go`・`commands.go`・`cmd_*.go`） |
| `internal/` | 索引の実装（`scan` → `extract` → `render` → `catalog`）と `config`・`template`（init）・`lint`・`review`・`sessions`／`retro`・`feed`／`interest`／`news`（ニュース）・`approvals`・`mdhtml`／`verify`（回答の HTML 化と照合）・`scope`・`schedule` |
| `.github/workflows/ci.yml` | CI。ubuntu と windows で gofmt／vet／test に加え、同じ入力から 2 回生成してバイト一致することを確かめる |
| `manual/` | コマンドごとの手引き（この目次は [README.md](README.md)） |
| `CONTRIBUTING.md` | 開発の決まり（テスト・決定性・持ち込まないもの） |
| `docs/` `work/` | 作者の設計メモ（`overview`・`decisions`・`glossary`・`conventions`）と作業状態。public 切替の前に履歴から除去し、以後は git 管理しない（2026-09-04 決定。2026-09-03 の「git 管理する」を上書き） |

```sh
go test ./...   # 依存なし。CI は gofmt -l . と go vet ./... も回す
```
