package game

import (
	"testing"
	"unicode/utf8"
)

func TestScramble(t *testing.T) {
	// the example in the design's notes
	if got := Scramble("oblak", "2026-10-27"); got != "DsifXe4=" {
		t.Errorf("Scramble = %s, want DsifXe4=", got)
	}
}

func TestAnswers(t *testing.T) {
	if len(Answers) < 900 {
		t.Fatalf("%d answers", len(Answers))
	}
	for _, w := range Answers {
		if utf8.RuneCountInString(w) != 5 {
			t.Errorf("%q is not five letters", w)
		}
	}
	used := map[string]bool{}
	for _, w := range Answers[1:] {
		used[w] = true
	}
	if w, err := Pick(used); err != nil || w != Answers[0] {
		t.Errorf("Pick = %q, %v: the one word left", w, err)
	}
	used[Answers[0]] = true
	if _, err := Pick(used); err == nil {
		t.Error("no words left, no error")
	}
}
