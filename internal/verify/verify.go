// Package verify は調査結果の「最も幻覚しやすい事実」を一次ソースへの GET だけで機械照合する。
//
// 対象は GitHub リポの実在・スター数・作成日、arXiv 論文の実在とタイトル、URL の生存、出典ページに逐語引用が
// 実在するか。原型は research-distill skill の verify.py(2026-08-16)。外へ送るのは公開 URL・リポ名・arXiv ID だけで、
// 照合は取得後にローカルで行う。判定は「実在・一致」だけで、真偽の意味判断はしない(索引・判定に LLM を入れない設計と同じ)。
package verify

import (
	"encoding/json"
	"fmt"
	"html"
	"regexp"
	"strings"
	"unicode/utf8"
)

// MinQuoteLen は逐語引用の最短文字数。これ未満はどこにでも偶然一致して照合が儀式化するので拒否する(原型と同じ 24)。
const MinQuoteLen = 24

// 判定。
const (
	Found    = "FOUND"
	NotFound = "NOT FOUND"
	Error    = "ERROR"
)

// Response は GET の結果。Fetcher が返す。
type Response struct {
	Status   int
	FinalURL string
	Body     string
}

// Fetcher は GET だけを行う取得器。テストでは固定レスポンスに差し替える。
type Fetcher interface {
	Get(url string) (*Response, error)
}

// Result は 1 件の照合結果。
type Result struct {
	Kind   string `json:"kind"`   // github / arxiv / url / quote
	Target string `json:"target"` // リポ名・ID・URL・引用文
	Status string `json:"status"` // FOUND / NOT FOUND / ERROR
	Detail string `json:"detail"` // 実測値(スター数・タイトル・HTTP 状態など)か、失敗の理由
}

// Line は 1 件 1 行の表示形(タブ区切り)。
func (r Result) Line() string {
	return r.Kind + "\t" + r.Target + "\t" + r.Status + "\t" + r.Detail
}

// GitHubRepo は GitHub API /repos の要点。
type GitHubRepo struct {
	FullName  string `json:"full_name"`
	Stars     int    `json:"stargazers_count"`
	CreatedAt string `json:"created_at"`
}

// ParseGitHub は /repos のレスポンス JSON から要点を取り出す。stargazers_count が無ければ API のメッセージを error にする。
func ParseGitHub(body string) (GitHubRepo, error) {
	var raw map[string]json.RawMessage
	if err := json.Unmarshal([]byte(body), &raw); err != nil {
		return GitHubRepo{}, fmt.Errorf("JSON でない: %w", err)
	}
	if _, ok := raw["stargazers_count"]; !ok {
		var msg string
		_ = json.Unmarshal(raw["message"], &msg)
		if msg == "" {
			msg = "unknown"
		}
		return GitHubRepo{}, fmt.Errorf("%s", msg)
	}
	var r GitHubRepo
	if err := json.Unmarshal([]byte(body), &r); err != nil {
		return GitHubRepo{}, err
	}
	return r, nil
}

var titleRE = regexp.MustCompile(`(?is)<title>(.*?)</title>`)

// ParseArxivTitle は arXiv 要旨ページの <title> の中身を返す。無ければ空。
func ParseArxivTitle(page string) string {
	m := titleRE.FindStringSubmatch(page)
	if m == nil {
		return ""
	}
	return strings.TrimSpace(m[1])
}

var (
	scriptRE = regexp.MustCompile(`(?is)<script\b.*?</script\s*>`)
	styleRE  = regexp.MustCompile(`(?is)<style\b.*?</style\s*>`)
	tagRE    = regexp.MustCompile(`<[^>]*>`)
	spaceRE  = regexp.MustCompile(`\s+`)
)

// StripHTML は HTML を平文にする。script/style を除き、タグを空白に置換し、実体参照を戻し、空白を畳む。
func StripHTML(page string) string {
	t := scriptRE.ReplaceAllString(page, " ")
	t = styleRE.ReplaceAllString(t, " ")
	t = tagRE.ReplaceAllString(t, " ")
	t = html.UnescapeString(t)
	return strings.TrimSpace(spaceRE.ReplaceAllString(t, " "))
}

// CheckQuoteText は引用が出典 HTML の本文に逐語で存在するかを返す。許すのは空白の揺れだけで、
// 言い換えを弾くのが目的なので曖昧一致にしない。MinQuoteLen 未満は照合拒否(Error)。
func CheckQuoteText(page, quote string) (status, detail string) {
	q := strings.TrimSpace(spaceRE.ReplaceAllString(quote, " "))
	if utf8.RuneCountInString(q) < MinQuoteLen {
		return Error, fmt.Sprintf("%d 字未満の引用は照合しない", MinQuoteLen)
	}
	if strings.Contains(StripHTML(page), q) {
		return Found, "逐語一致"
	}
	return NotFound, "本文に逐語では無い(言い換え・誤引用の疑い)"
}

// GitHub はリポの実在・スター数・作成日を照合する。
func GitHub(f Fetcher, repo string) Result {
	r := Result{Kind: "github", Target: repo}
	res, err := f.Get("https://api.github.com/repos/" + repo)
	if err != nil {
		return fail(r, err)
	}
	if res.Status == 404 {
		r.Status, r.Detail = NotFound, "HTTP 404"
		return r
	}
	if res.Status != 200 {
		r.Status, r.Detail = Error, fmt.Sprintf("HTTP %d", res.Status)
		return r
	}
	g, err := ParseGitHub(res.Body)
	if err != nil {
		return fail(r, err)
	}
	r.Status = Found
	r.Detail = fmt.Sprintf("%s stars=%d created=%s", g.FullName, g.Stars, g.CreatedAt)
	return r
}

// Arxiv は論文の実在とタイトルを要旨ページで照合する。
func Arxiv(f Fetcher, id string) Result {
	r := Result{Kind: "arxiv", Target: id}
	res, err := f.Get("https://arxiv.org/abs/" + id)
	if err != nil {
		return fail(r, err)
	}
	if res.Status == 404 {
		r.Status, r.Detail = NotFound, "HTTP 404"
		return r
	}
	if res.Status != 200 {
		r.Status, r.Detail = Error, fmt.Sprintf("HTTP %d", res.Status)
		return r
	}
	title := ParseArxivTitle(res.Body)
	if title == "" {
		r.Status, r.Detail = NotFound, "要旨ページに <title> が無い"
		return r
	}
	r.Status, r.Detail = Found, title
	return r
}

// URL は出典 URL の生存(HTTP 200)を確認する。
func URL(f Fetcher, u string) Result {
	r := Result{Kind: "url", Target: u}
	res, err := f.Get(u)
	if err != nil {
		return fail(r, err)
	}
	if res.Status == 200 {
		r.Status, r.Detail = Found, "HTTP 200 "+res.FinalURL
		return r
	}
	r.Status, r.Detail = NotFound, fmt.Sprintf("HTTP %d", res.Status)
	return r
}

// Quotes は出典ページを 1 回取得し、各引用が逐語で実在するかを照合する。
func Quotes(f Fetcher, u string, quotes []string) []Result {
	out := make([]Result, 0, len(quotes))
	res, err := f.Get(u)
	for _, q := range quotes {
		r := Result{Kind: "quote", Target: q}
		switch {
		case err != nil:
			r = fail(r, err)
		case res.Status != 200:
			r.Status, r.Detail = Error, fmt.Sprintf("HTTP %d %s", res.Status, u)
		default:
			r.Status, r.Detail = CheckQuoteText(res.Body, q)
		}
		out = append(out, r)
	}
	return out
}

func fail(r Result, err error) Result {
	r.Status, r.Detail = Error, err.Error()
	return r
}
