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
	"strings"
	"time"
)

// Reply はフォームからの回答。serve が受け取り、一時置き場に JSON で書く(apply が読む)。
type Reply struct {
	Nonce      string      `json:"nonce"`
	ReceivedAt string      `json:"received_at"` // RFC 3339(受信時刻)
	Items      []ReplyItem `json:"items"`
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
	HTML    []byte           // 配信するフォーム(RenderForm の出力)
	Nonce   string           // フォームに埋めた nonce。POST の nonce と一致しなければ拒否
	Timeout time.Duration    // 0 なら無期限
	OnReady func(url string) // 待ち受けを始めたら呼ぶ(ブラウザを開く・URL を表示する)
	Now     func() time.Time // 受信時刻(テスト用。nil なら time.Now)
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
		rep.ReceivedAt = now().Format(time.RFC3339)
		select {
		case got <- rep:
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(`{"ok":true}` + "\n"))
		default:
			fail(http.StatusConflict, "既に回答を受け取った")
		}
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

// NewNonce は起動ごとの照合値(16 バイトの乱数を 16 進 32 文字)を返す。
func NewNonce() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		// crypto/rand が失敗する環境では時刻で代用する(照合の目的は他タブ・古いフォームの混入防止で、秘密鍵ではない)
		return strings.ReplaceAll(fmt.Sprintf("%032x", time.Now().UnixNano()), " ", "0")
	}
	return hex.EncodeToString(b)
}
