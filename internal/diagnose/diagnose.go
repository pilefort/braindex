// Package diagnose は「いま何を走査対象にし、何が読めて、何が索引から抜けているか」を人が確かめる診断を組み立てる。
//
// 索引に行が無いことは「ノートが無い」証明にならない(設計レビュー補足 2026-09-06)。読めなかった範囲のノートは
// 載っていないだけで、あるかどうかは分からない。設定を変えた・archive へ移した場所の行も、削除ではない。
// この診断は、その「載っていない理由」を読み手が自分で確かめられるよう、3 つに分けて見せる:
//
//   - 設定(情報源): どの設定ファイルと root を使い、どの置き場(notes_dirs・extra)を見に行くか
//   - いま走査すると: この瞬間に索引を再生成したら載る件数・読めなかった範囲・警告(リポ別の置き場の状態つき)
//   - 保存済みの索引: ディスクにある catalog.md の生成日・件数・走査の記録と、いまの走査との差
//
// 「いま」と「保存済み」を分けるのは、両者が食い違うとき(索引が古い・前回は読めなかった)に、どちらを信じるかを
// 読み手が決められるようにするため。走査は索引の生成と同じ catalog.Build を使う(別の走査を書くと結果がずれる)。
// 索引も元ノートも書き換えない。LLM は使わず、同じ材料からは同じ出力になる(規則ベース)。
package diagnose

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"

	"github.com/pilefort/braindex/internal/catalog"
	"github.com/pilefort/braindex/internal/indexdata"
	"github.com/pilefort/braindex/internal/scan"
)

// Input は診断の材料。
type Input struct {
	ConfigFile  string      // 使った設定ファイルのパス。"" なら無し(フラグだけで動作)
	Cfg         scan.Config // 解決済みの走査設定(Root は解決済み)
	CatalogPath string      // 保存済みの索引の場所
	Date        string      // 診断日 YYYY-MM-DD
	Path        string      // 問い合わせるパス(root 相対)。"" なら無し
}

// Report は診断の結果。テキスト(Render)と JSON(JSON)は同じこの値から作る。
type Report struct {
	Date     string     `json:"date"`
	Config   ConfigInfo `json:"config"`
	Scan     ScanInfo   `json:"scan"`
	Saved    SavedInfo  `json:"saved_index"`
	Path     *PathInfo  `json:"path,omitempty"`
	Problems []string   `json:"problems"` // 要確認の理由(終了コード 2 の根拠)。無ければ空
}

// ConfigInfo は解決済みの設定(情報源)。
type ConfigInfo struct {
	File             string      `json:"file"` // 設定ファイルの絶対パス。"" なら無し
	Root             string      `json:"root"` // root の絶対パス(スラッシュ区切り)
	NotesDirs        []string    `json:"notes_dirs"`
	NotesDirsDefault bool        `json:"notes_dirs_default"` // notes_dirs が未指定で既定を使った
	Extra            []ExtraInfo `json:"extra"`
}

// ExtraInfo は extra の 1 規則と、その起点の状態。
type ExtraInfo struct {
	Repo      string   `json:"repo"`
	Path      string   `json:"path"`
	Recursive bool     `json:"recursive"`
	Kind      string   `json:"kind"`
	Exclude   []string `json:"exclude"`
	Status    string   `json:"status"`  // ok | missing | unreadable | not_dir | archive
	Entries   int      `json:"entries"` // この起点の下で索引に載る件数(自動規則で拾った分も含む)
}

// ScanInfo は「いま走査すると」の結果。
type ScanInfo struct {
	Entries  int        `json:"entries"`
	Repos    []RepoInfo `json:"repos"` // 走査と同じ規則で列挙したリポ(repo_depth 段目・ドット始まりを除く)。名前昇順。ノートが無いものも載る
	Gaps     []GapInfo  `json:"gaps"`
	Warnings []string   `json:"warnings"` // 読めなかった範囲以外の警告(存在しない extra など)
}

// RepoInfo は 1 リポの走査結果。
type RepoInfo struct {
	Name    string      `json:"name"`
	Entries int         `json:"entries"`
	Kinds   []KindCount `json:"kinds"`
	Places  []PlaceInfo `json:"places"` // docs/decisions.md と notes_dirs の各置き場
	Gaps    int         `json:"gaps"`   // このリポの中の読めなかった範囲の数
}

// KindCount は種別ごとの件数。
type KindCount struct {
	Kind    string `json:"kind"`
	Entries int    `json:"entries"`
}

// PlaceInfo は規約の置き場(docs/decisions.md・notes_dirs の各ディレクトリ)の状態。
type PlaceInfo struct {
	Path    string `json:"path"`    // リポ内の相対パス
	Status  string `json:"status"`  // ok | missing | unreadable | not_dir | is_dir
	Entries int    `json:"entries"` // この置き場から索引に載る件数
}

// GapInfo は読めなかった範囲。
type GapInfo struct {
	Path   string `json:"path"` // root 相対。ディレクトリは末尾に "/"
	Dir    bool   `json:"dir"`
	Reason string `json:"reason"`
}

// SavedInfo は保存済みの索引の状態。
type SavedInfo struct {
	File      string    `json:"file"`
	Status    string    `json:"status"` // ok | missing | unreadable | invalid
	Error     string    `json:"error,omitempty"`
	Generated string    `json:"generated,omitempty"` // 先頭「生成:」行の日付
	Entries   int       `json:"entries"`
	Coverage  string    `json:"coverage,omitempty"` // complete | gaps | unknown(記録を持たない版で生成)
	Gaps      []GapInfo `json:"gaps"`
	Diff      *DiffInfo `json:"diff,omitempty"` // Status が ok のときだけ
}

// DiffInfo は保存済みの索引といまの走査との差(パスで突き合わせる。中身の変化は見ない)。
type DiffInfo struct {
	NotIndexed  []string    `json:"not_indexed"`  // いま見つかるが索引に無い(再生成で載る)
	Unconfirmed []DiffEntry `json:"unconfirmed"`  // 索引にあるが、今回読めなかった範囲の中(有無は分からない)
	OutOfScope  []DiffEntry `json:"out_of_scope"` // 索引にあるが、いまの設定では走査しない場所
	Gone        []string    `json:"gone"`         // 索引にあるが、置き場は確認できてそのパスに無い
}

// Count は差の合計。
func (d DiffInfo) Count() int {
	return len(d.NotIndexed) + len(d.Unconfirmed) + len(d.OutOfScope) + len(d.Gone)
}

// DiffEntry は理由つきの差の 1 件。
type DiffEntry struct {
	Path   string `json:"path"`
	Reason string `json:"reason"`
}

// PathInfo は問い合わせたパス 1 件の見立て。
type PathInfo struct {
	Path    string `json:"path"`
	Covered bool   `json:"covered"`
	Rule    string `json:"rule"`    // 対象なら当たった規則、対象外なら理由
	Scanned string `json:"scanned"` // found | gap | absent
	Gap     string `json:"gap,omitempty"`
	Indexed string `json:"indexed"` // yes | no | unknown(索引を読めない)
	Entry   string `json:"entry,omitempty"`
}

// Build は診断を組み立てる。走査は索引の生成と同じ経路(catalog.Build)で行い、索引は読むだけ。
// root が読めない・設定が不正なら error。
func Build(in Input) (Report, error) {
	if in.Date == "" {
		return Report{}, errors.New("診断日が空")
	}
	res, err := catalog.Build(in.Cfg, in.Date)
	if err != nil {
		return Report{}, err
	}
	rootAbs, err := filepath.Abs(in.Cfg.Root)
	if err != nil {
		return Report{}, err
	}
	notesDirs := effectiveNotesDirs(in.Cfg)
	r := Report{Date: in.Date}
	r.Config = ConfigInfo{
		File:             absSlash(in.ConfigFile),
		Root:             filepath.ToSlash(rootAbs),
		NotesDirs:        notesDirs,
		NotesDirsDefault: len(in.Cfg.NotesDirs) == 0,
		Extra:            extraInfos(in.Cfg, rootAbs, res),
	}
	r.Scan = scanInfo(in.Cfg, rootAbs, notesDirs, res)
	saved, savedEntries := savedInfo(in.CatalogPath, in.Cfg, res)
	r.Saved = saved
	if in.Path != "" {
		p := pathInfo(in.Cfg, notesDirs, in.Path, res, saved, savedEntries)
		r.Path = &p
	}
	r.Problems = problems(r)
	return r, nil
}

// JSON は Report を整形した JSON(末尾に改行)にする。
func JSON(r Report) ([]byte, error) {
	b, err := json.MarshalIndent(r, "", "  ")
	if err != nil {
		return nil, err
	}
	return append(b, '\n'), nil
}

// effectiveNotesDirs は走査が実際に使うノート置き場(空の指定を除き、無ければ既定)。
func effectiveNotesDirs(cfg scan.Config) []string {
	if len(cfg.NotesDirs) == 0 {
		return []string{scan.DefaultNotesDir}
	}
	out := []string{}
	for _, nd := range cfg.NotesDirs {
		nd = strings.Trim(filepath.ToSlash(nd), "/")
		if nd != "" {
			out = append(out, nd)
		}
	}
	return out
}

func absSlash(p string) string {
	if p == "" {
		return ""
	}
	if a, err := filepath.Abs(p); err == nil {
		p = a
	}
	return filepath.ToSlash(p)
}

// gapInfos は読めなかった範囲を出力の形(ディレクトリは末尾 "/")にする。
func gapInfos(gaps []scan.Gap) []GapInfo {
	out := make([]GapInfo, 0, len(gaps))
	for _, g := range gaps {
		out = append(out, GapInfo{Path: gapPath(g), Dir: g.Dir, Reason: g.Reason})
	}
	return out
}

func gapPath(g scan.Gap) string {
	if g.Dir {
		return g.Rel + "/"
	}
	return g.Rel
}

// isGapDir は rel が読めなかったディレクトリそのものかを返す(Stat は通っても列挙で失敗した場所を拾う)。
func isGapDir(gaps []scan.Gap, rel string) bool {
	for _, g := range gaps {
		if g.Dir && g.Rel == rel {
			return true
		}
	}
	return false
}

// placeStatus は置き場の状態を返す。wantFile は docs/decisions.md のようにファイルを期待する置き場。
func placeStatus(abs string, wantFile bool) string {
	fi, err := os.Stat(abs)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return "missing"
	case err != nil:
		return "unreadable"
	case wantFile && fi.IsDir():
		return "is_dir"
	case !wantFile && !fi.IsDir():
		return "not_dir"
	}
	return "ok"
}

// extraInfos は extra の各規則の起点の状態と、その下で索引に載る件数を返す。
func extraInfos(cfg scan.Config, rootAbs string, res catalog.Result) []ExtraInfo {
	out := make([]ExtraInfo, 0, len(cfg.Extra))
	for _, ex := range cfg.Extra {
		base := strings.Trim(filepath.ToSlash(ex.Path), "/")
		if base == "." {
			base = ""
		}
		baseRel := path.Join(ex.Repo, base)
		ei := ExtraInfo{Repo: ex.Repo, Path: ex.Path, Recursive: ex.Recursive, Kind: ex.Kind, Exclude: append([]string{}, ex.Exclude...)}
		switch {
		case hasArchiveSeg(baseRel):
			ei.Status = "archive"
		case isGapDir(res.Coverage.Gaps, baseRel):
			ei.Status = "unreadable"
		default:
			ei.Status = placeStatus(filepath.Join(rootAbs, filepath.FromSlash(baseRel)), false)
		}
		ei.Entries = countUnder(res.Records, baseRel, ex.Recursive)
		out = append(out, ei)
	}
	return out
}

// countUnder は baseRel の下にある索引の行を数える。recursive でなければ直下だけ。
func countUnder(records []indexdata.Entry, baseRel string, recursive bool) int {
	n := 0
	for _, e := range records {
		rest, ok := strings.CutPrefix(e.Path, baseRel+"/")
		if !ok || rest == "" {
			continue
		}
		if !recursive && strings.Contains(rest, "/") {
			continue
		}
		n++
	}
	return n
}

// scanInfo は「いま走査すると」を組み立てる。リポは走査と同じ規則で列挙する(repo_depth 段目の全ディレクトリ。ノートが無いものも)。
func scanInfo(cfg scan.Config, rootAbs string, notesDirs []string, res catalog.Result) ScanInfo {
	si := ScanInfo{Entries: res.Entries, Repos: []RepoInfo{}, Gaps: gapInfos(res.Coverage.Gaps), Warnings: []string{}}
	// 走査の警告には読めなかった範囲が同じ文言で 1 行ずつ入っている。二重に見せないよう、それ以外だけ残す
	gapLines := map[string]bool{}
	for _, g := range res.Coverage.Gaps {
		gapLines[g.Rel+": "+g.Reason] = true
	}
	for _, w := range res.Warnings {
		if !gapLines[w] {
			si.Warnings = append(si.Warnings, w)
		}
	}
	byRepo := map[string][]indexdata.Entry{}
	for _, e := range res.Records {
		byRepo[e.Repo] = append(byRepo[e.Repo], e)
	}
	// リポの列挙は走査と同じ規則にする(repo_depth が 2 なら group/name がリポ)。
	// ここで root 直下だけを見ると、depth 2 の hub で group をリポとして並べてしまう
	repos, _, err := scan.ListRepos(rootAbs, cfg.Depth())
	if err != nil {
		return si // catalog.Build が通っているので普通は読める
	}
	for _, rp := range repos {
		name := rp.Name
		ri := RepoInfo{Name: name, Entries: len(byRepo[name]), Kinds: []KindCount{}, Places: []PlaceInfo{}}
		kinds := map[string]int{}
		for _, e := range byRepo[name] {
			kinds[e.Kind]++
		}
		for k, n := range kinds {
			ri.Kinds = append(ri.Kinds, KindCount{Kind: k, Entries: n})
		}
		sort.Slice(ri.Kinds, func(i, j int) bool { return ri.Kinds[i].Kind < ri.Kinds[j].Kind })
		for _, g := range res.Coverage.Gaps {
			if g.Rel == name || strings.HasPrefix(g.Rel, name+"/") {
				ri.Gaps++
			}
		}
		repoDir := rp.Dir
		decRel := name + "/docs/decisions.md"
		dec := PlaceInfo{Path: "docs/decisions.md", Status: placeStatus(filepath.Join(repoDir, "docs", "decisions.md"), true)}
		for _, e := range byRepo[name] {
			if e.Path == decRel {
				dec.Entries++
			}
		}
		ri.Places = append(ri.Places, dec)
		for _, nd := range notesDirs {
			pl := PlaceInfo{Path: nd}
			ndRel := path.Join(name, nd)
			switch {
			case nd == ".":
				pl.Status = "ok"
				pl.Entries = len(byRepo[name]) - dec.Entries
			case isGapDir(res.Coverage.Gaps, ndRel):
				pl.Status = "unreadable"
			default:
				pl.Status = placeStatus(filepath.Join(repoDir, filepath.FromSlash(nd)), false)
			}
			if nd != "." {
				pl.Entries = countUnder(byRepo[name], ndRel, true)
			}
			ri.Places = append(ri.Places, pl)
		}
		si.Repos = append(si.Repos, ri)
	}
	sort.Slice(si.Repos, func(i, j int) bool { return si.Repos[i].Name < si.Repos[j].Name })
	return si
}

// savedInfo は保存済みの索引を読み、いまの走査と突き合わせる。索引は読むだけで書き換えない。
// 読み戻した行も返す(問い合わせたパスの照合に使う。二度読みしない)。
func savedInfo(catalogPath string, cfg scan.Config, res catalog.Result) (SavedInfo, []indexdata.Entry) {
	si := SavedInfo{File: absSlash(catalogPath), Gaps: []GapInfo{}}
	b, err := os.ReadFile(catalogPath)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			si.Status = "missing"
		} else {
			si.Status = "unreadable"
			si.Error = scan.DescribeErr(err)
		}
		return si, nil
	}
	entries, err := indexdata.ParseCatalog(b)
	if err != nil {
		si.Status = "invalid"
		si.Error = err.Error()
		return si, nil
	}
	cov, err := catalog.ParseCoverage(b)
	if err != nil {
		si.Status = "invalid"
		si.Error = err.Error()
		return si, nil
	}
	si.Status = "ok"
	si.Generated = generatedDate(b)
	si.Entries = len(entries)
	switch {
	case !cov.Known:
		si.Coverage = "unknown"
	case len(cov.Gaps) == 0:
		si.Coverage = "complete"
	default:
		si.Coverage = "gaps"
	}
	si.Gaps = gapInfos(cov.Gaps)
	d := Compare(entries, res.Records, res.Coverage, cfg)
	si.Diff = &d
	return si, entries
}

// generatedDate は索引の先頭「生成: YYYY-MM-DD / ...」行の日付を返す。無ければ ""。
// 入力の BOM と CRLF は正規化する(indexdata と同じ規則。BOM はソースにリテラルを置かずバイトで見る)。
func generatedDate(b []byte) string {
	if len(b) >= 3 && b[0] == 0xEF && b[1] == 0xBB && b[2] == 0xBF {
		b = b[3:]
	}
	s := strings.ReplaceAll(string(b), "\r\n", "\n")
	s = strings.ReplaceAll(s, "\r", "\n")
	for _, line := range strings.Split(s, "\n") {
		if strings.HasPrefix(line, "## ") {
			break
		}
		if rest, ok := strings.CutPrefix(line, "生成: "); ok {
			date, _, _ := strings.Cut(rest, " ")
			return date
		}
	}
	return ""
}

// Compare は保存済みの索引の行(saved)と、いまの走査で索引に載る行(current)をパスで突き合わせる。
// 前回にあって今回無い行は、そのまま「無い」にしない: 今回読めなかった範囲(cov)の中なら確認不能、
// いまの設定(cfg)が見に行かない場所なら対象外、残りだけが「無い」。純関数で、同じ入力からは同じ結果を返す。
func Compare(saved, current []indexdata.Entry, cov catalog.Coverage, cfg scan.Config) DiffInfo {
	d := DiffInfo{NotIndexed: []string{}, Unconfirmed: []DiffEntry{}, OutOfScope: []DiffEntry{}, Gone: []string{}}
	savedSet := make(map[string]bool, len(saved))
	for _, e := range saved {
		savedSet[e.Path] = true
	}
	curSet := make(map[string]bool, len(current))
	for _, e := range current {
		if curSet[e.Path] {
			continue
		}
		curSet[e.Path] = true
		if !savedSet[e.Path] {
			d.NotIndexed = append(d.NotIndexed, e.Path)
		}
	}
	seen := map[string]bool{}
	for _, e := range saved {
		if curSet[e.Path] || seen[e.Path] {
			continue
		}
		seen[e.Path] = true
		switch g, inGap := cov.Gap(e.Path); {
		case inGap:
			d.Unconfirmed = append(d.Unconfirmed, DiffEntry{Path: e.Path, Reason: gapPath(g)})
		case !scan.Covers(cfg, e.Path):
			d.OutOfScope = append(d.OutOfScope, DiffEntry{Path: e.Path, Reason: WhyNotCovered(cfg, e.Path)})
		default:
			d.Gone = append(d.Gone, e.Path)
		}
	}
	sort.Strings(d.NotIndexed)
	sort.Slice(d.Unconfirmed, func(i, j int) bool { return d.Unconfirmed[i].Path < d.Unconfirmed[j].Path })
	sort.Slice(d.OutOfScope, func(i, j int) bool { return d.OutOfScope[i].Path < d.OutOfScope[j].Path })
	sort.Strings(d.Gone)
	return d
}

// pathInfo は問い合わせたパス 1 件について、設定・いまの走査・保存済みの索引のそれぞれで見立てる。
func pathInfo(cfg scan.Config, notesDirs []string, rel string, res catalog.Result, saved SavedInfo, savedEntries []indexdata.Entry) PathInfo {
	rel = strings.Trim(filepath.ToSlash(rel), "/")
	pi := PathInfo{Path: rel, Covered: scan.Covers(cfg, rel)}
	if pi.Covered {
		pi.Rule = WhichRule(cfg, notesDirs, rel)
	} else {
		pi.Rule = WhyNotCovered(cfg, rel)
	}
	pi.Scanned = "absent"
	for _, e := range res.Records {
		if e.Path == rel {
			pi.Scanned = "found"
			break
		}
	}
	if pi.Scanned == "absent" {
		if g, ok := res.Coverage.Gap(rel); ok {
			pi.Scanned = "gap"
			pi.Gap = gapPath(g)
		}
	}
	pi.Indexed = "unknown"
	if saved.Status == "ok" {
		pi.Indexed = "no"
		for _, e := range savedEntries {
			if e.Path == rel {
				pi.Indexed = "yes"
				pi.Entry = describe(e)
				break
			}
		}
	}
	return pi
}

// describe は索引の行を「日付・タイトル」の形にする(日付が無ければタイトルだけ)。
func describe(e indexdata.Entry) string {
	if e.Date == "" {
		return e.Title
	}
	return e.Date + "・" + e.Title
}

// WhichRule は対象のパスに当たった規則を言う(docs/decisions.md・notes_dirs・extra の順。scan.Covers と同じ順序)。
func WhichRule(cfg scan.Config, notesDirs []string, rel string) string {
	repo, inRepo, _ := strings.Cut(rel, "/")
	if inRepo == "docs/decisions.md" {
		return "docs/decisions.md（決定記録）"
	}
	for _, nd := range notesDirs {
		if nd == "." || strings.HasPrefix(inRepo, nd+"/") {
			return "notes_dirs " + nd
		}
	}
	for _, ex := range cfg.Extra {
		if ex.Repo == repo && scan.Covers(scan.Config{NotesDirs: cfg.NotesDirs, Extra: []scan.ExtraRule{ex}}, rel) {
			return fmt.Sprintf("extra %s/%s", ex.Repo, ex.Path)
		}
	}
	return "（規則を特定できない）"
}

// WhyNotCovered は対象外のパスについて、scan.Covers が false を返す理由を短く言う。
// 判定そのものは Covers に任せ、ここは理由のラベルだけを付ける(理由を細かく分けるのは読み手のため)。
func WhyNotCovered(cfg scan.Config, rel string) string {
	rel = strings.Trim(filepath.ToSlash(rel), "/")
	if rel == "" {
		return "パスが空"
	}
	if !strings.HasSuffix(strings.ToLower(rel), ".md") {
		return "拡張子が .md でない"
	}
	if hasArchiveSeg(rel) {
		return "パスに archive セグメントを含む（アーカイブは索引から外れる）"
	}
	repo, inRepo, ok := strings.Cut(rel, "/")
	if !ok || inRepo == "" {
		return "リポ名だけで、リポ内のパスが無い"
	}
	if strings.HasPrefix(repo, ".") {
		return "ドットで始まるリポは見ない"
	}
	for _, seg := range strings.Split(inRepo, "/") {
		if seg == "" || seg == "." || seg == ".." {
			return "パスの形が不正（空のセグメント・. ・..）"
		}
	}
	for _, ex := range cfg.Extra {
		if ex.Repo != repo {
			continue
		}
		base := strings.Trim(filepath.ToSlash(ex.Path), "/")
		if base == "" || base == "." || strings.HasPrefix(inRepo, base+"/") {
			return fmt.Sprintf("extra %s/%s の範囲だが、exclude に当たるか、直下のみの指定でサブディレクトリにある", ex.Repo, ex.Path)
		}
	}
	return fmt.Sprintf("ノート置き場（%s）にも docs/decisions.md にも extra にも無い場所", strings.Join(effectiveNotesDirs(cfg), "・"))
}

// hasArchiveSeg は root 相対パスのいずれかのセグメントが archive か(scan と同じ規則)。
func hasArchiveSeg(rel string) bool {
	for _, seg := range strings.Split(rel, "/") {
		if seg == "archive" {
			return true
		}
	}
	return false
}

// problems は要確認の理由を並べる(終了コード 2 の根拠)。順序は 走査 → 保存済みの索引。
func problems(r Report) []string {
	p := []string{}
	if r.Scan.Entries == 0 {
		p = append(p, "いま走査しても索引に載るファイルが 1 件も無い（root・notes_dirs・extra を見直す）")
	}
	if n := len(r.Scan.Gaps); n > 0 {
		p = append(p, fmt.Sprintf("いま走査すると読めなかった範囲が %d 件ある（その範囲のノートは索引に載らない。無いのか読めないのかは分からない）", n))
	}
	if n := len(r.Scan.Warnings); n > 0 {
		p = append(p, fmt.Sprintf("走査の警告が %d 件ある（存在しない extra など。設定を見直す）", n))
	}
	switch r.Saved.Status {
	case "missing":
		p = append(p, "保存済みの索引が無い（hub で braindex を実行して作る）")
	case "unreadable":
		p = append(p, "保存済みの索引を読めない: "+r.Saved.Error)
	case "invalid":
		p = append(p, "保存済みの索引が braindex の書く形でない（手で編集された）: "+r.Saved.Error)
	case "ok":
		switch r.Saved.Coverage {
		case "unknown":
			p = append(p, "保存済みの索引に走査の記録が無い（この記録を持たない版で生成。欠けがあったかは分からない。再生成すると付く）")
		case "gaps":
			p = append(p, fmt.Sprintf("保存済みの索引は読めなかった範囲 %d 件を残して作られた（その範囲のノートは載っていない）", len(r.Saved.Gaps)))
		}
		if d := r.Saved.Diff; d != nil && d.Count() > 0 {
			p = append(p, fmt.Sprintf("保存済みの索引といまの走査に差がある（未反映 %d・確認不能 %d・対象外 %d・無い %d）。再生成するといまの走査に揃う",
				len(d.NotIndexed), len(d.Unconfirmed), len(d.OutOfScope), len(d.Gone)))
		}
	}
	return p
}
