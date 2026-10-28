package theatre

import "strings"

// Luda kuća: a photo box per show, its heading "29.10. Četvrtak 19:30",
// the title below it and the picture linked to the tickets.
func parseLudaKuca(p *Page) ([]Show, error) {
	doc, err := parseHTML(p.Body)
	if err != nil {
		return nil, err
	}
	boxes := all(doc, class("kd-photobox"))
	if len(boxes) == 0 {
		return nil, errNoList
	}
	var shows []Show
	for _, box := range boxes {
		content := first(box, class("phb-content"))
		head := text(first(content, tag("h5")))
		date, ok := Day(head, p.Today)
		title := text(first(content, tag("p")))
		if !ok || title == "" || strings.EqualFold(title, "zakupljena predstava") {
			continue // a private booking
		}
		shows = append(shows, Show{Date: date, Time: Clock(head), Title: title,
			Link: link(p.URL, attr(first(first(box, class("photobox-img")), tag("a")), "href"))})
	}
	return shows, nil
}
