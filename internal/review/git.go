package review

import (
	"bytes"
	"errors"
	"fmt"
	"os/exec"
	"regexp"
	"sort"
	"strings"
)

// Git は git コマンドの薄い包み。git はあれば使うだけで braindex の依存にはしない
// (無い環境では git を使う節を飛ばして警告する)。
type Git struct {
	path string // 実行ファイルのパス
}

// LookGit は PATH から git を探す。無ければ ok=false。
func LookGit() (g Git, ok bool) {
	p, err := exec.LookPath("git")
	if err != nil {
		return Git{}, false
	}
	return Git{path: p}, true
}

// run は dir をカレントにして git を実行し、stdout を返す。失敗時は stderr の要点をエラーに含める。
func (g Git) run(dir string, args ...string) (string, error) {
	cmd := exec.Command(g.path, append([]string{"-C", dir}, args...)...)
	out, err := cmd.Output()
	if err != nil {
		var ee *exec.ExitError
		if errors.As(err, &ee) && len(ee.Stderr) > 0 {
			return "", fmt.Errorf("git %s: %s", args[0], strings.TrimSpace(string(ee.Stderr)))
		}
		return "", fmt.Errorf("git %s: %w", args[0], err)
	}
	return string(bytes.ReplaceAll(out, []byte("\r\n"), []byte("\n"))), nil
}

// InRepo は dir が git の作業ツリーの中かを返す(サブディレクトリでもよい)。
func (g Git) InRepo(dir string) bool {
	out, err := g.run(dir, "rev-parse", "--is-inside-work-tree")
	return err == nil && strings.TrimSpace(out) == "true"
}

// Snapshot は前回日時点の索引のコミット。
type Snapshot struct {
	Content []byte
	Commit  string // 短いハッシュ
	Date    string // コミット日 YYYY-MM-DD
}

// FileAt は dir の rel(dir 相対・スラッシュ区切り)について、until(YYYY-MM-DD)の終わりまでに入った
// 最後のコミット時点の内容を返す。そのコミットが無い(初回・まだコミットしていない)なら ok=false。
// git 管理外なら error。
func (g Git) FileAt(dir, rel, until string) (s Snapshot, ok bool, err error) {
	out, err := g.run(dir, "log", "-1", "--format=%h %as", "--until="+until+" 23:59:59", "--", rel)
	if err != nil {
		return s, false, err
	}
	line := strings.TrimSpace(out)
	if line == "" {
		return s, false, nil
	}
	hash, date, _ := strings.Cut(line, " ")
	// <hash>:./<rel> の ./ は -C のディレクトリ基準(リポのルート基準ではない)
	content, err := g.run(dir, "show", hash+":./"+rel)
	if err != nil {
		return s, false, err
	}
	return Snapshot{Content: []byte(content), Commit: hash, Date: date}, true, nil
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

var hashLine = regexp.MustCompile(`^[0-9a-f]{40}$`)

// ChangedSince は dir で since(YYYY-MM-DD。その日を含む)以降のコミットが pathspecs の範囲で触ったファイルを集める。
// 同じファイルが複数のコミットに現れたら 1 行にまとめ、前回日の時点と今の有無で 追加／変更／削除 を決める。
// 窓の中で作られて消えたファイルは載せない(前回にも今にも無い)。リネームは旧パスを削除・新パスを追加として扱う。
// パスは dir 相対(--relative)。dir の外のファイルは含まれない。
func (g Git) ChangedSince(dir, since string, pathspecs []string) (RepoChanges, error) {
	// 時刻を明示する。日付だけだと git は「その日の今の時刻」と解釈し、0 時〜実行時刻のコミットが落ちる
	args := []string{"log", "--since=" + since + " 00:00:00", "--name-status", "--relative", "--format=%H", "--"}
	args = append(args, pathspecs...)
	out, err := g.run(dir, args...)
	if err != nil {
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
