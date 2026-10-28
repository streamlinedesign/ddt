package theatre

import (
	"strings"
)

// HNK Zagreb: an item per show with the day, the title (h3; h4 is the
// composer) linked to the show at "#25.10.19.30", and the time with the
// stage ("19:30 Glavna pozornica HNK"). Guest performances abroad
// ("Gostovanje") are left out.
func parseHNK(p *Page) ([]Show, error) {
	doc, err := parseHTML(p.Body)
	if err != nil {
		return nil, err
	}
	items := all(doc, class("item", "keep-img-ratio"))
	if len(items) == 0 {
		return nil, errNoList
	}
	var shows []Show
	for _, it := range items {
		title := first(it, class("title"))
		a := first(title, tag("a"))
		if a == nil {
			continue
		}
		href := attr(a, "href")
		_, frag, _ := strings.Cut(href, "#") // "25.10.19.30"
		date, ok := Day(frag, p.Today)
		if !ok {
			continue
		}
		tickets := first(it, class("tickets"))
		if strings.Contains(text(tickets), "Gostovanje") {
			continue
		}
		shows = append(shows, Show{Date: date, Time: Clock(text(tickets)), Title: text(first(title, tag("h3"))),
			Link: link(p.URL, href)})
	}
	return shows, nil
}
