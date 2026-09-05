package template

import (
	"bytes"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"github.com/pilefort/braindex/internal/config"
)

// 雛形の braindex.json は、全機能を足して組み立てた結果とバイト一致する。設定の雛形は 1 枚だけを正とし
// (決定 2026-09-03)、節はそこから切り出すので、雛形を直したらこの整形(固定キー順・インデント 2)に揃える。
func TestBuildConfig_AllEqualsTemplate(t *testing.T) {
	got, changed, err := BuildConfig(nil, []Feature{FeatureAll})
	if err != nil {
		t.Fatal(err)
	}
	if !changed {
		t.Error("空から組み立てたのに changed=false")
	}
	tmpl, err := templates.ReadFile("templates/hub/braindex.json")
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, tmpl) {
		t.Errorf("雛形の braindex.json が組み立て結果と違う(雛形を固定キー順・インデント 2 に揃える):\n--- 組み立て\n%s\n--- 雛形\n%s", got, tmpl)
	}
}

// 節ごとの組み立て: core は root・notes_dirs・extra だけ。schedule の jobs は足した機能の分だけ。
// できた設定はどれも config.Load で読める(未知キーが無い)。
func TestBuildConfig_Sections(t *testing.T) {
	keys := func(b []byte) []string {
		var m map[string]json.RawMessage
		if err := json.Unmarshal(b, &m); err != nil {
			t.Fatalf("読めない: %v\n%s", err, b)
		}
		var ks []string
		for _, k := range configKeyOrder {
			if _, ok := m[k]; ok {
				ks = append(ks, k)
			}
		}
		return ks
	}
	jobs := func(b []byte) string {
		var c struct {
			Schedule struct {
				Jobs []struct{ Name string } `json:"jobs"`
			} `json:"schedule"`
		}
		if err := json.Unmarshal(b, &c); err != nil {
			t.Fatal(err)
		}
		var names []string
		for _, j := range c.Schedule.Jobs {
			names = append(names, j.Name)
		}
		return strings.Join(names, ",")
	}
	cases := []struct {
		in   []Feature
		keys string
		jobs string
	}{
		{nil, "root,notes_dirs,extra", ""},
		{[]Feature{FeatureConventions}, "root,notes_dirs,extra,approvals", ""},
		{[]Feature{FeatureReview}, "root,notes_dirs,extra,review,approvals", ""},
		{[]Feature{FeatureSchedule}, "root,notes_dirs,extra,schedule", ""},
		{[]Feature{FeatureSchedule, FeatureRetro}, "root,notes_dirs,extra,retro,schedule", "retro"},
		{[]Feature{FeatureSchedule, FeatureReview}, "root,notes_dirs,extra,review,approvals,schedule", "review"},
		{[]Feature{FeatureAll}, "root,notes_dirs,extra,review,retro,approvals,news,schedule", "review,retro"},
	}
	dir := t.TempDir()
	for i, c := range cases {
		b, _, err := BuildConfig(nil, c.in)
		if err != nil {
			t.Fatalf("%v: %v", c.in, err)
		}
		if got := strings.Join(keys(b), ","); got != c.keys {
			t.Errorf("%v: keys=%s want %s", c.in, got, c.keys)
		}
		if got := jobs(b); got != c.jobs {
			t.Errorf("%v: jobs=%s want %s", c.in, got, c.jobs)
		}
		p := filepath.Join(dir, "cfg"+string(rune('a'+i))+".json")
		writeAt(t, dir, filepath.Base(p), b)
		if _, found, err := config.Load(p); err != nil || !found {
			t.Errorf("%v: config.Load が読めない: found=%v err=%v", c.in, found, err)
		}
	}
	// schedule の jobs が空でも "jobs": [] と書く(節が無いのと違い、既定の 2 本にならない)
	b, _, err := BuildConfig(nil, []Feature{FeatureSchedule})
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(b, []byte(`"jobs": []`)) {
		t.Errorf("schedule だけのとき jobs が空配列でない:\n%s", b)
	}
}

// 既存の設定に節を足す: 既にあるキーは値も位置も変えず、無い節だけ固定順の位置に入る。
// 利用者が足した未知のキーも落とさない(末尾に名前順)。数値の表記(0.08)も触らない。
func TestBuildConfig_KeepsExisting(t *testing.T) {
	existing := []byte(`{
  "notes_dirs": ["notes", "docs/notes"],
  "root": "/home/me/src",
  "retro": {"threshold": 0.10, "window_days": 7},
  "zzz_mine": {"k": 1},
  "aaa_mine": true
}
`)
	got, changed, err := BuildConfig(existing, []Feature{FeatureRetro, FeatureNews})
	if err != nil {
		t.Fatal(err)
	}
	if !changed {
		t.Error("news を足したのに changed=false")
	}
	want := `{
  "root": "/home/me/src",
  "notes_dirs": [
    "notes",
    "docs/notes"
  ],
  "extra": [],
  "retro": {
    "threshold": 0.10,
    "window_days": 7
  },
  "news": {
`
	if !strings.HasPrefix(string(got), want) {
		t.Errorf("既存キーの値・順序が保たれていない:\n--- got\n%s\n--- want(先頭)\n%s", got, want)
	}
	if !strings.HasSuffix(string(got), "  \"aaa_mine\": true,\n  \"zzz_mine\": {\n    \"k\": 1\n  }\n}\n") {
		t.Errorf("未知のキーが末尾に名前順で残っていない:\n%s", got)
	}

	// 全部揃っていれば changed=false。整形だけは揃える
	got2, changed, err := BuildConfig(got, []Feature{FeatureRetro, FeatureNews})
	if err != nil {
		t.Fatal(err)
	}
	if changed || !bytes.Equal(got, got2) {
		t.Errorf("2 回目: changed=%v equal=%v", changed, bytes.Equal(got, got2))
	}
}

// 同じ入力からは常に同じバイト列(決定性)。空からでも既存からでも。
func TestBuildConfig_Deterministic(t *testing.T) {
	for _, in := range [][]Feature{nil, {FeatureReview}, {FeatureAll}} {
		var outs [2][]byte
		for i := range outs {
			b, _, err := BuildConfig([]byte(`{"root": "x", "extra": ["a"]}`), in)
			if err != nil {
				t.Fatal(err)
			}
			outs[i] = b
		}
		if !bytes.Equal(outs[0], outs[1]) {
			t.Errorf("%v: 2 回の結果が違う", in)
		}
	}
}

// 壊れた既存ファイルはエラー(黙って作り直さない)。
func TestBuildConfig_InvalidExisting(t *testing.T) {
	if _, _, err := BuildConfig([]byte(`{"root": `), nil); err == nil {
		t.Error("壊れた JSON でエラーにならない")
	}
	if _, _, err := BuildConfig([]byte(`[1]`), nil); err == nil {
		t.Error("オブジェクトでない JSON でエラーにならない")
	}
	// null は json.Unmarshal がエラーにせず map を nil にする。panic せずエラーにする
	if _, _, err := BuildConfig([]byte(`null`), nil); err == nil {
		t.Error("null でエラーにならない")
	}
}
