package template

import (
	"fmt"
	"sort"
	"strings"
)

// Feature は hub に段階的に足せる機能の単位(決定 2026-09-05「init は段 0 だけ配り、機能は → manual/init-update.md「決めたこと」
// init -add <機能> で 1 つずつ足す」)。core は常に入り、他は利用者が選んで足す。
// 機能ごとに「配るファイル」「braindex.json に足す節」「依存する機能」を持つ。
type Feature string

const (
	FeatureCore        Feature = "core"        // 索引の設定だけ(README・CLAUDE.md・.gitattributes・braindex.json の root/notes_dirs/extra)
	FeatureConventions Feature = "conventions" // docs/・work/ の規約と、記録を整えるスキル 3 本
	FeatureReview      Feature = "review"      // 週次レビュー(braindex review)。conventions に依存
	FeatureRetro       Feature = "retro"       // レトロスペクティブ(braindex retro)
	FeatureNews        Feature = "news"        // ニュースサジェスト(braindex news)
	FeatureSchedule    Feature = "schedule"    // 定期実行(braindex schedule)。jobs は足した機能の分だけ
	FeatureAll         Feature = "all"         // 上の全部(従来の init と同じ配布物)
)

// featureSpec は 1 機能の配布物。
type featureSpec struct {
	Summary  string    // -list に出す 1 行
	Files    []string  // templates/hub/ 相対のパス。braindex.json と .gitignore は「節・行の追加」で扱う(config.go・gitignore.go)
	Sections []string  // braindex.json に足す節(最上位キー)
	Deps     []Feature // 先に要る機能
}

// featureOrder は表示と解決の順(段の順)。all は含めない。
var featureOrder = []Feature{FeatureCore, FeatureConventions, FeatureReview, FeatureRetro, FeatureNews, FeatureSchedule}

// DefaultFeatures は braindex init が既定で足す機能(core は常に入る)。利用者の置き場・書き方を変えないもの
// (設定の節と skill を足すだけ)は既定に入れ、規約への乗り換えを迫る conventions とそれに依存する review だけを
// -add で選ばせる(入口の設計 2026-09-05。同日の「段 0 だけ」を上書き)。
var DefaultFeatures = []Feature{FeatureRetro, FeatureNews, FeatureSchedule}

// IsDefault は f が既定で入る機能か(core を含む)。
func IsDefault(f Feature) bool {
	if f == FeatureCore {
		return true
	}
	for _, d := range DefaultFeatures {
		if d == f {
			return true
		}
	}
	return false
}

// features は機能と配布物の対応表。templates/hub/ の全ファイルがどれか 1 つに属する(テストで確かめる)。
//
// SPEC からのずれ(2026-09-05・実装時の判断): 設定の extra は「索引の設定」なので core に、approvals 節は
// work/APPROVALS.md と docs/decisions.md を配る conventions に置く。どちらも SPEC の表に無かったキー。
var features = map[Feature]featureSpec{
	FeatureCore: {
		Summary:  "索引の設定だけ(README・CLAUDE.md・.gitattributes・braindex.json の root/notes_dirs/extra)。常に入る",
		Files:    []string{".gitattributes", "CLAUDE.md", "README.md", ConfigPath},
		Sections: []string{"root", "notes_dirs", "extra"},
	},
	FeatureConventions: {
		Summary: "docs/(overview・glossary・decisions・conventions・notes/)と work/(APPROVALS・TODO)の規約、スキル record-lint・contradiction-scan・research-distill、設定の approvals 節",
		Files: []string{
			".claude/skills/contradiction-scan/SKILL.md",
			".claude/skills/record-lint/SKILL.md",
			".claude/skills/research-distill/SKILL.md",
			"docs/conventions.md",
			"docs/decisions.md",
			"docs/glossary.md",
			"docs/notes/common/.gitkeep",
			"docs/notes/project/.gitkeep",
			"docs/overview.md",
			"work/APPROVALS.md",
			"work/TODO.md",
		},
		Sections: []string{"approvals"},
	},
	FeatureReview: {
		Summary:  "週次レビュー(braindex review): スキル braindex-review・work/review/・設定の review 節",
		Files:    []string{".claude/skills/braindex-review/SKILL.md", "work/review/.gitkeep"},
		Sections: []string{"review"},
		Deps:     []Feature{FeatureConventions},
	},
	FeatureRetro: {
		Summary:  "レトロスペクティブ(braindex retro): スキル retro・設定の retro 節",
		Files:    []string{".claude/skills/retro/SKILL.md"},
		Sections: []string{"retro"},
	},
	FeatureNews: {
		Summary:  "ニュースサジェスト(braindex news): news/feeds.example.json・.gitignore の news の行・設定の news 節",
		Files:    []string{GitignorePath, "news/feeds.example.json"},
		Sections: []string{"news"},
	},
	FeatureSchedule: {
		Summary:  "定期実行(braindex schedule): 設定の schedule 節(jobs は review・retro・news のうち足した分)",
		Sections: []string{"schedule"},
	},
}

// FeatureList は all を除く機能を段の順で返す。
func FeatureList() []Feature {
	out := make([]Feature, len(featureOrder))
	copy(out, featureOrder)
	return out
}

// FeatureInfo は 1 機能の説明・配るファイル・設定の節・依存を返す(-list 用)。無い機能は ok=false。
func FeatureInfo(f Feature) (summary string, files, sections []string, deps []Feature, ok bool) {
	spec, ok := features[f]
	if !ok {
		return "", nil, nil, nil, false
	}
	return spec.Summary, append([]string(nil), spec.Files...), append([]string(nil), spec.Sections...), append([]Feature(nil), spec.Deps...), true
}

// ParseFeatures は "conventions,review" のようなカンマ区切りを読む。空の要素は飛ばし、
// 未知の名前は候補を添えてエラーにする。
func ParseFeatures(s string) ([]Feature, error) {
	var out []Feature
	for _, raw := range strings.Split(s, ",") {
		name := strings.TrimSpace(raw)
		if name == "" {
			continue
		}
		f := Feature(name)
		if _, ok := features[f]; !ok && f != FeatureAll {
			return nil, fmt.Errorf("未知の機能 %q(候補: %s)", name, strings.Join(featureNames(), ", "))
		}
		out = append(out, f)
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("機能が空(候補: %s)", strings.Join(featureNames(), ", "))
	}
	return out, nil
}

func featureNames() []string {
	var names []string
	for _, f := range featureOrder {
		names = append(names, string(f))
	}
	return append(names, string(FeatureAll))
}

// checkFeatures は req が全部対応表にある(か all)ことを確かめる。Resolve は対応表に無い名前を
// 黙って落とすので、配布物を作る入口(FeatureFiles・BuildConfig・InstallFeatures)はこれを先に通す。
func checkFeatures(req []Feature) error {
	for _, f := range req {
		if _, ok := features[f]; !ok && f != FeatureAll {
			return fmt.Errorf("未知の機能 %q(候補: %s)", f, strings.Join(featureNames(), ", "))
		}
	}
	return nil
}

// Resolve は要求された機能に core と依存を足し、段の順に並べて返す。all は全機能に展開する。
// added は要求に無かったが依存として足した機能(利用者に「足した」と伝えるため)。
func Resolve(req []Feature) (feats, added []Feature) {
	want := map[Feature]bool{FeatureCore: true}
	asked := map[Feature]bool{}
	var walk func(f Feature)
	walk = func(f Feature) {
		if want[f] {
			return
		}
		want[f] = true
		for _, d := range features[f].Deps {
			walk(d)
		}
	}
	for _, f := range req {
		if f == FeatureAll {
			for _, g := range featureOrder {
				asked[g] = true
				walk(g)
			}
			continue
		}
		asked[f] = true
		walk(f)
	}
	for _, f := range featureOrder {
		if !want[f] {
			continue
		}
		feats = append(feats, f)
		if !asked[f] && f != FeatureCore {
			added = append(added, f)
		}
	}
	return feats, added
}

// FeatureNames は台帳に書く形(core を除き、名前の昇順)。
func FeatureNames(feats []Feature) []string {
	var out []string
	for _, f := range feats {
		if f == FeatureCore || f == FeatureAll {
			continue
		}
		out = append(out, string(f))
	}
	sort.Strings(out)
	return out
}

// FeatureFiles は feats の配布物を Path の昇順で返す。braindex.json は feats の節だけで組み立て、
// .gitignore は news の行を持つ。core と依存は Resolve で足す(all もここで展開される)。
func FeatureFiles(feats []Feature) ([]File, error) {
	if err := checkFeatures(feats); err != nil {
		return nil, err
	}
	feats, _ = Resolve(feats)
	all, err := Files(KindHub)
	if err != nil {
		return nil, err
	}
	byPath := map[string][]byte{}
	for _, f := range all {
		byPath[f.Path] = f.Content
	}
	seen := map[string]bool{}
	var out []File
	for _, f := range feats {
		for _, p := range features[f].Files {
			if seen[p] {
				continue
			}
			seen[p] = true
			var content []byte
			switch p {
			case ConfigPath:
				content, _, err = BuildConfig(nil, feats)
				if err != nil {
					return nil, err
				}
			default:
				b, ok := byPath[p]
				if !ok {
					return nil, fmt.Errorf("機能 %s の配布物 %s が雛形に無い", f, p)
				}
				content = b
			}
			out = append(out, File{Path: p, Content: content})
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Path < out[j].Path })
	return out, nil
}
