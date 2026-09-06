package main

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// 登録済みのサブコマンドは README のコマンドの表と manual/README.md の目次に全部載っている。
// コマンドを足したときに共有文書を書き忘れると、利用者は -h でしかその存在を知れない
// (2026-09-06 の統合で search・diagnose の 2 つを後から書いた)。
func TestCommands_文書に載っている(t *testing.T) {
	readme, err := os.ReadFile(filepath.Join("..", "..", "README.md"))
	if err != nil {
		t.Fatal(err)
	}
	manual, err := os.ReadFile(filepath.Join("..", "..", "manual", "README.md"))
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range commandNames() {
		if strings.HasPrefix(name, "zz-") { // 他のテストが一時的に登録する名前
			continue
		}
		if !bytes.Contains(readme, []byte("| `braindex "+name+"` |")) {
			t.Errorf("README.md のコマンドの表に `braindex %s` の行が無い", name)
		}
		// manual の目次は「`braindex approvals`・`answer`・`verify`」のように 2 つ目以降を短く書く
		if !bytes.Contains(manual, []byte("`braindex "+name+"`")) && !bytes.Contains(manual, []byte("`"+name+"`")) {
			t.Errorf("manual/README.md の目次に %s が無い", name)
		}
	}
}

// 一覧は名前の長さが違っても説明の開始位置を揃える(桁を固定していると長い名前の行だけずれる)。
func TestPrintCommands_桁が揃う(t *testing.T) {
	run := func([]string, io.Writer, io.Writer) int { return 0 }
	register(&command{name: "zz-very-long-name", summary: "長い名前", run: run})
	t.Cleanup(func() { delete(commands, "zz-very-long-name") })

	var b bytes.Buffer
	printCommands(&b)
	lines := strings.Split(strings.TrimRight(b.String(), "\n"), "\n")
	if len(lines) < 2 {
		t.Fatalf("コマンドが 2 つ以上あるはず:\n%s", b.String())
	}
	// 説明の開始位置(名前の後ろの空白を飛ばした桁)が全行で同じか
	col := func(l string) int {
		name := strings.Fields(l)[0]
		i := strings.Index(l, name) + len(name)
		for i < len(l) && l[i] == ' ' {
			i++
		}
		return i
	}
	want := col(lines[0])
	for _, l := range lines {
		if got := col(l); got != want {
			t.Errorf("説明の開始位置が揃っていない(%d 桁目・他は %d 桁目):\n%s", got, want, b.String())
		}
	}
	// 一番長い名前の行でも、名前と説明がくっつかない
	for _, l := range lines {
		if strings.Contains(l, "zz-very-long-name 長い名前") {
			return
		}
	}
	t.Errorf("長い名前の行に空白が無い:\n%s", b.String())
}

// 同名のサブコマンドを二重に register するとプログラムの誤りとして panic し、先の登録は残る。
func TestRegister_DuplicatePanics(t *testing.T) {
	run := func([]string, io.Writer, io.Writer) int { return 0 }
	register(&command{name: "zz-dup", summary: "1 回目", run: run})
	t.Cleanup(func() { delete(commands, "zz-dup") })
	defer func() {
		if recover() == nil {
			t.Errorf("二重登録で panic しない")
		}
		if c := commands["zz-dup"]; c == nil || c.summary != "1 回目" {
			t.Errorf("二重登録で先の登録が上書きされた: %+v", c)
		}
	}()
	register(&command{name: "zz-dup", summary: "2 回目", run: run})
}
