package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/pilefort/braindex/internal/news"
)

// cliFake は claude CLI の代わり。渡されたプロンプトを記録し、決めた応答を返す。
type cliFake struct {
	prompts []string
	reply   string
	err     error
}

func (f *cliFake) Annotate(_ context.Context, prompt string) (string, error) {
	f.prompts = append(f.prompts, prompt)
	return f.reply, f.err
}

// annotatorFunc は関数を news.Annotator にする。
type annotatorFunc func(context.Context, string) (string, error)

func (f annotatorFunc) Annotate(ctx context.Context, p string) (string, error) { return f(ctx, p) }

func useFakeCLI(t *testing.T, f *cliFake) {
	t.Helper()
	orig := newNewsAnnotator
	newNewsAnnotator = func(news.Settings) (news.Annotator, error) { return f, nil }
	t.Cleanup(func() { newNewsAnnotator = orig })
}

func llmFetch(t *testing.T, hub string, args ...string) (code int, so, se string) {
	t.Helper()
	var sob, seb bytes.Buffer
	code = dispatch(append([]string{"news", "fetch", "-config", filepath.Join(hub, "braindex.json"), "-date", "2026-08-15", "-layer", "daily",
		"-stdout", "-sessions", filepath.Join(hub, "no-such-dir")}, args...), &sob, &seb)
	return code, sob.String(), seb.String()
}

// 既定(llm 未設定)では LLM を 1 回も呼ばない。
func TestNewsFetch_LLM既定は呼ばない(t *testing.T) {
	hub, _ := newsHub(t)
	f := &cliFake{reply: `[]`}
	useFakeCLI(t, f)
	code, _, se := llmFetch(t, hub, "-no-score")
	if code != 2 { // 取得失敗 1 本(C)の警告
		t.Fatalf("exit=%d\n%s", code, se)
	}
	if len(f.prompts) != 0 {
		t.Errorf("llm=off なのに %d 回呼んだ", len(f.prompts))
	}
}

// llm=claude-cli: 見出し・概要・プロファイルの語だけを渡し、応答の関心度で語の点を上書きし、訳を添える。キャッシュに残す。
// -no-llm で止まる。
func TestNewsFetch_LLM(t *testing.T) {
	hub, _ := newsHub(t)
	// serendipity は 0。ここは LLM の採点と訳の検査で、関心外からの拾い上げが混ざると見分けにくい
	writeFile(t, filepath.Join(hub, "braindex.json"), `{"root": "..", "news": {"llm": "claude-cli", "llm_model": "m1", "serendipity": 0}}`)
	writeFile(t, filepath.Join(hub, "news", "interests.md"), "ゴルーチン\n") // 記事1 の概要に当たる(語の点 2)
	// 記事1 は LLM が 0(語の点 2 を下げる)、記事2 は 3 に訳つき
	f := &cliFake{reply: `[{"id":"__ID1__","t":"","s":"","r":0},{"id":"__ID2__","t":"記事二の訳","s":"","r":3}]`}
	useFakeCLI(t, f)
	// 記事 ID はリンクの SHA なので、まず -no-llm 無しで 1 回走らせて ID を拾う…のは回りくどい。応答は ID を後から埋める
	ids := map[string]string{}
	newNewsAnnotator = func(news.Settings) (news.Annotator, error) {
		return annotatorFunc(func(ctx context.Context, p string) (string, error) {
			f.prompts = append(f.prompts, p)
			for _, l := range strings.Split(p, "\n") {
				if strings.HasPrefix(l, "[{") {
					// 採点対象の JSON から id を取り出す(順は記事の記載順)
					var got []map[string]string
					if err := json.Unmarshal([]byte(l), &got); err != nil {
						return "", err
					}
					for _, g := range got {
						ids[g["t"]] = g["id"]
					}
				}
			}
			r := strings.NewReplacer("__ID1__", ids["記事1"], "__ID2__", ids["記事2"]).Replace(f.reply)
			return r, nil
		}), nil
	}
	code, so, se := llmFetch(t, hub)
	if code != 2 {
		t.Fatalf("exit=%d\n%s%s", code, so, se)
	}
	// 記事1 は訳が返らないので、その 1 件だけをもう一度聞く(2 回目も空なら以後は聞かない)。
	if len(f.prompts) != 2 {
		t.Fatalf("呼び出し %d 回 want 2(最初の 1 回＋記事1 の聞き直し)\n%s", len(f.prompts), se)
	}
	p := f.prompts[0]
	mustContain(t, "prompt", p, "記事1", "ゴルーチン の話", `"lang":"en"`, "## 関心プロファイル", "ゴルーチン")
	if strings.Contains(p, "example.com") {
		t.Errorf("リンクをプロンプトに載せている(不要な情報)")
	}
	mustContain(t, "stderr", se, "LLM 補助: 2 件を聞いて 2 件に注釈・訳が返らず 1 件を聞き直し")
	// 記事2 が 3(LLM)で主要、記事1 は 0(LLM)で関心外。訳が添えられる
	mustContain(t, "stdout", so, "[記事2](https://example.com/2) ★3（LLM）／訳: 記事二の訳", "関心外と判定 1 件:", "[記事1](https://example.com/1) ★0（LLM・ゴルーチン）")
	if _, err := os.Stat(filepath.Join(hub, "news", news.LLMCacheFile)); err != nil {
		t.Errorf("キャッシュが書かれていない: %v", err)
	}

	// 2 回目(-replay で既読を無視)はキャッシュから出すので呼ばない
	f.prompts = nil
	code, so, _ = llmFetch(t, hub, "-replay")
	if code != 2 || len(f.prompts) != 0 {
		t.Errorf("2 回目: exit=%d 呼び出し %d 回(キャッシュで済ませたい)", code, len(f.prompts))
	}
	mustContain(t, "stdout", so, "訳: 記事二の訳")

	// -no-llm: 呼ばず、語の点だけ(記事1 が 2)
	os.Remove(filepath.Join(hub, "news", news.LLMCacheFile))
	f.prompts = nil
	code, so, _ = llmFetch(t, hub, "-replay", "-no-llm")
	if code != 2 || len(f.prompts) != 0 {
		t.Errorf("-no-llm: exit=%d 呼び出し %d 回", code, len(f.prompts))
	}
	mustContain(t, "stdout", so, "[記事1](https://example.com/1) ★2（ゴルーチン）")
	if strings.Contains(so, "訳:") {
		t.Errorf("-no-llm で訳が出ている")
	}
}

// CLI が無い・失敗したときは警告(終了コード 2)にして、語の点で出す(フォールバック)。
func TestNewsFetch_LLMフォールバック(t *testing.T) {
	hub, _ := newsHub(t)
	writeFile(t, filepath.Join(hub, "braindex.json"), `{"root": "..", "news": {"llm": "claude-cli"}}`)
	writeFile(t, filepath.Join(hub, "news", "interests.md"), "ゴルーチン\n")

	// CLI が見つからない
	orig := newNewsAnnotator
	newNewsAnnotator = func(news.Settings) (news.Annotator, error) { return nil, news.ErrNoClaudeCLI }
	t.Cleanup(func() { newNewsAnnotator = orig })
	code, so, se := llmFetch(t, hub)
	if code != 2 {
		t.Fatalf("exit=%d\n%s", code, se)
	}
	mustContain(t, "stderr", se, "警告: LLM 補助", "claude CLI が見つからない", "語の一致の点で続ける")
	mustContain(t, "stdout", so, "[記事1](https://example.com/1) ★2（ゴルーチン）")

	// 呼び出しが失敗
	useFakeCLI(t, &cliFake{err: errors.New("timeout")})
	code, so, se = llmFetch(t, hub, "-replay")
	if code != 2 {
		t.Fatalf("exit=%d\n%s", code, se)
	}
	mustContain(t, "stderr", se, "警告: LLM 補助", "timeout", "1 バッチ失敗")
	mustContain(t, "stdout", so, "★2（ゴルーチン）")
}

// 設定の値が違えば終了コード 1。
func TestNewsFetch_LLM設定の誤り(t *testing.T) {
	hub, _ := newsHub(t)
	writeFile(t, filepath.Join(hub, "braindex.json"), `{"root": "..", "news": {"llm": "gpt"}}`)
	code, _, se := newsFetch(t, hub)
	if code != 1 {
		t.Fatalf("exit=%d\n%s", code, se)
	}
	mustContain(t, "stderr", se, "news.llm", "claude-cli")
}

// レビュー #91-1: -no-score のときは LLM も呼ばない。
func TestNewsFetch_NoScoreはLLMも呼ばない(t *testing.T) {
	hub, _ := newsHub(t)
	writeFile(t, filepath.Join(hub, "braindex.json"), `{"root": "..", "news": {"llm": "claude-cli"}}`)
	f := &cliFake{reply: `[]`}
	useFakeCLI(t, f)
	code, _, se := llmFetch(t, hub, "-no-score")
	if code != 2 {
		t.Fatalf("exit=%d\n%s", code, se)
	}
	if len(f.prompts) != 0 {
		t.Errorf("-no-score なのに LLM を %d 回呼んだ", len(f.prompts))
	}
}

// レビュー #91-3: claude が無くても、読み込んだキャッシュの訳と点は効かせる。
func TestNewsFetch_LLM無しでもキャッシュは効く(t *testing.T) {
	hub, _ := newsHub(t)
	writeFile(t, filepath.Join(hub, "braindex.json"), `{"root": "..", "news": {"llm": "claude-cli"}}`)
	writeFile(t, filepath.Join(hub, "news", "interests.md"), "ゴルーチン\n")
	// 記事2 の ID を拾うために 1 回、fake で走らせてキャッシュを作る
	ids := map[string]string{}
	orig := newNewsAnnotator
	t.Cleanup(func() { newNewsAnnotator = orig })
	newNewsAnnotator = func(news.Settings) (news.Annotator, error) {
		return annotatorFunc(func(_ context.Context, p string) (string, error) {
			for _, l := range strings.Split(p, "\n") {
				if strings.HasPrefix(l, "[{") {
					var got []map[string]string
					json.Unmarshal([]byte(l), &got)
					for _, g := range got {
						ids[g["t"]] = g["id"]
					}
				}
			}
			return `[{"id":"` + ids["記事2"] + `","t":"記事二の訳","s":"","r":3}]`, nil
		}), nil
	}
	if code, so, se := llmFetch(t, hub); code != 2 {
		t.Fatalf("exit=%d\n%s%s", code, so, se)
	}
	newNewsAnnotator = func(news.Settings) (news.Annotator, error) { return nil, news.ErrNoClaudeCLI }
	code, so, se := llmFetch(t, hub, "-replay")
	if code != 2 {
		t.Fatalf("exit=%d\n%s", code, se)
	}
	mustContain(t, "stderr", se, "claude CLI が見つからない")
	mustContain(t, "stdout", so, "[記事2](https://example.com/2) ★3（LLM）／訳: 記事二の訳")
}

// 不要ばかり付く取材先の上限(決定 2026-09-06)は、LLM の採点でも外れない。
// 外れると「上限を 1 に下げた」と言いながら 3 で主要表示に出る(外部レビュー 2026-09-12)。
func TestNewsFetch_LLMの点でも下げた取材先の上限は効く(t *testing.T) {
	hub, _ := newsHub(t)
	writeFile(t, filepath.Join(hub, "braindex.json"), `{"root": "..", "news": {"llm": "claude-cli", "serendipity": 0}}`)
	writeFile(t, filepath.Join(hub, "news", "interests.md"), "ゴルーチン\n")
	// A は不要率 10/11 = 91%(DemoteMinJudged 10 件以上・DemoteDropRate 80% 超)なので下げる取材先
	writeFile(t, filepath.Join(hub, "news", ".stats.json"),
		`{"digests": {"digest_2026-08-14_daily.md": {"A": {"shown": 11, "kept": 1, "dropped": 10, "hidden": 0, "rescued": 0}}}}`)

	ids := map[string]string{}
	newNewsAnnotator = func(news.Settings) (news.Annotator, error) {
		return annotatorFunc(func(_ context.Context, p string) (string, error) {
			for _, l := range strings.Split(p, "\n") {
				if strings.HasPrefix(l, "[{") {
					var got []map[string]string
					if err := json.Unmarshal([]byte(l), &got); err != nil {
						return "", err
					}
					for _, g := range got {
						ids[g["t"]] = g["id"]
					}
				}
			}
			// LLM は両方を 3(最高)と採点する
			return `[{"id":"` + ids["記事1"] + `","t":"訳1","s":"","r":3},{"id":"` + ids["記事2"] + `","t":"訳2","s":"","r":3}]`, nil
		}), nil
	}
	t.Cleanup(func() { newNewsAnnotator = nil })

	code, so, se := llmFetch(t, hub)
	if code != 2 { // 取得失敗 1 本(C)の警告
		t.Fatalf("exit=%d\n%s%s", code, so, se)
	}
	mustContain(t, "stdout", so, "不要が多い取材先 1 本は関心度の上限を 1 に下げた",
		"[記事1](https://example.com/1) ★1（LLM・ゴルーチン）", "[記事2](https://example.com/2) ★1（LLM）")
	if strings.Contains(so, "★3") {
		t.Errorf("下げた取材先の記事が LLM の点で戻った:\n%s", so)
	}
}
