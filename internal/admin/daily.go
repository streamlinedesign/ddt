package admin

import (
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"tisak/internal/store"
)

// The word and the album of the day are written ahead, one for each date. A day without one
// prints without that panel.

// today is the paper's date, as the tables store it.
func (a *Admin) todayDate() time.Time {
	now := time.Now().In(a.Loc)
	return time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)
}

// parseDay reads a date input: 2026-10-28.
func parseDay(s string) (time.Time, bool) {
	t, err := time.Parse("2006-01-02", strings.TrimSpace(s))
	return t, err == nil && t.Year() >= 2000 && t.Year() < 2100
}

// gaps lists the days of the next two weeks, today first, that have nothing yet.
func gaps(from time.Time, have map[string]bool) []time.Time {
	var out []time.Time
	for i := 0; i < 14; i++ {
		d := from.AddDate(0, 0, i)
		if !have[d.Format("2006-01-02")] {
			out = append(out, d)
		}
	}
	return out
}

// firstGap is the day a new entry is offered for: the first free day, or today.
func firstGap(g []time.Time, today time.Time) time.Time {
	if len(g) > 0 {
		return g[0]
	}
	return today
}

// ---------- the word of the day ----------

type wordForm struct {
	store.Word
	Errors map[string]string
}

func (a *Admin) words(w http.ResponseWriter, r *http.Request) {
	a.wordsPage(w, r, wordForm{Errors: map[string]string{}})
}

func (a *Admin) wordsPage(w http.ResponseWriter, r *http.Request, f wordForm) {
	today := a.todayDate()
	list, err := a.Store.Words(r.Context(), today)
	if err != nil {
		a.fail(w, err)
		return
	}
	have := map[string]bool{}
	for _, x := range list {
		have[x.Day.Format("2006-01-02")] = true
	}
	g := gaps(today, have)
	if f.Day.IsZero() {
		f.Day = firstGap(g, today)
		if d, ok := parseDay(r.URL.Query().Get("dan")); ok {
			f.Day = d
		}
	}
	v := a.base(r, w, "Riječ dana", "rijeci")
	a.render(w, "words", "page", struct {
		view
		Words []store.Word
		Gaps  []time.Time
		Today time.Time
		Form  wordForm
	}{v, list, g, today, f})
}

func (a *Admin) readWord(r *http.Request, w store.Word) wordForm {
	f := wordForm{Word: w, Errors: map[string]string{}}
	f.Word.Word = strings.TrimSpace(r.PostFormValue("word"))
	f.Link = strings.TrimSpace(r.PostFormValue("link"))
	f.Description = strings.TrimSpace(r.PostFormValue("description"))
	day, ok := parseDay(r.PostFormValue("day"))
	if ok {
		f.Day = day
	} else {
		f.Errors["day"] = "Odaberi dan."
	}
	switch {
	case f.Word.Word == "":
		f.Errors["word"] = "Upiši riječ."
	case len([]rune(f.Word.Word)) > 60:
		f.Errors["word"] = "Najviše 60 znakova."
	}
	if msg := checkURL(f.Link, false); msg != "" {
		f.Errors["link"] = msg
	}
	if len([]rune(f.Description)) > 800 {
		f.Errors["description"] = "Najviše 800 znakova."
	}
	return f
}

func (a *Admin) newWord(w http.ResponseWriter, r *http.Request) {
	f := a.readWord(r, store.Word{})
	if len(f.Errors) == 0 {
		_, err := a.Store.SaveWord(r.Context(), f.Word)
		if errors.Is(err, store.ErrTaken) {
			f.Errors["day"] = "Taj dan već ima riječ. Uredi nju."
		} else if err != nil {
			a.fail(w, err)
			return
		}
	}
	if len(f.Errors) > 0 {
		a.wordsPage(w, r, f)
		return
	}
	back(w, r, "/admin/rijeci", "ok", "Riječ „"+f.Word.Word+"” stoji za "+shortDay(f.Day))
}

func (a *Admin) word(w http.ResponseWriter, r *http.Request) {
	n, ok := id(r)
	if !ok {
		http.NotFound(w, r)
		return
	}
	x, err := a.Store.Word(r.Context(), n)
	if a.notFound(w, r, err) {
		return
	}
	a.wordPage(w, r, wordForm{Word: x, Errors: map[string]string{}})
}

func (a *Admin) wordPage(w http.ResponseWriter, r *http.Request, f wordForm) {
	v := a.base(r, w, f.Word.Word, "rijeci")
	a.render(w, "words", "edit", struct {
		view
		Form wordForm
	}{v, f})
}

func (a *Admin) saveWord(w http.ResponseWriter, r *http.Request) {
	n, ok := id(r)
	if !ok {
		http.NotFound(w, r)
		return
	}
	x, err := a.Store.Word(r.Context(), n)
	if a.notFound(w, r, err) {
		return
	}
	f := a.readWord(r, x)
	if len(f.Errors) == 0 {
		_, err := a.Store.SaveWord(r.Context(), f.Word)
		if errors.Is(err, store.ErrTaken) {
			f.Errors["day"] = "Taj dan već ima drugu riječ."
		} else if a.notFound(w, r, err) {
			return
		}
	}
	if len(f.Errors) > 0 {
		a.wordPage(w, r, f)
		return
	}
	back(w, r, "/admin/rijeci", "ok", "Spremljeno.")
}

func (a *Admin) deleteWord(w http.ResponseWriter, r *http.Request) {
	n, ok := id(r)
	if !ok {
		http.NotFound(w, r)
		return
	}
	if err := a.Store.DeleteWord(r.Context(), n); err != nil {
		a.fail(w, err)
		return
	}
	back(w, r, "/admin/rijeci", "ok", "Riječ je obrisana.")
}

// ---------- the album of the day ----------

type albumForm struct {
	store.Album
	Errors map[string]string
}

func (a *Admin) albums(w http.ResponseWriter, r *http.Request) {
	a.albumsPage(w, r, albumForm{Errors: map[string]string{}})
}

func (a *Admin) albumsPage(w http.ResponseWriter, r *http.Request, f albumForm) {
	today := a.todayDate()
	list, err := a.Store.Albums(r.Context(), today)
	if err != nil {
		a.fail(w, err)
		return
	}
	have := map[string]bool{}
	for _, x := range list {
		have[x.Day.Format("2006-01-02")] = true
	}
	g := gaps(today, have)
	if f.Day.IsZero() {
		f.Day = firstGap(g, today)
		if d, ok := parseDay(r.URL.Query().Get("dan")); ok {
			f.Day = d
		}
	}
	v := a.base(r, w, "Album dana", "albumi")
	a.render(w, "albums", "page", struct {
		view
		Albums []store.Album
		Gaps   []time.Time
		Today  time.Time
		Form   albumForm
	}{v, list, g, today, f})
}

// albumCover is how wide a cover is kept: the panel is a third of the page at most.
const albumCover = 800

func (a *Admin) readAlbum(r *http.Request, al store.Album) albumForm {
	f := albumForm{Album: al, Errors: map[string]string{}}
	f.Artist = strings.TrimSpace(r.PostFormValue("artist"))
	f.Title = strings.TrimSpace(r.PostFormValue("title"))
	f.Genre = strings.TrimSpace(r.PostFormValue("genre"))
	f.Link = strings.TrimSpace(r.PostFormValue("link"))
	day, ok := parseDay(r.PostFormValue("day"))
	if ok {
		f.Day = day
	} else {
		f.Errors["day"] = "Odaberi dan."
	}
	need := func(field, value, msg string, max int) {
		switch {
		case value == "":
			f.Errors[field] = msg
		case len([]rune(value)) > max:
			f.Errors[field] = "Najviše " + strconv.Itoa(max) + " znakova."
		}
	}
	need("artist", f.Artist, "Upiši izvođača.", 120)
	need("title", f.Title, "Upiši naslov albuma.", 160)
	if len([]rune(f.Genre)) > 60 {
		f.Errors["genre"] = "Najviše 60 znakova."
	}
	num := func(field string, lo, hi int) int {
		s := strings.TrimSpace(r.PostFormValue(field))
		if s == "" {
			return 0
		}
		n, err := strconv.Atoi(s)
		if err != nil || n < lo || n > hi {
			f.Errors[field] = "Broj od " + strconv.Itoa(lo) + " do " + strconv.Itoa(hi) + "."
		}
		return n
	}
	f.Year = num("year", 1900, 2100)
	f.Minutes = num("minutes", 1, 600)
	f.Tracks = num("tracks", 1, 200)
	if msg := checkURL(f.Link, false); msg != "" {
		f.Errors["link"] = msg
	}
	if r.PostFormValue("remove_cover") != "" {
		f.Cover = ""
	}
	if hash, msg := a.upload(r, "cover", albumCover); msg != "" {
		f.Errors["cover"] = msg
	} else if hash != "" {
		f.Cover = hash
	}
	return f
}

func (a *Admin) newAlbum(w http.ResponseWriter, r *http.Request) {
	f := a.readAlbum(r, store.Album{})
	if len(f.Errors) == 0 {
		_, err := a.Store.SaveAlbum(r.Context(), f.Album)
		if errors.Is(err, store.ErrTaken) {
			f.Errors["day"] = "Taj dan već ima album. Uredi njega."
		} else if err != nil {
			a.fail(w, err)
			return
		}
	}
	if len(f.Errors) > 0 {
		a.albumsPage(w, r, f)
		return
	}
	back(w, r, "/admin/albumi", "ok", "Album „"+f.Title+"” stoji za "+shortDay(f.Day))
}

func (a *Admin) album(w http.ResponseWriter, r *http.Request) {
	n, ok := id(r)
	if !ok {
		http.NotFound(w, r)
		return
	}
	x, err := a.Store.Album(r.Context(), n)
	if a.notFound(w, r, err) {
		return
	}
	a.albumPage(w, r, albumForm{Album: x, Errors: map[string]string{}})
}

func (a *Admin) albumPage(w http.ResponseWriter, r *http.Request, f albumForm) {
	v := a.base(r, w, f.Title, "albumi")
	a.render(w, "albums", "edit", struct {
		view
		Form albumForm
	}{v, f})
}

func (a *Admin) saveAlbum(w http.ResponseWriter, r *http.Request) {
	n, ok := id(r)
	if !ok {
		http.NotFound(w, r)
		return
	}
	x, err := a.Store.Album(r.Context(), n)
	if a.notFound(w, r, err) {
		return
	}
	f := a.readAlbum(r, x)
	if len(f.Errors) == 0 {
		_, err := a.Store.SaveAlbum(r.Context(), f.Album)
		if errors.Is(err, store.ErrTaken) {
			f.Errors["day"] = "Taj dan već ima drugi album."
		} else if a.notFound(w, r, err) {
			return
		}
	}
	if len(f.Errors) > 0 {
		a.albumPage(w, r, f)
		return
	}
	back(w, r, "/admin/albumi", "ok", "Spremljeno.")
}

func (a *Admin) deleteAlbum(w http.ResponseWriter, r *http.Request) {
	n, ok := id(r)
	if !ok {
		http.NotFound(w, r)
		return
	}
	if err := a.Store.DeleteAlbum(r.Context(), n); err != nil {
		a.fail(w, err)
		return
	}
	back(w, r, "/admin/albumi", "ok", "Album je obrisan.")
}

var shortDays = []string{"ned", "pon", "uto", "sri", "čet", "pet", "sub"}

// shortDay writes a date briefly: sri 28.10.
func shortDay(t time.Time) string {
	return shortDays[t.Weekday()] + " " + t.Format("2.1.")
}
