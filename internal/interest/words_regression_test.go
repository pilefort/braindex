package interest

import (
	"reflect"
	"testing"
)

func TestWords_KatakanaDoubleHyphen(t *testing.T) {
	if isKatakana('゠') {
		t.Error("separator classified as Katakana")
	}
	got := Words("ゴルーチン゠パース")
	if !reflect.DeepEqual(got, Words("ゴルーチン・パース")) {
		t.Fatalf("words=%v", got)
	}
}
