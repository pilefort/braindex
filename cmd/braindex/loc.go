package main

import (
	"fmt"
	"time"
)

// localLoc は「その日の 0 時」と週の境界を決めるタイムゾーン(既定: 実行環境のローカル)。
// 窓の起点を UTC の 0 時にすると、同じ「N 日前から」が retro と news で別の日を指し、
// 両方を定期実行に載せたときに食い違う(決定 2026-09-03)。
// 環境に依存する部分をこの 1 か所に集める(設計レビュー 2026-09-06 M3b)。テストが UTC に差し替える。
var localLoc = time.Local

// localMidnight は YYYY-MM-DD を localLoc の 0 時にする。
func localMidnight(date string) (time.Time, error) {
	t, err := time.ParseInLocation("2006-01-02", date, localLoc)
	if err != nil {
		return time.Time{}, fmt.Errorf("日付は YYYY-MM-DD で指定する: %q", date)
	}
	return t, nil
}

// todayOrNow は date(YYYY-MM-DD)があればその localLoc の 0 時、無ければ今を返す。
// -date を渡したときに time.Now() が混ざらないようにする口。
func todayOrNow(date string) (time.Time, error) {
	if date == "" {
		return time.Now(), nil
	}
	return localMidnight(date)
}
