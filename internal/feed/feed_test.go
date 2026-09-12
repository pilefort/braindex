package feed

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func readTestdata(t *testing.T, name string) []byte {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("testdata", name))
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// RSS 2.0: CDATA の HTML を素のテキストに、guid(isPermaLink)をリンクの代わりに、content:encoded を概要の代わりに使う。
func TestParse_RSS2(t *testing.T) {
	d, err := ParseBytes(readTestdata(t, "rss2.xml"))
	if err != nil {
		t.Fatal(err)
	}
	if d.Format != "rss2" || d.Title != "Example RSS 2.0" {
		t.Errorf("format/title: %q %q", d.Format, d.Title)
	}
	want := []Entry{
		{ID: EntryID("https://example.com/a", ""), Title: "記事A", Link: "https://example.com/a?utm_source=rss&utm_medium=feed",
			Published: "2026-08-14", Summary: "Hello world & more. extra spaces"},
		{ID: EntryID("https://example.com/b", ""), Title: "記事B", Link: "https://example.com/b",
			Published: "2026-08-15", Summary: "本文だけ"}, // 23:30 -0500 は UTC で翌日
		{ID: EntryID("", "(無題)"), Title: "(無題)", Link: "", Published: "", Summary: "タイトル無し"},
	}
	if !reflect.DeepEqual(d.Entries, want) {
		t.Errorf("entries:\n got %+v\nwant %+v", d.Entries, want)
	}
}

// Atom: rel=alternate のリンクを採り enclosure は無視、html 型のタイトルはタグを除く、リンクが無ければ URL 形式の id、
// xhtml 型の content は本文の文字だけ、published が無ければ updated。
func TestParse_Atom(t *testing.T) {
	d, err := ParseBytes(readTestdata(t, "atom.xml"))
	if err != nil {
		t.Fatal(err)
	}
	if d.Format != "atom" || d.Title != "Example Atom" {
		t.Errorf("format/title: %q %q", d.Format, d.Title)
	}
	want := []Entry{
		{ID: EntryID("https://example.com/c", ""), Title: "記事 C", Link: "https://example.com/c", Published: "2026-08-14", Summary: "概要C"},
		{ID: EntryID("https://example.com/d", ""), Title: "記事D", Link: "https://example.com/d", Published: "2026-08-14", Summary: "本文 D"},
	}
	if !reflect.DeepEqual(d.Entries, want) {
		t.Errorf("entries:\n got %+v\nwant %+v", d.Entries, want)
	}
}

// Atom の type="xhtml": 入れ子の要素の境目で語が繋がらない(html 型でタグが空白に置き換わるのと揃える)。
func TestParse_AtomXHTMLSeparatesElements(t *testing.T) {
	in := `<feed xmlns="http://www.w3.org/2005/Atom"><entry><title>T</title>` +
		`<link href="https://example.com/x"/>` +
		`<content type="xhtml"><div xmlns="http://www.w3.org/1999/xhtml"><p>前半</p><p>後半</p></div></content>` +
		`</entry></feed>`
	d, err := ParseBytes([]byte(in))
	if err != nil {
		t.Fatal(err)
	}
	if len(d.Entries) != 1 {
		t.Fatalf("entries=%d", len(d.Entries))
	}
	if got := d.Entries[0].Summary; got != "前半 後半" {
		t.Errorf("summary=%q, want %q", got, "前半 後半")
	}
}

// RSS 1.0: item は channel の外にあり、日付は dc:date。末尾 "/" の有無で ID が変わらない。
func TestParse_RSS1(t *testing.T) {
	d, err := ParseBytes(readTestdata(t, "rss1.xml"))
	if err != nil {
		t.Fatal(err)
	}
	if d.Format != "rss1" || d.Title != "Example RSS 1.0" {
		t.Errorf("format/title: %q %q", d.Format, d.Title)
	}
	want := []Entry{
		{ID: EntryID("https://example.com/e", ""), Title: "記事E", Link: "https://example.com/e/", Published: "2026-08-14", Summary: "概要E"},
	}
	if !reflect.DeepEqual(d.Entries, want) {
		t.Errorf("entries:\n got %+v\nwant %+v", d.Entries, want)
	}
}

// 同じ入力からは同じ出力(決定性)。
func TestParse_Deterministic(t *testing.T) {
	for _, name := range []string{"rss2.xml", "atom.xml", "rss1.xml"} {
		b := readTestdata(t, name)
		d1, err1 := ParseBytes(b)
		d2, err2 := ParseBytes(b)
		if err1 != nil || err2 != nil || !reflect.DeepEqual(d1, d2) {
			t.Errorf("%s: 2 回のパースが一致しない", name)
		}
	}
}

// フィードでない XML・空・平文は ErrNotFeed(非厳格モードなので閉じ忘れは補正され、最初の要素で判定する)。
func TestParse_NotFeed(t *testing.T) {
	for _, in := range []string{
		`<?xml version="1.0"?><html><body>x</body></html>`,
		`<html><p>not closed`,
		`<feed xmlns="http://example.com/not-atom"><entry/></feed>`,
		``,
		`Just text`,
	} {
		_, err := ParseBytes([]byte(in))
		if !errors.Is(err, ErrNotFeed) {
			t.Errorf("%q: err=%v(ErrNotFeed を期待)", in, err)
		}
	}
}

// UTF-8 BOM は読み飛ばし、ISO-8859-1 は UTF-8 に直し、それ以外の文字コードはエラー。
func TestParse_Charset(t *testing.T) {
	bom := append([]byte("\xef\xbb\xbf"), readTestdata(t, "rss2.xml")...)
	if d, err := ParseBytes(bom); err != nil || len(d.Entries) != 3 {
		t.Errorf("BOM 付き: err=%v entries=%d", err, len(d.Entries))
	}

	latin := []byte(`<?xml version="1.0" encoding="ISO-8859-1"?><rss version="2.0"><channel><title>Caf` + "\xe9" + `</title></channel></rss>`)
	d, err := ParseBytes(latin)
	if err != nil || d.Title != "Café" {
		t.Errorf("ISO-8859-1: err=%v title=%q", err, d.Title)
	}

	sjis := []byte(`<?xml version="1.0" encoding="Shift_JIS"?><rss version="2.0"><channel><title>x</title></channel></rss>`)
	if _, err := ParseBytes(sjis); err == nil || !strings.Contains(err.Error(), "Shift_JIS") {
		t.Errorf("Shift_JIS: 対応外のエラーにならない: %v", err)
	}
}

// DTD で宣言された実体は展開しない。外部実体(file://)はファイルを読まず、
// 入れ子の実体(実体爆弾)も膨らまない。どちらも参照は文字のまま残る。
func TestParse_DoesNotExpandEntities(t *testing.T) {
	secret := filepath.Join(t.TempDir(), "secret.txt")
	if err := os.WriteFile(secret, []byte("秘密の中身"), 0o600); err != nil {
		t.Fatal(err)
	}
	// file:///tmp/x(POSIX)・file:///C:/…(Windows)のどちらでも成り立つ形にする
	fileURL := "file:///" + strings.TrimPrefix(filepath.ToSlash(secret), "/")
	external := `<?xml version="1.0"?><!DOCTYPE rss [<!ENTITY xxe SYSTEM "` + fileURL + `">]>` +
		`<rss version="2.0"><channel><title>T</title><item><title>A&xxe;B</title>` +
		`<link>https://example.com/x</link></item></channel></rss>`
	d, err := ParseBytes([]byte(external))
	if err != nil {
		t.Fatal(err)
	}
	if len(d.Entries) != 1 {
		t.Fatalf("entries=%d", len(d.Entries))
	}
	if strings.Contains(d.Entries[0].Title, "秘密の中身") {
		t.Errorf("外部実体が展開されてファイルの中身が入った: %q", d.Entries[0].Title)
	}

	bomb := `<?xml version="1.0"?><!DOCTYPE lolz [<!ENTITY lol "lol">` +
		`<!ENTITY lol2 "&lol;&lol;&lol;&lol;&lol;&lol;&lol;&lol;&lol;&lol;">` +
		`<!ENTITY lol3 "&lol2;&lol2;&lol2;&lol2;&lol2;&lol2;&lol2;&lol2;&lol2;&lol2;">]>` +
		`<rss version="2.0"><channel><title>&lol3;</title></channel></rss>`
	d2, err := ParseBytes([]byte(bomb))
	if err != nil {
		t.Fatal(err)
	}
	if len(d2.Title) > len("&lol3;") {
		t.Errorf("入れ子の実体が展開された(題名 %d バイト)", len(d2.Title))
	}
}

// 未定義の実体参照(&nbsp;)が混じっていても落ちない。
func TestParse_LenientEntities(t *testing.T) {
	in := `<rss version="2.0"><channel><title>T</title><item><title>A&nbsp;B</title><link>https://example.com/x</link></item></channel></rss>`
	d, err := Parse(bytes.NewReader([]byte(in)))
	if err != nil || len(d.Entries) != 1 {
		t.Fatalf("err=%v entries=%d", err, len(d.Entries))
	}
	if !strings.HasPrefix(d.Entries[0].Title, "A") || !strings.HasSuffix(d.Entries[0].Title, "B") {
		t.Errorf("title=%q", d.Entries[0].Title)
	}
}

func TestAtomSummaryPreservesProseAfterMetadata(t *testing.T) {
	raw := `<feed xmlns="http://www.w3.org/2005/Atom"><entry><title>Title</title><summary type="xhtml"><div xmlns="http://www.w3.org/1999/xhtml"><p>Points: 12</p><p>Actual explanation.</p></div></summary></entry></feed>`
	d, err := ParseBytes([]byte(raw))
	if err != nil {
		t.Fatal(err)
	}
	if d.Entries[0].Summary != "Points: 12 Actual explanation." {
		t.Fatal(d.Entries[0].Summary)
	}
}
