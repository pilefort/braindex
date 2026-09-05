package main

import (
	"bytes"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
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
	mustContain(t, "learn", so, "# 学習の提案 2026-09-01（直近 14 日）", "材料: ノート 1・セッション ", "## 触れているがノートに無い", "## 訂正の文脈に繰り返し出る", "## 残した記事にあるがノートに無い")
	// keep の見出し「ゴルーチンの本」は索引に無いので「残した記事にあるがノートに無い」に載る
	if !strings.Contains(so, "- ゴルーチン — keep 1 件") {
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
