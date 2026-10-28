package theatre

import (
	"fmt"
	"slices"
	"strings"
)

// Kerempuh: a section per month (data-month="11"), in it an item per show
// with the day ("25."), the title linked and the time ("20:00 H"). Shows
// Kerempuh plays elsewhere ("Kerempuh u gostima") are left out; guests who
// play at Kerempuh stay.
func parseKerempuh(p *Page) ([]Show, error) {
	doc, err := parseHTML(p.Body)
	if err != nil {
		return nil, err
	}
	months := all(doc, class("month-section"))
	if len(months) == 0 {
		return nil, errNoList
	}
	var shows []Show
	for _, m := range months {
		month := attr(m, "data-month")
		for _, it := range all(m, class("event-item")) {
			if slices.Contains(strings.Fields(attr(it, "class")), "kerempuh-u-gostima-bg") {
				continue
			}
			day := strings.TrimSuffix(text(first(it, class("day-number"))), ".")
			date, ok := Day(fmt.Sprintf("%s.%s.", day, month), p.Today)
			a := first(first(it, class("event-title")), tag("a"))
			if !ok || a == nil {
				continue
			}
			shows = append(shows, Show{Date: date, Time: Clock(text(first(it, class("time")))), Title: text(a),
				Link: link(p.URL, attr(a, "href"))})
		}
	}
	return shows, nil
}
