package schedule

import (
	"strings"
	"testing"
)

var (
	jobReview = Job{Name: "review", Args: []string{"review"}, When: "weekly:mon:09:00"}
	jobRetro  = Job{Name: "retro", Args: []string{"retro", "check"}, When: "weekly:mon:09:05"}
)

func TestCronLine(t *testing.T) {
	cases := []struct {
		hub, exe string
		job      Job
		want     string
	}{
		{"/home/u/hub", "/home/u/go/bin/braindex", jobReview,
			"0 9 * * 1 cd '/home/u/hub' && '/home/u/go/bin/braindex' 'review' # braindex:review"},
		{"/home/u/hub", "/home/u/go/bin/braindex", jobRetro,
			"5 9 * * 1 cd '/home/u/hub' && '/home/u/go/bin/braindex' 'retro' 'check' # braindex:retro"},
		{"/home/u/hub", "/home/u/go/bin/braindex", Job{Name: "news", Args: []string{"news", "fetch"}, When: "daily:07:30"},
			"30 7 * * * cd '/home/u/hub' && '/home/u/go/bin/braindex' 'news' 'fetch' # braindex:news"},
		// 単引用符を含むパスは '"'"' で退避する(sh の定石)
		{"/home/o'brien/hub", "/bin/braindex", jobReview,
			`0 9 * * 1 cd '/home/o'"'"'brien/hub' && '/bin/braindex' 'review' # braindex:review`},
		// 空白を含むパスは単引用符の中に入るので分割されない
		{"/home/u/my hub", "/bin/braindex", jobReview,
			"0 9 * * 1 cd '/home/u/my hub' && '/bin/braindex' 'review' # braindex:review"},
	}
	for _, c := range cases {
		got, err := CronLine(c.hub, c.exe, c.job)
		if err != nil {
			t.Errorf("CronLine(%q,%q,%s): %v", c.hub, c.exe, c.job.Name, err)
			continue
		}
		if got != c.want {
			t.Errorf("CronLine(%q,%q,%s):\n got=%s\nwant=%s", c.hub, c.exe, c.job.Name, got, c.want)
		}
	}
	if _, err := CronLine("/hub", "/bin/braindex", Job{Name: "x", Args: []string{"review"}, When: "毎週"}); err == nil {
		t.Error("when が壊れていたらエラーにする")
	}
}

const hub = "/home/u/hub"

func TestMerge_空のcrontabに足す(t *testing.T) {
	got := Merge("", hub, []string{"0 9 * * 1 x"})
	want := "# BEGIN braindex /home/u/hub\n0 9 * * 1 x\n# END braindex /home/u/hub\n"
	if got != want {
		t.Errorf("got=%q want=%q", got, want)
	}
}

func TestMerge_既存の行を残して末尾に足す(t *testing.T) {
	existing := "0 0 * * * /usr/bin/backup\n# 私のメモ\n"
	got := Merge(existing, hub, []string{"0 9 * * 1 x"})
	if !strings.HasPrefix(got, existing) {
		t.Errorf("ブロック外の行はそのまま先頭に残す: got=%q", got)
	}
	if !strings.Contains(got, "# BEGIN braindex "+hub) || !strings.Contains(got, "# END braindex "+hub) {
		t.Errorf("マーカーで囲む: got=%q", got)
	}
}

func TestMerge_ブロックだけ差し替える(t *testing.T) {
	existing := "0 0 * * * /usr/bin/backup\n" +
		"# BEGIN braindex " + hub + "\n古い行\n# END braindex " + hub + "\n" +
		"@reboot /usr/bin/other\n"
	got := Merge(existing, hub, []string{"新しい行"})
	if strings.Contains(got, "古い行") {
		t.Errorf("ブロックの中は入れ替える: got=%q", got)
	}
	for _, keep := range []string{"/usr/bin/backup", "@reboot /usr/bin/other", "新しい行"} {
		if !strings.Contains(got, keep) {
			t.Errorf("%q が残っていない: got=%q", keep, got)
		}
	}
	// ブロックの後ろにあった行は、ブロックの後ろのままにする
	if strings.Index(got, "新しい行") > strings.Index(got, "@reboot") {
		t.Errorf("行の順序が入れ替わっている: got=%q", got)
	}
}

// 2 回登録しても同じ状態に収束する(install の再実行で二重にならない)。
func TestMerge_冪等(t *testing.T) {
	once := Merge("0 0 * * * /usr/bin/backup\n", hub, []string{"0 9 * * 1 x"})
	twice := Merge(once, hub, []string{"0 9 * * 1 x"})
	if once != twice {
		t.Errorf("2 回目で変わった:\n1回目=%q\n2回目=%q", once, twice)
	}
	if n := strings.Count(twice, "# BEGIN braindex"); n != 1 {
		t.Errorf("マーカーが %d 組ある: %q", n, twice)
	}
}

// 別の hub のブロックは消さない・触らない(1 台で複数の hub を回せる)。
func TestMerge_別のhubのブロックは触らない(t *testing.T) {
	other := "# BEGIN braindex /home/u/other\n0 8 * * 1 y\n# END braindex /home/u/other\n"
	got := Merge(other, hub, []string{"0 9 * * 1 x"})
	if !strings.Contains(got, "0 8 * * 1 y") || !strings.Contains(got, "# BEGIN braindex /home/u/other") {
		t.Errorf("別 hub のブロックが消えた: got=%q", got)
	}
	if got2 := Remove(got, hub); !strings.Contains(got2, "0 8 * * 1 y") {
		t.Errorf("Remove で別 hub のブロックが消えた: got=%q", got2)
	}
}

func TestRemove(t *testing.T) {
	existing := "0 0 * * * /usr/bin/backup\n# BEGIN braindex " + hub + "\n0 9 * * 1 x\n# END braindex " + hub + "\n"
	got := Remove(existing, hub)
	want := "0 0 * * * /usr/bin/backup\n"
	if got != want {
		t.Errorf("got=%q want=%q", got, want)
	}
	// 無いものを消しても変わらない
	if again := Remove(got, hub); again != want {
		t.Errorf("ブロックが無いときは何も変えない: got=%q", again)
	}
}

func TestBlockLines(t *testing.T) {
	existing := "x\n# BEGIN braindex " + hub + "\n行1\n行2\n# END braindex " + hub + "\ny\n"
	got := BlockLines(existing, hub)
	if len(got) != 2 || got[0] != "行1" || got[1] != "行2" {
		t.Errorf("got=%v", got)
	}
	if got := BlockLines("x\ny\n", hub); got != nil {
		t.Errorf("ブロックが無ければ nil: got=%v", got)
	}
}

// 終了マーカーだけ消えた壊れた crontab でも、次の install が正しい形に戻す。
func TestMerge_終了マーカーが無い(t *testing.T) {
	existing := "keep\n# BEGIN braindex " + hub + "\n古い行\n"
	got := Merge(existing, hub, []string{"新しい行"})
	if strings.Contains(got, "古い行") {
		t.Errorf("壊れたブロックは作り直す: got=%q", got)
	}
	if !strings.HasPrefix(got, "keep\n") {
		t.Errorf("ブロックより前の行は残す: got=%q", got)
	}
	if n := strings.Count(got, "# END braindex"); n != 1 {
		t.Errorf("終了マーカーを 1 つ足す: got=%q", got)
	}
}

// 終了マーカーが無いブロックでも、中の行は最後の 1 行まで数える。
// 最終行を終了マーカーの位置とみなすと、利用者が書き足した行が install のたびに 1 行ずつ消える。
func TestBlockLines_終了マーカーが無い(t *testing.T) {
	existing := "keep\n# BEGIN braindex " + hub + "\n0 9 * * 1 x # braindex:review\n手で足した行\n"
	got := BlockLines(existing, hub)
	want := []string{"0 9 * * 1 x # braindex:review", "手で足した行"}
	if len(got) != len(want) {
		t.Fatalf("行数が違う: got=%v want=%v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("%d 行目: got=%q want=%q", i, got[i], want[i])
		}
	}
}

func TestJobOfLine(t *testing.T) {
	if got := JobOfLine("0 9 * * 1 cd '/h' && '/b' 'review' # braindex:review"); got != "review" {
		t.Errorf("got=%q", got)
	}
	if got := JobOfLine("0 9 * * 1 /usr/bin/other"); got != "" {
		t.Errorf("目印が無ければ空: got=%q", got)
	}
}

// CRLF の crontab を読んでも LF に揃え、末尾は必ず改行 1 つで終える。
func TestMerge_改行の正規化(t *testing.T) {
	got := Merge("a\r\nb\r\n\n\n", hub, []string{"x"})
	if strings.Contains(got, "\r") {
		t.Errorf("CR が残っている: %q", got)
	}
	if !strings.HasSuffix(got, "\n") || strings.HasSuffix(got, "\n\n") {
		t.Errorf("末尾の改行は 1 つ: %q", got)
	}
}

// crontab -l の失敗が「まだ crontab が無い」ことかを、出力の文言だけで見分ける。
// 権限などの失敗まで「無い」と読むと、書き戻しで利用者の crontab を全消しする(決定 2026-09-03 A')。
func TestIsNoCrontab(t *testing.T) {
	cases := []struct {
		in   string
		want bool
	}{
		{"crontab: no crontab for someone", true},                           // macOS(BSD cron)・2026-09-03 実測
		{"no crontab for someone", true},                                    // Linux(cronie / vixie-cron)
		{"crontab: no crontab for someone\n", true},                         // 末尾の改行は無視する
		{"crontab: you are not authorized to use cron", false},              // 権限
		{"crontab: can't open your crontab file: Permission denied", false}, // 読めない
		{"", false}, // 文言なし(実行ファイルが無いなど)
		// 呼び出し側は stdout と stderr を混ぜて渡すので、利用者の crontab 本文が来ることがある。
		// 「no crontab を含む」で見ると、この本文を「空」と誤判定して全消しする。
		{"0 3 * * * /usr/bin/backup\n# no crontab entries below this line\n0 4 * * * /usr/bin/rotate\n", false},
		{"echo 'NO CRONTAB'", false}, // 行頭でない
		{"no crontab", false},        // "for <user>" が無い(文言の一部だけの一致は採らない)
	}
	for _, c := range cases {
		if got := IsNoCrontab(c.in); got != c.want {
			t.Errorf("IsNoCrontab(%q) = %v, want %v", c.in, got, c.want)
		}
	}
}

// 判別を出力の文言でするので、言語設定で文言が変わらないよう LC_ALL=C を付けて実行する。
func TestReadCrontab_文言を英語に固定する(t *testing.T) {
	c := ReadCrontab()
	if c.Name != "crontab" || len(c.Args) != 1 || c.Args[0] != "-l" {
		t.Fatalf("crontab -l を返す: got=%+v", c)
	}
	for _, e := range c.Env {
		if e == "LC_ALL=C" {
			return
		}
	}
	t.Errorf("LC_ALL=C が無い: Env=%v", c.Env)
}
