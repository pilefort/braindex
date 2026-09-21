# TypeSafe にニュースの採点を任せる

[← 手引き](README.md) · [ニュース](news.md)

`braindex-typesafe` は、ニュースの見出しを TypeSafe API に送り、関心度と分野を返す別の実行ファイルです。
利用する人だけが導入します。API の利用料金は利用者の負担です。braindex 本体は採点用の外部 API を呼びません。

## 導入と設定

```sh
go install github.com/pilefort/braindex/cmd/braindex-typesafe@latest
```

実行ファイルを PATH に置き、既存の `braindex.json` の `news` 節に次を設定します。

```json
{"llm":"command","llm_command":["braindex-typesafe"]}
```

API キーは、このプログラムを起動するプロセスの環境変数 `TYPESAFE_API_KEY` に設定します。
キーを `braindex.json` やコマンドの引数に書かないでください。定期実行では、その実行環境にも設定が必要です。
Windows のレジストリなど、プロセスの環境変数以外は探しません。キーがなければ送信せず終了コード 1 になります。

## 送る内容を先に確かめる

次の JSON を UTF-8 の `request.json` として保存します。

```json
{"version":1,"terms":["ai"],"items":[{"id":"sample","feed":"Sample feed","lang":"en","t":"New measurement method","s":"Sample summary"}]}
```

```sh
braindex-typesafe -dry-run < request.json
```

PowerShell では次のように標準入力へ渡します。

```powershell
Get-Content -Raw -Encoding utf8 request.json | braindex-typesafe -dry-run
```

`-dry-run` はキーなしでも使えます。送信予定の本文を標準エラーに整形して出し、標準出力に `{"items":[]}` を返します。
API へは送信しません。本文には見出しと関心語が含まれるため、出力の共有先には注意してください。

## 送信する情報

| 情報 | API への送信 |
|---|---|
| 見出し・フィード名・関心語 | 送る |
| 概要 | 既定では送らない。`-summary` を付けたときだけ `state.summary` として送る |
| 記事の URL・記事 ID・言語 | 送らない |
| ノート本文・セッション本文・keep の見出し一覧 | 受け取らず、送らない |

本体から渡る関心語は keep（残したニュースの見出し）か extra（`news/interests.md`）に出た語だけです。
keep の回数の降順、extra の回数の降順、語の昇順で上位 30 語まで渡します。index・sessions だけに出る語は含めません。
モデル名と採点・分野の質問もリクエスト本文に含みます。1 記事につき 1 回送信します。

## 点と分野

| 「読む価値がある」確率 | 返す点 |
|---|---|
| 0.55 未満 | 1 |
| 0.55 以上、0.75 未満 | 2 |
| 0.75 以上 | 3 |

0 点は返しません。境界は試用用の初期値で、`-t2`・`-t3` で変えられます（`0 <= t2 <= t3 <= 1`）。
分野は it / chemistry / physics / metrology / other_science / other が既定です。
選択の confidence が 0.5 未満、または欠けている場合は分野タグを返しません。
`-fields fields.json` で、分野名から説明への JSON オブジェクト（1〜255 件）に差し替えられます。

```json
{"it":"Software and developer tools","physics":"Physics and astronomy"}
```

## その他のフラグと失敗時の動作

| フラグ | 内容 |
|---|---|
| `-model` | 既定 `jev-latest` |
| `-endpoint` | 既定 `https://api.typesafe.ai/v1/systemone`。ローカル試験用の送信先も指定できる |
| `-usage-log usage.jsonl` | 送信した記事ごとに日時（`time`）・id・input_tokens・output_tokens・ms を 1 行の JSON で追記する。見出し・関心語・キーは書かない。失敗して利用量を取得できなかった記事のトークン数は 0 |
| `-summary` | 概要も送る。設定では `"llm_command":["braindex-typesafe","-summary"]` |

429・529 は 2 秒、4 秒、8 秒待ち、最大 3 回再試行します。それ以外の失敗は、その記事を応答から外して次へ進みます。
全記事が失敗した場合は終了コード 1、一部でも採点できた場合は 0 です。
本体は採点の欠落やプログラムの失敗を警告に数え、該当記事を語の点で表示し、ダイジェストを書いて終了コード 2 を返します。
`-no-llm` と `-no-score`、設定の `llm_timeout_sec` と `llm_budget_sec` は外部採点にも効きます。
キャッシュは他の採点方式と共通の記事 ID 単位です。設定やしきい値を変えても採点済みの記事は聞き直しません。
