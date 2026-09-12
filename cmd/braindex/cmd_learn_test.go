package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/pilefort/braindex/internal/learn"
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

// learnAnswer は braindex learn answer <動詞> [フラグ] <語>... を回す(-config・-date・-sessions は learn と同じ)。
func learnAnswer(t *testing.T, hub string, verb string, rest ...string) (code int, so, se string) {
	t.Helper()
	var sob, seb bytes.Buffer
	args := append([]string{"learn", "answer", verb, "-config", filepath.Join(hub, "braindex.json"), "-date", "2026-09-01", "-sessions", retroTestdata}, rest...)
	code = dispatch(args, &sob, &seb)
	return code, sob.String(), seb.String()
}

// learnAt は日付を変えて braindex learn を回す。
func learnAt(t *testing.T, hub, date string, args ...string) (code int, so, se string) {
	t.Helper()
	var sob, seb bytes.Buffer
	code = dispatch(append([]string{"learn", "-config", filepath.Join(hub, "braindex.json"), "-date", date, "-sessions", retroTestdata}, args...), &sob, &seb)
	return code, sob.String(), seb.String()
}

func learnAnswers(t *testing.T, hub string, args ...string) (code int, so, se string) {
	t.Helper()
	var sob, seb bytes.Buffer
	code = dispatch(append([]string{"learn", "answers", "-config", filepath.Join(hub, "braindex.json"), "-date", "2026-09-01"}, args...), &sob, &seb)
	return code, sob.String(), seb.String()
}

// 候補に「既知」と回答すると work/learn/answers.json に節と語の組で残り、次回の提示から伏せる(伏せた数だけ出す)。
// 一覧に出て、解除すると次回からまた出る。
func TestLearnAnswer_回答すると次回から伏せる(t *testing.T) {
	_, hub := learnHub(t)
	answersPath := filepath.Join(hub, "work", "learn", "answers.json")
	_, so, _ := runLearnIn(t, hub, "-sessions", retroTestdata)
	mustContain(t, "回答前", so, "- ゴルーチン — keep 1 件／本文で発見")
	if strings.Contains(so, "回答済み") {
		t.Errorf("回答が無いのに回答済みの行がある:\n%s", so)
	}

	code, so, se := learnAnswer(t, hub, "known", "ゴルーチン")
	if code != 2 || !strings.Contains(se, "JSON でない 1 行を飛ばした") {
		t.Fatalf("answer exit=%d\n%s%s", code, so, se)
	}
	mustContain(t, "answer", so, "回答: 残した記事にあるが索引に無い: ゴルーチン — 既知（2026-09-01）\n", "保存先: "+answersPath+"\n")
	mustContain(t, "answers.json", readFile(t, answersPath), `"version": 1`, `"section": "read_not_written"`, `"word": "ゴルーチン"`, `"answer": "known"`, `"date": "2026-09-01"`)

	code, so, _ = runLearnIn(t, hub, "-sessions", retroTestdata)
	if code != 2 {
		t.Fatalf("回答後 exit=%d\n%s", code, so)
	}
	if strings.Contains(so, "- ゴルーチン") {
		t.Errorf("回答済みの候補が出ている:\n%s", so)
	}
	mustContain(t, "回答後", so, "回答済み: 伏せた 1 件（既知 1・不要 0・後で 0）\n", "## 残した記事にあるが索引に無い（0）")
	_, so2, _ := runLearnIn(t, hub, "-sessions", retroTestdata)
	if so != so2 {
		t.Error("回答後の 2 回の出力が違う")
	}
	_, js, _ := runLearnIn(t, hub, "-sessions", retroTestdata, "-json")
	var v struct {
		ReadNotWritten []struct{} `json:"read_not_written"`
		Feedback       struct {
			Hidden []struct {
				Section string `json:"section"`
				Item    struct {
					Word  string `json:"word"`
					Keeps int    `json:"keeps"`
				} `json:"item"`
				Answer string `json:"answer"`
				Date   string `json:"date"`
			} `json:"hidden"`
			Known int `json:"known"`
		} `json:"feedback"`
	}
	if err := json.Unmarshal([]byte(js), &v); err != nil {
		t.Fatalf("json: %v\n%s", err, js)
	}
	if len(v.ReadNotWritten) != 0 || v.Feedback.Known != 1 || len(v.Feedback.Hidden) != 1 || v.Feedback.Hidden[0].Section != "read_not_written" ||
		v.Feedback.Hidden[0].Item.Word != "ゴルーチン" || v.Feedback.Hidden[0].Item.Keeps != 1 || v.Feedback.Hidden[0].Answer != "known" || v.Feedback.Hidden[0].Date != "2026-09-01" {
		t.Errorf("json の feedback: %s", js)
	}

	code, so, se = learnAnswers(t, hub)
	if code != 0 {
		t.Fatalf("answers exit=%d\n%s", code, se)
	}
	mustContain(t, "answers", so, "# 学習候補への回答（1 件）", "- 残した記事にあるが索引に無い: ゴルーチン — 既知（2026-09-01）\n")
	_, js, _ = learnAnswers(t, hub, "-json")
	if js != readFile(t, answersPath) {
		t.Errorf("answers -json がファイルと違う:\n%s", js)
	}

	// 解除(フラグを動詞の前に置く形でも通る)。材料は読まないので警告なしの 0
	var sob, seb bytes.Buffer
	if code := dispatch([]string{"learn", "answer", "-config", filepath.Join(hub, "braindex.json"), "clear", "ゴルーチン"}, &sob, &seb); code != 0 {
		t.Fatalf("clear exit=%d\n%s%s", code, sob.String(), seb.String())
	}
	mustContain(t, "clear", sob.String(), "解除: 残した記事にあるが索引に無い: ゴルーチン\n")
	_, so, _ = runLearnIn(t, hub, "-sessions", retroTestdata)
	mustContain(t, "解除後", so, "- ゴルーチン — keep 1 件／本文で発見")
	if strings.Contains(so, "回答済み") {
		t.Errorf("解除したのに回答済みの行がある:\n%s", so)
	}
	if _, so, _ = learnAnswers(t, hub); !strings.Contains(so, "（0 件）") {
		t.Errorf("解除後の一覧:\n%s", so)
	}
}

// 「後で」は再提示日の前日まで伏せ、当日から回答日つきで出す。既定の再提示日は窓の日数後。
func TestLearnAnswer_後では再提示日まで伏せる(t *testing.T) {
	_, hub := learnHub(t)
	code, so, se := learnAnswer(t, hub, "later", "-until", "2026-09-03", "ゴルーチン")
	if code != 2 {
		t.Fatalf("exit=%d\n%s%s", code, so, se)
	}
	mustContain(t, "later", so, "回答: 残した記事にあるが索引に無い: ゴルーチン — 後で（2026-09-01 に回答・2026-09-03 から再提示）\n")

	_, so, _ = learnAt(t, hub, "2026-09-02")
	if strings.Contains(so, "- ゴルーチン") {
		t.Errorf("前日に出ている:\n%s", so)
	}
	mustContain(t, "前日", so, "回答済み: 伏せた 1 件（既知 0・不要 0・後で 1）\n")

	_, so, _ = learnAt(t, hub, "2026-09-03")
	mustContain(t, "当日", so, "回答済み: 伏せた 0 件（既知 0・不要 0・後で 0）・「後で」の期限が来て再提示 1 件\n",
		"- ゴルーチン — keep 1 件／本文で発見（1 行・1 ファイル）: repo-a/docs/notes/concurrency.md:6／後で（2026-09-01 に回答）の期限が来たので再提示\n")
	if _, so, _ = learnAnswers(t, hub, "-date", "2026-09-03"); !strings.Contains(so, "2026-09-03 から再提示・期限切れ）") {
		t.Errorf("一覧が期限切れと言わない:\n%s", so)
	}

	// 既定の再提示日は今日 + 窓の日数(14)
	if _, so, _ = learnAnswer(t, hub, "later", "ゴルーチン"); !strings.Contains(so, "2026-09-15 から再提示）") {
		t.Errorf("既定の再提示日:\n%s", so)
	}
	if code, _, se := learnAnswer(t, hub, "later", "-until", "2026-09-01", "ゴルーチン"); code != 1 || !strings.Contains(se, "今日(2026-09-01)より後") {
		t.Errorf("今日以前の -until: exit=%d\n%s", code, se)
	}
	if code, _, se := learnAnswer(t, hub, "known", "-until", "2026-09-03", "ゴルーチン"); code != 1 || !strings.Contains(se, "-until は later だけ") {
		t.Errorf("known に -until: exit=%d\n%s", code, se)
	}
}

// 候補に無い語には回答できない(綴りの誤りを黙って記録しない)。失敗したら何も書かない。
func TestLearnAnswer_候補に無い語は書かない(t *testing.T) {
	_, hub := learnHub(t)
	answersPath := filepath.Join(hub, "work", "learn", "answers.json")
	if code, _, se := learnAnswer(t, hub, "unwanted", "nosuchword"); code != 1 || !strings.Contains(se, "候補に無い: nosuchword") {
		t.Errorf("exit=%d\n%s", code, se)
	}
	// 節を限れば、その節に無い語も候補に無い
	if code, _, se := learnAnswer(t, hub, "known", "-section", "unsettled", "ゴルーチン"); code != 1 || !strings.Contains(se, "候補に無い: 触れているが索引に無い: ゴルーチン") {
		t.Errorf("-section: exit=%d\n%s", code, se)
	}
	// 2 語のうち 1 語が無ければ全部書かない
	if code, _, se := learnAnswer(t, hub, "known", "ゴルーチン", "nosuchword"); code != 1 || !strings.Contains(se, "候補に無い: nosuchword") {
		t.Errorf("2 語: exit=%d\n%s", code, se)
	}
	if _, err := os.Stat(answersPath); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("失敗したのに書いた: %v", err)
	}
	if code, _, se := learnAnswer(t, hub, "clear", "ゴルーチン"); code != 1 || !strings.Contains(se, "回答が無い: ゴルーチン") {
		t.Errorf("clear: exit=%d\n%s", code, se)
	}
}

// 壊れた回答ファイルは、提示では反映せずに警告(候補は出す)、回答では上に書かない(終了コード 1)。
func TestLearnAnswer_壊れた回答ファイル(t *testing.T) {
	_, hub := learnHub(t)
	answersPath := filepath.Join(hub, "work", "learn", "answers.json")
	writeFile(t, answersPath, "{broken")
	code, so, se := runLearnIn(t, hub, "-sessions", retroTestdata)
	if code != 2 || !strings.Contains(se, "braindex learn: 警告: 回答を反映せずに出す: ") || !strings.Contains(se, "answers.json") {
		t.Errorf("提示: exit=%d\n%s", code, se)
	}
	mustContain(t, "提示", so, "- ゴルーチン — keep 1 件")
	if code, _, se := learnAnswer(t, hub, "known", "ゴルーチン"); code != 1 || !strings.Contains(se, "直すか、ファイルごと消して") {
		t.Errorf("回答: exit=%d\n%s", code, se)
	}
	if got := readFile(t, answersPath); got != "{broken" {
		t.Errorf("壊れたファイルを書き換えた: %q", got)
	}
	if code, _, se := learnAnswers(t, hub); code != 1 || !strings.Contains(se, "answers.json") {
		t.Errorf("一覧: exit=%d\n%s", code, se)
	}
}

// 引数の誤りは何も書かずに 1。語は候補と同じ規則で 1 語にする(大文字は畳む・重複は 1 回)。
func TestLearnAnswer_引数の誤りは1(t *testing.T) {
	_, hub := learnHub(t)
	cfg := filepath.Join(hub, "braindex.json")
	cases := []struct {
		args []string
		want string
	}{
		{[]string{"learn", "answer", "-config", cfg}, "回答(known / unwanted / later / clear)と語を指定する"},
		{[]string{"learn", "answer", "maybe", "-config", cfg, "x"}, `回答 "maybe" は無い`},
		{[]string{"learn", "answer", "known", "-config", cfg}, "語を 1 つ以上"},
		{[]string{"learn", "answer", "known", "-config", cfg, "a b c"}, "候補の語として扱えない"},
		{[]string{"learn", "answer", "known", "-config", cfg, "-section", "nope", "ゴルーチン"}, `節 "nope" は無い`},
		{[]string{"learn", "answer", "known", "-config", filepath.Join(hub, "nope.json"), "ゴルーチン"}, "設定ファイルが無い"},
		{[]string{"learn", "answers", "-config", cfg, "extra"}, "受け付けない"},
		{[]string{"learn", "extra"}, "受け付けない"},
	}
	for _, c := range cases {
		var so, se bytes.Buffer
		if code := dispatch(c.args, &so, &se); code != 1 || !strings.Contains(se.String(), c.want) {
			t.Errorf("%v: exit=%d want %q\n%s", c.args, code, c.want, se.String())
		}
	}
	if _, err := os.Stat(filepath.Join(hub, "work", "learn", "answers.json")); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("誤りで書いた: %v", err)
	}
	terms, err := learnTerms([]string{"Kubernetes", "kubernetes", "ゴルーチン"})
	if err != nil || strings.Join(terms, ",") != "kubernetes,ゴルーチン" {
		t.Errorf("learnTerms: %v %v", terms, err)
	}
	// -h は 0
	for _, args := range [][]string{{"learn", "answer", "-h"}, {"learn", "answers", "-h"}, {"learn", "answer", "known", "-h"}} {
		var so, se bytes.Buffer
		if code := dispatch(args, &so, &se); code != 0 || !strings.Contains(se.String(), "使い方: braindex learn answer") {
			t.Errorf("%v: exit=%d\n%s", args, code, se.String())
		}
	}
}

// 回答の保存は全件置換なので、読んでから書くまでの間に別のプロセスが別の語へ回答していると、
// そのまま書けば後勝ちで相手の回答が消える。書き込み直前に読み直し、消さずに足す
// (Codex レビュー 2026-09-12)。mutate の中が「別プロセスが保存した」瞬間にあたる。
func TestUpdateAnswers_同時に保存しても相手の回答が消えない(t *testing.T) {
	path := filepath.Join(t.TempDir(), "work", "learn", "answers.json")
	mine := learn.Feedback{Section: learn.SectionStumbles, Word: "mine", Answer: learn.Known, Date: "2026-09-01"}
	theirs := learn.Feedback{Section: learn.SectionStumbles, Word: "theirs", Answer: learn.Unwanted, Date: "2026-09-01"}

	err := updateAnswers(path, func(fb *learn.Feedbacks) error {
		// ここで別のプロセスが自分の回答を保存した(こちらは保存前の中身を持っている)
		var other learn.Feedbacks
		other.Set(theirs)
		if err := other.Save(path); err != nil {
			return err
		}
		fb.Set(mine)
		return nil
	})
	if err != nil {
		t.Fatalf("updateAnswers: %v", err)
	}

	got, err := learn.LoadFeedbacks(path)
	if err != nil {
		t.Fatalf("LoadFeedbacks: %v", err)
	}
	for _, want := range []learn.Feedback{mine, theirs} {
		f, ok := got.Find(want.Section, want.Word)
		if !ok {
			t.Errorf("%s の回答が消えた: %+v", want.Word, got.Answers)
			continue
		}
		if f != want {
			t.Errorf("%s の回答が変わった: got=%+v want=%+v", want.Word, f, want)
		}
	}
}
