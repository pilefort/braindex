package review

import (
	"os/exec"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/pilefort/braindex/internal/gitutil"
)

// Git は git コマンドの薄い包み。git はあれば使うだけで braindex の依存にはしない
// (無い環境では git を使う節を飛ばして警告する)。
type Git struct {
	path          string // 実行ファイルのパス
	sinceAsFilter bool   // --since-as-filter(git 2.37 以降)に対応するか。バージョンが読めない/パースできないときは安全側(false=従来の --since)
}

// LookGit は PATH から git を探す。無ければ ok=false。
func LookGit() (g Git, ok bool) {
	p, err := exec.LookPath("git")
	if err != nil {
		return Git{}, false
	}
	g = Git{path: p}
	g.sinceAsFilter = g.detectSinceAsFilter()
	return g, true
}

// gitVersionRe は `git version 2.39.2.windows.1` のような出力から主・副バージョンを取り出す。
var gitVersionRe = regexp.MustCompile(`^git version (\d+)\.(\d+)`)

// sinceAsFilterFromVersion は `git version` の出力から --since-as-filter(2.37 以降)に対応するかを判定する
// 純関数。バージョン文字列が読み取れない/パースできないときは安全側(false)に倒す(2026-09-12 ユーザー判断)。
func sinceAsFilterFromVersion(out string) bool {
	m := gitVersionRe.FindStringSubmatch(strings.TrimSpace(out))
	if m == nil {
		return false
	}
	major, errMajor := strconv.Atoi(m[1])
	minor, errMinor := strconv.Atoi(m[2])
	if errMajor != nil || errMinor != nil {
		return false
	}
	if major != 2 {
		return major > 2
	}
	return minor >= 37
}

// detectSinceAsFilter は実際に `git version` を実行してバージョンを調べる。失敗したときも安全側(false)に倒す。
func (g Git) detectSinceAsFilter() bool {
	out, err := g.run(".", "version")
	if err != nil {
		return false
	}
	return sinceAsFilterFromVersion(out)
}

// run は dir をカレントにして git を実行し、stdout を返す。失敗時は stderr の要点をエラーに含める。
// core.quotePath を切るのは、既定(true)だと ASCII 以外のパスが "\346\227\245..." と八進エスケープされ、
// 索引のパスと突き合わせられず表示も読めないため(二重引用符・バックスラッシュ・制御文字は false でも
// エスケープされる)。
func (g Git) run(dir string, args ...string) (string, error) {
	return gitutil.Run(g.path, dir, args...)
}

// InRepo は dir が git の作業ツリーの中かを返す(サブディレクトリでもよい)。
func (g Git) InRepo(dir string) bool {
	out, err := g.run(dir, "rev-parse", "--is-inside-work-tree")
	return err == nil && strings.TrimSpace(out) == "true"
}

// noCommits は dir が git 管理下で、まだコミットが 1 つも無い(git init 直後で HEAD の指す先が無い)かを返す。
// git log はこの状態を失敗にするので、呼び出し側は log が失敗したときにこれで見分けて「無し」に倒す。
func (g Git) noCommits(dir string) bool {
	if !g.InRepo(dir) {
		return false
	}
	_, err := g.run(dir, "rev-parse", "--verify", "--quiet", "HEAD")
	return err != nil
}

// Snapshot は前回日時点の索引のコミット。
type Snapshot struct {
	Content []byte
	Commit  string // 短いハッシュ
	Date    string // コミット日 YYYY-MM-DD
	Time    string // コミット時刻 ISO8601(%cI)。差分ファイルの起点に使う
}

// FileAt は dir の rel(dir 相対・スラッシュ区切り)について、until(YYYY-MM-DD)の終わりまでに入った
// 最後のコミット時点の内容を返す。そのコミットが無い(初回・まだコミットしていない)なら ok=false。
// git 管理外なら error。
func (g Git) FileAt(dir, rel, until string) (s Snapshot, ok bool, err error) {
	out, err := g.run(dir, "log", "-1", "--format=%h %as %cI", "--until="+until+" 23:59:59", "--", rel)
	if err != nil {
		if g.noCommits(dir) {
			return s, false, nil
		}
		return s, false, err
	}
	line := strings.TrimSpace(out)
	if line == "" {
		return s, false, nil
	}
	hash, rest, _ := strings.Cut(line, " ")
	date, iso, _ := strings.Cut(rest, " ")
	// <hash>:./<rel> の ./ は -C のディレクトリ基準(リポのルート基準ではない)
	content, err := g.run(dir, "show", hash+":./"+rel)
	if err != nil {
		return s, false, err
	}
	return Snapshot{Content: []byte(content), Commit: hash, Date: date, Time: iso}, true, nil
}

// ChangedFile は前回日以降のコミットで触られた 1 ファイル(リポ相対・スラッシュ区切り)。
type ChangedFile struct {
	Path   string
	Status string // 追加 / 変更 / 削除
}

// RepoChanges は 1 リポ分の差分ファイル。Files はパス昇順。
type RepoChanges struct {
	Repo    string
	Commits int // since 以降のコミット数(pathspec に触れたものだけ)
	Files   []ChangedFile
}

// hashLine は --format=%H のコミット行。SHA-1 なら 40 桁、SHA-256 のリポ(--object-format=sha256)なら 64 桁。
var hashLine = regexp.MustCompile(`^([0-9a-f]{40}|[0-9a-f]{64})$`)

// ChangedSince は dir で since 以降のコミットが pathspecs の範囲で触ったファイルを集める。
// since は git が読める時刻の文字列(ISO8601 か "YYYY-MM-DD HH:MM:SS")。git の --since はその時刻ちょうどの
// コミットを含む(2026-09-06 実測 → docs/notes/common/git-since-boundary.md)。
// --since は「古いコミットに当たったら、その先を辿るのをやめる」ので、コミット日時が履歴の順序と食い違って
// いると新しいコミットを取りこぼす(同記録)。g.sinceAsFilter(git 2.37 以降)なら --since-as-filter を使い、
// 打ち切らずに全部見る。未満のときは従来どおり --since のまま(2026-09-12 ユーザー判断)。
// 同じファイルが複数のコミットに現れたら 1 行にまとめ、前回日の時点と今の有無で 追加／変更／削除 を決める。
// 窓の中で作られて消えたファイルは載せない(前回にも今にも無い)。リネームは旧パスを削除・新パスを追加として扱う。
// パスは dir 相対(--relative)。dir の外のファイルは含まれない。
func (g Git) ChangedSince(dir, since string, pathspecs []string) (RepoChanges, error) {
	sinceFlag := "--since=" + startOfDayIfDate(since)
	if g.sinceAsFilter {
		sinceFlag = "--since-as-filter=" + startOfDayIfDate(since)
	}
	args := []string{"log", sinceFlag, "--name-status", "--relative", "--format=%H", "--"}
	args = append(args, pathspecs...)
	out, err := g.run(dir, args...)
	if err != nil {
		if g.noCommits(dir) {
			return RepoChanges{}, nil
		}
		return RepoChanges{}, err
	}
	return parseNameStatus(out), nil
}

// fileEvent はパス 1 つに対する 1 コミットでの出来事(新しい順に並ぶ)。
type fileEvent struct {
	path string
	kind byte // 'A' 追加 / 'M' 変更 / 'D' 削除
}

// parseNameStatus は git log --name-status --format=%H の出力(新しいコミットが先)を読む。
func parseNameStatus(out string) RepoChanges {
	var rc RepoChanges
	var commits [][]fileEvent // コミットごとの出来事。出力どおり新しい順
	add := func(ev ...fileEvent) {
		if len(commits) == 0 {
			commits = append(commits, nil)
		}
		commits[len(commits)-1] = append(commits[len(commits)-1], ev...)
	}
	for _, line := range strings.Split(out, "\n") {
		if hashLine.MatchString(line) {
			rc.Commits++
			commits = append(commits, nil)
			continue
		}
		if !strings.Contains(line, "\t") {
			continue
		}
		fields := strings.Split(line, "\t")
		status := fields[0]
		if status == "" {
			continue
		}
		switch status[0] {
		case 'R': // R<score>\t旧\t新
			if len(fields) >= 3 {
				add(fileEvent{path: fields[1], kind: 'D'}, fileEvent{path: fields[2], kind: 'A'})
			}
		case 'C': // C<score>\t元\t複製
			if len(fields) >= 3 {
				add(fileEvent{path: fields[2], kind: 'A'})
			}
		case 'A', 'D':
			add(fileEvent{path: fields[1], kind: status[0]})
		default: // M・T などは変更
			add(fileEvent{path: fields[1], kind: 'M'})
		}
	}
	// 古いコミットから辿り、前回日時点の有無(最初の出来事が A なら無かった)と今の有無(最後の出来事が D なら無い)を決める
	type state struct{ existedBefore, existsNow bool }
	states := map[string]*state{}
	order := []string{}
	for ci := len(commits) - 1; ci >= 0; ci-- {
		for _, ev := range commits[ci] {
			st, seen := states[ev.path]
			if !seen {
				st = &state{existedBefore: ev.kind != 'A'}
				states[ev.path] = st
				order = append(order, ev.path)
			}
			st.existsNow = ev.kind != 'D'
		}
	}
	for _, p := range order {
		st := states[p]
		switch {
		case st.existedBefore && st.existsNow:
			rc.Files = append(rc.Files, ChangedFile{Path: p, Status: "変更"})
		case !st.existedBefore && st.existsNow:
			rc.Files = append(rc.Files, ChangedFile{Path: p, Status: "追加"})
		case st.existedBefore && !st.existsNow:
			rc.Files = append(rc.Files, ChangedFile{Path: p, Status: "削除"})
		}
	}
	sort.Slice(rc.Files, func(i, j int) bool { return rc.Files[i].Path < rc.Files[j].Path })
	return rc
}

// dateOnly は YYYY-MM-DD だけの文字列。
var dateOnly = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}$`)

// startOfDayIfDate は日付だけの since に 00:00:00 を足す。
// git は日付だけの --since を「その日の今の時刻」と解釈するので、時刻を明示しないと
// その日の 0 時〜実行時刻のコミットが、実行する時刻しだいで落ちる。
func startOfDayIfDate(since string) string {
	if dateOnly.MatchString(since) {
		return since + " 00:00:00"
	}
	return since
}
