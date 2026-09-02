package approvals

import (
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

func TestResolve(t *testing.T) {
	root := t.TempDir()
	hub := filepath.Join(root, "my hub!")
	p, err := Resolve(filepath.Join(hub, "work", "APPROVALS.md"), filepath.Join(root, "tmp"))
	if err != nil {
		t.Fatal(err)
	}
	if p.Project != hub {
		t.Errorf("Project = %q, want %q", p.Project, hub)
	}
	if p.Decisions != filepath.Join(hub, "docs", "decisions.md") {
		t.Errorf("Decisions = %q", p.Decisions)
	}
	if !regexp.MustCompile(`^my-hub-[0-9a-f]{6}$`).MatchString(p.ID) {
		t.Errorf("ID = %q", p.ID)
	}
	if p.Reply != filepath.Join(root, "tmp", "approvals-"+p.ID+".reply.json") || !strings.HasSuffix(p.Applied, ".applied.json") {
		t.Errorf("Reply = %q Applied = %q", p.Reply, p.Applied)
	}
	// 同じパスなら同じ id、別の hub なら別の id
	q, _ := Resolve(filepath.Join(hub, "work", "APPROVALS.md"), "")
	if q.ID != p.ID {
		t.Errorf("同じ hub で id が違う: %q %q", p.ID, q.ID)
	}
	if !strings.HasPrefix(q.Reply, DefaultDir()) {
		t.Errorf("dir 省略時は DefaultDir: %q", q.Reply)
	}
	r, _ := Resolve(filepath.Join(root, "other", "work", "APPROVALS.md"), "")
	if r.ID == p.ID {
		t.Error("別の hub で id が同じ")
	}
	// work 直下でないファイルは親をプロジェクトとみなす
	s, _ := Resolve(filepath.Join(root, "flat", "APPROVALS.md"), "")
	if s.Project != filepath.Join(root, "flat") {
		t.Errorf("work 無し: Project = %q", s.Project)
	}
}
