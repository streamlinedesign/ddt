package fetch

import (
	"bytes"
	"context"
	"encoding/xml"
	"errors"
	"fmt"
	"html"
	"io"
	"regexp"
	"strings"
	"time"

	"golang.org/x/text/encoding/charmap"

	"tisak/internal/store"
)

func init() { Register("rss", RSS{}) }

// RSS reads RSS 2.0 and Atom feeds. Settings: url.
type RSS struct{}

func (RSS) Fetch(ctx context.Context, c *Client, settings map[string]string) ([]store.Item, error) {
	url := settings["url"]
	if url == "" {
		return nil, errors.New("rss: no url in settings")
	}
	body, err := c.Get(ctx, url)
	if err != nil {
		return nil, err
	}
	items, err := ParseFeed(body)
	if err != nil {
		return nil, err
	}
	// Some feeds write local time and call it GMT (Germanijak). "zone" reads the clock in that zone instead.
	if z := settings["zone"]; z != "" {
		loc, err := time.LoadLocation(z)
		if err != nil {
			return nil, fmt.Errorf("rss: zone %q: %w", z, err)
		}
		for i, it := range items {
			if t := it.PublishedAt; t != nil {
				local := time.Date(t.Year(), t.Month(), t.Day(), t.Hour(), t.Minute(), t.Second(), 0, loc).UTC()
				items[i].PublishedAt = &local
			}
		}
	}
	return items, nil
}

type feed struct {
	XMLName xml.Name
	Items   []feedItem `xml:"channel>item"` // RSS
	Entries []feedItem `xml:"entry"`        // Atom
}

type feedItem struct {
	// Namespaced fields come first: encoding/xml gives an element to the first field whose name
	// matches, and a field without a namespace matches every namespace (media:description too).
	MediaTitle       string `xml:"http://search.yahoo.com/mrss/ title"`
	MediaDescription string `xml:"http://search.yahoo.com/mrss/ description"`
	MediaContent     []struct {
		URL    string `xml:"url,attr"`
		Medium string `xml:"medium,attr"`
		Type   string `xml:"type,attr"`
	} `xml:"http://search.yahoo.com/mrss/ content"`
	MediaThumb []struct {
		URL string `xml:"url,attr"`
	} `xml:"http://search.yahoo.com/mrss/ thumbnail"`
	Content string `xml:"http://purl.org/rss/1.0/modules/content/ encoded"`
	DCDate  string `xml:"http://purl.org/dc/elements/1.1/ date"`
	Creator string `xml:"http://purl.org/dc/elements/1.1/ creator"`

	Title       string     `xml:"title"`
	Links       []atomLink `xml:"link"`
	GUID        string     `xml:"guid"`
	ID          string     `xml:"id"`
	Description string     `xml:"description"`
	Summary     string     `xml:"summary"`
	AtomContent string     `xml:"content"`
	PubDate     string     `xml:"pubDate"`
	Published   string     `xml:"published"`
	Updated     string     `xml:"updated"`
	Author      struct {
		Name string `xml:"name"`
	} `xml:"author"`
	Comments   string `xml:"comments"`
	Enclosures []struct {
		URL  string `xml:"url,attr"`
		Type string `xml:"type,attr"`
	} `xml:"enclosure"`
	// HRT's own fields: a ready 740 px thumbnail and the full picture
	Thumb string `xml:"thumb"`
	Image string `xml:"image"`
}

// RSS <link> is text, Atom <link> is an attribute; one type reads both.
type atomLink struct {
	Href string `xml:"href,attr"`
	Rel  string `xml:"rel,attr"`
	Text string `xml:",chardata"`
}

// ParseFeed reads a feed body into items, in feed order (newest first in every feed we use).
func ParseFeed(body []byte) ([]store.Item, error) {
	items, _, err := parseFeed(body)
	return items, err
}

// parseFeed also gives each item's raw description, for fetchers that read more out of it.
func parseFeed(body []byte) ([]store.Item, []string, error) {
	body = bytes.TrimPrefix(body, []byte("\xef\xbb\xbf")) // HRT starts with a byte order mark
	dec := xml.NewDecoder(bytes.NewReader(body))
	dec.Strict = false
	dec.Entity = xml.HTMLEntity
	dec.CharsetReader = func(label string, r io.Reader) (io.Reader, error) {
		switch strings.ToLower(label) {
		case "utf-8", "utf8", "":
			return r, nil
		case "iso-8859-2", "latin2":
			return charmap.ISO8859_2.NewDecoder().Reader(r), nil
		case "windows-1250", "cp1250":
			return charmap.Windows1250.NewDecoder().Reader(r), nil
		case "iso-8859-1", "latin1", "windows-1252":
			return charmap.Windows1252.NewDecoder().Reader(r), nil
		}
		return nil, fmt.Errorf("charset %s", label)
	}
	var f feed
	if err := dec.Decode(&f); err != nil {
		return nil, nil, fmt.Errorf("feed: %w", err)
	}
	raw := append(f.Items, f.Entries...)
	items := make([]store.Item, 0, len(raw))
	descs := make([]string, 0, len(raw))
	for _, r := range raw {
		it := store.Item{
			Title:      Clean(r.Title),
			Link:       strings.TrimSpace(r.link()),
			Author:     Clean(first(r.Creator, r.Author.Name)),
			PictureURL: r.picture(),
			Extra:      map[string]string{},
		}
		it.Summary = Summary(first(r.Description, r.Summary, r.Content, r.AtomContent), 320)
		it.GUID = strings.TrimSpace(first(r.GUID, r.ID, it.Link))
		if t, ok := ParseDate(first(r.PubDate, r.Published, r.Updated, r.DCDate)); ok {
			it.PublishedAt = &t
		}
		if r.Comments != "" {
			it.Extra["comments"] = strings.TrimSpace(r.Comments)
		}
		if it.Title == "" || it.Link == "" {
			continue
		}
		items = append(items, it)
		descs = append(descs, first(r.Description, r.Summary, r.Content))
	}
	return items, descs, nil
}

func (r feedItem) link() string {
	for _, l := range r.Links {
		if l.Href != "" && (l.Rel == "" || l.Rel == "alternate") {
			return l.Href
		}
	}
	for _, l := range r.Links {
		if t := strings.TrimSpace(l.Text); t != "" {
			return t
		}
	}
	return ""
}

var imgSrc = regexp.MustCompile(`(?i)<img[^>]+src=["']([^"']+)["']`)

func (r feedItem) picture() string {
	if r.Thumb != "" {
		return strings.TrimSpace(r.Thumb)
	}
	if r.Image != "" {
		return strings.TrimSpace(r.Image)
	}
	for _, e := range r.Enclosures {
		if strings.HasPrefix(e.Type, "image/") && e.URL != "" {
			return e.URL
		}
	}
	for _, m := range r.MediaContent {
		if (m.Medium == "image" || strings.HasPrefix(m.Type, "image/")) && m.URL != "" {
			return m.URL
		}
	}
	for _, m := range r.MediaThumb {
		if m.URL != "" {
			return m.URL
		}
	}
	for _, s := range []string{r.Content, r.Description, r.AtomContent} {
		if m := imgSrc.FindStringSubmatch(s); m != nil {
			return html.UnescapeString(m[1])
		}
	}
	return ""
}

func first(s ...string) string {
	for _, v := range s {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}

var (
	tags     = regexp.MustCompile(`(?s)<[^>]*>`)
	spaces   = regexp.MustCompile(`\s+`)
	trailer  = regexp.MustCompile(`(?i)\s*(continue reading|read more|pročitaj(te)? više|nastavi čitati)\s*(…|\.\.\.|&hellip;)?\s*$`)
	wpFooter = regexp.MustCompile(`(?i)\s*(The post .{1,300} appeared first on .{1,80}|Objava .{1,300} pojavila se prvi (puta|put) na .{1,80})$`)
	ellipsis = regexp.MustCompile(`\s*\[(…|&#8230;|\.\.\.)\]\s*$`)
)

// Clean turns feed text into plain text: no tags, entities decoded, spaces folded.
func Clean(s string) string {
	s = tags.ReplaceAllString(s, " ")
	s = html.UnescapeString(html.UnescapeString(s)) // some feeds escape twice
	return strings.TrimSpace(spaces.ReplaceAllString(s, " "))
}

// Summary cleans a description and cuts it at a word near max runes, ending with an ellipsis.
func Summary(s string, max int) string {
	s = Clean(s)
	s = wpFooter.ReplaceAllString(s, "")
	if i := strings.Index(s, "[…]"); i > 0 { // WordPress cuts the excerpt here and pads it with a footer
		s = strings.TrimSpace(s[:i]) + "…"
	}
	s = trailer.ReplaceAllString(s, "")
	s = ellipsis.ReplaceAllString(s, "…")
	r := []rune(s)
	if len(r) <= max {
		return s
	}
	cut := string(r[:max])
	if i := strings.LastIndexAny(cut, " "); i > max/2 {
		cut = cut[:i]
	}
	return strings.TrimRight(cut, " ,;:–-") + "…"
}

var dateLayouts = []string{
	time.RFC1123Z, time.RFC1123, time.RFC3339, time.RFC3339Nano,
	"Mon, 2 Jan 2006 15:04:05 -0700", "Mon, 2 Jan 2006 15:04:05 MST", "Mon, 02 Jan 2006 15:04:05 Z",
	"Mon, 2 Jan 2006 15:04:05 Z", "2 Jan 2006 15:04:05 -0700", "2006-01-02T15:04:05", "2006-01-02 15:04:05",
}

// ParseDate reads the date formats feeds actually use.
func ParseDate(s string) (time.Time, bool) {
	s = strings.TrimSpace(s)
	if s == "" {
		return time.Time{}, false
	}
	for _, l := range dateLayouts {
		if t, err := time.Parse(l, s); err == nil {
			return t.UTC(), true
		}
	}
	return time.Time{}, false
}
