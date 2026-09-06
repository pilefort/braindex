package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/pilefort/braindex/internal/scan/scantest"
)

// diagnoseRoot は root 直下に hub(設定つき)・alpha(ノート 2 件と決定記録)・beta(docs 無し)を置き、設定ファイルのパスを返す。
func diagnoseRoot(t *testing.T) (root, cfgPath string) {
	t.Helper()
	root = t.TempDir()
	cfgPath = filepath.Join(root, "hub", "braindex.json")
	writeFile(t, cfgPath, `{"root": ".."}`)
	writeFile(t, filepath.Join(root, "alpha", "docs", "notes", "a.md"), "# 甲\n\n記録日: 2026-08-01\n\n本文\n")
	writeFile(t, filepath.Join(root, "alpha", "docs", "notes", "common", "c.md"), "# 丙\n\n記録日: 2026-08-03\n\n本文\n")
	writeFile(t, filepath.Join(root, "alpha", "docs", "decisions.md"), "# 決定\n\n## 決めた\n\n記録日: 2026-08-02\n理由: x\n根拠: y\n")
	if err := os.MkdirAll(filepath.Join(root, "beta"), 0o755); err != nil {
		t.Fatal(err)
	}
	return root, cfgPath
}

// generate は索引を作る(引数無しの braindex と同じ経路)。
func generate(t *testing.T, cfgPath string) {
	t.Helper()
	var so, se bytes.Buffer
	if code := dispatch([]string{"-config", cfgPath, "-date", "2026-09-01"}, &so, &se); code != 0 {
		t.Fatalf("索引の生成: exit=%d\n%s%s", code, so.String(), se.String())
	}
}

func diagnoseRun(t *testing.T, args ...string) (code int, stdout, stderr string) {
	t.Helper()
	var so, se bytes.Buffer
	code = dispatch(append([]string{"diagnose"}, args...), &so, &se)
	return code, so.String(), se.String()
}

// 索引がまだ無ければ要確認(2)。作った直後はいまの走査と一致して問題なし(0)。索引もノートも書き換えない。
func TestDiagnose_索引の有無(t *testing.T) {
	root, cfgPath := diagnoseRoot(t)
	code, out, errOut := diagnoseRun(t, "-config", cfgPath, "-date", "2026-09-06")
	if code != 2 {
		t.Fatalf("exit=%d want 2\n%s%s", code, out, errOut)
	}
	for _, want := range []string{
		"# braindex diagnose: 走査の診断（2026-09-06）\n",
		"- 要確認 1 件:\n  - 保存済みの索引が無い（hub で braindex を実行して作る）\n",
		"- 索引に載る: 3 件（root 直下 3 リポのうち 1 リポ）\n",
		"  - beta: 0 件 — docs/decisions.md 無し・docs/notes 無し\n",
		"  - hub: 0 件 — docs/decisions.md 無し・docs/notes 無し\n",
		"- 状態: 無し（hub で braindex を実行して作る）\n",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("出力に %q が無い:\n%s", want, out)
		}
	}
	if !strings.Contains(errOut, "要確認 1 件(終了コード 2)") {
		t.Errorf("stderr: %s", errOut)
	}
	if _, err := os.Stat(filepath.Join(root, "hub", "index", "catalog.md")); err == nil {
		t.Errorf("診断が索引を書いている")
	}

	generate(t, cfgPath)
	before := readFile(t, filepath.Join(root, "hub", "index", "catalog.md"))
	code, out, errOut = diagnoseRun(t, "-config", cfgPath, "-date", "2026-09-06")
	if code != 0 || errOut != "" {
		t.Fatalf("exit=%d want 0\n%s%s", code, out, errOut)
	}
	if !strings.Contains(out, "## まとめ\n- 問題なし") || !strings.Contains(out, "- 状態: 生成 2026-09-01・3 件\n- 走査の記録: 読めなかった範囲なし\n- いまの走査との差: なし\n") {
		t.Errorf("出力:\n%s", out)
	}
	if got := readFile(t, filepath.Join(root, "hub", "index", "catalog.md")); got != before {
		t.Errorf("診断が索引を書き換えた")
	}
}

// 索引を作った後にノートが増減すると、未反映と「無い」に分けて 2 を返す。-catalog で索引の場所を指せる。
func TestDiagnose_索引が古い(t *testing.T) {
	root, cfgPath := diagnoseRoot(t)
	generate(t, cfgPath)
	moved := filepath.Join(root, "hub", "old-catalog.md")
	if err := os.Rename(filepath.Join(root, "hub", "index", "catalog.md"), moved); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(root, "alpha", "docs", "notes", "new.md"), "# 新\n\n記録日: 2026-09-05\n")
	if err := os.Remove(filepath.Join(root, "alpha", "docs", "notes", "a.md")); err != nil {
		t.Fatal(err)
	}
	code, out, _ := diagnoseRun(t, "-config", cfgPath, "-catalog", moved, "-date", "2026-09-06")
	if code != 2 {
		t.Fatalf("exit=%d want 2\n%s", code, out)
	}
	for _, want := range []string{
		"保存済みの索引といまの走査に差がある（未反映 1・確認不能 0・対象外 0・無い 1）",
		"- 場所: " + filepath.ToSlash(moved) + "\n",
		"  - 未反映 1 件（いま見つかるが索引に無い。再生成で載る）\n    - alpha/docs/notes/new.md\n",
		"  - 無い 1 件（索引にあるが、置き場は確認できてそのパスに無い）\n    - alpha/docs/notes/a.md\n",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("出力に %q が無い:\n%s", want, out)
		}
	}
}

// 読めなくなったディレクトリは「読めなかった範囲」に出て、索引にあった行は削除でなく確認不能になる。
func TestDiagnose_読めなかった範囲(t *testing.T) {
	root, cfgPath := diagnoseRoot(t)
	generate(t, cfgPath)
	scantest.MakeUnreadable(t, filepath.Join(root, "alpha", "docs", "notes", "common"))
	code, out, _ := diagnoseRun(t, "-config", cfgPath, "-date", "2026-09-06", "-path", "alpha/docs/notes/common/c.md")
	if code != 2 {
		t.Fatalf("exit=%d want 2\n%s", code, out)
	}
	for _, want := range []string{
		"いま走査すると読めなかった範囲が 1 件ある",
		"- 読めなかった範囲: 1 件（この範囲のノートは載らない。無いのか読めないのかは分からない）\n  - alpha/docs/notes/common/ — ",
		"  - alpha: 2 件（decisions 1・notes 1）。読めなかった範囲 1\n",
		"  - 確認不能 1 件（索引にあるが、今回読めなかった範囲の中。有無は分からない）\n    - alpha/docs/notes/common/c.md — alpha/docs/notes/common/\n",
		"## パス alpha/docs/notes/common/c.md\n- いまの設定: 対象（notes_dirs docs/notes）\n- いま走査すると: 読めなかった範囲 alpha/docs/notes/common/ の中（有無は分からない）\n- 保存済みの索引: 載っている（2026-08-03・丙）\n",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("出力に %q が無い:\n%s", want, out)
		}
	}
	if strings.Contains(out, "無い 1 件") {
		t.Errorf("読めなかった範囲の行を「無い」にしている:\n%s", out)
	}
}

// 設定を変えて走査しなくなった場所の行は「対象外」で、理由が付く。-path でその理由を問える。
func TestDiagnose_対象外(t *testing.T) {
	_, cfgPath := diagnoseRoot(t)
	generate(t, cfgPath)
	writeFile(t, cfgPath, `{"root": "..", "notes_dirs": ["wiki"]}`)
	code, out, _ := diagnoseRun(t, "-config", cfgPath, "-date", "2026-09-06", "-path", "alpha/docs/notes/a.md")
	if code != 2 {
		t.Fatalf("exit=%d want 2\n%s", code, out)
	}
	for _, want := range []string{
		"- notes_dirs: wiki\n",
		"  - alpha: 1 件（decisions 1） — wiki 無し\n",
		"  - 対象外 2 件（索引にあるが、いまの設定では走査しない場所）\n    - alpha/docs/notes/a.md — ノート置き場（wiki）にも docs/decisions.md にも extra にも無い場所\n",
		"## パス alpha/docs/notes/a.md\n- いまの設定: 対象外（ノート置き場（wiki）にも docs/decisions.md にも extra にも無い場所）\n- いま走査すると: 見つからない（置き場は確認できた。無いか、対象外）\n- 保存済みの索引: 載っている（2026-08-01・甲）\n",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("出力に %q が無い:\n%s", want, out)
		}
	}
	// 対象のパスは規則の名前が付き、いま見つかる
	_, out, _ = diagnoseRun(t, "-config", cfgPath, "-date", "2026-09-06", "-path", "alpha/docs/decisions.md")
	if !strings.Contains(out, "## パス alpha/docs/decisions.md\n- いまの設定: 対象（docs/decisions.md（決定記録））\n- いま走査すると: 見つかる（索引に載る）\n- 保存済みの索引: 載っている（2026-08-02・決定（1 件））\n") {
		t.Errorf("出力:\n%s", out)
	}
}

// -json はテキストと同じ値から作る。件数・パス・要確認の理由がテキストと一致する。
func TestDiagnose_JSONとテキストが一致(t *testing.T) {
	root, cfgPath := diagnoseRoot(t)
	generate(t, cfgPath)
	writeFile(t, filepath.Join(root, "alpha", "docs", "notes", "new.md"), "# 新\n\n記録日: 2026-09-05\n")
	writeFile(t, cfgPath, `{"root": "..", "extra": [{"repo": "beta", "path": "missing", "kind": "x"}]}`)
	code, text, _ := diagnoseRun(t, "-config", cfgPath, "-date", "2026-09-06", "-path", "beta/missing/x.md")
	jcode, js, _ := diagnoseRun(t, "-config", cfgPath, "-date", "2026-09-06", "-path", "beta/missing/x.md", "-json")
	if code != 2 || jcode != 2 {
		t.Fatalf("exit=%d/%d want 2\n%s", code, jcode, text)
	}
	var got struct {
		Date   string `json:"date"`
		Config struct {
			File      string   `json:"file"`
			Root      string   `json:"root"`
			NotesDirs []string `json:"notes_dirs"`
			Extra     []struct {
				Repo, Path, Status string
				Entries            int
			} `json:"extra"`
		} `json:"config"`
		Scan struct {
			Entries  int      `json:"entries"`
			Warnings []string `json:"warnings"`
			Repos    []struct {
				Name    string `json:"name"`
				Entries int    `json:"entries"`
			} `json:"repos"`
		} `json:"scan"`
		Saved struct {
			Status    string `json:"status"`
			Generated string `json:"generated"`
			Entries   int    `json:"entries"`
			Coverage  string `json:"coverage"`
			Diff      struct {
				NotIndexed []string `json:"not_indexed"`
				Gone       []string `json:"gone"`
			} `json:"diff"`
		} `json:"saved_index"`
		Path struct {
			Covered bool   `json:"covered"`
			Rule    string `json:"rule"`
			Scanned string `json:"scanned"`
			Indexed string `json:"indexed"`
		} `json:"path"`
		Problems []string `json:"problems"`
	}
	if err := json.Unmarshal([]byte(js), &got); err != nil {
		t.Fatalf("JSON でない: %v\n%s", err, js)
	}
	if got.Date != "2026-09-06" || got.Config.File != filepath.ToSlash(cfgPath) || got.Config.Root != filepath.ToSlash(root) || len(got.Config.NotesDirs) != 1 {
		t.Errorf("設定: %+v", got.Config)
	}
	if ex := got.Config.Extra; len(ex) != 1 || ex[0].Status != "missing" || ex[0].Entries != 0 {
		t.Errorf("extra: %+v", ex)
	}
	if got.Scan.Entries != 4 || len(got.Scan.Warnings) != 1 || len(got.Scan.Repos) != 3 || got.Scan.Repos[0].Name != "alpha" || got.Scan.Repos[0].Entries != 4 {
		t.Errorf("走査: %+v", got.Scan)
	}
	if got.Saved.Status != "ok" || got.Saved.Generated != "2026-09-01" || got.Saved.Entries != 3 || got.Saved.Coverage != "complete" || fmt.Sprint(got.Saved.Diff.NotIndexed) != "[alpha/docs/notes/new.md]" || len(got.Saved.Diff.Gone) != 0 {
		t.Errorf("保存済み: %+v", got.Saved)
	}
	if !got.Path.Covered || got.Path.Rule != "extra beta/missing" || got.Path.Scanned != "absent" || got.Path.Indexed != "no" {
		t.Errorf("パス: %+v", got.Path)
	}
	if len(got.Problems) != 2 {
		t.Errorf("要確認: %q", got.Problems)
	}
	// テキスト側の数字と一致する
	for _, want := range []string{
		fmt.Sprintf("- 要確認 %d 件:\n", len(got.Problems)),
		fmt.Sprintf("- 索引に載る: %d 件（root 直下 %d リポのうち", got.Scan.Entries, len(got.Scan.Repos)),
		fmt.Sprintf("- 警告: %d 件\n  - %s\n", len(got.Scan.Warnings), got.Scan.Warnings[0]),
		fmt.Sprintf("- 状態: 生成 %s・%d 件\n", got.Saved.Generated, got.Saved.Entries),
		"    - " + got.Saved.Diff.NotIndexed[0] + "\n",
		"- いまの設定: 対象（" + got.Path.Rule + "）\n",
		"- 保存済みの索引: 載っていない\n",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("テキストに %q が無い:\n%s", want, text)
		}
	}
	for _, p := range got.Problems {
		if !strings.Contains(text, "  - "+p+"\n") {
			t.Errorf("要確認 %q がテキストに無い", p)
		}
	}
}

// 設定の値の誤り(extra の exclude が不正など)では、索引の生成は 1 で止まるが diagnose は 2 で診断を出す:
// 読んだ設定と保存済みの索引を示し、走査できない理由を要確認に置く。索引もノートも書き換えない。
func TestDiagnose_設定の値の誤りは2で診断を出す(t *testing.T) {
	root, cfgPath := diagnoseRoot(t)
	generate(t, cfgPath)
	catalog := filepath.Join(root, "hub", "index", "catalog.md")
	before := readFile(t, catalog)
	writeFile(t, cfgPath, `{"root": "..", "extra": [{"repo": "alpha", "path": "docs", "kind": "x", "exclude": ["[a"]}]}`)
	// 索引の生成は同じ設定で 1
	var so, se bytes.Buffer
	if code := dispatch([]string{"-config", cfgPath, "-date", "2026-09-06"}, &so, &se); code != 1 {
		t.Fatalf("索引の生成: exit=%d want 1\n%s", code, se.String())
	}
	code, out, errOut := diagnoseRun(t, "-config", cfgPath, "-date", "2026-09-06")
	if code != 2 {
		t.Fatalf("exit=%d want 2\n%s%s", code, out, errOut)
	}
	for _, want := range []string{
		"- 要確認 1 件:\n  - 走査できない: extra alpha/docs: exclude のパターンが不正: \"[a\"（索引の生成も同じ理由で止まる。設定か root を直す）\n",
		"- 設定ファイル: " + filepath.ToSlash(cfgPath) + "\n",
		"- extra: 1 件\n  - alpha/docs（直下のみ・種別 x・除外 [a）: 起点あり・この起点の下で索引に載る 0 件\n",
		"## いま走査すると\n- 走査していない: extra alpha/docs: exclude のパターンが不正: \"[a\"\n",
		"- 状態: 生成 2026-09-01・3 件\n",
		"- いまの走査との差: 比べていない（走査していない）\n",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("テキストに %q が無い:\n%s", want, out)
		}
	}
	if !strings.Contains(errOut, "要確認 1 件(終了コード 2)") {
		t.Errorf("stderr: %s", errOut)
	}
	if readFile(t, catalog) != before {
		t.Error("索引が書き換わった")
	}
	// -json も同じ値から出る
	jcode, js, _ := diagnoseRun(t, "-config", cfgPath, "-date", "2026-09-06", "-json")
	var got struct {
		Scan struct {
			Failed string `json:"failed"`
		} `json:"scan"`
		Saved struct {
			Status string          `json:"status"`
			Diff   json.RawMessage `json:"diff"`
		} `json:"saved_index"`
		Problems []string `json:"problems"`
	}
	if err := json.Unmarshal([]byte(js), &got); err != nil || jcode != 2 {
		t.Fatalf("JSON: exit=%d err=%v\n%s", jcode, err, js)
	}
	if !strings.Contains(got.Scan.Failed, "exclude のパターンが不正") || got.Saved.Status != "ok" || got.Saved.Diff != nil || len(got.Problems) != 1 {
		t.Errorf("JSON: %+v", got)
	}
}

// 設定ファイルが無ければ -root が要る(索引の生成と同じ規則)。フラグの誤り・位置引数は 1。-h は 0。
func TestDiagnose_BadArgs(t *testing.T) {
	_, cfgPath := diagnoseRoot(t)
	for _, args := range [][]string{
		{"-config", cfgPath, "extra"},
		{"-config", cfgPath, "-date", "2026/09/06"},
		{"-config", filepath.Join(t.TempDir(), "none.json")},
		{"-bogus"},
	} {
		code, out, errOut := diagnoseRun(t, args...)
		if code != 1 || errOut == "" || out != "" {
			t.Errorf("%v: exit=%d stdout=%q stderr=%q", args, code, out, errOut)
		}
	}
	t.Chdir(t.TempDir())
	if code, _, errOut := diagnoseRun(t); code != 1 || !strings.Contains(errOut, "root が未指定") {
		t.Errorf("設定なし: exit=%d stderr=%s", code, errOut)
	}
	if code, _, errOut := diagnoseRun(t, "-h"); code != 0 || !strings.Contains(errOut, "使い方: braindex diagnose") {
		t.Errorf("-h: exit=%d stderr=%s", code, errOut)
	}
}

// 設定ファイルが無くても -root だけで動く(設定ファイル: 無し、索引はカレントの index/catalog.md)。
func TestDiagnose_RootOnly(t *testing.T) {
	root, _ := diagnoseRoot(t)
	t.Chdir(t.TempDir())
	code, out, _ := diagnoseRun(t, "-root", root, "-date", "2026-09-06")
	if code != 2 {
		t.Fatalf("exit=%d want 2\n%s", code, out)
	}
	if !strings.Contains(out, "- 設定ファイル: 無し（フラグだけで動作）\n") || !strings.Contains(out, "- 索引に載る: 3 件") || !strings.Contains(out, "/index/catalog.md\n- 状態: 無し") {
		t.Errorf("出力:\n%s", out)
	}
}
