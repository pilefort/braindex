package main

import (
	"bytes"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"

	"github.com/pilefort/braindex/internal/retro"
)

func execRetroExtract(t *testing.T, args ...string) (code int, stdout, stderr string) {
	t.Helper()
	var so, se bytes.Buffer
	code = dispatch(append([]string{"retro", "extract"}, args...), &so, &se)
	return code, so.String(), se.String()
}

// -out にセッションごとの md と index.tsv を書く。要約は 1 行。架空ログには JSON でない行があるので 2。
func TestRetroExtract_Out(t *testing.T) {
	fixUTC(t)
	out := filepath.Join(t.TempDir(), "retro")
	code, so, se := execRetroExtract(t, "-sessions", retroTestdata, "-out", out)
	if code != 2 {
		t.Fatalf("exit=%d want 2\nstdout=%s\nstderr=%s", code, so, se)
	}
	if want := "braindex retro extract: 3 セッション・発話 6・訂正 1 → " + out + "\n"; so != want {
		t.Errorf("要約:\n want=%q\n  got=%q", want, so)
	}
	idx, err := os.ReadFile(filepath.Join(out, "index.tsv"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(string(idx), "start\tproject\tsession\tuser_turns\tcorrections\tfile\n") || !strings.Contains(string(idx), "2026-08-20 01:00\t/work/repo-a\taaaa1111\t3\t1\tsessions/_work_repo-a/20260820_0100_aaaa1111.md\n") {
		t.Errorf("index.tsv: %q", string(idx))
	}
	md, err := os.ReadFile(filepath.Join(out, "sessions", "_work_repo-a", "20260820_0100_aaaa1111.md"))
	if err != nil {
		t.Fatal(err)
	}
	s := string(md)
	// 直前のアシスタント本文は、前の発話以降の 2 発話を " / " でつないだ 1 行(時刻は最後の発話)。ツール名と回数を添える
	if !strings.Contains(s, "### [08-20 01:02] #2 USER ★ 訂正候補: 違う, そうじゃな, じゃなくて\n← ASSISTANT [08-20 01:01]: はい、作ります。 / できました。 [tools: Read×2, Bash]\n違う、そうじゃなくて docs だけ\n") {
		t.Errorf("ダイジェストの発話: %q", s)
	}
	// system-reminder の中身は本文に出ない
	if strings.Contains(s, "これは注意書き") {
		t.Errorf("reminder が混じる: %q", s)
	}
	if strings.Contains(s, "\r\n") {
		t.Error("CRLF が混じる")
	}
}

// 既定の出力先は OS の一時ディレクトリの braindex-retro(リポには書かない)。
// テストは OS の一時ディレクトリをテスト専用の場所に差し替える(利用者が実ログから書き出した既定の置き場を、架空ログで上書きしないため)。
func TestRetroExtract_DefaultOut(t *testing.T) {
	fixUTC(t)
	tmp := t.TempDir()
	for _, k := range []string{"TMPDIR", "TMP", "TEMP"} { // Unix は TMPDIR、Windows は TMP → TEMP の順に見る
		t.Setenv(k, tmp)
	}
	if got := os.TempDir(); got != tmp {
		t.Fatalf("os.TempDir() が差し替わらない: want=%q got=%q", tmp, got)
	}
	code, so, _ := execRetroExtract(t, "-sessions", retroTestdata, "-since", "2026-08-15")
	if code != 2 {
		t.Fatalf("exit=%d want 2\n%s", code, so)
	}
	want := "braindex retro extract: 1 セッション・発話 3・訂正 1 → " + filepath.Join(tmp, "braindex-retro") + "\n"
	if so != want {
		t.Errorf("要約:\n want=%q\n  got=%q", want, so)
	}
	if _, err := os.Stat(filepath.Join(tmp, "braindex-retro", "index.tsv")); err != nil {
		t.Errorf("テスト専用の一時ディレクトリに index.tsv が無い: %v", err)
	}
}

// 2 回書いても同じ内容(決定性)。
func TestRetroExtract_Deterministic(t *testing.T) {
	fixUTC(t)
	out1, out2 := filepath.Join(t.TempDir(), "a"), filepath.Join(t.TempDir(), "b")
	execRetroExtract(t, "-sessions", retroTestdata, "-out", out1)
	execRetroExtract(t, "-sessions", retroTestdata, "-out", out2)
	for _, rel := range []string{"index.tsv", filepath.Join("sessions", "_work_repo-a", "20260820_0100_aaaa1111.md")} {
		a, err1 := os.ReadFile(filepath.Join(out1, rel))
		b, err2 := os.ReadFile(filepath.Join(out2, rel))
		if err1 != nil || err2 != nil || !bytes.Equal(a, b) {
			t.Errorf("%s: 2 回の出力が一致しない(err=%v/%v)", rel, err1, err2)
		}
	}
}

func TestRetroExtract_Errors(t *testing.T) {
	fixUTC(t)
	cases := [][]string{
		{"-sessions", retroTestdata, "-since", "2026-08-15", "-window-days", "7"},
		{"-sessions", retroTestdata, "-since", "bad"},
		{"-sessions", filepath.Join(t.TempDir(), "no-such-dir")},
		{"-sessions", retroTestdata, "extra-arg"},
	}
	for _, args := range cases {
		code, so, se := execRetroExtract(t, args...)
		if code != 1 {
			t.Errorf("%v: exit=%d want 1\nstdout=%s\nstderr=%s", args, code, so, se)
		}
		if !strings.Contains(se, "braindex retro extract") {
			t.Errorf("%v: stderr にコマンド名が無い: %q", args, se)
		}
	}
	var so, se bytes.Buffer
	if code := dispatch([]string{"retro", "extract", "-h"}, &so, &se); code != 0 || !strings.Contains(se.String(), "一時ディレクトリ") {
		t.Errorf("-h: exit=%d stderr=%q", code, se.String())
	}
}

// 同じ出力先に書き直すと、前回の sessions/ と index.tsv は消えて今回の窓の分だけになる。出力先の他のファイルは触らない。
func TestRetroExtract_Rewrite(t *testing.T) {
	fixUTC(t)
	out := filepath.Join(t.TempDir(), "retro")
	if code, so, se := execRetroExtract(t, "-sessions", retroTestdata, "-out", out); code != 2 {
		t.Fatalf("1 回目: exit=%d want 2\nstdout=%s\nstderr=%s", code, so, se)
	}
	keep := filepath.Join(out, "keep.txt")
	if err := os.WriteFile(keep, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if code, so, se := execRetroExtract(t, "-sessions", retroTestdata, "-out", out, "-since", "2026-08-15"); code != 2 {
		t.Fatalf("2 回目: exit=%d want 2\nstdout=%s\nstderr=%s", code, so, se)
	}
	var files []string
	err := filepath.WalkDir(filepath.Join(out, "sessions"), func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() {
			rel, _ := filepath.Rel(out, p)
			files = append(files, filepath.ToSlash(rel))
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"sessions/_work_repo-a/20260820_0100_aaaa1111.md"}; !reflect.DeepEqual(files, want) {
		t.Errorf("2 回目の後に残る md: want=%v got=%v", want, files)
	}
	idx, err := os.ReadFile(filepath.Join(out, "index.tsv"))
	if err != nil {
		t.Fatal(err)
	}
	if lines := strings.Count(string(idx), "\n"); lines != 2 {
		t.Errorf("index.tsv は見出し + 今回の 1 行のはず: %q", string(idx))
	}
	if _, err := os.Stat(keep); err != nil {
		t.Errorf("出力先の他のファイルが消えた: %v", err)
	}
}

// ダイジェストと index.tsv の書き込みは、置き換えに失敗しても既にあるファイルを壊さない(半端な内容で上書きしない)。
// index.tsv を次に読むのはレトロスペクティブの手順で、半端な索引は黙って途中までしか辿れない(設計レビュー 2026-09-06 M14)。
// コマンド全体は前回の出力を先に消すので、ここでは書き出しの段(writeExtractOutput)だけを見る。
func TestRetroExtract_書き込みに失敗しても既にあるファイルは壊れない(t *testing.T) {
	for _, target := range []string{"index.tsv", "sessions/p/a.md"} {
		t.Run(target, func(t *testing.T) {
			out := t.TempDir()
			old := retro.Result{Index: []byte("前回の索引\n"), Files: []retro.DigestFile{{RelPath: "sessions/p/a.md", Content: []byte("前回のダイジェスト\n")}}}
			if err := writeExtractOutput(out, old); err != nil {
				t.Fatal(err)
			}
			path := filepath.Join(out, filepath.FromSlash(target))
			before, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			blockReplace(t, path)

			cur := retro.Result{Index: []byte("今回の索引\n"), Files: []retro.DigestFile{{RelPath: "sessions/p/a.md", Content: []byte("今回のダイジェスト\n")}}}
			if err := writeExtractOutput(out, cur); err == nil {
				t.Fatal("エラーにならない")
			}
			if after, _ := os.ReadFile(path); !bytes.Equal(before, after) {
				t.Errorf("元のファイルが変わった: %q", after)
			}
			des, err := os.ReadDir(filepath.Dir(path))
			if err != nil {
				t.Fatal(err)
			}
			for _, de := range des {
				if strings.HasPrefix(de.Name(), ".") {
					t.Errorf("一時ファイルが残った: %s", de.Name())
				}
			}
		})
	}
}

// blockReplace は path を「原子的には書き換えられない」状態にする。path そのものは書けるので、
// 切り詰めてから書く os.WriteFile は成功して前回の内容を失い、一時ファイル経由の置き換えは失敗して前回の内容が残る。
// Windows: path を開いたままにする(Go の os.Open は FILE_SHARE_DELETE を付けないので、置き換えと削除が失敗する)。
// それ以外: 親ディレクトリの書き込み権限を外す(一時ファイルを作れない。root は権限を無視するので skip)。
func blockReplace(t *testing.T, path string) {
	t.Helper()
	if runtime.GOOS == "windows" {
		f, err := os.Open(path)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { f.Close() })
		return
	}
	if os.Getuid() == 0 {
		t.Skip("root は権限を無視するので、書けない置き場を作れない")
	}
	dir := filepath.Dir(path)
	if err := os.Chmod(dir, 0o555); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chmod(dir, 0o755) })
}

// ダイジェストと index.tsv は本人だけが読める権限で書く(共有の /tmp を持つ環境で会話の本文を他のユーザに見せない)。
// Windows は POSIX の権限ビットがほぼ効かないので見ない。
func TestRetroExtract_権限は本人だけ(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows では POSIX の権限ビットがほぼ効かない")
	}
	fixUTC(t)
	out := filepath.Join(t.TempDir(), "retro")
	if code, so, se := execRetroExtract(t, "-sessions", retroTestdata, "-out", out); code != 2 {
		t.Fatalf("exit=%d want 2\nstdout=%s\nstderr=%s", code, so, se)
	}
	for _, c := range []struct {
		path string
		want os.FileMode
	}{
		{out, 0o700},
		{filepath.Join(out, "index.tsv"), 0o600},
		{filepath.Join(out, "sessions", "_work_repo-a"), 0o700},
		{filepath.Join(out, "sessions", "_work_repo-a", "20260820_0100_aaaa1111.md"), 0o600},
	} {
		fi, err := os.Stat(c.path)
		if err != nil {
			t.Fatal(err)
		}
		if got := fi.Mode().Perm(); got != c.want {
			t.Errorf("%s: perm=%04o want %04o", c.path, got, c.want)
		}
	}
}
