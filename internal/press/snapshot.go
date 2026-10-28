package press

import (
	"fmt"
	"strconv"
	"time"

	"tisak/internal/store"
)

// Snapshot is everything one edition shows, frozen at print time. It is stored with the edition
// as JSON, so any edition can be rendered again or inspected later.
type Snapshot struct {
	Day       string    `json:"day"` // 2026-10-27
	DayLong   string    `json:"day_long"`
	Year      int       `json:"year"`
	Number    int       `json:"number"`
	Version   int       `json:"version"`
	Reason    string    `json:"reason,omitempty"`
	PrintedAt time.Time `json:"printed_at"`
	// Lead is the main article; the side column beside it has the weather, the word, NASA's picture and the album.
	Lead     *Lead            `json:"lead,omitempty"`
	Weather  []store.Forecast `json:"weather,omitempty"`
	Word     *WordPanel       `json:"word,omitempty"`
	Sky      *SkyPanel        `json:"sky,omitempty"`
	Album    *AlbumPanel      `json:"album,omitempty"`
	Sections []Section        `json:"sections"`
	// The culture pages print after the section CultureAfter names, or after the last one.
	Events       []EventCard    `json:"events,omitempty"`
	Films        *FilmsPanel    `json:"films,omitempty"`
	Theatres     *TheatresPanel `json:"theatres,omitempty"`
	CultureAfter string         `json:"culture_after,omitempty"`
	// The end of the edition: the game, the dinner, the sources and Kontakt.
	Game    *GamePanel    `json:"game,omitempty"`
	Dinner  *DinnerPanel  `json:"dinner,omitempty"`
	Sources []SourceChip  `json:"sources,omitempty"`
	Contact *ContactPanel `json:"contact,omitempty"`
	// Everything is the day's full haul for the Sve iz dana page.
	Everything []AllCategory `json:"everything"`
}

type Section struct {
	Slug        string      `json:"slug"`
	Title       string      `json:"title"`
	Layout      string      `json:"layout"`
	Flag        *Pic        `json:"flag,omitempty"`
	LeadPicture bool        `json:"lead_picture"`
	InMenu      bool        `json:"in_menu"`
	Cards       []Card      `json:"cards"`
	Chart       *ChartPanel `json:"chart,omitempty"`   // Glazba's layout only
	Matches     *MatchPanel `json:"matches,omitempty"` // Sport's layout only
}

type Card struct {
	ItemID       int64     `json:"item_id"`
	Title        string    `json:"title"`
	Summary      string    `json:"summary,omitempty"`
	Link         string    `json:"link"`
	Author       string    `json:"author,omitempty"`
	Published    time.Time `json:"published,omitzero"`
	Time         string    `json:"time,omitempty"` // 07:30, local
	Source       SourceRef `json:"source"`
	Picture      *Pic      `json:"picture,omitempty"`
	Points       string    `json:"points,omitempty"`
	Comments     string    `json:"comments,omitempty"`
	CommentsLink string    `json:"comments_link,omitempty"`
}

type SourceRef struct {
	Name string `json:"name"`
	Logo *Pic   `json:"logo,omitempty"`
}

// Pic is a stored picture; the edition carries it as images/<hash>.<ext>.
type Pic struct {
	Hash string `json:"hash"`
	Ext  string `json:"ext"`
	W    int    `json:"w,omitempty"`
	H    int    `json:"h,omitempty"`
}

func (p Pic) File() string { return p.Hash + "." + p.Ext }

type AllCategory struct {
	Slug    string      `json:"slug"`
	Title   string      `json:"title"`
	Sources []AllSource `json:"sources"`
}

type AllSource struct {
	Name  string `json:"name"`
	Items []Card `json:"items"`
}

var (
	days   = []string{"Nedjelja", "Ponedjeljak", "Utorak", "Srijeda", "Četvrtak", "Petak", "Subota"}
	months = []string{"siječnja", "veljače", "ožujka", "travnja", "svibnja", "lipnja",
		"srpnja", "kolovoza", "rujna", "listopada", "studenoga", "prosinca"}
)

// DayLong writes a date the Croatian way: Utorak, 27. listopada 2026.
func DayLong(t time.Time) string {
	return fmt.Sprintf("%s, %d. %s %d.", days[t.Weekday()], t.Day(), months[t.Month()-1], t.Year())
}

// Plural picks the Croatian form for a count: 1 komentar, 2 komentara, 5 komentara.
func Plural(n string, one, few, many string) string {
	v, err := strconv.Atoi(n)
	if err != nil {
		return many
	}
	switch {
	case v%10 == 1 && v%100 != 11:
		return one
	case v%10 >= 2 && v%10 <= 4 && (v%100 < 12 || v%100 > 14):
		return few
	}
	return many
}
