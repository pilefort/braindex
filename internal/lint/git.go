package lint

import (
	"os/exec"
	"path/filepath"
)

// HeadContent は path の git HEAD 版の内容を返す。
// git が無い・git 管理外・未追跡・まだコミットが無い、のどれでも ok=false を返してエラーにはしない
// (比較を飛ばすだけ。呼び出し側は要約の「HEAD 比較 N 件」で飛ばした数が分かるようにする)。
// git はあれば使うだけで、braindex の依存にはしない。
func HeadContent(path string) (content []byte, ok bool) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return nil, false
	}
	// HEAD:./<name> は -C で指定したディレクトリからの相対パス(リポのルートからではない)
	out, err := exec.Command("git", "-C", filepath.Dir(abs), "show", "HEAD:./"+filepath.Base(abs)).Output()
	if err != nil {
		return nil, false
	}
	return out, true
}
