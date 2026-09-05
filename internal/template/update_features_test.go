package template

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// 台帳の Features に記録された機能の分だけ追従する。足していない機能のファイルは作らない。
func TestUpdate_FollowsLedgerFeatures(t *testing.T) {
	dst := t.TempDir()
	if _, err := InstallFeatures(dst, []Feature{FeatureRetro}); err != nil {
		t.Fatal(err)
	}
	for _, p := range []string{"README.md", ".claude/skills/retro/SKILL.md"} {
		if err := os.Remove(filepath.Join(dst, filepath.FromSlash(p))); err != nil {
			t.Fatal(err)
		}
	}
	res, err := Update(dst, KindHub, UpdateOptions{})
	if err != nil {
		t.Fatalf("Update: %v", err)
	}
	if res.Inferred {
		t.Error("台帳に記録があるのに推定している")
	}
	if got := strings.Join(FeatureNames(res.Features), ","); got != "retro" {
		t.Errorf("features=%s want retro", got)
	}
	for _, p := range []string{"README.md", ".claude/skills/retro/SKILL.md"} {
		if !has(res.Created, p) {
			t.Errorf("%s が作られていない: %v", p, res.Created)
		}
	}
	for _, p := range []string{"docs", "work", "news", ".gitignore", ".claude/skills/record-lint"} {
		if _, err := os.Lstat(filepath.Join(dst, filepath.FromSlash(p))); err == nil {
			t.Errorf("足していない機能の %s を作った", p)
		}
	}
}

// 台帳に機能の記録が無い hub(旧版で作ったもの)は、存在するファイルと設定の節から機能を推定し、台帳に書く。
func TestUpdate_InfersFeaturesWithoutLedger(t *testing.T) {
	dst := t.TempDir()
	writeAt(t, dst, "docs/conventions.md", []byte("私の規約\n"))
	writeAt(t, dst, ".claude/skills/retro/SKILL.md", []byte("私の retro\n"))
	writeAt(t, dst, ConfigPath, []byte(`{"root": "/x", "news": {"dir": "news"}}`+"\n"))
	res, err := Update(dst, KindHub, UpdateOptions{})
	if err != nil {
		t.Fatalf("Update: %v", err)
	}
	if !res.Inferred {
		t.Error("推定していない")
	}
	if got := strings.Join(FeatureNames(res.Features), ","); got != "conventions,news,retro" {
		t.Errorf("features=%s want conventions,news,retro", got)
	}
	for _, p := range []string{"docs/overview.md", "work/APPROVALS.md", ".claude/skills/record-lint/SKILL.md", "news/feeds.example.json", ".gitignore"} {
		if !has(res.Created, p) {
			t.Errorf("推定した機能の %s が作られていない: %v", p, res.Created)
		}
	}
	for _, p := range []string{".claude/skills/braindex-review/SKILL.md", "work/review"} {
		if _, err := os.Lstat(filepath.Join(dst, filepath.FromSlash(p))); err == nil {
			t.Errorf("推定に無い機能の %s を作った", p)
		}
	}
	// 編集済み(台帳なし)の braindex.json には無い節(approvals・retro)を足し、root は残す。.new も置く
	if !has(res.Merged, ConfigPath) {
		t.Errorf("braindex.json が Merged に無い: %v", res.Merged)
	}
	cfg := string(readAt(t, dst, ConfigPath))
	for _, want := range []string{`"root": "/x"`, `"approvals"`, `"retro"`, `"news"`} {
		if !strings.Contains(cfg, want) {
			t.Errorf("braindex.json に %s が無い:\n%s", want, cfg)
		}
	}
	if strings.Contains(cfg, `"schedule"`) || strings.Contains(cfg, `"review"`) {
		t.Errorf("推定に無い機能の節を足した:\n%s", cfg)
	}
	if _, err := os.Lstat(filepath.Join(dst, ConfigPath+NewSuffix)); err != nil {
		t.Error("編集済みの braindex.json に .new が無い")
	}
	led, _, _ := LoadLedger(dst)
	if got := strings.Join(led.Features, ","); got != "conventions,news,retro" {
		t.Errorf("台帳の features=%s", got)
	}
	// 2 回目は台帳の記録を使う(推定しない)
	res, err = Update(dst, KindHub, UpdateOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if res.Inferred {
		t.Error("台帳に書いた後も推定している")
	}
}

// InferFeatures の判定: 配布物か設定の節が 1 つでもあれば足してあるとみなす。.gitignore だけでは news にしない。
func TestInferFeatures(t *testing.T) {
	dst := t.TempDir()
	got, err := InferFeatures(dst)
	if err != nil || len(got) != 0 {
		t.Errorf("空: got=%v err=%v", got, err)
	}
	writeAt(t, dst, GitignorePath, []byte("news/digest_*\n"))
	if got, _ := InferFeatures(dst); len(got) != 0 {
		t.Errorf(".gitignore だけで推定した: %v", got)
	}
	writeAt(t, dst, "work/review/.gitkeep", nil)
	writeAt(t, dst, ConfigPath, []byte(`{"schedule": {"jobs": []}}`))
	got, _ = InferFeatures(dst)
	if s := strings.Join(FeatureNames(got), ","); s != "review,schedule" {
		t.Errorf("got=%s want review,schedule", s)
	}
	writeAt(t, dst, ConfigPath, []byte(`{"root": `))
	if _, err := InferFeatures(dst); err == nil {
		t.Error("壊れた braindex.json でエラーにならない")
	}
}

// 編集済みの .gitignore は news の行だけ足し、.new は置かない(雛形は行の集まりでしかないため)。
func TestUpdate_GitignoreMergesWithoutNew(t *testing.T) {
	dst := t.TempDir()
	if _, err := InstallFeatures(dst, []Feature{FeatureNews}); err != nil {
		t.Fatal(err)
	}
	writeAt(t, dst, GitignorePath, []byte("*.tmp\n"))
	res, err := Update(dst, KindHub, UpdateOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if !has(res.Merged, GitignorePath) || len(res.Conflicts) != 0 {
		t.Errorf("merged=%v conflicts=%v", res.Merged, res.Conflicts)
	}
	gi := string(readAt(t, dst, GitignorePath))
	if !strings.HasPrefix(gi, "*.tmp\n") || !strings.Contains(gi, "news/.ingested/") {
		t.Errorf(".gitignore:\n%s", gi)
	}
	if _, err := os.Lstat(filepath.Join(dst, GitignorePath+NewSuffix)); err == nil {
		t.Error(".gitignore.new を置いた")
	}
	// 揃った後は何も書かず、Conflicts にも Merged にも出ない
	res, err = Update(dst, KindHub, UpdateOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if has(res.Merged, GitignorePath) || len(res.Conflicts) != 0 {
		t.Errorf("2 回目: merged=%v conflicts=%v", res.Merged, res.Conflicts)
	}
}

// -dry-run では braindex.json の節の追記も書かない(結果には出す)。
func TestUpdate_DryRunDoesNotMerge(t *testing.T) {
	dst := t.TempDir()
	if _, err := InstallFeatures(dst, []Feature{FeatureRetro}); err != nil {
		t.Fatal(err)
	}
	edited := []byte(`{"root": "/x"}` + "\n")
	writeAt(t, dst, ConfigPath, edited)
	res, err := Update(dst, KindHub, UpdateOptions{DryRun: true})
	if err != nil {
		t.Fatal(err)
	}
	if !has(res.Merged, ConfigPath) {
		t.Errorf("merged=%v", res.Merged)
	}
	if got := readAt(t, dst, ConfigPath); !bytes.Equal(got, edited) {
		t.Errorf("dry-run なのに書いた:\n%s", got)
	}
}

// 機能の記録が無い旧版の台帳を持つ hub に init -add しても、既にある機能を落とさない(推定して合わせる)。
func TestInstallFeatures_KeepsInferredOnOldLedger(t *testing.T) {
	dst := t.TempDir()
	if _, err := InstallFeatures(dst, []Feature{FeatureAll}); err != nil {
		t.Fatal(err)
	}
	led, _, _ := LoadLedger(dst)
	b, _ := json.MarshalIndent(struct {
		Version int               `json:"version"`
		Kind    string            `json:"kind"`
		Files   map[string]string `json:"files"`
	}{led.Version, led.Kind, led.Files}, "", "  ")
	writeAt(t, dst, LedgerPath, b) // features キーの無い旧版の台帳
	if _, err := InstallFeatures(dst, []Feature{FeatureRetro}); err != nil {
		t.Fatal(err)
	}
	led, _, _ = LoadLedger(dst)
	if got := strings.Join(led.Features, ","); got != "conventions,news,retro,review,schedule" {
		t.Errorf("features=%s", got)
	}
}

// 台帳の features に今の版が知らない名前(新しい版の braindex が書いたもの)があっても、update は落とさず残す。
// 追従の対象は知っている機能だけ。
func TestUpdate_KeepsUnknownFeatureNamesInLedger(t *testing.T) {
	dst := t.TempDir()
	if _, err := InstallFeatures(dst, []Feature{FeatureRetro}); err != nil {
		t.Fatal(err)
	}
	led, _, err := LoadLedger(dst)
	if err != nil {
		t.Fatal(err)
	}
	led.Features = []string{"future-feature", "retro"}
	if err := SaveLedger(dst, led); err != nil {
		t.Fatal(err)
	}
	res, err := Update(dst, KindHub, UpdateOptions{})
	if err != nil {
		t.Fatalf("Update: %v", err)
	}
	if got := strings.Join(FeatureNames(res.Features), ","); got != "retro" {
		t.Errorf("features=%s want retro", got)
	}
	led, _, err = LoadLedger(dst)
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(led.Features, ","); got != "future-feature,retro" {
		t.Errorf("台帳の features=%s want future-feature,retro(未知の名前を落とした)", got)
	}
}
