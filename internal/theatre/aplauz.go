package theatre

import (
	"encoding/json"
	"errors"
	"regexp"
	"strings"
	"time"
)

// Aplauz teatar has no stage of its own and its schedule page fills itself
// in the browser: from its Impresario app (aplauzLive) and from the shows
// typed into /assets/schedule-data.js (STATIC_SCHEDULE), the app's winning
// on the same day and title. The site's URL is that script; the app is
// asked as well and skipped when it does not answer. Only the shows in
// Zagreb are kept.
const aplauzLive = "https://impresario.aplauz-teatar.hr/api/javni-raspored"

type aplauzShow struct {
	Date  string `json:"date"` // "2026-11-12"
	Time  string `json:"time"` // "20:00"
	Title string `json:"title"`
	Venue string `json:"venue"`
}

var (
	aplauzStatic = regexp.MustCompile(`(?s)STATIC_SCHEDULE\s*=\s*(\[.*?\])\s*;`)
	aplauzHref   = regexp.MustCompile(`"([^"]+)"\s*:\s*\{\s*href:\s*"([^"]+)"`)
)

func parseAplauz(p *Page) ([]Show, error) {
	m := aplauzStatic.FindSubmatch(p.Body)
	if m == nil {
		return nil, errNoList
	}
	var static []aplauzShow
	if err := json.Unmarshal(m[1], &static); err != nil {
		return nil, err
	}
	var live []aplauzShow
	if p.More != nil {
		if body, err := p.More(aplauzLive, nil); err == nil {
			json.Unmarshal(body, &live) // not a list: the static ones alone
		}
	}
	hrefs := map[string]string{}
	for _, h := range aplauzHref.FindAllSubmatch(p.Body, -1) {
		hrefs[string(h[1])] = string(h[2])
	}

	merged := live
	taken := map[string]bool{}
	for _, s := range live {
		taken[s.Date+"|"+s.Title] = true
	}
	for _, s := range static {
		if !taken[s.Date+"|"+s.Title] {
			merged = append(merged, s)
		}
	}
	var shows []Show
	for _, s := range merged {
		if !strings.Contains(strings.ToLower(s.Venue), "zagreb") {
			continue
		}
		date, err := time.ParseInLocation(time.DateOnly, s.Date, time.UTC)
		if err != nil {
			continue
		}
		shows = append(shows, Show{Date: date, Time: Clock(s.Time), Title: s.Title, Link: link(p.URL, hrefs[s.Title])})
	}
	if len(static) == 0 && len(live) == 0 {
		return nil, errors.New("no shows in the app or the script")
	}
	return shows, nil
}
