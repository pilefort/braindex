# braindex news — ニュースサジェスト

[← README](../README.md) ／ [手引きの目次](README.md)

hub で `braindex news fetch` を実行すると、`news/feeds.json` のフィードを GET し、既読（`news/.seen.json`）に無い記事を
`news/digest_<日付>_<層>.md`（記録用）と、同名の `.html`（選別 UI・既定ブラウザで開く）に書く。
記事は関心プロファイルで採点し、関心度が `news.show_min_score`（既定 2）以上を主要表示、未満は「関心外と判定」に折りたたむ。
HTML の「選別を書き出す」が保存した JSON を `braindex news apply` が取り込み、「残す」を `news/keep/YYYY-MM.md` に追記する。
keep は次のプロファイルの出典になるので、**選別がそのまま関心の推定に戻る**。
外へ出る通信はフィードの GET だけで、セッション内容もノート本文も送らない。HTML は外部の JS・CSS を参照しない。
フィードのリンクは `http(s)` のものだけを載せる（それ以外は題名だけを出し、選別 JSON にも `news/keep/` にも入れない）。

フィード一覧 `news/feeds.json` は自分で作る。`braindex init -add news` は隣に `news/feeds.example.json`（公開フィード 3 件の見本）を置くので、
コピーして書き換える。`name` と `url` を持つオブジェクトの配列:

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
| `news suggest` | 直近の会話・索引・keep から作った関心プロファイルに当たる取材先（RSS）を、同梱の取材先目録（17 ジャンル・98 本）から候補として出す。`feeds.json` に登録済みのものは除く。`-top N`（既定 10）・`-json` |

重みは出典ごとに最大を 1 に正規化した値の和で、規則ベース。既定では LLM を使わない。窓の起点は `retro` と同じローカルの 0 時。
数えるセッションの範囲も `retro` と同じで、既定は `root` 配下だけ（設定 `retro.all_projects` か `-all-projects` で全部。→ [retro.md](retro.md#数えるセッションの範囲)）。
主なフラグ: `-config` `-date YYYY-MM-DD` `-layer` `-out` `-stdout` `-no-open` `-no-score`（採点せず全件を主要表示）`-no-llm`
`-replay`（既読を無視して再生成し、既読も更新しない）`-days` `-top` `-json` `-sessions` `-inbox`。
終了コード: 0 成功／1 失敗（**同じ日の出力先が既にある**・全フィードの取得失敗。何も書かない）／2 警告つきで完了（一部のフィードが取れなかった・採点の出典が無かった・選別や統計を取り込めなかった・LLM 補助が呼べなかった）。
同じ日に 2 回動かすと、既にあるダイジェストは上書きせず終了コード 1 で止まる（`braindex review` と同じ。読み直すだけなら `-out` で別名に、捨ててよければ `-stdout` に出す）。

**設定例**（`braindex init -add news` が足す節と同じ。全部省略可で、値は既定）:

```json
"news": { "dir": "news", "feeds": "news/feeds.json", "seen_days": 90, "profile_days": 14,
          "cap_per_layer": { "daily": 15, "weekly": 25 }, "show_min_score": 2,
          "llm": "off", "llm_model": "", "llm_timeout_sec": 120 }
```

**置き場 `news/`**: `feeds.json`（自分で書く）・`keep/YYYY-MM.md`（残した見出し。蓄積側なので版管理に残す）・`interests.md`（任意の補助）が利用者のもの。
`digest_*`・`.seen.json`（既読）・`.stats.json`（選別の統計）・`.llm_cache.json`・`.ingested/`（取り込み済みの選別 JSON）は作業ファイルで、
`braindex init -add news` が hub の `.gitignore` に足す行が除外する。

**補助ファイル `news/interests.md` の書き方**: 1 行 1 語。空行と `#` 始まりは読まない。行は記事側と同じ語の抽出規則（ラテン文字 3 字以上・カタカナ 2 字以上・漢字 2〜6 字。
大文字小文字は畳む）を通してから語にするので、**規則で語にならない書き方（`ai`・`go` のような 2 字のラテン文字、記号だけ、長い漢字の複合語）は記事側でも語にならず、
表に重みつきで並んでも照合には効かない**。`braindex news profile` の表で `extra` 列に載っている語が、記事の見出しに現れる形と同じかを確かめる。
`|` を含む行は表の描画を崩すので書かない。

**LLM 補助（opt-in）**: `"llm": "claude-cli"` にすると、`claude` CLI（PATH にあるもの）をヘッドレスで呼び、英語見出しの日本語訳と関心度 0〜3 を受け取って
語の一致の点に重ねる（バッジの説明とダイジェストに `LLM` と出る）。渡すのは見出し・概要・言語・関心プロファイルの語・keep の見出しだけで、リンク・セッション本文・
ノート本文は渡さない。結果は `news/.llm_cache.json` に記事 ID で覚え、同じ記事を 2 回聞かない。`llm_model` で `--model` を指定できる（空なら CLI の既定）。
CLI が無い・`llm_timeout_sec` を超えた・応答が JSON でないときは警告（終了コード 2）にして、その記事は語の点のまま出す。`-no-llm`（と `-no-score`）で止まる。
`claude` は `--tools ""` で起動し、道具は一切使わせない。記事の見出し・概要は `<articles>` の区切りに入れて渡し、その中の指示に従わないよう明示する
（渡すもの＝見出し・概要・言語・語・keep の見出し／許すこと＝翻訳と 0〜3 の採点だけ）。フィードは他人が書いた本文なので、そこに指示を埋めても採点は動かない。

**定期実行**: `braindex init` が `schedule.jobs` に次の job を入れる（時刻は `braindex.json` で直す）。登録は `braindex schedule install`（仕組みは [schedule.md](schedule.md)）:

```json
{ "name": "news", "args": ["news", "fetch", "-layer", "daily", "-no-open"], "when": "daily:07:30" }
```

`-no-open` なのは、cron・schtasks から起動したプロセスがログイン中のデスクトップにウィンドウを出せないため。朝に `news/digest_<日付>_daily.html` を自分で開く。
選別を書き出した JSON は次回の `fetch` か `braindex news apply` が拾う。

**取り込みは中身を検査する**: 選別 JSON はブラウザのダウンロード先（誰でも置ける場所）から拾うので、そのまま信じない。
`date` が `YYYY-MM-DD` の形でないファイルは取り込まず、`.ingested/` へも移さない（中を見て消せるように元の場所に残す）
——`date` は `news/keep/YYYY-MM.md` のパスの一部になるため。`feed_stats` は `feeds.json` にある取材先の名前で、数が 0 以上の項目だけを数え、
外れた項目は落として 1 行で伝える。1 件も取り込めなかった回は `.stats.json` を触らない。

**不要ばかり付く取材先は下げる**（2026-09-06 追加）: `.stats.json` の累計で「残す／不要を選んだ数」が 10 件以上、
そのうち不要が 80% を超える取材先は、次回から関心度の上限を 1 に下げる（主要表示から折りたたみ側へ回る）。
不要率は「見た数」でなく「選んだ数」で割る——折りたたみに入って目に入らなかった記事を不要と数えないため。
承知のうえの代償が 1 つある: いったん下がると `keep` に入りにくくなるので、不要率が下がる機会も減る。
完全に読まなくなった取材先は `feeds.json` から消す（間引き候補として `news fetch` が出す）。

**取材先の候補（`braindex news suggest`）**: 何を `feeds.json` に書けばよいか分からないとき、直近の会話で使っている技術から取材先を探す。
同梱の目録の各取材先が持つ照合語（例: Docker Blog → `docker` `dockerfile` `compose` `コンテナ`）と関心プロファイルの語を手元で突き合わせ、
当たった語の重みの和が大きい順に出す。照合は手元だけで、通信も LLM もしない。出力は当たった語と数だけで発話の本文は載せない。
候補を採るときは、出力の URL を `feeds.json` に `{"name": "…", "url": "…"}` として書く（選別 HTML からの登録は次の版）。
照合語は作者が付けた分類で、当たり方が外れることがある。`braindex news profile` で自分の語を見て、`interests.md` に語を足せば当たりを寄せられる。
