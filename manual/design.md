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

## 決めたこと

記録日・理由・根拠は `docs/decisions.md` にあった当時の記録のまま。

### module パスは `github.com/pilefort/braindex` のまま維持する（相対 import にはしない）

記録日: 2026-09-02
理由: Go の module モードでは相対 import はコンパイルエラーで、最も近い形は `module braindex` だが、それだと `go install github.com/pilefort/braindex/cmd/braindex@latest` の一行導入が通らなくなる。owner 名はリポ URL で既に公開されているので、コード内の module パスは追加の識別子漏れにならない。却下案: `module braindex`（README の導入手順を clone → `go install ./cmd/braindex` に書き換える代償が大きい）。
根拠: PR #1（控え: pr/1.md） のユーザーコメント「import パスを相対に」と、そのレビュー対応コメント（https://github.com/pilefort/braindex/pull/1 ・2026-09-02。返答後に #1 はそのままマージされた）／Go の `go build` のエラー文「relative import paths are not supported in module mode」（レビューの実測 2026-09-02）／会話 2026-09-02（ユーザー指示「軽微なものは必要かどうかを判断して明確に必要な理由がなければ削除」に基づく Claude の判断。変えるなら go.mod と README の install 行）

### go.mod の `go 1.26` は下げない

記録日: 2026-09-02
理由: テストが `t.Chdir`（Go 1.24 以降）を使うので 1.18 までは下げられない。1.26 は記録日時点でサポート中のリリースで、古い toolchain でも `go install` 時に自動で取得される（`GOTOOLCHAIN=auto` の既定）ので、下げて得られる利点（自動取得の回避）は小さい。却下案: `go 1.24` に下げる（以後 1.25・1.26 の機能を使うたびに上げ直す手間が増える）。
根拠: `cmd/braindex/main_test.go` の `t.Chdir`（PR #2（控え: pr/2.md） レビュー対応・2026-09-02）／Go の toolchain 自動選択 https://go.dev/doc/toolchain ／PR #1（控え: pr/1.md） レビュー 3 節「任意」の実測（`go 1.18` でもビルドは通る・2026-09-02）／会話 2026-09-02（同上・Claude の判断）

### README の節は「構成 → 何をするか → セットアップ → コマンド → 設計 → 開発」の順に置く

記録日: 2026-09-03
理由: 初見が上から読める順にする。コマンドを足すときは `## コマンド` の下に `### braindex <名前> — <1 行>` を 1 つ増やし、先頭の入出力の一覧表に 1 行足すだけで済む形にした。旧構成はリファレンスが先に来て概念が後ろに埋まり、インストール手順が「使い方」と「セットアップ」の 2 か所に分かれて食い違いはじめていた。
根拠: PR #4（控え: pr/4.md）6（控え: pr/46.md）（2026-09-03 マージ・`README.md`）／旧 README の全行を新 README と突き合わせて脱落なしを確認した実測（同 PR 本文）

### 終了コードは 0 成功 / 1 失敗 / 2 警告つき完了に揃える（3 は retro check の閾値超えと approvals serve の時間切れだけ）

記録日: 2026-09-03
理由: 「2 なら結果は使える」が全コマンドで成り立てば、定期実行から一律に成否を判定できる。時間切れ（`approvals serve`）とスケジューラの失敗（`schedule`）は完了していないので 2 から外し、未反映がある `approvals apply` と、選別・統計を取り込めなかった `news fetch` を 2 に寄せた。却下: 現状維持／コマンド群ごとに別の規約を認める（規約の意味が薄まる）。
根拠: 会話 2026-09-03（ユーザー判断・承認待ちフォームの回答）。実装は PR #5（控え: pr/5.md）4（控え: pr/54.md）。指摘の出どころは当時の PR #35・#36・#37・#49（控え: pr/35.md・pr/36.md・pr/37.md・pr/49.md）・#5（控え: pr/35.md・pr/36.md・pr/37.md・pr/49.md・pr/5.md）0（控え: pr/35.md・pr/36.md・pr/37.md・pr/49.md・pr/50.md） のレビュー記録（2026-09-03・除去済み。コードの読みで、実測ではない）。規約の文言は `cmd/braindex/main.go` のパッケージコメントと `docs/overview.md`

### 生成物に載せるリンクは http(s) だけにする（スキームを持たない相対パス・#見出しは通す）

記録日: 2026-09-03
理由: `javascript:` や `data:` は開いただけでコードが動く。フィード由来のリンクは他人が書いたもので、`news/keep/` は git 管理の蓄積側なので、生成の入口で落とすのが一番安全。スキームの無いリンクまで落とすと文書内リンクが壊れるので、そこは通す。却下: 危険なスキームだけ落とす（新しいスキームを追いかけ続けることになる）／表示だけの `answer` を緩める／現状維持。
根拠: 会話 2026-09-03（ユーザー判断・承認待ちフォームの回答）。実装は PR #5（控え: pr/5.md）6（控え: pr/56.md）（判定は `internal/weblink`）。指摘の出どころは当時の PR #38・#48（控え: pr/38.md・pr/48.md）・#4（控え: pr/38.md・pr/48.md・pr/4.md）9（控え: pr/38.md・pr/48.md・pr/49.md） のレビュー記録（2026-09-03・除去済み）。実測（`[x](javascript:alert(1))` がそのまま `href` になる）は `docs/notes/project/review-evidence-2026-09.md`「`javascript:` リンクがそのまま `href` になっていた」

### README は入口だけ（何であるか・セットアップ・段階的な取り込み・コマンド一覧・設計の要点）にし、コマンドの詳細は `manual/` に 1 ページずつ分ける

記録日: 2026-09-05
理由: README が 700 行を超え、初見が「何であるか」と「最初の 1 手」に辿り着く前にリファレンスに埋もれていた。2026-09-03 の「構成 → 何をするか → セットアップ → コマンド → 設計 → 開発」の順は入口の中で保ち、
「何をするか」は段階的な取り込みの表に吸収する。詳細の置き場を `docs/` にしないのは、`docs/` を git 管理から外す決定（2026-09-04）があり公開物にできないため。
コマンドを足すときは README の一覧表に 1 行と、`manual/` の該当ページ（無ければ 1 ページ）を足す。却下: README を長いまま目次で補う（目次があっても本文の量は減らない）
根拠: 会話 2026-09-05（ユーザー指示「文章が多すぎる。多くなりそうなら markdown ファイル自体を分けて欲しい」）／旧 README の全行を新 README と `manual/*.md` に突き合わせ、脱落が見出しの改名と意図した要約だけであることを確認した実測（PR 本文）

### 窓の 0 時とタイムゾーンの解決は `cmd/braindex/loc.go` の `localLoc` 1 か所に集める

記録日: 2026-09-06
理由: 「その日の 0 時」を作る場所が 3 か所に散っていて、`retro` はローカルの 0 時、`interest.Build` と `learn` は `time.Parse`（＝UTC）の 0 時になっていた。同じ「直近 14 日」が機能ごとに別の日を指し、両方を定期実行に載せると食い違う（決定 2026-09-03 で揃えたはずが、`interest` の中で解き直していて効いていなかった）。`interest.Build` は窓を時刻の引数（`Since`・`Until`）で受け取り、日付から時刻を作るのをやめる——ライブラリの中で環境（タイムゾーン）を読むと、呼び出し側が何を渡しても結果が環境で変わる。`learn` は `interest` に渡したのと同じ窓をそのまま使う。
根拠: 設計レビュー 2026-09-06 M3(b)（`docs/notes/project/design-review-2026-09-06.md`）／揃える決定は `docs/decisions.md` 2026-09-03（窓の起点はローカルの 0 時）／実装は `cmd/braindex/loc.go`・`internal/interest/profile.go`、境界の判定は `TestBuild_窓は渡された時刻で切る`（PR `review/m3b-tz-windows`）

### `retro`・`news`・`learn` が数えるセッションは、既定で `root` 配下で交わしたものだけにする

記録日: 2026-09-06
理由: `~/.claude/projects` には、索引に載らないリポジトリでの会話も、OS のシステムディレクトリで動かしたときの会話も混ざる。全部数えると、訂正率も関心プロファイルも「その人が braindex で扱っている範囲」の外の影響を受け、率が動いた理由を追えない。判定はセッションの最初の `cwd` が `root` の配下かどうか（`filepath.Rel` が `..` で始まらない。Windows では大文字小文字を無視する）。`cwd` がログに無いセッションは、置き場のディレクトリ名（区切りをハイフンに潰したもの）しか分からず元のパスに戻せないので、配下かどうか判定せず除く。除いた件数は「外」と「作業ディレクトリ不明」で 1 行ずつにまとめる（セッションごとに出すと数百行になる）。全部数える口は設定 `retro.all_projects` と `-all-projects`。`root` が決まらないときは絞らない——設定ファイルがあって `root` が無いときだけ警告し、設定ファイルを使わず `-sessions` だけで回すときは黙って全部数える（hub を持たない使い方は正当なので、毎回警告を出さない）。
根拠: 設計レビュー 2026-09-06 M2（`docs/notes/project/design-review-2026-09-06.md`）／`work/TODO.md` にあった「システムディレクトリの行が `retro stats -by project` に出る」はこれで閉じる／実装は `internal/sessions`（`Options.UnderRoot`）と `cmd/braindex/loc.go` の `sessionRoot`、判定表は `TestUnderRoot`（PR `review/m2-scope`）

### 定型（機械が流し込んだ指示）の判定は読み取り層に置き、`retro`・`news`・`learn` で共有する

記録日: 2026-09-06
理由: 同じ冒頭の発話が何セッションにも現れるものは、人が打った発話ではなく機械が流し込んだ指示（定期実行・スクリプト・貼り付けたテンプレ）で、数えると訂正率も関心プロファイルも歪む。判定は `learn` だけが持っていたので、同じログから `retro` は定型を数え、`learn` は数えないという食い違いが起きていた。`sessions.MarkBoilerplate` に移して `Turn.Boilerplate` を立て、読み取りの最後に必ず掛ける。どこから読んでも同じ発話が定型になる。あわせて、braindex 自身が `claude -p` に渡すプロンプトの先頭に `[braindex-news]` の印を置き、`ExcludeReason` が `[braindex-` で始まる発話を除くようにした——`claude -p` のプロンプトは相手側のセッションログに人間の発話として残るので、印が無いと braindex が自分で作った発話を自分で数えてしまう。発話の通し番号（`Turn.Index`）は振り直さない。定型かどうかは数える側の判断で、ログの中での位置は変わらないため。
根拠: 設計レビュー 2026-09-06 M11（`docs/notes/project/design-review-2026-09-06.md`）／判定の由来（冒頭 120 字・3 セッション・40 字未満は除く）は `docs/decisions.md` 2026-09-05 の learn の閾値／実装は `internal/sessions/boilerplate.go`、判定表は `TestBoilerplateKey`・`TestMarkBoilerplate`（PR `review/m11-boilerplate`）

### README の画面は、架空のサンプルから opt-in のテストで撮り直せるようにする

記録日: 2026-09-09
理由: 公開リポなので実際のノート・フィード・判断は写せず、かといって手で撮った画像は画面が変わると古くなる。
サンプルデータと撮影手順をコミットしておけば、誰でも同じ画像を作り直せて、中身に個人識別子が入る余地も無い。
却下: A. 手で撮って貼るだけ（作り直せず、変更のたびに古くなる）／B. CI で毎回撮る（ブラウザの導入が要り、
画像の差分がコミットのたびに出る）。画像は `assets/screenshots/`、生成は `BRAINDEX_SCREENSHOT=1 go test
./internal/screenshots`（既存のブラウザ検査と同じ opt-in の型）。
根拠: 会話 2026-09-09（利用者の依頼「どういう見た目で出るのか README に貼っておいて」）／実装は
`internal/screenshots/`・PR #155（控え: pr/155.md）
