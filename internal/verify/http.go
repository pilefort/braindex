package verify

import (
	"io"
	"net/http"
	"os"
	"time"
)

// userAgent は照合の GET に付ける UA。何が取りに来たかを相手に示すため。
const userAgent = "braindex-verify/1.0 (local; GET-only)"

// maxBody は読む本文の上限。逐語引用の照合には十分で、巨大な応答でメモリを食わないため。
const maxBody = 8 << 20

// HTTPFetcher は net/http で GET する Fetcher。リダイレクトに追従し、最終 URL を返す。
// api.github.com 宛てで環境変数 GITHUB_TOKEN があれば Authorization に付ける(レート制限対策・任意)。
type HTTPFetcher struct {
	Client *http.Client
	Token  string // 空なら環境変数 GITHUB_TOKEN
}

// NewHTTPFetcher は既定(タイムアウト 20 秒)の取得器を返す。
func NewHTTPFetcher() *HTTPFetcher {
	return &HTTPFetcher{Client: &http.Client{Timeout: 20 * time.Second}}
}

// Get は GET だけを行う。本文は送らない。
func (h *HTTPFetcher) Get(url string) (*Response, error) {
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", userAgent)
	if req.URL.Host == "api.github.com" {
		req.Header.Set("Accept", "application/vnd.github+json")
		tok := h.Token
		if tok == "" {
			tok = os.Getenv("GITHUB_TOKEN")
		}
		if tok != "" {
			req.Header.Set("Authorization", "Bearer "+tok)
		}
	}
	res, err := h.Client.Do(req)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	body, err := io.ReadAll(io.LimitReader(res.Body, maxBody))
	if err != nil {
		return nil, err
	}
	return &Response{Status: res.StatusCode, FinalURL: res.Request.URL.String(), Body: string(body)}, nil
}
