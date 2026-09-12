package news

import (
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

// ロック: 2 つ目は取れない(ErrLocked にロックの場所と外し方)。外せばまた取れる。
func TestLock(t *testing.T) {
	newsDir := filepath.Join(t.TempDir(), "news")
	unlock, stale, err := Lock(newsDir, "fetch")
	if err != nil || stale != "" {
		t.Fatalf("1 つ目: err=%v stale=%q", err, stale)
	}
	path := filepath.Join(newsDir, LockFile)
	if b, err := os.ReadFile(path); err != nil || !strings.Contains(string(b), `"op":"fetch"`) || !strings.Contains(string(b), `"pid":`) {
		t.Errorf("ロックの中身: err=%v %s", err, b)
	}
	_, _, err = Lock(newsDir, "apply")
	if !errors.Is(err, ErrLocked) || !strings.Contains(err.Error(), path) || !strings.Contains(err.Error(), "このファイルを消す") {
		t.Errorf("2 つ目: %v", err)
	}
	unlock()
	if _, err := os.Stat(path); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("外したのに残っている: %v", err)
	}
	unlock2, _, err := Lock(newsDir, "apply")
	if err != nil {
		t.Fatalf("外した後: %v", err)
	}
	unlock2()
}

// 残留: LockStaleAfter より古いロックは外して取り直す(stale にその旨)。中身が壊れていれば更新時刻で古さを測る。
func TestLock_残留(t *testing.T) {
	newsDir := filepath.Join(t.TempDir(), "news")
	if err := os.MkdirAll(newsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(newsDir, LockFile)
	old := time.Now().Add(-LockStaleAfter - time.Minute).Format(time.RFC3339)
	if err := os.WriteFile(path, []byte(`{"pid": 1, "started": "`+old+`", "op": "fetch"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	unlock, stale, err := Lock(newsDir, "fetch")
	if err != nil {
		t.Fatalf("残留を外せない: %v", err)
	}
	if !strings.Contains(stale, "残留") || !strings.Contains(stale, path) || !strings.Contains(stale, old) {
		t.Errorf("stale: %q", stale)
	}
	unlock()

	// 新しいロック(始まったばかり)は残留でない
	fresh := time.Now().Format(time.RFC3339)
	if err := os.WriteFile(path, []byte(`{"pid": 1, "started": "`+fresh+`", "op": "apply"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, _, err := Lock(newsDir, "fetch"); !errors.Is(err, ErrLocked) || !strings.Contains(err.Error(), fresh) {
		t.Errorf("新しいロック: %v", err)
	}

	// 壊れたロック: 更新時刻が新しければ取れない、古ければ外す
	if err := os.WriteFile(path, []byte("{壊れた"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, _, err := Lock(newsDir, "fetch"); !errors.Is(err, ErrLocked) || !strings.Contains(err.Error(), "中身を読めない") {
		t.Errorf("壊れた新しいロック: %v", err)
	}
	stamp := time.Now().Add(-2 * LockStaleAfter)
	if err := os.Chtimes(path, stamp, stamp); err != nil {
		t.Fatal(err)
	}
	unlock, stale, err = Lock(newsDir, "fetch")
	if err != nil || !strings.Contains(stale, "残留") {
		t.Errorf("壊れた古いロック: err=%v stale=%q", err, stale)
	}
	if unlock != nil {
		unlock()
	}
}

// 未完了の記録: 出力先で見つける・置き換える・消す。空になればファイルも消す。
func TestPending(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "news", PendingFile)
	p, err := LoadPending(path)
	if err != nil || len(p.Runs) != 0 {
		t.Fatalf("無いとき: err=%v %+v", err, p)
	}
	md := filepath.Join(dir, "news", "digest_2026-08-15_daily.md")
	p.Put(PendingRun{Op: "fetch", Started: "2026-08-15T07:30:00+09:00", Date: "2026-08-15", Layer: "daily", Outputs: []string{md, md[:len(md)-3] + ".html"}})
	p.Put(PendingRun{Op: "fetch", Started: "2026-08-16T07:30:00+09:00", Date: "2026-08-16", Layer: "daily", Outputs: []string{filepath.Join(dir, "news", "digest_2026-08-16_daily.md")}})
	if err := p.Save(path); err != nil {
		t.Fatal(err)
	}
	got, err := LoadPending(path)
	if err != nil || got.Version != PendingVersion || !reflect.DeepEqual(got.Runs, p.Runs) {
		t.Errorf("往復: err=%v\n%+v\n%+v", err, got, p)
	}
	if r, ok := got.Find(md); !ok || r.Date != "2026-08-15" || r.Primary() != md {
		t.Errorf("Find: %v %+v", ok, r)
	}
	if _, ok := got.Find(filepath.Join(dir, "news", "other.md")); ok {
		t.Error("無い出力先で見つかった")
	}
	// 同じ出力先を置き換える(再実行)
	got.Put(PendingRun{Op: "fetch", Started: "2026-08-15T08:00:00+09:00", Date: "2026-08-15", Layer: "daily", Outputs: []string{md}})
	if len(got.Runs) != 2 {
		t.Errorf("置き換えで増えた: %+v", got.Runs)
	}
	if r, _ := got.Find(md); r.Started != "2026-08-15T08:00:00+09:00" {
		t.Errorf("置き換わっていない: %+v", r)
	}
	// 完了 → 消す。空になればファイルも消える
	got.Remove(md)
	got.Remove(filepath.Join(dir, "news", "digest_2026-08-16_daily.md"))
	if err := got.Save(path); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("空なのにファイルが残っている: %v", err)
	}
	// 壊れていればエラー(黙って捨てない)
	if err := os.WriteFile(path, []byte("{壊れた"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadPending(path); err == nil || !strings.Contains(err.Error(), "壊れている") {
		t.Errorf("壊れた記録: %v", err)
	}
}

// 古い残留ロックを 2 つの実行が同時に見つけても、外していいのは片方だけ。
// 先に外して取り直した側のロックを、後から来た方が消してはいけない(外部レビュー 2026-09-12)。
func TestLock_残留を同時に見つけても他人のロックは消さない(t *testing.T) {
	newsDir := filepath.Join(t.TempDir(), "news")
	if err := os.MkdirAll(newsDir, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(newsDir, LockFile)
	old := time.Now().Add(-LockStaleAfter - time.Minute).Format(time.RFC3339)
	b, _ := json.Marshal(lockInfo{PID: 1, Started: old, Op: "fetch"})
	if err := os.WriteFile(path, append(b, '\n'), 0o644); err != nil {
		t.Fatal(err)
	}
	// B はここで残留ロックを読んだ。まだ外していない
	readByB, _, err := readLock(path)
	if err != nil {
		t.Fatal(err)
	}
	// A が先に残留を外して取り直す
	unlockA, stale, err := Lock(newsDir, "fetch")
	if err != nil || !strings.Contains(stale, "残留") {
		t.Fatalf("A: err=%v stale=%q", err, stale)
	}
	defer unlockA()
	mine, _, err := readLock(path)
	if err != nil || mine.PID != os.Getpid() {
		t.Fatalf("A のロックが無い: err=%v %+v", err, mine)
	}
	// B が自分の読んだ残留ロックを外そうとする。A のロックは消さない
	if ok, err := stealStale(path, readByB); ok || err != nil {
		t.Errorf("他人のロックを外した: ok=%v err=%v", ok, err)
	}
	if got, _, err := readLock(path); err != nil || got != mine {
		t.Errorf("A のロックが消えた/変わった: err=%v %+v", err, got)
	}
	// B は「別の braindex news が動いている」で止まる(2 つが同時に取れない)
	if _, _, err := Lock(newsDir, "fetch"); !errors.Is(err, ErrLocked) {
		t.Errorf("B も取れてしまった: %v", err)
	}
}
