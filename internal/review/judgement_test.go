package review

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

// 機械節は毎週埋まるので回っているように見えるが、判断の節が空なら振り返りの回路は動いていない。
// 見出しがあって中身の無い節だけを返す(設計レビュー 2026-09-06 M7)。
func TestEmptyJudgementSections(t *testing.T) {
	dir := t.TempDir()
	write := func(name, body string) string {
		p := filepath.Join(dir, name)
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
		return p
	}
	draft := func(digest, archive, next string) string {
		return "# 週次レビュー 2026-09-02\n\n## 索引（件数と増減）\n\n前回 1 件 → 今回 1 件\n" +
			"\n## 今週の差分ダイジェスト（リポ別）\n\n" + digest +
			"\n## アーカイブ（実施・見送りと理由）\n\n" + archive +
			"\n## 次アクション\n\n" + next
	}

	cases := []struct {
		desc, body string
		want       []string
	}{
		{"全部ひな型のまま", draft("（差分ファイルを実物で読み、リポごとに 1〜3 行）\n", "（候補ごとに 実施／見送り と理由）\n", "（1〜3 件）\n"),
			JudgementSections},
		{"全部埋まっている", draft("- repo-a: フィードの取得を直した\n", "- 見送り: まだ参照する\n", "- 取材先を 1 つ足す\n"), nil},
		{"次アクションだけ空", draft("- repo-a: 直した\n", "- 実施: 移した\n", "（1〜3 件）\n"), []string{"次アクション"}},
		{"空行だけ", draft("\n\n", "- 実施\n", "- 次\n"), []string{"今週の差分ダイジェスト（リポ別）"}},
		{"見出しごと無いファイルは何も言わない", "# 週次レビュー 2026-09-02\n\n覚え書き\n", nil},
	}
	for _, c := range cases {
		p := write("d.md", c.body)
		got, warns := emptyJudgementSections(p)
		if !reflect.DeepEqual(got, c.want) {
			t.Errorf("emptyJudgementSections[%s]: want=%v got=%v", c.desc, c.want, got)
		}
		if len(warns) != 0 {
			t.Errorf("[%s] 警告: %v", c.desc, warns)
		}
	}

	// 前回のファイルが無い・パスが空なら何も言わない(初回・別名で出したとき)
	if got, warns := emptyJudgementSections(filepath.Join(dir, "no-such.md")); got != nil || warns != nil {
		t.Errorf("無いファイル: %v %v", got, warns)
	}
	if got, warns := emptyJudgementSections(""); got != nil || warns != nil {
		t.Errorf("空のパス: %v %v", got, warns)
	}

	// コードフェンスの中の見出しは節にしない
	p := write("fence.md", "# R\n\n## 次アクション\n\n```\n## 今週の差分ダイジェスト（リポ別）\n```\n- 次\n")
	if got, _ := emptyJudgementSections(p); got != nil {
		t.Errorf("フェンス内の見出しを節と見た: %v", got)
	}
}
