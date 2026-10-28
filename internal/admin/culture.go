package admin

import (
	"fmt"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"

	"tisak/internal/fetch"
	"tisak/internal/press"
	"tisak/internal/store"
	"tisak/internal/tmdb"
)

// The culture pages: the events on the wall are written here, the films of the week are picked
// from TMDB, and the releases and theatre shows the fetch found can be kept out of the edition.

// ---------- events ----------

// eventPoster is how wide an event's poster is kept: a poster on the wall is narrow, twice that for sharp screens.
const eventPoster = 700

var clockInput = regexp.MustCompile(`^([01]?\d|2[0-3]):[0-5]\d$`)

type eventForm struct {
	store.Event
	Errors map[string]string
}

func (a *Admin) events(w http.ResponseWriter, r *http.Request) {
	a.eventsPage(w, r, eventForm{Event: store.Event{Start: a.todayDate()}, Errors: map[string]string{}})
}

// eventRow is an event as the list shows it.
type eventRow struct {
	store.Event
	When string
	On   bool // on today
}

func (a *Admin) eventsPage(w http.ResponseWriter, r *http.Request, f eventForm) {
	today := a.todayDate()
	coming, err := a.Store.Events(r.Context(), today)
	if err != nil {
		a.fail(w, err)
		return
	}
	past, err := a.Store.PastEvents(r.Context(), today, 12)
	if err != nil {
		a.fail(w, err)
		return
	}
	rows := func(list []store.Event) []eventRow {
		var out []eventRow
		for _, e := range list {
			out = append(out, eventRow{Event: e, When: press.When(e, today), On: !e.Start.After(today) && !e.End.Before(today)})
		}
		return out
	}
	a.render(w, "events", "page", struct {
		view
		Coming, Past []eventRow
		Form         eventForm
	}{a.base(r, w, "Događanja", "dogadanja"), rows(coming), rows(past), f})
}

func (a *Admin) readEvent(r *http.Request, e store.Event) eventForm {
	f := eventForm{Event: e, Errors: map[string]string{}}
	f.Title = strings.TrimSpace(r.PostFormValue("title"))
	f.Venue = strings.TrimSpace(r.PostFormValue("venue"))
	f.Price = strings.TrimSpace(r.PostFormValue("price"))
	f.Link = strings.TrimSpace(r.PostFormValue("link"))
	f.Time = strings.TrimSpace(r.PostFormValue("time"))
	limit := func(field, value string, max int) {
		if len([]rune(value)) > max {
			f.Errors[field] = "Najviše " + strconv.Itoa(max) + " znakova."
		}
	}
	if f.Title == "" {
		f.Errors["title"] = "Upiši naslov."
	}
	limit("title", f.Title, 120)
	limit("venue", f.Venue, 80)
	limit("price", f.Price, 40)
	if msg := checkURL(f.Link, false); msg != "" {
		f.Errors["link"] = msg
	}
	if f.Time != "" && !clockInput.MatchString(f.Time) {
		f.Errors["time"] = "Vrijeme kao 20:00."
	}
	start, ok := parseDay(r.PostFormValue("start"))
	if ok {
		f.Start = start
	} else {
		f.Errors["start"] = "Odaberi prvi dan."
	}
	if end, ok := parseDay(r.PostFormValue("end")); ok {
		f.End = end
	} else {
		f.End = f.Start // one day
	}
	if f.End.Before(f.Start) {
		f.Errors["end"] = "Zadnji dan ne može biti prije prvog."
	}
	if r.PostFormValue("remove_poster") != "" {
		f.Poster = ""
	}
	if hash, msg := a.uploadUpTo(r, "poster", eventPoster, maxPhoto); msg != "" {
		f.Errors["poster"] = msg
	} else if hash != "" {
		f.Poster = hash
	}
	return f
}

func (a *Admin) newEvent(w http.ResponseWriter, r *http.Request) {
	f := a.readEvent(r, store.Event{})
	if len(f.Errors) == 0 {
		if _, err := a.Store.SaveEvent(r.Context(), f.Event); err != nil {
			a.fail(w, err)
			return
		}
	}
	if len(f.Errors) > 0 {
		a.eventsPage(w, r, f)
		return
	}
	back(w, r, "/admin/dogadanja", "ok", "„"+f.Title+"” je na zidu od "+shortDay(f.Start))
}

func (a *Admin) event(w http.ResponseWriter, r *http.Request) {
	n, ok := id(r)
	if !ok {
		http.NotFound(w, r)
		return
	}
	x, err := a.Store.Event(r.Context(), n)
	if a.notFound(w, r, err) {
		return
	}
	a.eventPage(w, r, eventForm{Event: x, Errors: map[string]string{}})
}

func (a *Admin) eventPage(w http.ResponseWriter, r *http.Request, f eventForm) {
	a.render(w, "events", "edit", struct {
		view
		Form eventForm
	}{a.base(r, w, f.Title, "dogadanja"), f})
}

func (a *Admin) saveEvent(w http.ResponseWriter, r *http.Request) {
	n, ok := id(r)
	if !ok {
		http.NotFound(w, r)
		return
	}
	x, err := a.Store.Event(r.Context(), n)
	if a.notFound(w, r, err) {
		return
	}
	f := a.readEvent(r, x)
	if len(f.Errors) > 0 {
		a.eventPage(w, r, f)
		return
	}
	if _, err := a.Store.SaveEvent(r.Context(), f.Event); a.notFound(w, r, err) {
		return
	}
	back(w, r, "/admin/dogadanja", "ok", "Spremljeno.")
}

func (a *Admin) deleteEvent(w http.ResponseWriter, r *http.Request) {
	n, ok := id(r)
	if !ok {
		http.NotFound(w, r)
		return
	}
	if err := a.Store.DeleteEvent(r.Context(), n); err != nil {
		a.fail(w, err)
		return
	}
	back(w, r, "/admin/dogadanja", "ok", "Događanje je obrisano. Izdanja u kojima je izašlo ostaju kakva jesu.")
}

// ---------- films ----------

// cinemaWeek is the Thursday that starts the cinema week holding t.
func cinemaWeek(t time.Time) time.Time {
	return t.AddDate(0, 0, -((int(t.Weekday()) - int(time.Thursday) + 7) % 7))
}

func weekRange(week time.Time) string {
	end := week.AddDate(0, 0, 6)
	return fmt.Sprintf("%d. %d. – %d. %d.", week.Day(), int(week.Month()), end.Day(), int(end.Month()))
}

// filmWeek is the week the page works on: ?tjedan=, else the one running today.
func (a *Admin) filmWeek(r *http.Request) time.Time {
	if d, ok := parseDay(r.FormValue("tjedan")); ok {
		return cinemaWeek(d)
	}
	return cinemaWeek(a.todayDate())
}

func (a *Admin) films(w http.ResponseWriter, r *http.Request) {
	today := a.todayDate()
	week := a.filmWeek(r)
	picks, err := a.Store.FilmPicks(r.Context(), week)
	if err != nil {
		a.fail(w, err)
		return
	}
	releases, err := a.Store.Releases(r.Context(), today, today.AddDate(0, 0, 13))
	if err != nil {
		a.fail(w, err)
		return
	}
	q := strings.TrimSpace(r.URL.Query().Get("q"))
	var found []tmdb.Result
	var searchErr string
	switch {
	case !a.hasTMDB():
	case q != "":
		if found, err = a.TMDB.Search(r.Context(), q); err != nil {
			a.Log.Printf("tmdb search: %v", err) // the client keeps the key out of its errors
			searchErr = "TMDB se nije javio. Pokušaj ponovo za koju minutu."
		}
		if len(found) > 12 {
			found = found[:12]
		}
	}
	this := cinemaWeek(today)
	a.render(w, "films", "page", struct {
		view
		Week, This, Prev, Next time.Time
		Range                  string
		Picks                  []store.Film
		Releases               []store.Release
		Query, SearchErr       string
		Found                  []tmdb.Result
		HasKey                 bool
	}{a.base(r, w, "Filmovi", "filmovi"), week, this, week.AddDate(0, 0, -7), week.AddDate(0, 0, 7), weekRange(week),
		picks, releases, q, searchErr, found, a.hasTMDB()})
}

func (a *Admin) hasTMDB() bool { return a.TMDB != nil && a.TMDB.Key != "" }

func (a *Admin) filmsURL(week time.Time) string {
	return "/admin/filmovi?tjedan=" + week.Format("2006-01-02")
}

// addPick looks the film up on TMDB, keeps it with its poster, and puts it among the week's picks.
func (a *Admin) addPick(w http.ResponseWriter, r *http.Request) {
	week := a.filmWeek(r)
	to := a.filmsURL(week)
	n, err := strconv.Atoi(r.PostFormValue("film"))
	if err != nil || n <= 0 {
		back(w, r, to, "bad", "Odaberi film iz tražilice.")
		return
	}
	if !a.hasTMDB() {
		back(w, r, to, "bad", "TMDB ključ nije postavljen, pa ne mogu dohvatiti film.")
		return
	}
	m, err := a.TMDB.Movie(r.Context(), n)
	if err != nil {
		a.Log.Printf("tmdb movie %d: %v", n, err)
		back(w, r, to, "bad", "TMDB se nije javio. Pokušaj ponovo za koju minutu.")
		return
	}
	var pics fetch.PictureSaver
	if a.Media != nil {
		pics = a.Media
	}
	film, err := fetch.SaveMovie(r.Context(), a.Store, pics, m)
	if err != nil {
		a.fail(w, err)
		return
	}
	if err := a.Store.AddFilmPick(r.Context(), week, film.TMDBID); err != nil {
		a.fail(w, err)
		return
	}
	back(w, r, to, "ok", "„"+film.Title+"” je među preporukama za "+weekRange(week))
}

func (a *Admin) removePick(w http.ResponseWriter, r *http.Request) {
	week := a.filmWeek(r)
	n, _ := strconv.Atoi(r.PostFormValue("film"))
	if err := a.Store.RemoveFilmPick(r.Context(), week, n); err != nil {
		a.fail(w, err)
		return
	}
	back(w, r, a.filmsURL(week), "ok", "Film je maknut s preporuka.")
}

// hideRelease keeps a film the fetch found out of Uskoro u kinima, or lets it back.
func (a *Admin) hideRelease(w http.ResponseWriter, r *http.Request) {
	n, err := strconv.Atoi(r.PathValue("id"))
	if err != nil {
		http.NotFound(w, r)
		return
	}
	hide := r.PostFormValue("hide") != ""
	if err := a.Store.SetReleaseHidden(r.Context(), n, hide); a.notFound(w, r, err) {
		return
	}
	msg := "Film se opet prikazuje u najavama."
	if hide {
		msg = "Film je skriven iz najava."
	}
	back(w, r, a.filmsURL(a.filmWeek(r)), "ok", msg)
}

// ---------- theatres ----------

type showDay struct {
	Day   time.Time
	Shows []store.Show
}

func (a *Admin) theatres(w http.ResponseWriter, r *http.Request) {
	today := a.todayDate()
	shows, err := a.Store.Shows(r.Context(), today, today.AddDate(0, 0, 13))
	if err != nil {
		a.fail(w, err)
		return
	}
	var days []showDay
	for _, s := range shows {
		if len(days) == 0 || !days[len(days)-1].Day.Equal(s.Day) {
			days = append(days, showDay{Day: s.Day})
		}
		days[len(days)-1].Shows = append(days[len(days)-1].Shows, s)
	}
	a.render(w, "theatres", "page", struct {
		view
		Days  []showDay
		Count int
	}{a.base(r, w, "Kazališta", "kazalista"), days, len(shows)})
}

func (a *Admin) hideShow(w http.ResponseWriter, r *http.Request) {
	n, ok := id(r)
	if !ok {
		http.NotFound(w, r)
		return
	}
	hide := r.PostFormValue("hide") != ""
	if err := a.Store.SetShowHidden(r.Context(), n, hide); a.notFound(w, r, err) {
		return
	}
	msg := "Predstava se opet prikazuje."
	if hide {
		msg = "Predstava je skrivena. Ostaje skrivena i kad je sljedeći dohvat opet nađe."
	}
	back(w, r, "/admin/kazalista#s"+strconv.FormatInt(n, 10), "ok", msg)
}
