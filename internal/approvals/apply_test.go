package approvals

import (
	"strings"
	"testing"
)

func TestApply_HoldIdempotent(t *testing.T) {
	for _, comment := range []string{"", "待つ", "一行目\n二行目"} {
		src := []byte("# 承認待ち\n\n## 1. 題\n本文\n")
		rep := Reply{Items: []ReplyItem{{N: 1, Choice: "hold", Comment: comment}}}
		a := Apply(src, nil, rep, "2026-03-04")
		b := Apply(a.Approvals, nil, rep, "2026-03-04")
		if string(a.Approvals) != string(b.Approvals) {
			t.Errorf("再反映で変化: %s", b.Approvals)
		}
		c := Apply(a.Approvals, nil, rep, "2026-03-05")
		if !strings.Contains(string(c.Approvals), "保留（2026-03-05）") {
			t.Error("別日の保留が消えた")
		}
	}
}

func TestApply_AllWarningsPreservesInput(t *testing.T) {
	src := []byte("# 承認待ち\r\n\r\n## 8. 題\r\n本文\r\n\r\n")
	res := Apply(src, nil, Reply{Items: []ReplyItem{{N: 99, Choice: "A"}}}, "2026-03-04")
	if string(src) != string(res.Approvals) {
		t.Errorf("未反映で変化: %s", res.Approvals)
	}
}

// 見出しが同じ項目が並んでいても、答えた項目だけを消す(消し込みを題で引くと、答えていない同題の項目まで消える)。
func TestApply_DuplicateTitles(t *testing.T) {
	src := []byte("# 承認待ち\n\n" +
		"## 1. 命名\n\n**決めたいこと:** 索引の列名\n**なぜ今決めるか:** 次の PR\n**選択肢:**\n" +
		"- A. path — 短い\n- B. file — 明確\n**私の案:** A — 短い\n**決めないとどうなるか:** 止まる\n\n" +
		"## 2. 命名\n\n**決めたいこと:** 設定の鍵名\n**なぜ今決めるか:** 次の PR\n**選択肢:**\n" +
		"- A. notes_dirs — 複数形\n- B. notes_dir — 単数形\n**私の案:** A — 複数形\n**決めないとどうなるか:** 止まる\n")
	rep := Reply{ReceivedAt: "2026-03-04T10:00:00+09:00", Items: []ReplyItem{{N: 2, Title: "命名", Choice: "B"}}}
	res := Apply(src, []byte("# 設計判断\n"), rep, "2026-03-04")
	if res.Decided != 1 {
		t.Fatalf("decided=%d summary=%v", res.Decided, res.Summary)
	}
	if !strings.Contains(string(res.Decisions), "## 命名 → B. notes_dir\n") {
		t.Errorf("decisions =\n%s", res.Decisions)
	}
	d := Parse(res.Approvals)
	if len(d.Items) != 1 {
		t.Fatalf("残る項目 = %d 件（答えていない項目まで消えた）:\n%s", len(d.Items), res.Approvals)
	}
	if got := d.Items[0].Fields[FieldWhat]; got != "索引の列名" {
		t.Errorf("残ったのが別の項目: %q\n%s", got, res.Approvals)
	}
}

// 受信時刻の無い回答(手で書いた JSON を -reply で渡した場合)でも、根拠行に空の時刻を出さない。
func TestApply_NoReceivedAt(t *testing.T) {
	rep := Reply{Items: []ReplyItem{{N: 1, Title: "ログの出力先", Choice: "A"}}}
	res := Apply(load(t, "two-items.md"), nil, rep, "2026-03-04")
	if res.Decided != 1 {
		t.Fatalf("decided=%d summary=%v", res.Decided, res.Summary)
	}
	if !strings.Contains(string(res.Decisions), "根拠: 会話 2026-03-04（ユーザー判断・承認フォームの回答）。") {
		t.Errorf("根拠行 =\n%s", res.Decisions)
	}
}

// コメント無しの保留(フォームで「保留」だけ押した場合)でも、書き戻す行に余分な空白や空の括弧を残さない。
func TestApply_HoldWithoutComment(t *testing.T) {
	rep := Reply{Items: []ReplyItem{{N: 2, Title: "ログの出力先", Choice: "hold"}}}
	res := Apply(load(t, "two-items.md"), nil, rep, "2026-03-04")
	if res.Held != 1 {
		t.Fatalf("held=%d summary=%v", res.Held, res.Summary)
	}
	if !strings.Contains(string(res.Approvals), "**保留（2026-03-04）:**\n") {
		t.Errorf("保留行の末尾に空白が残る: %q", string(res.Approvals))
	}
	if got := res.Summary[0]; got != "[2] ログの出力先 → 保留" {
		t.Errorf("summary = %q", got)
	}
	if d := Parse(res.Approvals); len(d.Items) != 2 || len(d.Items[1].Holds) != 2 {
		t.Errorf("再解析: %+v", d.Items)
	}
}

func TestApply_ChoiceAndHold(t *testing.T) {
	rep := Reply{ReceivedAt: "2026-03-04T10:00:00+09:00", Items: []ReplyItem{
		{N: 1, Title: "設定ファイルの形式を JSON にするか TOML にするか", Choice: "A", Comment: ""},
		{N: 2, Title: "ログの出力先", Choice: "hold", Comment: "CI 側の仕様を見てから"},
	}}
	res := Apply(load(t, "two-items.md"), []byte("# 設計判断\n\n## 既存の決定\n\n記録日: 2026-01-01\n理由: r\n根拠: e\n"), rep, "2026-03-04")
	if res.Decided != 1 || res.Held != 1 {
		t.Errorf("decided=%d held=%d", res.Decided, res.Held)
	}
	wantDec := "# 設計判断\n\n## 既存の決定\n\n記録日: 2026-01-01\n理由: r\n根拠: e\n" +
		"\n## 設定ファイルの形式を JSON にするか TOML にするか → A. JSON\n\n" +
		"記録日: 2026-03-04\n" +
		"理由: 標準ライブラリだけで読める／コメントが書けない。私の案の理由: 依存を増やさない方針（決定 2026-01-10）と合う。却下: B. TOML（コメントが書ける／依存が 1 つ増える）\n" +
		"根拠: 会話 2026-03-04（ユーザー判断・承認フォームの回答 2026-03-04T10:00:00+09:00）。なぜ今決めたか: 設定の読み込みを次の PR で実装するため。文面は braindex approvals apply の機械生成（結論文は整えてよい）\n"
	if string(res.Decisions) != wantDec {
		t.Errorf("decisions =\n%s\nwant\n%s", res.Decisions, wantDec)
	}
	wantAp := "# 承認待ち\n\n空が正常。答えが出たら理由ごと `docs/decisions.md` へ移す。\n\n" +
		"## 1. ログの出力先\n\n**決めたいこと:** 実行ログを stderr に出すかファイルに書くか\n**なぜ今決めるか:** CI で出力を拾う必要が出た\n**選択肢:**\n" +
		"- A. stderr — 追加の設定なし／長い実行で流れる\n- B. ファイル — 残る／置き場の規約が要る\n- C. 両方 — 便利／実装が 2 倍\n" +
		"**私の案:** 案なし\n**決めないとどうなるか:** 既定で stderr のまま進む\n**保留（2026-02-01）:** CI の仕様が決まるまで\n" +
		"**保留（2026-03-04）:** CI 側の仕様を見てから\n"
	if string(res.Approvals) != wantAp {
		t.Errorf("approvals =\n%s\nwant\n%s", res.Approvals, wantAp)
	}
	if strings.Join(res.Summary, "|") != "[1] 設定ファイルの形式を JSON にするか TOML にするか → A. JSON|[2] ログの出力先 → 保留（CI 側の仕様を見てから）" {
		t.Errorf("summary = %v", res.Summary)
	}
	// 反映後をもう一度解析できる(保留行は欄にならない)
	d2 := Parse(res.Approvals)
	if len(d2.Items) != 1 || d2.Items[0].N != 1 || len(d2.Items[0].Holds) != 2 || len(d2.Items[0].Warnings) != 0 {
		t.Errorf("再解析: %+v", d2.Items)
	}
}

func TestApply_OtherAndAllDecided(t *testing.T) {
	rep := Reply{ReceivedAt: "2026-03-04T10:00:00+09:00", Items: []ReplyItem{
		{N: 1, Title: "設定ファイルの形式を JSON にするか TOML にするか", Choice: "B", Comment: "コメントが要る\n将来の拡張"},
		{N: 2, Title: "ログの出力先", Choice: "other", Comment: "stderr と JSON Lines のファイルの両方\n\nCI では後者を拾う"},
	}}
	res := Apply(load(t, "two-items.md"), nil, rep, "2026-03-04")
	if res.Decided != 2 || res.Held != 0 {
		t.Errorf("decided=%d held=%d", res.Decided, res.Held)
	}
	wantAp := "# 承認待ち\n\n（なし。2026-03-04 に承認フォームの回答で 2 件を docs/decisions.md へ移動）\n"
	if string(res.Approvals) != wantAp {
		t.Errorf("approvals =\n%s", res.Approvals)
	}
	dec := string(res.Decisions)
	if !strings.HasPrefix(dec, "# 設計判断\n\n## 設定ファイルの形式を JSON にするか TOML にするか → B. TOML\n") {
		t.Errorf("空の decisions には H1 を足す:\n%s", dec)
	}
	mustHave := []string{
		"理由: コメントが書ける／依存が 1 つ増える。補足: コメントが要る 将来の拡張（私の案 A は不採用）。却下: A. JSON（標準ライブラリだけで読める／コメントが書けない）\n",
		"\n## ログの出力先 → stderr と JSON Lines のファイルの両方\n\n記録日: 2026-03-04\n理由: CI では後者を拾う。却下: A. stderr（追加の設定なし／長い実行で流れる）／B. ファイル（残る／置き場の規約が要る）／C. 両方（便利／実装が 2 倍）\n",
	}
	for _, w := range mustHave {
		if !strings.Contains(dec, w) {
			t.Errorf("decisions に無い:\n%s\n---\n%s", w, dec)
		}
	}
	if len(res.Summary) != 2 || !strings.Contains(res.Summary[1], "その他: stderr と JSON Lines のファイルの両方 CI では後者を拾う") {
		t.Errorf("summary = %v", res.Summary)
	}
}

// decisions.md に同じ見出しが既にあるときは、追記はするが検知して知らせる(同じ判断を 2 回積んでも気づけないため)。
func TestApply_DuplicateHeadingWarns(t *testing.T) {
	dec := []byte("# 設計判断\n\n## 設定ファイルの形式を JSON にするか TOML にするか → A. JSON\n\n記録日: 2026-01-01\n理由: r\n根拠: e\n")
	rep := Reply{ReceivedAt: "2026-03-04T10:00:00+09:00", Items: []ReplyItem{
		{N: 1, Title: "設定ファイルの形式を JSON にするか TOML にするか", Choice: "A"},
	}}
	res := Apply(load(t, "two-items.md"), dec, rep, "2026-03-04")
	if res.Decided != 1 {
		t.Fatalf("decided=%d summary=%v", res.Decided, res.Summary)
	}
	if len(res.DuplicateHeadings) != 1 || res.DuplicateHeadings[0] != "設定ファイルの形式を JSON にするか TOML にするか → A. JSON" {
		t.Errorf("重複見出しを検知できない: %v", res.DuplicateHeadings)
	}
	// 追記そのものは止めない(止めると回答が失われる)。同じ見出しが 2 回出る。
	if n := strings.Count(string(res.Decisions), "## 設定ファイルの形式を JSON にするか TOML にするか → A. JSON"); n != 2 {
		t.Errorf("追記が止まっている:\n%s", res.Decisions)
	}
	// 見出しが違えば検知しない
	rep2 := Reply{Items: []ReplyItem{{N: 2, Title: "ログの出力先", Choice: "hold"}}}
	res2 := Apply(load(t, "two-items.md"), dec, rep2, "2026-03-04")
	if len(res2.DuplicateHeadings) != 0 {
		t.Errorf("違う見出しなのに検知した: %v", res2.DuplicateHeadings)
	}
}

func TestApply_UnknownAndNoop(t *testing.T) {
	src := load(t, "two-items.md")
	dec := []byte("# 設計判断\n")
	rep := Reply{Items: []ReplyItem{
		{N: 9, Title: "存在しない", Choice: "A"},
		{N: 1, Title: "設定ファイルの形式を JSON にするか TOML にするか", Choice: "Z"},
		{N: 2, Title: "ログの出力先", Choice: "other", Comment: ""},
	}}
	res := Apply(src, dec, rep, "2026-03-04")
	if res.Decided != 0 || res.Held != 1 || string(res.Decisions) != string(dec) {
		t.Errorf("decided=%d held=%d decisions=%q", res.Decided, res.Held, res.Decisions)
	}
	if len(res.Summary) != 3 || !strings.HasPrefix(res.Summary[0], "警告: 回答の項目 [9] 存在しない") || !strings.Contains(res.Summary[1], `"Z" が選択肢に無い`) || !strings.Contains(res.Summary[2], "コメント無し → 保留扱い") {
		t.Errorf("summary = %v", res.Summary)
	}
	if !strings.Contains(string(res.Approvals), "## 1. 設定ファイル") || !strings.Contains(string(res.Approvals), "## 2. ログの出力先") {
		t.Errorf("項目が残る:\n%s", res.Approvals)
	}
	// 題名で引けないとき n で引く
	res = Apply(src, dec, Reply{Items: []ReplyItem{{N: 1, Choice: "A"}}}, "2026-03-04")
	if res.Decided != 1 {
		t.Errorf("n で引けない: %v", res.Summary)
	}
}
