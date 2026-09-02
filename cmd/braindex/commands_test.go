package main

import (
	"io"
	"testing"
)

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
