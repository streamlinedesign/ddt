package press

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"tisak/internal/store"
	"tisak/internal/tmdb"
)

// The culture pages of the edition: the events wall, the films and the theatres, and Billboard's top ten in Glazba.

// CultureAfter is the section the culture pages follow, as in the design; without it they close the edition.
const CultureAfter = "kultura"

// FilmsPanel is Filmovi: the films picked for the cinema week, and the ones coming to cinemas by day.
type FilmsPanel struct {
	Week  string     `json:"week"` // 29. 10. – 4. 11.
	Picks []FilmCard `json:"picks,omitempty"`
	Soon  []FilmDay  `json:"soon,omitempty"`
}

type FilmCard struct {
	Title    string `json:"title"`
	Original string `json:"original,omitempty"`
	Meta     string `json:"meta,omitempty"` // Drama, Triler · 2 h 10 min · 2026
	Link     string `json:"link"`
	Poster   *Pic   `json:"poster,omitempty"`
}

type FilmDay struct {
	Day   string     `json:"day"`  // Četvrtak
	Date  string     `json:"date"` // 29 · X · 2026
	Films []FilmCard `json:"films"`
}

// TheatresPanel is Kazališta: this week's shows to Sunday and next week's, by day, a show's times on one line.
type TheatresPanel struct {
	Count    int       `json:"count"` // theatres with a show
	ThisWeek []ShowDay `json:"this_week,omitempty"`
	NextWeek []ShowDay `json:"next_week,omitempty"`
}

type ShowDay struct {
	Day   string    `json:"day"`
	Date  string    `json:"date"`
	Count int       `json:"count"`
	Shows []ShowRow `json:"shows"`
}

type ShowRow struct {
	Title    string   `json:"title"`
	Theatre  string   `json:"theatre"`
	Initials string   `json:"initials"` // the poster's stand-in: G, TE
	Link     string   `json:"link,omitempty"`
	Times    []string `json:"times,omitempty"`
}

// EventCard is one poster on the Događanja wall.
type EventCard struct {
	Title  string `json:"title"`
	Venue  string `json:"venue,omitempty"`
	When   string `json:"when"` // Čet 29.10. · 20:00, do 4.11., 1.11. – 9.11.
	Price  string `json:"price,omitempty"`
	Link   string `json:"link,omitempty"`
	Poster *Pic   `json:"poster,omitempty"`
	Colors string `json:"colors"` // --bg and --fg of a poster without a picture
}

// ChartPanel is Billboard's top ten songs and albums, beside Glazba's news.
type ChartPanel struct {
	Week   string     `json:"week"` // 24. 10. 2026.
	Songs  []ChartRow `json:"songs,omitempty"`
	Albums []ChartRow `json:"albums,omitempty"`
}

type ChartRow struct {
	Rank   int    `json:"rank"`
	Title  string `json:"title"`
	Artist string `json:"artist"`
	Move   string `json:"move"` // up, down, new or same
}

// How far ahead the edition looks: releases and events two weeks, theatres to the Sunday after next.
const (
	releasesAhead = 13
	eventsAhead   = 13
)

// Charts and Billboard's two charts in the charts table, as the fetch keeps them.
const (
	chartSongs  = "hot100"
	chartAlbums = "billboard200"
	// chartAge is how old a chart may be and still print: Billboard's week is ahead of the date it comes out.
	chartAge = 21 * 24 * time.Hour
)

// pickCulture fills the events wall, the films and the theatres. Each is left out when it has nothing.
func (p *Press) pickCulture(ctx context.Context, snap *Snapshot, day time.Time) error {
	var err error
	if snap.Events, err = p.pickEvents(ctx, day); err != nil {
		return fmt.Errorf("events: %w", err)
	}
	if snap.Films, err = p.pickFilms(ctx, day); err != nil {
		return fmt.Errorf("films: %w", err)
	}
	if snap.Theatres, err = p.pickTheatres(ctx, day); err != nil {
		return fmt.Errorf("theatres: %w", err)
	}
	return nil
}

var shortDays = [...]string{"Ned", "Pon", "Uto", "Sri", "Čet", "Pet", "Sub"}

// DayLabel is "Čet 29.10.".
func DayLabel(t time.Time) string {
	return fmt.Sprintf("%s %d.%d.", shortDays[t.Weekday()], t.Day(), int(t.Month()))
}

// cinemaWeek is the Thursday that starts the cinema week holding t.
func cinemaWeek(t time.Time) time.Time {
	return t.AddDate(0, 0, -((int(t.Weekday()) - int(time.Thursday) + 7) % 7))
}

// romans are the months as the day heads print them: 30 · X · 2026.
var romans = [...]string{"", "I", "II", "III", "IV", "V", "VI", "VII", "VIII", "IX", "X", "XI", "XII"}

// dayHead is a day's head: Petak, 30 · X · 2026.
func dayHead(t time.Time) (string, string) {
	return days[t.Weekday()], fmt.Sprintf("%d · %s · %d", t.Day(), romans[t.Month()], t.Year())
}

func (p *Press) pickFilms(ctx context.Context, day time.Time) (*FilmsPanel, error) {
	week := cinemaWeek(day)
	end := week.AddDate(0, 0, 6)
	f := &FilmsPanel{Week: fmt.Sprintf("%d. %d. – %d. %d.", week.Day(), int(week.Month()), end.Day(), int(end.Month()))}
	picks, err := p.Store.FilmPicks(ctx, week)
	if err != nil {
		return nil, err
	}
	for _, x := range picks {
		f.Picks = append(f.Picks, p.filmCard(ctx, x))
	}
	rels, err := p.Store.Releases(ctx, day, day.AddDate(0, 0, releasesAhead))
	if err != nil {
		return nil, err
	}
	var last time.Time
	for _, r := range rels {
		if r.Hidden {
			continue
		}
		if !r.Day.Equal(last) {
			d, date := dayHead(r.Day)
			f.Soon = append(f.Soon, FilmDay{Day: d, Date: date})
			last = r.Day
		}
		at := &f.Soon[len(f.Soon)-1]
		at.Films = append(at.Films, p.filmCard(ctx, r.Film))
	}
	if len(f.Picks) == 0 && len(f.Soon) == 0 {
		return nil, nil
	}
	return f, nil
}

func (p *Press) filmCard(ctx context.Context, x store.Film) FilmCard {
	c := FilmCard{Title: x.Title, Link: tmdb.Link(x.TMDBID), Poster: p.pic(ctx, x.Poster)}
	if x.OriginalTitle != "" && x.OriginalTitle != x.Title {
		c.Original = x.OriginalTitle
	}
	var meta []string
	if x.Genres != "" {
		meta = append(meta, x.Genres)
	}
	if x.Runtime > 0 {
		meta = append(meta, runtime(x.Runtime))
	}
	if x.Year > 0 {
		meta = append(meta, strconv.Itoa(x.Year))
	}
	c.Meta = strings.Join(meta, " · ")
	return c
}

// runtime is "2 h 10 min" or "95 min".
func runtime(min int) string {
	switch {
	case min < 60:
		return fmt.Sprintf("%d min", min)
	case min%60 == 0:
		return fmt.Sprintf("%d h", min/60)
	}
	return fmt.Sprintf("%d h %d min", min/60, min%60)
}

func (p *Press) pickTheatres(ctx context.Context, day time.Time) (*TheatresPanel, error) {
	sunday := day.AddDate(0, 0, (7-int(day.Weekday()))%7)
	shows, err := p.Store.Shows(ctx, day, sunday.AddDate(0, 0, 7))
	if err != nil {
		return nil, err
	}
	t := &TheatresPanel{}
	t.ThisWeek, t.NextWeek, t.Count = showDays(shows, sunday)
	if t.Count == 0 {
		return nil, nil
	}
	return t, nil
}

// showDays groups the shows by day, a show's times on one line, into the days to sunday and the days
// after it, and counts the theatres.
func showDays(shows []store.Show, sunday time.Time) (this, next []ShowDay, theatres int) {
	seen := map[string]bool{}
	var days []ShowDay
	var dates []time.Time
	at := map[string]int{} // theatre|title → its row in the last day
	for _, s := range shows {
		if s.Hidden {
			continue
		}
		seen[s.Theatre] = true
		if len(dates) == 0 || !dates[len(dates)-1].Equal(s.Day) {
			d, date := dayHead(s.Day)
			days = append(days, ShowDay{Day: d, Date: date})
			dates = append(dates, s.Day)
			clear(at)
		}
		d := &days[len(days)-1]
		k := s.Theatre + "|" + strings.ToLower(s.Title)
		i, ok := at[k]
		if !ok {
			i = len(d.Shows)
			at[k] = i
			d.Shows = append(d.Shows, ShowRow{Title: s.Title, Theatre: s.Theatre, Initials: initials(s.Theatre), Link: s.Link})
			d.Count++
		}
		if s.Time != "" {
			d.Shows[i].Times = append(d.Shows[i].Times, s.Time)
		}
	}
	for i, d := range days {
		if dates[i].After(sunday) {
			next = append(next, d)
		} else {
			this = append(this, d)
		}
	}
	return this, next, len(seen)
}

// initials stand in for a show's poster: Gavella is G, Teatar EXIT is TE.
func initials(name string) string {
	var b strings.Builder
	for _, w := range strings.Fields(name) {
		if r, _ := utf8.DecodeRuneInString(w); unicode.IsLetter(r) {
			b.WriteRune(unicode.ToUpper(r))
		}
		if b.Len() >= 2 {
			break
		}
	}
	return b.String()
}

// eventColors are the wall's poster colours, from the design, the same order every print.
var eventColors = []string{
	"--bg:#e8442e;--fg:#fff4e0", "--bg:#1d3557;--fg:#f1faee", "--bg:#f4d35e;--fg:#1b1b1b", "--bg:#2a9d8f;--fg:#f8f5ec",
	"--bg:#111;--fg:#f5f5f5", "--bg:#f1e3c8;--fg:#3b2a1a", "--bg:#7b2cbf;--fg:#fdf0ff", "--bg:#ff8fab;--fg:#2b0a14",
}

// pickEvents is the wall: the events of the next two weeks, the ones already on first.
func (p *Press) pickEvents(ctx context.Context, day time.Time) ([]EventCard, error) {
	list, err := p.Store.EventsBetween(ctx, day, day.AddDate(0, 0, eventsAhead))
	if err != nil {
		return nil, err
	}
	var out []EventCard
	for i, e := range list {
		c := EventCard{Title: e.Title, Venue: e.Venue, Price: e.Price, Link: e.Link, Poster: p.pic(ctx, e.Poster),
			Colors: eventColors[i%len(eventColors)], When: When(e, day)}
		out = append(out, c)
	}
	return out, nil
}

// When says when an event is on, from the day of the edition: Čet 29.10., do 4.11., 1.11. – 9.11.,
// and the time after a dot.
func When(e store.Event, day time.Time) string {
	var s string
	switch {
	case e.Start.Equal(e.End):
		s = DayLabel(e.Start)
	case !e.Start.After(day):
		s = fmt.Sprintf("do %d.%d.", e.End.Day(), int(e.End.Month()))
	default:
		s = fmt.Sprintf("%d.%d. – %d.%d.", e.Start.Day(), int(e.Start.Month()), e.End.Day(), int(e.End.Month()))
	}
	if e.Time != "" {
		s += " · " + e.Time
	}
	return s
}

// pickChart is Billboard's top ten for Glazba, or nil when neither chart is recent.
func (p *Press) pickChart(ctx context.Context, day time.Time) (*ChartPanel, error) {
	c := &ChartPanel{}
	var newest time.Time
	for _, x := range []struct {
		chart string
		rows  *[]ChartRow
	}{{chartSongs, &c.Songs}, {chartAlbums, &c.Albums}} {
		// Billboard dates a chart by the Saturday after it comes out, so look a week ahead.
		week, top, err := p.Store.LatestChart(ctx, x.chart, day.AddDate(0, 0, 7))
		if err != nil {
			return nil, err
		}
		if week.IsZero() || day.Sub(week) > chartAge {
			continue
		}
		if week.After(newest) {
			newest = week
		}
		for _, e := range top {
			*x.rows = append(*x.rows, ChartRow{Rank: e.Rank, Title: e.Title, Artist: e.Artist, Move: move(e)})
		}
	}
	if newest.IsZero() {
		return nil, nil
	}
	c.Week = newest.Format("2. 1. 2006.")
	return c, nil
}

// move is a place's change since last week.
func move(e store.ChartEntry) string {
	switch {
	case e.Last == 0:
		return "new"
	case e.Last > e.Rank:
		return "up"
	case e.Last < e.Rank:
		return "down"
	}
	return "same"
}
