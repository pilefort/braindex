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
// schedule の jobs は、設定に節のある機能(review・retro・news)の分だけ持つ。job を足すのは「その機能の節を今回足した」
// ときと「schedule 節を今回足した」ときだけで、既にある job は触らず、利用者が消した job を足し直すこともない
// (入口の設計 2026-09-05「review は足したら加える」)。
// feats は Resolve で core と依存を足してから使う(all もここで展開される)。
func BuildConfig(existing []byte, feats []Feature) (out []byte, changed bool, err error) {
	if err := checkFeatures(feats); err != nil {
		return nil, false, err
	}
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
	added := map[string]bool{} // 今回足した節
	for _, f := range feats {
		for _, key := range features[f].Sections {
			if _, ok := cur[key]; ok {
				continue
			}
			v, ok := tmpl[key]
			if !ok {
				return nil, false, fmt.Errorf("雛形の %s に節 %q が無い", ConfigPath, key)
			}
			if key == "schedule" {
				v = []byte(`{"jobs": []}`) // job は下で、節のある機能の分だけ足す
			}
			cur[key] = v
			added[key] = true
		}
	}
	if len(added) > 0 {
		changed = true
	}
	if _, ok := cur["schedule"]; ok {
		jobsAdded, err := addScheduleJobs(cur, tmpl, added)
		if err != nil {
			return nil, false, err
		}
		changed = changed || jobsAdded
	}
	out, err = renderConfig(cur)
	return out, changed, err
}

// scheduleJobFeatures は雛形の job 名と、その job が要る設定の節。
var scheduleJobFeatures = map[string]string{"review": "review", "retro": "retro", "news": "news"}

// addScheduleJobs は cur の schedule.jobs に、雛形の job のうち「対応する節が cur にあり、同名の job がまだ無く、
// その節か schedule 節を今回足した」ものを末尾に足す。既にある job は触らない。
func addScheduleJobs(cur, tmpl map[string]json.RawMessage, added map[string]bool) (bool, error) {
	var sec map[string]json.RawMessage
	if err := json.Unmarshal(cur["schedule"], &sec); err != nil {
		return false, fmt.Errorf("schedule 節: %w", err)
	}
	if sec == nil {
		return false, fmt.Errorf("schedule 節がオブジェクトでない")
	}
	var jobs []json.RawMessage
	if raw, ok := sec["jobs"]; ok && len(bytes.TrimSpace(raw)) > 0 && string(bytes.TrimSpace(raw)) != "null" {
		if err := json.Unmarshal(raw, &jobs); err != nil {
			return false, fmt.Errorf("schedule.jobs: %w", err)
		}
	}
	have := map[string]bool{}
	for _, j := range jobs {
		var job struct {
			Name string `json:"name"`
		}
		if err := json.Unmarshal(j, &job); err != nil {
			return false, fmt.Errorf("schedule.jobs: %w", err)
		}
		have[job.Name] = true
	}
	var tsec struct {
		Jobs []json.RawMessage `json:"jobs"`
	}
	if err := json.Unmarshal(tmpl["schedule"], &tsec); err != nil {
		return false, fmt.Errorf("雛形の schedule 節: %w", err)
	}
	changed := false
	for _, j := range tsec.Jobs {
		var job struct {
			Name string `json:"name"`
		}
		if err := json.Unmarshal(j, &job); err != nil {
			return false, fmt.Errorf("雛形の schedule.jobs: %w", err)
		}
		key, known := scheduleJobFeatures[job.Name]
		if !known || have[job.Name] {
			continue
		}
		if _, ok := cur[key]; !ok {
			continue
		}
		if !added[key] && !added["schedule"] {
			continue // 以前からある機能の job は、利用者が消したかもしれないので足し直さない
		}
		jobs = append(jobs, j)
		changed = true
	}
	if !changed {
		return false, nil
	}
	if jobs == nil {
		jobs = []json.RawMessage{}
	}
	b, err := json.Marshal(jobs)
	if err != nil {
		return false, err
	}
	sec["jobs"] = b
	out, err := json.Marshal(sec) // map のキーは昇順に出る(いまは jobs だけ)
	if err != nil {
		return false, err
	}
	cur["schedule"] = out
	return true, nil
}

// MissingConfigKeys は cfg(節を足したあとの braindex.json)に対し、feats の節のうち雛形の節の中にあって cfg に無いキーを
// 「節.キー」の形で返す(昇順)。節そのものが無いものは含めない(BuildConfig が足す)。配列(schedule.jobs)の中は見ない。
// update が「.new を置く必要があるか」を決めるのに使う(決定 2026-09-05)。
func MissingConfigKeys(cfg []byte, feats []Feature) ([]string, error) {
	feats, _ = Resolve(feats)
	var cur map[string]json.RawMessage
	if err := json.Unmarshal(cfg, &cur); err != nil {
		return nil, fmt.Errorf("%s: %w", ConfigPath, err)
	}
	tmpl, err := configSections()
	if err != nil {
		return nil, err
	}
	var missing []string
	for _, f := range feats {
		for _, key := range features[f].Sections {
			tv, ok := tmpl[key]
			if !ok {
				continue
			}
			cv, ok := cur[key]
			if !ok {
				continue
			}
			var tm, cm map[string]json.RawMessage
			if json.Unmarshal(tv, &tm) != nil || tm == nil {
				continue // 節がオブジェクトでない(root など)
			}
			if json.Unmarshal(cv, &cm) != nil || cm == nil {
				continue // 利用者の値がオブジェクトでないなら比べようがない
			}
			for k := range tm {
				if _, ok := cm[k]; !ok {
					missing = append(missing, key+"."+k)
				}
			}
		}
	}
	sort.Strings(missing)
	return missing, nil
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
