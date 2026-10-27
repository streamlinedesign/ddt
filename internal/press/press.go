// Package press prints editions: it picks what goes in, freezes it as a snapshot,
// renders the page with everything inlined, and writes the edition's folder. An edition never changes.
package press

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"html/template"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"
	"unicode"

	"tisak/internal/media"
	"tisak/internal/store"
)

type Press struct {
	Store *store.Store
	Media *media.Store
	Web   fs.FS  // templates/ and static/
	Out   string // editions go in Out/2026/10/27/v1/
	Site  string // the public site's root, for links back to the archive; "/" by default
	Loc   *time.Location
	Now   func() time.Time
}

// capacity is how many cards each layout holds.
var capacity = map[string]int{"row3": 3, "rows3": 6, "lead2": 3, "lead2row3": 6, "list": 12, "six": 12}

// Layout describes a section layout for the dashboard.
type Layout struct {
	Name, Label string
	Cards       int
}

// Layouts lists every layout, in the order the dashboard offers them.
func Layouts() []Layout {
	return []Layout{
		{"row3", "Jedan red od tri", capacity["row3"]},
		{"rows3", "Dva reda od tri", capacity["rows3"]},
		{"lead2", "Velika vijest i dvije uz nju", capacity["lead2"]},
		{"lead2row3", "Velika vijest, dvije uz nju i red od tri", capacity["lead2row3"]},
		{"list", "Popis naslova", capacity["list"]},
		{"six", "Šest stupaca (Hacker News)", capacity["six"]},
	}
}

// Window is how far back the press looks for items: a day and a half covers a quiet night.
const Window = 36 * time.Hour

// Print makes the next edition of today: Verzija 1 at five, one more for every reprint.
func (p *Press) Print(ctx context.Context, reason string) (store.Edition, string, error) {
	now := p.now()
	day := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)
	number, version, err := p.Store.NextNumbers(ctx, day)
	if err != nil {
		return store.Edition{}, "", err
	}
	snap, err := p.Pick(ctx, now)
	if err != nil {
		return store.Edition{}, "", err
	}
	snap.Number, snap.Version, snap.Reason = number, version, reason

	page, err := p.Render("edition", snap)
	if err != nil {
		return store.Edition{}, "", err
	}
	all, err := p.Render("sve", snap)
	if err != nil {
		return store.Edition{}, "", err
	}
	dir := p.Dir(day, version)
	if err := p.write(dir, page, all, snap); err != nil {
		return store.Edition{}, "", err
	}
	js, _ := json.Marshal(snap)
	e := store.Edition{Day: day, Number: number, Version: version, Reason: reason, Snapshot: js, HTML: string(page), Bytes: len(page)}
	if e.ID, err = p.Store.SaveEdition(ctx, e); err != nil {
		os.RemoveAll(dir) // the folder must not outlive a failed save
		return store.Edition{}, "", err
	}
	return e, dir, nil
}

func (p *Press) now() time.Time {
	t := time.Now()
	if p.Now != nil {
		t = p.Now()
	}
	return t.In(p.Loc)
}

// Dir is an edition's folder.
func (p *Press) Dir(day time.Time, version int) string {
	return filepath.Join(p.Out, day.Format("2006"), day.Format("01"), day.Format("02"), fmt.Sprintf("v%d", version))
}

// Pick chooses each category's cards from its sources and freezes them.
func (p *Press) Pick(ctx context.Context, now time.Time) (Snapshot, error) {
	snap := Snapshot{
		Day: now.Format("2006-01-02"), DayLong: DayLong(now), Year: now.Year(), PrintedAt: now,
	}
	cats, err := p.Store.Categories(ctx)
	if err != nil {
		return snap, err
	}
	sources, err := p.Store.Sources(ctx)
	if err != nil {
		return snap, err
	}
	since := now.Add(-Window)
	dayStart := now.Add(-24 * time.Hour)
	for _, c := range cats {
		if !c.Enabled {
			continue
		}
		sec := Section{Slug: c.Slug, Title: c.Title, Layout: c.Layout, LeadPicture: c.LeadPicture, InMenu: c.InMenu,
			Flag: p.pic(ctx, c.Flag)}
		all := AllCategory{Slug: c.Slug, Title: c.Title}
		var lists [][]Card
		var quota []int
		for _, s := range sources {
			if s.CategoryID != c.ID || !s.Enabled {
				continue
			}
			items, err := p.Store.RecentItems(ctx, s.ID, since, 60)
			if err != nil {
				return snap, err
			}
			ref := SourceRef{Name: s.Name, Logo: p.pic(ctx, s.Logo)}
			var cards, today []Card
			for _, it := range items {
				card := p.card(ctx, it, ref, now.Location())
				cards = append(cards, card)
				if it.PublishedAt == nil || !it.PublishedAt.Before(dayStart) {
					today = append(today, card)
				}
			}
			lists = append(lists, cards)
			quota = append(quota, s.PerEdition)
			if len(today) > 0 {
				all.Sources = append(all.Sources, AllSource{Name: s.Name, Items: today})
			}
		}
		sec.Cards = interleave(lists, quota, capacity[c.Layout])
		if c.LeadPicture {
			leadFirst(sec.Cards)
		} else {
			for i := range sec.Cards {
				sec.Cards[i].Picture = nil
			}
		}
		if len(sec.Cards) == 0 {
			continue // a category with nothing to show stays out of the edition and the menu
		}
		snap.Sections = append(snap.Sections, sec)
		if len(all.Sources) > 0 {
			snap.Everything = append(snap.Everything, all)
		}
	}
	return snap, nil
}

func (p *Press) card(ctx context.Context, it store.Item, ref SourceRef, loc *time.Location) Card {
	c := Card{ItemID: it.ID, Title: it.Title, Summary: it.Summary, Link: it.Link, Author: it.Author, Source: ref,
		Picture: p.pic(ctx, it.Picture), Points: it.Extra["points"], Comments: it.Extra["comment_count"], CommentsLink: it.Extra["comments"]}
	if it.PublishedAt != nil {
		c.Published = *it.PublishedAt
		c.Time = it.PublishedAt.In(loc).Format("15:04")
	}
	return c
}

func (p *Press) pic(ctx context.Context, hash string) *Pic {
	if hash == "" {
		return nil
	}
	m, err := p.Media.DB.MediaByHash(ctx, hash)
	if err != nil {
		return nil
	}
	return &Pic{Hash: m.Hash, Ext: m.Ext, W: m.Width, H: m.Height}
}

// interleave takes cards from each source in turn, up to each source's quota, until the layout is full.
// The same story from two feeds (HRT files some stories under several) prints once.
func interleave(lists [][]Card, quota []int, limit int) []Card {
	var out []Card
	seen := map[string]bool{}
	taken := make([]int, len(lists))
	next := make([]int, len(lists))
	for len(out) < limit {
		progress := false
		for i := range lists {
			if len(out) >= limit {
				break
			}
			for taken[i] < quota[i] && next[i] < len(lists[i]) {
				c := lists[i][next[i]]
				next[i]++
				key := storyKey(c)
				if seen[key] || seen[c.Link] {
					continue
				}
				seen[key], seen[c.Link] = true, true
				out = append(out, c)
				taken[i]++
				progress = true
				break
			}
		}
		if !progress {
			break
		}
	}
	return out
}

func storyKey(c Card) string {
	return strings.Map(func(r rune) rune {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			return unicode.ToLower(r)
		}
		return -1
	}, c.Title)
}

// leadFirst moves the first card with a picture to the front and drops the pictures of the rest,
// since only the lead prints one.
func leadFirst(cards []Card) {
	for i, c := range cards {
		if c.Picture != nil {
			copy(cards[1:i+1], cards[:i])
			cards[0] = c
			break
		}
	}
	for i := 1; i < len(cards); i++ {
		cards[i].Picture = nil
	}
}

// write puts the edition's folder together under a temporary name, then moves it into place,
// so a half-written edition is never served.
func (p *Press) write(dir string, page, all []byte, snap Snapshot) error {
	if _, err := os.Stat(dir); err == nil {
		return fmt.Errorf("edition folder %s already exists", dir)
	}
	tmp := dir + ".printing"
	os.RemoveAll(tmp)
	if err := os.MkdirAll(filepath.Join(tmp, "images"), 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(tmp, "index.html"), page, 0o644); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(tmp, "sve.html"), all, 0o644); err != nil {
		return err
	}
	for _, pic := range snap.pictures() {
		if err := linkOrCopy(p.Media.Path(pic.Hash, pic.Ext), filepath.Join(tmp, "images", pic.File())); err != nil {
			return fmt.Errorf("picture %s: %w", pic.File(), err)
		}
	}
	return os.Rename(tmp, dir)
}

func (s Snapshot) pictures() []Pic {
	seen := map[string]bool{}
	var out []Pic
	add := func(p *Pic) {
		if p != nil && !seen[p.Hash] {
			seen[p.Hash] = true
			out = append(out, *p)
		}
	}
	for _, sec := range s.Sections {
		add(sec.Flag)
		for _, c := range sec.Cards {
			add(c.Picture)
			add(c.Source.Logo)
		}
	}
	return out
}

// linkOrCopy hard-links a stored picture into an edition (no extra disk), copying when it cannot.
func linkOrCopy(from, to string) error {
	if err := os.Link(from, to); err == nil {
		return nil
	}
	in, err := os.Open(from)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.Create(to)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return err
	}
	return out.Close()
}

// Render runs a page template over a snapshot, with the styles and scripts inlined.
func (p *Press) Render(name string, snap Snapshot) ([]byte, error) {
	assets, err := p.assets()
	if err != nil {
		return nil, err
	}
	site := p.Site
	if site == "" {
		site = "/"
	}
	view := struct {
		Snapshot
		Assets   Assets
		Site     string
		Sve      string
		Swatches []string
	}{snap, assets, site, "sve.html", []string{"--bg", "--bg-subtle", "--fg", "--fg-muted", "--border", "--rule", "--accent", "--cold"}}
	return p.execute(name, view)
}

// Archive is the data the archive page shows.
type Archive struct {
	Assets Assets
	Site   string
	Sve    string
	Year   int
	Months []ArchiveMonth
}

type ArchiveMonth struct {
	Title    string // listopad 2026.
	Editions []ArchiveEdition
}

type ArchiveEdition struct {
	Link    string
	DayLong string
	Number  int
	Version int
	Reason  string
	KB      int
}

var monthNames = []string{"siječanj", "veljača", "ožujak", "travanj", "svibanj", "lipanj",
	"srpanj", "kolovoz", "rujan", "listopad", "studeni", "prosinac"}

// RenderArchive draws the archive: every edition, newest first, by month. Reprints sit under their day.
func (p *Press) RenderArchive(eds []store.EditionInfo) ([]byte, error) {
	assets, err := p.assets()
	if err != nil {
		return nil, err
	}
	site := p.Site
	if site == "" {
		site = "/"
	}
	a := Archive{Assets: assets, Site: site, Sve: site + "sve/", Year: p.now().Year()}
	for _, e := range eds {
		title := fmt.Sprintf("%s %d.", monthNames[e.Day.Month()-1], e.Day.Year())
		if len(a.Months) == 0 || a.Months[len(a.Months)-1].Title != title {
			a.Months = append(a.Months, ArchiveMonth{Title: title})
		}
		m := &a.Months[len(a.Months)-1]
		m.Editions = append(m.Editions, ArchiveEdition{
			Link:    fmt.Sprintf("%s%s/v%d/", site, e.Day.Format("2006/01/02"), e.Version),
			DayLong: DayLong(e.Day), Number: e.Number, Version: e.Version, Reason: e.Reason, KB: (e.Bytes + 512) / 1024,
		})
	}
	return p.execute("archive", a)
}

func (p *Press) execute(name string, data any) ([]byte, error) {
	t, err := template.New("").Funcs(template.FuncMap{
		"pic":    func(p Pic) string { return "images/" + p.File() },
		"rows":   rows,
		"take":   func(c []Card, n int) []Card { return c[:min(n, len(c))] },
		"skip":   func(c []Card, n int) []Card { return c[min(n, len(c)):] },
		"plural": Plural,
	}).ParseFS(p.Web, "templates/*.html")
	if err != nil {
		return nil, err
	}
	var buf bytes.Buffer
	if err := t.ExecuteTemplate(&buf, name, data); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func rows(cards []Card, n int) [][]Card {
	var out [][]Card
	for len(cards) > 0 {
		k := min(n, len(cards))
		out = append(out, cards[:k])
		cards = cards[k:]
	}
	return out
}
