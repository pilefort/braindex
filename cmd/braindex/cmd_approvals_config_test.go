package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/pilefort/braindex/internal/approvals"
)

// braindex.json の approvals 節で置き場を決められる。
// 設定に書いた相対パスは「braindex.json のある場所」基準で解く(コマンドを打ったカレント基準ではない)。
func TestApprovals_設定から置き場を決める(t *testing.T) {
	dir := t.TempDir()
	hub := filepath.Join(dir, "hub")
	ap := filepath.Join(hub, "work", "判断待ち.md")
	tmp := filepath.Join(dir, "tmp")
	cfg := filepath.Join(hub, "braindex.json")
	writeFile(t, ap, sampleApprovals)
	writeFile(t, cfg, `{"root": "..", "approvals": {"file": "work/判断待ち.md", "decisions": "docs/決定.md"}}`)

	var so, se bytes.Buffer
	if code := dispatch([]string{"approvals", "status", "-config", cfg, "-dir", tmp}, &so, &se); code != 0 {
		t.Fatalf("code=%d\nstdout=%s\nstderr=%s", code, so.String(), se.String())
	}
	out := so.String()
	mustContain(t, "stdout", out, "items=1", "project="+hub)
	// 決定の追記先も設定で差し替わる(hub 相対)
	if want := filepath.Join(hub, "docs", "決定.md"); !strings.Contains(out, want) {
		t.Errorf("decisions が設定を見ていない: %q に %q が無い", out, want)
	}
}

// -file を明示したらフラグが勝つ(設定は無視される)。
func TestApprovals_フラグが設定に勝つ(t *testing.T) {
	dir := t.TempDir()
	hub := filepath.Join(dir, "hub")
	fromFlag := filepath.Join(hub, "work", "APPROVALS.md")
	tmp := filepath.Join(dir, "tmp")
	cfg := filepath.Join(hub, "braindex.json")
	writeFile(t, fromFlag, sampleApprovals)
	// 設定は存在しないファイルを指す。フラグが勝つならこれは読まれない
	writeFile(t, cfg, `{"root": "..", "approvals": {"file": "work/無い.md"}}`)

	var so, se bytes.Buffer
	if code := dispatch([]string{"approvals", "status", "-config", cfg, "-file", fromFlag, "-dir", tmp}, &so, &se); code != 0 {
		t.Fatalf("code=%d\nstdout=%s\nstderr=%s", code, so.String(), se.String())
	}
	mustContain(t, "stdout", so.String(), "items=1")
}

// -config で指定したファイルが無ければエラー(打ち間違いを黙って無視しない)。
// 既定の置き場に無いのは正常(フラグと既定だけで動く)。
func TestApprovals_configの指定ミスはエラー(t *testing.T) {
	dir := t.TempDir()
	var so, se bytes.Buffer
	code := dispatch([]string{"approvals", "status", "-config", filepath.Join(dir, "無い.json"), "-dir", dir}, &so, &se)
	if code != 1 {
		t.Errorf("指定した設定ファイルが無いのに code=%d\n%s", code, se.String())
	}
	if !strings.Contains(se.String(), "設定ファイルが無い") {
		t.Errorf("理由が分からない: %s", se.String())
	}
}

// approvals.timeout_sec が負なら設定の誤りとして止める。
func TestApprovals_負のtimeoutは設定エラー(t *testing.T) {
	dir := t.TempDir()
	hub := filepath.Join(dir, "hub")
	ap := filepath.Join(hub, "work", "APPROVALS.md")
	cfg := filepath.Join(hub, "braindex.json")
	writeFile(t, ap, sampleApprovals)
	writeFile(t, cfg, `{"root": "..", "approvals": {"timeout_sec": -5}}`)

	var so, se bytes.Buffer
	code := dispatch([]string{"approvals", "status", "-config", cfg, "-dir", filepath.Join(dir, "tmp")}, &so, &se)
	if code != 1 {
		t.Errorf("負の timeout_sec を通した: code=%d\n%s", code, so.String())
	}
	if !strings.Contains(se.String(), "timeout_sec") {
		t.Errorf("どのキーの誤りか分からない: %s", se.String())
	}
}

// -file を渡さない apply が、設定（無ければ既定）から置き場を決められる。
//
// serve と status は loadApprovals を通るが、apply だけ approvals.Resolve を直に呼んでいたため、
// -file の既定を "" にした時点で「回答があるのに『回答はない』と言って終了コード 0 で終わる」
// 取りこぼしになっていた（filepath.Abs("") ＝ カレントが APPROVALS.md 扱いになり、id が別物になる）。
func TestApprovalsApply_fileを渡さなくても解決する(t *testing.T) {
	dir := t.TempDir()
	hub := filepath.Join(dir, "hub")
	ap := filepath.Join(hub, "work", "APPROVALS.md")
	dec := filepath.Join(hub, "docs", "decisions.md")
	tmp := filepath.Join(dir, "tmp")
	cfg := filepath.Join(hub, "braindex.json")
	writeFile(t, ap, sampleApprovals)
	writeFile(t, dec, "# 設計判断\n")
	writeFile(t, cfg, `{"root": ".."}`) // approvals 節は省略＝既定の work/APPROVALS.md を使う
	p, err := approvals.Resolve(ap, tmp)
	if err != nil {
		t.Fatal(err)
	}
	writeFile(t, p.Reply, sampleReply)

	var so, se bytes.Buffer
	code := dispatch([]string{"approvals", "apply", "-config", cfg, "-dir", tmp}, &so, &se)
	if code != 0 {
		t.Fatalf("code=%d\nstdout=%s\nstderr=%s", code, so.String(), se.String())
	}
	if strings.Contains(so.String(), "回答はない") {
		t.Fatalf("回答があるのに取りこぼした: %s", so.String())
	}
	b, err := os.ReadFile(dec)
	if err != nil {
		t.Fatalf("decisions.md を読めない: %v", err)
	}
	if !strings.Contains(string(b), "設定ファイルの形式") {
		t.Errorf("決定が追記されていない: %s", b)
	}
}

// 設定の decisions も、file と同じく braindex.json のある場所を基準に解く。
// 基準が 2 つ（設定ファイルの場所と、APPROVALS.md から推定した hub）あると、
// work/ 直下でない置き方をした瞬間に決定の追記先がずれる。
func TestApprovals_設定のdecisionsは設定ファイル基準(t *testing.T) {
	dir := t.TempDir()
	hub := filepath.Join(dir, "hub")
	ap := filepath.Join(hub, "work", "pending", "APPROVALS.md") // work/ 直下ではない
	tmp := filepath.Join(dir, "tmp")
	cfg := filepath.Join(hub, "braindex.json")
	writeFile(t, ap, sampleApprovals)
	writeFile(t, cfg, `{"root": "..", "approvals": {"file": "work/pending/APPROVALS.md", "decisions": "docs/decisions.md"}}`)

	var so, se bytes.Buffer
	if code := dispatch([]string{"approvals", "status", "-config", cfg, "-dir", tmp}, &so, &se); code != 0 {
		t.Fatalf("code=%d\n%s", code, se.String())
	}
	want := filepath.Join(hub, "docs", "decisions.md")
	if !strings.Contains(so.String(), want) {
		t.Errorf("decisions が hub 基準で解けていない: %q に %q が無い", so.String(), want)
	}
}
