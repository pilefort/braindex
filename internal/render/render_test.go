package render

import (
	"bytes"
	"strings"
	"testing"
)

func sampleEntries() []Entry {
	// わざと未ソート(repo・日付バラバラ)で渡し、Render 側のソートを検証する。
	return []Entry{
		{Repo: "repo-b", Date: "2026-07-20", Kind: "notes", Title: "Title B | pipe", Summary: "Sum B | x", Path: "repo-b/docs/notes/b.md"},
		{Repo: "repo-a", Date: "2026-07-28", Kind: "notes/project", Title: "Title A", Summary: "Summary A", Path: "repo-a/docs/notes/project/a.md"},
		{Repo: "repo-a", Date: "2026-07-30", Kind: "decisions", Title: "Dec A", Summary: "d1 / d2", Path: "repo-a/docs/decisions.md"},
	}
}

func TestRender_Golden(t *testing.T) {
	want := strings.Join([]string{
		"# 知識カタログ(braindex 自動生成 — 手で編集しない)",
		"",
		"生成: 2026-08-07 / 2 リポジトリ / 3 件",
		"再生成: brain ルートで `go run ./cmd/braindex`",
		"使い方: このファイルを grep → ヒット行のパス(projects ルート相対)の実ファイルを読む",
		"",
		"## repo-a",
		"| 日付 | 種別 | タイトル | 要旨 | パス |",
		"|---|---|---|---|---|",
		"| 2026-07-30 | decisions | Dec A | d1 / d2 | repo-a/docs/decisions.md |",
		"| 2026-07-28 | notes/project | Title A | Summary A | repo-a/docs/notes/project/a.md |",
		"",
		"## repo-b",
		"| 日付 | 種別 | タイトル | 要旨 | パス |",
		"|---|---|---|---|---|",
		"| 2026-07-20 | notes | Title B ｜ pipe | Sum B ｜ x | repo-b/docs/notes/b.md |",
		"",
	}, "\n")

	got := string(Render(sampleEntries(), "2026-08-07"))
	if got != want {
		t.Errorf("catalog 不一致:\n--- got ---\n%s\n--- want ---\n%s", got, want)
	}
}

func TestRender_Deterministic(t *testing.T) {
	// 同一入力・同一 genDate なら 2 回生成してバイト一致(週次 diff の前提)。
	a := Render(sampleEntries(), "2026-08-07")
	b := Render(sampleEntries(), "2026-08-07")
	if !bytes.Equal(a, b) {
		t.Errorf("2 回生成でバイト不一致")
	}
}

func TestRender_TildeDateSort(t *testing.T) {
	// "~" 印(mtime 近似)は比較時に外す。~2026-07-20 は 2026-07-10 より新しいので先に出る。
	es := []Entry{
		{Repo: "r", Date: "2026-07-10", Kind: "notes", Title: "古い", Summary: "s", Path: "r/docs/notes/old.md"},
		{Repo: "r", Date: "~2026-07-20", Kind: "notes", Title: "新しい", Summary: "s", Path: "r/docs/notes/new.md"},
	}
	got := string(Render(es, "2026-08-07"))
	iNew := strings.Index(got, "新しい")
	iOld := strings.Index(got, "古い")
	if iNew < 0 || iOld < 0 || iNew > iOld {
		t.Errorf("~日付の降順ソートが不正: new=%d old=%d", iNew, iOld)
	}
}
