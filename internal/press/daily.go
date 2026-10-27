package press

import (
	"context"
	"errors"
	"html"
	"html/template"
	"regexp"
	"strings"
	"time"

	"tisak/internal/store"
)

// Lead is the main article, printed whole.
type Lead struct {
	Title       string      `json:"title"`
	Description string      `json:"description,omitempty"`
	Author      string      `json:"author,omitempty"`
	Picture     *Pic        `json:"picture,omitempty"`
	PictureAlt  string      `json:"picture_alt,omitempty"`
	Credit      string      `json:"credit,omitempty"`
	Blocks      []LeadBlock `json:"blocks"`
}

// LeadBlock is a paragraph, a subheading, a quote or a picture of the main article.
type LeadBlock struct {
	Type    string `json:"type"` // p, h, quote, img
	Text    string `json:"text,omitempty"`
	Picture *Pic   `json:"picture,omitempty"`
	Caption string `json:"caption,omitempty"`
}

// WordPanel is the word of the day.
type WordPanel struct {
	Word        string `json:"word"`
	Link        string `json:"link,omitempty"`
	Description string `json:"description,omitempty"`
}

// SkyPanel is NASA's Astronomy Picture of the Day: the day before's in the morning edition.
type SkyPanel struct {
	Day     string `json:"day"`
	Title   string `json:"title"`
	Link    string `json:"link"`
	Picture *Pic   `json:"picture,omitempty"` // none for a video
	Alt     string `json:"alt,omitempty"`
	Credit  string `json:"credit,omitempty"`
}

// AlbumPanel is the album of the day.
type AlbumPanel struct {
	Artist  string `json:"artist"`
	Title   string `json:"title"`
	Year    int    `json:"year,omitempty"`
	Genre   string `json:"genre,omitempty"`
	Minutes int    `json:"minutes,omitempty"`
	Tracks  int    `json:"tracks,omitempty"`
	Cover   *Pic   `json:"cover,omitempty"`
	Link    string `json:"link,omitempty"`
}

// skyAge is how old NASA's picture may be and still print: older means the feed is down.
const skyAge = 3 * 24 * time.Hour

// pickDaily fills the main article and the side column for the day.
func (p *Press) pickDaily(ctx context.Context, snap *Snapshot, now time.Time) error {
	day := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)

	a, err := p.Store.MainArticle(ctx, day)
	switch {
	case err == nil:
		snap.Lead = p.lead(ctx, a)
	case !errors.Is(err, store.ErrNotFound):
		return err
	}

	if snap.Weather, err = p.Store.WeatherFor(ctx, day); err != nil {
		return err
	}

	w, err := p.Store.WordFor(ctx, day)
	switch {
	case err == nil:
		snap.Word = &WordPanel{Word: w.Word, Link: w.Link, Description: w.Description}
	case !errors.Is(err, store.ErrNotFound):
		return err
	}

	sky, err := p.Store.LatestAPOD(ctx, day)
	switch {
	case err == nil && day.Sub(sky.Day) <= skyAge:
		snap.Sky = &SkyPanel{Day: sky.Day.Format("2006-01-02"), Title: sky.Title, Link: sky.Link,
			Picture: p.pic(ctx, sky.Picture), Alt: sky.Alt, Credit: sky.Credit}
	case err != nil && !errors.Is(err, store.ErrNotFound):
		return err
	}

	al, err := p.Store.AlbumFor(ctx, day)
	switch {
	case err == nil:
		snap.Album = &AlbumPanel{Artist: al.Artist, Title: al.Title, Year: al.Year, Genre: al.Genre, Minutes: al.Minutes,
			Tracks: al.Tracks, Cover: p.pic(ctx, al.Cover), Link: al.Link}
	case !errors.Is(err, store.ErrNotFound):
		return err
	}
	return nil
}

// lead turns an article into the printed main article, leaving out empty blocks.
func (p *Press) lead(ctx context.Context, a store.Article) *Lead {
	l := &Lead{Title: a.Title, Description: a.Description, Author: a.Author, Picture: p.pic(ctx, a.Picture),
		PictureAlt: a.PictureAlt, Credit: a.PictureCredit}
	for _, b := range a.Blocks {
		lb := LeadBlock{Type: b.Type, Text: strings.TrimSpace(b.Text), Caption: strings.TrimSpace(b.Caption)}
		switch b.Type {
		case "img":
			if lb.Picture = p.pic(ctx, b.Picture); lb.Picture == nil {
				continue
			}
		case "p", "h", "quote":
			if lb.Text == "" {
				continue
			}
		default:
			continue
		}
		l.Blocks = append(l.Blocks, lb)
	}
	return l
}

var (
	markLink   = regexp.MustCompile(`\[([^\]\n]+)\]\((https?://[^\s()]+)\)`)
	markBold   = regexp.MustCompile(`\*\*([^*\n]+)\*\*`)
	markItalic = regexp.MustCompile(`\*([^*\n]+)\*`)
)

// Inline writes an article's text as HTML. The admin types plain text with three marks:
// *italic*, **bold** and [words](https://link). A line break stays a line break. Nothing else is HTML.
func Inline(s string) template.HTML {
	s = html.EscapeString(strings.TrimSpace(s))
	s = markLink.ReplaceAllString(s, `<a href="$2">$1</a>`)
	s = markBold.ReplaceAllString(s, `<strong>$1</strong>`)
	s = markItalic.ReplaceAllString(s, `<em>$1</em>`)
	s = strings.ReplaceAll(s, "\n", "<br>\n")
	return template.HTML(s)
}

// Plain is an article's text without its marks, for a short line in the dashboard.
func Plain(s string) string {
	s = markLink.ReplaceAllString(s, "$1")
	s = markBold.ReplaceAllString(s, "$1")
	return markItalic.ReplaceAllString(s, "$1")
}

// Host names a link's site for a panel's source link: enciklopedija.hr.
func Host(link string) string {
	s := strings.TrimPrefix(strings.TrimPrefix(link, "https://"), "http://")
	if i := strings.IndexAny(s, "/?#"); i >= 0 {
		s = s[:i]
	}
	return strings.TrimPrefix(s, "www.")
}
