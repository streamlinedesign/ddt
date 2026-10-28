package theatre

import (
	"regexp"
	"strings"

	"golang.org/x/net/html"
)

var exitTime = regexp.MustCompile(`^\d{1,2}:\d{2}\s*h`)

// Teatar EXIT: an article per show with the date ("26. 10. 2026."), the
// time ("19:00h"), the town when it is played elsewhere, and the title
// linked to /predstave/…. Shows outside Zagreb are left out.
func parseExit(p *Page) ([]Show, error) {
	doc, err := parseHTML(p.Body)
	if err != nil {
		return nil, err
	}
	articles := all(doc, tag("article"))
	if len(articles) == 0 {
		return nil, errNoList
	}
	var shows []Show
	for _, art := range articles {
		date, ok := Day(text(first(art, tag("h3"))), p.Today)
		a := first(art, both(tag("a"), func(n *html.Node) bool {
			return strings.HasPrefix(attr(n, "href"), "/predstave/") && first(n, tag("h2")) != nil
		}))
		if !ok || a == nil {
			continue
		}
		// At home the title follows the map link to Ilica 208; on tour, the
		// town.
		if prev := prevElement(a); prev != nil && prev.Data == "p" && !strings.Contains(strings.ToLower(text(prev)), "zagreb") {
			continue
		}
		at := first(art, both(tag("span"), func(n *html.Node) bool { return exitTime.MatchString(text(n)) }))
		shows = append(shows, Show{Date: date, Time: Clock(text(at)), Title: text(first(a, tag("h2"))),
			Link: link(p.URL, attr(a, "href"))})
	}
	return shows, nil
}

// prevElement is the element before n among its siblings, or nil.
func prevElement(n *html.Node) *html.Node {
	for s := n.PrevSibling; s != nil; s = s.PrevSibling {
		if s.Type == html.ElementNode {
			return s
		}
	}
	return nil
}
