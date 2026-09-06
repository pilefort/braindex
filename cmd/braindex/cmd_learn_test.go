package main

import (
	"bytes"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"github.com/pilefort/braindex/internal/scan/scantest"
)

func runLearnIn(t *testing.T, hub string, args ...string) (code int, so, se string) {
	t.Helper()
	var sob, seb bytes.Buffer
	code = dispatch(append([]string{"learn", "-config", filepath.Join(hub, "braindex.json"), "-date", "2026-09-01"}, args...), &sob, &seb)
	return code, sob.String(), seb.String()
}

// learn は news profile と同じ材料(索引・セッション・keep)を読み、3 つの節を Markdown で出す。
func TestLearn_基本(t *testing.T) {
	hub := profileHub(t)
	// testdata には JSON でない行が 1 つあり、sessions の警告で終了コード 2 になる(news profile と同じ)
	code, so, se := runLearnIn(t, hub, "-sessions", retroTestdata)
	if code != 2 || !strings.Contains(se, "JSON でない 1 行を飛ばした") {
		t.Fatalf("exit=%d\n%s%s", code, so, se)
	}
	mustContain(t, "learn", so, "# 学習の提案 2026-09-01（直近 14 日）", "材料: ノート 1・セッション ", "本文照合: ", "## 触れているが索引に無い", "## 訂正の文脈に繰り返し出る", "## 残した記事にあるが索引に無い")
	// keep の見出し「ゴルーチンの本」は索引に無いので「残した記事にあるが索引に無い」に載る。本文にも無く、走査は全部読めたので「未発見」
	if !strings.Contains(so, "- ゴルーチン — keep 1 件／本文でも未発見（走査した範囲は全部読めた）") {
		t.Errorf("keep の語が載っていない:\n%s", so)
	}
	// 決定性
	_, so2, _ := runLearnIn(t, hub, "-sessions", retroTestdata)
	if so != so2 {
		t.Error("2 回の出力が違う")
	}
	// -json
	_, js, _ := runLearnIn(t, hub, "-sessions", retroTestdata, "-json")
	var v struct {
		Today   string         `json:"today"`
		Sources map[string]int `json:"sources"`
	}
	if err := json.Unmarshal([]byte(js), &v); err != nil || v.Today != "2026-09-01" || v.Sources["index"] != 1 {
		t.Errorf("json: err=%v %+v\n%s", err, v, js)
	}
}

func TestLearn_フラグの誤りは1(t *testing.T) {
	hub := profileHub(t)
	if code, _, se := runLearnIn(t, hub, "extra"); code != 1 || !strings.Contains(se, "受け付けない") {
		t.Errorf("位置引数: exit=%d\n%s", code, se)
	}
	if code, _, se := runLearnIn(t, hub, "-top", "-1"); code != 1 || !strings.Contains(se, "-top は 0 以上") {
		t.Errorf("-top -1: exit=%d\n%s", code, se)
	}
	var so, se bytes.Buffer
	if code := dispatch([]string{"learn", "-config", filepath.Join(hub, "nope.json")}, &so, &se); code != 1 || !strings.Contains(se.String(), "設定ファイルが無い") {
		t.Errorf("設定なし: exit=%d\n%s", code, se.String())
	}
}

// -date より後の発話は数えない(窓の上限)。testdata の発話は 2026-08 下旬なので、遠い過去の -date では訂正 0
func TestLearn_窓の上限(t *testing.T) {
	hub := profileHub(t)
	var so, se bytes.Buffer
	dispatch([]string{"learn", "-config", filepath.Join(hub, "braindex.json"), "-date", "2026-01-15", "-sessions", retroTestdata, "-json"}, &so, &se)
	var v struct {
		Sources map[string]int `json:"sources"`
	}
	if err := json.Unmarshal(so.Bytes(), &v); err != nil || v.Sources["corrections"] != 0 {
		t.Errorf("窓の外の訂正を数えた: err=%v %+v", err, v.Sources)
	}
}

// learnHub は profileHub と同じ hub に、要旨(先頭行)には無く本文の後半にだけ「ゴルーチン」を書いたノートを足して索引を作り直す。
// keep の「ゴルーチン」は索引(タイトル・要旨)に無いが本文にはある——「索引に無い」を「ノートに無い」と読んではいけない例。
func learnHub(t *testing.T) (parent, hub string) {
	t.Helper()
	hub = profileHub(t)
	parent = filepath.Dir(hub)
	writeFile(t, filepath.Join(parent, "repo-a", "docs", "notes", "concurrency.md"), "# 並行処理\n\n結論: チャネルで渡す\n記録日: 2026-08-25\n\n本文の後半でゴルーチンの寿命に触れる。\n")
	var so, se bytes.Buffer
	if code := dispatch([]string{"-config", filepath.Join(hub, "braindex.json"), "-date", "2026-09-01"}, &so, &se); code != 0 {
		t.Fatalf("index exit=%d\n%s", code, se.String())
	}
	return parent, hub
}

// 本文にだけある語は「本文で発見」と出典の位置(パス:行)を出し、本文の行そのものは載せない。
func TestLearn_本文照合で発見(t *testing.T) {
	_, hub := learnHub(t)
	code, so, se := runLearnIn(t, hub, "-sessions", retroTestdata)
	if code != 2 || !strings.Contains(se, "JSON でない 1 行を飛ばした") {
		t.Fatalf("exit=%d\n%s%s", code, so, se)
	}
	mustContain(t, "learn", so, "材料: ノート 2・セッション ", "ファイルを読んだ・確認できなかった範囲なし",
		"- ゴルーチン — keep 1 件／本文で発見（1 行・1 ファイル）: repo-a/docs/notes/concurrency.md:6\n")
	if strings.Contains(so, "寿命に触れる") {
		t.Errorf("本文の行が出力に載っている:\n%s", so)
	}
	if strings.Contains(se, "本文照合を飛ばした") {
		t.Errorf("照合できたのに飛ばした警告が出た:\n%s", se)
	}
	// -json は項目に evidence、全体に verification を持つ
	_, js, _ := runLearnIn(t, hub, "-sessions", retroTestdata, "-json")
	var v struct {
		ReadNotWritten []struct {
			Word     string `json:"word"`
			Evidence struct {
				Status    string `json:"status"`
				Files     int    `json:"files"`
				Lines     int    `json:"lines"`
				Locations []struct {
					Path string `json:"path"`
					Line int    `json:"line"`
				} `json:"locations"`
			} `json:"evidence"`
		} `json:"read_not_written"`
		Verification struct {
			Done     bool `json:"done"`
			Words    int  `json:"words"`
			Files    int  `json:"files"`
			Complete bool `json:"complete"`
		} `json:"verification"`
	}
	if err := json.Unmarshal([]byte(js), &v); err != nil {
		t.Fatalf("json: %v\n%s", err, js)
	}
	if len(v.ReadNotWritten) == 0 || v.ReadNotWritten[0].Word != "ゴルーチン" || v.ReadNotWritten[0].Evidence.Status != "found" ||
		len(v.ReadNotWritten[0].Evidence.Locations) != 1 || v.ReadNotWritten[0].Evidence.Locations[0].Path != "repo-a/docs/notes/concurrency.md" || v.ReadNotWritten[0].Evidence.Locations[0].Line != 6 {
		t.Errorf("evidence: %s", js)
	}
	if !v.Verification.Done || v.Verification.Words == 0 || v.Verification.Files == 0 || !v.Verification.Complete {
		t.Errorf("verification: %+v", v.Verification)
	}
	if strings.Contains(js, "寿命に触れる") {
		t.Errorf("本文の行が JSON に載っている:\n%s", js)
	}
}

// 読めないノートがあれば、未発見の語を「無い」と断定せず「確認不能」にし、その範囲を警告に出す(終了コード 2)。
func TestLearn_読めない範囲は確認不能(t *testing.T) {
	parent, hub := learnHub(t)
	scantest.MakeUnreadable(t, filepath.Join(parent, "repo-a", "docs", "notes", "concurrency.md"))
	code, so, se := runLearnIn(t, hub, "-sessions", retroTestdata)
	if code != 2 {
		t.Fatalf("exit=%d want 2\n%s%s", code, so, se)
	}
	mustContain(t, "learn", so, "確認できなかった範囲 1 件（この中にあるかは分からない）", "- repo-a/docs/notes/concurrency.md — ",
		"- ゴルーチン — keep 1 件／確認不能（読めなかった範囲がある）")
	if !strings.Contains(se, "braindex learn: 警告: repo-a/docs/notes/concurrency.md: ") {
		t.Errorf("stderr に読めなかった範囲の警告が無い:\n%s", se)
	}
}

// root が無く走査できない hub でも落ちない。照合を飛ばした旨を警告し(終了コード 2)、候補は索引だけの判定で出す。
func TestLearn_rootが無ければ照合を飛ばす(t *testing.T) {
	hub := t.TempDir()
	writeFile(t, filepath.Join(hub, "braindex.json"), `{}`)
	writeFile(t, filepath.Join(hub, "news", "keep", "2026-08.md"), "# 2026-08\n\n- [ゴルーチンの本](https://example.com/g)\n")
	code, so, se := runLearnIn(t, hub, "-sessions", retroTestdata, "-all-projects")
	if code != 2 {
		t.Fatalf("exit=%d want 2\n%s%s", code, so, se)
	}
	mustContain(t, "learn", so, "本文照合: なし（", "- ゴルーチン — keep 1 件\n")
	if !strings.Contains(se, "braindex learn: 警告: 本文照合を飛ばした: root が未指定") {
		t.Errorf("stderr に照合を飛ばした理由が無い:\n%s", se)
	}
}
