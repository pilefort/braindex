package main

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

const goodIssue = "# ISSUE: 例\n\n## 現在の作業\nx\n\n## 状態\n- [x] a\n- [ ] b  ← いまここ\n\n最終更新: 2026-09-01\n"

// ファイルを渡すとそれを検査する。指摘なしは 0、指摘ありは 2 で「パス:行: 内容」を stdout に出す。
func TestLint_File(t *testing.T) {
	p := filepath.Join(t.TempDir(), "ISSUE-a.md")
	writeFile(t, p, goodIssue)
	var so, se bytes.Buffer
	if code := dispatch([]string{"lint", "-no-git", "-date", "2026-09-02", p}, &so, &se); code != 0 {
		t.Fatalf("exit=%d want 0\nstdout=%s\nstderr=%s", code, so.String(), se.String())
	}
	if !strings.Contains(so.String(), "braindex lint: 1 ファイル・指摘 0 件") {
		t.Errorf("要約が無い: %s", so.String())
	}

	writeFile(t, p, strings.Replace(goodIssue, "  ← いまここ", "", 1))
	so.Reset()
	if code := dispatch([]string{"lint", "-no-git", "-date", "2026-09-02", p}, &so, &se); code != 2 {
		t.Fatalf("exit=%d want 2\n%s", code, so.String())
	}
	if !strings.Contains(so.String(), filepath.ToSlash(p)+": ") || !strings.Contains(so.String(), "いまここ") {
		t.Errorf("指摘の形が違う: %s", so.String())
	}
	if !strings.Contains(so.String(), "指摘 1 件") {
		t.Errorf("要約の件数が違う: %s", so.String())
	}
}

// 行番号つきの指摘は「パス:行: 内容」。
func TestLint_LineNumber(t *testing.T) {
	p := filepath.Join(t.TempDir(), "ISSUE-a.md")
	writeFile(t, p, strings.Replace(goodIssue, "2026-09-01", "2026-09-09", 1)) // 10 行目が未来
	var so, se bytes.Buffer
	if code := dispatch([]string{"lint", "-no-git", "-date", "2026-09-02", p}, &so, &se); code != 2 {
		t.Fatalf("exit=%d want 2\n%s", code, so.String())
	}
	if !strings.Contains(so.String(), filepath.ToSlash(p)+":10: ") {
		t.Errorf("行番号つきの形でない: %s", so.String())
	}
}

// パスを渡さなければ root 直下の各リポの work/ISSUE-*.md。. で始まるディレクトリと work の無いリポは飛ばす。表示は root 相対。
func TestLint_Root(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "repo-a", "work", "ISSUE-x.md"), goodIssue)
	writeFile(t, filepath.Join(root, "repo-b", "work", "ISSUE-y.md"), strings.Replace(goodIssue, "最終更新: 2026-09-01\n", "", 1))
	writeFile(t, filepath.Join(root, "repo-b", "work", "TODO.md"), "# TODO\n")
	writeFile(t, filepath.Join(root, ".hidden", "work", "ISSUE-z.md"), "# x\n")
	writeFile(t, filepath.Join(root, "repo-c", "README.md"), "# c\n")
	var so, se bytes.Buffer
	if code := dispatch([]string{"lint", "-no-git", "-date", "2026-09-02", "-root", root}, &so, &se); code != 2 {
		t.Fatalf("exit=%d want 2\nstdout=%s\nstderr=%s", code, so.String(), se.String())
	}
	s := so.String()
	if !strings.Contains(s, "repo-b/work/ISSUE-y.md: ") {
		t.Errorf("root 相対の表示でない: %s", s)
	}
	if strings.Contains(s, ".hidden") || strings.Contains(s, "TODO.md") {
		t.Errorf("対象外が混じる: %s", s)
	}
	if !strings.Contains(s, "braindex lint: 2 ファイル・指摘 1 件") {
		t.Errorf("要約が違う: %s", s)
	}
}

// ディレクトリを渡すと直下の ISSUE-*.md。
func TestLint_Dir(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "ISSUE-a.md"), goodIssue)
	writeFile(t, filepath.Join(dir, "ISSUE-b.md"), goodIssue)
	writeFile(t, filepath.Join(dir, "APPROVALS.md"), "# 承認待ち\n")
	var so, se bytes.Buffer
	if code := dispatch([]string{"lint", "-no-git", "-date", "2026-09-02", dir}, &so, &se); code != 0 {
		t.Fatalf("exit=%d want 0\n%s%s", code, so.String(), se.String())
	}
	if !strings.Contains(so.String(), "2 ファイル") {
		t.Errorf("2 ファイルを検査していない: %s", so.String())
	}
}

// -stale-days は -date を基準に経過日数を見る。
func TestLint_StaleDays(t *testing.T) {
	p := filepath.Join(t.TempDir(), "ISSUE-a.md")
	writeFile(t, p, goodIssue)
	var so, se bytes.Buffer
	if code := dispatch([]string{"lint", "-no-git", "-date", "2026-09-30", "-stale-days", "7", p}, &so, &se); code != 2 {
		t.Fatalf("exit=%d want 2\n%s", code, so.String())
	}
	if !strings.Contains(so.String(), "29 日") {
		t.Errorf("経過日数が出ていない: %s", so.String())
	}
}

// 誤り: root もパスも無い / -date の形式 / 存在しないパス / -stale-days が負。-h は 0。
func TestLint_Errors(t *testing.T) {
	var so, se bytes.Buffer
	if code := dispatch([]string{"lint", "-no-git", "-config", filepath.Join(t.TempDir(), "none.json")}, &so, &se); code != 1 {
		t.Errorf("root 無しの exit=%d want 1: %s", code, se.String())
	}
	if code := dispatch([]string{"lint", "-date", "2026/09/02", "x"}, &so, &se); code != 1 {
		t.Errorf("-date 不正の exit=%d want 1", code)
	}
	if code := dispatch([]string{"lint", "-no-git", filepath.Join(t.TempDir(), "nope.md")}, &so, &se); code != 1 {
		t.Errorf("存在しないパスの exit=%d want 1", code)
	}
	if code := dispatch([]string{"lint", "-stale-days", "-1", "x"}, &so, &se); code != 1 {
		t.Errorf("-stale-days 負の exit=%d want 1", code)
	}
	se.Reset()
	if code := dispatch([]string{"lint", "-h"}, &so, &se); code != 0 || !strings.Contains(se.String(), "使い方") {
		t.Errorf("-h の exit=%d: %s", code, se.String())
	}
}

// 同じ入力からは同じ出力(決定性)。
func TestLint_Deterministic(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "repo-b", "work", "ISSUE-y.md"), strings.Replace(goodIssue, "## 状態", "## 進捗", 1))
	writeFile(t, filepath.Join(root, "repo-a", "work", "ISSUE-x.md"), strings.Replace(goodIssue, "  ← いまここ", "", 1))
	run := func() string {
		var so, se bytes.Buffer
		dispatch([]string{"lint", "-no-git", "-date", "2026-09-02", "-root", root}, &so, &se)
		return so.String()
	}
	a, b := run(), run()
	if a != b {
		t.Errorf("出力が揺れる:\n%s\n---\n%s", a, b)
	}
	if strings.Index(a, "repo-a/") > strings.Index(a, "repo-b/") {
		t.Errorf("パス順でない:\n%s", a)
	}
}

// git 管理下のファイルは HEAD と比べ、消えたチェック項目と据え置きの最終更新を指摘する。-no-git で飛ばす。
func TestLint_Git(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git が無い")
	}
	dir := t.TempDir()
	git := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", append([]string{"-C", dir, "-c", "user.name=t", "-c", "user.email=t@example.invalid", "-c", "core.autocrlf=false"}, args...)...)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	git("init", "-q")
	p := filepath.Join(dir, "ISSUE-a.md")
	writeFile(t, p, goodIssue)
	git("add", "ISSUE-a.md")
	git("commit", "-q", "-m", "init")

	// 項目 a を落とし、最終更新はそのまま
	writeFile(t, p, strings.Replace(goodIssue, "- [x] a\n", "", 1))
	var so, se bytes.Buffer
	if code := dispatch([]string{"lint", "-date", "2026-09-02", p}, &so, &se); code != 2 {
		t.Fatalf("exit=%d want 2\nstdout=%s\nstderr=%s", code, so.String(), se.String())
	}
	s := so.String()
	if !strings.Contains(s, "消えた") || !strings.Contains(s, "HEAD と同じ") || !strings.Contains(s, "HEAD 比較 1 件") {
		t.Errorf("HEAD 比較の指摘が無い:\n%s", s)
	}
	so.Reset()
	if code := dispatch([]string{"lint", "-no-git", "-date", "2026-09-02", p}, &so, &se); code != 0 {
		t.Errorf("-no-git の exit=%d want 0:\n%s", code, so.String())
	}
	if strings.Contains(so.String(), "HEAD 比較") {
		t.Errorf("-no-git なのに HEAD 比較している: %s", so.String())
	}

	// 未追跡のファイルは比較しない
	q := filepath.Join(dir, "ISSUE-b.md")
	writeFile(t, q, goodIssue)
	so.Reset()
	if code := dispatch([]string{"lint", "-date", "2026-09-02", q}, &so, &se); code != 0 || strings.Contains(so.String(), "HEAD 比較") {
		t.Errorf("未追跡の exit=%d 出力=%s", code, so.String())
	}
	_ = os.Remove(q)
}

// ISSUE-*.md 以外の .md はノートの曖昧さ検査になる。指摘は「パス:行: [種別] 内容」。
func TestLint_NoteAuto(t *testing.T) {
	p := filepath.Join(t.TempDir(), "note.md")
	writeFile(t, p, "# 例\n\n記録日: 2026-09-01\n\n最近かなり増えた。\n")
	var so, se bytes.Buffer
	if code := dispatch([]string{"lint", p}, &so, &se); code != 2 {
		t.Fatalf("exit=%d want 2\nstdout=%s\nstderr=%s", code, so.String(), se.String())
	}
	s := so.String()
	if !strings.Contains(s, filepath.ToSlash(p)+":5: [曖昧な数量詞] 〔最近〕") || !strings.Contains(s, "〔かなり〕") {
		t.Errorf("ノート検査の形でない: %s", s)
	}
	if strings.Contains(s, "いまここ") {
		t.Errorf("ノートに ISSUE の検査が走った: %s", s)
	}
	if !strings.Contains(s, "指摘 2 件") {
		t.Errorf("要約の件数が違う: %s", s)
	}
}

// -kind issue でノートを ISSUE として検査でき、-kind note で ISSUE-*.md をノートとして検査できる。誤った値は 1。
func TestLint_KindOverride(t *testing.T) {
	dir := t.TempDir()
	note := filepath.Join(dir, "note.md")
	writeFile(t, note, "# 例\n\n記録日: 2026-09-01\n\n本文。\n")
	issue := filepath.Join(dir, "ISSUE-a.md")
	writeFile(t, issue, goodIssue)
	var so, se bytes.Buffer
	if code := dispatch([]string{"lint", "-no-git", "-date", "2026-09-02", "-kind", "issue", note}, &so, &se); code != 2 || !strings.Contains(so.String(), "いまここ") {
		t.Errorf("-kind issue: exit=%d stdout=%s", code, so.String())
	}
	so.Reset()
	if code := dispatch([]string{"lint", "-kind", "note", issue}, &so, &se); code != 0 || !strings.Contains(so.String(), "指摘 0 件") {
		t.Errorf("-kind note: exit=%d stdout=%s", code, so.String())
	}
	so.Reset()
	se.Reset()
	if code := dispatch([]string{"lint", "-kind", "memo", note}, &so, &se); code != 1 || !strings.Contains(se.String(), "-kind は issue か note") {
		t.Errorf("誤った -kind: exit=%d stderr=%s", code, se.String())
	}
}

// -kind note でディレクトリを渡すと直下の *.md。
func TestLint_NoteDir(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "a.md"), "2026-09-01 の記録。\n")
	writeFile(t, filepath.Join(dir, "b.md"), "日付なし。\n")
	writeFile(t, filepath.Join(dir, "c.txt"), "日付なし。\n")
	var so, se bytes.Buffer
	if code := dispatch([]string{"lint", "-kind", "note", dir}, &so, &se); code != 2 {
		t.Fatalf("exit=%d want 2\n%s%s", code, so.String(), se.String())
	}
	if !strings.Contains(so.String(), "b.md: [日付なし]") || !strings.Contains(so.String(), "2 ファイル") || strings.Contains(so.String(), "c.txt") {
		t.Errorf("対象が違う: %s", so.String())
	}
}

// 用語集: -glossary で渡すか、ノートのあるリポの docs/glossary.md を自動で探す。無ければ未定義用語は見ない。
func TestLint_Glossary(t *testing.T) {
	repo := t.TempDir()
	note := filepath.Join(repo, "docs", "notes", "n.md")
	writeFile(t, note, "2026-09-01 「新語」と「既知語」。\n")
	var so, se bytes.Buffer
	if code := dispatch([]string{"lint", note}, &so, &se); code != 0 {
		t.Errorf("用語集なし: exit=%d stdout=%s", code, so.String())
	}
	writeFile(t, filepath.Join(repo, "docs", "glossary.md"), "## 既知語\n定義。\n")
	so.Reset()
	if code := dispatch([]string{"lint", note}, &so, &se); code != 2 || !strings.Contains(so.String(), "[未定義用語(候補)] 用語「新語」") || strings.Contains(so.String(), "「既知語」") {
		t.Errorf("自動検出: exit=%d stdout=%s", code, so.String())
	}
	other := filepath.Join(t.TempDir(), "g.md")
	writeFile(t, other, "新語: 定義。\n")
	so.Reset()
	if code := dispatch([]string{"lint", "-glossary", other, note}, &so, &se); code != 2 || !strings.Contains(so.String(), "用語「既知語」") || strings.Contains(so.String(), "用語「新語」") {
		t.Errorf("-glossary: exit=%d stdout=%s", code, so.String())
	}
	se.Reset()
	if code := dispatch([]string{"lint", "-glossary", filepath.Join(repo, "nope.md"), note}, &so, &se); code != 1 || !strings.Contains(se.String(), "用語集を読めない") {
		t.Errorf("無い用語集: exit=%d stderr=%s", code, se.String())
	}
}

// -json は指摘の配列(path・line・msg・kind・severity)だけを stdout に出す。指摘なしは空配列。
func TestLint_JSON(t *testing.T) {
	p := filepath.Join(t.TempDir(), "note.md")
	writeFile(t, p, "最近の話。\n")
	var so, se bytes.Buffer
	if code := dispatch([]string{"lint", "-json", p}, &so, &se); code != 2 {
		t.Fatalf("exit=%d want 2\n%s%s", code, so.String(), se.String())
	}
	var got []map[string]any
	if err := json.Unmarshal(so.Bytes(), &got); err != nil {
		t.Fatalf("JSON でない: %v\n%s", err, so.String())
	}
	if len(got) != 2 || got[0]["kind"] != "no_date" || got[0]["severity"] != "warn" || got[1]["line"] != float64(1) || got[1]["kind"] != "vague_quantifier" {
		t.Errorf("内容が違う: %s", so.String())
	}
	writeFile(t, p, "2026-09-01 の記録。\n")
	so.Reset()
	if code := dispatch([]string{"lint", "-json", p}, &so, &se); code != 0 || strings.TrimSpace(so.String()) != "[]" {
		t.Errorf("指摘なし: exit=%d stdout=%q", code, so.String())
	}
}
