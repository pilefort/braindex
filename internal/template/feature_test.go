package template

import (
	"bytes"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// 対応表は hub の雛形を漏れなく・重複なく覆う。雛形にファイルを足したら、どの機能で配るかを
// 対応表に書かないとこのテストが落ちる(黙って all だけに入る、を防ぐ)。
func TestFeatures_CoverAllHubFiles(t *testing.T) {
	all, err := Files(KindHub)
	if err != nil {
		t.Fatal(err)
	}
	owner := map[string]Feature{}
	for _, f := range FeatureList() {
		_, files, _, _, ok := FeatureInfo(f)
		if !ok {
			t.Fatalf("FeatureInfo(%s) が無い", f)
		}
		for _, p := range files {
			if prev, dup := owner[p]; dup {
				t.Errorf("%s が %s と %s の両方に属している", p, prev, f)
			}
			owner[p] = f
		}
	}
	for _, f := range all {
		if _, ok := owner[f.Path]; !ok {
			t.Errorf("雛形の %s がどの機能にも属していない", f.Path)
		}
	}
	for p := range owner {
		found := false
		for _, f := range all {
			if f.Path == p {
				found = true
			}
		}
		if !found {
			t.Errorf("対応表の %s が雛形に無い", p)
		}
	}
}

// 設定の節も、雛形の braindex.json の最上位キーを漏れなく・重複なく覆う。
func TestFeatures_CoverAllConfigSections(t *testing.T) {
	tmpl, err := configSections()
	if err != nil {
		t.Fatal(err)
	}
	owner := map[string]Feature{}
	for _, f := range FeatureList() {
		_, _, secs, _, _ := FeatureInfo(f)
		for _, k := range secs {
			if prev, dup := owner[k]; dup {
				t.Errorf("節 %s が %s と %s の両方に属している", k, prev, f)
			}
			owner[k] = f
			if _, ok := tmpl[k]; !ok {
				t.Errorf("対応表の節 %s が雛形の braindex.json に無い", k)
			}
		}
	}
	for k := range tmpl {
		if _, ok := owner[k]; !ok {
			t.Errorf("雛形の節 %s がどの機能にも属していない", k)
		}
	}
	// 固定順に無いキーは末尾に回る。雛形のキーは全部固定順に載っているべき
	for k := range tmpl {
		if !has(configKeyOrder, k) {
			t.Errorf("節 %s が configKeyOrder に無い", k)
		}
	}
}

// Resolve: core は常に入り、review は conventions を連れてくる。足した依存は added に出る。all は全部。
func TestResolve(t *testing.T) {
	join := func(fs []Feature) string {
		var s []string
		for _, f := range fs {
			s = append(s, string(f))
		}
		return strings.Join(s, ",")
	}
	cases := []struct {
		in          []Feature
		want, added string
	}{
		{nil, "core", ""},
		{[]Feature{FeatureCore}, "core", ""},
		{[]Feature{FeatureRetro}, "core,retro", ""},
		{[]Feature{FeatureReview}, "core,conventions,review", "conventions"},
		{[]Feature{FeatureReview, FeatureConventions}, "core,conventions,review", ""},
		{[]Feature{FeatureSchedule, FeatureNews}, "core,news,schedule", ""},
		{[]Feature{FeatureAll}, "core,conventions,review,retro,news,schedule", ""},
		{[]Feature{FeatureRetro, FeatureRetro}, "core,retro", ""},
	}
	for _, c := range cases {
		got, added := Resolve(c.in)
		if join(got) != c.want || join(added) != c.added {
			t.Errorf("Resolve(%v): got=%s added=%s want=%s/%s", c.in, join(got), join(added), c.want, c.added)
		}
	}
	// 2 回通しても同じ(冪等)
	once, _ := Resolve([]Feature{FeatureReview})
	twice, added := Resolve(once)
	if join(once) != join(twice) || len(added) != 0 {
		t.Errorf("Resolve が冪等でない: %s → %s (added=%v)", join(once), join(twice), added)
	}
}

// ParseFeatures: カンマ区切り・空白・空要素を許し、未知の名前は候補つきでエラー。
func TestParseFeatures(t *testing.T) {
	got, err := ParseFeatures(" conventions, review ,,")
	if err != nil || len(got) != 2 || got[0] != FeatureConventions || got[1] != FeatureReview {
		t.Errorf("ParseFeatures: got=%v err=%v", got, err)
	}
	if got, err := ParseFeatures("all"); err != nil || len(got) != 1 || got[0] != FeatureAll {
		t.Errorf("all: got=%v err=%v", got, err)
	}
	for _, bad := range []string{"nope", "", "review,nope", "core,ALL"} {
		_, err := ParseFeatures(bad)
		if err == nil {
			t.Errorf("%q でエラーにならない", bad)
			continue
		}
		if !strings.Contains(err.Error(), "conventions") || !strings.Contains(err.Error(), "all") {
			t.Errorf("%q のエラーに候補が無い: %v", bad, err)
		}
	}
}

// FeatureNames は台帳に書く形(core を除き昇順)。
func TestFeatureNames(t *testing.T) {
	feats, _ := Resolve([]Feature{FeatureSchedule, FeatureReview})
	got := strings.Join(FeatureNames(feats), ",")
	if got != "conventions,review,schedule" {
		t.Errorf("FeatureNames=%s", got)
	}
	if n := FeatureNames([]Feature{FeatureCore}); len(n) != 0 {
		t.Errorf("core だけなら空: %v", n)
	}
}

// FeatureFiles: core だけなら 4 ファイル。review は conventions の分も連れてくる。all は雛形の全ファイル。
func TestFeatureFiles(t *testing.T) {
	paths := func(fs []File) []string {
		var out []string
		for _, f := range fs {
			out = append(out, f.Path)
		}
		return out
	}
	core, err := FeatureFiles(nil)
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(paths(core), ","); got != ".gitattributes,CLAUDE.md,README.md,braindex.json" {
		t.Errorf("core の配布物: %s", got)
	}
	rev, err := FeatureFiles([]Feature{FeatureReview})
	if err != nil {
		t.Fatal(err)
	}
	ps := paths(rev)
	for _, want := range []string{"docs/conventions.md", "work/APPROVALS.md", ".claude/skills/record-lint/SKILL.md", ".claude/skills/braindex-review/SKILL.md", "work/review/.gitkeep"} {
		if !has(ps, want) {
			t.Errorf("review の配布物に %s が無い: %v", want, ps)
		}
	}
	for _, bad := range []string{".claude/skills/retro/SKILL.md", "news/feeds.example.json", ".gitignore"} {
		if has(ps, bad) {
			t.Errorf("review の配布物に %s が混じっている", bad)
		}
	}
	if !sort.StringsAreSorted(ps) {
		t.Errorf("昇順でない: %v", ps)
	}

	all, err := FeatureFiles([]Feature{FeatureAll})
	if err != nil {
		t.Fatal(err)
	}
	tmpl, _ := Files(KindHub)
	if len(all) != len(tmpl) {
		t.Fatalf("all=%d 雛形=%d", len(all), len(tmpl))
	}
	for i := range all {
		if all[i].Path != tmpl[i].Path || !bytes.Equal(all[i].Content, tmpl[i].Content) {
			t.Errorf("all の %s が雛形と違う", all[i].Path)
		}
	}
}

// InstallFeatures: 段 0 → conventions → review と順に足しても、all を一度に足したのと同じ hub になる。
// 同じ機能を 2 回足しても何も変わらない。台帳には足した機能が昇順で載る。
func TestInstallFeatures_Incremental(t *testing.T) {
	step := filepath.Join(t.TempDir(), "step")
	once := filepath.Join(t.TempDir(), "once")
	for _, feats := range [][]Feature{nil, {FeatureConventions}, {FeatureReview}, {FeatureRetro}, {FeatureNews}, {FeatureSchedule}} {
		if _, err := InstallFeatures(step, feats); err != nil {
			t.Fatalf("InstallFeatures(%v): %v", feats, err)
		}
	}
	if _, err := InstallFeatures(once, []Feature{FeatureAll}); err != nil {
		t.Fatal(err)
	}
	// 2 回目は何も作らず、何も足さない
	res, err := InstallFeatures(step, []Feature{FeatureAll})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Created) != 0 || len(res.Merged) != 0 {
		t.Errorf("2 回目に変更がある: created=%v merged=%v", res.Created, res.Merged)
	}
	files, _ := Files(KindHub)
	for _, f := range files {
		a, err := os.ReadFile(filepath.Join(step, filepath.FromSlash(f.Path)))
		if err != nil {
			t.Errorf("段階的に足した hub に %s が無い: %v", f.Path, err)
			continue
		}
		b, _ := os.ReadFile(filepath.Join(once, filepath.FromSlash(f.Path)))
		if !bytes.Equal(a, b) {
			t.Errorf("%s が一括と段階的で違う:\n--- 段階的\n%s\n--- 一括\n%s", f.Path, a, b)
		}
	}
	for _, dir := range []string{step, once} {
		led, _, err := LoadLedger(dir)
		if err != nil {
			t.Fatal(err)
		}
		if got := strings.Join(led.Features, ","); got != "conventions,news,retro,review,schedule" {
			t.Errorf("%s の台帳 features=%s", dir, got)
		}
	}
}

// 段 0 だけの hub: braindex.json は root・notes_dirs・extra だけで、docs/・work/・skill・news は無い。
func TestInstallFeatures_CoreOnly(t *testing.T) {
	dst := t.TempDir()
	res, err := InstallFeatures(dst, nil)
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(res.Created, ","); got != ".gitattributes,CLAUDE.md,README.md,braindex.json" {
		t.Errorf("created=%s", got)
	}
	cfg := readAt(t, dst, ConfigPath)
	for _, bad := range []string{`"review"`, `"retro"`, `"news"`, `"schedule"`, `"approvals"`} {
		if bytes.Contains(cfg, []byte(bad)) {
			t.Errorf("段 0 の braindex.json に %s がある:\n%s", bad, cfg)
		}
	}
	for _, p := range []string{"docs", "work", ".claude", "news", ".gitignore"} {
		if _, err := os.Stat(filepath.Join(dst, p)); err == nil {
			t.Errorf("段 0 に %s がある", p)
		}
	}
	led, _, _ := LoadLedger(dst)
	if len(led.Features) != 0 {
		t.Errorf("段 0 の台帳に features がある: %v", led.Features)
	}
}

// 利用者が編集した braindex.json に節を足す: 既存の値は残り、Merged に出て、台帳は据え置き
// (編集済みと分かる状態を保つ)。配った版のままなら台帳も進む。
func TestInstallFeatures_MergesIntoEditedConfig(t *testing.T) {
	dst := t.TempDir()
	if _, err := InstallFeatures(dst, nil); err != nil {
		t.Fatal(err)
	}
	led, _, _ := LoadLedger(dst)
	distributed := led.Files[ConfigPath]

	// 配った版のまま retro を足す → 台帳は新しい内容に進む
	res, err := InstallFeatures(dst, []Feature{FeatureRetro})
	if err != nil {
		t.Fatal(err)
	}
	if !has(res.Merged, ConfigPath) || has(res.Created, ConfigPath) {
		t.Errorf("retro: merged=%v created=%v", res.Merged, res.Created)
	}
	led, _, _ = LoadLedger(dst)
	cfg := readAt(t, dst, ConfigPath)
	if led.Files[ConfigPath] == distributed || led.Files[ConfigPath] != Hash(cfg) {
		t.Errorf("配った版のままの設定に節を足したのに台帳が進んでいない")
	}

	// 利用者が root を書き換えた後に news を足す → root は残り、台帳は据え置き
	edited := bytes.Replace(cfg, []byte(`"root": ".."`), []byte(`"root": "/my/notes"`), 1)
	writeAt(t, dst, ConfigPath, edited)
	before := led.Files[ConfigPath]
	if _, err := InstallFeatures(dst, []Feature{FeatureNews}); err != nil {
		t.Fatal(err)
	}
	cfg = readAt(t, dst, ConfigPath)
	if !bytes.Contains(cfg, []byte(`"root": "/my/notes"`)) || !bytes.Contains(cfg, []byte(`"news"`)) || !bytes.Contains(cfg, []byte(`"retro"`)) {
		t.Errorf("編集した root が残っていない、または節が足りない:\n%s", cfg)
	}
	led, _, _ = LoadLedger(dst)
	if led.Files[ConfigPath] != before {
		t.Errorf("編集済みの設定に節を足したのに台帳が進んだ")
	}
	if got := strings.Join(led.Features, ","); got != "news,retro" {
		t.Errorf("features=%s", got)
	}
}
