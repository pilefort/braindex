package news

import "testing"

// 先頭に UTF-8 BOM が付いた feeds.json も読める。
//
// braindex.json と違い feeds.json は braindex init が配らず利用者が丸ごと手で書くので
// (README「フィード一覧 news/feeds.json は自分で作る」)、BOM を付けるエディタで
// 書かれる確率はこちらの方が高い。決定 2026-09-02「入力側は BOM 除去と
// CRLF・CR の正規化をしてから解析する」の対象。
func TestParseFeeds_BOM(t *testing.T) {
	body := `[{"name": "A", "url": "https://example.com/feed.xml", "layer": "daily"}]`
	srcs, err := ParseFeeds(append([]byte{0xEF, 0xBB, 0xBF}, body...), "feeds.json")
	if err != nil {
		t.Fatalf("BOM 付きを読めない: %v", err)
	}
	if len(srcs) != 1 || srcs[0].Name != "A" {
		t.Errorf("BOM の後ろが読めていない: %+v", srcs)
	}
}

// 落とすのは先頭の 1 個だけ。値の中の U+FEFF は JSON 文字列の合法な文字なので保たれる。
//
// 入力は生バイト、期待値は Go のエスケープ。生の BOM を Go のソースに置けない
// (illegal byte order mark)のは期待値の側だけで、入力までエスケープで書くと
// BOM 除去コードが触りうるバイトが 1 つも無いテストになる。
func TestParseFeeds_BOM_途中のものは消さない(t *testing.T) {
	body := []byte("[{\"name\": \"A\xEF\xBB\xBFB\", \"url\": \"https://example.com/f.xml\", \"layer\": \"daily\"}]")
	srcs, err := ParseFeeds(body, "feeds.json")
	if err != nil {
		t.Fatalf("ParseFeeds: %v", err)
	}
	if len(srcs) != 1 || srcs[0].Name != "A\ufeffB" {
		t.Errorf("値の中の U+FEFF が変わった: %q", srcs[0].Name)
	}
}
