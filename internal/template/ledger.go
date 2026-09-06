package template

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/pilefort/braindex/internal/fsutil"
)

// 台帳は「配った版のハッシュ」を持つ。update はこれと現物を突き合わせて、利用者がそのファイルを
// 編集したかを見分ける(決定 2026-09-04)。dpkg が conffile の MD5 を記録するのと同じ型で、
// 未編集なら黙って今の版にし、編集済みなら隣に .new を置く。
// 記録が無いファイルは「編集済み」として扱う——素性が分からないものを勝手に上書きしないため。
const (
	LedgerPath    = ".braindex/template.json" // hub からの相対パス
	LedgerVersion = 1                         // 書式の版。読み手が書式の変化に気づくための目印
)

// Ledger は展開した雛形の記録。Files は雛形からの相対パス("/" 区切り)から内容のハッシュへ。
// Features は init -add で足した機能(core を除く・名前の昇順)。update はこの分だけ追従する(決定 2026-09-05)。
// hub では空でも "features": [] と書く。キーごと無い(nil)のは機能の記録を持たない旧版の台帳で、
// update はそのとき存在するファイルから機能を推定する(段 0 の hub の [] とは区別する)。
type Ledger struct {
	Version  int               `json:"version"`
	Kind     string            `json:"kind"`
	Features []string          `json:"features,omitzero"`
	Files    map[string]string `json:"files"`
}

// Hash は雛形 1 ファイルの内容ハッシュ(sha256 の 16 進)を返す。
func Hash(b []byte) string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

// LoadLedger は dst の台帳を読む。無ければ空の Ledger と found=false を返す(エラーにしない。
// 台帳を持たない hub で update を初めて走らせるのは普通に起きるため)。
func LoadLedger(dst string) (Ledger, bool, error) {
	empty := Ledger{Version: LedgerVersion, Files: map[string]string{}}
	b, err := os.ReadFile(filepath.Join(dst, filepath.FromSlash(LedgerPath)))
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return empty, false, nil
		}
		return empty, false, err // PathError がパスを持つので包み直さない
	}
	var l Ledger
	if err := json.Unmarshal(b, &l); err != nil {
		return empty, false, fmt.Errorf("%s: %w", LedgerPath, err)
	}
	if l.Files == nil {
		l.Files = map[string]string{}
	}
	return l, true, nil
}

// SaveLedger は台帳を書く。同じ内容なら常に同じバイト列になる(encoding/json は map のキーを
// 昇順に出し、改行は LF・インデントは空白 2 つ・末尾に改行を 1 つ足す)。
func SaveLedger(dst string, l Ledger) error {
	if l.Files == nil {
		l.Files = map[string]string{}
	}
	if l.Kind == string(KindHub) && l.Features == nil {
		l.Features = []string{} // omitzero で消えないよう空配列にする(nil は「記録なし」の意味)
	}
	l.Version = LedgerVersion
	b, err := json.MarshalIndent(l, "", "  ")
	if err != nil {
		return err
	}
	b = append(b, '\n')
	p := filepath.Join(dst, filepath.FromSlash(LedgerPath))
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return err
	}
	return fsutil.WriteAtomic(p, b, 0o644)
}
