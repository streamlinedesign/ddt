package theatre

import (
	"strings"

	"golang.org/x/net/html"
)

// Komedija: a table of the Events Manager plugin, 30 shows a page, more on
// ?pno=2. Each row: the date ("29.10. , Čet"), the title linked, the stage
// and "19:30 -22:30", and a cancellation note.
func parseKomedija(p *Page) ([]Show, error) {
	var shows []Show
	body := p.Body
	for page := 1; ; page++ {
		doc, err := parseHTML(body)
		if err != nil {
			return nil, err
		}
		list := first(doc, class("em-events-list"))
		if list == nil {
			if page == 1 {
				return nil, errNoList
			}
			break
		}
		for _, tr := range all(list, tag("tr")) {
			cells := children(tr, "td")
			if len(cells) < 2 {
				continue // the header
			}
			if strings.Contains(strings.ToUpper(text(first(tr, class("otkazano")))), "OTKAZ") {
				continue
			}
			date, ok := Day(text(cells[0]), p.Today)
			a := first(cells[1], both(tag("a"), func(n *html.Node) bool { return text(n) != "" }))
			if !ok || a == nil {
				continue
			}
			shows = append(shows, Show{Date: date, Time: Clock(text(first(cells[1], class("adresa")))), Title: text(a),
				Link: link(p.URL, attr(a, "href"))})
		}
		// Another page only while this one ends inside the window.
		next := first(list, class("next", "page-numbers"))
		if next == nil || page == 3 || len(shows) == 0 || shows[len(shows)-1].Date.After(p.Today.AddDate(0, 0, Ahead)) {
			break
		}
		if body, err = p.More(link(p.URL, attr(next, "href")), nil); err != nil {
			return nil, err
		}
	}
	return shows, nil
}

// children are n's child elements named name.
func children(n *html.Node, name string) []*html.Node {
	var out []*html.Node
	for c := range n.ChildNodes() {
		if c.Type == html.ElementNode && c.Data == name {
			out = append(out, c)
		}
	}
	return out
}
