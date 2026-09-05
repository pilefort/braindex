package template

import (
	"bytes"
	"strings"
	"testing"
)

func gitignoreTemplate(t *testing.T) []byte {
	t.Helper()
	b, err := templates.ReadFile("templates/hub/.gitignore")
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// 無いところには雛形をそのまま。既に全部あれば触らない。自分の行があれば末尾に足す(末尾改行の有無も吸収)。
func TestMergeGitignore(t *testing.T) {
	tmpl := gitignoreTemplate(t)
	if got, changed := MergeGitignore(nil, tmpl); !changed || !bytes.Equal(got, tmpl) {
		t.Errorf("空から: changed=%v\n%s", changed, got)
	}
	if got, changed := MergeGitignore(tmpl, tmpl); changed || !bytes.Equal(got, tmpl) {
		t.Errorf("同じもの: changed=%v\n%s", changed, got)
	}
	mine := []byte("*.log\n.DS_Store")
	got, changed := MergeGitignore(mine, tmpl)
	if !changed {
		t.Fatal("自分の行だけのとき changed=false")
	}
	s := string(got)
	if !strings.HasPrefix(s, "*.log\n.DS_Store\n\n") {
		t.Errorf("自分の行が先頭に残っていない、または空行で区切られていない:\n%s", s)
	}
	for _, ln := range strings.Split(strings.TrimSpace(string(tmpl)), "\n") {
		if !strings.Contains(s, ln+"\n") {
			t.Errorf("雛形の行 %q が無い:\n%s", ln, s)
		}
	}
	// 2 回目は変わらない
	if got2, changed := MergeGitignore(got, tmpl); changed || !bytes.Equal(got, got2) {
		t.Errorf("2 回目: changed=%v", changed)
	}
	// 一部だけあるときは、無い行だけ足す(ある行を二重にしない)
	partial := []byte("news/digest_*\n")
	got, _ = MergeGitignore(partial, tmpl)
	if strings.Count(string(got), "news/digest_*") != 1 {
		t.Errorf("ある行が二重になった:\n%s", got)
	}
	if !strings.Contains(string(got), "news/.ingested/") {
		t.Errorf("無い行が足されていない:\n%s", got)
	}
	// CRLF の既存ファイルでも、ある行を二重にしない
	crlf := bytes.ReplaceAll(tmpl, []byte("\n"), []byte("\r\n"))
	if _, changed := MergeGitignore(crlf, tmpl); changed {
		t.Error("CRLF の既存ファイルに同じ行を足した")
	}
}

// InstallFeatures で news を足すとき、利用者の .gitignore には news の行を末尾に足し、Merged に出す。
// 既存だった .gitignore は台帳に記録しない(素性が分からないため)。
func TestInstallFeatures_MergesGitignore(t *testing.T) {
	dst := t.TempDir()
	writeAt(t, dst, GitignorePath, []byte("*.tmp\n"))
	res, err := InstallFeatures(dst, []Feature{FeatureNews})
	if err != nil {
		t.Fatal(err)
	}
	if !has(res.Merged, GitignorePath) || has(res.Created, GitignorePath) || has(res.Skipped, GitignorePath) {
		t.Errorf("merged=%v created=%v skipped=%v", res.Merged, res.Created, res.Skipped)
	}
	b := readAt(t, dst, GitignorePath)
	if !strings.HasPrefix(string(b), "*.tmp\n") || !strings.Contains(string(b), "news/.ingested/\n") {
		t.Errorf(".gitignore の中身:\n%s", b)
	}
	led := loadLedger(t, dst)
	if _, ok := led.Files[GitignorePath]; ok {
		t.Error("既存だった .gitignore を台帳に記録している")
	}
	// 2 回目は Skipped
	res, err = InstallFeatures(dst, []Feature{FeatureNews})
	if err != nil {
		t.Fatal(err)
	}
	if !has(res.Skipped, GitignorePath) || len(res.Merged) != 0 {
		t.Errorf("2 回目: merged=%v skipped=%v", res.Merged, res.Skipped)
	}
}

// 既存が CRLF なら足す行も CRLF(改行の混在を作らない)。雛形の注釈行が既にあれば二重にしない。
func TestMergeGitignore_CRLFAndComment(t *testing.T) {
	tmpl := gitignoreTemplate(t)
	got, changed := MergeGitignore([]byte("*.tmp\r\nnews/digest_*\r\n"), tmpl)
	if !changed {
		t.Fatal("無い行があるのに changed=false")
	}
	if bytes.Contains(bytes.ReplaceAll(got, []byte("\r\n"), nil), []byte("\n")) {
		t.Errorf("CRLF の既存に LF の行を足した:\n%q", got)
	}
	if !bytes.Contains(got, []byte("news/.ingested/\r\n")) {
		t.Errorf("無い行が CRLF で足されていない:\n%q", got)
	}
	// 注釈行と一部のパターン行が既にある → 注釈は足さず、無いパターン行だけ足す
	lines := strings.Split(strings.TrimRight(string(tmpl), "\n"), "\n")
	comment := lines[0]
	if !strings.HasPrefix(comment, "#") {
		t.Fatalf("雛形の 1 行目が注釈でない: %q", comment)
	}
	got, _ = MergeGitignore([]byte(comment+"\nnews/digest_*\n"), tmpl)
	if n := strings.Count(string(got), comment); n != 1 {
		t.Errorf("注釈行が %d 回ある:\n%s", n, got)
	}
}
