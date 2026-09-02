package approvals

import (
	"fmt"
	"strings"
)

// ApplyResult は Apply の出力。Approvals と Decisions は書き戻す新しい内容。
type ApplyResult struct {
	Approvals []byte   // 答えた項目を消し、保留に印を付け、番号を振り直した APPROVALS.md
	Decisions []byte   // 決定を 3 段で末尾に追記した decisions.md(決定が無ければ入力のまま)
	Summary   []string // 1 項目 1 行の要約(警告も含む)
	Decided   int      // decisions に移した件数
	Held      int      // 保留にした件数
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
	byTitle := map[string]*Item{}
	byN := map[int]*Item{}
	for i := range d.Items {
		byTitle[d.Items[i].Title] = &d.Items[i]
		byN[d.Items[i].N] = &d.Items[i]
	}
	var res ApplyResult
	decided := map[string]bool{}
	holds := map[string]string{}
	var entries []string
	for _, r := range rep.Items {
		title := strings.TrimSpace(r.Title)
		it := byTitle[title]
		if it == nil && title == "" {
			it = byN[r.N]
		}
		if it == nil {
			res.Summary = append(res.Summary, fmt.Sprintf("警告: 回答の項目 [%d] %s が APPROVALS.md に見つからない → 未反映", r.N, title))
			continue
		}
		choice := strings.TrimSpace(r.Choice)
		comment := strings.TrimSpace(r.Comment)
		if choice == "hold" || (choice == "other" && comment == "") {
			note := comment
			if choice == "other" {
				note = "「その他」だがコメント無し → 保留扱い"
			}
			holds[it.Title] = note
			res.Held++
			res.Summary = append(res.Summary, fmt.Sprintf("[%d] %s → 保留（%s）", it.N, it.Title, note))
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
		evidence := fmt.Sprintf("会話 %s（ユーザー判断・承認フォームの回答 %s）", today, rep.ReceivedAt)
		if why := it.Fields[FieldWhyNow]; why != "" {
			evidence += "。なぜ今決めたか: " + sentence(strings.Join(nonEmptyLines(why), " "))
		}
		evidence += "。文面は braindex approvals apply の機械生成（結論文は整えてよい）"
		entries = append(entries, fmt.Sprintf("\n## %s\n\n記録日: %s\n理由: %s\n根拠: %s\n", heading, today, reason, evidence))
		decided[it.Title] = true
		res.Decided++
	}

	// APPROVALS.md を組み立て直す
	var remaining []string
	for _, it := range d.Items {
		if decided[it.Title] {
			continue
		}
		raw := it.Raw
		if note, ok := holds[it.Title]; ok {
			raw = strings.TrimRight(raw, "\n") + "\n**保留（" + today + "）:** " + note + "\n"
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
