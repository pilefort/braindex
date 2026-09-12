package news

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

// 中断からの立て直しと、並行起動の排他(設計レビュー補足 2026-09-06「処理単位の復旧」)。
//
// fetch はダイジェスト md → 選別 UI html → 既読 の順に別々のファイルを書く。1 ファイルの半端は WriteAtomic が
// 防ぐが、途中で止まると「md はあるのに既読が進んでいない」のようなファイル間の食い違いが残り、次の fetch は
// 「同じ日の出力先が既にある」(決定 2026-09-03 → manual/news.md「決めたこと」)で止まって手が出せない。そこで書き始める前に記録(PendingFile)を
// 置き、既読まで書き終えたら消す。記録にある出力先は完了していないので、次の fetch が書き直して既読まで進める。
// 記録に無い出力先は完了した回なので、これまでどおり上書きしない。
//
// 選別の取り込み(Ingest)には記録が要らない: 選別 JSON 1 つごとに keep → 統計 → 取り込み済みへ移す、の順で書き、
// どこで止まっても次回は同じ JSON を読んで同じ結果(keep はリンクで重複を除く・統計はダイジェスト単位の上書き)に揃う。
//
// 並行起動(定期実行と手動が重なる等)は LockFile で片方だけにする。両方が動くと、取り込みが同じ keep を 2 回足す。
// ロックは終了時に消す。落ちて残ったロックは LockStaleAfter より古ければ残留とみなして外す。
// それより新しい残留は、動いている braindex news が無いことを確かめて手で消す(メッセージに場所を出す)。

const (
	// LockFile は Dir の下。braindex news が保存物(既読・統計・keep・ダイジェスト)を触っている間だけある印。
	// git 管理外(hub の .gitignore の news/.*.json)。
	LockFile = ".lock.json"
	// PendingFile は Dir の下。fetch が書き始めてから既読を書き終えるまでの記録。完了すると消える。git 管理外。
	PendingFile = ".pending.json"
	// PendingVersion は記録の書式の版。
	PendingVersion = 1
	// LockStaleAfter はこれより古いロックを残留(前の実行が落ちた)とみなす。LLM 補助が全バッチで待ち時間いっぱい
	// かかっても 1 回の fetch はこれより短い(既定 120 秒 × 記事 20 件ごと)。
	LockStaleAfter = time.Hour
)

// ErrLocked は別の braindex news がロックを持っているとき。
var ErrLocked = errors.New("別の braindex news が動いている")

// lockInfo はロックファイルの中身(人が見て何のロックか分かるように)。
type lockInfo struct {
	PID     int    `json:"pid"`
	Started string `json:"started"` // RFC3339
	Op      string `json:"op"`      // fetch / apply
}

func (l lockInfo) String() string {
	if l.Started == "" {
		return "中身を読めない"
	}
	return fmt.Sprintf("%s に始まった %s", l.Started, l.Op)
}

// Lock は newsDir にロック(LockFile)を取り、外す関数を返す。作成は O_EXCL なので、同時に 2 つが取ろうとしても
// 片方だけが通る。ロックがあれば取らずに ErrLocked(場所・開始時刻・外し方をメッセージに含む)。
// LockStaleAfter より古いロックは残留とみなして外し、取り直す。そのときは stale にその旨を返す(呼び出し側が警告にする)。
func Lock(newsDir, op string) (unlock func(), stale string, err error) {
	path := filepath.Join(newsDir, LockFile)
	if err := os.MkdirAll(newsDir, 0o755); err != nil {
		return nil, "", err
	}
	for attempt := 0; attempt < 3; attempt++ {
		ok, err := tryLock(path, op)
		if err != nil {
			return nil, "", err
		}
		if ok {
			return func() { os.Remove(path) }, stale, nil
		}
		info, age, err := readLock(path)
		if errors.Is(err, fs.ErrNotExist) {
			continue // 見た直後に終わった → 取り直す
		}
		if err != nil {
			return nil, "", fmt.Errorf("ロック %s を読めない: %w", path, err)
		}
		if age <= LockStaleAfter {
			return nil, "", fmt.Errorf("%w: %s(%s。終わるのを待つ。動いている braindex news が無ければ、このファイルを消す)", ErrLocked, path, info)
		}
		stolen, err := stealStale(path, info)
		if err != nil {
			return nil, "", fmt.Errorf("残留したロックを外せない: %w", err)
		}
		if !stolen {
			continue // 別の実行が先に外して取り直した → 取り直す(次の周回でその新しいロックを見る)
		}
		stale = fmt.Sprintf("残留していたロックを外した: %s(%s。%s 以上たっても終わっていないので、前の実行は途中で落ちている)", path, info, LockStaleAfter)
	}
	return nil, "", fmt.Errorf("%w: %s", ErrLocked, path)
}

// stealStale は残留とみなしたロックを外す。外すのは want(古いと判断したときに読んだ中身)が今もあるときだけで、
// 別の実行が先に外して取り直していれば false を返す(その新しいロックを消さない)。
// 確かめずに消すと、同じ残留ロックを見つけた 2 つの実行が両方ともロックを取れてしまう。
// 「同じなら消す」を 1 回で行う手立てが os に無いので、確かめてから消すまでの隙間は残る(窓を狭めるところまで)。
func stealStale(path string, want lockInfo) (bool, error) {
	now, _, err := readLock(path)
	if errors.Is(err, fs.ErrNotExist) {
		return false, nil // 先に誰かが外した
	}
	if err != nil {
		return false, err
	}
	if now != want {
		return false, nil // 別の実行が取り直した後のロック。これは残留ではない
	}
	if err := os.Remove(path); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return false, err
	}
	return true, nil
}

// tryLock は O_EXCL でロックを作る。既にあれば false(エラーにしない)。
func tryLock(path, op string) (bool, error) {
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if err != nil {
		if errors.Is(err, fs.ErrExist) {
			return false, nil
		}
		return false, fmt.Errorf("ロックを作れない: %w", err)
	}
	b, _ := json.Marshal(lockInfo{PID: os.Getpid(), Started: time.Now().Format(time.RFC3339), Op: op}) // 数と文字列だけなので失敗しない
	_, werr := f.Write(append(b, '\n'))
	if cerr := f.Close(); werr == nil {
		werr = cerr
	}
	if werr != nil {
		os.Remove(path)
		return false, fmt.Errorf("ロックを書けない: %w", werr)
	}
	return true, nil
}

// readLock はロックの中身と古さを返す。中身を読めない(壊れている)ときはファイルの更新時刻で古さを測る。
func readLock(path string) (lockInfo, time.Duration, error) {
	var info lockInfo
	fi, err := os.Stat(path)
	if err != nil {
		return info, 0, err
	}
	age := time.Since(fi.ModTime())
	if b, err := os.ReadFile(path); err == nil && json.Unmarshal(b, &info) == nil {
		if t, err := time.Parse(time.RFC3339, info.Started); err == nil {
			age = time.Since(t)
		}
	}
	return info, age, nil
}

// PendingRun は fetch 1 回の記録。
type PendingRun struct {
	Op      string   `json:"op"`      // "fetch"
	Started string   `json:"started"` // RFC3339
	Date    string   `json:"date"`    // 今日として使った日付(-date)
	Layer   string   `json:"layer"`   // 層(-layer)
	Outputs []string `json:"outputs"` // 書く(書いた)出力の絶対パス。先頭がダイジェスト md、次が選別 UI html
}

// Primary は記録を見分ける出力先(先頭のダイジェスト md)。
func (r PendingRun) Primary() string {
	if len(r.Outputs) == 0 {
		return ""
	}
	return r.Outputs[0]
}

// Pending は未完了の記録(PendingFile)。完了した回は消えるので、普段は空(ファイルも無い)。
type Pending struct {
	Version int          `json:"version"`
	Runs    []PendingRun `json:"runs"`
}

// LoadPending は記録を読む。無ければ空。壊れていればエラー(黙って捨てると、未完了の回を完了扱いにしてしまう)。
func LoadPending(path string) (Pending, error) {
	var p Pending
	b, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return p, nil
		}
		return p, fmt.Errorf("未完了の記録を読めない: %w", err)
	}
	if err := json.Unmarshal(b, &p); err != nil {
		return Pending{}, fmt.Errorf("未完了の記録 %s が壊れている(消せば記録なしから始まる): %w", path, err)
	}
	return p, nil
}

// Save は記録を書く(原子的)。Runs が空ならファイルを消す(無いのが普段の状態)。
func (p Pending) Save(path string) error {
	if len(p.Runs) == 0 {
		if err := os.Remove(path); err != nil && !errors.Is(err, fs.ErrNotExist) {
			return err
		}
		return nil
	}
	p.Version = PendingVersion
	b, err := json.MarshalIndent(p, "", " ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return writeAtomic(path, append(b, '\n'), 0o644)
}

// Find は出力先 out(ダイジェスト md)の記録を返す。
func (p Pending) Find(out string) (PendingRun, bool) {
	for _, r := range p.Runs {
		if samePath(r.Primary(), out) {
			return r, true
		}
	}
	return PendingRun{}, false
}

// Put は記録を足す。同じ出力先の記録があれば置き換える(再実行は前の記録を引き継がず今回の開始時刻で持つ)。
func (p *Pending) Put(r PendingRun) {
	p.Remove(r.Primary())
	p.Runs = append(p.Runs, r)
}

// Remove は出力先 out の記録を消す(完了したとき)。
func (p *Pending) Remove(out string) {
	kept := p.Runs[:0]
	for _, r := range p.Runs {
		if !samePath(r.Primary(), out) {
			kept = append(kept, r)
		}
	}
	p.Runs = kept
}

// samePath は 2 つのパスが同じファイルを指すか(Windows は大文字小文字を区別しない)。
func samePath(a, b string) bool {
	a, b = filepath.Clean(a), filepath.Clean(b)
	if runtime.GOOS == "windows" {
		return strings.EqualFold(a, b)
	}
	return a == b
}
