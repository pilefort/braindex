package retro

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// 判定表: 訂正（エージェントの振る舞いへの訂正）と非訂正の中立な例文。
// 例文は架空で、個人の題材を含まない。既定辞書を変えるときはここも直す。
var correctionCases = []struct {
	text string
	want []string // 当たる辞書の行（辞書の順）
}{
	{"違う、docs だけでいい", []string{"違う"}},
	{"そうじゃなくて、設定ファイルの話", []string{"そうじゃな", "じゃなくて"}},
	{"テストじゃなくて実装を先に", []string{"じゃなくて"}},
	{"リポ全体ではなく docs だけを走査して", []string{"ではなく"}},
	{"その提案はやめて。頼んだことだけやって", []string{"やめて"}},
	{"勝手にファイルを消さないで", []string{"勝手に"}},
	{"何度も同じ確認をしないで", []string{"しないで", "何度も"}},
	{"前にも言ったけど、日本語で書いて", []string{"前にも"}},
	{"それは前も言ったよね", []string{"前も言", "言ったよね"}},
	{"日付が間違ってる", []string{"間違"}},
	{"この集計はおかしい。件数が合わない", []string{"おかしい"}},
	{"それは嘘。ファイルは存在しない", []string{"嘘"}},
	{"幻覚では？出典を出して", []string{"幻覚"}},
	{"ハルシネしてない？", []string{"ハルシネ"}},
	{"見出しの位置がずれてる", []string{"ずれ"}},
	{"この言い回しに違和感がある", []string{"違和感"}},
	{"その変更は意味がない。戻して", []string{"意味がない", "戻して"}},
	{"コメントを直して", []string{"直して"}},
	{"英語版は不要。日本語だけ", []string{"不要"}},
	{"余計な提案はいらない", []string{"いらない", "余計"}},
	{"要らないファイルまで作ってる", []string{"要らない"}},
	{"それはダメ。承認を取ってから", []string{"ダメ"}},
	{"ルールを忘れてる", []string{"忘れ"}},
	{"何回も同じ質問をしないで", []string{"しないで", "何回も"}},
	{"意味ない変更は戻して", []string{"意味ない", "戻して"}},
	{"ちがう、その設定じゃない", []string{"ちがう"}},
	{"表の列がズレてる", []string{"ズレ"}},
	{"それはだめ。先に確認して", []string{"だめ"}},
}

func TestClassify_Corrections(t *testing.T) {
	corr := Corrections()
	for _, c := range correctionCases {
		got := patterns(Classify(c.text, corr))
		if !reflect.DeepEqual(got, c.want) {
			t.Errorf("訂正 %q: want=%v got=%v", c.text, c.want, got)
		}
	}
}

// 既定辞書（訂正）の全行が判定表のどれかに当たる。辞書に行を足したら判定表にも例文を足す。
func TestClassify_Corrections_CoversEveryLine(t *testing.T) {
	corr := Corrections()
	hit := map[string]bool{}
	for _, c := range correctionCases {
		for _, m := range Classify(c.text, corr) {
			hit[m.Pattern] = true
		}
	}
	for _, p := range corr.Patterns {
		if !hit[p] {
			t.Errorf("既定辞書の行 %q が判定表で使われていない", p)
		}
	}
}

func TestClassify_NonCorrections(t *testing.T) {
	corr := Corrections()
	texts := []string{
		"索引を作って",
		"設定ファイルの書き方を教えて",
		"テストを追加してからコミットして",
		"ありがとう。次は README",
		"すごい、これで動いた",
		"PR を作って push まで済ませて",
		"この関数の計算量は？",
		"週別の集計も出せる？",
		"docs/notes に知見を書いて",
		"ブランチを切って作業を始めて",
		"決定性テストを同じコミットに含めて",
		"出力形式は Markdown の表で",
		"了解。それで進めて",
		"OK、マージして",
		"3 件目のオプションで",
		"今日の日付で記録して",
		"warning は stderr に出して",
		"実装は新しいセッションで始める",
		"テンプレの英語版は入れない方針",
		"面倒だけど全件確認して",
		"",
	}
	for _, text := range texts {
		if got := Classify(text, corr); len(got) != 0 {
			t.Errorf("非訂正 %q: 当たってはいけない: %v", text, patterns(got))
		}
	}
}

func TestClassify_Sentiment(t *testing.T) {
	sent := Sentiment()
	cases := []struct {
		text string
		want []string
	}{
		{"ありがとう、助かる", []string{"助かる", "ありがと"}},
		{"だるい。しつこい確認はやめて", []string{"だるい", "しつこい"}},
		{"索引を作って", nil},
	}
	for _, c := range cases {
		if got := patterns(Classify(c.text, sent)); !reflect.DeepEqual(got, c.want) {
			t.Errorf("感情 %q: want=%v got=%v", c.text, c.want, got)
		}
	}
}

func TestClassify_MultipleDictionaries(t *testing.T) {
	// 複数の辞書を渡すと、辞書の順 → 行の順で Match が並び、Dict と Text が入る
	got := Classify("違う。でもありがとう", Corrections(), Sentiment())
	want := []Match{
		{Dict: "corrections", Pattern: "違う", Text: "違う"},
		{Dict: "sentiment", Pattern: "ありがと", Text: "ありがと"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("want=%+v got=%+v", want, got)
	}
	if got := Classify("違う"); got != nil {
		t.Errorf("辞書なしは nil: got=%+v", got)
	}
}

func TestParse(t *testing.T) {
	d, err := Parse("mine", "# コメント\n\n  違う  \n^やり直し\n\n# 末尾\n")
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"違う", "^やり直し"}; !reflect.DeepEqual(d.Patterns, want) || d.Name != "mine" {
		t.Errorf("Parse: name=%q patterns=%v", d.Name, d.Patterns)
	}
	// 正規表現として使える（^ が効く）
	if got := patterns(Classify("やり直し", d)); !reflect.DeepEqual(got, []string{"^やり直し"}) {
		t.Errorf("先頭一致: got=%v", got)
	}
	if got := Classify("もう一度やり直し", d); len(got) != 0 {
		t.Errorf("先頭でなければ当たらない: got=%v", patterns(got))
	}
	// CRLF・BOM 付きでも読める
	d2, err := Parse("crlf", "\xEF\xBB\xBF違う\r\nやめて\r\n")
	if err != nil || !reflect.DeepEqual(d2.Patterns, []string{"違う", "やめて"}) {
		t.Errorf("CRLF/BOM: err=%v patterns=%v", err, d2.Patterns)
	}
}

func TestParse_BadRegexp(t *testing.T) {
	_, err := Parse("bad", "違う\n(未閉じ\n")
	if err == nil {
		t.Fatal("不正な正規表現はエラーにする")
	}
	if !strings.Contains(err.Error(), "bad") || !strings.Contains(err.Error(), "2 行目") {
		t.Errorf("エラーに辞書名と行番号を含める: %v", err)
	}
}

func TestParse_Empty(t *testing.T) {
	d, err := Parse("empty", "# 何も無い\n")
	if err != nil {
		t.Fatal(err)
	}
	if len(d.Patterns) != 0 || Classify("違う", d) != nil {
		t.Errorf("空の辞書は何にも当たらない: %v", d.Patterns)
	}
}

func TestLoad(t *testing.T) {
	path := filepath.Join(t.TempDir(), "extra.txt")
	if err := os.WriteFile(path, []byte("# 追加\n出典を出して\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	d, err := Load(path)
	if err != nil {
		t.Fatal(err)
	}
	if d.Name != "extra.txt" || !reflect.DeepEqual(d.Patterns, []string{"出典を出して"}) {
		t.Errorf("Load: name=%q patterns=%v", d.Name, d.Patterns)
	}
	if _, err := Load(filepath.Join(t.TempDir(), "no-such.txt")); err == nil {
		t.Error("無いファイルはエラー")
	}
}

func TestDefaults(t *testing.T) {
	c, s := Corrections(), Sentiment()
	if c.Name != "corrections" || s.Name != "sentiment" {
		t.Errorf("既定辞書の名前: %q %q", c.Name, s.Name)
	}
	if len(c.Patterns) < 20 || len(s.Patterns) < 5 {
		t.Errorf("既定辞書が空に近い: corrections=%d sentiment=%d", len(c.Patterns), len(s.Patterns))
	}
	// 既定辞書は何度呼んでも同じ内容（毎回読み直さない）
	if Corrections() != c {
		t.Error("Corrections() は同じ辞書を返す")
	}
}

func patterns(ms []Match) []string {
	if len(ms) == 0 {
		return nil
	}
	out := make([]string, len(ms))
	for i, m := range ms {
		out[i] = m.Pattern
	}
	return out
}
