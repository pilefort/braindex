package template

import (
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strings"
	"testing"
)

// hub の雛形に、骨として必要なファイルが揃っている。パスは "/" 区切りで昇順。
func TestFiles_Hub(t *testing.T) {
	files, err := Files(KindHub)
	if err != nil {
		t.Fatalf("Files: %v", err)
	}
	got := map[string]bool{}
	var paths []string
	for _, f := range files {
		got[f.Path] = true
		paths = append(paths, f.Path)
	}
	for _, want := range []string{
		"README.md", "CLAUDE.md", "braindex.json", ".gitattributes",
		"docs/overview.md", "docs/glossary.md", "docs/decisions.md", "docs/conventions.md",
		"docs/notes/common/.gitkeep", "docs/notes/project/.gitkeep",
		"work/APPROVALS.md", "work/TODO.md", "work/review/.gitkeep",
		".claude/skills/braindex-review/SKILL.md",
	} {
		if !got[want] {
			t.Errorf("雛形に無い: %s", want)
		}
	}
	if !sort.StringsAreSorted(paths) {
		t.Errorf("パスが昇順でない: %v", paths)
	}
	for _, f := range files {
		if strings.Contains(f.Path, "\\") || strings.HasPrefix(f.Path, "/") {
			t.Errorf("パスの形式が不正: %q", f.Path)
		}
	}
}

// 無い種別はエラー。
func TestFiles_UnknownKind(t *testing.T) {
	if _, err := Files(Kind("nope")); err == nil {
		t.Errorf("未対応の種別でエラーになっていない")
	}
}

// Install は全ファイルを作り、再実行では既存ファイルを上書きせず Skipped に積む。
func TestInstall_NeverOverwrites(t *testing.T) {
	dst := filepath.Join(t.TempDir(), "hub")
	res, err := Install(dst, KindHub)
	if err != nil {
		t.Fatalf("Install: %v", err)
	}
	files, err := Files(KindHub)
	if err != nil {
		t.Fatalf("Files: %v", err)
	}
	if len(res.Created) != len(files) || len(res.Skipped) != 0 {
		t.Errorf("初回: created=%d skipped=%d want %d/0", len(res.Created), len(res.Skipped), len(files))
	}
	for _, f := range files {
		b, err := os.ReadFile(filepath.Join(dst, filepath.FromSlash(f.Path)))
		if err != nil {
			t.Errorf("作られていない: %s (%v)", f.Path, err)
			continue
		}
		if string(b) != string(f.Content) {
			t.Errorf("内容が違う: %s", f.Path)
		}
	}

	// 利用者が編集したファイルは残る
	readme := filepath.Join(dst, "README.md")
	if err := os.WriteFile(readme, []byte("edited by user\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	res, err = Install(dst, KindHub)
	if err != nil {
		t.Fatalf("Install(2 回目): %v", err)
	}
	if len(res.Created) != 0 || len(res.Skipped) != len(files) {
		t.Errorf("2 回目: created=%d skipped=%d want 0/%d", len(res.Created), len(res.Skipped), len(files))
	}
	b, err := os.ReadFile(readme)
	if err != nil {
		t.Fatal(err)
	}
	if string(b) != "edited by user\n" {
		t.Errorf("既存ファイルが上書きされた: %q", b)
	}
}

// 一部だけ既に存在する場合は、無いものだけ作る。dst が無ければ作る。
func TestInstall_Partial(t *testing.T) {
	dst := filepath.Join(t.TempDir(), "new", "hub")
	if err := os.MkdirAll(filepath.Join(dst, "docs"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dst, "docs", "decisions.md"), []byte("# mine\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	res, err := Install(dst, KindHub)
	if err != nil {
		t.Fatalf("Install: %v", err)
	}
	if strings.Join(res.Skipped, ",") != "docs/decisions.md" {
		t.Errorf("skipped=%v want [docs/decisions.md]", res.Skipped)
	}
	b, err := os.ReadFile(filepath.Join(dst, "docs", "decisions.md"))
	if err != nil {
		t.Fatal(err)
	}
	if string(b) != "# mine\n" {
		t.Errorf("既存ファイルが上書きされた: %q", b)
	}
	if _, err := os.Stat(filepath.Join(dst, "index")); err == nil {
		t.Errorf("index/ は braindex 実行時に作るので雛形には含めない")
	}
}

// 既存確認(Lstat)が「無い」以外の理由で失敗したとき、エラー文に対象のパスが 2 回出ない
// (os の PathError が既にパスを持つので、包み直すと二重になる)。
func TestInstall_LstatErrorMentionsPathOnce(t *testing.T) {
	var dst string
	if runtime.GOOS == "windows" {
		// ファイル名に使えない '<' を含む展開先: Lstat が ERROR_INVALID_NAME(ErrNotExist ではない)で失敗する
		dst = filepath.Join(t.TempDir(), "a<b")
	} else {
		// docs を通常ファイルにする: docs/conventions.md の Lstat が ENOTDIR(ErrNotExist ではない)で失敗する
		dst = filepath.Join(t.TempDir(), "hub")
		if err := os.MkdirAll(dst, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dst, "docs"), []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	_, err := Install(dst, KindHub)
	if err == nil {
		t.Fatal("エラーになっていない")
	}
	if n := strings.Count(err.Error(), dst); n != 1 {
		t.Errorf("エラー文に展開先のパスが %d 回出る(1 回のはず): %v", n, err)
	}
}

// 雛形の本文は LF のみ(CRLF を持ち込まない)。絶対パス・原型固有の語を含まない。
// 個人識別子(ユーザー名・実在リポ名)そのものはこのテストにも書かない(CLAUDE.md「してはいけないこと」)。
// それらは公開前チェックリストの grep で見る。
func TestFiles_Hygiene(t *testing.T) {
	files, err := Files(KindHub)
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range files {
		s := string(f.Content)
		if strings.Contains(s, "\r") {
			t.Errorf("CRLF を含む: %s", f.Path)
		}
		for _, bad := range []string{"C:/", "C:\\", "/Users/", "/home/", "brain-review"} {
			if strings.Contains(s, bad) {
				t.Errorf("%s に %q が含まれる", f.Path, bad)
			}
		}
		// 週次レビューの記録は work/review/(2026-09-02 決定)。原型の review/ 直下を書き残さない
		if m := bareReview.FindString(s); m != "" {
			t.Errorf("%s に work/ 配下でない %q がある", f.Path, m)
		}
	}
}

// bareReview は "work/" の付かないレビュー記録のパス(review/YYYY-MM-DD.md)を見つける。
var bareReview = regexp.MustCompile(`(^|[^/])review/YYYY`)
