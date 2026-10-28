package press

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"tisak/internal/game"
	"tisak/internal/store"
)

// The end of the edition: the matches inside Sport, then Riječililica, Večera, O tisku, Izvori and Kontakt.

// MatchPanel is Sport's middle column: the last result and the next match.
type MatchPanel struct {
	Last *MatchCard `json:"last,omitempty"`
	Next *MatchCard `json:"next,omitempty"`
}

type MatchCard struct {
	League string    `json:"league"`
	When   string    `json:"when"` // 6. 10. 2026. for a result; Čet 12.11. for a match ahead
	Time   string    `json:"time,omitempty"`
	Venue  string    `json:"venue,omitempty"`
	Home   string    `json:"home"`
	Away   string    `json:"away"`
	Score  string    `json:"score,omitempty"` // 1 – 2
	Goals  []string  `json:"goals,omitempty"` // Perišić 17'
	Form   []FormRow `json:"form,omitempty"`
	Link   string    `json:"link"`
}

// FormRow is a team's last results, newest last: P for a win, N for a draw, I for a loss.
type FormRow struct {
	Team    string `json:"team"`
	Results string `json:"results"` // P P N I P
}

// GamePanel is Riječililica's day: the word scrambled with the date, and the game's number.
type GamePanel struct {
	Day       string `json:"day"`
	Number    int    `json:"number"`
	Scrambled string `json:"scrambled"`
}

// DinnerPanel is Večera: up to three courses, and a cocktail.
type DinnerPanel struct {
	Courses  []store.Course `json:"courses,omitempty"`
	Cocktail *store.Course  `json:"cocktail,omitempty"`
}

// SourceChip is one source in Izvori: its logo, or its name.
type SourceChip struct {
	Name string `json:"name"`
	Logo *Pic   `json:"logo,omitempty"`
}

// ContactPanel is Kontakt: the addresses the admin set, and the form, which posts to the site.
type ContactPanel struct {
	Channels []Channel `json:"channels,omitempty"`
	Action   string    `json:"action"`
}

type Channel struct {
	Label string `json:"label"`
	Value string `json:"value"`
	Link  string `json:"link,omitempty"`
	Note  string `json:"note,omitempty"`
}

// How far around the day the match cards look. A national team plays about once a month, so the
// last result stays up until the next one.
const (
	resultBack   = 35
	fixtureAhead = 45
)

// pickPlay fills the game, the dinner, the sources and Kontakt; the matches go into Sport by its layout.
func (p *Press) pickPlay(ctx context.Context, snap *Snapshot, day time.Time) error {
	g, err := p.Store.DrawGameWord(ctx, day, game.Pick)
	if err != nil {
		return fmt.Errorf("riječililica: %w", err)
	}
	snap.Game = &GamePanel{Day: snap.Day, Number: g.Number, Scrambled: game.Scramble(g.Word, snap.Day)}
	d, err := p.Store.DinnerOn(ctx, day)
	switch {
	case err == nil:
		snap.Dinner = dinnerPanel(d)
	case !errors.Is(err, store.ErrNotFound):
		return fmt.Errorf("dinner: %w", err)
	}
	if snap.Sources, err = p.sourceChips(ctx); err != nil {
		return fmt.Errorf("sources: %w", err)
	}
	settings, err := p.Store.Settings(ctx)
	if err != nil {
		return err
	}
	site := p.Site
	if site == "" {
		site = "/"
	}
	snap.Contact = &ContactPanel{Channels: Channels(settings), Action: site + "kontakt"}
	return nil
}

func dinnerPanel(d store.Dinner) *DinnerPanel {
	out := &DinnerPanel{}
	for _, c := range d.Courses {
		if strings.TrimSpace(c.Title) == "" {
			continue
		}
		if c.Kind == "cocktail" {
			c := c
			out.Cocktail = &c
			continue
		}
		out.Courses = append(out.Courses, c)
	}
	if len(out.Courses) == 0 && out.Cocktail == nil {
		return nil
	}
	return out
}

// CourseLabels name the courses as the page prints them.
var CourseLabels = map[string]string{"starter": "Predjelo", "main": "Glavno jelo", "dessert": "Desert", "cocktail": "Koktel"}

// Contact's settings, as the dashboard keeps them.
const (
	SettingEmail = "kontakt_email"
	SettingPress = "kontakt_mediji"
	SettingPlace = "kontakt_adresa"
	SettingHours = "kontakt_vrijeme"
)

// Channels are Kontakt's addresses, in the design's order, the ones set only.
func Channels(s map[string]string) []Channel {
	var out []Channel
	if v := s[SettingEmail]; v != "" {
		out = append(out, Channel{Label: "Općenito", Value: v, Link: "mailto:" + v, Note: "Pitanja, ispravci i prijedlozi."})
	}
	if v := s[SettingPress]; v != "" {
		out = append(out, Channel{Label: "Za medije", Value: v, Link: "mailto:" + v})
	}
	if v := s[SettingPlace]; v != "" {
		out = append(out, Channel{Label: "Adresa", Value: v})
	}
	if v := s[SettingHours]; v != "" {
		out = append(out, Channel{Label: "Radno vrijeme", Value: v})
	}
	return out
}

// sourceChips are the sources in print, once each: feeds sharing a logo are one chip, named by
// what their names share ("HRT").
func (p *Press) sourceChips(ctx context.Context) ([]SourceChip, error) {
	sources, err := p.Store.Sources(ctx)
	if err != nil {
		return nil, err
	}
	var out []SourceChip
	byLogo := map[string]int{}
	byName := map[string]bool{}
	for _, s := range sources {
		if !s.Enabled {
			continue
		}
		if s.Logo != "" {
			if i, ok := byLogo[s.Logo]; ok {
				out[i].Name = sharedWords(out[i].Name, s.Name)
				continue
			}
			if pic := p.pic(ctx, s.Logo); pic != nil {
				byLogo[s.Logo] = len(out)
				out = append(out, SourceChip{Name: s.Name, Logo: pic})
				continue
			}
		}
		if !byName[s.Name] {
			byName[s.Name] = true
			out = append(out, SourceChip{Name: s.Name})
		}
	}
	return out, nil
}

// sharedWords is the words two names start with, or the first name: "HRT Vijesti" and "HRT EU" are "HRT".
func sharedWords(a, b string) string {
	aw, bw := strings.Fields(a), strings.Fields(b)
	n := 0
	for n < len(aw) && n < len(bw) && aw[n] == bw[n] {
		n++
	}
	if n == 0 {
		return a
	}
	return strings.Join(aw[:n], " ")
}

// pickMatches is Sport's middle column, or nil when there is no match to show.
func (p *Press) pickMatches(ctx context.Context, day time.Time) (*MatchPanel, error) {
	mp := &MatchPanel{}
	last, err := p.Store.LastResult(ctx, day.AddDate(0, 0, -resultBack), day)
	switch {
	case err == nil:
		mp.Last = resultCard(last)
	case !errors.Is(err, store.ErrNotFound):
		return nil, err
	}
	next, err := p.Store.NextFixture(ctx, day, day.AddDate(0, 0, fixtureAhead))
	switch {
	case err == nil:
		mp.Next = fixtureCard(next)
		results, err := p.Store.TeamResults(ctx, next.Team, day, 5)
		if err != nil {
			return nil, err
		}
		if f := form(next.Team, results); f != "" {
			mp.Next.Form = []FormRow{{Team: TeamName(next.Team), Results: f}}
		}
	case !errors.Is(err, store.ErrNotFound):
		return nil, err
	}
	if mp.Last == nil && mp.Next == nil {
		return nil, nil
	}
	return mp, nil
}

func matchLink(m store.Match) string { return "https://www.thesportsdb.com/event/" + m.EventID }

func resultCard(m store.Match) *MatchCard {
	c := &MatchCard{League: LeagueName(m.League), When: m.Day.Format("2. 1. 2006."), Home: TeamName(m.Home), Away: TeamName(m.Away),
		Score: fmt.Sprintf("%d – %d", *m.HomeScore, *m.AwayScore), Link: matchLink(m)}
	goals := append([]store.Goal(nil), m.Goals...)
	sort.SliceStable(goals, func(i, j int) bool { return goals[i].Minute < goals[j].Minute })
	for _, g := range goals {
		s := fmt.Sprintf("%s %d'", surname(g.Player), g.Minute)
		switch g.Kind {
		case "penalty":
			s += " (11 m)"
		case "own":
			s += " (autogol)"
		}
		c.Goals = append(c.Goals, s)
	}
	return c
}

func fixtureCard(m store.Match) *MatchCard {
	return &MatchCard{League: LeagueName(m.League), When: DayLabel(m.Day), Time: m.Time, Venue: m.Venue,
		Home: TeamName(m.Home), Away: TeamName(m.Away), Link: matchLink(m)}
}

// form is a team's results, oldest first: P P N I P.
func form(team string, newestFirst []store.Match) string {
	var out []string
	for i := len(newestFirst) - 1; i >= 0; i-- {
		m := newestFirst[i]
		us, them := *m.HomeScore, *m.AwayScore
		if m.Away == team {
			us, them = them, us
		}
		switch {
		case us > them:
			out = append(out, "P")
		case us == them:
			out = append(out, "N")
		default:
			out = append(out, "I")
		}
	}
	return strings.Join(out, " ")
}

// surname is how a scorer is listed: the last name, or the whole name when it is one word.
func surname(name string) string {
	f := strings.Fields(name)
	if len(f) == 0 {
		return name
	}
	return f[len(f)-1]
}

// Badge is a team's letter on the match card.
func Badge(team string) string {
	r, _ := utf8.DecodeRuneInString(team)
	return string(unicode.ToUpper(r))
}

// teamsHR are national teams in Croatian; clubs keep their own names.
var teamsHR = map[string]string{
	"Croatia": "Hrvatska", "Spain": "Španjolska", "England": "Engleska", "France": "Francuska", "Germany": "Njemačka",
	"Italy": "Italija", "Portugal": "Portugal", "Netherlands": "Nizozemska", "Belgium": "Belgija", "Denmark": "Danska",
	"Switzerland": "Švicarska", "Austria": "Austrija", "Poland": "Poljska", "Czech Republic": "Češka", "Czechia": "Češka",
	"Slovakia": "Slovačka", "Slovenia": "Slovenija", "Serbia": "Srbija", "Bosnia and Herzegovina": "Bosna i Hercegovina",
	"Bosnia-Herzegovina": "Bosna i Hercegovina", "Montenegro": "Crna Gora", "North Macedonia": "Sjeverna Makedonija",
	"Albania": "Albanija", "Kosovo": "Kosovo", "Hungary": "Mađarska", "Romania": "Rumunjska", "Bulgaria": "Bugarska",
	"Greece": "Grčka", "Turkey": "Turska", "Türkiye": "Turska", "Ukraine": "Ukrajina", "Scotland": "Škotska",
	"Wales": "Wales", "Northern Ireland": "Sjeverna Irska", "Republic of Ireland": "Irska", "Ireland": "Irska",
	"Norway": "Norveška", "Sweden": "Švedska", "Finland": "Finska", "Iceland": "Island", "Estonia": "Estonija",
	"Latvia": "Latvija", "Lithuania": "Litva", "Georgia": "Gruzija", "Armenia": "Armenija", "Azerbaijan": "Azerbajdžan",
	"Israel": "Izrael", "Cyprus": "Cipar", "Malta": "Malta", "Luxembourg": "Luksemburg", "Moldova": "Moldavija",
	"Belarus": "Bjelorusija", "Kazakhstan": "Kazahstan", "Faroe Islands": "Farski Otoci", "Gibraltar": "Gibraltar",
	"Andorra": "Andora", "San Marino": "San Marino", "Liechtenstein": "Lihtenštajn", "Brazil": "Brazil",
	"Argentina": "Argentina", "USA": "SAD", "United States": "SAD", "Mexico": "Meksiko", "Japan": "Japan",
	"Morocco": "Maroko", "Egypt": "Egipat", "Canada": "Kanada",
}

// TeamName is a team as the paper prints it.
func TeamName(s string) string {
	if hr, ok := teamsHR[s]; ok {
		return hr
	}
	return s
}

var leaguesHR = map[string]string{
	"UEFA Nations League": "Liga nacija", "FIFA World Cup": "Svjetsko prvenstvo",
	"FIFA World Cup Qualifying UEFA": "Kvalifikacije za SP", "UEFA European Championships": "Europsko prvenstvo",
	"UEFA European Championship Qualifying": "Kvalifikacije za EP", "International Friendlies": "Prijateljska utakmica",
	"UEFA Champions League": "Liga prvaka", "UEFA Europa League": "Europska liga",
	"UEFA Europa Conference League": "Konferencijska liga", "UEFA Conference League": "Konferencijska liga",
	"Croatian First Football League": "HNL", "Croatian Football Cup": "Hrvatski kup",
}

// LeagueName is a competition as the paper prints it.
func LeagueName(s string) string {
	if hr, ok := leaguesHR[s]; ok {
		return hr
	}
	return s
}
