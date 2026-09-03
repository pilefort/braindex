package main

import (
	"bytes"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/pilefort/braindex/internal/retro"
)

// profileHub は hub に 索引(窓内のノート 1 件)・keep 履歴・補助ファイル を置く。セッションは retro と同じ testdata を使う。
func profileHub(t *testing.T) (hub string) {
	t.Helper()
	parent, hub := hubWithRepo(t)
	writeFile(t, filepath.Join(parent, "repo-a", "docs", "notes", "feed.md"), "# フィードのパース\n\n結論: RSS を読む\n記録日: 2026-08-25\n")
	var so, se bytes.Buffer
	if code := dispatch([]string{"-config", filepath.Join(hub, "braindex.json"), "-date", "2026-09-01"}, &so, &se); code != 0 {
		t.Fatalf("index exit=%d\n%s", code, se.String())
	}
	writeFile(t, filepath.Join(hub, "news", "keep", "2026-08.md"), "# 2026-08\n\n- [ゴルーチンの本](https://example.com/g)\n")
	writeFile(t, filepath.Join(hub, "news", "interests.md"), "# 補助\nRust\n")
	return hub
}

func newsProfile(t *testing.T, hub string, args ...string) (code int, so, se string) {
	t.Helper()
	var sob, seb bytes.Buffer
	code = dispatch(append([]string{"news", "profile", "-config", filepath.Join(hub, "braindex.json"), "-date", "2026-09-01"}, args...), &sob, &seb)
	return code, sob.String(), seb.String()
}

// 窓の起点は retro と同じ「ローカルの 0 時」。UTC の 0 時にすると、同じ「N 日前から」が
// retro と別の日を指し、両方を定期実行に載せたときに食い違う(決定 2026-09-03)。
func TestProfileSince_窓の起点はローカル0時(t *testing.T) {
	loc := time.FixedZone("JST", 9*60*60)
	old := retroLoc
	retroLoc = loc
	t.Cleanup(func() { retroLoc = old })

	got, err := profileSince("2026-09-01", 14)
	if err != nil {
		t.Fatal(err)
	}
	if want := time.Date(2026, 8, 18, 0, 0, 0, 0, loc); !got.Equal(want) {
		t.Errorf("since = %v want %v", got, want)
	}
	if got.UTC().Hour() == 0 && got.UTC().Day() == 18 {
		t.Errorf("UTC の 0 時になっている: %v", got.UTC())
	}
	// retro の窓と同じ起点になる
	w := retro.Recent(time.Date(2026, 9, 1, 12, 0, 0, 0, loc), 14, loc)
	if !w.Since.Equal(got) {
		t.Errorf("retro の窓 %v と違う: %v", w.Since, got)
	}
}

func TestNewsProfile_Sources(t *testing.T) {
	hub := profileHub(t)
	// testdata には JSON でない行が 1 つあり、sessions の警告で終了コード 2 になる(retro と同じ)
	code, so, se := newsProfile(t, hub, "-sessions", retroTestdata)
	if code != 2 || !strings.Contains(se, "JSON でない 1 行を飛ばした") {
		t.Fatalf("exit=%d\n%s%s", code, so, se)
	}
	mustContain(t, "profile", so, "# 関心プロファイル 2026-09-01（直近 14 日）", "| 語 | 重み | index | sessions | keep | extra |")
	// 出典ごとに 1 語ずつ: 索引のタイトル「パース」、セッション本文「索引」、keep「ゴルーチン」、補助「rust」
	for _, w := range []string{"| パース |", "| 索引 |", "| ゴルーチン |", "| rust |"} {
		if !strings.Contains(so, w) {
			t.Errorf("%s が無い:\n%s", w, so)
		}
	}
	if !strings.Contains(so, "材料: ノート 1・セッション ") || !strings.Contains(so, "keep 1・補助 1") {
		t.Errorf("材料:\n%s", so)
	}

	// 決定性
	_, so2, _ := newsProfile(t, hub, "-sessions", retroTestdata)
	if so != so2 {
		t.Error("2 回の出力が違う")
	}

	// -json
	_, js, _ := newsProfile(t, hub, "-sessions", retroTestdata, "-json")
	var v struct {
		Today string `json:"today"`
		Terms []struct {
			Word   string             `json:"word"`
			Counts map[string]float64 `json:"counts"`
		} `json:"terms"`
	}
	if err := json.Unmarshal([]byte(js), &v); err != nil || v.Today != "2026-09-01" || len(v.Terms) == 0 {
		t.Errorf("json: err=%v %+v", err, v)
	}
	for _, tm := range v.Terms {
		if tm.Word == "rust" && tm.Counts["extra"] != 1 {
			t.Errorf("rust の出典: %v", tm.Counts)
		}
	}

	// -top
	_, so3, _ := newsProfile(t, hub, "-sessions", retroTestdata, "-top", "1")
	if !strings.Contains(so3, "（上位 1 語。残り ") {
		t.Errorf("top:\n%s", so3)
	}
}

// 索引もセッションの置き場も無ければ警告して残りで作る(終了コード 2)。
func TestNewsProfile_MissingSources(t *testing.T) {
	_, hub := hubWithRepo(t)
	writeFile(t, filepath.Join(hub, "news", "interests.md"), "Rust\n")
	code, so, se := newsProfile(t, hub, "-sessions", filepath.Join(hub, "no-such-dir"))
	if code != 2 {
		t.Fatalf("exit=%d\n%s", code, se)
	}
	mustContain(t, "stderr", se, "索引", "が無いので飛ばした", "セッションログの置き場", "警告 2 件")
	mustContain(t, "stdout", so, "| rust | 1.000 | | | | 1 |", "材料: ノート 0・セッション 0・keep 0・補助 1")
}

// セッションの置き場は -sessions → news.sessions_dir → retro.sessions_dir の順に決まる。
// 置き場が無いと警告にその名前が出るので、どれを使ったかが分かる。
func TestNewsProfile_SessionsDirFallback(t *testing.T) {
	_, hub := hubWithRepo(t)
	cfg := filepath.Join(hub, "braindex.json")

	writeFile(t, cfg, `{"root": "..", "news": {"sessions_dir": "from-news"}, "retro": {"sessions_dir": "from-retro"}}`)
	if _, _, se := newsProfile(t, hub, "-sessions", "from-flag"); !strings.Contains(se, "置き場 from-flag が無い") {
		t.Errorf("-sessions が最優先でない:\n%s", se)
	}
	if _, _, se := newsProfile(t, hub); !strings.Contains(se, "置き場 from-news が無い") {
		t.Errorf("news.sessions_dir が retro より優先されない:\n%s", se)
	}

	writeFile(t, cfg, `{"root": "..", "retro": {"sessions_dir": "from-retro"}}`)
	if _, _, se := newsProfile(t, hub); !strings.Contains(se, "置き場 from-retro が無い") {
		t.Errorf("retro.sessions_dir に落ちない:\n%s", se)
	}
}

func TestNewsProfile_Errors(t *testing.T) {
	_, hub := hubWithRepo(t)
	if code, _, se := newsProfile(t, hub, "-date", "bad"); code != 1 || !strings.Contains(se, "-date は YYYY-MM-DD") {
		t.Errorf("date: exit=%d %s", code, se)
	}
	if code, _, se := newsProfile(t, hub, "extra"); code != 1 || !strings.Contains(se, `引数 ["extra"] は受け付けない`) {
		t.Errorf("引数: exit=%d %s", code, se)
	}
	var so, se bytes.Buffer
	if code := dispatch([]string{"news", "profile", "-h"}, &so, &se); code != 0 || !strings.Contains(se.String(), "使い方: braindex news profile") {
		t.Errorf("-h: exit=%d %s", code, se.String())
	}
	se.Reset()
	if code := dispatch([]string{"news", "profile", "-config", filepath.Join(hub, "nope.json")}, &so, &se); code != 1 || !strings.Contains(se.String(), "設定ファイルが無い") {
		t.Errorf("config: exit=%d %s", code, se.String())
	}
}
