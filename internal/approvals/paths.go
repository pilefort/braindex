package approvals

import (
	"crypto/sha1"
	"encoding/hex"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// Paths は 1 つの APPROVALS.md に対する置き場。
type Paths struct {
	ID        string // <hub 名>-<絶対パスの hash 6 桁>。一時置き場のファイル名に使う(ファイル名に使える文字だけ)
	Project   string // hub のディレクトリ(APPROVALS.md の親が work なら その親、そうでなければ親)
	Approvals string // APPROVALS.md の絶対パス
	Decisions string // 既定の docs/decisions.md(Project 基準)
	Reply     string // 一時置き場の回答 JSON(serve が書く・apply が読む)
	Applied   string // 反映済みに改名した回答 JSON
}

var unsafeID = regexp.MustCompile(`[^A-Za-z0-9._-]+`)

// DefaultDir は回答 JSON の既定の置き場(OS の一時ディレクトリの下)。回答には選択と自由記述しか入らないが、
// hub の外に置く(索引や git に混ざらない)。
func DefaultDir() string {
	return filepath.Join(os.TempDir(), "braindex-approvals")
}

// Resolve は APPROVALS.md のパスから置き場を決める。dir は回答 JSON の置き場(空なら DefaultDir)。
func Resolve(approvalsPath, dir string) (Paths, error) {
	abs, err := filepath.Abs(approvalsPath)
	if err != nil {
		return Paths{}, err
	}
	work := filepath.Dir(abs)
	project := work
	if strings.EqualFold(filepath.Base(work), "work") {
		project = filepath.Dir(work)
	}
	if dir == "" {
		dir = DefaultDir()
	}
	base := strings.Trim(unsafeID.ReplaceAllString(filepath.Base(project), "-"), "-.")
	if base == "" {
		base = "project"
	}
	sum := sha1.Sum([]byte(strings.ToLower(filepath.ToSlash(project))))
	id := base + "-" + hex.EncodeToString(sum[:])[:6]
	return Paths{
		ID:        id,
		Project:   project,
		Approvals: abs,
		Decisions: filepath.Join(project, "docs", "decisions.md"),
		Reply:     filepath.Join(dir, "approvals-"+id+".reply.json"),
		Applied:   filepath.Join(dir, "approvals-"+id+".applied.json"),
	}, nil
}
