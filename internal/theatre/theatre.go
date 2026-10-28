// Package theatre reads Zagreb's theatres' schedule pages. Each theatre is its own source
// (kind kazaliste), so it has its own line on the dashboard, and its page is asked for once a day.
//
// A theatre's parser turns its page into shows; Read keeps the ones in the next four weeks.
package theatre

import (
	"context"
	"fmt"
	"net/url"
	"strings"
	"time"

	"tisak/internal/store"
)

// Show is a show as a theatre's page gives it.
type Show struct {
	Date  time.Time // the day, at midnight UTC as the tables keep days
	Time  string    // "19:30", or empty
	Title string
	Link  string // absolute, or empty
}

// Page is a fetched schedule page as its parser gets it.
type Page struct {
	Body  []byte
	URL   *url.URL  // where Body was read from, for relative links
	Today time.Time // the day of the fetch, for dates without a year
	// More asks the theatre's site for one more page: the next part of a
	// list that loads as you scroll, the data a page fills itself from.
	// form is nil for a GET, else POSTed.
	More func(rawURL string, form url.Values) ([]byte, error)
}

// Parser reads a schedule page.
type Parser func(p *Page) ([]Show, error)

// Site is one theatre's schedule page.
type Site struct {
	Source  string // the source's "site" setting and the shows' source: "komedija"
	Theatre string // as printed: "Komedija"
	URL     string
	Parse   Parser
}

// Sites are the theatres whose pages are read (Uriel's list, 25 Oct 2026;
// Aplauz's page is https://aplauz-teatar.hr/raspored.html, see aplauz.go).
// A site without a parser is not fetched.
var Sites = []Site{
	{Source: "komedija", Theatre: "Komedija", URL: "https://www.komedija.hr/www/raspored-predstava/", Parse: parseKomedija},
	{Source: "kerempuh", Theatre: "Kerempuh", URL: "https://kazalistekerempuh.hr/raspored/", Parse: parseKerempuh},
	{Source: "hnk", Theatre: "HNK Zagreb", URL: "https://www.hnk.hr/hr/raspored/", Parse: parseHNK},
	{Source: "scenoteka", Theatre: "Scenoteka", URL: "https://scenoteka.hr/raspored/", Parse: parseScenoteka},
	{Source: "ludakuca", Theatre: "Luda kuća", URL: "https://www.ludakuca.hr/raspored/", Parse: parseLudaKuca},
	{Source: "aplauz", Theatre: "Aplauz teatar", URL: "https://aplauz-teatar.hr/assets/schedule-data.js", Parse: parseAplauz},
	{Source: "exit", Theatre: "Teatar EXIT", URL: "https://teatarexit.hr/raspored", Parse: parseExit},
	{Source: "gavella", Theatre: "Gavella", URL: "https://www.gavella.hr/raspored-izvedbi", Parse: parseGavella},
	{Source: "knap", Theatre: "KNAP", URL: "https://www.knap.hr/kazaliste/raspored-predstava/", Parse: parseKNAP},
}

// Ahead is how many days ahead shows are kept: this week and next, with room to spare.
const Ahead = 28

// Lookup finds a site by its source name.
func Lookup(source string) (Site, bool) {
	for _, s := range Sites {
		if s.Source == source {
			return s, true
		}
	}
	return Site{}, false
}

// Getter fetches a page: a GET when form is nil, else a POST of the form.
type Getter func(ctx context.Context, rawURL string, form url.Values) ([]byte, error)

// Read fetches a site's page and gives its shows in the window from today on.
// A page that parses to nothing has most likely changed, and says so.
func Read(ctx context.Context, s Site, get Getter, today time.Time) ([]store.Show, error) {
	body, err := get(ctx, s.URL, nil)
	if err != nil {
		return nil, err
	}
	u, _ := url.Parse(s.URL)
	more := func(rawURL string, form url.Values) ([]byte, error) { return get(ctx, rawURL, form) }
	found, err := s.Parse(&Page{Body: body, URL: u, Today: today, More: more})
	if err != nil {
		return nil, fmt.Errorf("%s: %w", s.URL, err)
	}
	if len(found) == 0 {
		return nil, fmt.Errorf("%s: no shows found on the page", s.URL)
	}
	return Window(found, s.Theatre, today), nil
}

// Window keeps the shows in [today, today+Ahead] with a title, once each,
// as the store's rows of theatre.
func Window(found []Show, theatre string, today time.Time) []store.Show {
	end := today.AddDate(0, 0, Ahead)
	seen := map[string]bool{}
	var out []store.Show
	for _, s := range found {
		title := strings.Join(strings.Fields(s.Title), " ")
		if title == "" || s.Date.Before(today) || s.Date.After(end) {
			continue
		}
		key := s.Date.Format(time.DateOnly) + "|" + s.Time + "|" + strings.ToLower(title)
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, store.Show{Theatre: theatre, Title: clip(title, 200), Day: s.Date, Time: s.Time, Link: clip(s.Link, 500)})
	}
	return out
}

func clip(s string, n int) string {
	r := []rune(s)
	if len(r) > n {
		return string(r[:n])
	}
	return s
}
