package admin

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"

	"tisak/internal/store"
	"tisak/internal/tmdb"
)

// TestCulture puts up an event and picks a film through the dashboard, hides a theatre show and a
// release, and checks what the press prints. TMDB and everything else here is made up.
func TestCulture(t *testing.T) {
	a, h := testAdmin(t)
	ctx := context.Background()
	fake := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/search/movie":
			w.Write([]byte(`{"results":[{"id":41,"title":"Izmišljeni film","original_title":"Made-up Film","release_date":"2026-09-01"}]}`))
		case "/movie/41":
			w.Write([]byte(`{"id":41,"title":"Izmišljeni film","original_title":"Made-up Film","release_date":"2026-09-01","runtime":95,
				"genres":[{"id":18,"name":"Drama"}]}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer fake.Close()
	a.TMDB = &tmdb.Client{Key: "probni-kljuc", Endpoint: fake.URL, HTTP: fake.Client()}

	b := &browserT{t: t, h: h, ip: "10.0.0.6"}
	b.signIn("ispravna-lozinka")
	day := a.todayDate()
	iso := func(n int) string { return day.AddDate(0, 0, n).Format("2006-01-02") }
	post := func(path string, f url.Values) *httptest.ResponseRecorder {
		t.Helper()
		f.Set("csrf", b.csrf)
		return b.do("POST", path, f, nil)
	}

	// events: a bad one comes back with its mistakes, a good one goes on the wall
	b.do("GET", "/admin/dogadanja", nil, nil)
	w := post("/admin/dogadanja", url.Values{"title": {"Izložba"}, "start": {iso(3)}, "end": {iso(1)}, "time": {"25:00"}})
	if body := w.Body.String(); !strings.Contains(body, "Zadnji dan ne može biti prije prvog") || !strings.Contains(body, "Vrijeme kao 20:00") {
		t.Errorf("a bad event was not refused: %d", w.Code)
	}
	if w := post("/admin/dogadanja", url.Values{"title": {"Izložba plakata"}, "venue": {"Galerija"}, "start": {iso(-2)}, "end": {iso(6)},
		"price": {"Besplatno"}}); w.Code != http.StatusSeeOther {
		t.Fatalf("new event: %d %s", w.Code, w.Body)
	}
	if w := post("/admin/dogadanja", url.Values{"title": {"Koncert"}, "start": {iso(1)}, "time": {"20:00"}, "link": {"https://primjer.hr/k"}}); w.Code != http.StatusSeeOther {
		t.Fatalf("new event: %d", w.Code)
	}
	if body := b.do("GET", "/admin/dogadanja", nil, nil).Body.String(); !strings.Contains(body, "u tijeku") || !strings.Contains(body, "Koncert") {
		t.Error("the list lacks the events")
	}

	// films: search TMDB, pick for this week; the key never shows
	body := b.do("GET", "/admin/filmovi?q=film", nil, nil).Body.String()
	if !strings.Contains(body, "Izmišljeni film") || strings.Contains(body, "probni-kljuc") {
		t.Fatalf("search: %s", body)
	}
	if w := post("/admin/filmovi/preporuke", url.Values{"film": {"41"}}); w.Code != http.StatusSeeOther {
		t.Fatalf("pick: %d %s", w.Code, w.Body)
	}
	picks, err := a.Store.FilmPicks(ctx, cinemaWeek(day))
	if err != nil || len(picks) != 1 || picks[0].Genres != "Drama" || picks[0].Runtime != 95 {
		t.Fatalf("picks %+v %v", picks, err)
	}
	if err := a.Store.SaveRelease(ctx, 41, day.AddDate(0, 0, 2)); err != nil {
		t.Fatal(err)
	}

	// theatres: a hidden show stays hidden when the next fetch finds it again
	shows := []store.Show{{Theatre: "Probno kazalište", Title: "Predstava", Day: day, Time: "19:30"},
		{Theatre: "Probno kazalište", Title: "Predstava", Day: day, Time: "21:00"},
		{Theatre: "Probno kazalište", Title: "Skrivena", Day: day.AddDate(0, 0, 1), Time: "20:00"}}
	if err := a.Store.ReplaceShows(ctx, "proba", day, shows); err != nil {
		t.Fatal(err)
	}
	got, _ := a.Store.Shows(ctx, day, day.AddDate(0, 0, 13))
	var hidden int64
	for _, s := range got {
		if s.Title == "Skrivena" {
			hidden = s.ID
		}
	}
	b.do("GET", "/admin/kazalista", nil, nil)
	if w := post("/admin/kazalista/"+itoa(hidden)+"/skrij", url.Values{"hide": {"1"}}); w.Code != http.StatusSeeOther {
		t.Fatalf("hide: %d", w.Code)
	}
	if err := a.Store.ReplaceShows(ctx, "proba", day, shows); err != nil {
		t.Fatal(err)
	}
	if body := b.do("GET", "/admin/kazalista", nil, nil).Body.String(); !strings.Contains(body, "skriveno") {
		t.Error("the show came back unhidden after a fetch")
	}

	// Billboard: no chart yet, then this week's
	if week, top, err := a.Store.LatestChart(ctx, "hot100", day); err != nil || !week.IsZero() || top != nil {
		t.Errorf("no chart: %v %v %v", week, top, err)
	}
	if err := a.Store.SaveChart(ctx, "hot100", day.AddDate(0, 0, 3), []store.ChartEntry{{Rank: 1, Title: "Pjesma", Artist: "Netko", Last: 2}}); err != nil {
		t.Fatal(err)
	}
	if week, top, err := a.Store.LatestChart(ctx, "hot100", day.AddDate(0, 0, 7)); err != nil || !week.Equal(day.AddDate(0, 0, 3)) || len(top) != 1 {
		t.Errorf("chart: %v %v %v", week, top, err)
	}

	snap, err := a.Press.Pick(ctx, day.Add(12*60*60*1e9))
	if err != nil {
		t.Fatal(err)
	}
	if len(snap.Events) != 2 || snap.Events[0].Title != "Izložba plakata" || !strings.HasPrefix(snap.Events[0].When, "do ") {
		t.Errorf("events %+v", snap.Events)
	}
	if snap.Films == nil || len(snap.Films.Picks) != 1 || len(snap.Films.Soon) != 1 || snap.Films.Picks[0].Meta != "Drama · 1 h 35 min · 2026" {
		t.Errorf("films %+v", snap.Films)
	}
	if th := snap.Theatres; th == nil || th.Count != 1 || len(th.ThisWeek)+len(th.NextWeek) != 1 {
		t.Errorf("theatres %+v", th)
	} else if d := append(th.ThisWeek, th.NextWeek...)[0]; len(d.Shows) != 1 || strings.Join(d.Shows[0].Times, " ") != "19:30 21:00" {
		t.Errorf("one show, both times: %+v", d)
	}

	// a hidden release is left out, and with nothing else coming the films still print their picks
	post("/admin/filmovi/41/skrij", url.Values{"hide": {"1"}})
	snap, _ = a.Press.Pick(ctx, day.Add(12*60*60*1e9))
	if snap.Films == nil || len(snap.Films.Soon) != 0 {
		t.Errorf("hidden release printed: %+v", snap.Films)
	}
}

func itoa(n int64) string { return strconv.FormatInt(n, 10) }
