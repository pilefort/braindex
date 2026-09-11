package approvals

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/pilefort/braindex/internal/fsutil"
)

// Reply はフォームからの回答。serve が受け取り、一時置き場に JSON で書く(apply が読む)。
type Reply struct {
	Nonce      string      `json:"nonce"`
	ReceivedAt string      `json:"received_at"` // RFC 3339(受信時刻)
	Items      []ReplyItem `json:"items"`
	// Result は apply が反映したときに書き足す結果(.applied.json にだけ入る)。
	// approvals wait は、改名されたかでなくこれを見て「反映できたか」を伝える。
	Result *AppliedResult `json:"result,omitempty"`
}

// AppliedResult は回答を反映した結果。Warnings は未反映の項目(見つからない・選択肢に無い)。
type AppliedResult struct {
	Decided  int      `json:"decided"`
	Held     int      `json:"held"`
	Warnings []string `json:"warnings,omitempty"`
}

// ReplyItem は 1 項目の答え。Choice は選択肢の Key(A/B/…)か "other"(コメントに結論)か "hold"(保留)。
type ReplyItem struct {
	N       int    `json:"n"`
	Title   string `json:"title"`
	Choice  string `json:"choice"`
	Comment string `json:"comment"`
}

// ErrTimeout は Timeout の間に回答が来なかった。
var ErrTimeout = errors.New("回答が来ないまま時間切れ")

// ServeOptions は Serve の設定。
type ServeOptions struct {
	HTML      []byte           // 配信するフォーム(RenderForm の出力)
	Nonce     string           // フォームに埋めた nonce。POST の nonce と一致しなければ拒否
	Timeout   time.Duration    // 0 なら無期限
	ReplyPath string           // 回答 JSON の書き込み先。空なら書かない(呼び出し側が Serve の戻り値を書く)
	OnReady   func(url string) // 待ち受けを始めたら呼ぶ(ブラウザを開く・URL を表示する)
	Now       func() time.Time // 受信時刻(テスト用。nil なら time.Now)
}

// Serve は 127.0.0.1 の空きポートでフォームを配信し、正しい回答の POST を 1 回受けたら止まる。
// 常駐はしない。受け口は 127.0.0.1 だけで、Origin が自分以外の POST と nonce の違う POST は拒否する
// (他のサイトのページから叩かれても答えが混ざらない)。
// 返り値: 回答／ErrTimeout／ctx のエラー。
func Serve(ctx context.Context, o ServeOptions) (Reply, error) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return Reply{}, fmt.Errorf("待ち受けを開けない: %w", err)
	}
	port := ln.Addr().(*net.TCPAddr).Port
	self := map[string]bool{
		fmt.Sprintf("http://127.0.0.1:%d", port): true,
		fmt.Sprintf("http://localhost:%d", port): true,
	}
	now := o.Now
	if now == nil {
		now = time.Now
	}
	got := make(chan Reply, 1)
	var mu sync.Mutex // 回答は 1 回だけ。保存 → 受理の間に 2 本目の POST を割り込ませない
	answered := false
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Header().Set("Cache-Control", "no-store")
		w.Write(o.HTML)
	})
	mux.HandleFunc("/reply", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		fail := func(code int, msg string) {
			w.WriteHeader(code)
			json.NewEncoder(w).Encode(map[string]string{"error": msg})
		}
		if r.Method != http.MethodPost {
			fail(http.StatusMethodNotAllowed, "POST だけ")
			return
		}
		if org := r.Header.Get("Origin"); org != "" && !self[org] {
			fail(http.StatusForbidden, "Origin が自分ではない")
			return
		}
		var rep Reply
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&rep); err != nil {
			fail(http.StatusBadRequest, "JSON を読めない")
			return
		}
		if rep.Nonce != o.Nonce {
			fail(http.StatusForbidden, "nonce が一致しない(古いフォーム)")
			return
		}
		if len(rep.Items) == 0 {
			fail(http.StatusBadRequest, "項目が無い")
			return
		}
		// 応答を返す前にディスクへ書く。後から呼び出し側が書くと、書けなかったときに
		// ブラウザは完了表示のまま回答だけ消える(押した本人には成功に見える)。
		mu.Lock()
		defer mu.Unlock()
		if answered {
			fail(http.StatusConflict, "既に回答を受け取った")
			return
		}
		rep.ReceivedAt = now().Format(time.RFC3339)
		saved := ""
		if o.ReplyPath != "" {
			if err := WriteReply(o.ReplyPath, rep); err != nil {
				// 受け取ったことにしない(押し直せる・フォームは JSON 貼り付けの失敗表示に落ちる)
				fail(http.StatusInternalServerError, "回答を保存できない: "+err.Error())
				return
			}
			saved = o.ReplyPath
		}
		answered = true
		got <- rep
		w.WriteHeader(http.StatusOK)
		json.NewEncoder(w).Encode(map[string]any{"ok": true, "saved": saved})
	})
	srv := &http.Server{Handler: mux, ReadHeaderTimeout: 10 * time.Second}
	serveErr := make(chan error, 1)
	go func() { serveErr <- srv.Serve(ln) }()
	if o.OnReady != nil {
		o.OnReady(fmt.Sprintf("http://127.0.0.1:%d/", port))
	}

	var timeout <-chan time.Time
	if o.Timeout > 0 {
		t := time.NewTimer(o.Timeout)
		defer t.Stop()
		timeout = t.C
	}
	var rep Reply
	var result error
	select {
	case rep = <-got:
	case <-timeout:
		result = ErrTimeout
	case <-ctx.Done():
		result = ctx.Err()
	case err := <-serveErr:
		return Reply{}, fmt.Errorf("待ち受けが止まった: %w", err)
	}
	// 回答への応答を送り終えてから閉じる(Shutdown は処理中の要求を待つ)
	shutCtx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := srv.Shutdown(shutCtx); err != nil {
		srv.Close()
	}
	return rep, result
}

// WriteReply は回答を JSON で書く(置き場が無ければ作る)。Serve が応答を返す前に呼ぶ。
// 書き切ってから置き換える(fsutil.WriteAtomic)ので、途中で失敗しても半端な JSON は残らない。
// 次に読むのは apply(コマンド)で、半端な JSON は黙って取り込めないになる(設計レビュー 2026-09-06 M14)。
func WriteReply(path string, rep Reply) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	b, err := json.MarshalIndent(rep, "", " ")
	if err != nil {
		return err
	}
	return fsutil.WriteAtomic(path, append(b, '\n'), 0o644)
}

// NewNonce は起動ごとの照合値(16 バイトの乱数を 16 進 32 文字)を返す。
// crypto/rand.Read は Go 1.24 以降エラーを返さない(取れなければプログラムごと落ちる)ので、
// 時刻など予測できる値へのフォールバックは持たない。
func NewNonce() string {
	b := make([]byte, 16)
	rand.Read(b)
	return hex.EncodeToString(b)
}
