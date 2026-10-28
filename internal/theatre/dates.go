package theatre

import (
	"regexp"
	"strconv"
	"strings"
	"time"
)

// The parsers' shared readers of dates and times as Croatian pages write
// them.

// months are the Croatian month names, nominative and genitive, by their
// stems: "listopad", "listopada".
var months = []string{"siječ", "veljač", "ožuj", "trav", "svib", "lipanj", "srp", "kolovoz", "ruj", "listopad", "studen", "prosin"}

var (
	numericDate = regexp.MustCompile(`(\d{1,2})\s*\.\s*(\d{1,2})\s*\.(?:\s*(\d{4})\.?)?`)
	wordDate    = regexp.MustCompile(`(\d{1,2})\s*\.?\s+(\p{L}+)(?:\s+(\d{4})\.?)?`)
	clock       = regexp.MustCompile(`\b([01]?\d|2[0-3])(?:[:h]|\.)([0-5]\d)`)
)

// Day reads the first date in s: "25.10.2026.", "25. 10.", "subota, 25.
// listopada 2026.". A date without a year is the next one from today on
// (a page read in December lists January's shows). ok is false when s has
// no date.
func Day(s string, today time.Time) (time.Time, bool) {
	s = strings.ToLower(s)
	var d, m, y int
	if g := numericDate.FindStringSubmatch(s); g != nil {
		d, _ = strconv.Atoi(g[1])
		m, _ = strconv.Atoi(g[2])
		y, _ = strconv.Atoi(g[3])
	} else {
		for _, g := range wordDate.FindAllStringSubmatch(s, -1) {
			if mm := monthOf(g[2]); mm > 0 {
				d, _ = strconv.Atoi(g[1])
				m = mm
				y, _ = strconv.Atoi(g[3])
				break
			}
		}
	}
	if d < 1 || d > 31 || m < 1 || m > 12 {
		return time.Time{}, false
	}
	if y == 0 {
		y = today.Year()
		// Earlier than two months back: next year's.
		if time.Date(y, time.Month(m), d, 0, 0, 0, 0, time.UTC).Before(today.AddDate(0, -2, 0)) {
			y++
		}
	}
	t := time.Date(y, time.Month(m), d, 0, 0, 0, 0, time.UTC)
	if t.Day() != d {
		return time.Time{}, false // 31.11.
	}
	return t, true
}

func monthOf(word string) int {
	for i, stem := range months {
		if strings.HasPrefix(word, stem) {
			return i + 1
		}
	}
	return 0
}

// Clock reads the first time of day in s: "19:30", "19.30 h", "20h00".
// It is "" when s has none. "20.11." and "10.2026" are dates, not times.
func Clock(s string) string {
	for _, m := range clock.FindAllStringSubmatchIndex(s, -1) {
		if rest := s[m[1]:]; strings.HasPrefix(rest, ".") || (rest != "" && rest[0] >= '0' && rest[0] <= '9') {
			continue
		}
		if m[0] > 0 && (s[m[0]-1] == '.' || s[m[0]-1] >= '0' && s[m[0]-1] <= '9') {
			continue // the year or month of "25.10.2026"
		}
		h, _ := strconv.Atoi(s[m[2]:m[3]])
		return strconv.Itoa(100 + h)[1:] + ":" + s[m[4]:m[5]]
	}
	return ""
}
