# 変更履歴

版ごとに、利用者に見える変更（足したコマンド・設定・出力・終了コード）を書く。内部の整理は書かない。
日付は git のコミット日。詳しい説明は各コマンドの [`manual/`](manual/README.md)。

## 未リリース（v0.2.0 以降・2026-09-11〜2026-09-13）

### 足したもの
- **`braindex related`** — いまのリポ・ISSUE・直近のセッション・引数の語から、関連するノートを「このリポ」「他のリポ」に分けて並べる。索引の生成が `index/links.tsv`（ノート同士のつながり）を一緒に書くようになった
- **`braindex type`（suggest・apply）** — ノート本文の「種別:」1 行（失敗・手順・観測）を後付けし、`search -type`・`scope -type` で絞れる
- **`braindex explain`** — 解説の Markdown と隣の `.svg` から、目次・図・表から描いたグラフつきの自己完結 HTML を作る
- **`braindex verify session`** — 「テストを通した」などの発言に、対応するコマンドの実行がセッションログにあったかを照合する
- **`braindex -check`** — 索引を書かずに最新かを返す（0 最新／3 古い）
- **`braindex init -agent codex`** — `~/.codex/AGENTS.md` と `~/.agents/skills/` に配る。retro・news・learn が読むログは Claude Code のまま
- **`braindex approvals wait`・`approvals hook`** — フックが開いたフォームの回答を待ち、`apply` が反映の結果を残す
- **news** — 選別画面から取材先・検索語の候補を登録し `apply` が `feeds.json` に追記する。一般ニュースの層 `general`。LLM 補助の実行時間の上限 `news.llm_budget_sec`（既定 600 秒）と keep 履歴を遡る月数 `news.keep_months`
- **learn** — 4 つの閾値を設定 `learn` 節で変えられる
- **schedule** — cron の出力を hub のログに残し、Windows のタスク名にパスのハッシュを足す（同名 hub の衝突を避ける）
- **sessions** — uuid・parentUuid・usage・ツール呼び出しの結果・サブエージェントのログを読む。確認済みの Claude Code の版を 2.1.270 まで上げた

### 変えたもの
- 走査: `extra` の `exclude` を他の規則で拾ったファイルにも効かせ、末尾スラッシュ（`drafts/`）も効く。置き場の中のドットのフォルダは索引に載せない
- `scope`: `-topic` と `-repo` を一緒に使える。結果の JSON に `n_chunks`
- `lint -kind note`: 「なぜ欠落」を「理由:」行か「理由」の見出しの有無で判定。コードフェンスの中は見ない。最終更新が今日なら「HEAD と同じ」を出さない
- `verify`: url の 404/410 以外の失敗は ERROR にする。引用の改行・タブで 1 件 1 行が崩れない
- `init`: `claude` があるときだけ news の LLM 補助を有効にする。再実行で保持した既存ファイルは件数にまとめる
- 書き込みはすべて原子的にし、`answer`・`explain`・`retro extract`・承認の回答 JSON は本人だけが読める権限で書く
- `approvals serve`: Origin ヘッダの無い POST を拒否。同じ回答を 2 回反映しても保留行を重ねない
- `review`: `-stdout` と `-out` の同時指定を拒否。`-since` の未来日付と取りこぼしを直す
- 文書と実装の食い違い 16 件を直した（矛盾検査 2026-09-13）

## v0.2.0（2026-09-09）

news・approvals・answer/verify・lint -kind note・scope・schedule と、`docs/`・`work/` の分離。

### 足したもの
- **`braindex news`**（fetch・apply・profile・suggest・overview・reading） — RSS/Atom を GET で取り、関心プロファイル（索引・セッション・`news/keep/`）で採点し、選別 UI の HTML を出す。概要の仕分け、保存した記事との相談、関心外からの日替わりの拾い上げ、claude CLI 経由の翻訳と採点（opt-in）
- **`braindex approvals`**（serve・apply・status・hook） — `work/APPROVALS.md` をフォームにして 127.0.0.1 で 1 回だけ配信し、回答を `docs/decisions.md` に 3 段で追記する
- **`braindex answer`** — Markdown を自己完結 HTML にして開く。`-append` で話題ごとのスレッド
- **`braindex verify`** — GitHub リポ・arXiv ID・URL・逐語引用の実在を GET で照合
- **`braindex lint -kind note`** — 出典なき主張・日付なし・曖昧な数量詞・なぜ欠落・未定義用語の検査（`-glossary`・`-json`）。decisions の失効行も検査
- **`braindex scope`** — 横断の矛盾検査の走査対象を索引から列挙・絞り込み・chunk 分割
- **`braindex schedule`** — 定期実行を OS のスケジューラ（cron・schtasks）に登録
- **`braindex search`** — 本文を語の一致で検索し、出典位置と確認できなかった範囲を出す
- **`braindex diagnose`** — 何を見に行き、何が読めて、索引が何を取りこぼしているかを出す
- **`braindex learn`**（answer） — 関心プロファイルと訂正の文脈から学習の提案を決定論の 3 信号で出し、候補に「既知・不要・後で」を返せる
- **`braindex update`** — 台帳 `.braindex/template.json` の分だけ雛形を追従（編集済みは `.new`）
- **`braindex -version`**
- 設定 `repo_depth`（`<group>/<name>` の 2 段）。`index/changes.json`（本文だけの変更を内容ハッシュと観測日で記録）
- hub テンプレの skill: record-lint・contradiction-scan・research-distill

### 変えたもの
- `init` の既定を「利用者の置き場を変えない」機能（core・retro・news・schedule）にし、`-add <機能>` で足す。`-add conventions` が唯一の乗り換え
- decisions の索引行を最新の記録日・件数・末尾 1 件にする。走査で読めなかった範囲を索引の先頭に記録する
- `retro check` は閾値超えかつ基準期間からの上振れで鳴らす。retro・news・learn が数えるセッションは既定で root 配下だけ
- 終了コードを共通化: 0 成功／1 失敗／2 警告つき完了／3 は `retro check`・`approvals serve`・`approvals wait` だけ
- CI に macOS を足した
- `docs/`・`work/` をこのリポの追跡対象から外した（作者の設計メモは別の private リポ）

## v0.1.0（2026-09-03）

索引 CLI とその周辺（Phase 1〜3）。

- **`braindex`** — 複数リポの `docs/notes/` と `docs/decisions.md` を 1 枚の `index/catalog.md` にまとめる。同じ入力なら常にバイト一致・LF 固定
- **`braindex init`** — hub と各リポ（`-repo`）にフォルダ規約の骨格を展開。既存ファイルは上書きしない
- **`braindex review`** — 週次レビューの下書き（索引の増減・差分ファイル・放置 TODO・アーカイブ候補）
- **`braindex lint`** — `work/ISSUE-*.md` の形の検査
- **`braindex retro`**（stats・check・extract） — Claude Code のセッションログから訂正率を計り、閾値超えで終了コード 3
- CI: ubuntu / windows・gofmt・vet・test・2 回生成のバイト一致
