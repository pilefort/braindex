package verify

import (
	"io"
	"net/http"
	"os"
	"time"
)

// userAgent は照合の GET に付ける UA。何が取りに来たかを相手に示すため。
const userAgent = "braindex-verify/1.0 (local; GET-only)"

// maxBody は読む本文の既定の上限。逐語引用の照合には十分で、巨大な応答でメモリを食わないため。
// この値そのものは変えない。打ち切ったかどうかは Response.Truncated に出るので、
// 呼び出し側(Quotes)は打ち切り以降を「無い」と断定しないよう扱える。
const maxBody = 8 << 20

// HTTPFetcher は net/http で GET する Fetcher。リダイレクトに追従し、最終 URL を返す。
// api.github.com 宛てで環境変数 GITHUB_TOKEN があれば Authorization に付ける(レート制限対策・任意)。
type HTTPFetcher struct {
	Client  *http.Client
	Token   string // 空なら環境変数 GITHUB_TOKEN
	MaxBody int64  // 読む本文の上限(バイト)。0 なら既定の maxBody。テストで小さい値に差し替えて打ち切りを再現する。
}

// NewHTTPFetcher は既定(タイムアウト 20 秒)の取得器を返す。
func NewHTTPFetcher() *HTTPFetcher {
	return &HTTPFetcher{Client: &http.Client{Timeout: 20 * time.Second}}
}

// limit は実際に読む本文の上限を返す。MaxBody 未設定(0)なら既定の maxBody。
func (h *HTTPFetcher) limit() int64 {
	if h.MaxBody > 0 {
		return h.MaxBody
	}
	return maxBody
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
	limit := h.limit()
	// 上限ちょうどで打ち切ったかを判別するため、上限より 1 バイト多く読む。
	body, err := io.ReadAll(io.LimitReader(res.Body, limit+1))
	if err != nil {
		return nil, err
	}
	truncated := false
	if int64(len(body)) > limit {
		body = body[:limit]
		truncated = true
	}
	return &Response{Status: res.StatusCode, FinalURL: res.Request.URL.String(), Body: string(body), Truncated: truncated}, nil
}
