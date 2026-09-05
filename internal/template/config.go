package template

import (
	"bytes"
	"encoding/json"
	"fmt"
	"path"
	"sort"
)

// ConfigPath は hub の設定ファイル(展開先からの相対)。
const ConfigPath = "braindex.json"

// configKeyOrder は braindex.json の最上位キーの固定順。テンプレ(templates/hub/braindex.json)と同じ順で、
// 節を足しても位置が揺れない(利用者の diff を小さく保つ)。ここに無いキーは末尾に名前順で並べる。
var configKeyOrder = []string{"root", "notes_dirs", "extra", "review", "retro", "approvals", "news", "schedule"}

// configSections はテンプレの braindex.json を最上位キーごとに切り出す。テンプレは「全機能を足した完成形」
// 1 枚だけを正として持ち(決定 2026-09-03「設定の雛形はテンプレ 1 つ」)、機能ごとの節はそこから取る。
func configSections() (map[string]json.RawMessage, error) {
	b, err := templates.ReadFile(path.Join("templates", string(KindHub), ConfigPath))
	if err != nil {
		return nil, err
	}
	var m map[string]json.RawMessage
	if err := json.Unmarshal(b, &m); err != nil {
		return nil, fmt.Errorf("雛形の %s: %w", ConfigPath, err)
	}
	return m, nil
}

// BuildConfig は existing(hub の今の braindex.json。無ければ nil)に feats の節を足して返す。
//
// 既にあるキーは値を変えない(利用者の編集を守る)。足すのは無いキーだけで、書き戻しは固定のキー順・
// インデント 2・末尾改行 1 つ。同じ入力なら常に同じバイト列になる。changed は 1 つでも節を足したか。
// existing が既に全部の節を持てば、内容は同じでも整形だけは揃えて返す(changed=false)。
// schedule の jobs は review・retro のうち設定に節があるもの(今回足す分と、以前に足した分)だけにする
// (無い機能の定期実行を配らないため)。
// feats は Resolve で core と依存を足してから使う(all もここで展開される)。
func BuildConfig(existing []byte, feats []Feature) (out []byte, changed bool, err error) {
	feats, _ = Resolve(feats)
	cur := map[string]json.RawMessage{}
	if len(bytes.TrimSpace(existing)) > 0 {
		if err := json.Unmarshal(existing, &cur); err != nil {
			return nil, false, fmt.Errorf("%s: %w", ConfigPath, err)
		}
		if cur == nil { // JSON の null。Unmarshal はエラーにせず map を nil にする
			return nil, false, fmt.Errorf("%s: オブジェクトでない(null)", ConfigPath)
		}
	}
	tmpl, err := configSections()
	if err != nil {
		return nil, false, err
	}
	for _, f := range feats {
		spec, ok := features[f]
		if !ok {
			return nil, false, fmt.Errorf("未知の機能 %q", f)
		}
		for _, key := range spec.Sections {
			if _, ok := cur[key]; ok {
				continue
			}
			v, ok := tmpl[key]
			if !ok {
				return nil, false, fmt.Errorf("雛形の %s に節 %q が無い", ConfigPath, key)
			}
			if key == "schedule" {
				// review・retro の節はこのループで schedule より先に入る(featureOrder の順)ので、
				// 今回足す分も、以前に足してあった分も cur を見れば分かる
				if v, err = filterScheduleJobs(v, cur); err != nil {
					return nil, false, err
				}
			}
			cur[key] = v
			changed = true
		}
	}
	out, err = renderConfig(cur)
	return out, changed, err
}

// filterScheduleJobs は schedule 節の jobs を、設定に節のある機能のものだけに絞る
// (name が review なら review 節、retro なら retro 節が要る)。他の名前の job は残す。
func filterScheduleJobs(raw json.RawMessage, cur map[string]json.RawMessage) (json.RawMessage, error) {
	var sec struct {
		Jobs []json.RawMessage `json:"jobs"`
	}
	if err := json.Unmarshal(raw, &sec); err != nil {
		return nil, fmt.Errorf("雛形の schedule 節: %w", err)
	}
	kept := []json.RawMessage{}
	for _, j := range sec.Jobs {
		var job struct {
			Name string `json:"name"`
		}
		if err := json.Unmarshal(j, &job); err != nil {
			return nil, fmt.Errorf("雛形の schedule.jobs: %w", err)
		}
		if job.Name == "review" || job.Name == "retro" {
			if _, ok := cur[job.Name]; !ok {
				continue
			}
		}
		kept = append(kept, j)
	}
	// 節の他のキーは触らない(いまは jobs だけだが、増えても落とさない)
	var m map[string]json.RawMessage
	if err := json.Unmarshal(raw, &m); err != nil {
		return nil, err
	}
	jobs, err := json.Marshal(kept)
	if err != nil {
		return nil, err
	}
	m["jobs"] = jobs
	return json.Marshal(m) // map のキーは昇順に出る(いまは jobs だけ)
}

// renderConfig は最上位キーを固定順に並べて書き出す。値は json.Indent で整形するだけで、
// 中身(数値の表記・入れ子のキー順)は入力のまま。
func renderConfig(m map[string]json.RawMessage) ([]byte, error) {
	var keys []string
	seen := map[string]bool{}
	for _, k := range configKeyOrder {
		if _, ok := m[k]; ok {
			keys = append(keys, k)
			seen[k] = true
		}
	}
	var rest []string
	for k := range m {
		if !seen[k] {
			rest = append(rest, k)
		}
	}
	sort.Strings(rest)
	keys = append(keys, rest...)

	var buf bytes.Buffer
	buf.WriteString("{\n")
	for i, k := range keys {
		kb, err := json.Marshal(k)
		if err != nil {
			return nil, err
		}
		// Compact で入力の空白・改行を落としてから Indent する(RawMessage は元の整形を保っているため)
		var cb, vb bytes.Buffer
		if err := json.Compact(&cb, m[k]); err != nil {
			return nil, fmt.Errorf("%s の %q: %w", ConfigPath, k, err)
		}
		if err := json.Indent(&vb, cb.Bytes(), "  ", "  "); err != nil {
			return nil, fmt.Errorf("%s の %q: %w", ConfigPath, k, err)
		}
		buf.WriteString("  ")
		buf.Write(kb)
		buf.WriteString(": ")
		buf.Write(vb.Bytes())
		if i < len(keys)-1 {
			buf.WriteByte(',')
		}
		buf.WriteByte('\n')
	}
	buf.WriteString("}\n")
	return buf.Bytes(), nil
}
