package news

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"

	"github.com/pilefort/braindex/internal/interest"
)

// FeedRequest は画面で選んだ URL と、検索フィードの場合だけ承認した語。
type FeedRequest struct {
	URL   string `json:"url"`
	Query string `json:"query,omitempty"`
}

// AddFeeds は名前などを手元で引き直し、既存の整形を保って末尾へ足す。
// 選別 JSON の自己申告は信用せず、目録か承認した検索語と一致する URL だけを通す。
func AddFeeds(path string, catalog []CatalogEntry, reqs []FeedRequest, layer string, date string) (added []string, msgs []string, err error) {
	existing, err := os.ReadFile(path)
	if err != nil {
		return nil, nil, err
	}
	srcs, err := ParseFeeds(existing, path)
	if err != nil {
		return nil, nil, err
	}
	fi, err := os.Stat(path)
	if err != nil {
		return nil, nil, err
	}
	urls, names := map[string]bool{}, map[string]bool{}
	for _, s := range srcs {
		urls[normalizeURL(s.URL)], names[s.Name] = true, true
	}
	if layer == LayerAll {
		layer = ""
	}
	var rows [][]byte
	for _, req := range reqs {
		s := Source{Layer: layer, Note: "braindex news の候補から追加（" + date + "）"}
		if req.Query == "" {
			e, ok := LookupCatalog(catalog, req.URL)
			if !ok {
				msgs = append(msgs, fmt.Sprintf("目録に無い URL: %q", req.URL))
				continue
			}
			s.Name, s.URL, s.Lang, s.Category = e.Name, e.URL, e.Lang(), e.Genre
			if e.IsGeneralNews() {
				s.Layer = LayerGeneralNews
			}
		} else {
			if req.URL != SearchFeedURL(req.Query) {
				msgs = append(msgs, fmt.Sprintf("検索語と URL が合わない: %q", req.URL))
				continue
			}
			if ws := interest.Words(req.Query); len(ws) != 1 || ws[0] != req.Query {
				msgs = append(msgs, fmt.Sprintf("語の規則を通らない検索語: %q", req.Query))
				continue
			}
			s.Name, s.URL, s.Lang, s.Category = "検索: "+req.Query, SearchFeedURL(req.Query), SearchFeedLang(req.Query), "検索"
		}
		if urls[normalizeURL(s.URL)] {
			continue
		}
		if names[s.Name] {
			msgs = append(msgs, "同名の取材先が別 URL で登録済み: "+s.Name)
			continue
		}
		// layer が空でもキーを明示する。キー順は利用者向けの登録形式に固定する。
		// json.Marshal は & を \u0026 にするので、手で書くファイルに合わせて Encoder で HTML エスケープを切る
		var rowBuf bytes.Buffer
		enc := json.NewEncoder(&rowBuf)
		enc.SetEscapeHTML(false)
		if merr := enc.Encode(struct {
			Name     string `json:"name"`
			URL      string `json:"url"`
			Layer    string `json:"layer"`
			Lang     string `json:"lang"`
			Category string `json:"category"`
			Note     string `json:"note"`
		}{s.Name, s.URL, s.Layer, s.Lang, s.Category, s.Note}); merr != nil {
			return nil, msgs, merr
		}
		rows = append(rows, bytes.TrimRight(rowBuf.Bytes(), "\n"))
		added = append(added, s.Name)
		urls[normalizeURL(s.URL)], names[s.Name] = true, true
	}
	if len(rows) == 0 {
		return nil, msgs, nil
	}
	end := bytes.LastIndexByte(existing, ']')
	start := bytes.IndexByte(existing, '[')
	if start < 0 || end < start {
		return nil, msgs, fmt.Errorf("フィード一覧 %s: JSON の配列でない", path)
	}
	var buf bytes.Buffer
	if len(srcs) == 0 {
		buf.Write(existing[:start+1])
	} else {
		// 末尾要素直後にカンマを挿し、それ以降の空白もそのまま保つ。
		last := end
		for last > start && bytes.ContainsRune([]byte(" \t\r\n"), rune(existing[last-1])) {
			last--
		}
		buf.Write(existing[:last])
		buf.WriteByte(',')
		buf.Write(existing[last:end])
	}
	for i, row := range rows {
		if i > 0 {
			buf.WriteByte(',')
		}
		buf.WriteString("\n  ")
		buf.Write(row)
	}
	buf.WriteByte('\n')
	buf.Write(existing[end:])
	if _, err := ParseFeeds(buf.Bytes(), path); err != nil {
		return nil, msgs, err
	}
	if err := writeAtomic(path, buf.Bytes(), fi.Mode().Perm()); err != nil {
		return nil, msgs, err
	}
	return added, msgs, nil
}
