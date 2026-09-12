package diagnose

import (
	"fmt"
	"strings"
)

// Render は Report を人が読むテキスト(Markdown 風)にする。JSON と同じ値から作るので情報は一致する。
// 先頭に「まとめ」を置き、head で見ても要確認の有無が分かるようにする。
func Render(r Report) []byte {
	var b strings.Builder
	fmt.Fprintf(&b, "# braindex diagnose: 走査の診断（%s）\n\n", r.Date)
	b.WriteString("索引に行が無いことは「ノートが無い」証明にならない。何を見に行くか（設定）、いま何が読めるか（走査）、保存済みの索引がどこまで追いついているか（索引）を分けて示す。\n\n")

	b.WriteString("## まとめ\n")
	if len(r.Problems) == 0 {
		b.WriteString("- 問題なし: いまの走査に読めなかった範囲は無く、保存済みの索引はいまの走査と一致している\n")
	} else {
		fmt.Fprintf(&b, "- 要確認 %d 件:\n", len(r.Problems))
		for _, p := range r.Problems {
			b.WriteString("  - " + p + "\n")
		}
	}

	b.WriteString("\n## 設定（情報源）\n")
	if r.Config.File == "" {
		b.WriteString("- 設定ファイル: 無し（フラグだけで動作）\n")
	} else {
		b.WriteString("- 設定ファイル: " + r.Config.File + "\n")
	}
	b.WriteString("- root: " + r.Config.Root + "\n")
	nd := strings.Join(r.Config.NotesDirs, ", ")
	if r.Config.NotesDirsDefault {
		nd += "（既定）"
	}
	b.WriteString("- notes_dirs: " + nd + "\n")
	if len(r.Config.Extra) == 0 {
		b.WriteString("- extra: なし\n")
	} else {
		fmt.Fprintf(&b, "- extra: %d 件\n", len(r.Config.Extra))
		for _, ex := range r.Config.Extra {
			b.WriteString("  - " + extraLine(ex) + "\n")
		}
	}

	b.WriteString("\n## いま走査すると\n")
	if r.Scan.Failed != "" {
		b.WriteString("- 走査していない: " + r.Scan.Failed + "\n")
	} else {
		writeScan(&b, r.Scan)
	}

	b.WriteString("\n## 保存済みの索引\n")
	b.WriteString("- 場所: " + r.Saved.File + "\n")
	switch r.Saved.Status {
	case "missing":
		b.WriteString("- 状態: 無し（hub で braindex を実行して作る）\n")
	case "unreadable":
		b.WriteString("- 状態: 読めない: " + r.Saved.Error + "\n")
	case "invalid":
		b.WriteString("- 状態: braindex の書く形でない（手で編集された）: " + r.Saved.Error + "\n")
	default:
		gen := r.Saved.Generated
		if gen == "" {
			gen = "不明"
		}
		fmt.Fprintf(&b, "- 状態: 生成 %s・%d 件\n", gen, r.Saved.Entries)
		switch r.Saved.Coverage {
		case "unknown":
			b.WriteString("- 走査の記録: 記録なし（この記録を持たない版で生成。欠けがあったかは分からない）\n")
		case "complete":
			b.WriteString("- 走査の記録: 読めなかった範囲なし\n")
		default:
			fmt.Fprintf(&b, "- 走査の記録: 読めなかった範囲 %d 件（この範囲のノートは載っていない）\n", len(r.Saved.Gaps))
			for _, g := range r.Saved.Gaps {
				b.WriteString("  - " + g.Path + " — " + g.Reason + "\n")
			}
		}
		switch d := r.Saved.Diff; {
		case d == nil:
			b.WriteString("- いまの走査との差: 比べていない（走査していない）\n")
		case d.Count() == 0:
			b.WriteString("- いまの走査との差: なし\n")
		default:
			fmt.Fprintf(&b, "- いまの走査との差: %d 件\n", d.Count())
			writeDiffList(&b, "未反映", "いま見つかるが索引に無い。再生成で載る", d.NotIndexed, nil)
			writeDiffList(&b, "確認不能", "索引にあるが、今回読めなかった範囲の中。有無は分からない", nil, d.Unconfirmed)
			writeDiffList(&b, "対象外", "索引にあるが、いまの設定では走査しない場所", nil, d.OutOfScope)
			writeDiffList(&b, "無い", "索引にあるが、置き場は確認できてそのパスに無い", d.Gone, nil)
		}
	}

	if p := r.Path; p != nil {
		b.WriteString("\n## パス " + p.Path + "\n")
		switch {
		case r.Scan.Failed != "":
			b.WriteString("- いまの設定: 判定していない（走査できない設定）\n")
			b.WriteString("- いま走査すると: 走査していない\n")
		case p.Covered:
			b.WriteString("- いまの設定: 対象（" + p.Rule + "）\n")
		default:
			b.WriteString("- いまの設定: 対象外（" + p.Rule + "）\n")
		}
		if r.Scan.Failed == "" {
			switch p.Scanned {
			case "found":
				b.WriteString("- いま走査すると: 見つかる（索引に載る）\n")
			case "gap":
				b.WriteString("- いま走査すると: 読めなかった範囲 " + p.Gap + " の中（有無は分からない）\n")
			default:
				b.WriteString("- いま走査すると: 見つからない（置き場は確認できた。無いか、対象外）\n")
			}
		}
		switch p.Indexed {
		case "yes":
			b.WriteString("- 保存済みの索引: 載っている（" + p.Entry + "）\n")
		case "no":
			b.WriteString("- 保存済みの索引: 載っていない\n")
		default:
			b.WriteString("- 保存済みの索引: 索引が無い・読めないので分からない\n")
		}
	}
	return []byte(b.String())
}

// writeScan は「いま走査すると」の本体(件数・読めなかった範囲・警告・リポ別)を書く。走査できたときだけ呼ぶ。
func writeScan(b *strings.Builder, s ScanInfo) {
	withNotes := 0
	for _, ri := range s.Repos {
		if ri.Entries > 0 {
			withNotes++
		}
	}
	fmt.Fprintf(b, "- 索引に載る: %d 件（%d リポのうち %d リポ）\n", s.Entries, len(s.Repos), withNotes)
	if len(s.Gaps) == 0 {
		b.WriteString("- 読めなかった範囲: なし\n")
	} else {
		fmt.Fprintf(b, "- 読めなかった範囲: %d 件（この範囲のノートは載らない。無いのか読めないのかは分からない）\n", len(s.Gaps))
		for _, g := range s.Gaps {
			b.WriteString("  - " + g.Path + " — " + g.Reason + "\n")
		}
	}
	if len(s.Warnings) == 0 {
		b.WriteString("- 警告: なし\n")
	} else {
		fmt.Fprintf(b, "- 警告: %d 件\n", len(s.Warnings))
		for _, w := range s.Warnings {
			b.WriteString("  - " + w + "\n")
		}
	}
	if len(s.Repos) == 0 {
		b.WriteString("- リポ別: 設定された段数にリポのディレクトリが無い（ドットで始まるものは見ない）\n")
	} else {
		b.WriteString("- リポ別:\n")
		for _, ri := range s.Repos {
			b.WriteString("  - " + repoLine(ri) + "\n")
		}
	}
}

// extraLine は extra の 1 規則を「repo/path（再帰・種別 kind・除外 …）: 起点の状態」の形にする。
func extraLine(ex ExtraInfo) string {
	mode := "直下のみ"
	if ex.Recursive {
		mode = "再帰"
	}
	attrs := []string{mode, "種別 " + ex.Kind}
	if len(ex.Exclude) > 0 {
		attrs = append(attrs, "除外 "+strings.Join(ex.Exclude, ", "))
	}
	var status string
	switch ex.Status {
	case "ok":
		status = fmt.Sprintf("起点あり・この起点の下で索引に載る %d 件", ex.Entries)
	case "missing":
		status = "起点が存在しない（設定の誤り。索引には何も載らない）"
	case "unreadable":
		status = "起点を読めなかった（確認不能）"
	case "not_dir":
		status = "起点がディレクトリではない（設定の誤り）"
	case "archive":
		status = "起点のパスに archive を含むので全件除外（載せるなら archive の外に置く）"
	default:
		status = ex.Status
	}
	return fmt.Sprintf("%s/%s（%s）: %s", ex.Repo, ex.Path, strings.Join(attrs, "・"), status)
}

// repoLine は 1 リポを「名前: N 件（種別 n・…）— 置き場の状態。読めなかった範囲 N」の形にする。
// 置き場の状態は、ノートが 1 件も無いリポでは全部、あるリポでは問題のある置き場だけを書く。
func repoLine(ri RepoInfo) string {
	var sb strings.Builder
	fmt.Fprintf(&sb, "%s: %d 件", ri.Name, ri.Entries)
	if len(ri.Kinds) > 0 {
		parts := make([]string, 0, len(ri.Kinds))
		for _, k := range ri.Kinds {
			parts = append(parts, fmt.Sprintf("%s %d", k.Kind, k.Entries))
		}
		sb.WriteString("（" + strings.Join(parts, "・") + "）")
	}
	var notes []string
	for _, p := range ri.Places {
		if ri.Entries == 0 || p.Status != "ok" {
			notes = append(notes, placeWords(p))
		}
	}
	if len(notes) > 0 {
		sb.WriteString(" — " + strings.Join(notes, "・"))
	}
	if ri.Gaps > 0 {
		fmt.Fprintf(&sb, "。読めなかった範囲 %d", ri.Gaps)
	}
	return sb.String()
}

// placeWords は置き場の状態を短く言う。
func placeWords(p PlaceInfo) string {
	switch p.Status {
	case "ok":
		if p.Path == "docs/decisions.md" {
			return p.Path + " あり"
		}
		if p.Entries == 0 {
			return p.Path + " あり（索引に載る .md 無し）"
		}
		return fmt.Sprintf("%s あり（%d 件）", p.Path, p.Entries)
	case "missing":
		return p.Path + " 無し"
	case "unreadable":
		return p.Path + " 読めなかった"
	case "not_dir":
		return p.Path + " はディレクトリではない"
	case "is_dir":
		return p.Path + " はディレクトリ（載らない）"
	}
	return p.Path + " " + p.Status
}

// writeDiffList は差の一覧を 1 種類書く。件数 0 なら書かない。paths か entries のどちらかを渡す。
func writeDiffList(b *strings.Builder, label, note string, paths []string, entries []DiffEntry) {
	n := len(paths) + len(entries)
	if n == 0 {
		return
	}
	fmt.Fprintf(b, "  - %s %d 件（%s）\n", label, n, note)
	for _, p := range paths {
		b.WriteString("    - " + p + "\n")
	}
	for _, e := range entries {
		b.WriteString("    - " + e.Path + " — " + e.Reason + "\n")
	}
}
