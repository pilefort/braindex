package feed

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"time"
)

// 取得の既定値。
const (
	DefaultUserAgent = "braindex-news/1.0 (+https://github.com/pilefort/braindex)"
	DefaultTimeout   = 20 * time.Second
	DefaultMaxBytes  = 10 << 20 // 10 MiB。これを超えるフィードは読まない(暴走した応答で止まらないため)
	maxRedirects     = 10
)

// Fetcher はフィードを GET してパースする。ゼロ値で使えるが、テストでは Client を差し替える。
type Fetcher struct {
	Client    *http.Client // nil なら DefaultTimeout・リダイレクト maxRedirects 回までのクライアント
	UserAgent string       // 空なら DefaultUserAgent
	MaxBytes  int64        // 0 なら DefaultMaxBytes
}

// ErrTooLarge は応答が MaxBytes を超えたとき。
var ErrTooLarge = errors.New("応答が大きすぎる")

// Fetch は url を GET してフィードにする。送るのは URL と User-Agent・Accept だけ(本文・Cookie は無い)。
// 4xx/5xx は "HTTP <code>" のエラー、本文が MaxBytes を超えれば ErrTooLarge、XML がフィードでなければ ErrNotFeed(を包む)。
func (f Fetcher) Fetch(ctx context.Context, url string) (Document, error) {
	client := f.Client
	if client == nil {
		client = &http.Client{
			Timeout: DefaultTimeout,
			CheckRedirect: func(req *http.Request, via []*http.Request) error {
				if len(via) >= maxRedirects {
					return fmt.Errorf("リダイレクトが %d 回を超えた", maxRedirects)
				}
				return nil
			},
		}
	}
	ua := f.UserAgent
	if ua == "" {
		ua = DefaultUserAgent
	}
	max := f.MaxBytes
	if max <= 0 {
		max = DefaultMaxBytes
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return Document{}, err
	}
	req.Header.Set("User-Agent", ua)
	req.Header.Set("Accept", "application/rss+xml, application/atom+xml, application/rdf+xml, application/xml, text/xml;q=0.9, */*;q=0.5")
	resp, err := client.Do(req)
	if err != nil {
		return Document{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return Document{}, fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	b, err := io.ReadAll(io.LimitReader(resp.Body, max+1))
	if err != nil {
		return Document{}, err
	}
	if int64(len(b)) > max {
		return Document{}, fmt.Errorf("%w(%d バイト超)", ErrTooLarge, max)
	}
	doc, err := ParseBytes(b)
	if err != nil {
		return Document{}, err
	}
	for i := range doc.Entries {
		e := &doc.Entries[i]
		e.ID = EntryID(e.Link, e.Title, url)
	}
	return doc, nil
}
