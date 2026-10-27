package press

import (
	"regexp"
	"strings"
	"testing"
	"time"

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
