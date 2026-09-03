package main

import (
	"fmt"
	"io"
	"sort"
)

// command はサブコマンド(braindex <name> [args])。
//
// 各サブコマンドは自分の cmd_<name>.go の init() で register する。索引生成(引数無し・フラグのみ)は
// サブコマンドではなく main.go の run が担う。後続フェーズ(review / retro / news)も同じ形で足す。
type command struct {
	name    string
	summary string // -h の一覧に出す 1 行
	run     func(args []string, stdout, stderr io.Writer) int
}

var commands = map[string]*command{}

// register はサブコマンドを登録する。同名の二重登録はプログラムの誤りなので panic。
func register(c *command) {
	if _, dup := commands[c.name]; dup {
		panic("braindex: コマンドの二重登録 " + c.name)
	}
	commands[c.name] = c
}

// commandNames は登録済みのサブコマンド名を昇順で返す(一覧の表示順を決定的にする)。
func commandNames() []string {
	names := make([]string, 0, len(commands))
	for n := range commands {
		names = append(names, n)
	}
	sort.Strings(names)
	return names
}

// printCommands はサブコマンドの一覧を書く。
// 桁は一番長い名前に合わせる。固定幅にしていると、その幅を超える名前を足したとき
// その行だけ説明の開始位置がずれる(approvals が入って実際にずれた)。
func printCommands(w io.Writer) {
	names := commandNames()
	width := 0
	for _, n := range names {
		if len(n) > width {
			width = len(n)
		}
	}
	for _, n := range names {
		fmt.Fprintf(w, "  %-*s %s\n", width, n, commands[n].summary)
	}
}
