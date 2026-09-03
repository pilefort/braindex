package news

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func selectionJSON(date, layer string, keeps string, stats string) string {
	return `{"type": "braindex-news-selection", "date": "` + date + `", "layer": "` + layer + `", "exported_at": "2026-08-15T10:00:00Z",
	 "keeps": [` + keeps + `], "feed_stats": {` + stats + `}}`
}

func TestParseSelection(t *testing.T) {
	s, err := ParseSelection([]byte(selectionJSON("2026-08-15", "daily",
		`{"id": "a", "title": "残す記事", "link": "https://x/keep", "feed": "F1", "category": "tech", "score": "2", "rescued": false}`,
		`"F1": {"shown": 10, "kept": 1, "dropped": 4}`)))
	if err != nil || s.Date != "2026-08-15" || len(s.Keeps) != 1 || s.Keeps[0].Title != "残す記事" || s.FeedStats["F1"].Kept != 1 || s.FeedStats["F1"].Hidden != 0 {
		t.Errorf("err=%v %+v", err, s)
	}
	for _, in := range []string{`{}`, `{"type": "other"}`, `not json`} {
		if _, err := ParseSelection([]byte(in)); !errors.Is(err, ErrNotSelection) {
			t.Errorf("%s: err=%v", in, err)
		}
	}
	// feed_stats が無くても map は空で返る
	if s, err := ParseSelection([]byte(`{"type": "braindex-news-selection"}`)); err != nil || s.FeedStats == nil {
		t.Errorf("feed_stats 無し: err=%v %+v", err, s)
	}
}

func TestStats(t *testing.T) {
	st := Stats{Digests: map[string]map[string]FeedStats{
		"2026-08-15_daily": {"F1": {Shown: 15, Kept: 1, Dropped: 4}},
		"2026-08-16_daily": {"F1": {Shown: 10, Dropped: 2, Hidden: 10, Rescued: 1}, "F2": {Shown: 3, Kept: 3}},
	}}
	tot := st.Totals()
	if tot["F1"] != (FeedStats{Shown: 25, Kept: 1, Dropped: 6, Hidden: 10, Rescued: 1}) || tot["F2"].Kept != 3 {
		t.Errorf("Totals: %+v", tot)
	}

	totals := map[string]FeedStats{
		"良":     {Shown: 30, Kept: 5, Dropped: 10},
		"悪":     {Shown: 25, Dropped: 20},
		"まだ":    {Shown: 5, Dropped: 5},
		"全部関心外": {Hidden: 30},
		"救済あり":  {Hidden: 30, Rescued: 1},
	}
	if got := PruneCandidates(totals, 20); !reflect.DeepEqual(got, []string{"全部関心外", "悪"}) {
		t.Errorf("PruneCandidates: %v", got)
	}

	// 往復と決定性
	path := filepath.Join(t.TempDir(), ".stats.json")
	if err := st.Save(path); err != nil {
		t.Fatal(err)
	}
	b1, _ := os.ReadFile(path)
	st2, err := LoadStats(path)
	if err != nil || !reflect.DeepEqual(st, st2) {
		t.Errorf("往復: err=%v %+v", err, st2)
	}
	st2.Save(path)
	if b2, _ := os.ReadFile(path); string(b1) != string(b2) {
		t.Error("2 回の保存が一致しない")
	}
	if empty, err := LoadStats(filepath.Join(t.TempDir(), "none.json")); err != nil || len(empty.Digests) != 0 {
		t.Errorf("無いとき: err=%v %+v", err, empty)
	}
}

func TestKeepMarkdown(t *testing.T) {
	md := KeepMarkdown([]Keep{{Title: "T [x]", Link: "https://x/1", Feed: "F"}, {Link: "https://x/2", Feed: "G", Rescued: true}}, "2026-08-15", "daily")
	want := "\n## 2026-08-15（daily）\n\n- [T ［x］](https://x/1) — F\n- [(無題)](https://x/2) — G（関心外から救済）\n"
	if md != want {
		t.Errorf("got:\n%s\nwant:\n%s", md, want)
	}
}

func TestIngest(t *testing.T) {
	newsDir := filepath.Join(t.TempDir(), "news")
	inbox := filepath.Join(newsDir, "inbox")
	os.MkdirAll(inbox, 0o755)
	keeps := `{"id": "a", "title": "残す記事", "link": "https://x/keep", "feed": "F1", "category": "tech"}`
	os.WriteFile(filepath.Join(inbox, "braindex-news-selection_2026-08-15_daily_1.json"),
		[]byte(selectionJSON("2026-08-15", "daily", keeps, `"F1": {"shown": 10, "kept": 1, "dropped": 4}`)), 0o644)
	os.WriteFile(filepath.Join(inbox, "braindex-news-selection_bad.json"), []byte(`{"type": "other"}`), 0o644)
	os.WriteFile(filepath.Join(inbox, "unrelated.json"), []byte(`{}`), 0o644)

	msgs, err := Ingest(newsDir, []string{inbox, filepath.Join(newsDir, "no-such-dir")})
	if err != nil {
		t.Fatal(err)
	}
	// 名前順(数字 < 英字)なので取り込みが先、形式違いが後
	if len(msgs) != 2 || msgs[0] != "取り込み: braindex-news-selection_2026-08-15_daily_1.json（残す 1 件）" || !strings.Contains(msgs[1], "飛ばした") || !strings.Contains(msgs[1], "_bad.json") {
		t.Errorf("msgs: %q", msgs)
	}
	keepMD, err := os.ReadFile(filepath.Join(newsDir, "keep", "2026-08.md"))
	if err != nil || string(keepMD) != "# 選別済みニュース 2026-08\n\n## 2026-08-15（daily）\n\n- [残す記事](https://x/keep) — F1\n" {
		t.Errorf("keep: err=%v\n%s", err, keepMD)
	}
	st, _ := LoadStats(filepath.Join(newsDir, StatsFile))
	if st.Digests["2026-08-15_daily"]["F1"].Kept != 1 {
		t.Errorf("stats: %+v", st)
	}
	// 取り込んだものは移動、形式違いと無関係なファイルは残る
	if _, err := os.Stat(filepath.Join(newsDir, IngestedDir, "braindex-news-selection_2026-08-15_daily_1.json")); err != nil {
		t.Error("取り込み済みへ移っていない")
	}
	if _, err := os.Stat(filepath.Join(inbox, "braindex-news-selection_bad.json")); err != nil {
		t.Error("形式違いのファイルが消えた")
	}
	if _, err := os.Stat(filepath.Join(inbox, "unrelated.json")); err != nil {
		t.Error("無関係なファイルが消えた")
	}

	// 同じダイジェストの再書き出し: keep は重複しない・統計は上書き(二重計上しない)
	os.WriteFile(filepath.Join(inbox, "braindex-news-selection_2026-08-15_daily_2.json"),
		[]byte(selectionJSON("2026-08-15", "daily", keeps+`, {"id": "b", "title": "追加", "link": "https://x/more", "feed": "F1", "rescued": true}`,
			`"F1": {"shown": 10, "kept": 2, "dropped": 6, "hidden": 5, "rescued": 1}`)), 0o644)
	if _, err := Ingest(newsDir, []string{inbox}); err != nil {
		t.Fatal(err)
	}
	keepMD, _ = os.ReadFile(filepath.Join(newsDir, "keep", "2026-08.md"))
	if strings.Count(string(keepMD), "https://x/keep") != 1 || !strings.Contains(string(keepMD), "- [追加](https://x/more) — F1（関心外から救済）") {
		t.Errorf("keep 2 回目:\n%s", keepMD)
	}
	st, _ = LoadStats(filepath.Join(newsDir, StatsFile))
	if tot := st.Totals(); tot["F1"] != (FeedStats{Shown: 10, Kept: 2, Dropped: 6, Hidden: 5, Rescued: 1}) {
		t.Errorf("上書き: %+v", tot)
	}

	// 何も無ければ何もしない(統計ファイルも作らない)
	empty := filepath.Join(t.TempDir(), "news")
	if msgs, err := Ingest(empty, []string{filepath.Join(empty, "inbox")}); err != nil || msgs != nil {
		t.Errorf("空: %v %v", msgs, err)
	}
	if _, err := os.Stat(filepath.Join(empty, StatsFile)); err == nil {
		t.Error("空でも統計ファイルを作った")
	}
}

// 選別 JSON の置き場と hub が別ドライブだと os.Rename が失敗する
// (Windows で実測: "The system cannot move the file to a different disk drive")。
// そのときも取り込みを完了させ、統計まで書く。
func TestIngest_CrossDevice(t *testing.T) {
	newsDir := filepath.Join(t.TempDir(), "news")
	inbox := t.TempDir()
	name := SelectionPrefix + "2026-08-15_daily_20260815100000.json"
	src := filepath.Join(inbox, name)
	if err := os.WriteFile(src, []byte(selectionJSON("2026-08-15", "daily",
		`{"id": "a", "title": "残す記事", "link": "https://x/keep", "feed": "F1"}`,
		`"F1": {"shown": 3, "kept": 1}`)), 0o644); err != nil {
		t.Fatal(err)
	}
	orig := osRename
	osRename = func(string, string) error {
		return errors.New("The system cannot move the file to a different disk drive.")
	}
	t.Cleanup(func() { osRename = orig })

	msgs, err := Ingest(newsDir, []string{inbox})
	if err != nil {
		t.Fatalf("Ingest: %v", err)
	}
	if len(msgs) != 1 || !strings.Contains(msgs[0], "取り込み: "+name) {
		t.Errorf("msgs: %v", msgs)
	}
	if _, err := os.Stat(src); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("元のファイルが消えていない: %v", err)
	}
	if b, err := os.ReadFile(filepath.Join(newsDir, IngestedDir, name)); err != nil || !strings.Contains(string(b), "残す記事") {
		t.Errorf("取り込み済みに写っていない: err=%v", err)
	}
	st, err := LoadStats(filepath.Join(newsDir, StatsFile))
	if err != nil || st.Digests["2026-08-15_daily"]["F1"].Kept != 1 {
		t.Errorf("統計: err=%v %+v", err, st.Digests)
	}
}

// 置き場の名前に glob の記号が入っていても拾う(ディレクトリ名をパターンとして解釈しない)。無い置き場は黙って飛ばす。
func TestIngest_DirNameWithGlobMeta(t *testing.T) {
	base := t.TempDir()
	inbox := filepath.Join(base, "down[loads]")
	if err := os.MkdirAll(inbox, 0o755); err != nil {
		t.Fatal(err)
	}
	name := SelectionPrefix + "2026-08-15_daily_20260815100000.json"
	if err := os.WriteFile(filepath.Join(inbox, name), []byte(selectionJSON("2026-08-15", "daily",
		`{"id": "a", "title": "残す記事", "link": "https://x/keep", "feed": "F1"}`,
		`"F1": {"shown": 3, "kept": 1}`)), 0o644); err != nil {
		t.Fatal(err)
	}
	newsDir := filepath.Join(base, "news")
	msgs, err := Ingest(newsDir, []string{filepath.Join(base, "no-such-dir"), inbox})
	if err != nil {
		t.Fatalf("Ingest: %v", err)
	}
	if len(msgs) != 1 || !strings.Contains(msgs[0], "取り込み: "+name) {
		t.Errorf("msgs: %v", msgs)
	}
	if _, err := os.Stat(filepath.Join(newsDir, IngestedDir, name)); err != nil {
		t.Errorf("取り込み済みに無い: %v", err)
	}
}
