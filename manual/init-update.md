# braindex init / update — 骨格の展開と追従

[← README](../README.md) ／ [手引きの目次](README.md)

## braindex init — 骨格の展開

hub リポ（引数なし）か各プロジェクトのリポ（`-repo <dir>`）に骨格を展開する。既存ファイルは上書きしない。

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

| 現物の状態 | update の動き |
|---|---|
| 無い | 作る |
| 配った版のまま（台帳のハッシュと一致） | 今の版にする |
| 既に今の版と同じ | 何もしない |
| 利用者が編集した | **現物を残し、隣に `<名前>.new` を置く** |
| 利用者が編集した `braindex.json`・`.gitignore` | 無い節・行だけ足す（「追記」）。`braindex.json` は加えて `.new` も置く（節の中の新しいキーは足さないため）。`.gitignore` は `.new` を置かない |

追従するのは台帳の `features`（`init -add` で足した機能）の分だけで、足していない機能のファイルは作らない。
`features` の記録が無い hub（機能の仕組みが入る前の `init` で作ったもの）は、存在するファイルと `braindex.json` の節から
足してある機能を推定して台帳に書き、その旨を 1 行出す。

台帳を持たない hub は、既存ファイルの素性が分からないので「編集済み」の側に倒す。ただし現物が今の版と同じなら
「そのまま」になるので、`.new` が置かれるのは現物と今の版が食い違うファイルだけになる。`.new` の中身を見て、
要るところだけ自分のファイルに写す。`-dry-run` は何も書かずに変更点だけを出し、`-force` は編集済みも上書きする。

取り込みは 2 手になる。**バイナリの更新は `update` の担当ではない**——実行ファイルの入れ替えは Go のツールチェーンが受け持ち、`update` は hub の中身だけを見る。

```sh
go install github.com/pilefort/braindex/cmd/braindex@latest   # 1. バイナリ。@latest はタグに解決する
braindex update                                               # 2. 雛形の追従＋索引の再生成
```

**`braindex.json` が変わるときは版差に注意する。** 新しい節の入った設定を古い版の braindex で読むと、
未知のキーはエラーなので索引生成を含む全コマンドが止まる。複数のマシンで使っているなら、
先にすべてのマシンの braindex を更新する（この場合 `update` は警告を出す）。
