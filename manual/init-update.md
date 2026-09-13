# braindex init / update — 骨格の展開と追従

[← README](../README.md) ／ [手引きの目次](README.md)

## braindex init — 骨格の展開

hub リポ（引数なし）か各プロジェクトのリポ（`-repo <dir>`）に骨格を展開する。既存ファイルは上書きしない。

対応先は `-agent claude,codex` で選ぶ。省略時は `claude` だけ。カンマ区切りで指定し、重複と空要素は無視する。
未知の名前は候補を示してエラーにする。`-repo` と `-agent` は併用できない。

```sh
braindex init -agent codex -add conventions   # Codex 向けに索引・規約・記録の点検スキルを配る
braindex init -agent claude,codex             # 両方に配る
```

Codex には `~/.codex/AGENTS.md` の hub ごとの管理範囲と、`~/.agents/skills/<名前>/SKILL.md` を配る。
指示は `CODEX_HOME` があればその下の `AGENTS.md` に入る。スキルの置き場は `CODEX_HOME` の影響を受けない。
`AGENTS.md` は管理範囲だけを書き足し・差し替え、外側の利用者の文章は改行も含めて保持する。複数の hub は別々の範囲を使う。
指示の合計には `project_doc_max_bytes`（既定 32 KiB）の上限があるため、利用者の `AGENTS.md` が大きいと指示が途中で切れる。
読み込み場所と上限は [公式 AGENTS.md 仕様](https://learn.chatgpt.com/docs/agent-configuration/agents-md)、スキルの置き場は
[公式 Skills 仕様](https://learn.chatgpt.com/docs/build-skills) に従う（2026-09-13 確認）。

Codex のセッションログは未対応。`braindex retro`・`news`（関心）・`learn` は Claude Code のログ（`~/.claude/projects`）だけを読む。
そのため Codex に `retro` スキルは配らない。`conventions` の 3 本と `review` の `braindex-review` は、選んだ機能の分だけ配る。
`codex` だけなら hub に `CLAUDE.md`・`.claude/skills/` は作らない。README・設定と、選んだ機能の docs/・work/・news/ は共通で配る。
以下の配布物の表は既定の Claude Code 向けのもの。

news の設定を新しく書くときは、PATH に `claude` があれば LLM 補助（訳と採点）を有効にする。
見つからなければ無効で作り、導入後に `news.llm` を `claude-cli` にする案内を出す。既存の news 節は変更しない。
保持した既存ファイルは `保持(既存): N 件` の 1 行にまとめ、新しく作成・追記したファイルはそれぞれ 1 行で表示する。

hub の既定は「利用者の置き場と書き方を変えない」機能: `core`（`README.md`・`CLAUDE.md`・`.gitattributes`・`braindex.json` の
`root`／`notes_dirs`／`extra`）と `retro`・`news`・`schedule`。規約への乗り換えを迫る `conventions` と、それに依存する `review` は
`-add <機能>[,<機能>...]` で足す（2026-09-05 の入口の設計。同日の「既定は段 0」を上書き）。`-add core` なら索引の設定だけになる。
機能と配布物の対応は `braindex init -list` が出す:

| 機能 | 既定 | 配るもの | `braindex.json` に足す節 | 依存 |
|---|---|---|---|---|
| `core` | ○ | `README.md`・`CLAUDE.md`・`.gitattributes`・`braindex.json` | `root`・`notes_dirs`・`extra` | — |
| `conventions` | | `docs/`（overview・glossary・decisions・conventions・notes/）・`work/`（APPROVALS・TODO）・skill `record-lint`・`contradiction-scan`・`research-distill` | `approvals` | — |
| `review` | | skill `braindex-review`・`work/review/` | `review` | `conventions`（自動で含め、その旨を出す） |
| `retro` | ○ | skill `retro` | `retro` | — |
| `news` | ○ | `news/feeds.example.json`・`.gitignore` の news の行 | `news` | — |
| `schedule` | ○ | — | `schedule`（`jobs` は足してある review・retro・news の分） | — |
| `all` | | 上の全部 | 全部 | — |

同じ機能を 2 回足しても安全: ファイルは既存を残し、`braindex.json` は無い節だけを固定のキー順で足す（既にある値は触らない）。
`schedule.jobs` には、機能を足したときにその機能の job（`review`／`retro check`／`news fetch`）を末尾に足す。既にある job は触らず、
以前から入っている機能の job を利用者が消していても足し直さない。`.gitignore` も無い行だけを末尾に足す。
足した機能は台帳 `.braindex/template.json` の `features` に記録され、`update` の追従範囲になる。
未知の機能名は候補を出して終了コード 1。`-repo` と `-add` は併用できない。

`braindex init` が置く `braindex.json`（`"root": ".."`）が設定の雛形の正本で、braindex のリポジトリに別置きの雛形は置いていない。
手で `braindex.json` を書くなら、キーの一覧は [generate.md](generate.md) の設定の表を見る。

## 各リポに骨格を置く（任意）

```sh
braindex init -repo ../alpha   # docs/notes/{common,project}/・docs/decisions.md・work/{APPROVALS,TODO}.md
```

hub 側・リポ側とも既存ファイルは上書きしないので、再実行しても安全。
hub の判断を埋めるスキルは機能ごとに入る（`.claude/skills/` の `record-lint`・`contradiction-scan`・`research-distill` は
`-add conventions`、`braindex-review` は `-add review`、`retro` は `-add retro`）。

## エージェントに横断検索させる

Codex は `-agent codex` で `~/.codex/AGENTS.md`（`CODEX_HOME` 指定時はその下）に指示が入るので、以下の 3 行を手で足す必要はない。

hub の `CLAUDE.md` には「索引を grep → 実ファイルを読む」の手順が入るが、hub の外のリポで作業している
セッションからも引かせるには、利用者のグローバル `CLAUDE.md`（Claude Code なら `~/.claude/CLAUDE.md`）に次の 3 行を足す（`<hub>` は hub の場所）:

```md
- 複数リポにまたがる知識を答える前に、`<hub>/index/catalog.md` を `grep -i <語>` で引く（記憶で答えない）
- ヒット行のパスは `root`（hub の `braindex.json`。既定は hub の親ディレクトリ）からの相対。その実ファイルを読む。要旨は 80 字の手がかりであって、内容の代わりではない
- 何も当たらなければ、そう言う。ノートや決定をでっち上げない
```

## braindex update — 追いつかせる

hub（引数なし）か各プロジェクトのリポ（`-repo <dir>`）の雛形由来ファイルを、いま入っている braindex の版に
追いつかせ、続けて索引を再生成する。`init` が「まだ無いものを足す」のに対し、`update` は
「既にあるものを今の版にする」。CLI に機能を足しても、既に立ち上がっている hub には
スキルや雛形の改良が届かない（`init` は既存ファイルを上書きしないため）ので、その経路になる。

判定は台帳 `.braindex/template.json`（`init` が書く「配った版のハッシュ」）で行う。

対応先は台帳の `agents` に記録する。後から `init -agent codex` などで足せるが、既存の対応先は消さない。
`agents` キーの無い旧版の台帳は `claude` だけと推定し、その旨を表示して記録する。
Codex のスキルは台帳の `home` にホーム配布用キー（`agents/skills/<名前>/SKILL.md`）とハッシュを記録する。
実際の置き場は `~/.agents/skills/` で、更新の判定は下表と同じ。作成・更新・保持はホーム分を分けて表示する。
`AGENTS.md` の管理範囲はハッシュを記録せず、`-force` に関係なく毎回現在の版に差し替える。
`-dry-run` ではホームのファイル・`.new`・台帳も書き込まない。

| 現物の状態 | update の動き |
|---|---|
| 無い | 作る |
| 配った版のまま（台帳のハッシュと一致） | 今の版にする |
| 既に今の版と同じ | 何もしない |
| 利用者が編集した | **現物を残し、隣に `<名前>.new` を置く** |
| 利用者が編集した `braindex.json`・`.gitignore` | 無い節・行だけ足す（「追記」）。`-force` でも上書きせず、`root` や利用者の行は消えない。`braindex.json` は、足したあとも雛形の節の中にしか無いキーが残るときだけ `.new` を置き、そのキーを出力に添える。揃っていれば「そのまま」（終了コード 2 にしない）。`.gitignore` は `.new` を置かない |

hub のファイルでは、`braindex.json` と `.gitignore` が例外になる（決定 2026-09-05 → 下の「決めたこと」。`root` や利用者が足した行を消さないため）:

| ファイル | `init` | `update` | `update -force` |
|---|---|---|---|
| `braindex.json` | 無ければ作る（あれば触らない） | 無い節を足す。足しても雛形にしか無いキーが残れば `.new` | **上書きしない。** 無い節を足すだけ |
| `.gitignore` | 無ければ作る（あれば触らない） | 無い行を足す。`.new` は置かない | **上書きしない。** 無い行を足すだけ |
| それ以外（README・CLAUDE.md・skill・docs/work の雛形など） | 無ければ作る（あれば触らない） | 配った版のままなら今の版にする。編集済みなら `.new` | 編集済みでも今の版で上書きする |

台帳は**1 ファイル書くごとに保存する**（2026-09-06）。最後にまとめて保存すると、途中で書き込みに失敗したときに
「書いたのに台帳に無い」ファイルができ、次の `update` がそれを「利用者が編集した」と見て `.new` を置いてしまう。

追従するのは台帳の `features`（`init -add` で足した機能）の分だけで、足していない機能のファイルは作らない。
`features` の記録が無い hub（機能の仕組みが入る前の `init` で作ったもの）は、存在するファイルと `braindex.json` の節から
足してある機能を推定して台帳に書き、その旨を 1 行出す。

台帳を持たない hub は、既存ファイルの素性が分からないので「編集済み」の側に倒す。ただし現物が今の版と同じなら
「そのまま」になるので、`.new` が置かれるのは現物と今の版が食い違うファイルだけになる。`.new` の中身を見て、
要るところだけ自分のファイルに写す。`-dry-run` は何も書かずに変更点だけを出し、`-force` は編集済みも上書きする（`braindex.json`・`.gitignore` は除く）。
台帳の `features` に今の版が知らない機能名があれば（新しい版の braindex が足したもの）、その機能は追従せず、stderr に「先に `go install` で更新すること」と警告する。

取り込みは 2 手になる。**バイナリの更新は `update` の担当ではない**——実行ファイルの入れ替えは Go のツールチェーンが受け持ち、`update` は hub の中身だけを見る。

```sh
go install github.com/pilefort/braindex/cmd/braindex@latest   # 1. バイナリ。@latest はタグに解決する
braindex update                                               # 2. 雛形の追従＋索引の再生成
```

**`braindex.json` が変わるときは版差に注意する。** 新しい節の入った設定を古い版の braindex で読むと、
未知のキーはエラーなので索引生成を含む全コマンドが止まる。複数のマシンで使っているなら、
先にすべてのマシンの braindex を更新する（この場合 `update` は警告を出す）。

## 決めたこと

### Codex 対応は導入・索引・規約・スキルまでとし、Codex のログは未対応と案内する

記録日: 2026-09-13
理由: 索引と規約は共通で使えるが、セッションログを読む機能は Claude Code の形式に依存する。未対応の範囲を導入時にも明示する。
根拠: 同日の実装 SPEC §1・§3b・判断済み 1（ユーザー提示）。

### Codex 向けの指示ファイル `AGENTS.md` は `braindex init` が配る

記録日: 2026-09-13
理由: 利用者が指示を手で転記せずに導入できる。本文は同梱の `CLAUDE.md` と同じ元から作り、管理範囲だけを更新する。
根拠: 同日の実装 SPEC §3a・判断済み 2（ユーザー提示）。

### `init` は配る先（Claude／Codex）を選ばせ、既定は Claude だけにする

記録日: 2026-09-13
理由: 従来の導入方法を保ち、Codex の共通設定への配布は選んだときだけにする。
根拠: 同日の実装 SPEC §1（ユーザー提示）。

### Codex 向けの指示は `~/.codex/AGENTS.md` に書く

記録日: 2026-09-13
理由: hub 以外のリポジトリで作業する Codex にも索引を引く指示を読ませる。`CODEX_HOME` を指定した場合はその場所に従う。
根拠: 同日の実装 SPEC §3a と [公式 AGENTS.md 仕様](https://learn.chatgpt.com/docs/agent-configuration/agents-md)（同日確認）。

### Codex 向けのスキルは `~/.agents/skills/<名前>/SKILL.md` に置く

記録日: 2026-09-13
理由: Codex が個人スキルを読む場所に合わせる。以前の `~/.codex/skills` への配布の決定を覆す。
根拠: 同日の実装 SPEC §3b・§3c と [公式 Skills 仕様](https://learn.chatgpt.com/docs/build-skills)（同日確認）。

記録日・理由・根拠は `docs/decisions.md` にあった当時の記録のまま。

### `braindex.example.json` を消して、設定の雛形の正を `braindex init` の埋め込みテンプレ 1 つにするか → A. example json を消し、README の使い方 2 を「`braindex init` で雛形を展開」に変え、地図の行を削る

記録日: 2026-09-03
理由: 利点: 正が 1 つ／欠点: リポを眺めるだけの人が設定の雛形を読めない（テンプレのパスは深い）（選択肢の記載どおり・ユーザー補足なし）。却下: B. 両方残し、README に「同じ内容」と書き、両者が一致することを検査するテストを足す
根拠: 会話 2026-09-03（ユーザー判断・承認待ち HTML の回答）。選択肢と得失は当時の `work/APPROVALS.md`（このリポでは git 管理外。却下案は理由行に転記済み）

### `braindex update` は hub 資産の追従と索引の再生成をまとめて行う（バイナリの更新は含めない）

記録日: 2026-09-04
理由: CLI に足した機能が、既に立ち上がっている hub にはテンプレ・スキル・設定の節として届かない（`init` が既存ファイルを上書きしないため）。この追従を担うコマンドを新設し、索引の再生成まで 1 回で済ませて「実行すれば hub が最新になる」状態にする。バイナリの再インストールは `go install` の担当のままにし、取り込みは `go install` と `braindex update` の 2 手にする。**技術的に不可能だからではない**——Windows でも実行中の braindex.exe は `go install` で置換できると実測したので、含めない理由は担当範囲の切り分けだけ。却下: 資産の追従のみ（索引を別に叩く手間が残る）／バイナリの再インストールまで含める（ユーザー判断で B を採った）
根拠: 会話 2026-09-04（ユーザー判断・承認フォームの回答 B）／追従の穴は `internal/template` の `Install`（既存ファイルを上書きしない）と hub テンプレ 17 ファイル（`internal/template/templates/hub/`）——立ち上がり済みの hub では README・CLAUDE.md・`braindex.json`・`docs/` 3 種などが「既存」としてスキップされ、改良が届かない／実行中の置換の実測は `docs/notes/common/scaffold-update-prior-art.md`（2026-09-04・Windows 11 Home 26200・go1.26.5）

### `braindex update` はテンプレの内容ハッシュで「利用者が編集したか」を見分ける

記録日: 2026-09-04
理由: `init` の「利用者の編集を壊さない」原則を保ったまま、未編集のファイルだけを自動で追従させるため。`init` の時点でテンプレの内容ハッシュを hub に記録し、一致すれば上書き、一致しなければ隣に `.new` を置いて報告する。記録を持たない既存の hub は、素性が分からないので「編集済み」の側に倒す（2026-09-04 追記: ただし現物が今の版と同じなら「そのまま」になるため、`.new` が置かれるのは現物と今の版が食い違うファイルだけ。実装後の実測で確認した）。却下: 記録を持たず常に `.new` を置く（触っていないファイルまで毎回増え、追従の自動化にならない）／既定で上書きし `-keep` で退避する（`init` の原則と矛盾し、利用者の編集を既定で壊す）
根拠: 会話 2026-09-04（ユーザー判断・承認フォームの回答 A）／`init` の非上書き仕様は `internal/template/template.go` の `Install`／`.new` が出る条件は `internal/template/update.go` の判定順（`bytes.Equal` を台帳より先に見る）と `TestUpdate_Unchanged`・`TestUpdate_NoLedgerTreatsExistingAsEdited`

### `braindex.json` の未知キーはエラーのまま据え置き、`update` は「先に全マシンの braindex を更新せよ」と警告する

記録日: 2026-09-04
理由: 打ち間違い（`notes_dir` のような綴り違い）を無言で無視しない既存の設計を優先した。代償として、新しい版が `update` で節を足した hub は古い版のマシンで読めず、索引生成を含む全コマンドが止まる——そこは `update` の警告で伝える。却下: 未知のキーを警告に緩めて読み飛ばす（打ち間違いの検出が消える）／未知の節は警告・節の中の未知キーはエラーの非対称（規則が 2 段になり README での説明が要る。Claude の推奨案だったが採らなかった）
根拠: 会話 2026-09-04（承認フォームの回答 A）／現状の実装は `internal/config` の `DisallowUnknownFields`（テストあり）／`update` の担当範囲は同日の決定「hub 資産の追従＋索引の再生成」

### `braindex init` は段 0（索引の設定だけ）を配り、機能は `braindex init -add <機能>` で 1 つずつ足す

記録日: 2026-09-05
理由: 「段階的に取り込める」を既定の振る舞いにするには、最初の 1 手が梯子の段 0 でなければならない。今の `init` は `docs/`・`work/`・skill 5 本・全節入りの
`braindex.json` を一括で配り、索引だけ欲しい人にも規約の乗り換えを迫る。`-add review|retro|news|learn|schedule`（と `-add all` で従来の 1 手）で機能ごとに
設定の節・skill・置き場を足し、`update` は足した分だけ追従させる。却下: 一括のまま README で「使わない設定は消してよい」と案内（最初に触った人が離脱する）／
`-minimal` を足すだけ（既定が一括のままで「段階的」が既定にならない）（`learn` は配布物が無いため機能名にしなかった。実装は `internal/template/feature.go`）
根拠: 会話 2026-09-05（ユーザー判断・承認フォームの回答 A）／梯子の定義は README「段階的な取り込み」／設計は `work/SPEC-adoption.md`

### 足した機能は台帳 `.braindex/template.json` の `features` に記録し、`update` はその分だけ追従する。記録の無い hub は存在するファイルから推定する

記録日: 2026-09-05
理由: `init -add` で機能を選べるようにした以上、`update` が全機能のファイルを作ると「足していない機能が勝手に入る」ことになり、段階的な取り込みが
`update` の 1 手で崩れる。何を足したかは配った側（braindex）しか知らないので、配った版のハッシュと同じ台帳に持つ。機能の記録を持たない旧版の
台帳（キー無し）と段 0 の hub（空配列）は区別し、前者だけ「機能の配布物か設定の節が 1 つでもあればその機能は足してある」と推定して台帳に書く。
却下: 推定だけで台帳に持たない（利用者が消したファイルを update が毎回復活させる）／記録の無い hub は core 扱い（旧 hub の skill や設定が追従から外れる）
根拠: 設計は `work/SPEC-adoption.md`（2026-09-05）。実装は PR #1（控え: pr/1.md）0（控え: pr/10.md）5（控え: pr/105.md）（機能の対応表）・#106（`init -add`）・#107（`update` の追従と推定）

### `braindex.json` の雛形は 1 枚のまま最上位キーごとに切り出し、機能の分だけ固定キー順で組み立てる。`extra` は core、`approvals` は conventions に属する

記録日: 2026-09-05
理由: 設定の雛形を機能ごとの断片に分けると正本が増え、片方だけ直す事故が起きる（2026-09-03「設定の雛形はテンプレ 1 つ」と同じ理由）。
1 枚から節を取り出して足せば、雛形の整形を組み立て結果と一致させるテスト 1 本で正本の一意性を保てる。`extra` は索引の走査設定なので段 0 に含め、
`approvals` は `work/APPROVALS.md` と `docs/decisions.md` を配る conventions に付ける（SPEC の表に無かった 2 キーの実装時判断）。
却下: 機能ごとの JSON 断片を別ファイルで持つ（正本が 7 枚になる）／`extra` を別機能にする（索引の設定が 2 段に割れる）
根拠: `internal/template/config.go`・`TestBuildConfig_AllEqualsTemplate`（PR #1（控え: pr/1.md）0（控え: pr/10.md）5（控え: pr/105.md））／SPEC は `work/SPEC-adoption.md`

### `braindex init` の既定は「利用者の置き場を変えない」機能（core・retro・news・schedule）にし、`-add` で選ばせるのは conventions と review だけにする

記録日: 2026-09-05（同日の「`init` は段 0 だけを配る」を上書き。消さない）
理由: retro（材料はセッションログ）・news（材料はフィード URL）・schedule は設定の節と skill を足すだけで、利用者の既存の置き場・書き方を変えない。
「乗り換え」を迫るのは conventions（と、それに依存する review）だけなので、そこだけを opt-in にすれば「段階的」の目的は満たせる。
`schedule.jobs` は news の分（`news fetch -layer daily -no-open`・毎日 07:30）も持ち、機能を足したときにその job を末尾に足す（既にある job は触らず、
以前からある機能の job を利用者が消していても足し直さない）。却下: 段 0 だけ（索引しか要らない人には正しいが、retro・news を単品で欲しい人が `-add` を 3 回打つ）／
単品は主経路に出さない・3 入口の案（最初の相談の案。撤回）。`-add core` で段 0 だけにもできる
根拠: 会話 2026-09-05（2 回目の相談。ユーザー判断「セッションログ読み取りと、フィード取得、定期実行はやりたい人がいそう」「そこまでは braindex init に入れて良い」）／
`work/ISSUE-adoption.md`「入口の設計」／実装は PR `adopt/default-entry`（`template.DefaultFeatures`・`addScheduleJobs`）

### `update` は、編集済みの `braindex.json` に節を足したあと雛形にしか無いキーが無ければ `.new` を置かず終了コード 2 にもしない

記録日: 2026-09-05（2026-09-04「編集済みは `.new` を置く」を `braindex.json` について狭める）
理由: 最上位の節は `update` が足すようになったので、`.new` が要るのは「節の中に雛形の新しいキーがある」ときだけになった。テンプレの `root` は `..` で
実運用の hub はほぼ全部が「編集済み」なので、そのままだと `update` のたびに `.new` と終了コード 2 が出て定期実行の判定にならない。
却下: 現状維持（毎回 diff して消す）／`.new` は置くが終了コードだけ変える（`.new` のごみが残る）
根拠: PR #1（控え: pr/1.md）0（控え: pr/10.md）7（控え: pr/107.md） のレビュー実測（`root` を直しただけの hub で毎回 `.new`・`diff` は 1 行）／会話 2026-09-05（ユーザー判断・承認フォーム #2）／実装は PR `adopt/review-decisions`

### `update -force` でも `braindex.json` と `.gitignore` は節・行を足すだけにし、`root` と利用者の行を消さない

記録日: 2026-09-05
理由: `-force` の目的は「`.new` を出さずに雛形へ揃える」で、`root` の破壊は目的に含まれない。`root` が `..` に戻ると別の親を指した hub は索引が出せなくなる。
却下: 文字どおり全部上書き（手引きに「root も戻る」と注意を書く）
根拠: PR #1（控え: pr/1.md）0（控え: pr/10.md）7（控え: pr/107.md） のレビュー実測（`-force` で `.gitignore` の `*.tmp` と `root` が消える）／会話 2026-09-05（ユーザー判断・承認フォーム #3）／実装は PR `adopt/review-decisions`

### 台帳の `features` に今の版が知らない機能名があれば `update` が警告する（終了コードは変えない）

記録日: 2026-09-05
理由: 新しい版が記録した機能を古い版で `update` すると、その機能は無言で追従されない。2026-09-04「`braindex.json` の未知キーは `update` が警告する」と同じ趣旨で、
取り込み漏れを隠さない。名前は台帳に残す（PR #107 のレビューで修正済み）。却下: 何も出さない
根拠: 会話 2026-09-05（ユーザー判断・承認フォーム #4）／実装は PR `adopt/review-decisions`

### hub テンプレの README はニュース（`news`）と学習（`learn`）の節も持つ

記録日: 2026-09-05
理由: SPEC「文書」節の要求「hub テンプレは全機能の説明を持ち、未導入は `init -add` で足すと書く」に対し、news・learn の節が無かった（#108 で「全機能の説明を持つ」と
明言したので食い違いになった）。却下: 冒頭の文を「機能の有無で分けていない」に弱める（news を足した人がテンプレから使い方に辿れない）
根拠: PR #1（控え: pr/1.md）0（控え: pr/10.md）8（控え: pr/108.md） のレビュー（`internal/template/templates/hub/README.md` の節の実測）／会話 2026-09-05（ユーザー判断・承認フォーム #5）／実装は PR `adopt/review-decisions`

### `braindex.json` の未知の節はエラーのまま。警告に緩めない（2026-09-04 の再確認）

記録日: 2026-09-06
理由: 2026-09-04 の「未知キーはエラー」を覆すかの再提案で、覆さないと決めた。緩める根拠に挙げた「複数マシンで braindex の版がずれ、新しい版が足した節を古い版が読めずに止まる」は、まだ観測していない推測。緩める方向は後からでも安全に動かせるが、厳しくする方向は既存の設定を壊すので、観測してから緩めればよい。却下: 最上位の未知の節だけ警告（終了コード 2）に緩める案——版ズレで止まらなくなる代わりに、書き間違えた節名が黙って無視される。
根拠: 会話 2026-09-06（ユーザー判断・承認フォームの回答）／覆す対象は `docs/decisions.md` 2026-09-04「`braindex.json` の未知のキーはエラー」／設計レビュー 2026-09-06 M9
