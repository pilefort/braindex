package news

import (
	"errors"
	"fmt"
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

// keep は git 管理の蓄積側なので、http(s) でないリンクの記事は記録しない(決定 2026-09-03 → manual/design.md「決めたこと」)。
// リンクの無い記事と同じ扱いにする(題名だけ書くと、次回の重複判定に引っかからず毎回増える)。
func TestIngest_keepに載せるのはhttpのみ(t *testing.T) {
	newsDir := filepath.Join(t.TempDir(), "news")
	inbox := filepath.Join(newsDir, "inbox")
	os.MkdirAll(inbox, 0o755)
	keeps := `{"id": "a", "title": "危ない", "link": "javascript:alert(1)", "feed": "F1"},` +
		`{"id": "b", "title": "普通", "link": "https://x/ok", "feed": "F1"}`
	os.WriteFile(filepath.Join(inbox, "braindex-news-selection_2026-08-15_daily_1.json"),
		[]byte(selectionJSON("2026-08-15", "daily", keeps, `"F1": {"shown": 2, "kept": 2}`)), 0o644)

	if _, err := Ingest(newsDir, []string{inbox}, nil); err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(filepath.Join(newsDir, "keep", "2026-08.md"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(got), "javascript:") || strings.Contains(string(got), "危ない") {
		t.Errorf("http(s) でないリンクの記事を記録している:\n%s", got)
	}
	if !strings.Contains(string(got), "- [普通](https://x/ok) — F1") {
		t.Errorf("http(s) の記事が記録されていない:\n%s", got)
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

	msgs, err := Ingest(newsDir, []string{inbox, filepath.Join(newsDir, "no-such-dir")}, nil)
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
	if _, err := Ingest(newsDir, []string{inbox}, nil); err != nil {
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
	if msgs, err := Ingest(empty, []string{filepath.Join(empty, "inbox")}, nil); err != nil || msgs != nil {
		t.Errorf("空: %v %v", msgs, err)
	}
	if _, err := os.Stat(filepath.Join(empty, StatsFile)); err == nil {
		t.Error("空でも統計ファイルを作った")
	}
}

// 置き場をまたぐと、dirs の順(<news.dir>/inbox → -inbox)が名前順より先に効いてはいけない。
// dirA(dirs の 1 番目)に基底名が後になるファイル(_9)、dirB(2 番目)に先になるファイル(_1)を置く。
// 名前順(全体で並べ替え)なら _1 → _9 の順で処理され、後勝ちで _9 の統計が残る。
// dirs の順のまま連結すると _9 → _1 の順になり、_1 が「後勝ち」で残ってしまう(バグ)。
func TestIngest_置き場をまたいでも名前順(t *testing.T) {
	newsDir := filepath.Join(t.TempDir(), "news")
	dirA := filepath.Join(newsDir, "inbox")
	dirB := t.TempDir()
	os.MkdirAll(dirA, 0o755)

	keeps := `{"id": "a", "title": "記事", "link": "https://x/a", "feed": "F1"}`
	os.WriteFile(filepath.Join(dirA, "braindex-news-selection_2026-08-15_daily_9.json"),
		[]byte(selectionJSON("2026-08-15", "daily", keeps, `"F1": {"shown": 9, "kept": 9}`)), 0o644)
	os.WriteFile(filepath.Join(dirB, "braindex-news-selection_2026-08-15_daily_1.json"),
		[]byte(selectionJSON("2026-08-15", "daily", keeps, `"F1": {"shown": 1, "kept": 1}`)), 0o644)

	if _, err := Ingest(newsDir, []string{dirA, dirB}, nil); err != nil {
		t.Fatal(err)
	}
	st, err := LoadStats(filepath.Join(newsDir, StatsFile))
	if err != nil {
		t.Fatal(err)
	}
	// 名前順なら _1 が先・_9 が後に処理され、後勝ちで _9(kept=9) が残る
	if got := st.Digests["2026-08-15_daily"]["F1"].Kept; got != 9 {
		t.Errorf("kept=%d want 9(名前順でなく dirs の順で処理されている)", got)
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

	msgs, err := Ingest(newsDir, []string{inbox}, nil)
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
	msgs, err := Ingest(newsDir, []string{filepath.Join(base, "no-such-dir"), inbox}, nil)
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

// 選別 JSON は信用境界の外(ブラウザのダウンロード先)から拾うので、date は
// keep ファイルのパスの一部になる前に形を検査する(設計レビュー 2026-09-06 H4)。
func TestIngest_dateの形が違う選別JSONは取り込まない(t *testing.T) {
	base := t.TempDir()
	newsDir := filepath.Join(base, "hub", "news")
	inbox := filepath.Join(newsDir, "inbox")
	if err := os.MkdirAll(inbox, 0o755); err != nil {
		t.Fatal(err)
	}
	keeps := `{"id": "a", "title": "残す記事", "link": "https://x/keep", "feed": "F1"}`
	for i, date := range []string{"../../escape", "2026-8-15", "", "2026-08-15 ", "20260815"} {
		name := fmt.Sprintf("%s%d.json", SelectionPrefix, i)
		if err := os.WriteFile(filepath.Join(inbox, name),
			[]byte(selectionJSON(date, "daily", keeps, `"F1": {"shown": 1, "kept": 1}`)), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	msgs, err := Ingest(newsDir, []string{inbox}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(msgs) != 5 {
		t.Fatalf("msgs の数: %d %q", len(msgs), msgs)
	}
	for _, m := range msgs {
		if !strings.Contains(m, "が YYYY-MM-DD でない") || !strings.Contains(m, "取り込まない") {
			t.Errorf("警告の文面: %q", m)
		}
	}
	// keep ファイルは 1 つもできない(置き場の外にも中にも)
	if err := filepath.WalkDir(base, func(p string, d fs.DirEntry, err error) error {
		if err == nil && !d.IsDir() && strings.HasSuffix(p, ".md") {
			t.Errorf("keep ファイルができた: %s", p)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	// 統計にも入らない・取り込み済みへも移さない(人が中を見て消せるように元の場所に残す)
	if _, err := os.Stat(filepath.Join(newsDir, StatsFile)); err == nil {
		t.Error("統計ファイルを作った")
	}
	des, _ := os.ReadDir(inbox)
	if len(des) != 5 {
		t.Errorf("元の場所に残っていない: %d 件", len(des))
	}
	if _, err := os.Stat(filepath.Join(newsDir, IngestedDir)); err == nil {
		t.Error("取り込み済みへ移した")
	}
}

// feed_stats は feeds.json にある名前で数が 0 以上のものだけ数える。
// 外れた項目は落として 1 行にまとめて伝える(記事ごとに警告を出さない)。
func TestIngest_feedStatsの検査(t *testing.T) {
	newsDir := filepath.Join(t.TempDir(), "news")
	inbox := filepath.Join(newsDir, "inbox")
	if err := os.MkdirAll(inbox, 0o755); err != nil {
		t.Fatal(err)
	}
	stats := `"F1": {"shown": 10, "kept": 1}, "知らない取材先": {"shown": 5, "kept": 5}, "F2": {"shown": -3, "kept": 1}`
	if err := os.WriteFile(filepath.Join(inbox, SelectionPrefix+"1.json"),
		[]byte(selectionJSON("2026-08-15", "daily", `{"id": "a", "title": "T", "link": "https://x/1", "feed": "F1"}`, stats)), 0o644); err != nil {
		t.Fatal(err)
	}
	known := map[string]bool{"F1": true, "F2": true}
	msgs, err := Ingest(newsDir, []string{inbox}, known)
	if err != nil {
		t.Fatal(err)
	}
	if len(msgs) != 2 || !strings.Contains(msgs[0], "feed_stats") || !strings.Contains(msgs[0], "2 項目") {
		t.Errorf("msgs: %q", msgs)
	}
	st, _ := LoadStats(filepath.Join(newsDir, StatsFile))
	snap := st.Digests["2026-08-15_daily"]
	if len(snap) != 1 || snap["F1"].Shown != 10 {
		t.Errorf("統計に外れた項目が入った: %+v", snap)
	}
	// 検査に落ちても取り込み自体は続ける(keep は入る)
	keepMD, err := os.ReadFile(filepath.Join(newsDir, "keep", "2026-08.md"))
	if err != nil || !strings.Contains(string(keepMD), "https://x/1") {
		t.Errorf("keep: err=%v\n%s", err, keepMD)
	}
}

// known が nil(feeds.json を読めなかった)ときは名前を照合しない。負の数の検査だけ残る。
func TestIngest_feedStatsは名前一覧が無ければ照合しない(t *testing.T) {
	newsDir := filepath.Join(t.TempDir(), "news")
	inbox := filepath.Join(newsDir, "inbox")
	if err := os.MkdirAll(inbox, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(inbox, SelectionPrefix+"1.json"),
		[]byte(selectionJSON("2026-08-15", "daily", ``, `"知らない取材先": {"shown": 5}, "F2": {"kept": -1}`)), 0o644); err != nil {
		t.Fatal(err)
	}
	msgs, err := Ingest(newsDir, []string{inbox}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(msgs) != 2 || !strings.Contains(msgs[0], "1 項目") {
		t.Errorf("msgs: %q", msgs)
	}
	st, _ := LoadStats(filepath.Join(newsDir, StatsFile))
	if snap := st.Digests["2026-08-15_daily"]; len(snap) != 1 || snap["知らない取材先"].Shown != 5 {
		t.Errorf("統計: %+v", snap)
	}
}

// 統計を書けずに止まっても(保存だけを失敗させて再現)、再実行で keep を重複させずに統計と取り込み済みを揃える。
// 取り込み済みへ移すのは統計を書いた後なので、その前に止まれば選別 JSON は置き場に残り、次回また拾える
// (設計レビュー補足 2026-09-06「処理単位の復旧」)。移した後に統計を書けずに止まると、その選別の数は二度と拾えない。
func TestIngest_統計を書けずに止まっても再実行で揃う(t *testing.T) {
	newsDir := filepath.Join(t.TempDir(), "news")
	inbox := filepath.Join(newsDir, "inbox")
	if err := os.MkdirAll(inbox, 0o755); err != nil {
		t.Fatal(err)
	}
	name := SelectionPrefix + "2026-08-15_daily_1.json"
	keeps := `{"id": "a", "title": "残す記事", "link": "https://x/keep", "feed": "F1"}`
	if err := os.WriteFile(filepath.Join(inbox, name),
		[]byte(selectionJSON("2026-08-15", "daily", keeps, `"F1": {"shown": 3, "kept": 1}`)), 0o644); err != nil {
		t.Fatal(err)
	}
	statsPath := filepath.Join(newsDir, StatsFile)
	orig := writeAtomic
	writeAtomic = func(path string, data []byte, perm fs.FileMode) error {
		if filepath.Base(path) == StatsFile { // 統計の保存だけ失敗させる(ディスクが一杯・電源断の代わり)
			return errors.New("統計を書けない(注入)")
		}
		return orig(path, data, perm)
	}
	t.Cleanup(func() { writeAtomic = orig })
	if _, err := Ingest(newsDir, []string{inbox}, nil); err == nil {
		t.Fatal("統計を書けないのに成功した")
	}
	// 止まった時点: keep は書けていてよいが、選別 JSON は置き場に残っている(取り込み済みへ移していない)
	if _, err := os.Stat(filepath.Join(inbox, name)); err != nil {
		t.Errorf("統計を書けなかったのに選別 JSON を置き場から動かした: %v", err)
	}
	writeAtomic = orig

	// 再実行: 揃う
	msgs, err := Ingest(newsDir, []string{inbox}, nil)
	if err != nil {
		t.Fatalf("再実行: %v", err)
	}
	if len(msgs) != 1 || !strings.Contains(msgs[0], "取り込み: "+name) {
		t.Errorf("再実行の msgs: %q", msgs)
	}
	keepMD, err := os.ReadFile(filepath.Join(newsDir, KeepDir, "2026-08.md"))
	if err != nil {
		t.Fatal(err)
	}
	if n := strings.Count(string(keepMD), "https://x/keep"); n != 1 {
		t.Errorf("keep に %d 回(1 回だけのはず):\n%s", n, keepMD)
	}
	st, err := LoadStats(statsPath)
	if err != nil || st.Digests["2026-08-15_daily"]["F1"].Kept != 1 {
		t.Errorf("再実行後の統計: err=%v %+v", err, st.Digests)
	}
	if _, err := os.Stat(filepath.Join(newsDir, IngestedDir, name)); err != nil {
		t.Errorf("再実行後も取り込み済みに無い: %v", err)
	}
	if _, err := os.Stat(filepath.Join(inbox, name)); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("再実行後も置き場に残っている: %v", err)
	}
}

// 不要ばかり付く取材先は点の上限を下げて主要表示から下ろす(決定 2026-09-06 → manual/news.md「決めたこと」)。
// 不要率は「見た数」でなく「選んだ数」で割る——折りたたみに入って目に入らなかった記事を
// 不要と数えないため。
func TestDemotedFeeds(t *testing.T) {
	got := DemotedFeeds(map[string]FeedStats{
		"下げる":          {Shown: 40, Kept: 1, Dropped: 9},              // 判定 10・不要率 0.9
		"境界(0.8 ちょうど)": {Shown: 40, Kept: 2, Dropped: 8},              // 0.8 は「超え」でないので下げない
		"材料不足":         {Shown: 40, Kept: 0, Dropped: 9},              // 判定 9
		"救済を数える":       {Shown: 40, Kept: 0, Dropped: 9, Rescued: 1},  // 判定 10・不要率 0.9
		"見ただけ":         {Shown: 100, Hidden: 100},                     // 判定 0
		"読んでいる":        {Shown: 40, Kept: 20, Dropped: 5, Rescued: 2}, // 不要率 0.19
	})
	want := map[string]bool{"下げる": true, "救済を数える": true}
	if len(got) != len(want) {
		t.Fatalf("下げた取材先: %v", got)
	}
	for name := range want {
		if !got[name] {
			t.Errorf("%s が下がっていない: %v", name, got)
		}
	}
	if DemotedFeeds(nil) != nil {
		t.Error("材料が無ければ nil")
	}
}
