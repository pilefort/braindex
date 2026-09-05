package learn

import (
	"bytes"
	"strings"
	"testing"
	"time"

	"github.com/pilefort/braindex/internal/interest"
	"github.com/pilefort/braindex/internal/retro"
	"github.com/pilefort/braindex/internal/sessions"
)

func at(day int, hour int) time.Time {
	return time.Date(2026, 9, day, hour, 0, 0, 0, time.UTC)
}

func human(idx int, t time.Time, text string) sessions.Turn {
	return sessions.Turn{Role: sessions.User, Index: idx, Time: t, Text: text}
}

func fixture() Input {
	p := interest.Profile{Today: "2026-09-05", Days: 14, Sources: map[string]int{"index": 3, "sessions": 50, "keep": 2},
		Terms: []interest.Term{
			{Word: "kubernetes", Weight: 1.0, Counts: map[string]float64{"sessions": 4}},            // 触れているがノートに無い
			{Word: "terraform", Weight: 0.9, Counts: map[string]float64{"sessions": 3, "index": 1}}, // ノートにある → 出ない
			{Word: "grpc", Weight: 0.5, Counts: map[string]float64{"sessions": 2}},                  // 閾値未満
			{Word: "webassembly", Weight: 0.4, Counts: map[string]float64{"keep": 2}},               // 残した記事にあるがノートに無い
			{Word: "rust", Weight: 0.4, Counts: map[string]float64{"keep": 1, "index": 2}},          // ノートにある → 出ない
		}}
	ss := []sessions.Session{
		{ID: "s1", Project: "/p/a", Turns: []sessions.Turn{
			human(1, at(1, 9), "Kubernetes の Ingress を設定して"),
			human(2, at(1, 10), "違う。Ingress じゃなくて Gateway API で"),
		}},
		{ID: "s2", Project: "/p/a", Turns: []sessions.Turn{
			human(1, at(2, 9), "Ingress の TLS を直して"),
			human(2, at(2, 10), "そうじゃない。証明書は cert-manager が持つ"),
		}},
		{ID: "s3", Project: "/p/b", Turns: []sessions.Turn{
			human(1, at(3, 9), "Terraform の plan を見せて"),
			human(2, at(3, 10), "ありがとう"),
		}},
	}
	dict, _ := retro.Parse("test", "違う\nそうじゃない\n")
	return Input{Profile: p, Sessions: ss, Window: retro.Window{Since: at(1, 0)}, Dicts: []*retro.Dictionary{dict}, Options: Options{MinSessions: 3, MinCorrections: 2, Top: 10}}
}

func TestBuild_信号3つ(t *testing.T) {
	r := Build(fixture())
	if len(r.Unsettled) != 1 || r.Unsettled[0].Word != "kubernetes" || r.Unsettled[0].Sessions != 4 {
		t.Errorf("触れているがノートに無い: %+v", r.Unsettled)
	}
	// Ingress は訂正 2 発話（s1・s2）の直前の発話に出る。Gateway は 1 発話だけ → 出ない
	if len(r.Stumbles) != 1 || r.Stumbles[0].Word != "ingress" || r.Stumbles[0].Corrections != 2 || r.Stumbles[0].Sessions != 2 {
		t.Errorf("訂正の文脈: %+v", r.Stumbles)
	}
	if len(r.ReadNotWritten) != 1 || r.ReadNotWritten[0].Word != "webassembly" || r.ReadNotWritten[0].Keeps != 2 {
		t.Errorf("残した記事にあるがノートに無い: %+v", r.ReadNotWritten)
	}
	if r.Sources["corrections"] != 2 {
		t.Errorf("訂正発話数: %d", r.Sources["corrections"])
	}
}

func TestBuild_窓の外は数えない(t *testing.T) {
	in := fixture()
	in.Window = retro.Window{Since: at(2, 0)} // s1 は窓の外
	r := Build(in)
	if len(r.Stumbles) != 0 {
		t.Errorf("窓の外の訂正を数えた: %+v", r.Stumbles)
	}
}

func TestBuild_Deterministic(t *testing.T) {
	a := Build(fixture()).Marshal()
	b := Build(fixture()).Marshal()
	if !bytes.Equal(a, b) {
		t.Fatalf("同じ材料でバイト列が違う:\n%s\n---\n%s", a, b)
	}
	s := string(a)
	for _, want := range []string{"# 学習の提案 2026-09-05（直近 14 日）", "材料: ノート 3・セッション 50・訂正 2 発話・keep 2", "## 触れているがノートに無い", "kubernetes", "## 訂正の文脈に繰り返し出る", "ingress", "## 残した記事にあるがノートに無い", "webassembly"} {
		if !strings.Contains(s, want) {
			t.Errorf("出力に %q が無い:\n%s", want, s)
		}
	}
	if strings.Contains(s, "Gateway API で") {
		t.Errorf("発話の本文が出力に載っている:\n%s", s)
	}
}

func TestBuild_Topで切る(t *testing.T) {
	in := fixture()
	in.Options.MaxSessionRatio = 1 // 汎用語の除外を切る(5 セッション中 4 本に出る kubernetes を試すため)
	in.Options.Top = 0             // 0 は全件
	if got := len(Build(in).Unsettled); got != 1 {
		t.Errorf("Top=0 で全件のはず: %d", got)
	}
	in.Options.Top = 1
	in.Options.MaxSessionRatio = 1 // 汎用語の除外を切る(5 セッション中 5 本に出る語を試すため)
	in.Profile.Terms = append(in.Profile.Terms, interest.Term{Word: "istio", Counts: map[string]float64{"sessions": 5}})
	r := Build(in)
	if len(r.Unsettled) != 1 || r.Unsettled[0].Word != "istio" {
		t.Errorf("Top=1 で数の多い Istio だけのはず: %+v", r.Unsettled)
	}
}

func TestBuild_同じ冒頭の発話は定型として除く(t *testing.T) {
	in := fixture()
	// 3 セッションで同じ冒頭の長いプロンプト(機械実行の翻訳依頼のようなもの)が訂正辞書に当たる
	boiler := strings.Repeat("以下は英語ニュースの見出しの JSON 配列。各項目を日本語に翻訳せよ。違うものは除く。", 3) + " Kafka Streams "
	for i := 0; i < 3; i++ {
		in.Sessions = append(in.Sessions, sessions.Session{ID: "m" + string(rune('1'+i)), Turns: []sessions.Turn{human(1, at(4, 9), boiler+string(rune('a'+i)))}})
	}
	r := Build(in)
	for _, it := range r.Stumbles {
		if it.Word == "kafka" {
			t.Errorf("定型の発話の語が訂正の文脈に載った: %+v", r.Stumbles)
		}
	}
	if r.Sources["boilerplate"] != 3 {
		t.Errorf("定型として除いた発話数: %d", r.Sources["boilerplate"])
	}
	if r.Sources["corrections"] != 2 {
		t.Errorf("訂正発話数に定型を含めた: %d", r.Sources["corrections"])
	}
}

func TestBuild_全セッションに出る汎用語は除く(t *testing.T) {
	in := fixture()
	in.Profile.Sources["sessions"] = 10
	in.Profile.Terms = append(in.Profile.Terms, interest.Term{Word: "確認", Counts: map[string]float64{"sessions": 9}}) // 90% に出る → 落ちる
	r := Build(in)
	for _, it := range r.Unsettled {
		if it.Word == "確認" {
			t.Errorf("汎用語が載った: %+v", r.Unsettled)
		}
	}
	// kubernetes は 4/10 = 40% → 既定 0.1 を超えるので落ちる。閾値を上げれば載る
	if len(r.Unsettled) != 0 {
		t.Errorf("40%% の語も既定では落ちるはず: %+v", r.Unsettled)
	}
	in.Options.MaxSessionRatio = 0.5
	r = Build(in)
	if len(r.Unsettled) != 1 || r.Unsettled[0].Word != "kubernetes" {
		t.Errorf("閾値 0.5 で kubernetes だけ載るはず: %+v", r.Unsettled)
	}
}
