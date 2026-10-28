package theatre

// KNAP: a row per show with the date ("28.10."), the title linked (to the
// show, or to its tickets) and the time.
func parseKNAP(p *Page) ([]Show, error) {
	doc, err := parseHTML(p.Body)
	if err != nil {
		return nil, err
	}
	rows := all(doc, class("timetable-rows"))
	if len(rows) == 0 {
		return nil, errNoList
	}
	var shows []Show
	for _, row := range rows {
		date, ok := Day(text(first(first(row, class("event-date")), tag("b"))), p.Today)
		a := first(first(row, class("event-title")), tag("a"))
		if !ok || a == nil {
			continue
		}
		shows = append(shows, Show{Date: date, Time: Clock(text(first(row, class("event-time")))), Title: text(a),
			Link: link(p.URL, attr(a, "href"))})
	}
	return shows, nil
}
