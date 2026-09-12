package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/pilefort/braindex/internal/news"
)

// 中断からの立て直し(設計レビュー補足 2026-09-06「処理単位の復旧」)。
// fetch は md → html → 既読 の順に別ファイルを書くので、途中で止まると「md はあるのに既読が進んでいない」
// のような食い違いが残る。書き始める前に news/.pending.json へ記録し、書き終えたら消す。
// 次の fetch は、その記録にある出力先なら「既にある」で止めずに書き直して既読まで進める。

// 保存の途中で止めてから、同じ日を再実行すると完了する。
// 止め方: 既読は保存だけを失敗させる(差し替え点)。html は書く先の場所にディレクトリを置く(置き換えが失敗する)。
func TestNewsFetch_途中で止まった同じ日は再実行で完了する(t *testing.T) {
	for _, tc := range []struct {
		name  string
		stop  func(t *testing.T, newsDir string) (undo func())
		wrote []string // 止まった時点で書けているファイル
	}{
		{"既読を書けない(md と html は書けた後)", func(t *testing.T, _ string) func() {
			orig := saveSeen
			saveSeen = func(news.Seen, string) error { return errors.New("既読を書けない(注入)") }
			return func() { saveSeen = orig }
		}, []string{"digest_2026-08-15_weekly.md", "digest_2026-08-15_weekly.html"}},
		{"選別 UI を書けない(md だけ書けた後)", func(t *testing.T, newsDir string) func() {
			blocked := filepath.Join(newsDir, "digest_2026-08-15_weekly.html")
			if err := os.MkdirAll(blocked, 0o755); err != nil {
				t.Fatal(err)
			}
			return func() { os.Remove(blocked) }
		}, []string{"digest_2026-08-15_weekly.md"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			hub, _ := newsHub(t)
			newsDir := filepath.Join(hub, "news")
			undo := tc.stop(t, newsDir)
			code, _, se := newsFetch(t, hub, "-layer", "weekly")
			if code != 1 {
				t.Fatalf("止めたのに exit=%d\n%s", code, se)
			}
			// 止まった時点: 書けたものはある・既読は進んでいない・ロックは外れている・記録は残っている
			for _, f := range tc.wrote {
				if _, err := os.Stat(filepath.Join(newsDir, f)); err != nil {
					t.Errorf("止まる前に書けているはずの %s が無い: %v", f, err)
				}
			}
			if _, err := os.Stat(filepath.Join(newsDir, ".seen.json")); !errors.Is(err, fs.ErrNotExist) {
				t.Errorf("止まったのに既読が進んだ: %v", err)
			}
			if _, err := os.Stat(filepath.Join(newsDir, ".lock.json")); !errors.Is(err, fs.ErrNotExist) {
				t.Errorf("失敗した後にロックが残っている: %v", err)
			}
			if _, err := os.Stat(filepath.Join(newsDir, ".pending.json")); err != nil {
				t.Errorf("止まったのに未完了の記録が無い: %v", err)
			}
			undo()

			// 再実行: 「既にある」で止まらず、書き直して既読まで進める
			code, so, se := newsFetch(t, hub, "-layer", "weekly")
			if code != 0 {
				t.Fatalf("再実行 exit=%d want 0\n%s%s", code, so, se)
			}
			mustContain(t, "再実行の stdout", so, "途中で止まっていた", "digest_2026-08-15_weekly.md", "news ダイジェスト:", "news 選別 UI:")
			md := readFile(t, filepath.Join(newsDir, "digest_2026-08-15_weekly.md"))
			mustContain(t, "md", md, "## B（新着 1 件）", "[記事3](https://example.com/3)")
			mustContain(t, "html", readFile(t, filepath.Join(newsDir, "digest_2026-08-15_weekly.html")), "<!doctype html>", "<h3>B（新着 1 件）</h3>")
			seen := readFile(t, filepath.Join(newsDir, ".seen.json"))
			if strings.Count(seen, "2026-08-15") != 1 {
				t.Errorf("既読:\n%s", seen)
			}
			for _, f := range []string{".pending.json", ".lock.json"} {
				if _, err := os.Stat(filepath.Join(newsDir, f)); !errors.Is(err, fs.ErrNotExist) {
					t.Errorf("完了したのに %s が残っている: %v", f, err)
				}
			}

			// 完了した後の同じ日は、これまでどおり「既にある」で止まる(決定 2026-09-03 → manual/news.md「決めたこと」は完了した回に効く)
			code, _, se = newsFetch(t, hub, "-layer", "weekly")
			if code != 1 || !strings.Contains(se, "既にある") {
				t.Errorf("完了後の再実行: exit=%d\n%s", code, se)
			}
		})
	}
}

// 並行起動: ロック(news/.lock.json)があれば 2 つ目は何も書かずに終了コード 1。apply も同じロックを見る。
// 1 時間より古いロックは残留(前の実行が落ちた)とみなして外し、警告つき(2)で進む。
func TestNewsFetch_並行起動は片方だけ(t *testing.T) {
	hub, _ := newsHub(t)
	newsDir := filepath.Join(hub, "news")
	lock := filepath.Join(newsDir, ".lock.json")
	fresh := `{"pid": 1, "started": "` + time.Now().Format(time.RFC3339) + `", "op": "fetch"}`
	writeFile(t, lock, fresh)

	code, _, se := newsFetch(t, hub, "-layer", "weekly")
	if code != 1 {
		t.Fatalf("ロック中の fetch: exit=%d want 1\n%s", code, se)
	}
	mustContain(t, "stderr", se, "別の braindex news が動いている", ".lock.json")
	for _, f := range []string{"digest_2026-08-15_weekly.md", ".seen.json", ".pending.json"} {
		if _, err := os.Stat(filepath.Join(newsDir, f)); !errors.Is(err, fs.ErrNotExist) {
			t.Errorf("ロック中に %s を書いた: %v", f, err)
		}
	}
	if readFile(t, lock) != fresh {
		t.Error("他人のロックを書き換えた")
	}

	var so, seb bytes.Buffer
	if code := dispatch([]string{"news", "apply", "-config", filepath.Join(hub, "braindex.json"), "-inbox", t.TempDir()}, &so, &seb); code != 1 || !strings.Contains(seb.String(), "別の braindex news が動いている") {
		t.Errorf("ロック中の apply: exit=%d\n%s", code, seb.String())
	}

	// 残留: 1 時間より古い → 外して進む(警告 → 2)
	writeFile(t, lock, `{"pid": 1, "started": "2026-08-15T00:00:00Z", "op": "fetch"}`)
	code, _, se = newsFetch(t, hub, "-layer", "weekly")
	if code != 2 {
		t.Fatalf("残留ロック: exit=%d want 2\n%s", code, se)
	}
	mustContain(t, "stderr", se, "残留", ".lock.json")
	if _, err := os.Stat(filepath.Join(newsDir, "digest_2026-08-15_weekly.md")); err != nil {
		t.Errorf("残留ロックを外した後に書けていない: %v", err)
	}
	if _, err := os.Stat(lock); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("完了したのにロックが残っている: %v", err)
	}
}

// 別の日(層)の未完了の記録は、今回の実行では完了させられないので警告(2)して残す。
// 今回の分は完了したら記録から消えるので、残るのは古い 1 件だけ。
func TestNewsFetch_別の日の未完了は警告して残す(t *testing.T) {
	hub, _ := newsHub(t)
	newsDir := filepath.Join(hub, "news")
	pending := filepath.Join(newsDir, ".pending.json")
	oldMD, _ := json.Marshal(filepath.Join(newsDir, "digest_2026-08-14_daily.md"))
	oldHTML, _ := json.Marshal(filepath.Join(newsDir, "digest_2026-08-14_daily.html"))
	writeFile(t, pending, `{"version": 1, "runs": [{"op": "fetch", "started": "2026-08-14T07:30:00+09:00", "date": "2026-08-14", "layer": "daily", "outputs": [`+string(oldMD)+`, `+string(oldHTML)+`]}]}`)

	code, _, se := newsFetch(t, hub, "-layer", "weekly")
	if code != 2 {
		t.Fatalf("exit=%d want 2\n%s", code, se)
	}
	mustContain(t, "stderr", se, "完了していない", "2026-08-14", "daily", "-date 2026-08-14 -layer daily")
	if _, err := os.Stat(filepath.Join(newsDir, "digest_2026-08-15_weekly.md")); err != nil {
		t.Errorf("今回の分を書けていない: %v", err)
	}
	got := readFile(t, pending)
	if !strings.Contains(got, "2026-08-14") || strings.Contains(got, "weekly") {
		t.Errorf("記録: 古い 1 件だけが残るはず:\n%s", got)
	}
}
