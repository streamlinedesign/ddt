package admin

import (
	"context"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"tisak/internal/game"
	"tisak/internal/press"
	"tisak/internal/site"
	"tisak/internal/store"
)

// TestPlay writes a dinner and Kontakt's addresses through the dashboard, sends messages through
// the site's form, reads them in the inbox, and checks what the press prints. Everything is made up.
func TestPlay(t *testing.T) {
	a, h := testAdmin(t)
	ctx := context.Background()
	b := &browserT{t: t, h: h, ip: "10.0.0.7"}
	b.signIn("ispravna-lozinka")
	day := a.todayDate()
	iso := func(n int) string { return day.AddDate(0, 0, n).Format("2006-01-02") }
	post := func(path string, f url.Values) *httptest.ResponseRecorder {
		t.Helper()
		f.Set("csrf", b.csrf)
		return b.do("POST", path, f, nil)
	}

	// Večera: a course without a title is refused, then a dinner of two courses and a cocktail
	b.do("GET", "/admin/vecera", nil, nil)
	if body := post("/admin/vecera", url.Values{"day": {iso(-1)}, "main_intro": {"Bez naslova"}}).Body.String(); !strings.Contains(body, "Upiši naslov, ili isprazni") {
		t.Error("a course without a title was not refused")
	}
	if w := post("/admin/vecera", url.Values{"day": {iso(-1)},
		"starter_title": {"Juha od bundeve"}, "starter_meta": {"4 osobe"}, "starter_ingredients": {"bundeva\n\n  luk  \nsol"},
		"main_title": {"Rižoto"}, "main_method": {"Prodinstaj.\nZalij."},
		"cocktail_title": {"Negroni"}, "cocktail_ingredients": {"gin\nvermut\nbitter"}}); w.Code != http.StatusSeeOther {
		t.Fatalf("new dinner: %d %s", w.Code, w.Body)
	}
	if body := post("/admin/vecera", url.Values{"day": {iso(-1)}, "dessert_title": {"Kolač"}}).Body.String(); !strings.Contains(body, "Taj dan već ima večeru") {
		t.Error("a second dinner on the same day was not refused")
	}
	if body := b.do("GET", "/admin/vecera", nil, nil).Body.String(); !strings.Contains(body, "Juha od bundeve · Rižoto · Negroni") || !strings.Contains(body, ">danas<") {
		t.Error("the list lacks today's dinner")
	}

	// Kontakt's addresses: a bad one comes back, good ones are kept
	b.do("GET", "/admin/kontakt", nil, nil)
	if body := post("/admin/kontakt", url.Values{"kontakt_email": {"Uredništvo <u@primjer.hr>"}}).Body.String(); !strings.Contains(body, "Upiši samo adresu") {
		t.Error("an address with a name was not refused")
	}
	if w := post("/admin/kontakt", url.Values{"kontakt_email": {"urednistvo@primjer.hr"}, "kontakt_vrijeme": {"Pon – pet, 9 – 17"}}); w.Code != http.StatusSeeOther {
		t.Fatalf("contact settings: %d", w.Code)
	}

	// the site takes messages from the form: robots are thanked and forgotten, a flood is slowed
	s := (&site.Site{Store: a.Store, Press: a.Press, Log: log.New(io.Discard, "", 0), ClientIP: a.Auth.ClientIP}).Handler()
	send := func(ip string, f url.Values) *httptest.ResponseRecorder {
		r := httptest.NewRequest("POST", "/kontakt", strings.NewReader(f.Encode()))
		r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		r.RemoteAddr = ip + ":5555"
		w := httptest.NewRecorder()
		s.ServeHTTP(w, r)
		return w
	}
	good := url.Values{"name": {"Čitateljica"}, "email": {"citateljica@primjer.hr"}, "subject": {"Ispravak"}, "message": {"Prvi redak.\nDrugi <b>redak</b>."}}
	if w := send("192.0.2.1", good); w.Code != http.StatusOK || !strings.Contains(w.Body.String(), "Poruka je stigla") {
		t.Fatalf("message: %d %s", w.Code, w.Body)
	}
	if w := send("192.0.2.2", url.Values{"name": {"Robot"}, "email": {"r@primjer.hr"}, "message": {"kupi"}, "website": {"http://spam"}}); w.Code != http.StatusOK {
		t.Errorf("robot: %d", w.Code)
	}
	if w := send("192.0.2.2", url.Values{"name": {"Netko"}, "email": {"nije adresa"}, "message": {"bok"}}); w.Code != http.StatusBadRequest {
		t.Errorf("bad address: %d", w.Code)
	}
	for i := 0; i < 4; i++ {
		send("192.0.2.3", good)
	}
	if w := send("192.0.2.3", good); w.Code != http.StatusOK {
		t.Errorf("fifth message: %d", w.Code)
	}
	if w := send("192.0.2.3", good); w.Code != http.StatusTooManyRequests {
		t.Errorf("sixth message within the hour: %d", w.Code)
	}

	// the inbox: six messages, none from the robot; opening one marks it read, archiving moves it
	if n, _ := a.Store.UnreadMessages(ctx); n != 6 {
		t.Errorf("unread = %d, want 6", n)
	}
	body := b.do("GET", "/admin/kontakt", nil, nil).Body.String()
	if strings.Contains(body, "Robot") || !strings.Contains(body, `class="nav-count" title="Nepročitane poruke">6<`) {
		t.Error("the inbox is wrong")
	}
	list, _ := a.Store.Messages(ctx, false, 10)
	first := itoa(list[len(list)-1].ID)
	body = b.do("GET", "/admin/kontakt/"+first, nil, nil).Body.String()
	if !strings.Contains(body, "Drugi &lt;b&gt;redak&lt;/b&gt;.") || !strings.Contains(body, "mailto:citateljica@primjer.hr?subject=Re: Ispravak") {
		t.Errorf("message page: %s", body)
	}
	if n, _ := a.Store.UnreadMessages(ctx); n != 5 {
		t.Errorf("unread after opening = %d", n)
	}
	if w := post("/admin/kontakt/"+first+"/arhiva", url.Values{"archive": {"1"}}); w.Code != http.StatusSeeOther {
		t.Fatalf("archive: %d", w.Code)
	}
	if arch, _ := a.Store.Messages(ctx, true, 10); len(arch) != 1 {
		t.Errorf("archive holds %d", len(arch))
	}
	if w := post("/admin/kontakt/"+first+"/obrisi", url.Values{}); w.Code != http.StatusSeeOther {
		t.Fatalf("delete: %d", w.Code)
	}

	// matches: last week's result and next month's match, in Zagreb's dates; Sport prints them with no news at all
	if _, err := a.Store.AddCategory(ctx, store.Category{Slug: "sport", Title: "Sport", Layout: "sport", InMenu: true, Enabled: true}); err != nil {
		t.Fatal(err)
	}
	two, one := 2, 1
	for _, m := range []store.Match{
		{EventID: "1", Source: "sportsdb:1", Team: "Croatia", League: "FIFA World Cup Qualifying UEFA", Home: "Croatia", Away: "Faroe Islands",
			HomeScore: &two, AwayScore: &one, Day: day.AddDate(0, 0, -5), Time: "20:45",
			Goals: []store.Goal{{Minute: 17, Player: "Ivan Perišić", Home: true}, {Minute: 40, Player: "Netko Drugi", Kind: "own"}, {Minute: 70, Player: "Luka Modrić", Home: true, Kind: "penalty"}}},
		{EventID: "2", Source: "sportsdb:1", Team: "Croatia", League: "UEFA Nations League", Home: "Montenegro", Away: "Croatia",
			Day: day.AddDate(0, 0, 20), Time: "18:00", Venue: "Podgorica City Stadium"},
	} {
		if err := a.Store.SaveMatch(ctx, m); err != nil {
			t.Fatal(err)
		}
	}

	snap, err := a.Press.Pick(ctx, day.Add(12*time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if snap.Game == nil || snap.Game.Number != 1 || snap.Game.Scrambled == "" {
		t.Fatalf("game %+v", snap.Game)
	}
	again, _ := a.Press.Pick(ctx, day.Add(13*time.Hour))
	if again.Game == nil || *again.Game != *snap.Game {
		t.Errorf("a reprint drew another word: %+v %+v", snap.Game, again.Game)
	}
	if d := snap.Dinner; d == nil || len(d.Courses) != 2 || d.Cocktail == nil || strings.Join(d.Courses[0].Ingredients, "|") != "bundeva|luk|sol" {
		t.Errorf("dinner %+v", snap.Dinner)
	}
	if c := snap.Contact; c == nil || len(c.Channels) != 2 || c.Channels[0].Link != "mailto:urednistvo@primjer.hr" || c.Action != "/kontakt" {
		t.Errorf("contact %+v", snap.Contact)
	}
	var sport *press.Section
	for i := range snap.Sections {
		if snap.Sections[i].Slug == "sport" {
			sport = &snap.Sections[i]
		}
	}
	if sport == nil || sport.Matches == nil || sport.Matches.Last == nil || sport.Matches.Next == nil {
		t.Fatalf("sport %+v", sport)
	}
	if l := sport.Matches.Last; l.Home != "Hrvatska" || l.Score != "2 – 1" || strings.Join(l.Goals, ", ") != "Perišić 17', Drugi 40' (autogol), Modrić 70' (11 m)" {
		t.Errorf("last %+v", l)
	}
	if n := sport.Matches.Next; n.Away != "Hrvatska" || n.Time != "18:00" || len(n.Form) == 0 {
		t.Errorf("next %+v", n)
	}

	// the word drawn is one of the answers, and the page can unscramble it
	w, err := a.Store.DrawGameWord(ctx, day, game.Pick)
	if err != nil || game.Scramble(w.Word, day.Format("2006-01-02")) != snap.Game.Scrambled {
		t.Errorf("game word %+v %v", w, err)
	}
}
