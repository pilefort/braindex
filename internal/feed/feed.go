// Package feed は RSS 2.0 / Atom / RSS 1.0 のフィードを読み、記事の一覧にする(braindex news の取得部分)。
//
// 外へ出る通信はフィードの GET だけ(Fetch)。パース(Parse)は標準ライブラリの encoding/xml だけで行い、
// 出力は入力に対して決定的(記事の順はフィードの記載順・日付は UTC の暦日)。
// 記事の識別子(Entry.ID)は追跡パラメータを除いたリンクから作るので、同じ記事が utm_ 付きで再配信されても既読と一致する。
// 関心の採点や既読の管理はここでは行わない(採点は internal/interest、既読は internal/news)。
package feed

import (
	"bytes"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"strings"
)

// Entry はフィードの記事 1 件。
type Entry struct {
	ID        string // 正規化したリンク(無ければタイトル)の SHA-256 先頭 16 桁。既読・選別の鍵
	Title     string // タグ・実体参照・連続空白を整えたタイトル。無ければ "(無題)"
	Link      string // 記事の URL(フィードの記載どおり。正規化は ID の計算にだけ使う)
	Published string // 公開日 YYYY-MM-DD(UTC)。published → updated の順に採り、読めなければ空
	Summary   string // 概要(HTML を除き SummaryLimit 文字で切る)。無ければ空
}

// Document はパースしたフィード。
type Document struct {
	Format  string  // "rss2" / "atom" / "rss1"
	Title   string  // フィードの題名(整形済み。無ければ空)
	Entries []Entry // 記載順
}

// ErrNotFeed は XML が RSS 2.0 / Atom / RSS 1.0 のどれでもないとき。
var ErrNotFeed = errors.New("RSS 2.0 / Atom / RSS 1.0 のどれでもない")

const (
	nsAtom = "http://www.w3.org/2005/Atom"
	nsRDF  = "http://www.w3.org/1999/02/22-rdf-syntax-ns#"
)

// Parse は XML を読んでフィードにする。文字コードは UTF-8(BOM 可)と ISO-8859-1 系に対応し、それ以外はエラー。
func Parse(r io.Reader) (Document, error) {
	b, err := io.ReadAll(r)
	if err != nil {
		return Document{}, err
	}
	return ParseBytes(b)
}

// ParseBytes は Parse のバイト列版。
func ParseBytes(b []byte) (Document, error) {
	b = bytes.TrimPrefix(b, []byte("\xef\xbb\xbf"))
	dec := xml.NewDecoder(bytes.NewReader(b))
	dec.CharsetReader = charsetReader
	dec.Strict = false // 実在のフィードには未定義の実体参照(&nbsp; 等)が混じる。落とさず読む

	// 最初の要素で形式を決め、その要素ごと構造体に読む
	var start xml.StartElement
	for {
		tok, err := dec.Token()
		if err != nil {
			if errors.Is(err, io.EOF) {
				return Document{}, ErrNotFeed
			}
			return Document{}, fmt.Errorf("XML を読めない: %w", err)
		}
		if se, ok := tok.(xml.StartElement); ok {
			start = se
			break
		}
	}
	switch {
	case start.Name.Local == "rss":
		var v rss2
		if err := dec.DecodeElement(&v, &start); err != nil {
			return Document{}, fmt.Errorf("RSS 2.0 を読めない: %w", err)
		}
		return fromRSS("rss2", v.Channel.Title, v.Channel.Items), nil
	case start.Name.Local == "feed" && (start.Name.Space == nsAtom || start.Name.Space == ""):
		var v atomFeed
		if err := dec.DecodeElement(&v, &start); err != nil {
			return Document{}, fmt.Errorf("Atom を読めない: %w", err)
		}
		return fromAtom(v), nil
	case start.Name.Local == "RDF" && (start.Name.Space == nsRDF || start.Name.Space == ""):
		var v rss1
		if err := dec.DecodeElement(&v, &start); err != nil {
			return Document{}, fmt.Errorf("RSS 1.0 を読めない: %w", err)
		}
		return fromRSS("rss1", v.Channel.Title, v.Items), nil
	}
	return Document{}, fmt.Errorf("%w(最初の要素 <%s>)", ErrNotFeed, start.Name.Local)
}

// charsetReader は XML 宣言の encoding に応じてバイト列を UTF-8 に直す。
// 標準ライブラリだけで扱えるのは UTF-8 と 1 バイト = 1 文字の ISO-8859-1 系まで。それ以外は読めないと伝える
// (黙って化けた文字を索引に入れない)。
func charsetReader(label string, r io.Reader) (io.Reader, error) {
	switch strings.ToLower(strings.TrimSpace(label)) {
	case "", "utf-8", "utf8":
		return r, nil
	case "iso-8859-1", "latin1", "latin-1", "us-ascii", "ascii":
		b, err := io.ReadAll(r)
		if err != nil {
			return nil, err
		}
		var sb strings.Builder
		for _, c := range b {
			sb.WriteRune(rune(c))
		}
		return strings.NewReader(sb.String()), nil
	}
	return nil, fmt.Errorf("文字コード %q には対応していない(UTF-8 か ISO-8859-1 のフィードだけ読める)", label)
}

// ---- 形式ごとの構造体 ----
// 要素名に名前空間を付けない指定は、どの名前空間の要素にも一致する(RSS 1.0 の item は purl.org の名前空間にある)。

type rss2 struct {
	Channel struct {
		Title string    `xml:"title"`
		Items []rssItem `xml:"item"`
	} `xml:"channel"`
}

type rss1 struct {
	Channel struct {
		Title string `xml:"title"`
	} `xml:"channel"`
	Items []rssItem `xml:"item"` // RSS 1.0 では channel の外(rdf:RDF 直下)に並ぶ
}

type rssItem struct {
	Title       string `xml:"title"`
	Link        string `xml:"link"`
	GUID        string `xml:"guid"`
	Description string `xml:"description"`
	Encoded     string `xml:"http://purl.org/rss/1.0/modules/content/ encoded"`
	PubDate     string `xml:"pubDate"`
	DCDate      string `xml:"http://purl.org/dc/elements/1.1/ date"`
}

type atomFeed struct {
	Title   atomText    `xml:"title"`
	Entries []atomEntry `xml:"entry"`
}

type atomEntry struct {
	Title     atomText   `xml:"title"`
	Links     []atomLink `xml:"link"`
	ID        string     `xml:"id"`
	Summary   atomText   `xml:"summary"`
	Content   atomText   `xml:"content"`
	Published string     `xml:"published"`
	Updated   string     `xml:"updated"`
}

// atomText は Atom の Text Construct(title / summary / content)。type="xhtml" だと本文が子要素として入れ子になり、
// 文字列フィールドでは拾えないので、要素の中の文字データを入れ子ごと連結する。
// type="html" の場合は文字データが HTML そのものなので、後段の cleanText がタグを除く。
type atomText struct {
	Text string
}

func (t *atomText) UnmarshalXML(d *xml.Decoder, start xml.StartElement) error {
	var sb strings.Builder
	depth := 1
	for depth > 0 {
		tok, err := d.Token()
		if err != nil {
			return err
		}
		switch v := tok.(type) {
		case xml.StartElement:
			depth++
		case xml.EndElement:
			depth--
		case xml.CharData:
			sb.Write(v)
		}
	}
	t.Text = sb.String()
	return nil
}

type atomLink struct {
	Href string `xml:"href,attr"`
	Rel  string `xml:"rel,attr"`
}

func fromRSS(format, title string, items []rssItem) Document {
	d := Document{Format: format, Title: cleanText(title)}
	for _, it := range items {
		link := strings.TrimSpace(it.Link)
		if link == "" && isHTTP(it.GUID) {
			link = strings.TrimSpace(it.GUID)
		}
		summary := it.Description
		if strings.TrimSpace(summary) == "" {
			summary = it.Encoded
		}
		d.Entries = append(d.Entries, newEntry(it.Title, link, summary, it.PubDate, it.DCDate))
	}
	return d
}

func fromAtom(v atomFeed) Document {
	d := Document{Format: "atom", Title: cleanText(v.Title.Text)}
	for _, e := range v.Entries {
		link := atomAlternate(e.Links)
		if link == "" && isHTTP(e.ID) {
			link = strings.TrimSpace(e.ID)
		}
		summary := e.Summary.Text
		if strings.TrimSpace(summary) == "" {
			summary = e.Content.Text
		}
		d.Entries = append(d.Entries, newEntry(e.Title.Text, link, summary, e.Published, e.Updated))
	}
	return d
}

// atomAlternate は rel="alternate"(省略も同じ)のリンクを返す。enclosure や self は記事の URL ではない。
func atomAlternate(links []atomLink) string {
	for _, l := range links {
		if l.Rel == "" || l.Rel == "alternate" {
			if h := strings.TrimSpace(l.Href); h != "" {
				return h
			}
		}
	}
	return ""
}

func isHTTP(s string) bool {
	s = strings.TrimSpace(s)
	return strings.HasPrefix(s, "http://") || strings.HasPrefix(s, "https://")
}

// newEntry は各形式の生の値から Entry を組む。dates は優先順(先に読めたものを採る)。
func newEntry(title, link, summary string, dates ...string) Entry {
	t := cleanText(title)
	if t == "" {
		t = "(無題)"
	}
	e := Entry{Title: t, Link: link, Summary: CleanSummary(summary, SummaryLimit)}
	for _, d := range dates {
		if p := ParseDate(d); p != "" {
			e.Published = p
			break
		}
	}
	e.ID = EntryID(link, t)
	return e
}
