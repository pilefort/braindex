package weblink

import "testing"

func TestSafe(t *testing.T) {
	cases := []struct {
		in   string
		want bool
	}{
		{"https://example.com/a", true},
		{"http://example.com/a", true},
		{"HTTPS://EXAMPLE.COM", true},     // スキームは大小を区別しない
		{"  https://example.com  ", true}, // 前後の空白は無視する
		{"docs/notes/a.md", true},         // 相対パス
		{"#見出し", true},                    // 同一文書内リンク
		{"./a.md#x", true},
		{"a:b/c.md", false}, // コロンが先に来るので、スキームとして扱う
		{"./a:b.md", true},  // / が先なのでスキームではない
		{"javascript:alert(1)", false},
		{"JavaScript:alert(1)", false},
		{"data:text/html;base64,xxx", false},
		{"vbscript:msgbox", false},
		{"file:///C:/x", false},
		{"mailto:a@example.com", false},
		{"", true}, // 空はリンクにならないので、呼び出し側の扱いに任せる
	}
	for _, c := range cases {
		if got := Safe(c.in); got != c.want {
			t.Errorf("Safe(%q) = %v want %v", c.in, got, c.want)
		}
	}
}
