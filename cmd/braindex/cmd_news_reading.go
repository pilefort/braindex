package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"

	"github.com/pilefort/braindex/internal/config"
	"github.com/pilefort/braindex/internal/news"
)

// runNewsReading は保存記事を開く。回答は利用者の会話で作成したファイルを明示的に登録する。
// LLM や外部サービスを呼ばず、HTMLとデータはnewsの既存ロックの下で更新する。
func runNewsReading(args []string, stdout, stderr io.Writer) int {
	var cfgPath, id, qid, answer, ask string
	var noOpen, pending bool
	flags := flag.NewFlagSet("braindex news reading", flag.ContinueOnError)
	flags.SetOutput(stderr)
	flags.StringVar(&cfgPath, "config", defaultConfig, "hubの設定ファイル")
	flags.StringVar(&id, "id", "", "記事ID")
	flags.StringVar(&qid, "question", "", "質問ID（-idと併用。-answerが無ければ相談文を表示）")
	flags.StringVar(&answer, "answer", "", "質問への回答Markdownファイル。既存の回答は上書きしない")
	flags.StringVar(&ask, "ask", "", "記事に新しい相談を作ってから回答を登録する種類（overview / stuck / relate / try / detail）。-question の代わりに使う")
	flags.BoolVar(&pending, "pending", false, "回答が未登録の相談を一覧にする")
	flags.BoolVar(&noOpen, "no-open", false, "保存記事のHTMLを開かない")
	flags.Usage = func() {
		fmt.Fprintln(stderr, "使い方: braindex news reading [-config FILE] [-no-open] [-pending | -id ID (-question ID | -ask 種類) [-answer FILE]]")
		fmt.Fprintln(stderr, "選択と相談を先にnews applyで取り込む。回答の自動生成・外部送信はしない。")
		flags.PrintDefaults()
	}
	if err := flags.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return 0
		}
		return 1
	}
	fail := func(err error) int { fmt.Fprintln(stderr, "braindex news reading:", err); return 1 }
	if flags.NArg() != 0 || (pending && id != "") {
		return fail(errors.New("引数の組み合わせが不正（-hで使い方を表示）"))
	}
	if ask != "" {
		if id == "" || answer == "" || qid != "" {
			return fail(errors.New("-ask は -id と -answer と一緒に使う（-question は要らない）"))
		}
	} else if (id == "") != (qid == "") || (answer != "" && id == "") {
		return fail(errors.New("引数の組み合わせが不正（-hで使い方を表示）"))
	}
	fc, found, err := config.Load(cfgPath)
	if err != nil {
		return fail(err)
	}
	if !found {
		return fail(fmt.Errorf("設定ファイルが無い: %s", cfgPath))
	}
	dir := filepath.Join(filepath.Dir(cfgPath), filepath.FromSlash(fc.News.WithDefaults().Dir))
	unlock, stale, err := news.Lock(dir, "reading")
	if err != nil {
		return fail(err)
	}
	defer unlock()
	if stale != "" {
		fmt.Fprintln(stderr, "警告:", stale)
	}
	lib, err := news.LoadReading(dir)
	if err != nil {
		return fail(err)
	}
	if pending {
		ids := make([]string, 0, len(lib.Articles))
		for id := range lib.Articles {
			ids = append(ids, id)
		}
		sort.Strings(ids)
		n := 0
		for _, id := range ids {
			a := lib.Articles[id]
			for _, q := range a.Questions {
				if q.Answer == "" {
					fmt.Fprintf(stdout, "記事 %s / 質問 %s: %s — %s\n", id, q.ID, a.Title, q.Text)
					n++
				}
			}
		}
		fmt.Fprintf(stdout, "回答未登録: %d 件\n", n)
		return 0
	}
	if id != "" && answer == "" {
		p, err := lib.Prompt(id, qid)
		if err != nil {
			return fail(err)
		}
		fmt.Fprint(stdout, p)
		return 0
	}
	if ask != "" {
		qid, err = lib.Ask(id, ask)
		if err != nil {
			return fail(err)
		}
	}
	if answer != "" {
		b, err := os.ReadFile(answer)
		if err != nil {
			return fail(err)
		}
		if err = lib.Answer(id, qid, string(b)); err != nil {
			return fail(err)
		}
		if err = lib.Save(dir); err != nil {
			return fail(err)
		}
	}
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fail(err)
	}
	if err := news.WriteReading(dir, lib); err != nil {
		return fail(err)
	}
	path := filepath.Join(dir, news.ReadingHTML)
	fmt.Fprintln(stdout, "保存記事の一覧:", path)
	if !noOpen {
		if err := openInBrowser(path); err != nil {
			return fail(err)
		}
	}
	return 0
}
