package main

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// 最初の引数が登録済みのサブコマンド名なら、そのコマンドに残りの引数を渡す。
func TestDispatch_Subcommand(t *testing.T) {
	var gotArgs []string
	register(&command{name: "zz-test", summary: "テスト用", run: func(args []string, stdout, stderr io.Writer) int {
		gotArgs = args
		io.WriteString(stdout, "ran\n")
		return 7
	}})
	t.Cleanup(func() { delete(commands, "zz-test") })

	var so, se bytes.Buffer
	if code := dispatch([]string{"zz-test", "a", "-b"}, &so, &se); code != 7 {
		t.Errorf("exit=%d want 7", code)
	}
	if strings.Join(gotArgs, " ") != "a -b" || so.String() != "ran\n" {
		t.Errorf("引数の受け渡しが不正: args=%v stdout=%q", gotArgs, so.String())
	}
}

// 未知のサブコマンド名は終了コード 1 で、stderr にその名前を出す。
func TestDispatch_UnknownCommand(t *testing.T) {
	var so, se bytes.Buffer
	if code := dispatch([]string{"nope"}, &so, &se); code != 1 {
		t.Errorf("exit=%d want 1", code)
	}
	if !strings.Contains(se.String(), "nope") {
		t.Errorf("stderr にコマンド名が無い: %s", se.String())
	}
}

// 引数無し(フラグのみ)は従来どおり索引生成。-h は使い方を出して 0。不正なフラグ・余分な引数は 1。
func TestDispatch_IndexFlags(t *testing.T) {
	root := makeRoot(t)
	out := filepath.Join(t.TempDir(), "catalog.md")
	var so, se bytes.Buffer
	if code := dispatch([]string{"-root", root, "-out", out, "-date", "2026-01-03"}, &so, &se); code != 0 {
		t.Fatalf("exit=%d want 0\nstderr=%s", code, se.String())
	}
	if _, err := os.Stat(out); err != nil {
		t.Errorf("索引が書かれていない: %v", err)
	}

	so.Reset()
	se.Reset()
	if code := dispatch([]string{"-h"}, &so, &se); code != 0 {
		t.Errorf("-h の exit=%d want 0", code)
	}
	if !strings.Contains(se.String(), "使い方:") || !strings.Contains(se.String(), "-root") {
		t.Errorf("使い方が出ていない: %s", se.String())
	}

	se.Reset()
	if code := dispatch([]string{"-no-such-flag"}, &so, &se); code != 1 {
		t.Errorf("不正なフラグの exit=%d want 1", code)
	}
	se.Reset()
	if code := dispatch([]string{"-root", root, "extra-arg"}, &so, &se); code != 1 {
		t.Errorf("余分な引数の exit=%d want 1", code)
	}
}

// -h の一覧に登録済みのサブコマンドが名前順で出る。
func TestDispatch_HelpListsCommands(t *testing.T) {
	register(&command{name: "zz-b", summary: "2 番目", run: func([]string, io.Writer, io.Writer) int { return 0 }})
	register(&command{name: "zz-a", summary: "1 番目", run: func([]string, io.Writer, io.Writer) int { return 0 }})
	t.Cleanup(func() { delete(commands, "zz-a"); delete(commands, "zz-b") })
	var so, se bytes.Buffer
	dispatch([]string{"-h"}, &so, &se)
	s := se.String()
	ia, ib := strings.Index(s, "zz-a"), strings.Index(s, "zz-b")
	if ia < 0 || ib < 0 || ia > ib {
		t.Errorf("コマンドの一覧が名前順でない: %s", s)
	}
}
