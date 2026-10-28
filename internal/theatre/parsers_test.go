package theatre

import (
	"net/url"
	"slices"
	"testing"
	"time"

	"tisak/internal/store"
)

// The pages below are made up: small pages shaped like each theatre's schedule, with invented
// shows, so the parsers are tested without copies of anyone's site.

var today = time.Date(2026, 10, 25, 0, 0, 0, 0, time.UTC)

// fixtures are each site's first page, and the pages More gives after it, by URL (a POST's
// URL is followed by "?" and its form).
var fixtures = map[string]struct {
	page string
	more map[string]string
}{
	"komedija": {page: `<div class="em-events-list"><table>
<tr><th>Datum</th><th>Predstava</th></tr>
<tr><td>26.10. , Pon</td><td><a href="/www/events/prva/">Prva izmišljena</a><span class="adresa">Velika scena 19:30 -22:00</span><span class="otkazano"> </span></td></tr>
<tr><td>27.10. , Uto</td><td><a href="/www/events/druga/">Druga izmišljena</a><span class="adresa">Velika scena 20:00 -22:00</span><span class="otkazano">OTKAZANO</span></td></tr>
</table><a class="next page-numbers" href="?pno=2">›</a></div>`,
		more: map[string]string{"https://www.komedija.hr/www/raspored-predstava/?pno=2": `<table class="em-events-list">
<tr><td>3.11. , Uto</td><td><a href="/www/events/treca/">Treća izmišljena</a><span class="adresa">Mala scena 18:00</span></td></tr>
</table>`}},
	"kerempuh": {page: `<section class="month-section" data-month="10">
<div class="event-item"><span class="day-number">29.</span><div class="event-title"><a href="/predstava/smijeh/">Smijeh u podrumu</a></div><span class="time">20:00 H</span></div>
<div class="event-item kerempuh-u-gostima-bg"><span class="day-number">30.</span><div class="event-title"><a href="/predstava/put/">Na putu</a></div><span class="time">19:00 H</span></div>
</section><section class="month-section" data-month="11">
<div class="event-item"><span class="day-number">2.</span><div class="event-title"><a href="/predstava/noc/">Noćna scena</a></div><span class="time">22:00 H</span></div>
</section>`},
	"hnk": {page: `<div class="item keep-img-ratio"><div class="title"><a href="/hr/opera/predstave/opera1/#26.10.19.30"><h3>Izmišljena opera</h3><h4>Skladatelj</h4></a></div><div class="tickets">19:30 Glavna pozornica HNK</div></div>
<div class="item keep-img-ratio"><div class="title"><a href="/hr/balet/predstave/balet1/#27.10.20.00"><h3>Balet na putu</h3></a></div><div class="tickets">20:00 Gostovanje, Ljubljana</div></div>`},
	"scenoteka": {page: `<div class="timetable-row"><div class="col-20"><b>26.10.</b> 20:00</div><h3><a href="/predstave/mama/">MAMA DOLAZI</a></h3></div>`,
		more: map[string]string{
			"https://scenoteka.hr/wp-admin/admin-ajax.php?action=load_more_timetable&page=2": `<div class="timetable-row"><div class="col-20"><b>13.11.</b> 19:30</div><h3><a href="/predstave/tata/">TATA ODLAZI</a></h3></div>`,
			"https://scenoteka.hr/wp-admin/admin-ajax.php?action=load_more_timetable&page=3": `<div class="timetable-row"><div class="col-20"><b>30.11.</b> 19:30</div><h3><a href="/predstave/dijete/">DIJETE OSTAJE</a></h3></div>`,
		}},
	"ludakuca": {page: `<div class="kd-photobox"><div class="photobox-img"><a href="https://ulaznice.example/45">x</a></div><div class="phb-content"><h5>7.11. Subota 19:30</h5><p>Špiljski čovjek</p></div></div>
<div class="kd-photobox"><div class="photobox-img"><a href="https://ulaznice.example/46">x</a></div><div class="phb-content"><h5>8.11. Nedjelja 19:30</h5><p>ZAKUPLJENA PREDSTAVA</p></div></div>`},
	"aplauz": {page: `const STATIC_SCHEDULE = [
{"date":"2026-11-12","time":"20:00","title":"Drugi korak","venue":"Kazalište na Peščenici, Zagreb"},
{"date":"2026-11-13","time":"20:00","title":"Drugi korak","venue":"Kazalište na Peščenici, Zagreb"},
{"date":"2026-11-14","time":"20:00","title":"Drugi korak","venue":"Kino Velika Gorica"}
];
const LINKS = { "Drugi korak": { href: "/predstave/drugi-korak.html" } };`},
	"exit": {page: `<article><h3>27. 10. 2026.</h3><span>20:00h</span><p><a href="https://maps.example">Ilica 208, Zagreb</a></p><a href="/predstave/rat"><h2>IZMIŠLJENI RAT</h2></a></article>
<article><h3>28. 10. 2026.</h3><span>19:00h</span><p>Čazma</p><a href="/predstave/blago"><h2>Malo zlato</h2></a></article>`},
	"gavella": {page: `<div class="frontpage-theater-play-events"><table>
<tr><td class="hidden-md-up"><span class="date">Utorak, 27.10.2026.</span><span class="time">19:30</span></td><td><a href="/predstave/vrt">Vrt</a></td><td class="last"><a href="https://karte.example/1">Ulaznice</a></td></tr>
<tr><td class="hidden-md-up"><span class="date">Petak, 20.11.2026.</span><span class="time">19:30</span></td><td><b>39. Izmišljene večeri</b><div class="playcomment">Gostujuće kazalište<br>Netko Netković: ZA CIJELI ŽIVOT<br>- Prodaja ulaznica na blagajni</div></td><td class="last"><a href="https://karte.example/2">Ulaznice</a></td></tr>
</table></div>`},
	"knap": {page: `<div class="timetable-rows"><div class="event-date"><b>28.10.</b></div><div class="event-title"><a href="/kazaliste/predstave/paravan/">IZA ZAVJESE</a></div><div class="event-time">20:00</div></div>`},
}

// page is a site's made-up page; More answers from its fixtures, or with nothing.
func page(t *testing.T, s Site) *Page {
	t.Helper()
	f, ok := fixtures[s.Source]
	if !ok {
		t.Fatalf("no fixture for %s", s.Source)
	}
	u, _ := url.Parse(s.URL)
	return &Page{Body: []byte(f.page), URL: u, Today: today, More: func(raw string, form url.Values) ([]byte, error) {
		if form != nil {
			raw += "?" + form.Encode()
		}
		return []byte(f.more[raw]), nil
	}}
}

// has finds the show on day at time named title.
func has(shows []store.Show, day, at, title string) *store.Show {
	for i, x := range shows {
		if x.Day.Format(time.DateOnly) == day && x.Time == at && x.Title == title {
			return &shows[i]
		}
	}
	return nil
}

func TestParsers(t *testing.T) {
	for _, c := range []struct {
		source      string
		n           int // shows in the window
		day, at     string
		title, link string   // one of them
		not         []string // titles that must not be there
	}{
		{"komedija", 2, "2026-11-03", "18:00", "Treća izmišljena", "https://www.komedija.hr/www/events/treca/",
			[]string{"Druga izmišljena"}}, // cancelled
		{"kerempuh", 2, "2026-11-02", "22:00", "Noćna scena", "https://kazalistekerempuh.hr/predstava/noc/",
			[]string{"Na putu"}}, // played elsewhere
		{"hnk", 1, "2026-10-26", "19:30", "Izmišljena opera", "https://www.hnk.hr/hr/opera/predstave/opera1/#26.10.19.30",
			[]string{"Balet na putu"}}, // abroad
		{"scenoteka", 2, "2026-11-13", "19:30", "TATA ODLAZI", "https://scenoteka.hr/predstave/tata/", nil},
		{"ludakuca", 1, "2026-11-07", "19:30", "Špiljski čovjek", "https://ulaznice.example/45", []string{"ZAKUPLJENA PREDSTAVA"}},
		{"aplauz", 2, "2026-11-12", "20:00", "Drugi korak", "https://aplauz-teatar.hr/predstave/drugi-korak.html", nil},
		{"exit", 1, "2026-10-27", "20:00", "IZMIŠLJENI RAT", "https://teatarexit.hr/predstave/rat", []string{"Malo zlato"}},
		{"gavella", 2, "2026-11-20", "19:30", "39. Izmišljene večeri — Netko Netković: ZA CIJELI ŽIVOT", "https://karte.example/2", nil},
		{"knap", 1, "2026-10-28", "20:00", "IZA ZAVJESE", "https://www.knap.hr/kazaliste/predstave/paravan/", nil},
	} {
		t.Run(c.source, func(t *testing.T) {
			s, _ := Lookup(c.source)
			found, err := s.Parse(page(t, s))
			if err != nil {
				t.Fatal(err)
			}
			shows := Window(found, s.Theatre, today)
			if len(shows) != c.n {
				t.Errorf("%d shows in the window, want %d: %+v", len(shows), c.n, shows)
			}
			x := has(shows, c.day, c.at, c.title)
			if x == nil {
				t.Fatalf("no %s %s %q in %+v", c.day, c.at, c.title, shows)
			}
			if x.Link != c.link || x.Theatre != s.Theatre {
				t.Errorf("%q: link %q theatre %q, want %q %q", c.title, x.Link, x.Theatre, c.link, s.Theatre)
			}
			for _, bad := range c.not {
				if slices.ContainsFunc(shows, func(x store.Show) bool { return x.Title == bad }) {
					t.Errorf("%q should be left out", bad)
				}
			}
		})
	}
	for _, s := range Sites {
		if _, ok := fixtures[s.Source]; !ok {
			t.Errorf("%s has no made-up page", s.Source)
		}
	}
}

func TestScenotekaStopsPastWindow(t *testing.T) {
	s, _ := Lookup("scenoteka")
	p := page(t, s)
	asked := 0
	more := p.More
	p.More = func(raw string, form url.Values) ([]byte, error) { asked++; return more(raw, form) }
	if _, err := s.Parse(p); err != nil {
		t.Fatal(err)
	}
	if asked != 2 { // page 3 ends past the window, so page 4 is not asked for
		t.Errorf("asked for %d more pages, want 2", asked)
	}
}

func TestAplauzLiveWins(t *testing.T) {
	s, _ := Lookup("aplauz")
	p := page(t, s)
	p.More = func(raw string, _ url.Values) ([]byte, error) {
		if raw != aplauzLive {
			t.Errorf("asked for %s", raw)
		}
		return []byte(`[{"date":"2026-11-12","time":"19:30","title":"Drugi korak","venue":"Kazalište na Peščenici, Zagreb"},
			{"date":"2026-11-14","time":"20:00","title":"Umjetnost","venue":"Mala dvorana, Zagreb"},
			{"date":"2026-11-15","time":"20:00","title":"Umjetnost","venue":"HNK Varaždin"}]`), nil
	}
	found, err := s.Parse(p)
	if err != nil {
		t.Fatal(err)
	}
	shows := Window(found, s.Theatre, today)
	if len(shows) != 3 || has(shows, "2026-11-12", "19:30", "Drugi korak") == nil || has(shows, "2026-11-12", "20:00", "Drugi korak") != nil ||
		has(shows, "2026-11-14", "20:00", "Umjetnost") == nil || has(shows, "2026-11-13", "20:00", "Drugi korak") == nil {
		t.Errorf("shows = %+v", shows)
	}
}

func TestChangedPage(t *testing.T) {
	for _, s := range Sites {
		if _, err := s.Parse(&Page{Body: []byte("<html><body><p>Nova stranica</p></body></html>"), Today: today,
			More: func(string, url.Values) ([]byte, error) { return nil, nil }}); err == nil {
			t.Errorf("%s: no error for a page without a schedule", s.Source)
		}
	}
}
