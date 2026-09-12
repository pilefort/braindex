package approvals

import (
	"fmt"
	"strings"
)

// ApplyResult は Apply の出力。Approvals と Decisions は書き戻す新しい内容。
type ApplyResult struct {
	Approvals         []byte   // 答えた項目を消し、保留に印を付け、番号を振り直した APPROVALS.md
	Decisions         []byte   // 決定を 3 段で末尾に追記した decisions.md(決定が無ければ入力のまま)
	Summary           []string // 1 項目 1 行の要約(警告も含む)
	Decided           int      // decisions に移した件数
	Held              int      // 保留にした件数
	DuplicateHeadings []string // 追記しようとした見出しが decisions.md に既にあったもの(重複の可能性。追記は止めない)
}

// Apply は回答を APPROVALS.md と decisions.md に反映する。純粋関数(ファイルは触らない)。
//
// 選択肢を選んだ項目 → decisions.md に 3 段(結論 → 理由 → 根拠)で追記し、APPROVALS.md から消す。
// その他(コメントあり) → コメントの 1 行目を結論に。
// 保留、またはその他でコメント無し → 項目を残し「**保留（日付）:** コメント」を付ける。
// 残った項目は 1 から番号を振り直す。全部消えたら「（なし。…）」だけにする。
//
// 追記の文面は機械生成なので、見出しは「<決めたいこと> → <選んだ案>」の形にとどめ、却下した選択肢は得失ごと全部
// 転記する(work/ を git 管理しない hub では APPROVALS.md の履歴が残らないため、ここに残さないと失われる)。
func Apply(approvalsMD, decisionsMD []byte, rep Reply, today string) ApplyResult {
	d := Parse(approvalsMD)
	// 見出しの題も番号も重なりうる(番号は書き手が振るだけ)ので、引き当ても消し込みも添字で持つ。
	// 題で消し込むと、同じ題の答えていない項目まで一緒に消える。
	byTitle := map[string]int{}
	byN := map[int]int{}
	for i := range d.Items {
		if _, dup := byTitle[d.Items[i].Title]; !dup {
			byTitle[d.Items[i].Title] = i
		}
		if _, dup := byN[d.Items[i].N]; !dup {
			byN[d.Items[i].N] = i
		}
	}
	var res ApplyResult
	decided := map[int]bool{}
	holds := map[int]string{}
	var entries []string
	// 既存の見出し(decisions.md に既にあるもの)。同じ判断を 2 回積んで気づけない事故を防ぐため、
	// これから追記する見出しと重ならないかを見る(追記は止めない。止めると回答が失われるため)。
	existingHeadings := decisionHeadings(decisionsMD)
	for _, r := range rep.Items {
		title := strings.TrimSpace(r.Title)
		// 回答は番号と題の両方を持つ。番号の指す項目の題が一致すればそれ、違えば題で引く。
		idx := -1
		if i, ok := byN[r.N]; ok && (title == "" || d.Items[i].Title == title) {
			idx = i
		} else if i, ok := byTitle[title]; ok && title != "" {
			idx = i
		}
		if idx < 0 {
			res.Summary = append(res.Summary, fmt.Sprintf("警告: 回答の項目 [%d] %s が APPROVALS.md に見つからない → 未反映", r.N, title))
			continue
		}
		it := &d.Items[idx]
		choice := strings.TrimSpace(r.Choice)
		comment := strings.TrimSpace(r.Comment)
		if choice == "hold" || (choice == "other" && comment == "") {
			note := comment
			if choice == "other" {
				note = "「その他」だがコメント無し → 保留扱い"
			}
			holds[idx] = note
			res.Held++
			line := fmt.Sprintf("[%d] %s → 保留", it.N, it.Title)
			if note != "" {
				line += "（" + note + "）"
			}
			res.Summary = append(res.Summary, line)
			continue
		}
		var heading, reason string
		var rejected []Option
		if choice == "other" {
			lines := nonEmptyLines(comment)
			heading = it.Title + " → " + lines[0]
			reason = sentence(strings.Join(lines[1:], " "))
			if reason == "" {
				reason = "（結論のみ記載・補足なし）"
			}
			rejected = it.Options
			res.Summary = append(res.Summary, fmt.Sprintf("[%d] %s → その他: %s", it.N, it.Title, strings.Join(lines, " ")))
		} else {
			opt, ok := it.Option(strings.ToUpper(choice))
			if !ok {
				res.Summary = append(res.Summary, fmt.Sprintf("警告: [%d] %s の選択 %q が選択肢に無い → 未反映", it.N, it.Title, choice))
				continue
			}
			heading = it.Title + " → " + opt.Key + ". " + opt.Label
			var parts []string
			if opt.Desc != "" {
				parts = append(parts, sentence(opt.Desc))
			}
			if it.Recommended == opt.Key && it.Reason != "" {
				parts = append(parts, "私の案の理由: "+sentence(it.Reason))
			}
			if comment != "" {
				parts = append(parts, "補足: "+sentence(strings.Join(nonEmptyLines(comment), " ")))
			}
			if len(parts) == 0 {
				parts = append(parts, "（選択のみ・補足なし）")
			}
			reason = strings.Join(parts, "。")
			for _, o := range it.Options {
				if o.Key != opt.Key {
					rejected = append(rejected, o)
				}
			}
			s := fmt.Sprintf("[%d] %s → %s. %s", it.N, it.Title, opt.Key, opt.Label)
			if comment != "" {
				s += "（" + strings.Join(nonEmptyLines(comment), " ") + "）"
			}
			res.Summary = append(res.Summary, s)
			if it.Recommended != "" && it.Recommended != opt.Key {
				reason += "（私の案 " + it.Recommended + " は不採用）"
			}
		}
		if len(rejected) > 0 {
			var rs []string
			for _, o := range rejected {
				s := o.Key + ". " + o.Label
				if o.Desc != "" {
					s += "（" + o.Desc + "）"
				}
				rs = append(rs, s)
			}
			reason += "。却下: " + strings.Join(rs, "／")
		}
		evidence := fmt.Sprintf("会話 %s（ユーザー判断・承認フォームの回答）", today)
		if rep.ReceivedAt != "" { // 手で書いた回答 JSON には受信時刻が無い
			evidence = fmt.Sprintf("会話 %s（ユーザー判断・承認フォームの回答 %s）", today, rep.ReceivedAt)
		}
		if why := it.Fields[FieldWhyNow]; why != "" {
			evidence += "。なぜ今決めたか: " + sentence(strings.Join(nonEmptyLines(why), " "))
		}
		evidence += "。文面は braindex approvals apply の機械生成（結論文は整えてよい）"
		if existingHeadings[heading] {
			res.DuplicateHeadings = append(res.DuplicateHeadings, heading)
		}
		existingHeadings[heading] = true // 同じ回答の中で同じ見出しが重なる場合も検知する
		entries = append(entries, fmt.Sprintf("\n## %s\n\n記録日: %s\n理由: %s\n根拠: %s\n", heading, today, reason, evidence))
		decided[idx] = true
		res.Decided++
	}

	// 全件未反映なら番号や改行も含めて入力を保つ。
	if res.Decided == 0 && res.Held == 0 {
		res.Approvals, res.Decisions = approvalsMD, decisionsMD
		return res
	}

	// APPROVALS.md を組み立て直す
	var remaining []string
	for i, it := range d.Items {
		if decided[i] {
			continue
		}
		raw := it.Raw
		if note, ok := holds[i]; ok {
			// コメント無しのときに行末へ空白を残さない(Markdown の行末空白は強制改行になり、diff にも出る)
			hold := strings.TrimRight("**保留（"+today+"）:** "+note, " ")
			if !strings.Contains("\n"+raw, "\n"+hold+"\n") {
				raw = strings.TrimRight(raw, "\n") + "\n" + hold + "\n"
			}
		}
		remaining = append(remaining, raw)
	}
	if len(remaining) > 0 {
		pre := strings.TrimSpace(stripNone(d.Preamble))
		if pre == "" {
			pre = "# 承認待ち"
		}
		var parts []string
		for i, raw := range remaining {
			first, rest, _ := strings.Cut(raw, "\n")
			if m := headRe.FindStringSubmatch(first); m != nil {
				first = fmt.Sprintf("## %d. %s", i+1, m[2])
			}
			parts = append(parts, strings.TrimRight(first+"\n"+rest, "\n")+"\n")
		}
		res.Approvals = []byte(pre + "\n\n" + strings.Join(parts, "\n"))
	} else {
		res.Approvals = []byte(fmt.Sprintf("# 承認待ち\n\n（なし。%s に承認フォームの回答で %d 件を docs/decisions.md へ移動）\n", today, res.Decided))
	}

	res.Decisions = decisionsMD
	if len(entries) > 0 {
		base := strings.TrimRight(string(decisionsMD), "\n")
		if strings.TrimSpace(base) == "" {
			base = "# 設計判断"
		}
		res.Decisions = []byte(base + "\n" + strings.Join(entries, ""))
	}
	return res
}

// decisionHeadings は decisions.md に既にある見出し(## の後ろ)を集める。
// Apply が追記しようとする見出しと重ならないかを見るため(見出しの形式は headRe と共通)。
func decisionHeadings(decisionsMD []byte) map[string]bool {
	out := map[string]bool{}
	for _, line := range strings.Split(string(decisionsMD), "\n") {
		if m := headRe.FindStringSubmatch(line); m != nil {
			out[strings.TrimSpace(m[2])] = true
		}
	}
	return out
}

// sentence は文の末尾の句点と空白を落とす(「。」で連結したとき「。。」にしない)。
func sentence(s string) string {
	return strings.TrimRight(strings.TrimSpace(s), "。 ")
}

func nonEmptyLines(s string) []string {
	var out []string
	for _, l := range strings.Split(s, "\n") {
		if t := strings.TrimSpace(l); t != "" {
			out = append(out, t)
		}
	}
	if len(out) == 0 {
		out = append(out, "")
	}
	return out
}

// stripNone は前文から「（なし…」の行を除く(項目が残るときは不要)。
func stripNone(pre string) string {
	var out []string
	for _, l := range strings.Split(pre, "\n") {
		if strings.HasPrefix(strings.TrimSpace(l), "（なし") {
			continue
		}
		out = append(out, l)
	}
	return strings.Join(out, "\n")
}
