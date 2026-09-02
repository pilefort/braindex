package lint

import (
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"
)

var today = time.Date(2026, 9, 2, 0, 0, 0, 0, time.UTC)

// 規約どおりの ISSUE(指摘なし)。
const good = `# ISSUE: 例のタスク

仕様: SPEC-example.md

## 現在の作業
A を B にする。

## 状態
- [x] 調べた
- [ ] 直す  ← いまここ
- [ ] テスト

## 対象リスト
- [x] one.md
- [ ] two.md

## メモ
なし

最終更新: 2026-09-01
`

func check(t *testing.T, content string, o Options) []Warning {
	t.Helper()
	if o.Today.IsZero() {
		o.Today = today
	}
	if o.Exists == nil {
		o.Exists = func(string) bool { return true }
	}
	return Check("work/ISSUE-example.md", []byte(content), o)
}

func dump(ws []Warning) string {
	var b strings.Builder
	for _, w := range ws {
		fmt.Fprintf(&b, "%s:%d: %s\n", w.Path, w.Line, w.Msg)
	}
	return b.String()
}

// wantOne は指摘がちょうど 1 件で、その本文に subs をすべて含み、行番号が line であることを確かめる。
func wantOne(t *testing.T, ws []Warning, line int, subs ...string) {
	t.Helper()
	if len(ws) != 1 {
		t.Fatalf("指摘が %d 件(1 件を期待):\n%s", len(ws), dump(ws))
	}
	if ws[0].Line != line {
		t.Errorf("行番号 %d(%d を期待): %s", ws[0].Line, line, ws[0].Msg)
	}
	for _, s := range subs {
		if !strings.Contains(ws[0].Msg, s) {
			t.Errorf("指摘に %q が無い: %s", s, ws[0].Msg)
		}
	}
	if ws[0].Path != "work/ISSUE-example.md" {
		t.Errorf("Path が渡したものと違う: %q", ws[0].Path)
	}
}

func wantNone(t *testing.T, ws []Warning) {
	t.Helper()
	if len(ws) != 0 {
		t.Fatalf("指摘なしを期待:\n%s", dump(ws))
	}
}

func TestCheck_Good(t *testing.T) {
	wantNone(t, check(t, good, Options{}))
}

// 先頭の見出しは「# ISSUE: <タスク名>」。無い・別の見出しは指摘。
func TestCheck_H1(t *testing.T) {
	wantOne(t, check(t, strings.Replace(good, "# ISSUE: 例のタスク\n", "", 1), Options{}), 0, "# ISSUE:")
	wantOne(t, check(t, strings.Replace(good, "# ISSUE: 例のタスク", "# 例のタスク", 1), Options{}), 1, "# ISSUE:")
	// 全角コロンも許す
	wantNone(t, check(t, strings.Replace(good, "# ISSUE: 例のタスク", "# ISSUE：例のタスク", 1), Options{}))
}

// 必須の節は「## 現在の作業」と「## 状態」。対象リスト・メモは任意。
func TestCheck_Sections(t *testing.T) {
	wantOne(t, check(t, strings.Replace(good, "## 現在の作業\n", "## いまの作業\n", 1), Options{}), 0, "## 現在の作業")
	wantOne(t, check(t, strings.Replace(good, "## 状態\n", "## 進捗\n", 1), Options{}), 0, "## 状態")
	noOptional := strings.Replace(good, "## 対象リスト\n- [x] one.md\n- [ ] two.md\n\n## メモ\nなし\n\n", "", 1)
	wantNone(t, check(t, noOptional, Options{}))
	// 見出しの後ろに補足が続いてもよい
	wantNone(t, check(t, strings.Replace(good, "## 状態\n", "## 状態（PR 単位）\n", 1), Options{}))
}

// 「← いまここ」はちょうど 1 つ。無ければ次の 1 手が分からず、2 つ以上なら現在地が曖昧。
func TestCheck_HereMarker(t *testing.T) {
	wantOne(t, check(t, strings.Replace(good, "  ← いまここ", "", 1), Options{}), 0, "いまここ", "無い")
	two := strings.Replace(good, "- [ ] テスト\n", "- [ ] テスト ← いまここ\n", 1)
	wantOne(t, check(t, two, Options{}), 11, "いまここ", "2 個")
}

// 「最終更新: YYYY-MM-DD」の有無・形式・未来・経過日数。
func TestCheck_LastUpdated(t *testing.T) {
	wantOne(t, check(t, strings.Replace(good, "最終更新: 2026-09-01\n", "", 1), Options{}), 0, "最終更新", "無い")
	wantOne(t, check(t, strings.Replace(good, "2026-09-01", "2026-9-1", 1), Options{}), 20, "YYYY-MM-DD")
	wantOne(t, check(t, strings.Replace(good, "2026-09-01", "2026-09-03", 1), Options{}), 20, "未来")
	// 経過日数: StaleDays 0 は見ない。7 なら 32 日前は指摘、3 日前は指摘しない
	old := strings.Replace(good, "2026-09-01", "2026-08-01", 1)
	wantNone(t, check(t, old, Options{StaleDays: 0}))
	wantOne(t, check(t, old, Options{StaleDays: 7}), 20, "32 日", "2026-08-01")
	wantNone(t, check(t, strings.Replace(good, "2026-09-01", "2026-08-30", 1), Options{StaleDays: 7}))
	// 全角コロンも許す
	wantNone(t, check(t, strings.Replace(good, "最終更新: 2026-09-01", "最終更新：2026-09-01", 1), Options{}))
	// 日付の後に補足が続いてもよい。数字が続くものは形式の指摘
	wantNone(t, check(t, strings.Replace(good, "最終更新: 2026-09-01", "最終更新: 2026-09-01（PR を出した）", 1), Options{}))
	wantOne(t, check(t, strings.Replace(good, "2026-09-01", "2026-09-011", 1), Options{}), 20, "YYYY-MM-DD")
}

// 「仕様: SPEC-<slug>.md」の参照先が無ければ指摘。参照が無いファイルは見ない。
func TestCheck_SpecRef(t *testing.T) {
	missing := func(string) bool { return false }
	wantOne(t, check(t, good, Options{Exists: missing}), 3, "SPEC-example.md", "無い")
	wantNone(t, check(t, strings.Replace(good, "仕様: SPEC-example.md\n\n", "", 1), Options{Exists: missing}))
	// Exists が nil なら見ない
	ws := Check("work/ISSUE-example.md", []byte(good), Options{Today: today})
	wantNone(t, ws)
}

// HEAD にあったチェック項目が消えていたら指摘。印の移動・チェックの変化は消失ではない。
func TestCheck_ChecklistLost(t *testing.T) {
	prev := []byte(good)
	lost := strings.Replace(good, "- [ ] two.md\n", "", 1)
	lost = strings.Replace(lost, "2026-09-01", "2026-09-02", 1)
	wantOne(t, check(t, lost, Options{Prev: prev, HasPrev: true}), 0, "two.md", "消えた")

	moved := strings.Replace(good, "- [ ] 直す  ← いまここ\n- [ ] テスト\n", "- [x] 直す\n- [ ] テスト  ← いまここ\n", 1)
	moved = strings.Replace(moved, "2026-09-01", "2026-09-02", 1)
	wantNone(t, check(t, moved, Options{Prev: prev, HasPrev: true}))

	wantNone(t, check(t, good, Options{Prev: prev, HasPrev: true}))
	// HasPrev が false なら比較しない
	wantNone(t, check(t, lost, Options{Prev: prev, HasPrev: false}))
}

// 内容が変わったのに最終更新が HEAD と同じなら指摘。改行コードだけの違いは変化とみなさない。
func TestCheck_DateNotBumped(t *testing.T) {
	prev := []byte(good)
	changed := strings.Replace(good, "A を B にする。", "A を C にする。", 1)
	wantOne(t, check(t, changed, Options{Prev: prev, HasPrev: true}), 20, "最終更新", "HEAD")
	bumped := strings.Replace(changed, "2026-09-01", "2026-09-02", 1)
	wantNone(t, check(t, bumped, Options{Prev: prev, HasPrev: true}))
	wantNone(t, check(t, good, Options{Prev: prev, HasPrev: true}))
	crlf := strings.ReplaceAll(good, "\n", "\r\n")
	wantNone(t, check(t, crlf, Options{Prev: prev, HasPrev: true}))
}

// BOM と CRLF は正規化してから見る。同じ入力からは同じ指摘(決定性)。
func TestCheck_NormalizeAndDeterministic(t *testing.T) {
	bad := strings.Replace(good, "  ← いまここ", "", 1)
	bad = strings.Replace(bad, "## 状態\n", "## 進捗\n", 1)
	raw := append([]byte{0xEF, 0xBB, 0xBF}, []byte(strings.ReplaceAll(bad, "\n", "\r\n"))...)
	o := Options{Today: today, Exists: func(string) bool { return true }}
	a := Check("x.md", raw, o)
	b := Check("x.md", raw, o)
	if !reflect.DeepEqual(a, b) {
		t.Errorf("同じ入力で指摘が違う:\n%s\n---\n%s", dump(a), dump(b))
	}
	if len(a) != 2 {
		t.Fatalf("指摘 2 件を期待:\n%s", dump(a))
	}
	if strings.Contains(dump(a), "\r") {
		t.Errorf("指摘に CR が混じる: %q", dump(a))
	}
}

// 指摘は行番号の昇順(ファイル全体への指摘 = 0 が先頭)。
func TestCheck_Order(t *testing.T) {
	bad := strings.Replace(good, "# ISSUE: 例のタスク", "# 例のタスク", 1) // 1 行目
	bad = strings.Replace(bad, "2026-09-01", "2026-09-03", 1)    // 20 行目(未来)
	bad = strings.Replace(bad, "## 状態\n", "## 進捗\n", 1)          // 0(節が無い)
	ws := check(t, bad, Options{})
	if len(ws) != 3 {
		t.Fatalf("指摘 3 件を期待:\n%s", dump(ws))
	}
	for i := 1; i < len(ws); i++ {
		if ws[i-1].Line > ws[i].Line {
			t.Errorf("行番号順でない:\n%s", dump(ws))
		}
	}
}
