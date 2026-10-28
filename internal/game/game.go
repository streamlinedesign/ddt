// Package game is Riječililica's word: drawn at print time from a list that stays on the server,
// printed into the edition scrambled with its date, and checked in the reader's browser.
package game

import (
	_ "embed"
	"encoding/base64"
	"errors"
	"math/rand/v2"
	"strings"
	"unicode/utf8"
)

//go:embed odgovori.txt
var answersFile string

// Answers are the words the game can be about: common five-letter Croatian words.
var Answers = func() []string {
	var out []string
	for _, l := range strings.Split(answersFile, "\n") {
		w := strings.ToLower(strings.TrimSpace(l))
		if w != "" && !strings.HasPrefix(w, "#") && utf8.RuneCountInString(w) == 5 {
			out = append(out, w)
		}
	}
	return out
}()

// Pick draws a word that is not among the used ones.
func Pick(used map[string]bool) (string, error) {
	var left []string
	for _, w := range Answers {
		if !used[w] {
			left = append(left, w)
		}
	}
	if len(left) == 0 {
		return "", errors.New("every answer was used in the last year")
	}
	return left[rand.IntN(len(left))], nil
}

// Scramble hides the word in the page: each byte of it XORed with a value from the date's digits and
// its place. The page undoes it; this keeps honest people honest and is not meant to stop anyone
// reading the source.
func Scramble(word, day string) string {
	seed := 0
	for _, c := range day {
		if c >= '0' && c <= '9' {
			seed = (seed*10 + int(c-'0')) % 251
		}
	}
	b := []byte(word)
	for i := range b {
		b[i] ^= byte((seed + i*73 + 41) & 255)
	}
	return base64.StdEncoding.EncodeToString(b)
}
