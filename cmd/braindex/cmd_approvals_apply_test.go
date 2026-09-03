package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/pilefort/braindex/internal/approvals"
)

const sampleReply = `{"nonce":"n","received_at":"2026-03-04T10:00:00+09:00","items":[{"n":1,"title":"設定ファイルの形式","choice":"B","comment":"コメントが要る"}]}`

func TestApprovalsApply_RoundTrip(t *testing.T) {
	dir := t.TempDir()
	hub := filepath.Join(dir, "hub")
	ap := filepath.Join(hub, "work", "APPROVALS.md")
	dec := filepath.Join(hub, "docs", "decisions.md")
	tmp := filepath.Join(dir, "tmp")
	writeFile(t, ap, sampleApprovals)
	writeFile(t, dec, "# 設計判断\n")
	p, _ := approvals.Resolve(ap, tmp)

	// 回答が無ければ何もしない
	var so, se bytes.Buffer
	if code := dispatch([]string{"approvals", "apply", "-file", ap, "-dir", tmp}, &so, &se); code != 0 || !strings.Contains(so.String(), "回答はない") {
		t.Fatalf("回答なし: code=%d\n%s%s", code, so.String(), se.String())
	}
	if got := readFile(t, ap); got != sampleApprovals {
		t.Error("回答なしで APPROVALS.md が変わった")
	}

	writeFile(t, p.Reply, sampleReply)
	so.Reset()
	if code := dispatch([]string{"approvals", "apply", "-file", ap, "-dir", tmp, "-date", "2026-03-04"}, &so, &se); code != 0 {
		t.Fatalf("code=%d\n%s", code, se.String())
	}
	mustContain(t, "stdout", so.String(), "反映: 決定 1 件 → "+dec, "保留 0 件", "[1] 設定ファイルの形式 → B. TOML（コメントが要る）", "next: decisions.md")
	mustContain(t, "decisions", readFile(t, dec), "# 設計判断\n\n## 設定ファイルの形式 → B. TOML\n\n記録日: 2026-03-04\n理由: コメント可。補足: コメントが要る（私の案 A は不採用）。却下: A. JSON（依存なし）\n根拠: 会話 2026-03-04（ユーザー判断・承認フォームの回答 2026-03-04T10:00:00+09:00）。なぜ今決めたか: 次の PR。")
	if got := readFile(t, ap); !strings.HasPrefix(got, "# 承認待ち\n\n（なし。2026-03-04 に") {
		t.Errorf("APPROVALS.md =\n%s", got)
	}
	if _, err := os.Stat(p.Reply); err == nil {
		t.Error("reply.json が残っている")
	}
	if _, err := os.Stat(p.Applied); err != nil {
		t.Error("applied.json が無い")
	}
	// 2 回目は回答が無いので何もしない(冪等)
	so.Reset()
	if code := dispatch([]string{"approvals", "apply", "-file", ap, "-dir", tmp}, &so, &se); code != 0 || !strings.Contains(so.String(), "回答はない") {
		t.Errorf("2 回目: code=%d %s", code, so.String())
	}
}

// decisions.md に書けなかったら、APPROVALS.md からも消さず回答も残す。
// 先に APPROVALS.md から消すと、決定がどちらのファイルにも残らない状態で終わる。
func TestApprovalsApply_DecisionsWriteFails(t *testing.T) {
	if os.Getuid() == 0 {
		t.Skip("root では読み取り専用のファイルにも書けてしまう") // Windows では Getuid が -1
	}
	dir := t.TempDir()
	hub := filepath.Join(dir, "hub")
	ap := filepath.Join(hub, "work", "APPROVALS.md")
	dec := filepath.Join(hub, "docs", "decisions.md")
	tmp := filepath.Join(dir, "tmp")
	writeFile(t, ap, sampleApprovals)
	writeFile(t, dec, "# 設計判断\n")
	p, err := approvals.Resolve(ap, tmp)
	if err != nil {
		t.Fatal(err)
	}
	writeFile(t, p.Reply, sampleReply)
	if err := os.Chmod(dec, 0o444); err != nil { // 読めるが書けない
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chmod(dec, 0o644) }) // 読み取り専用のままだと TempDir の掃除が失敗する

	var so, se bytes.Buffer
	if code := dispatch([]string{"approvals", "apply", "-file", ap, "-dir", tmp, "-date", "2026-03-04"}, &so, &se); code != 1 {
		t.Fatalf("code=%d\n%s%s", code, so.String(), se.String())
	}
	if got := readFile(t, ap); got != sampleApprovals {
		t.Errorf("決定を書けなかったのに APPROVALS.md を書き換えた:\n%s", got)
	}
	if _, err := os.Stat(p.Reply); err != nil {
		t.Error("決定を書けなかったのに回答を .applied.json へ動かした")
	}
}

func TestApprovalsApply_ExplicitReplyAndNoDecisions(t *testing.T) {
	dir := t.TempDir()
	ap := filepath.Join(dir, "hub", "work", "APPROVALS.md")
	writeFile(t, ap, sampleApprovals)
	reply := filepath.Join(dir, "r.json")
	writeFile(t, reply, `{"items":[{"n":1,"choice":"hold","comment":"あとで"}]}`)
	var so, se bytes.Buffer
	if code := dispatch([]string{"approvals", "apply", "-file", ap, "-reply", reply, "-dir", filepath.Join(dir, "tmp"), "-date", "2026-03-04"}, &so, &se); code != 0 {
		t.Fatalf("code=%d\n%s", code, se.String())
	}
	mustContain(t, "stdout", so.String(), "決定 0 件", "保留 1 件", "→ 保留（あとで）")
	mustContain(t, "approvals", readFile(t, ap), "## 1. 設定ファイルの形式", "**保留（2026-03-04）:** あとで\n")
	if _, err := os.Stat(filepath.Join(dir, "hub", "docs", "decisions.md")); err == nil {
		t.Error("決定が無いのに decisions.md を作った")
	}
	if _, err := os.Stat(reply); err != nil {
		t.Error("-reply で指定した JSON を動かした")
	}

	se.Reset()
	if code := dispatch([]string{"approvals", "apply", "-file", ap, "-reply", reply, "-date", "bad"}, &so, &se); code != 1 || !strings.Contains(se.String(), "YYYY-MM-DD") {
		t.Errorf("-date 不正: code=%d %s", code, se.String())
	}
	writeFile(t, reply, "{broken")
	se.Reset()
	if code := dispatch([]string{"approvals", "apply", "-file", ap, "-reply", reply}, &so, &se); code != 1 {
		t.Errorf("壊れた JSON: code=%d %s", code, se.String())
	}
}
