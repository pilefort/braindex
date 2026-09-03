package main

import (
	"bytes"
	"io"
	"strings"
	"testing"
)

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
