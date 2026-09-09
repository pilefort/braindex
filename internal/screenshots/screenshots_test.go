// Package screenshots は README に貼る画面のスクリーンショットを作り直すための仕掛け。
//
// 生成する HTML は、すべてこのファイルの中の架空のサンプルから組む(実在のノート・フィード・
// 判断は使わない)。撮る手順は既存のブラウザ検査(internal/news/ui_browser_test.go)と同じで、
// Python の playwright を opt-in で呼ぶ。CI では走らない。
//
//	BRAINDEX_SCREENSHOT=1 go test ./internal/screenshots -run TestScreenshots -v
package screenshots

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/pilefort/braindex/internal/approvals"
	"github.com/pilefort/braindex/internal/feed"
	"github.com/pilefort/braindex/internal/interest"
	"github.com/pilefort/braindex/internal/mdhtml"
	"github.com/pilefort/braindex/internal/news"
)

// outDir は PNG の置き場(リポジトリ相対)。README から参照する。
const outDir = "../../assets/screenshots"

func TestScreenshots(t *testing.T) {
	if os.Getenv("BRAINDEX_SCREENSHOT") != "1" {
		t.Skip("スクリーンショットの生成は BRAINDEX_SCREENSHOT=1 と Python playwright が要る")
	}
	dir := t.TempDir()
	for name, b := range pages() {
		if err := os.WriteFile(filepath.Join(dir, name), b, 0644); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.MkdirAll(outDir, 0755); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("python", "screenshots.py", dir, outDir)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Run(); err != nil {
		t.Fatal(err)
	}
}

// pages は撮る画面の HTML を組む。ファイル名は screenshots.py の一覧と対応する。
func pages() map[string][]byte {
	return map[string][]byte{
		"approvals.html":     approvalsPage(),
		"news-overview.html": overviewPage(),
		"news-digest.html":   digestPage(),
		"news-reading.html":  readingPage(),
		"answer.html":        []byte(mdhtml.Page(answerMD, "索引を毎日作り直すべきか")),
		"answer-thread.html": []byte(mdhtml.ThreadPage(threadMD(), "索引の作り直し")),
	}
}

// ---- 承認フォーム(braindex approvals serve) ----

const approvalsMD = `# 承認待ち

空が正常。答えが出たら理由ごと ` + "`docs/decisions.md`" + ` へ移す。

## 1. 設定ファイルの形式を JSON にするか TOML にするか

**決めたいこと:** 設定ファイル ` + "`app.json`" + ` の形式。
**なぜ今決めるか:** 設定の読み込みを次の変更で実装するため、先に形式を決める必要がある。
**選択肢:**
- A. JSON — 標準ライブラリだけで読める／コメントが書けない
- B. TOML — コメントが書ける／依存が 1 つ増える
**私の案:** A — 依存を増やさない方針（決定 2026-01-10）と合う
**決めないとどうなるか:** 設定の読み込みが書けず、次の変更が止まる。

## 2. 実行ログの出力先

**決めたいこと:** 実行ログを標準エラー出力に出すか、ファイルに書くか。
**なぜ今決めるか:** 自動実行の結果を後から追いたいという要望が出たため。
**選択肢:**
- A. 標準エラー出力 — 追加の設定なし／長い実行では流れて消える
- B. ファイル — 後から読める／置き場と後片付けの規約が要る
- C. 両方 — 便利／実装と検査が 2 倍になる
**私の案:** 案なし
**決めないとどうなるか:** 既定のまま標準エラー出力に出し続ける。
`

func approvalsPage() []byte {
	return approvals.RenderForm(approvals.Parse([]byte(approvalsMD)), approvals.Meta{
		Project:     "hub",
		Path:        "work/APPROVALS.md",
		Nonce:       "sample",
		GeneratedAt: "2026-01-02 10:00",
	})
}

// ---- ニュース ----

var srcDev = news.Source{Name: "開発ブログ", Category: "開発", Lang: "ja"}
var srcSci = news.Source{Name: "研究ニュース", Category: "科学", Lang: "ja"}

func digestResults() []news.Result {
	return []news.Result{
		{Source: srcDev, New: []feed.Entry{
			{ID: "a1", Title: "メモリの扱いは言語でどう違うか", Summary: "確保と解放の考え方を、短いコードを並べて比べた記事です。", Link: "https://example.com/dev/1", Published: "2026-01-02"},
			{ID: "a2", Title: "テストを決定的に保つための小さな工夫", Summary: "時刻と乱数をどこで注入するかで、落ちるテストが減るという話。", Link: "https://example.com/dev/2", Published: "2026-01-02"},
			{ID: "a3", Title: "新しいエディタ拡張の紹介", Summary: "配色と補完を差し替える拡張の紹介記事です。", Link: "https://example.com/dev/3", Published: "2026-01-01"},
		}},
		{Source: srcSci, New: []feed.Entry{
			{ID: "b1", Title: "材料のつながり方と強度の関係", Summary: "同じ成分でも並び方で強さが変わることを、実験結果から説明しています。", Link: "https://example.com/sci/1", Published: "2026-01-02"},
			{ID: "b2", Title: "観測データの公開が進む", Summary: "観測所が過去 10 年分のデータを公開したという知らせ。", Link: "https://example.com/sci/2", Published: "2026-01-01"},
		}},
	}
}

func digestPage() []byte {
	return news.RenderHTML(digestResults(), news.DigestOptions{
		Today: "2026-01-02",
		Layer: "daily",
		Cap:   5,
		Ranking: news.Ranking{
			"a1": {Value: 3, Matched: []string{"メモリ", "言語"}},
			"a2": {Value: 3, Matched: []string{"テスト", "決定的"}},
			"a3": {Value: 0},
			"b1": {Value: 2, Matched: []string{"材料"}},
			"b2": {Value: 1, Matched: []string{"データ"}},
		},
		MinScore:    2,
		Serendipity: map[string]bool{"b2": true},
		LibraryHref: "reading.html",
	})
}

var _ = interest.MaxScore // 関心度の尺度は interest 側と共通(採点の値はこの尺度で書く)

const overviewSourceMD = `# ニュース概要 2026-01-02

詳しく知りたいと仕分けした 2 件の概要です。原文は各見出しのリンクから読めます。

<!--braindex-article id=a1 question=q-1 link=https://example.com/dev/1-->
## メモリの扱いは言語でどう違うか

前提から: プログラムは、計算の途中の値を置く場所を借りて使います。この「借りて返す」を
**誰がやるか**が言語ごとに違う、というのがこの記事の話題です。

- 自分で返す言語では、返し忘れると使える場所が減っていきます
- 自動で返す言語では、返す時機を決める仕組みが別に動きます

読みどころは、同じ処理を 2 つの言語で書き比べている節です。

<!--braindex-article id=b1 question=q-2 link=https://example.com/sci/1-->
## 材料のつながり方と強度の関係

同じ成分でも、粒の並び方が変わると強さが変わります。記事は、並び方を 3 種類作って
同じ力で曲げた実験を紹介しています。結論は「並びがそろっているほど、一方向には強く、
別の方向には弱い」。用途に合わせて並びを選ぶ、という話につながります。
`

func overviewPage() []byte {
	intro, arts := news.ParseOverview(overviewSourceMD)
	return news.RenderOverview("sample", "ニュース概要 2026-01-02", intro, arts)
}

func readingPage() []byte {
	return news.RenderReading(news.Reading{
		Articles: map[string]*news.ReadingArticle{
			"a1": {
				Keep:    news.Keep{ID: "a1", Title: "メモリの扱いは言語でどう違うか", Link: "https://example.com/dev/1", Feed: "開発ブログ", Category: "開発", Summary: "確保と解放の考え方を、短いコードを並べて比べた記事です。"},
				Date:    "2026-01-02",
				Status:  "later",
				Updated: "2026-01-02T10:05:00Z",
				Questions: []news.Question{{
					ID: "q-1", Mode: "stuck", Text: "用語から分からない。具体例で教えて。", Created: "2026-01-02T10:00:00Z",
					Answer: `プログラムが値を置く場所には、置いたらすぐ片づく場所と、片づけを頼むまで残る場所があります。
後者を借りたまま返さないと、使える場所が少しずつ減ります。

出典: https://example.com/dev/1`,
				}},
			},
			"b1": {
				Keep:    news.Keep{ID: "b1", Title: "材料のつながり方と強度の関係", Link: "https://example.com/sci/1", Feed: "研究ニュース", Category: "科学", Summary: "同じ成分でも並び方で強さが変わることを、実験結果から説明しています。"},
				Date:    "2026-01-02",
				Status:  "deep",
				Updated: "2026-01-02T10:06:00Z",
			},
		},
		Receipts: map[string]string{},
	})
}

// ---- 回答 HTML(braindex answer) ----

// fence はコードブロックの囲み。生の文字列リテラルの中には書けないので定数にする。
const fence = "```"

const answerMD = `# 索引を毎日作り直すべきか

結論から言うと、**毎日でなくてよい**。作り直しは差分が出たときだけで足りる。

## なぜか

索引は同じ入力から同じ結果になるので、ノートを触っていない日に作り直しても
1 バイトも変わらない。変わらない生成物のために時間を使う理由がない。

| 頻度 | 得られること | 費やすもの |
|---|---|---|
| 毎日 | 常に最新 | 変化のない日も走る |
| 変更したとき | 実質同じ | 走らせる仕掛けが要る |
| 週 1 回 | 週次の差分が読みやすい | 週の途中は古い |

## どうするか

` + fence + `sh
braindex        # 索引を作り直す
git diff        # 前回からの差分を読む
` + fence + `

1. ノートを書いた日の終わりに作り直す
2. 週の初めに、その週の差分をまとめて読む
3. 差分が空なら何もしない

> 索引をコミットしておくと、この差分がそのまま「前回からの変化」になる。
`

func threadMD() string {
	md := "# 索引の作り直し\n"
	md = mdhtml.Prepend(md, "索引の作り直し", mdhtml.Entry{
		At: "2026-01-02T09:30:00Z",
		Q:  "索引って毎日作り直したほうがいいの？",
		Body: `いいえ、毎日でなくてよいです。索引は同じ入力から同じ結果になるので、
ノートを触っていない日に作り直しても中身は変わりません。`,
	})
	md = mdhtml.Prepend(md, "索引の作り直し", mdhtml.Entry{
		At:   "2026-01-02T10:10:00Z",
		Q:    "じゃあ週 1 回でいい？",
		Body: answerMD,
	})
	return md
}
