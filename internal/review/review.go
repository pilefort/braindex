// Package review は週次レビューの機械部分を担う(braindex review)。
//
// 索引の増減・各リポの差分ファイル・放置 TODO・アーカイブ候補を集めて、work/review/YYYY-MM-DD.md の
// 下書きを決定的に作る。判断(差分の要約・アーカイブの可否・次アクション)は人か、人が使うエージェントが
// 行うので、ここでは見出しだけを置く。LLM は使わない(設計判断 2026-08-07「要旨は機械抽出」と同じ理由。
// 出力が揺れると前回との比較ができない)。
//
// 索引(index/catalog.md)は読むだけで書き換えない。再生成は従来の索引生成コマンドの役目。
// git はあれば使う(前回日時点の索引・差分ファイル・TODO の行日付)。無ければその部分を飛ばして警告する。
package review

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/pilefort/braindex/internal/catalog"
	"github.com/pilefort/braindex/internal/render"
	"github.com/pilefort/braindex/internal/scan"
)

// Settings は braindex.json の review 節。省略・0 は既定値。
type Settings struct {
	Dir            string `json:"dir"`              // レビュー記録の置き場(hub 相対・スラッシュ区切り)。既定 work/review
	SinceDays      int    `json:"since_days"`       // 前回の記録が無いときに遡る日数。既定 14
	StaleTodoWeeks int    `json:"stale_todo_weeks"` // TODO を放置とみなす週数。既定 4
	ArchiveMonths  int    `json:"archive_months"`   // アーカイブ候補にする古さ(月)。既定 6
}

// 既定値。
const (
	DefaultDir            = "work/review"
	DefaultSinceDays      = 14
	DefaultStaleTodoWeeks = 4
	DefaultArchiveMonths  = 6
)

// WithDefaults は空・0 の項目を既定値で埋めた複製を返す。
func (s Settings) WithDefaults() Settings {
	if s.Dir == "" {
		s.Dir = DefaultDir
	}
	if s.SinceDays <= 0 {
		s.SinceDays = DefaultSinceDays
	}
	if s.StaleTodoWeeks <= 0 {
		s.StaleTodoWeeks = DefaultStaleTodoWeeks
	}
	if s.ArchiveMonths <= 0 {
		s.ArchiveMonths = DefaultArchiveMonths
	}
	return s
}

// Input は下書きを作る材料。前回日と出力先の解決はコマンド側(cmd_review.go)が行う。
type Input struct {
	Today      string      // 今日 YYYY-MM-DD(閾値と見出しの基準)
	Since      string      // 前回日 YYYY-MM-DD
	SinceNote  string      // 前回日の根拠(冒頭に書く。例: "work/review/2026-08-26.md")
	Cfg        scan.Config // 走査設定。Root は解決済み(カレント基準か絶対)
	HubDir     string      // 設定ファイルのディレクトリ(hub のルート)
	CatalogRel string      // 索引の hub 相対パス(スラッシュ区切り。例 index/catalog.md)
	PrevPath   string      // 前回の下書き work/review/<前回日>.md。空なら判断の節の確認をしない
	Settings   Settings    // 既定値は Build が埋める
}

// Result は Build の結果。
type Result struct {
	Report   []byte   // 下書き(Markdown・LF)
	Warnings []string // 飛ばしたものの説明。無ければ空
}

// Build は材料から下書きを組み立てる。
func Build(in Input) (Result, error) {
	var res Result
	warn := func(format string, a ...any) { res.Warnings = append(res.Warnings, fmt.Sprintf(format, a...)) }
	s := in.Settings.WithDefaults()
	todoCutoff, err := DaysBefore(in.Today, 7*s.StaleTodoWeeks)
	if err != nil {
		return res, err
	}
	archiveCutoff, err := MonthsBefore(in.Today, s.ArchiveMonths)
	if err != nil {
		return res, err
	}

	// 今回の索引(走査を再実行する。ディスクの索引は書き換えない)
	built, err := catalog.Build(in.Cfg, in.Today)
	if err != nil {
		return res, err
	}
	res.Warnings = append(res.Warnings, built.Warnings...)
	afterEntries, err := ParseCatalog(built.Catalog)
	if err != nil {
		return res, err
	}

	g, hasGit := LookGit()
	var gp *Git
	if hasGit {
		gp = &g
	}

	// 前回の索引
	before, source, prevTime, ws := previousCatalog(gp, in.HubDir, in.CatalogRel, in.Since)
	res.Warnings = append(res.Warnings, ws...)
	// 前回の索引が読めなくても下書きは出す。読めない理由は前回の版が違う・手で壊した等で、
	// 増減が出せないだけで差分ファイル・放置 TODO・アーカイブ候補は作れる
	// (設計レビュー 2026-09-06 M3c)。今回の索引が読めないのはこちらのバグなので止める。
	var diff IndexDiff
	indexUnavailable := ""
	beforeEntries, perr := ParseCatalog(before)
	if perr != nil {
		indexUnavailable = perr.Error()
		warn("前回の索引を読めなかった(%v)。増減は出さない", perr)
	} else {
		diff = diffEntries(beforeEntries, afterEntries)
	}

	// 差分ファイル(索引に載ったリポだけ)。
	// 起点は「前回の索引を取ったコミットの時刻」。前回日の 0 時にすると、その日のうち索引を取る前に
	// 入った変更を前回と今回で二重に数える(設計レビュー 2026-09-06 M3b)。
	// 前回の索引がコミットから取れなかったときだけ、従来どおり前回日の 0 時にする。
	changeSince := in.Since + " 00:00:00"
	if prevTime != "" {
		changeSince = prevTime
	}
	ch := Changes{Since: in.Since, Pathspecs: pathspecs(in.Cfg), GitMissing: !hasGit}
	if !hasGit {
		warn("git が見つからないので差分ファイルの節を飛ばした")
	} else {
		for _, repo := range distinctRepos(afterEntries) {
			dir := filepath.Join(in.Cfg.Root, repo)
			if !g.InRepo(dir) {
				ch.Skipped = append(ch.Skipped, repo)
				warn("%s: git 管理外なので差分ファイルを飛ばした", repo)
				continue
			}
			rc, err := g.ChangedSince(dir, changeSince, ch.Pathspecs)
			if err != nil {
				ch.Skipped = append(ch.Skipped, repo)
				warn("%s: %v", repo, err)
				continue
			}
			rc.Repo = repo
			ch.Repos = append(ch.Repos, rc)
		}
	}

	// 放置 TODO
	todos, ws, err := StaleTodos(in.Cfg.Root, gp, todoCutoff)
	if err != nil {
		return res, err
	}
	res.Warnings = append(res.Warnings, ws...)

	// アーカイブ候補(今回の差分に無いもの)
	touched := ch.Touched()
	for p := range diff.Touched() {
		touched[p] = true
	}
	arch := ArchiveCandidates(afterEntries, archiveCutoff, touched)

	var b strings.Builder
	fmt.Fprintf(&b, "# 週次レビュー %s\n\n", in.Today)
	fmt.Fprintf(&b, "前回: %s（%s）\n", in.Since, in.SinceNote)
	b.WriteString("この下書きは `braindex review` が作った。機械節（索引・差分ファイル・放置 TODO・アーカイブ候補）は埋まっている。")
	b.WriteString("残りの節は差分ファイルの実物を読んで埋め、`braindex` で索引を再生成してから、索引と一緒にコミットする。\n\n")
	if indexUnavailable != "" {
		WriteIndexUnavailable(&b, indexUnavailable, source)
	} else {
		WriteIndexSection(&b, diff, source)
	}
	b.WriteString("\n")
	WriteChangesSection(&b, ch)
	b.WriteString("\n")
	WriteTodoSection(&b, todos, s.StaleTodoWeeks, todoCutoff)
	b.WriteString("\n")
	WriteArchiveSection(&b, arch, s.ArchiveMonths, archiveCutoff)
	b.WriteString("\n## 今週の差分ダイジェスト（リポ別）\n\n（差分ファイルを実物で読み、リポごとに 1〜3 行。索引の要旨だけで書かない）\n")
	b.WriteString("\n## アーカイブ（実施・見送りと理由）\n\n（候補ごとに 実施／見送り と理由。移動は承認の後）\n")
	b.WriteString("\n## 次アクション\n\n（1〜3 件）\n")
	// 前回の判断の節が空のままなら、今回の「次アクション」の直下に書く。機械節は毎週埋まるので
	// 回っているように見えるが、判断の節が空なら回路は動いていない(設計レビュー 2026-09-06 M7)。
	empty, ws := emptyJudgementSections(in.PrevPath)
	res.Warnings = append(res.Warnings, ws...)
	if len(empty) > 0 {
		msg := fmt.Sprintf("前回（%s）の判断の節が空のまま: %s", in.Since, strings.Join(empty, "・"))
		fmt.Fprintf(&b, "\n- %s\n", msg)
		res.Warnings = append(res.Warnings, msg)
	}
	res.Report = []byte(b.String())
	return res, nil
}

// previousCatalog は「前回の索引」を決める。hub が git 管理下で前回日以前のコミットがあればその時点の内容、
// 無ければディスク上の索引、それも無ければ空(全件が追加)。どれを使ったかを source で返す。
// commitTime は前回の索引を取ったコミットの時刻(ISO8601)。コミットから取れなかったときは空。
func previousCatalog(g *Git, hubDir, rel, since string) (content []byte, source, commitTime string, warnings []string) {
	reason := "hub が git 管理外"
	if g != nil && g.InRepo(hubDir) {
		snap, ok, err := g.FileAt(hubDir, rel, since)
		switch {
		case err != nil:
			warnings = append(warnings, fmt.Sprintf("前回の索引を git から取れない: %v", err))
			reason = "git から取れなかった"
		case ok:
			return snap.Content, fmt.Sprintf("%s（コミット %s・%s）", rel, snap.Commit, snap.Date), snap.Time, nil
		default:
			reason = "前回日以前のコミットが無い"
		}
	} else if g == nil {
		reason = "git が無い"
	}
	b, err := os.ReadFile(filepath.Join(hubDir, filepath.FromSlash(rel)))
	if err != nil {
		if !errors.Is(err, fs.ErrNotExist) {
			warnings = append(warnings, fmt.Sprintf("%s: %s", rel, describeErr(err)))
		}
		return nil, "なし（初回。全件を追加として数える）", "", warnings
	}
	return b, fmt.Sprintf("%s（ディスク。%s）", rel, reason), "", warnings
}

// pathspecs は差分ファイルを見る範囲(notes_dirs と docs/decisions.md)。
func pathspecs(cfg scan.Config) []string {
	dirs := cfg.NotesDirs
	if len(dirs) == 0 {
		dirs = []string{scan.DefaultNotesDir}
	}
	out := make([]string, 0, len(dirs)+1)
	for _, d := range dirs {
		d = strings.Trim(filepath.ToSlash(d), "/")
		if d != "" {
			out = append(out, d)
		}
	}
	return append(out, "docs/decisions.md")
}

// distinctRepos は索引に載ったリポ名を昇順で返す。
func distinctRepos(entries []render.Entry) []string {
	seen := map[string]bool{}
	var out []string
	for _, e := range entries {
		if !seen[e.Repo] {
			seen[e.Repo] = true
			out = append(out, e.Repo)
		}
	}
	sort.Strings(out)
	return out
}
