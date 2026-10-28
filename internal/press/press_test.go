package press

import (
	"regexp"
	"strings"
	"testing"
	"time"

	"tisak/internal/store"
	"tisak/web"
)

func TestDayLong(t *testing.T) {
	d := time.Date(2026, 10, 27, 5, 0, 0, 0, time.UTC)
	if got := DayLong(d); got != "Utorak, 27. listopada 2026." {
		t.Errorf("DayLong = %q", got)
	}
}

func TestPlural(t *testing.T) {
	for n, want := range map[string]string{"1": "komentar", "2": "komentara", "5": "komentara", "11": "komentara", "21": "komentar", "x": "komentara"} {
		if got := Plural(n, "komentar", "komentara", "komentara"); got != want {
			t.Errorf("Plural(%s) = %s, want %s", n, got, want)
		}
	}
	if got := Plural("3", "predstava", "predstave", "predstava"); got != "predstave" {
		t.Errorf("Plural(3) = %s", got)
	}
}

func card(title, link string) Card { return Card{Title: title, Link: link} }

func TestInterleave(t *testing.T) {
	a := []Card{card("A1", "a1"), card("A2", "a2"), card("A3", "a3")}
	b := []Card{card("B1", "b1"), card("A1!", "a1-copy"), card("B2", "b2")} // the same story under another feed
	got := interleave([][]Card{a, b}, []int{2, 2}, 6)
	var titles []string
	for _, c := range got {
		titles = append(titles, c.Title)
	}
	if strings.Join(titles, " ") != "A1 B1 A2 B2" {
		t.Errorf("got %v: want turns, quotas kept, the repeated story once", titles)
	}
	if got := interleave([][]Card{a, b}, []int{3, 3}, 3); len(got) != 3 {
		t.Errorf("limit not kept: %d cards", len(got))
	}
}

func TestLeadFirst(t *testing.T) {
	pic := &Pic{Hash: "ab", Ext: "jpg"}
	cards := []Card{card("no picture", "1"), {Title: "picture", Link: "2", Picture: pic}, {Title: "also", Link: "3", Picture: pic}}
	leadFirst(cards)
	if cards[0].Title != "picture" || cards[1].Title != "no picture" || cards[2].Picture != nil {
		t.Errorf("got %+v", cards)
	}
}

// A rendered edition must stand alone: no stylesheet or script files, no local paths except its own images/.
func TestRenderStandsAlone(t *testing.T) {
	p := &Press{Web: web.FS(""), Site: "/", Loc: time.UTC}
	logo := &Pic{Hash: "aa11", Ext: "svg"}
	snap := Snapshot{Day: "2026-10-27", DayLong: "Utorak, 27. listopada 2026.", Year: 2026, Number: 1, Version: 2, Reason: "ispravak",
		Sections: []Section{
			{Slug: "vijesti", Title: "Vijesti", Layout: "lead2row3", LeadPicture: true, InMenu: true, Cards: []Card{
				{Title: "Prva <b>vijest</b>", Link: "https://x/1", Summary: "Sažetak", Picture: &Pic{Hash: "bb22", Ext: "jpg", W: 1000, H: 563}, Source: SourceRef{Name: "HRT", Logo: logo}},
				{Title: "Druga", Link: "https://x/2", Source: SourceRef{Name: "Glazba.hr"}},
				{Title: "Treća", Link: "https://x/3", Source: SourceRef{Name: "HRT", Logo: logo}},
				{Title: "Četvrta", Link: "https://x/4", Source: SourceRef{Name: "HRT", Logo: logo}},
			}},
			{Slug: "hacker-news", Title: "Hacker News", Layout: "six", Cards: []Card{
				{Title: "Show HN", Link: "https://y/1", Author: "pg", Points: "42", Comments: "1", CommentsLink: "https://news.ycombinator.com/item?id=1"},
			}},
		}}
	html, err := p.Render("edition", snap)
	if err != nil {
		t.Fatal(err)
	}
	page := string(html)
	for _, want := range []string{
		`<span class="masthead-vol">Verzija 2</span>`, `<span class="masthead-vol">Br. 1</span>`, `<h2 id="vijesti">Vijesti</h2>`,
		`href="#vijesti"`, `class="col col--lead"`, `src="images/bb22.jpg"`, `src="images/aa11.svg"`,
		`Prva &lt;b&gt;vijest&lt;/b&gt;`, `<span class="card-source-name">Glazba.hr</span>`, `1 komentar</a>`, `font/woff2;base64,`,
	} {
		if !strings.Contains(page, want) {
			t.Errorf("page lacks %s", want)
		}
	}
	if strings.Contains(page, `href="#hacker-news"`) {
		t.Error("Hacker News is not in the menu")
	}
	for _, bad := range []*regexp.Regexp{
		regexp.MustCompile(`<link[^>]+stylesheet`), regexp.MustCompile(`<script[^>]+src=`), regexp.MustCompile(`url\((["']?)(fonts|icons|static)/`),
		regexp.MustCompile(`src="(?:/|static/|fonts/)`),
	} {
		if m := bad.FindString(page); m != "" {
			t.Errorf("page depends on an outside file: %s", m)
		}
	}
	if _, err := p.Render("sve", snap); err != nil {
		t.Errorf("sve: %v", err)
	}
}

// The culture pages print after Kultura, with Billboard beside Glazba's news; all the data here is made up.
func TestRenderCulture(t *testing.T) {
	p := &Press{Web: web.FS(""), Site: "/", Loc: time.UTC}
	poster := &Pic{Hash: "cc33", Ext: "jpg", W: 500, H: 750}
	snap := Snapshot{Day: "2026-10-28", DayLong: "Srijeda, 28. listopada 2026.", Year: 2026, Number: 2, Version: 1,
		Sections: []Section{
			{Slug: "glazba", Title: "Glazba", Layout: "music", InMenu: true, Cards: []Card{card("G1", "g1"), card("G2", "g2"),
				card("G3", "g3"), card("G4", "g4"), card("G5", "g5")},
				Chart: &ChartPanel{Week: "31. 10. 2026.", Songs: []ChartRow{{1, "Pjesma", "Netko", "same"}, {2, "Druga", "Netko drugi", "up"}},
					Albums: []ChartRow{{1, "Album", "Bend", "new"}}}},
			{Slug: "kultura", Title: "Kultura", Layout: "row3", InMenu: true, Cards: []Card{card("K1", "k1")}},
			{Slug: "tehnologija", Title: "Tehnologija", Layout: "row3", InMenu: true, Cards: []Card{card("T1", "t1")}},
		},
		CultureAfter: "kultura",
		Events: []EventCard{{Title: "Izložba", Venue: "Galerija", When: "do 4.11.", Price: "5 €", Colors: eventColors[0]},
			{Title: "Koncert", When: "Pet 30.10. · 20:00", Link: "https://primjer.hr/k", Poster: poster, Colors: eventColors[1]}},
		Films: &FilmsPanel{Week: "22. 10. – 28. 10.", Picks: []FilmCard{{Title: "Film", Link: "https://www.themoviedb.org/movie/1", Poster: poster}},
			Soon: []FilmDay{{Day: "Četvrtak", Date: "29 · X · 2026", Films: []FilmCard{{Title: "Bez postera", Link: "https://www.themoviedb.org/movie/2"}}}}},
		Theatres: &TheatresPanel{Count: 2, ThisWeek: []ShowDay{{Day: "Srijeda", Date: "28 · X · 2026", Count: 1,
			Shows: []ShowRow{{Title: "Predstava", Theatre: "Teatar EXIT", Initials: "TE", Link: "https://primjer.hr/p", Times: []string{"18:00", "20:00"}}}}}},
	}
	html, err := p.Render("edition", snap)
	if err != nil {
		t.Fatal(err)
	}
	page := string(html)
	for _, want := range []string{
		`<span class="panel-source">31. 10. 2026.</span>`, `<span class="chart__move chart__move--up" aria-label="gore">▲</span>`,
		`<span class="chart__move chart__move--new">novo</span>`, `style="--bg:#e8442e;--fg:#fff4e0"`, `class="piece piece--poster"`,
		`<p>do 4.11. · 5 €</p>`, `<span class="movies__blockmeta">22. 10. – 28. 10.</span>`, `<div class="movies__poster"><span>Poster</span></div>`,
		`Zagreb · 2 kazališta`, `28 · X · 2026 · 1 predstava`, `<li>18:00</li><li>20:00</li>`, `<span>TE</span>`, `src="images/cc33.jpg"`,
		`Ovaj proizvod koristi TMDB API`,
	} {
		if !strings.Contains(page, want) {
			t.Errorf("page lacks %s", want)
		}
	}
	order := []string{`id="glazba"`, `id="kultura"`, `id="dogadanja"`, `id="filmovi"`, `id="kazalista"`, `id="tehnologija"`}
	menu := []string{`href="#kultura"`, `href="#dogadanja"`, `href="#filmovi"`, `href="#kazalista"`, `href="#tehnologija"`}
	for _, list := range [][]string{order, menu} {
		at := 0
		for _, s := range list {
			i := strings.Index(page[at:], s)
			if i < 0 {
				t.Errorf("%s is missing or out of order", s)
				break
			}
			at += i
		}
	}
	if got := snap.pictures(); len(got) != 1 {
		t.Errorf("pictures = %v, want the one poster once", got)
	}
}

func TestWhen(t *testing.T) {
	day := time.Date(2026, 10, 28, 0, 0, 0, 0, time.UTC)
	d := func(n int) time.Time { return day.AddDate(0, 0, n) }
	for _, c := range []struct {
		e    store.Event
		want string
	}{
		{store.Event{Start: d(1), End: d(1), Time: "20:00"}, "Čet 29.10. · 20:00"},
		{store.Event{Start: d(-3), End: d(7)}, "do 4.11."},
		{store.Event{Start: d(4), End: d(12)}, "1.11. – 9.11."},
	} {
		if got := When(c.e, day); got != c.want {
			t.Errorf("When = %q, want %q", got, c.want)
		}
	}
	if cinemaWeek(day).Weekday() != time.Thursday || !cinemaWeek(d(1)).Equal(d(1)) || !cinemaWeek(day).Equal(d(-6)) {
		t.Errorf("cinema week of %s is %s", day, cinemaWeek(day))
	}
	if initials("Teatar EXIT") != "TE" || initials("Gavella") != "G" || initials("Luda kuća") != "LK" {
		t.Error("initials")
	}
}

// Sport prints its match cards even without news, and the edition ends with the game, the dinner,
// O tisku, Izvori and Kontakt, in the design's order; all the data here is made up.
func TestRenderPlay(t *testing.T) {
	p := &Press{Web: web.FS(""), Site: "/", Loc: time.UTC}
	snap := Snapshot{Day: "2026-10-28", DayLong: "Srijeda, 28. listopada 2026.", Year: 2026, Number: 3, Version: 1,
		Sections: []Section{
			{Slug: "vijesti", Title: "Vijesti", Layout: "row3", InMenu: true, Cards: []Card{card("V1", "v1")}},
			{Slug: "sport", Title: "Sport", Layout: "sport", InMenu: true, Matches: &MatchPanel{
				Last: &MatchCard{League: "Kvalifikacije", When: "23. 10. 2026.", Home: "Hrvatska", Away: "Farski Otoci", Score: "2 – 1",
					Goals: []string{"Perišić 17'"}, Link: "https://www.thesportsdb.com/event/1"},
				Next: &MatchCard{League: "Liga nacija", When: "Čet 12.11.", Time: "20:45", Home: "Crna Gora", Away: "Hrvatska",
					Form: []FormRow{{Team: "Hrvatska", Results: "P N P"}}, Link: "https://www.thesportsdb.com/event/2"}}},
		},
		Game: &GamePanel{Day: "2026-10-28", Number: 7, Scrambled: "DsifXe4="},
		Dinner: &DinnerPanel{Courses: []store.Course{{Kind: "starter", Title: "Juha", Ingredients: []string{"bundeva"}, Method: []string{"Skuhaj."}}},
			Cocktail: &store.Course{Kind: "cocktail", Title: "Negroni", Ingredients: []string{"gin"}}},
		Sources: []SourceChip{{Name: "HRT", Logo: &Pic{Hash: "dd44", Ext: "svg"}}, {Name: "Glazba.hr"}},
		Contact: &ContactPanel{Channels: []Channel{{Label: "Općenito", Value: "u@primjer.hr", Link: "mailto:u@primjer.hr"}}, Action: "/kontakt"},
	}
	html, err := p.Render("edition", snap)
	if err != nil {
		t.Fatal(err)
	}
	page := string(html)
	for _, want := range []string{
		`<div class="score-box">2 – 1</div>`, `<li>Perišić 17&#39;</li>`, `<div class="fixture-time">20:45</div>`, `Hrvatska · P N P`,
		`data-game="DsifXe4=" data-game-day="2026-10-28" data-game-n="7"`, `<span class="section-head-label">Br. 7</span>`,
		`<span class="course-label">Predjelo</span>`, `<h6>Negroni</h6>`, `<li>Skuhaj.</li>`, `Br. 3 · Verzija 1`,
		`<img src="images/dd44.svg" alt="HRT" draggable="false">`, `<span class="chip" title="Glazba.hr">Glazba.hr</span>`,
		`<a href="mailto:u@primjer.hr">u@primjer.hr</a>`, `<form class="contact__form" action="/kontakt" method="post">`,
		` data-play-lib>`, `Matter`,
	} {
		if !strings.Contains(page, want) {
			t.Errorf("page lacks %s", want)
		}
	}
	if words := regexp.MustCompile(`data-game-words>([^<]*)<`).FindStringSubmatch(page); words == nil || strings.Count(words[1], " ") < 10000 {
		t.Error("the guess list is not in the page")
	}
	order := []string{`id="vijesti"`, `id="sport"`, `class="col col-match"`, `id="rijecililica"`, `id="vecera"`, `id="o-tisku"`, `id="izvori"`, `id="kontakt"`}
	menu := []string{`href="#vijesti"`, `href="#sport"`, `href="#rijecililica"`, `href="#vecera"`, `href="#o-tisku"`, `href="#izvori"`, `href="#kontakt"`, `href="sve.html"`}
	for _, list := range [][]string{order, menu} {
		at := 0
		for _, s := range list {
			i := strings.Index(page[at:], s)
			if i < 0 {
				t.Errorf("%s is missing or out of order", s)
				break
			}
			at += i
		}
	}
	if m := regexp.MustCompile(`<script[^>]+src=`).FindString(page); m != "" {
		t.Errorf("page depends on an outside script: %s", m)
	}

	// a quiet day: no game, dinner, sources or contact, and the menu leaves them out
	html, err = p.Render("edition", Snapshot{Day: "2026-10-28", Number: 4, Version: 1})
	if err != nil {
		t.Fatal(err)
	}
	for _, gone := range []string{`href="#rijecililica"`, `href="#vecera"`, `href="#izvori"`, `href="#kontakt"`, `<script type="text/plain" data-play-lib>`} {
		if strings.Contains(string(html), gone) {
			t.Errorf("an empty day prints %s", gone)
		}
	}
}
