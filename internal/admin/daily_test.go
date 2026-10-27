package admin

import (
	"context"
	"net/http"
	"net/url"
	"os"
	"strings"
	"testing"

	"tisak/internal/store"
)

// TestFrontPage writes the top of an edition through the dashboard, as the admin would,
// and checks that the press prints it.
func TestFrontPage(t *testing.T) {
	if os.Getenv("TISAK_TEST_DB") == "" {
		t.Skip("TISAK_TEST_DB not set")
	}
	a, h := testAdmin(t)
	ctx := context.Background()
	b := &browserT{t: t, h: h, ip: "10.0.0.5"}
	b.signIn("ispravna-lozinka")
	b.do("GET", "/admin/rijeci", nil, nil) // for the token
	today := a.todayDate().Format("2006-01-02")
	post := func(path string, f url.Values) *http.Response {
		t.Helper()
		f.Set("csrf", b.csrf)
		return b.do("POST", path, f, nil).Result()
	}

	// the word: one a day
	word := url.Values{"day": {today}, "word": {"Antologija"}, "link": {"https://www.enciklopedija.hr/"},
		"description": {"*(grč. ἀνϑολογία)*, zbirka <b>književnih</b> djela."}}
	if r := post("/admin/rijeci", word); r.StatusCode != http.StatusSeeOther {
		t.Fatalf("new word: %d", r.StatusCode)
	}
	word.Set("word", "Druga")
	w := b.do("POST", "/admin/rijeci", word, nil)
	if !strings.Contains(w.Body.String(), "Taj dan već ima riječ") {
		t.Errorf("a second word for the day was not refused")
	}
	if r := post("/admin/rijeci", url.Values{"day": {"nije-datum"}, "word": {"X"}}); r.StatusCode != http.StatusOK {
		t.Errorf("a bad day: %d, want the form back", r.StatusCode)
	}

	// the album
	if r := post("/admin/albumi", url.Values{"day": {today}, "artist": {"King Crimson"}, "title": {"In the Wake of Poseidon"},
		"year": {"1970"}, "minutes": {"41"}, "tracks": {"8"}, "genre": {"Prog-Rock"}}); r.StatusCode != http.StatusSeeOther {
		t.Fatalf("new album: %d", r.StatusCode)
	}
	if w := b.do("POST", "/admin/albumi", url.Values{"csrf": {b.csrf}, "day": {today}, "artist": {"A"}, "title": {"B"}, "year": {"1492"}}, nil); !strings.Contains(w.Body.String(), "Broj od 1900") {
		t.Errorf("a year out of range was not refused")
	}

	// the article: made, written block by block, set for today
	r := post("/admin/clanci", url.Values{"title": {"Svečano otvorenje"}})
	loc := r.Header.Get("Location")
	if r.StatusCode != http.StatusSeeOther || !strings.HasPrefix(loc, "/admin/clanci/") {
		t.Fatalf("new article: %d to %q", r.StatusCode, loc)
	}
	blocks := func(texts ...string) url.Values {
		f := url.Values{"title": {"Svečano otvorenje"}, "description": {"Uvod"}, "author": {"The Boss"}}
		for i := 0; i < len(texts); i += 2 {
			f.Add("block_type", texts[i])
			f.Add("block_text", texts[i+1])
			f.Add("block_caption", "")
			f.Add("block_picture", "")
		}
		return f
	}
	f := blocks("p", "Prvi **odlomak** s [poveznicom](https://example.com/a?b=1&c=2).")
	f.Set("op", "add:quote")
	if r := post(loc, f); r.StatusCode != http.StatusSeeOther || r.Header.Get("Location") != loc+"#b1" {
		t.Fatalf("add a block: %d to %q", r.StatusCode, r.Header.Get("Location"))
	}
	f = blocks("p", "Prvi **odlomak** s [poveznicom](https://example.com/a?b=1&c=2).", "quote", "Citat [zlo](javascript:alert(1))", "h", "Podnaslov")
	f.Set("op", "up:2") // the subheading goes above the quote
	post(loc, f)
	var id int64
	for _, x := range must(a.Store.Articles(ctx)) {
		id = x.ID
		got := []string{}
		for _, bl := range x.Blocks {
			got = append(got, bl.Type)
		}
		if strings.Join(got, " ") != "p h quote" {
			t.Errorf("blocks after moving: %v", got)
		}
	}
	if r := post(loc+"/dan", url.Values{"day": {today}}); r.StatusCode != http.StatusSeeOther {
		t.Fatalf("set the day: %d", r.StatusCode)
	}

	// weather and NASA come from their fetchers; here they are saved directly
	day := a.todayDate()
	hi, lo := 15, 7
	if err := a.Store.SaveWeather(ctx, day, store.Forecast{City: "Zagreb", Link: "https://meteo.hr/", Max: &hi, Min: &lo,
		Parts: []store.ForecastPart{{Label: "Ujutro", Hour: 8, Temp: 8, Icon: "clear-day", Sky: "Sunčano"}}}); err != nil {
		t.Fatal(err)
	}
	if err := a.Store.SaveAPOD(ctx, store.APOD{Day: day.AddDate(0, 0, -1), Title: "M42", Link: "https://science.nasa.gov/", MediaType: "image"}); err != nil {
		t.Fatal(err)
	}

	snap, err := a.Press.Pick(ctx, day.Add(12*60*60*1e9))
	if err != nil {
		t.Fatal(err)
	}
	page, err := a.Press.Render("edition", snap)
	if err != nil {
		t.Fatal(err)
	}
	html := string(page)
	for _, want := range []string{
		`<h2 class="article-title">Svečano otvorenje</h2>`,
		`Prvi <strong>odlomak</strong> s <a href="https://example.com/a?b=1&amp;c=2">poveznicom</a>.`,
		`<h3>Podnaslov</h3>`,
		`Citat [zlo](javascript:alert(1))`, // not a link: only http and https are
		`<em>(grč. ἀνϑολογία)</em>, zbirka &lt;b&gt;književnih&lt;/b&gt; djela.`,
		`class="wx wx--clear-day"`, `.wx--clear-day{--wx:url(data:image/svg+xml;base64,`,
		`In the Wake of Poseidon`, `King Crimson · 1970`, `>M42</a>`,
	} {
		if !strings.Contains(html, want) {
			t.Errorf("the edition lacks %s", want)
		}
	}
	if strings.Contains(html, "wx--rain{") {
		t.Error("the edition carries an icon it does not show")
	}

	// off the front page, the pinned article stands in
	if r := post(loc+"/dan", url.Values{"makni": {"1"}}); r.StatusCode != http.StatusSeeOther {
		t.Fatalf("take off: %d", r.StatusCode)
	}
	if _, err := a.Store.MainArticle(ctx, day); err != store.ErrNotFound {
		t.Errorf("a day without a main article: %v", err)
	}
	post(loc+"/prikvaci", url.Values{"pin": {"1"}})
	if m, err := a.Store.MainArticle(ctx, day.AddDate(0, 0, 5)); err != nil || m.ID != id {
		t.Errorf("pinned article: %v %v", m.ID, err)
	}
}

func must[T any](v T, err error) T {
	if err != nil {
		panic(err)
	}
	return v
}
