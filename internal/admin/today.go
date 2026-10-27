package admin

import (
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"tisak/internal/auth"
	"tisak/internal/jobs"
	"tisak/internal/press"
	"tisak/internal/store"
)

type sourceRow struct {
	store.Source
	Category string
}

type previewData struct {
	Sections []press.Section
	Hidden   []store.HiddenItem
	CSRF     string
}

// today is the Danas page: what was printed, what the next print would carry, and how the sources are.
func (a *Admin) today(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	now := time.Now().In(a.Loc)
	day := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)

	eds, err := a.Store.Editions(ctx)
	if err != nil {
		a.fail(w, err)
		return
	}
	var todays []store.EditionInfo
	for _, e := range eds {
		if e.Day.Equal(day) {
			todays = append(todays, e)
		}
	}
	number, version, err := a.Store.NextNumbers(ctx, day)
	if err != nil {
		a.fail(w, err)
		return
	}
	rows, err := a.sourceRows(r)
	if err != nil {
		a.fail(w, err)
		return
	}
	failing := 0
	for _, s := range rows {
		if s.Enabled && s.LastOK != nil && !*s.LastOK {
			failing++
		}
	}
	pv, err := a.previewData(r)
	if err != nil {
		a.fail(w, err)
		return
	}
	last, _ := a.Store.LastFetch(ctx)

	v := a.base(r, w, "Danas", "danas")
	a.render(w, "today", "page", struct {
		view
		Day        time.Time
		Editions   []store.EditionInfo
		Number     int
		Version    int
		Status     jobs.Status
		LastFetch  time.Time
		FetchAt    string
		PrintAt    string
		Sources    []sourceRow
		Failing    int
		Preview    previewData
		AfterPrint bool
	}{v, now, todays, number, version, a.Jobs.Status(), last, a.FetchAt, a.PrintAt, rows, failing, pv,
		now.Format("15:04") >= a.PrintAt})
}

func (a *Admin) sourceRows(r *http.Request) ([]sourceRow, error) {
	cats, err := a.Store.Categories(r.Context())
	if err != nil {
		return nil, err
	}
	srcs, err := a.Store.Sources(r.Context())
	if err != nil {
		return nil, err
	}
	names := map[int64]string{}
	for _, c := range cats {
		names[c.ID] = c.Title
	}
	var out []sourceRow
	for _, c := range cats { // in the order the edition shows them
		for _, s := range srcs {
			if s.CategoryID == c.ID {
				out = append(out, sourceRow{s, c.Title})
			}
		}
	}
	for _, s := range srcs {
		if names[s.CategoryID] == "" {
			out = append(out, sourceRow{s, "Bez kategorije"})
		}
	}
	return out, nil
}

// previewData is what a print right now would carry, and what was hidden from it.
func (a *Admin) previewData(r *http.Request) (previewData, error) {
	now := time.Now().In(a.Loc)
	snap, err := a.Press.Pick(r.Context(), now)
	if err != nil {
		return previewData{}, err
	}
	hidden, err := a.Store.HiddenItems(r.Context(), now.Add(-press.Window))
	if err != nil {
		return previewData{}, err
	}
	who, _ := auth.From(r.Context())
	return previewData{Sections: snap.Sections, Hidden: hidden, CSRF: who.Session.CSRF}, nil
}

func (a *Admin) preview(w http.ResponseWriter, r *http.Request) {
	pv, err := a.previewData(r)
	if err != nil {
		a.fail(w, err)
		return
	}
	a.render(w, "today", "preview", pv)
}

func (a *Admin) hide(w http.ResponseWriter, r *http.Request)   { a.setHidden(w, r, true) }
func (a *Admin) unhide(w http.ResponseWriter, r *http.Request) { a.setHidden(w, r, false) }

// setHidden takes an item out of the next prints, or lets it back, and shows the preview again,
// so the card that moves up to fill the gap is visible at once.
func (a *Admin) setHidden(w http.ResponseWriter, r *http.Request, hidden bool) {
	n, ok := id(r)
	if !ok {
		http.NotFound(w, r)
		return
	}
	if a.notFound(w, r, a.Store.SetItemHidden(r.Context(), n, hidden)) {
		return
	}
	if r.Header.Get("HX-Request") == "" {
		http.Redirect(w, r, "/admin/#pregled", http.StatusSeeOther)
		return
	}
	a.preview(w, r)
}

// fetchAll runs every fetcher now and comes back to Danas with a summary.
func (a *Admin) fetchAll(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := detached(r, 5*time.Minute)
	defer cancel()
	results, err := a.Jobs.Fetch(ctx, nil, false)
	if errors.Is(err, jobs.ErrBusy) {
		back(w, r, "/admin/", "warn", a.busy())
		return
	}
	if err != nil {
		back(w, r, "/admin/", "bad", "Dohvat nije uspio: "+err.Error())
		return
	}
	items, failed := 0, []string{}
	for _, res := range results {
		items += res.Items
		if res.Err != nil {
			failed = append(failed, res.Source.Name)
		}
	}
	msg := fmt.Sprintf("Dohvaćeno: %s, %s.", count(len(results), "izvor", "izvora", "izvora"), count(items, "stavka", "stavke", "stavki"))
	kind := "ok"
	if len(failed) > 0 {
		kind = "warn"
		msg += " Nije uspjelo: " + strings.Join(failed, ", ") + "."
	}
	back(w, r, "/admin/", kind, msg)
}

// print prints today's next edition. A reprint needs a reason, which the archive shows.
func (a *Admin) print(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := detached(r, 5*time.Minute)
	defer cancel()
	now := time.Now().In(a.Loc)
	day := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)
	printed, err := a.Store.HasEdition(ctx, day)
	if err != nil {
		a.fail(w, err)
		return
	}
	reason := strings.TrimSpace(r.PostFormValue("razlog"))
	if printed && reason == "" {
		back(w, r, "/admin/", "bad", "Za pretisak upiši razlog: on stoji u arhivi uz izdanje.")
		return
	}
	if len([]rune(reason)) > 120 {
		back(w, r, "/admin/", "bad", "Razlog je predug: najviše 120 znakova.")
		return
	}
	if r.PostFormValue("dohvati") != "" {
		if _, err := a.Jobs.Fetch(ctx, nil, false); errors.Is(err, jobs.ErrBusy) {
			back(w, r, "/admin/", "warn", a.busy())
			return
		} else if err != nil {
			back(w, r, "/admin/", "bad", "Dohvat prije tiska nije uspio: "+err.Error())
			return
		}
	}
	e, _, err := a.Jobs.Print(ctx, reason, false)
	if errors.Is(err, jobs.ErrBusy) {
		back(w, r, "/admin/", "warn", a.busy())
		return
	}
	if err != nil {
		back(w, r, "/admin/", "bad", "Tisak nije uspio: "+err.Error())
		return
	}
	link := fmt.Sprintf("%s%s/v%d/", a.Press.Site, e.Day.Format("2006/01/02"), e.Version)
	setFlash(w, "ok", fmt.Sprintf("Otisnuto: Br. %d, Verzija %d (%d KB).", e.Number, e.Version, (e.Bytes+512)/1024), link)
	http.Redirect(w, r, "/admin/", http.StatusSeeOther)
}

func (a *Admin) busy() string {
	s := a.Jobs.Status()
	what := "tiska"
	if strings.HasPrefix(s.Running, "fetch") {
		what = "dohvaća"
	}
	return fmt.Sprintf("Upravo se %s (od %s). Pokušaj ponovo za minutu.", what, s.RunningSince.In(a.Loc).Format("15:04:05"))
}
