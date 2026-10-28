package theatre

import (
	"bytes"
	"errors"
	"net/url"
	"slices"
	"strings"

	"golang.org/x/net/html"
)

// The parsers' small toolkit for walking a page.

func parseHTML(body []byte) (*html.Node, error) {
	return html.Parse(bytes.NewReader(body))
}

// match tells whether an element is the one looked for.
type match func(*html.Node) bool

// class matches elements with every one of the classes.
func class(names ...string) match {
	return func(n *html.Node) bool {
		have := strings.Fields(attr(n, "class"))
		for _, c := range names {
			if !slices.Contains(have, c) {
				return false
			}
		}
		return true
	}
}

// tag matches elements named name.
func tag(name string) match {
	return func(n *html.Node) bool { return n.Data == name }
}

// both matches elements that a and b match.
func both(a, b match) match {
	return func(n *html.Node) bool { return a(n) && b(n) }
}

// all returns the elements under n (not n itself) that m matches, in
// document order.
func all(n *html.Node, m match) []*html.Node {
	var out []*html.Node
	for d := range n.Descendants() {
		if d.Type == html.ElementNode && m(d) {
			out = append(out, d)
		}
	}
	return out
}

// first returns the first element under n that m matches, or nil.
func first(n *html.Node, m match) *html.Node {
	if n == nil {
		return nil
	}
	for d := range n.Descendants() {
		if d.Type == html.ElementNode && m(d) {
			return d
		}
	}
	return nil
}

// text is the text under n, its spaces collapsed; "" for nil.
func text(n *html.Node) string {
	if n == nil {
		return ""
	}
	var b strings.Builder
	for d := range n.Descendants() {
		if d.Type == html.TextNode {
			b.WriteString(d.Data)
			b.WriteByte(' ')
		}
	}
	return strings.Join(strings.Fields(b.String()), " ")
}

// lines is the text under n split at its <br>s, empty lines left out.
func lines(n *html.Node) []string {
	if n == nil {
		return nil
	}
	var out []string
	var b strings.Builder
	flush := func() {
		if l := strings.Join(strings.Fields(b.String()), " "); l != "" {
			out = append(out, l)
		}
		b.Reset()
	}
	for d := range n.Descendants() {
		switch {
		case d.Type == html.TextNode:
			b.WriteString(d.Data)
			b.WriteByte(' ')
		case d.Type == html.ElementNode && d.Data == "br":
			flush()
		}
	}
	flush()
	return out
}

// attr is n's attribute key, or "".
func attr(n *html.Node, key string) string {
	if n == nil {
		return ""
	}
	for _, a := range n.Attr {
		if a.Key == key {
			return a.Val
		}
	}
	return ""
}

// link is href resolved against page; "" when it is empty, "#" or not http.
func link(page *url.URL, href string) string {
	href = strings.TrimSpace(href)
	if href == "" || strings.HasPrefix(href, "#") {
		return ""
	}
	u, err := url.Parse(href)
	if err != nil {
		return ""
	}
	if page != nil {
		u = page.ResolveReference(u)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return ""
	}
	return u.String()
}

// errNoList is a page without the list its parser looks for: the theatre
// has most likely changed its site.
var errNoList = errors.New("the schedule is not where it was on the page")
