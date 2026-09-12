// Package scan はスキャン対象ファイルの発見を担う。
//
// 自動規則: root の repo_depth 段下の各ディレクトリ D(既定 1 で root 直下。2 なら group/name)について
//   - notes_dirs の各ディレクトリ N について D/N/**/*.md を再帰収集(種別は N の末尾セグメント + 相対位置)。
//     notes_dirs は braindex.json で複数指定でき、既定は ["docs/notes"](2026-09-02。wiki/ 派を受け入れるため。
//     規約の名前は docs のまま)。同じファイルが複数の指定から拾われたら先に書いた指定のラベルで 1 回だけ
//   - D/docs/decisions.md があれば 1 エントリ(種別 decisions。notes_dirs に含まれていても decisions が勝つ)
//   - root 相対パスのセグメントに archive を含むものは除外(root 自身のパスは見ない)
//   - docs/ を持たないディレクトリは自然にスキップ
//
// 例外規則(braindex.json の extra): 指定リポの起点から *.md を収集(リポ直下・research/・projects/ 等の規約外の置き場)。
//
//	再帰 extra の種別は kind/<起点直下のサブディレクトリ>(2026-08-27)。
//
// 読めなかった範囲(権限エラー等で列挙・参照に失敗したディレクトリやファイル)は警告するだけでなく Result.Gaps に
// 構造化して返す。その範囲にノートが「無い」のか「読めなかった」のかは分からないので、索引に無いことを
// 削除の根拠にしない(設計レビュー補足 2026-09-06)。存在しない・設定の誤りは確認できた事実なので Gaps には入れない。
package scan

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
)

// Config は braindex.json の内容。
type Config struct {
	Root      string      `json:"root"`
	RepoDepth int         `json:"repo_depth"` // root から何段下のディレクトリをリポとみなすか。0 または省略で 1(root 直下)。2 なら group/name がリポ名
	NotesDirs []string    `json:"notes_dirs"` // 各リポのノート置き場(リポ相対・スラッシュ区切り)。複数可。空なら ["docs/notes"]
	Extra     []ExtraRule `json:"extra"`
}

// DefaultRepoDepth は repo_depth 未指定時の段数(root 直下をリポとみなす)。
const DefaultRepoDepth = 1

// Depth は有効なリポの段数を返す(RepoDepth が 0 なら既定の 1)。負の値の検査は Scan が行う。
func (c Config) Depth() int {
	if c.RepoDepth <= 0 {
		return DefaultRepoDepth
	}
	return c.RepoDepth
}

// ExtraRule は自動規則で拾えない配置(リポ直下など規約外の置き場)を補う例外指定。
type ExtraRule struct {
	Repo      string   `json:"repo"`      // 対象リポ(リポ名。repo_depth が 2 なら group/name のようにスラッシュ区切り)
	Path      string   `json:"path"`      // リポ内の起点。"." はリポ直下
	Recursive bool     `json:"recursive"` // false なら起点直下のみ
	Kind      string   `json:"kind"`      // catalog に載せる種別ラベル
	Exclude   []string `json:"exclude"`   // 除外パターン(path.Match のグロブ。"/" を含むなら起点からの相対パスに掛ける)。ディレクトリにも掛かり、当たった枝は丸ごと除外される
}

// Repo は root の下にある 1 リポ(ListRepos の結果)。
type Repo struct {
	Name string // リポ名(root 相対・スラッシュ区切り。depth 1 なら "alpha"、2 なら "work/alpha")。catalog の H2 見出し
	Dir  string // そのディレクトリのパス(ListRepos に渡した root と同じ基準)
}

// ListRepos は root の depth 段下のディレクトリをリポとして列挙する(索引・review・lint で共通の規則)。
//
//   - depth 1 なら root 直下、2 なら root/<group>/<name> の各ディレクトリがリポ。depth が 1 未満なら 1
//   - どの段でも "." で始まるディレクトリは見ない(.git など)。ディレクトリでないものも見ない
//   - 並びは各段のディレクトリ名の昇順(os.ReadDir の順)なので、同じ木からは常に同じ列になる
//
// root 自身を読めなければ error。途中の段(group)を列挙できなければ、その範囲を Gap(ディレクトリ)として返して
// 続ける——配下にリポが「無い」のか「読めなかった」のかは分からないので、無いと断定させないため。
func ListRepos(root string, depth int) (repos []Repo, gaps []Gap, err error) {
	if depth < 1 {
		depth = DefaultRepoDepth
	}
	entries, err := os.ReadDir(root)
	if err != nil {
		return nil, nil, fmt.Errorf("root を読めない: %w", err)
	}
	var walk func(dir, rel string, left int, entries []fs.DirEntry)
	walk = func(dir, rel string, left int, entries []fs.DirEntry) {
		for _, de := range entries {
			if !de.IsDir() || strings.HasPrefix(de.Name(), ".") {
				continue
			}
			name := de.Name()
			if rel != "" {
				name = rel + "/" + name
			}
			child := filepath.Join(dir, de.Name())
			if left == 1 {
				repos = append(repos, Repo{Name: name, Dir: child})
				continue
			}
			sub, err := os.ReadDir(child)
			if err != nil {
				gaps = append(gaps, Gap{Rel: name, Dir: true, Reason: DescribeErr(err)})
				continue
			}
			walk(child, name, left-1, sub)
		}
	}
	walk(root, "", depth, entries)
	return repos, gaps, nil
}

// SplitRepo は root 相対パス rel(スラッシュ区切り)をリポ名とリポ内のパスに分ける。cfg の段数に従い、
// depth 2 なら "work/alpha/docs/notes/x.md" → ("work/alpha", "docs/notes/x.md")。
// 段数に足りない・リポ名の段に空や "." で始まるセグメントがある・リポ内のパスが空なら ok=false。
// リポ内のパスの形(空のセグメント・"."・"..")は見ない。
func SplitRepo(cfg Config, rel string) (repo, inRepo string, ok bool) {
	depth := cfg.Depth()
	parts := strings.Split(strings.Trim(filepath.ToSlash(rel), "/"), "/")
	if len(parts) <= depth {
		return "", "", false
	}
	for _, seg := range parts[:depth] {
		if seg == "" || strings.HasPrefix(seg, ".") {
			return "", "", false
		}
	}
	inRepo = strings.Join(parts[depth:], "/")
	if inRepo == "" {
		return "", "", false
	}
	return strings.Join(parts[:depth], "/"), inRepo, true
}

// File は発見した 1 ファイル。
type File struct {
	Repo string // リポ名(catalog の H2 見出し。repo_depth が 2 なら group/name)
	Kind string // 種別ラベル(notes/common, notes/project, notes, notes/<sub>, decisions, ...)
	Rel  string // root 相対・スラッシュ区切りのパス
	Abs  string // 読み込み用の絶対パス
}

// Gap は走査で確認できなかった範囲。
// 走査は続けたが、この範囲にノートが「無い」のか「読めなかった」のかは分からない。
// 索引にこの範囲の行が無くても、削除と断定してはいけない。
type Gap struct {
	Rel    string // root 相対・スラッシュ区切り(ファイルかディレクトリ。末尾に "/" は付けない)
	Dir    bool   // true ならディレクトリ(列挙に失敗。配下すべてが確認不能)
	Reason string // 短い理由(DescribeErr の文言)
}

// Covers は rel(root 相対・スラッシュ区切り)がこの範囲に入るかを返す。ディレクトリなら配下も含む。
func (g Gap) Covers(rel string) bool {
	if rel == g.Rel {
		return true
	}
	return g.Dir && strings.HasPrefix(rel, g.Rel+"/")
}

// Result は Scan の結果。
type Result struct {
	Files    []File   // 見つけたファイル(走査順)
	Gaps     []Gap    // 読めなかった範囲(Rel 昇順・重複なし)。空なら、走査した範囲は全部確認できた
	Warnings []string // 飛ばしたものの説明(Gaps の分も含む)。無言スキップにしない
}

// DefaultNotesDir は notes_dirs 未指定時のノート置き場。
const DefaultNotesDir = "docs/notes"

// NormalizeNotesDir は notes_dirs の 1 件を、走査が実際に使う形(リポ相対・スラッシュ区切り・
// 先頭の "./" と重複した区切りを畳んだ形)にする。空の指定は "" を返す(呼び出し側で飛ばす)。
//
// 走査は filepath.Join で起点を作るので "./docs/notes" が "docs/notes" になる。
// 対象判定(Covers)や診断の表示が文字列のまま前方一致していると、走査は拾ったパスを
// 「対象外」と答えてしまう。同じ設定に同じ答えを返すため、正規化はこの 1 か所に集める。
func NormalizeNotesDir(nd string) string {
	nd = strings.Trim(filepath.ToSlash(nd), "/")
	if nd == "" {
		return ""
	}
	return path.Clean(nd)
}

// Scan は cfg に従って対象ファイルを発見する。
//
// 戻り値の Warnings は「飛ばしたもの」の説明(読めないディレクトリ・存在しない extra の起点など)。
// 無言でスキップせず呼び出し側に伝え、走査自体は続ける。root が空・読めない場合は error。
// 読めなかった範囲は Gaps にも構造化して返す(警告の文言だけでは後から機械で突き合わせられない)。
func Scan(cfg Config) (Result, error) {
	if cfg.Root == "" {
		return Result{}, errors.New("root が空")
	}
	if cfg.RepoDepth < 0 {
		return Result{}, fmt.Errorf("repo_depth は 1 以上(省略で 1): %d", cfg.RepoDepth)
	}
	depth := cfg.Depth()
	notesDirs := cfg.NotesDirs
	if len(notesDirs) == 0 {
		notesDirs = []string{DefaultNotesDir}
	}
	rootAbs, err := filepath.Abs(cfg.Root)
	if err != nil {
		return Result{}, err
	}
	for _, nd := range notesDirs {
		if err := checkRepoRelative("notes_dirs", nd); err != nil {
			return Result{}, err
		}
	}
	for _, ex := range cfg.Extra {
		if err := checkRepoName(ex.Repo, depth); err != nil {
			return Result{}, err
		}
		// ラベルは「extra <repo> の path」。他の文言の "extra <repo>/<path>" と並んだとき /path が値に見えないように
		if err := checkRepoRelative("extra "+ex.Repo+" の path", ex.Path); err != nil {
			return Result{}, err
		}
		for _, pat := range ex.Exclude {
			if _, err := path.Match(pat, ""); err != nil {
				return Result{}, fmt.Errorf("extra %s/%s: exclude のパターンが不正: %q", ex.Repo, ex.Path, pat)
			}
		}
	}
	c := &collector{rootAbs: rootAbs}
	var files []File
	seen := map[string]bool{} // 同一ファイルの重複排除(絶対パス)。先に拾った方(自動規則 → extra の順)のラベルが勝つ

	// 自動規則: root の depth 段下の各ディレクトリ(リポ)を走査。列挙できなかった group は確認不能の範囲
	repos, gaps, err := ListRepos(rootAbs, depth)
	if err != nil {
		return Result{}, err
	}
	for _, g := range gaps {
		c.addGap(g)
	}
	for _, r := range repos {
		name, repoDir := r.Name, r.Dir

		// docs/decisions.md(notes_dirs より先に拾い、種別 decisions を優先する)
		decPath := filepath.Join(repoDir, "docs", "decisions.md")
		if fi, err := os.Stat(decPath); err != nil {
			if !errors.Is(err, fs.ErrNotExist) {
				c.gap(decPath, false, err) // 無いのは正常、読めないのは警告して確認不能に
			}
		} else if !fi.IsDir() {
			if f := mkFile(rootAbs, name, "decisions", decPath); !hasArchiveSeg(f.Rel) {
				files = append(files, f)
				seen[decPath] = true
			}
		}

		// notes_dirs の各 N について D/N/**/*.md
		for _, nd := range notesDirs {
			nd = NormalizeNotesDir(nd)
			if nd == "" {
				continue
			}
			// 種別ラベルは各ディレクトリの末尾セグメント(docs/notes → notes、wiki → wiki)。既定の挙動は従来どおり
			label := path.Base(nd)
			notes := collectNotes(rootAbs, name, filepath.Join(repoDir, filepath.FromSlash(nd)), label, c)
			for _, f := range notes {
				if seen[f.Abs] {
					continue
				}
				seen[f.Abs] = true
				files = append(files, f)
			}
		}
	}

	// 例外規則。自動規則で拾い済みのファイルは載せない(重複排除)
	for _, ex := range cfg.Extra {
		for _, f := range collectExtra(rootAbs, ex, c) {
			if seen[f.Abs] {
				continue
			}
			seen[f.Abs] = true
			files = append(files, f)
		}
	}

	// 収集元にかかわらず、重なる extra の除外を優先する。
	kept := files[:0]
	for _, f := range files {
		if Covers(cfg, f.Rel) {
			kept = append(kept, f)
		}
	}
	return Result{Files: kept, Gaps: SortGaps(c.gaps), Warnings: c.warnings}, nil
}

// collector は走査中の警告と読めなかった範囲を集める。
type collector struct {
	rootAbs  string
	warnings []string
	gaps     []Gap
}

// warn は飛ばしたものを警告として記録する(確認できた事実。設定の誤り・存在しない起点など)。
func (c *collector) warn(format string, a ...any) {
	c.warnings = append(c.warnings, fmt.Sprintf(format, a...))
}

// gap は確認できなかった範囲(権限エラー等)を記録する。警告にも同じ内容を 1 行出す(従来の文言を保つ)。
func (c *collector) gap(abs string, dir bool, err error) {
	c.addGap(Gap{Rel: relSlash(c.rootAbs, abs), Dir: dir, Reason: DescribeErr(err)})
}

// addGap は root 相対で表した確認不能の範囲を記録する(ListRepos が返した group の分など)。
func (c *collector) addGap(g Gap) {
	c.gaps = append(c.gaps, g)
	c.warn("%s: %s", g.Rel, g.Reason)
}

// SortGaps は読めなかった範囲を Rel 昇順に並べ、同じ Rel の重複を落として返す(出力の決定性のため)。
func SortGaps(gaps []Gap) []Gap {
	if len(gaps) == 0 {
		return nil
	}
	out := make([]Gap, len(gaps))
	copy(out, gaps)
	sort.SliceStable(out, func(i, j int) bool { return out[i].Rel < out[j].Rel })
	w := 0
	for i, g := range out {
		if i > 0 && g.Rel == out[w-1].Rel {
			continue
		}
		out[w] = g
		w++
	}
	return out[:w]
}

// Covers は cfg の走査規則が rel(root 相対・スラッシュ区切り)を対象に含むかを返す。ファイルの有無は見ない。
//
// 前回の索引にあって今回無い行を「削除」と呼ぶ前に、今の設定がそのパスをそもそも見に行くかを確かめるのに使う。
// notes_dirs や extra を設定から外した・exclude を足した・archive の下へ移した行は「対象外」であって削除ではない。
// 判定は Scan と同じ規則(ドットで始まるリポは見ない・archive セグメントは除外・拡張子は .md・extra の exclude は
// ファイル名とディレクトリの枝に掛かる)を、ファイルシステムを見ずにパスだけで再現する。
func Covers(cfg Config, rel string) bool {
	rel = strings.Trim(filepath.ToSlash(rel), "/")
	if rel == "" || !isMarkdown(rel) || hasArchiveSeg(rel) {
		return false
	}
	repo, inRepo, ok := SplitRepo(cfg, rel)
	if !ok {
		return false
	}
	for _, seg := range strings.Split(inRepo, "/") {
		if seg == "" || seg == "." || seg == ".." {
			return false
		}
	}
	if excludedByExtra(cfg, repo, inRepo) {
		return false
	}
	if inRepo == "docs/decisions.md" {
		return true
	}
	notesDirs := cfg.NotesDirs
	if len(notesDirs) == 0 {
		notesDirs = []string{DefaultNotesDir}
	}
	for _, nd := range notesDirs {
		switch nd = NormalizeNotesDir(nd); {
		case nd == "":
			continue
		case nd == ".": // リポ直下を置き場にする指定。リポ内の全部が対象
			if !hasDotSeg(inRepo) {
				return true
			}
		case strings.HasPrefix(inRepo, nd+"/"):
			if !hasDotSeg(strings.TrimPrefix(inRepo, nd+"/")) {
				return true
			}
		}
	}
	for _, ex := range cfg.Extra {
		if ex.Repo != repo {
			continue
		}
		base := path.Clean(filepath.ToSlash(ex.Path))
		if base == "." {
			base = ""
		}
		fromBase := inRepo
		if base != "" {
			if !strings.HasPrefix(inRepo, base+"/") {
				continue
			}
			fromBase = inRepo[len(base)+1:]
		}
		parts := strings.Split(fromBase, "/")
		if hasDotSeg(fromBase) || (!ex.Recursive && len(parts) != 1) {
			continue
		}
		if excluded(parts[len(parts)-1], fromBase, ex.Exclude) {
			continue
		}
		// 再帰 extra は途中のディレクトリにも exclude が掛かり、当たった枝は丸ごと落ちる(collectExtra と同じ)
		blocked := false
		for i := 1; i < len(parts); i++ {
			if excluded(parts[i-1], strings.Join(parts[:i], "/"), ex.Exclude) {
				blocked = true
				break
			}
		}
		if blocked {
			continue
		}
		return true
	}
	return false
}

// checkRepoName は extra.repo が root から depth 段のリポ名(スラッシュ区切り)であることを確かめる。
// depth 1 なら "alpha"、2 なら "work/alpha"。段数が違う・空のセグメント・"." や ".."・バックスラッシュは設定の誤り。
func checkRepoName(repo string, depth int) error {
	segs := strings.Split(repo, "/")
	ok := len(segs) == depth && !strings.Contains(repo, `\`)
	for _, s := range segs {
		if s == "" || s == "." || s == ".." {
			ok = false
		}
	}
	if ok {
		return nil
	}
	if depth == 1 {
		return fmt.Errorf("extra: repo は root 直下のディレクトリ名だけを書く: %q", repo)
	}
	return fmt.Errorf("extra: repo は root から %d 段のディレクトリ名をスラッシュで区切って書く(repo_depth が %d): %q", depth, depth, repo)
}

// checkRepoRelative は設定のパス(notes_dirs・extra.path)がリポ内の相対パスであることを確かめる。
// ".." セグメントはリポの外へ出てしまい、root 相対でない行が索引に載る。絶対パスは filepath.Join が
// 先頭の区切りを捨てて相対扱いにするので外へは出ないが、書いた場所とは別の場所(リポの中)を指す。
// どちらも設定の誤りとして弾く。"" と "." はリポ直下の意味で許す。
func checkRepoRelative(what, p string) error {
	if p == "" || p == "." {
		return nil
	}
	if filepath.IsAbs(p) || filepath.IsAbs(filepath.FromSlash(p)) || strings.HasPrefix(p, "/") {
		return fmt.Errorf("%s: 絶対パスは書けない(リポ内の相対パスにする): %q", what, p)
	}
	for _, seg := range strings.Split(filepath.ToSlash(p), "/") {
		if seg == ".." {
			return fmt.Errorf("%s: \"..\" でリポの外を指せない: %q", what, p)
		}
	}
	return nil
}

// collectNotes は notesDir 以下の *.md を再帰収集する。archive セグメントは除外。
// label は直下の種別ラベル(サブディレクトリ配下は label/<先頭セグメント>)。
// notesDir が無いのは「そのリポにノートが無い」だけなので警告しない。読めない場合は警告して確認不能にする。
func collectNotes(rootAbs, repo, notesDir, label string, c *collector) []File {
	var out []File
	info, err := os.Stat(notesDir)
	if err != nil {
		if !errors.Is(err, fs.ErrNotExist) {
			c.gap(notesDir, true, err)
		}
		return out
	}
	if !info.IsDir() {
		c.warn("%s: ディレクトリではない", relSlash(rootAbs, notesDir))
		return out
	}
	filepath.WalkDir(notesDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			// WalkDir がここに err を渡すのはディレクトリを列挙できなかったとき(配下は確認不能)。警告して飛ばす
			c.gap(path, true, err)
			return nil
		}
		if d.IsDir() {
			if d.Name() == "archive" || (path != notesDir && strings.HasPrefix(d.Name(), ".")) {
				return filepath.SkipDir // アーカイブは普段の検索から外す
			}
			return nil
		}
		if !isMarkdown(d.Name()) {
			return nil
		}
		rel, err := filepath.Rel(notesDir, path)
		if err != nil {
			c.warn("%s: %s", relSlash(rootAbs, path), DescribeErr(err))
			return nil
		}
		if f := mkFile(rootAbs, repo, kindFromRel(rel, label), path); !hasArchiveSeg(f.Rel) {
			out = append(out, f)
		}
		return nil
	})
	return out
}

// collectExtra は例外規則に従ってファイルを収集する。
// 起点が無い・archive の下にあるのは設定の誤りなので警告する(自動規則の notesDir 不在とは違う)。
// 起点を読めないのは確認不能。
func collectExtra(rootAbs string, ex ExtraRule, c *collector) []File {
	base := filepath.Join(rootAbs, ex.Repo, filepath.FromSlash(ex.Path))
	var out []File
	if hasDotSeg(ex.Repo) {
		return out
	}
	// 起点自体が archive セグメントの下なら、archive の除外規則で全件落ちる。設定の誤りなので無言にしない
	if hasArchiveSeg(path.Join(ex.Repo, ex.Path)) {
		c.warn("extra %s/%s: パスに archive を含むので全件除外(載せるなら archive の外に置く)", ex.Repo, ex.Path)
		return out
	}
	if info, err := os.Stat(base); err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			c.warn("extra %s/%s: %s", ex.Repo, ex.Path, DescribeErr(err))
		} else {
			c.gap(base, true, err)
		}
		return out
	} else if !info.IsDir() {
		c.warn("extra %s/%s: ディレクトリではない", ex.Repo, ex.Path)
		return out
	}

	// relFromBase は起点からの相対パス(スラッシュ区切り)。exclude の "/" 入りパターンはこれに掛ける
	add := func(path, name, relFromBase, kind string) {
		if !isMarkdown(name) || excluded(name, relFromBase, ex.Exclude) {
			return
		}
		if f := mkFile(rootAbs, ex.Repo, kind, path); !hasArchiveSeg(f.Rel) {
			out = append(out, f)
		}
	}

	if ex.Recursive {
		filepath.WalkDir(base, func(path string, d fs.DirEntry, err error) error {
			if err != nil {
				c.gap(path, true, err)
				return nil
			}
			if d.IsDir() {
				if d.Name() == "archive" || (path != base && strings.HasPrefix(d.Name(), ".")) {
					return filepath.SkipDir
				}
				// exclude はディレクトリにも掛け、当たったら枝ごと落とす(gitignore と同じ感覚)。
				// 起点自身には掛けない(掛けると全件消え、除外指定の意図と食い違う)
				if path != base {
					rel, err := filepath.Rel(base, path)
					if err != nil {
						c.warn("%s: %v", path, err)
						return nil
					}
					if excluded(d.Name(), filepath.ToSlash(rel), ex.Exclude) {
						return filepath.SkipDir
					}
				}
				return nil
			}
			rel, err := filepath.Rel(base, path)
			if err != nil {
				c.warn("%s: %v", path, err)
				return nil
			}
			add(path, d.Name(), filepath.ToSlash(rel), extraKind(ex.Kind, base, path))
			return nil
		})
		return out
	}

	des, err := os.ReadDir(base)
	if err != nil {
		c.gap(base, true, err)
		return out
	}
	for _, de := range des {
		if de.IsDir() {
			continue
		}
		add(filepath.Join(base, de.Name()), de.Name(), de.Name(), ex.Kind)
	}
	return out
}

// extraKind は再帰 extra の種別を返す。起点直下なら kind そのまま、サブディレクトリ配下なら
// "kind/<先頭セグメント>"（docs/notes の kindFromRel と同じ規則。research/topic-a・projects/alpha 等）。
func extraKind(kind, base, path string) string {
	rel, err := filepath.Rel(base, path)
	if err != nil {
		return kind
	}
	parts := strings.Split(filepath.ToSlash(rel), "/")
	if len(parts) <= 1 {
		return kind
	}
	return kind + "/" + parts[0]
}

// kindFromRel は notesDir からの相対パスに応じた種別を返す。
// 直下なら label(既定 "notes")、サブディレクトリ配下なら "label/<先頭セグメント>"。
func kindFromRel(rel, label string) string {
	rel = filepath.ToSlash(rel)
	parts := strings.Split(rel, "/")
	if len(parts) <= 1 {
		return label
	}
	return label + "/" + parts[0]
}

func mkFile(rootAbs, repo, kind, absPath string) File {
	return File{
		Repo: repo,
		Kind: kind,
		Rel:  relSlash(rootAbs, absPath),
		Abs:  absPath,
	}
}

// relSlash は絶対パスを root 相対・スラッシュ区切りにする(catalog の Path と同じ形。警告のパスにも使う)。
func relSlash(rootAbs, absPath string) string {
	rel, err := filepath.Rel(rootAbs, absPath)
	if err != nil {
		rel = absPath
	}
	return filepath.ToSlash(rel)
}

func isMarkdown(name string) bool {
	return !strings.HasPrefix(filepath.Base(name), ".") && strings.HasSuffix(strings.ToLower(name), ".md")
}

// excludedByExtra は収集元に関係なく、各 extra 自身の走査範囲内でファイルと祖先を判定する。
func excludedByExtra(cfg Config, repo, inRepo string) bool {
	for _, ex := range cfg.Extra {
		if ex.Repo != repo || len(ex.Exclude) == 0 {
			continue
		}
		base := path.Clean(filepath.ToSlash(ex.Path))
		rel := inRepo
		if base != "." {
			if !strings.HasPrefix(inRepo, base+"/") {
				continue
			}
			rel = strings.TrimPrefix(inRepo, base+"/")
		}
		parts := strings.Split(rel, "/")
		if hasDotSeg(rel) || (!ex.Recursive && len(parts) != 1) {
			continue
		}
		for i, name := range parts {
			if excluded(name, strings.Join(parts[:i+1], "/"), ex.Exclude) {
				return true
			}
		}
	}
	return false
}

// hasDotSeg は指定された相対パスにドットで始まる区間があるかを返す。
func hasDotSeg(rel string) bool {
	for _, seg := range strings.Split(filepath.ToSlash(rel), "/") {
		if strings.HasPrefix(seg, ".") {
			return true
		}
	}
	return false
}

// excluded は exclude パターンに当たるかを判定する。パターンは path.Match のグロブ
// (ワイルドカード無しなら完全一致と同じ)。"/" を含むパターンは起点からの相対パス、
// 含まないパターンはファイル名・ディレクトリ名に掛ける。不正なパターンは Scan の入口で弾いてあるので、ここでは無視する。
func excluded(name, relFromBase string, patterns []string) bool {
	for _, pat := range patterns {
		pat = strings.TrimRight(pat, "/")
		target := name
		if strings.Contains(pat, "/") {
			target = relFromBase
		}
		if ok, _ := path.Match(pat, target); ok {
			return true
		}
	}
	return false
}

// hasArchiveSeg は root 相対パス(スラッシュ区切り)のいずれかのセグメントが archive かを判定する。
// 絶対パスに掛けると root 自身が archive ディレクトリの下にあるとき全件除外されるので、必ず相対パスを渡す。
func hasArchiveSeg(rel string) bool {
	for _, seg := range strings.Split(filepath.ToSlash(rel), "/") {
		if seg == "archive" {
			return true
		}
	}
	return false
}

// DescribeErr は警告向けにエラーを短く言い直す。*fs.PathError はパスを繰り返さないよう Err だけにし、
// 存在しない場合は OS ごとの文言でなく「存在しない」にする。
func DescribeErr(err error) string {
	if errors.Is(err, fs.ErrNotExist) {
		return "存在しない"
	}
	var pe *fs.PathError
	if errors.As(err, &pe) {
		return pe.Err.Error()
	}
	return err.Error()
}
