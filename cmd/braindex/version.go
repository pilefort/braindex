package main

import "runtime/debug"

// versionLine は braindex -version の 1 行。
//
// go install で入れたバイナリは module の版(タグ)が入る。go run・go build で作ったものは "(devel)" になる。
// vcs.revision は go build のときだけ入る(go install はモジュールキャッシュから作るので入らない)ので、
// 取れたときだけ短縮して添える。
//
// 「どの版が入っているか」を利用者が言えないと、動きの違いが版差なのか設定なのか切り分けられない
// (設計レビュー 2026-09-06 M9)。
func versionLine() string {
	v, rev := "(不明)", ""
	if bi, ok := debug.ReadBuildInfo(); ok {
		if bi.Main.Version != "" {
			v = bi.Main.Version
		}
		for _, s := range bi.Settings {
			if s.Key == "vcs.revision" {
				rev = s.Value
				if len(rev) > 7 {
					rev = rev[:7]
				}
			}
		}
	}
	if rev != "" {
		return "braindex " + v + " (" + rev + ")"
	}
	return "braindex " + v
}
