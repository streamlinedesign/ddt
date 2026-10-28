package admin

import (
	"errors"
	"net/http"
	"net/mail"
	"strconv"
	"strings"
	"time"

	"tisak/internal/press"
	"tisak/internal/store"
)

// The play pages: Večera is written here, a week or so at a time, and Kontakt's messages are read
// here, next to the addresses the edition prints.

// ---------- Večera ----------

var courseKinds = []string{"starter", "main", "dessert", "cocktail"}

// courseForm is one course as the form holds it: the lists one item to a line.
type courseForm struct {
	Kind, Label, Title, Intro, Meta string
	Ingredients, Method             string
}

type dinnerForm struct {
	ID      int64
	Day     time.Time
	Courses []courseForm
	Errors  map[string]string
}

func dinnerFormOf(d store.Dinner) dinnerForm {
	f := dinnerForm{ID: d.ID, Day: d.Day, Errors: map[string]string{}}
	for _, k := range courseKinds {
		c := courseForm{Kind: k, Label: press.CourseLabels[k]}
		for _, x := range d.Courses {
			if x.Kind == k {
				c.Title, c.Intro, c.Meta = x.Title, x.Intro, x.Meta
				c.Ingredients, c.Method = strings.Join(x.Ingredients, "\n"), strings.Join(x.Method, "\n")
			}
		}
		f.Courses = append(f.Courses, c)
	}
	return f
}

// lines are a textarea's lines, trimmed, the empty ones left out.
func lines(s string) []string {
	var out []string
	for _, l := range strings.Split(s, "\n") {
		if l = strings.TrimSpace(l); l != "" {
			out = append(out, l)
		}
	}
	return out
}

func (a *Admin) readDinner(r *http.Request, id int64) (dinnerForm, store.Dinner) {
	f := dinnerForm{ID: id, Errors: map[string]string{}}
	d := store.Dinner{ID: id}
	if day, ok := parseDay(r.PostFormValue("day")); ok {
		f.Day, d.Day = day, day
	} else {
		f.Errors["day"] = "Odaberi dan."
	}
	for _, k := range courseKinds {
		v := func(name string) string { return strings.TrimSpace(r.PostFormValue(k + "_" + name)) }
		c := courseForm{Kind: k, Label: press.CourseLabels[k], Title: v("title"), Intro: v("intro"), Meta: v("meta"),
			Ingredients: v("ingredients"), Method: v("method")}
		f.Courses = append(f.Courses, c)
		switch {
		case c.Title == "" && (c.Intro != "" || c.Ingredients != "" || c.Method != ""):
			f.Errors[k] = "Upiši naslov, ili isprazni cijeli recept."
		case len([]rune(c.Title)) > 100:
			f.Errors[k] = "Naslov ima najviše 100 znakova."
		case len([]rune(c.Intro)) > 600:
			f.Errors[k] = "Uvod ima najviše 600 znakova."
		case len([]rune(c.Meta)) > 60:
			f.Errors[k] = "Podatak uz naslov ima najviše 60 znakova."
		case len([]rune(c.Ingredients))+len([]rune(c.Method)) > 4000:
			f.Errors[k] = "Recept je predug."
		}
		if c.Title != "" {
			d.Courses = append(d.Courses, store.Course{Kind: k, Title: c.Title, Intro: c.Intro, Meta: c.Meta,
				Ingredients: lines(c.Ingredients), Method: lines(c.Method)})
		}
	}
	if len(d.Courses) == 0 && len(f.Errors) == 0 {
		f.Errors["starter"] = "Upiši barem jedno jelo."
	}
	return f, d
}

// dinnerRow is a dinner as the list shows it.
type dinnerRow struct {
	store.Dinner
	Titles string
	Now    bool // the one today's edition prints
}

func (a *Admin) dinners(w http.ResponseWriter, r *http.Request) {
	f := dinnerFormOf(store.Dinner{})
	f.Day = a.todayDate()
	if d, ok := parseDay(r.URL.Query().Get("dan")); ok {
		f.Day = d
	}
	a.dinnersPage(w, r, f)
}

func (a *Admin) dinnersPage(w http.ResponseWriter, r *http.Request, f dinnerForm) {
	today := a.todayDate()
	list, err := a.Store.Dinners(r.Context(), today)
	if err != nil {
		a.fail(w, err)
		return
	}
	current, err := a.Store.DinnerOn(r.Context(), today)
	if err != nil && !errors.Is(err, store.ErrNotFound) {
		a.fail(w, err)
		return
	}
	var coming, past []dinnerRow
	for _, d := range list {
		var titles []string
		for _, c := range d.Courses {
			titles = append(titles, c.Title)
		}
		row := dinnerRow{Dinner: d, Titles: strings.Join(titles, " · "), Now: d.ID == current.ID}
		if d.Day.Before(today) && !row.Now {
			past = append(past, row)
		} else {
			coming = append(coming, row)
		}
	}
	a.render(w, "dinners", "page", struct {
		view
		Coming, Past []dinnerRow
		Form         dinnerForm
	}{a.base(r, w, "Večera", "vecera"), coming, past, f})
}

func (a *Admin) newDinner(w http.ResponseWriter, r *http.Request) {
	f, d := a.readDinner(r, 0)
	if len(f.Errors) == 0 {
		_, err := a.Store.SaveDinner(r.Context(), d)
		if errors.Is(err, store.ErrTaken) {
			f.Errors["day"] = "Taj dan već ima večeru. Uredi nju."
		} else if err != nil {
			a.fail(w, err)
			return
		}
	}
	if len(f.Errors) > 0 {
		a.dinnersPage(w, r, f)
		return
	}
	back(w, r, "/admin/vecera", "ok", "Večera izlazi od "+shortDay(d.Day)+" do sljedeće, najviše tjedan dana.")
}

func (a *Admin) dinner(w http.ResponseWriter, r *http.Request) {
	n, ok := id(r)
	if !ok {
		http.NotFound(w, r)
		return
	}
	d, err := a.Store.Dinner(r.Context(), n)
	if a.notFound(w, r, err) {
		return
	}
	a.dinnerPage(w, r, dinnerFormOf(d))
}

func (a *Admin) dinnerPage(w http.ResponseWriter, r *http.Request, f dinnerForm) {
	a.render(w, "dinners", "edit", struct {
		view
		Form dinnerForm
	}{a.base(r, w, "Večera od "+shortDay(f.Day), "vecera"), f})
}

func (a *Admin) saveDinner(w http.ResponseWriter, r *http.Request) {
	n, ok := id(r)
	if !ok {
		http.NotFound(w, r)
		return
	}
	f, d := a.readDinner(r, n)
	if len(f.Errors) == 0 {
		_, err := a.Store.SaveDinner(r.Context(), d)
		if errors.Is(err, store.ErrTaken) {
			f.Errors["day"] = "Taj dan već ima drugu večeru."
		} else if a.notFound(w, r, err) {
			return
		}
	}
	if len(f.Errors) > 0 {
		a.dinnerPage(w, r, f)
		return
	}
	back(w, r, "/admin/vecera", "ok", "Spremljeno.")
}

func (a *Admin) deleteDinner(w http.ResponseWriter, r *http.Request) {
	n, ok := id(r)
	if !ok {
		http.NotFound(w, r)
		return
	}
	if err := a.Store.DeleteDinner(r.Context(), n); err != nil {
		a.fail(w, err)
		return
	}
	back(w, r, "/admin/vecera", "ok", "Večera je obrisana. Izdanja u kojima je izašla ostaju kakva jesu.")
}

// ---------- Kontakt ----------

// contactKeys are Kontakt's settings, in the form's order.
var contactKeys = []string{press.SettingEmail, press.SettingPress, press.SettingPlace, press.SettingHours}

type contactForm struct {
	Values map[string]string
	Errors map[string]string
}

func (a *Admin) contact(w http.ResponseWriter, r *http.Request) {
	settings, err := a.Store.Settings(r.Context())
	if err != nil {
		a.fail(w, err)
		return
	}
	a.contactPage(w, r, contactForm{Values: settings, Errors: map[string]string{}})
}

func (a *Admin) contactPage(w http.ResponseWriter, r *http.Request, f contactForm) {
	archived := r.URL.Query().Get("arhiva") != ""
	list, err := a.Store.Messages(r.Context(), archived, 200)
	if err != nil {
		a.fail(w, err)
		return
	}
	a.render(w, "contact", "page", struct {
		view
		Messages []store.Message
		Archived bool
		Form     contactForm
	}{a.base(r, w, "Kontakt", "kontakt"), list, archived, f})
}

func (a *Admin) saveContact(w http.ResponseWriter, r *http.Request) {
	f := contactForm{Values: map[string]string{}, Errors: map[string]string{}}
	for _, k := range contactKeys {
		v := strings.TrimSpace(r.PostFormValue(k))
		f.Values[k] = v
		if len([]rune(v)) > 200 {
			f.Errors[k] = "Najviše 200 znakova."
		}
	}
	for _, k := range []string{press.SettingEmail, press.SettingPress} {
		if v := f.Values[k]; v != "" && f.Errors[k] == "" {
			if m, err := mail.ParseAddress(v); err != nil || m.Address != v {
				f.Errors[k] = "Upiši samo adresu, kao urednistvo@primjer.hr."
			}
		}
	}
	if len(f.Errors) > 0 {
		a.contactPage(w, r, f)
		return
	}
	if err := a.Store.SetSettings(r.Context(), f.Values); err != nil {
		a.fail(w, err)
		return
	}
	back(w, r, "/admin/kontakt", "ok", "Spremljeno. Adrese izlaze u sljedećem izdanju.")
}

func (a *Admin) message(w http.ResponseWriter, r *http.Request) {
	n, ok := id(r)
	if !ok {
		http.NotFound(w, r)
		return
	}
	m, err := a.Store.OpenMessage(r.Context(), n)
	if a.notFound(w, r, err) {
		return
	}
	title := m.Subject
	if title == "" {
		title = "Poruka od " + m.Name
	}
	a.render(w, "contact", "message", struct {
		view
		Message store.Message
	}{a.base(r, w, title, "kontakt"), m})
}

func (a *Admin) archiveMessage(w http.ResponseWriter, r *http.Request) {
	n, ok := id(r)
	if !ok {
		http.NotFound(w, r)
		return
	}
	archive := r.PostFormValue("archive") != ""
	if err := a.Store.ArchiveMessage(r.Context(), n, archive); a.notFound(w, r, err) {
		return
	}
	if archive {
		back(w, r, "/admin/kontakt", "ok", "Poruka je u arhivi.")
		return
	}
	back(w, r, "/admin/kontakt/"+strconv.FormatInt(n, 10), "ok", "Poruka je opet u sandučiću.")
}

func (a *Admin) deleteMessage(w http.ResponseWriter, r *http.Request) {
	n, ok := id(r)
	if !ok {
		http.NotFound(w, r)
		return
	}
	if err := a.Store.DeleteMessage(r.Context(), n); err != nil {
		a.fail(w, err)
		return
	}
	back(w, r, "/admin/kontakt", "ok", "Poruka je obrisana.")
}
