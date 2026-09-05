package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/pilefort/braindex/internal/mdhtml"
)

// stubNow は答えを書いた日時を固定する(スレッドのエントリの見出しと id が決まる)。
func stubNow(t *testing.T, ts string) {
	t.Helper()
	tm, err := time.Parse(time.RFC3339, ts)
	if err != nil {
		t.Fatal(err)
	}
	orig := answerNow
	answerNow = func() time.Time { return tm }
	t.Cleanup(func() { answerNow = orig })
}

func readString(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// -append は一時置き場に <話題>.md のスレッドを作り、<話題>.html を開く。
func TestAnswer_Append_新規(t *testing.T) {
	dir, opened := stubAnswer(t)
	stubNow(t, "2026-09-06T03:20:00+09:00")
	src := filepath.Join(t.TempDir(), "ans.md")
	writeFile(t, src, "# 索引の設計\n\n名前順です。\n")
	var so, se bytes.Buffer
	if code := dispatch([]string{"answer", "-append", "索引の設計", "-q", "走査の順番は？", src}, &so, &se); code != 0 {
		t.Fatalf("exit=%d want 0\n%s", code, se.String())
	}
	md := readString(t, filepath.Join(dir, "索引の設計.md"))
	if !strings.Contains(md, `<!--braindex:entry at="2026-09-06T03:20:00+09:00" q="走査の順番は？"-->`) {
		t.Errorf("エントリのマーカーが無い:\n%s", md)
	}
	// 本文の先頭の h1 はスレッドのタイトルになり、エントリの中には残らない。
	if strings.Count(md, "# 索引の設計") != 1 {
		t.Errorf("h1 が二重になっている:\n%s", md)
	}
	h := readString(t, filepath.Join(dir, "索引の設計.html"))
	if !strings.Contains(h, `id="e-20260906T032000"`) || !strings.Contains(h, "走査の順番は？") {
		t.Errorf("HTML にエントリが出ていない:\n%s", h)
	}
	if len(*opened) != 1 || (*opened)[0] != filepath.Join(dir, "索引の設計.html") {
		t.Errorf("開いた先が違う: %v", *opened)
	}
	if !strings.Contains(so.String(), "足した ") {
		t.Errorf("スレッドに足した報告が無い: %s", so.String())
	}
}

// 2 回目の回答は先頭に積まれ、前のエントリは下に残る。
func TestAnswer_Append_2回目は上に積む(t *testing.T) {
	dir, _ := stubAnswer(t)
	src := filepath.Join(t.TempDir(), "ans.md")
	var so, se bytes.Buffer

	stubNow(t, "2026-09-05T20:00:00+09:00")
	writeFile(t, src, "# 索引の設計\n\n古い回答。\n")
	if code := dispatch([]string{"answer", "-no-open", "-append", "索引の設計", "-q", "1 問目", src}, &so, &se); code != 0 {
		t.Fatalf("1 回目 exit=%d\n%s", code, se.String())
	}
	stubNow(t, "2026-09-06T03:20:00+09:00")
	writeFile(t, src, "新しい回答。\n\n## 内訳\n\n- 箇条書き\n")
	if code := dispatch([]string{"answer", "-no-open", "-append", "索引の設計", "-q", "2 問目", src}, &so, &se); code != 0 {
		t.Fatalf("2 回目 exit=%d\n%s", code, se.String())
	}

	md := readString(t, filepath.Join(dir, "索引の設計.md"))
	i2, i1 := strings.Index(md, "2 問目"), strings.Index(md, "1 問目")
	if i2 < 0 || i1 < 0 || i2 > i1 {
		t.Errorf("新しいエントリが上に無い:\n%s", md)
	}
	if !strings.Contains(md, "古い回答。") {
		t.Errorf("前のエントリが消えている:\n%s", md)
	}
	// 回答本文の `## 内訳` ではエントリが割れない(境界はマーカーだけ)。
	title, es := mdhtml.ParseThread(md)
	if len(es) != 2 {
		t.Fatalf("エントリ数 %d want 2:\n%s", len(es), md)
	}
	if !strings.Contains(es[0].Body, "## 内訳") {
		t.Errorf("本文の見出しが失われた: %q", es[0].Body)
	}
	// タイトルは 1 回目の h1 のまま(2 回目の .md に h1 が無くても話題名に戻らない)。
	if title != "索引の設計" {
		t.Errorf("タイトル: got %q", title)
	}
}

// マーカーの無い 1 枚ものの .md にも足せる(既存の本文が日時不明の 1 エントリになる)。
func TestAnswer_Append_1枚ものに足す(t *testing.T) {
	dir, _ := stubAnswer(t)
	stubNow(t, "2026-09-06T03:20:00+09:00")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(dir, "話題.md"), "# 題名\n\n前からある本文。\n")
	src := filepath.Join(t.TempDir(), "ans.md")
	writeFile(t, src, "新しい回答。\n")
	var so, se bytes.Buffer
	if code := dispatch([]string{"answer", "-no-open", "-append", "話題", src}, &so, &se); code != 0 {
		t.Fatalf("exit=%d\n%s", code, se.String())
	}
	title, es := mdhtml.ParseThread(readString(t, filepath.Join(dir, "話題.md")))
	if title != "題名" {
		t.Errorf("タイトル: got %q", title)
	}
	if len(es) != 2 || es[0].Body != "新しい回答。" || es[1].Body != "前からある本文。" {
		t.Errorf("エントリ: %+v", es)
	}
	// -q なしのエントリは日時だけの見出しになる。
	h := readString(t, filepath.Join(dir, "話題.html"))
	if !strings.Contains(h, "日時不明") || !strings.Contains(h, "2026-09-06 03:20") {
		t.Errorf("見出しが出ていない:\n%s", h)
	}
}

// 話題名は一時置き場の中のファイル名なのでパス区切りを弾く。-q だけの指定も弾く(終了コード 1)。
func TestAnswer_Append_フラグの誤り(t *testing.T) {
	stubAnswer(t)
	src := filepath.Join(t.TempDir(), "a.md")
	writeFile(t, src, "x\n")
	cases := []struct {
		name string
		args []string
		want string
	}{
		{"パス区切り", []string{"answer", "-no-open", "-append", "../外", src}, "パス区切り"},
		{"空の話題名", []string{"answer", "-no-open", "-append", "  ", src}, "話題名が空"},
		{"-q だけ", []string{"answer", "-no-open", "-q", "質問", src}, "-q は -append"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var so, se bytes.Buffer
			if code := dispatch(tc.args, &so, &se); code != 1 {
				t.Fatalf("exit=%d want 1\n%s", code, se.String())
			}
			if !strings.Contains(se.String(), tc.want) {
				t.Errorf("stderr に %q が無い: %s", tc.want, se.String())
			}
		})
	}
}

// -out を付ければスレッドの HTML も好きな場所に書ける(スレッドの .md は一時置き場のまま)。
func TestAnswer_Append_Out(t *testing.T) {
	dir, _ := stubAnswer(t)
	stubNow(t, "2026-09-06T03:20:00+09:00")
	src := filepath.Join(t.TempDir(), "ans.md")
	writeFile(t, src, "本文\n")
	out := filepath.Join(t.TempDir(), "sub", "t.html")
	var so, se bytes.Buffer
	if code := dispatch([]string{"answer", "-no-open", "-append", "話題", "-out", out, src}, &so, &se); code != 0 {
		t.Fatalf("exit=%d\n%s", code, se.String())
	}
	if _, err := os.Stat(out); err != nil {
		t.Errorf("-out に書かれていない: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "話題.md")); err != nil {
		t.Errorf("スレッドの .md が一時置き場に無い: %v", err)
	}
}
