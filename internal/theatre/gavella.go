package theatre

import (
	"strings"

	"golang.org/x/net/html"
)

// Gavella: a table, a row per show. Its first cell (for phones) holds the
// date ("Utorak, 27.10.2026."), the time and the stage; the title cell is
// the one without a class, the title linked when it is a show.
func parseGavella(p *Page) ([]Show, error) {
	doc, err := parseHTML(p.Body)
	if err != nil {
		return nil, err
	}
	table := first(first(doc, class("frontpage-theater-play-events")), tag("table"))
	if table == nil {
		return nil, errNoList
	}
	var shows []Show
	for _, tr := range all(table, tag("tr")) {
		phone := first(tr, both(tag("td"), class("hidden-md-up")))
		var title *html.Node
		for _, td := range children(tr, "td") {
			if attr(td, "class") == "" {
				title = td
			}
		}
		if phone == nil || title == nil {
			continue
		}
		date, ok := Day(text(first(phone, class("date"))), p.Today)
		if !ok || text(title) == "" {
			continue
		}
		// A festival evening (Gavelline večeri) is "39. Gavelline večeri" in
		// bold and the visiting company and play below, its last line the
		// play: "Benjamin Hasić: ZA ŽIVOT CIJELI".
		name := text(first(title, both(tag("a"), func(n *html.Node) bool { return text(n) != "" })))
		if name == "" {
			name = text(first(title, tag("b")))
		}
		var play string
		for _, l := range lines(first(title, class("playcomment"))) {
			if !strings.HasPrefix(l, "-") { // "- Prodaja ulaznica u ZKM - u" is a note
				play = l
			}
		}
		if play != "" {
			name += " — " + play
		}
		if name == "" {
			name = text(title)
		}
		href := attr(first(title, tag("a")), "href")
		if href == "" {
			href = attr(first(first(tr, class("last")), tag("a")), "href") // the tickets
		}
		shows = append(shows, Show{Date: date, Time: Clock(text(first(phone, class("time")))), Title: name, Link: link(p.URL, href)})
	}
	return shows, nil
}
