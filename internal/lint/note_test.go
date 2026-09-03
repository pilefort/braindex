package lint

import (
	"strings"
	"testing"
)

// kinds は指摘の種別を出現順に返す。
func kinds(ws []Warning) []string {
	out := make([]string, 0, len(ws))
	for _, w := range ws {
		out = append(out, w.Kind)
	}
	return out
}

func has(ws []Warning, kind string) bool {
	for _, w := range ws {
		if w.Kind == kind {
			return true
		}
	}
	return false
}

func tokens(ws []Warning, kind string) map[string]bool {
	m := map[string]bool{}
	for _, w := range ws {
		if w.Kind != kind {
			continue
		}
		if i := strings.Index(w.Msg, "〔"); i >= 0 {
			if j := strings.Index(w.Msg, "〕"); j > i {
				m[w.Msg[i+len("〔"):j]] = true
			}
		}
	}
	return m
}

func note(t *testing.T, content string) []Warning {
	t.Helper()
	return CheckNote("docs/notes/x.md", []byte(content), NoteOptions{})
}

// 判定表: 1 行の本文に対してどの種別が出るか(原型 record-lint の test_lint.py を写した)。
func TestCheckNote_Table(t *testing.T) {
	cases := []struct {
		name    string
		content string
		want    []string // 出てほしい種別
		unwant  []string // 出てはいけない種別
	}{
		{"曖昧な数量詞を語ごとに拾う", "2026-08-12 最近、更新が多い。かなり伸びた。", []string{KindVagueQuantifier}, nil},
		{"数値と日付だけなら指摘なし", "2026-08-12 に 3 件増えた。", nil, []string{KindVagueQuantifier, KindNoDate}},
		{"コードフェンスの中は見ない", "```\n最近\n```\n2026-08-12\n", nil, []string{KindVagueQuantifier}},
		{"日付が無ければ文書全体に warn", "日付のない文", []string{KindNoDate}, nil},
		{"スラッシュの日付", "2026/8/1 実施", nil, []string{KindNoDate}},
		{"和式の日付", "2026 年 8 月 実施", nil, []string{KindNoDate}},
		{"出典なき数字は候補", "2026-08-12 CPU は 4.2% だった。", []string{KindUncitedFigure}, nil},
		{"出典マーカーがあれば数字を許す", "2026-08-12 CPU は 4.2%(出典: 分析ノート §1)", nil, []string{KindUncitedFigure}},
		{"矢印リンクも出典", "2026-08-12 3 件増えた(→ work/foo.csv)", nil, []string{KindUncitedFigure}},
		{"表の行は見ない", "2026-08-12\n| 指標 | 値 |\n|---|---|\n| CPU | 4.2% |", nil, []string{KindUncitedFigure}},
		{"小数だけの主張も候補", "2026-08-12 平均は 3.14 だった", []string{KindUncitedFigure}, nil},
		{"裸のヘッジは候補", "2026-08-12 これはたぶん正しい。", []string{KindBareHedge}, nil},
		{"推測タグがあればヘッジを許す", "2026-08-12 たぶん正しい（推測）", nil, []string{KindBareHedge}},
		{"未確認タグも許す", "2026-08-12 おそらく古い(未確認)", nil, []string{KindBareHedge}},
		{"出典があればヘッジを許す", "2026-08-12 おそらく正しい(出典: 分析ノート §1)", nil, []string{KindBareHedge}},
		{"ヘッジなし", "2026-08-12 に確定した。", nil, []string{KindBareHedge}},
		{"決定に理由が無ければ候補", "## 決定\n記録日: 2026-08-12\nこうする。\n", []string{KindMissingWhy}, nil},
		{"理由があればなぜ欠落は出ない", "## 決定\n記録日: 2026-08-12\n理由: 安全なため。\n根拠: 会話 2026-08-12\n", nil, []string{KindMissingWhy}},
		{"根拠行が無ければ候補", "## 決定\n記録日: 2026-08-25\n理由: 安全なため。\n", []string{KindMissingEvidence}, nil},
		{"根拠行があれば出ない", "## 決定\n記録日: 2026-08-25\n理由: 安全なため。\n根拠: 会話 2026-08-25(ユーザー判断)\n", nil, []string{KindMissingEvidence}},
		{"太字・全角コロンの根拠行も許す", "## 決定\n記録日: 2026-08-25\n理由: 安全なため。\n**根拠**： → docs/notes/x.md\n", nil, []string{KindMissingEvidence}},
		{"本文の矢印リンクだけでは根拠行の代わりにならない", "## 決定\n記録日: 2026-09-01\n理由: 実測(→ docs/notes/x.md)で速かったため。\n", []string{KindMissingEvidence}, nil},
		{"古い記録日でも根拠行は必須(遡及規則は持ち込まない)", "## 決定\n記録日: 2026-08-05\n実測(→ docs/notes/x.md)で速かったため。\n", []string{KindMissingEvidence}, nil},
		{"記録日の無いブロックは決定でない", "2026-08-12\n## 見出し\n本文。\n", nil, []string{KindMissingWhy, KindMissingEvidence}},
		{"記録日が日付で書かれていない決定も見る", "2026-08-12\n## 決定\n記録日: 不明(記録なし)\nこうする。\n", []string{KindMissingWhy, KindMissingEvidence}, nil},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			ws := note(t, c.content)
			for _, k := range c.want {
				if !has(ws, k) {
					t.Errorf("%s が出ない: %v", k, kinds(ws))
				}
			}
			for _, k := range c.unwant {
				if has(ws, k) {
					t.Errorf("%s が出てはいけない: %v", k, kinds(ws))
				}
			}
		})
	}
}

// 曖昧な数量詞は語ごとに 1 件・確度 warn・表示は「[曖昧な数量詞] 〔語〕 行」。
func TestCheckNote_VagueTokens(t *testing.T) {
	ws := note(t, "2026-08-12 最近、更新が多い。かなり伸びた。")
	got := tokens(ws, KindVagueQuantifier)
	for _, w := range []string{"最近", "多い", "かなり"} {
		if !got[w] {
			t.Errorf("〔%s〕 が無い: %v", w, got)
		}
	}
	for _, w := range ws {
		if w.Kind == KindVagueQuantifier && (w.Severity != SeverityWarn || !strings.HasPrefix(w.Msg, "[曖昧な数量詞] 〔")) {
			t.Errorf("形が違う: %+v", w)
		}
	}
}

// 日付はファイル名(20260901-x.md・2026-09-01-x.md)からも数える(索引の日付規則と揃える)。
func TestCheckNote_DateFromFilename(t *testing.T) {
	body := []byte("# 調査メモ\n\n結論: 甲。\n")
	for _, p := range []string{"docs/notes/20260901-survey.md", "docs/notes/2026-09-01-survey.md"} {
		if ws := CheckNote(p, body, NoteOptions{}); has(ws, KindNoDate) {
			t.Errorf("%s: ファイル名に日付があるのに日付なしが出た: %v", p, kinds(ws))
		}
	}
	if ws := CheckNote("docs/notes/survey.md", body, NoteOptions{}); !has(ws, KindNoDate) {
		t.Errorf("どこにも日付が無いのに日付なしが出ない: %v", kinds(ws))
	}
	// 月日が範囲外(13 月)のものは日付とみなさない
	if ws := CheckNote("docs/notes/20261301-survey.md", body, NoteOptions{}); !has(ws, KindNoDate) {
		t.Errorf("13 月を日付として数えた: %v", kinds(ws))
	}
}

// 確度: 数量詞・日付なしは warn、それ以外は candidate。
func TestCheckNote_Severity(t *testing.T) {
	ws := CheckNote("docs/decisions.md", []byte("## 決定\nかなり良い。CPU 4.2% はたぶん正しい。\n"), NoteOptions{Glossary: []byte(""), HasGlossary: true})
	sev := map[string]string{}
	for _, w := range ws {
		sev[w.Kind] = w.Severity
	}
	want := map[string]string{
		KindVagueQuantifier: SeverityWarn, KindNoDate: SeverityWarn,
		KindUncitedFigure: SeverityCandidate, KindBareHedge: SeverityCandidate,
		KindMissingWhy: SeverityCandidate, KindMissingEvidence: SeverityCandidate,
		KindUndefinedTerm: SeverityCandidate,
	}
	for k, s := range want {
		if sev[k] != s {
			t.Errorf("%s: severity=%q want %q(全指摘 %v)", k, sev[k], s, kinds(ws))
		}
	}
}

// decisions.md は記録日の無いブロックも決定として見る。
func TestCheckNote_DecisionsFileTreatsAllBlocks(t *testing.T) {
	ws := CheckNote("repo/docs/decisions.md", []byte("2026-08-12\n## こうする\n本文。\n"), NoteOptions{})
	if !has(ws, KindMissingWhy) || !has(ws, KindMissingEvidence) {
		t.Errorf("decisions.md の全ブロックが決定として見られていない: %v", kinds(ws))
	}
}

// 未定義用語: 用語集があるときだけ、鉤括弧の語・[[link]]・英大文字語のうち用語集に無いものを候補にする。
func TestCheckNote_UndefinedTerm(t *testing.T) {
	text := "2026-08-12 「星読み」と Foobar と [[chunk]] は新語。「既知語」は違う。"
	ws := CheckNote("x.md", []byte(text), NoteOptions{Glossary: []byte("既知語: 定義済み"), HasGlossary: true})
	var got []string
	for _, w := range ws {
		if w.Kind == KindUndefinedTerm {
			got = append(got, w.Msg)
		}
	}
	joined := strings.Join(got, "\n")
	for _, want := range []string{"「星読み」", "「Foobar」", "「chunk」"} {
		if !strings.Contains(joined, want) {
			t.Errorf("%s が無い:\n%s", want, joined)
		}
	}
	if strings.Contains(joined, "「既知語」") {
		t.Errorf("用語集にある語が出た:\n%s", joined)
	}
	if has(CheckNote("x.md", []byte(text), NoteOptions{}), KindUndefinedTerm) {
		t.Errorf("用語集が無いのに未定義用語が出た")
	}
}

// 指摘なしのノート(結論 → 理由 → 根拠・日付つき)。
func TestCheckNote_Clean(t *testing.T) {
	clean := `# 索引の生成は 1 秒で終わる

記録日: 2026-08-12

結論: 120 ファイルの索引を 0.8 秒で生成した(実測 2026-08-12・手元の環境)。
理由: 走査と抽出が 1 パスで済むため。
根拠: → docs/notes/project/bench.md
`
	if ws := note(t, clean); len(ws) != 0 {
		t.Errorf("指摘が出た: %+v", ws)
	}
}

// 決定性: 同じ入力から同じ順で同じ指摘が出る(行番号 → 種別)。
func TestCheckNote_Deterministic(t *testing.T) {
	text := "## 決定\n記録日: 2026-08-25\nかなり多い。CPU 4.2% はたぶん正しい。\n"
	a := CheckNote("docs/decisions.md", []byte(text), NoteOptions{Glossary: []byte(""), HasGlossary: true})
	b := CheckNote("docs/decisions.md", []byte(text), NoteOptions{Glossary: []byte(""), HasGlossary: true})
	if strings.Join(kinds(a), ",") != strings.Join(kinds(b), ",") {
		t.Fatalf("順が違う: %v / %v", kinds(a), kinds(b))
	}
	for i := 1; i < len(a); i++ {
		if a[i-1].Line > a[i].Line || (a[i-1].Line == a[i].Line && a[i-1].Kind > a[i].Kind) {
			t.Errorf("行番号・種別の順でない: %+v の後に %+v", a[i-1], a[i])
		}
	}
}

// BOM と CRLF を正規化して検査する。
func TestCheckNote_BOMAndCRLF(t *testing.T) {
	ws := note(t, "\uFEFF2026-08-12\r\n最近の話。\r\n")
	if !has(ws, KindVagueQuantifier) || has(ws, KindNoDate) {
		t.Errorf("正規化されていない: %v", kinds(ws))
	}
	for _, w := range ws {
		if w.Kind == KindVagueQuantifier && w.Line != 2 {
			t.Errorf("行番号が違う: %+v", w)
		}
	}
}
