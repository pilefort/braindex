package lint

import (
	"reflect"
	"strings"
	"testing"
)

const supersedeBody = "記録日: 2026-09-12\n理由: 手順を統一するため。\n根拠: 会話 2026-09-12\n"

// 正常な書式・違反・旧表記を同じ決定文書で検査する。
func TestCheckNote_Supersede(t *testing.T) {
	cases := []struct {
		name string
		line string
		kind string
		msg  string
	}{
		{"失効", "失効: 2026-09-12 → 新しい手順を採用する", "", ""},
		{"一部失効", "一部失効: 2026-09-12 → 新しい手順を採用する（保存先）", "", ""},
		{"後継は前方一致", "失効: 2026-09-12 → 新しい手順", "", ""},
		{"一部失効の後継も前方一致", "一部失効: 2026-09-12 → 新しい手順（保存先）", "", ""},
		{"失効行なし", "", "", ""},
		{"旧表記", "→ **2026-09-12に上書きされた** 新しい手順を採用する", "", ""},
		{"コロン欠落", "失効 2026-09-12 → 新しい手順を採用する", "supersede_format", "書式"},
		{"全角コロン", "失効： 2026-09-12 → 新しい手順を採用する", "supersede_format", "書式"},
		{"矢印欠落", "失効: 2026-09-12 新しい手順を採用する", "supersede_format", "書式"},
		{"後継空", "失効: 2026-09-12 → ", "supersede_format", "書式"},
		{"後継が空白だけ", "失効: 2026-09-12 → 　", "supersede_format", "書式"},
		{"日付欠落", "失効:  → 新しい手順を採用する", "supersede_format", "書式"},
		{"日付の桁不足", "失効: 2026-9-12 → 新しい手順を採用する", "supersede_date", "YYYY-MM-DD"},
		{"日付の区切り違い", "失効: 2026/09/12 → 新しい手順を採用する", "supersede_date", "YYYY-MM-DD"},
		{"存在しない日付", "失効: 2026-02-30 → 新しい手順を採用する", "supersede_date", "YYYY-MM-DD"},
		{"相対日付", "失効: 同日 → 新しい手順を採用する", "supersede_date", "YYYY-MM-DD"},
		{"一部失効の相対日付", "一部失効: 同日 → 新しい手順を採用する（保存先）", "supersede_date", "YYYY-MM-DD"},
		{"範囲括弧欠落", "一部失効: 2026-09-12 → 新しい手順を採用する", "supersede_format", "書式"},
		{"範囲閉じ括弧欠落", "一部失効: 2026-09-12 → 新しい手順を採用する（保存先", "supersede_format", "書式"},
		{"範囲空", "一部失効: 2026-09-12 → 新しい手順を採用する（）", "supersede_format", "書式"},
		{"範囲が空白だけ", "一部失効: 2026-09-12 → 新しい手順を採用する（　）", "supersede_format", "書式"},
		{"一部失効の後継空", "一部失効: 2026-09-12 → （保存先）", "supersede_format", "書式"},
		{"後継不在", "失効: 2026-09-12 → 別の手順", "supersede_target", "別の手順"},
		{"一部失効の後継不在", "一部失効: 2026-09-12 → 別の手順（保存先）", "supersede_target", "別の手順"},
		{"途中の部分一致は不可", "失効: 2026-09-12 → 手順を採用する", "supersede_target", "手順を採用する"},
		{"後継名の方が長い", "失効: 2026-09-12 → 新しい手順を採用することにする", "supersede_target", "新しい手順を採用することにする"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			content := "# 決定\n## 古い手順\n" + c.line + "\n" + supersedeBody + "\n## 新しい手順を採用する\n" + supersedeBody
			ws := CheckNote("docs/decisions.md", []byte(content), NoteOptions{})
			if c.kind == "" {
				wantNone(t, ws)
				return
			}
			if len(ws) != 1 {
				t.Fatalf("指摘が %d 件(1 件を期待):\n%s", len(ws), dump(ws))
			}
			w := ws[0]
			if w.Path != "docs/decisions.md" || w.Line != 3 || w.Kind != c.kind || w.Severity != SeverityWarn || !strings.Contains(w.Msg, c.msg) {
				t.Errorf("指摘の内容が違う: %+v", w)
			}
		})
	}
}

// 対象は decisions.md のコードフェンス外の ## 直下だけ。
func TestCheckNote_SupersedeScope(t *testing.T) {
	bad := "失効: 同日 → 存在しない決定\n"
	cases := []struct {
		name    string
		path    string
		content string
	}{
		{"通常ノート", "docs/notes/example.md", "## 決定\n" + bad + supersedeBody},
		{"別のファイル名", "docs/other-decisions.md", "## 決定\n" + bad + supersedeBody},
		{"空行の後", "docs/decisions.md", "## 決定\n\n" + bad + supersedeBody},
		{"本文の途中", "docs/decisions.md", "## 決定\n" + supersedeBody + bad},
		{"小見出し", "docs/decisions.md", "## 決定\n" + supersedeBody + "### 補足\n" + bad},
		{"コード例", "docs/decisions.md", "```\n## 決定\n" + bad + supersedeBody + "```\n"},
		{"最終行が見出し", "docs/decisions.md", "2026-09-12\n## 理由と根拠:\n"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			ws := CheckNote(c.path, []byte(c.content), NoteOptions{})
			for _, w := range ws {
				if strings.HasPrefix(w.Kind, "supersede_") {
					t.Errorf("対象外なのに指摘が出た: %+v", w)
				}
			}
		})
	}
}

// 後継は同じファイルの前後どちらにあってもよい。コード例と ### は見出しに数えない。
func TestCheckNote_SupersedeHeadings(t *testing.T) {
	line := "## 古い手順\n失効: 2026-09-12 → 新しい手順\n" + supersedeBody
	wantNone(t, CheckNote("docs/decisions.md", []byte("## 新しい手順\n"+supersedeBody+line), NoteOptions{}))
	for _, heading := range []string{"```\n## 新しい手順\n" + supersedeBody + "```\n", "### 新しい手順\n"} {
		ws := CheckNote("docs/decisions.md", []byte(line+heading), NoteOptions{})
		if len(ws) != 1 || ws[0].Kind != "supersede_target" || ws[0].Line != 2 {
			t.Errorf("後継不在の指摘を期待: %+v", ws)
		}
	}
}

// 日付と後継は独立に検査し、正規化後の行番号・種別順を保つ。
func TestCheckNote_SupersedeDeterministic(t *testing.T) {
	content := "\uFEFF## 古い手順\r\n失効: 同日 → 別の手順\r\n" + strings.ReplaceAll(supersedeBody, "\n", "\r\n")
	path := `repo\docs\decisions.md`
	a := CheckNote(path, []byte(content), NoteOptions{})
	b := CheckNote(path, []byte(content), NoteOptions{})
	if !reflect.DeepEqual(a, b) {
		t.Fatalf("同じ入力で指摘が変わる: %v / %v", a, b)
	}
	if got := kinds(a); !reflect.DeepEqual(got, []string{"supersede_date", "supersede_target"}) {
		t.Fatalf("指摘の種別と順序が違う: %v", got)
	}
	for _, w := range a {
		if w.Path != path || w.Line != 2 || w.Severity != SeverityWarn {
			t.Errorf("正規化後の指摘の位置・確度が違う: %+v", w)
		}
	}
}
