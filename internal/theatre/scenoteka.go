package theatre

import (
	"net/url"
	"strconv"
	"strings"
)

// Scenoteka: a row per show with the date ("25.10."), the time and the
// title linked. The page holds a week or so; the rest loads eight rows at a
// time from admin-ajax.php (action load_more_timetable, page 2, 3…), asked
// for while the last row is still inside the window.
func parseScenoteka(p *Page) ([]Show, error) {
	var shows []Show
	body := p.Body
	for page := 1; ; page++ {
		found, err := scenotekaRows(body, p)
		if err != nil {
			return nil, err
		}
		if len(found) == 0 {
			if page == 1 {
				return nil, errNoList
			}
			break
		}
		shows = append(shows, found...)
		if page == 8 || found[len(found)-1].Date.After(p.Today.AddDate(0, 0, Ahead)) {
			break
		}
		more := url.Values{"action": {"load_more_timetable"}, "page": {strconv.Itoa(page + 1)}}
		if body, err = p.More(link(p.URL, "/wp-admin/admin-ajax.php"), more); err != nil {
			return nil, err
		}
		if strings.TrimSpace(string(body)) == "" {
			break
		}
	}
	return shows, nil
}

func scenotekaRows(body []byte, p *Page) ([]Show, error) {
	doc, err := parseHTML(body)
	if err != nil {
		return nil, err
	}
	var shows []Show
	for _, row := range all(doc, class("timetable-row")) {
		when := first(row, class("col-20"))
		date, ok := Day(text(first(when, tag("b"))), p.Today)
		a := first(first(row, tag("h3")), tag("a"))
		if !ok || a == nil {
			continue
		}
		shows = append(shows, Show{Date: date, Time: Clock(text(when)), Title: text(a), Link: link(p.URL, attr(a, "href"))})
	}
	return shows, nil
}
