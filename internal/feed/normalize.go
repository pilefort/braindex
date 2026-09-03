package feed

import (
	"crypto/sha256"
	"encoding/hex"
	"html"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"
)

// SummaryLimit は概要の表示上限(文字数)。超えた分は切って "…" を付ける。
const SummaryLimit = 240

// trackingParam は追跡用のクエリパラメータ名。記事の同一性に関係しないので、ID の計算前に取り除く。
var trackingParam = regexp.MustCompile(`^(utm_[a-z]+|ref|source|cmpid)$`)

// NormalizeLink は追跡パラメータ(utm_*・ref・source・cmpid)を除き、末尾の "/" "?" "&" "#" を落とす。
// 残るクエリの順序は保つ(順序を並べ替えると別 ID になる記事が出るため)。
func NormalizeLink(u string) string {
	u = strings.TrimSpace(u)
	if u == "" {
		return ""
	}
	// フラグメントは記事の同一性に関係しない
	if i := strings.IndexByte(u, '#'); i >= 0 {
		u = u[:i]
	}
	base, query, hasQuery := strings.Cut(u, "?")
	if hasQuery {
		var keep []string
		for _, kv := range strings.Split(query, "&") {
			if kv == "" {
				continue
			}
			key, _, _ := strings.Cut(kv, "=")
			if trackingParam.MatchString(key) {
				continue
			}
			keep = append(keep, kv)
		}
		u = base
		if len(keep) > 0 {
			u += "?" + strings.Join(keep, "&")
		}
	}
	return strings.TrimRight(u, "/?&")
}

// EntryID は記事の識別子。正規化したリンク(無ければタイトル)の SHA-256 の先頭 16 桁(16 進)。
func EntryID(link, title string) string {
	basis := NormalizeLink(link)
	if basis == "" {
		basis = strings.TrimSpace(title)
	}
	sum := sha256.Sum256([]byte(basis))
	return hex.EncodeToString(sum[:8])
}

var (
	tagRe = regexp.MustCompile(`<[^>]*>`)
	wsRe  = regexp.MustCompile(`\s+`)
)

// cleanText は HTML のタグを除き、実体参照を戻し、空白を 1 つに畳む(タイトル・フィード名用。切り詰めない)。
func cleanText(raw string) string {
	t := html.UnescapeString(tagRe.ReplaceAllString(raw, " "))
	return strings.TrimSpace(wsRe.ReplaceAllString(t, " "))
}

// CleanSummary は description / summary(HTML 混じり)を素のテキストにして limit 文字で切り詰める。
// limit <= 0 なら切らない。切ったときは末尾に "…" を付ける(文字数は limit+1)。
func CleanSummary(raw string, limit int) string {
	t := cleanText(raw)
	if limit <= 0 || utf8.RuneCountInString(t) <= limit {
		return t
	}
	r := []rune(t)
	return strings.TrimRight(string(r[:limit]), " ") + "…"
}

// dateLayouts はフィードで見かける日付の書式。RSS 2.0 は RFC 822 系、Atom と dc:date は RFC 3339 系。
var dateLayouts = []string{
	time.RFC1123Z,
	time.RFC1123,
	time.RFC822Z,
	time.RFC822,
	"Mon, 2 Jan 2006 15:04:05 -0700", // 日が 1 桁
	"Mon, 2 Jan 2006 15:04:05 MST",
	"2 Jan 2006 15:04:05 -0700", // 曜日無し
	"2 Jan 2006 15:04:05 MST",
	"02 Jan 2006 15:04:05 -0700",
	"02 Jan 2006 15:04:05 MST",
	time.RFC3339,
	time.RFC3339Nano,
	"2006-01-02T15:04:05", // タイムゾーン無し(UTC とみなす)
	"2006-01-02 15:04:05Z07:00",
	"2006-01-02 15:04:05",
	"2006-01-02",
}

// ParseDate はフィードの日付文字列を UTC の暦日 YYYY-MM-DD にする。読めなければ空。
// 暦日を UTC に揃えるのは、同じ記事が実行環境のタイムゾーンで別の日にならないようにするため。
//
// 基準のゾーンを UTC に固定する(time.Parse ではなく ParseInLocation)。time.Parse はゾーンの略称
// (JST・EST など)を実行環境のローカルゾーンで解決するので、同じ "…08:00:00 JST" が JST のマシンでは
// 前日、UTC のマシンでは当日になっていた。オフセット付き(+09:00 等)の日付はこの指定の影響を受けない。
func ParseDate(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}
	for _, layout := range dateLayouts {
		if t, err := time.ParseInLocation(layout, s, time.UTC); err == nil {
			return t.UTC().Format("2006-01-02")
		}
	}
	return ""
}
