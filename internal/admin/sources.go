package admin

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	"tisak/internal/fetch"
	"tisak/internal/jobs"
	"tisak/internal/press"
	"tisak/internal/store"
)

// maxUpload is the largest logo, flag or cover accepted; maxPhoto the largest photo in an article,
// straight from a phone; maxBody the largest form, an article with two new photos.
const (
	maxUpload = 2 << 20
	maxPhoto  = 8 << 20
	maxBody   = 2*maxPhoto + 1<<20
)

type sourceGroup struct {
	Category store.Category
	Sources  []store.Source
}

func (a *Admin) sources(w http.ResponseWriter, r *http.Request) {
	cats, err := a.Store.Categories(r.Context())
	if err != nil {
		a.fail(w, err)
		return
	}
	srcs, err := a.Store.Sources(r.Context())
	if err != nil {
		a.fail(w, err)
		return
	}
	var groups []sourceGroup
	placed := map[int64]bool{}
	for _, c := range cats {
		g := sourceGroup{Category: c}
		for _, s := range srcs {
			if s.CategoryID == c.ID {
				g.Sources = append(g.Sources, s)
				placed[s.ID] = true
			}
		}
		groups = append(groups, g)
	}
	side := sourceGroup{Category: store.Category{Title: "Bočni stupac"}}
	loose := sourceGroup{Category: store.Category{Title: "Bez kategorije"}}
	for _, s := range srcs {
		switch {
		case placed[s.ID]:
		case fetch.Keeps(s.Kind):
			side.Sources = append(side.Sources, s)
		default:
			loose.Sources = append(loose.Sources, s)
		}
	}
	if len(side.Sources) > 0 {
		groups = append(groups, side)
	}
	if len(loose.Sources) > 0 {
		groups = append(groups, loose)
	}
	a.render(w, "sources", "page", struct {
		view
		Groups []sourceGroup
	}{a.base(r, w, "Izvori", "izvori"), groups})
}

// sourceForm is a source as typed, with what is wrong with it.
type sourceForm struct {
	store.Source
	URL, Zone string
	Errors    map[string]string
}

func formOf(s store.Source) sourceForm {
	return sourceForm{Source: s, URL: s.Settings["url"], Zone: s.Settings["zone"], Errors: map[string]string{}}
}

// readSource reads and checks the fields of a source form. Settings it does not know are kept.
func (a *Admin) readSource(r *http.Request, s store.Source) sourceForm {
	f := formOf(s)
	f.Name = strings.TrimSpace(r.PostFormValue("name"))
	f.Kind = r.PostFormValue("kind")
	f.URL = strings.TrimSpace(r.PostFormValue("url"))
	f.Zone = strings.TrimSpace(r.PostFormValue("zone"))
	f.Enabled = r.PostFormValue("enabled") != ""
	f.CategoryID, _ = strconv.ParseInt(r.PostFormValue("category"), 10, 64)
	per, err := strconv.Atoi(r.PostFormValue("per_edition"))
	f.PerEdition = per

	switch {
	case f.Name == "":
		f.Errors["name"] = "Upiši ime izvora."
	case len([]rune(f.Name)) > 80:
		f.Errors["name"] = "Najviše 80 znakova."
	}
	if !fetch.Known(f.Kind) {
		f.Errors["kind"] = "Odaberi vrstu."
	}
	if msg := checkURL(f.URL, needsURL(f.Kind)); msg != "" {
		f.Errors["url"] = msg
	}
	if f.Zone != "" {
		if _, err := time.LoadLocation(f.Zone); err != nil {
			f.Errors["zone"] = "Nepoznata vremenska zona. Primjer: Europe/Zagreb."
		}
	}
	if err != nil || per < 0 || per > 24 {
		f.Errors["per_edition"] = "Broj od 0 do 24."
	}
	switch {
	case fetch.Keeps(f.Kind) && f.CategoryID != 0:
		f.Errors["category"] = "Ova vrsta puni bočni stupac, pa nema kategoriju."
	case fetch.Keeps(f.Kind):
	default:
		if _, err := a.Store.Category(r.Context(), f.CategoryID); err != nil {
			f.Errors["category"] = "Odaberi kategoriju."
		}
	}

	settings := map[string]string{}
	for k, v := range s.Settings {
		settings[k] = v
	}
	settings["url"], settings["zone"] = f.URL, f.Zone
	for k, v := range settings {
		if v == "" {
			delete(settings, k)
		}
	}
	f.Settings = settings
	return f
}

// needsURL tells whether a kind cannot work without an address: Hacker News and the side column know theirs.
func needsURL(kind string) bool { return kind != "hn" && !fetch.Keeps(kind) }

func checkURL(raw string, required bool) string {
	if raw == "" {
		if required {
			return "Upiši adresu feeda."
		}
		return ""
	}
	u, err := url.Parse(raw)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return "Adresa mora počinjati s https:// ili http://."
	}
	return ""
}

func (a *Admin) newSourcePage(w http.ResponseWriter, r *http.Request) {
	s := store.Source{Kind: "rss", PerEdition: 2, Enabled: true}
	s.CategoryID, _ = strconv.ParseInt(r.URL.Query().Get("kategorija"), 10, 64)
	a.sourcePage(w, r, formOf(s))
}

func (a *Admin) sourcePage(w http.ResponseWriter, r *http.Request, f sourceForm) {
	cats, err := a.Store.Categories(r.Context())
	if err != nil {
		a.fail(w, err)
		return
	}
	title := f.Name
	if f.ID == 0 {
		title = "Novi izvor"
	}
	a.render(w, "sources", "edit", struct {
		view
		Form       sourceForm
		Categories []store.Category
		Kinds      []string
	}{a.base(r, w, title, "izvori"), f, cats, fetch.Kinds()})
}

func (a *Admin) newSource(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseMultipartForm(maxUpload); err != nil && !errors.Is(err, http.ErrNotMultipart) {
		back(w, r, "/admin/izvori/novi", "bad", "Logo je prevelik: najviše 2 MB.")
		return
	}
	f := a.readSource(r, store.Source{})
	if hash, msg := a.upload(r, "logo", 240); msg != "" {
		f.Errors["logo"] = msg
	} else {
		f.Logo = hash
	}
	if len(f.Errors) == 0 {
		n, err := a.Store.NewSource(r.Context(), f.Source)
		if errors.Is(err, store.ErrTaken) {
			f.Errors["name"] = "Izvor s tim imenom već postoji."
		} else if err != nil {
			a.fail(w, err)
			return
		} else {
			back(w, r, fmt.Sprintf("/admin/izvori/%d", n), "ok", "Izvor je dodan. Dohvati ga da vidiš što donosi.")
			return
		}
	}
	a.sourcePage(w, r, f)
}

func (a *Admin) source(w http.ResponseWriter, r *http.Request) {
	n, ok := id(r)
	if !ok {
		http.NotFound(w, r)
		return
	}
	s, err := a.Store.Source(r.Context(), n)
	if a.notFound(w, r, err) {
		return
	}
	a.sourcePage(w, r, formOf(s))
}

func (a *Admin) saveSource(w http.ResponseWriter, r *http.Request) {
	n, ok := id(r)
	if !ok {
		http.NotFound(w, r)
		return
	}
	old, err := a.Store.Source(r.Context(), n)
	if a.notFound(w, r, err) {
		return
	}
	if err := r.ParseMultipartForm(maxUpload); err != nil && !errors.Is(err, http.ErrNotMultipart) {
		back(w, r, r.URL.Path, "bad", "Logo je prevelik: najviše 2 MB.")
		return
	}
	f := a.readSource(r, old)
	if hash, msg := a.upload(r, "logo", 240); msg != "" {
		f.Errors["logo"] = msg
	} else if hash != "" {
		f.Logo = hash
	} else if r.PostFormValue("remove_logo") != "" {
		f.Logo = ""
	}
	if len(f.Errors) == 0 {
		err := a.Store.UpdateSource(r.Context(), f.Source)
		if errors.Is(err, store.ErrTaken) {
			f.Errors["name"] = "Izvor s tim imenom već postoji."
		} else if err != nil {
			a.fail(w, err)
			return
		} else {
			back(w, r, r.URL.Path, "ok", "Spremljeno. Promjena vrijedi od sljedećeg dohvata i tiska.")
			return
		}
	}
	a.sourcePage(w, r, f)
}

func (a *Admin) moveSource(w http.ResponseWriter, r *http.Request) {
	n, ok := id(r)
	if !ok {
		http.NotFound(w, r)
		return
	}
	by := 1
	if r.PostFormValue("smjer") == "gore" {
		by = -1
	}
	if a.notFound(w, r, a.Store.MoveSource(r.Context(), n, by)) {
		return
	}
	http.Redirect(w, r, "/admin/izvori", http.StatusSeeOther)
}

func (a *Admin) deleteSource(w http.ResponseWriter, r *http.Request) {
	n, ok := id(r)
	if !ok {
		http.NotFound(w, r)
		return
	}
	s, err := a.Store.Source(r.Context(), n)
	if a.notFound(w, r, err) {
		return
	}
	if err := a.Store.DeleteSource(r.Context(), n); err != nil {
		a.fail(w, err)
		return
	}
	back(w, r, "/admin/izvori", "ok", "Obrisan izvor "+s.Name+". Otisnuta izdanja ga i dalje imaju.")
}

// fetchSource fetches one source now and saves what it gives, like the morning fetch does.
func (a *Admin) fetchSource(w http.ResponseWriter, r *http.Request) {
	n, ok := id(r)
	if !ok {
		http.NotFound(w, r)
		return
	}
	to := fmt.Sprintf("/admin/izvori/%d", n)
	ctx, cancel := detached(r, 2*time.Minute)
	defer cancel()
	results, err := a.Jobs.Fetch(ctx, func(s store.Source) bool { return s.ID == n }, false)
	switch {
	case errors.Is(err, jobs.ErrBusy):
		back(w, r, to, "warn", a.busy())
	case err != nil:
		back(w, r, to, "bad", "Dohvat nije uspio: "+err.Error())
	case len(results) == 0:
		back(w, r, to, "warn", "Izvor je isključen, pa nije dohvaćen.")
	case results[0].Err != nil:
		back(w, r, to, "bad", "Dohvat nije uspio: "+results[0].Err.Error())
	default:
		res := results[0]
		back(w, r, to, "ok", fmt.Sprintf("Dohvaćeno: %s i %s, za %s.", count(res.Items, "stavka", "stavke", "stavki"),
			count(res.Pictures, "slika", "slike", "slika"), res.Took.Round(100*time.Millisecond)))
	}
}

// testSource tries a source's settings without saving anything, from the form as it stands.
func (a *Admin) testSource(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseMultipartForm(maxUpload); err != nil && !errors.Is(err, http.ErrNotMultipart) {
		a.fail(w, err)
		return
	}
	kind := r.PostFormValue("kind")
	settings := map[string]string{"url": strings.TrimSpace(r.PostFormValue("url"))}
	if z := strings.TrimSpace(r.PostFormValue("zone")); z != "" {
		settings["zone"] = z
	}
	type result struct {
		Err      string
		Count    int
		Pictures int
		Items    []store.Item
		Took     time.Duration
	}
	var res result
	f, ok := fetch.Lookup(kind)
	switch {
	case fetch.Keeps(kind):
		res.Err = "Ova vrsta puni bočni stupac i sprema sama. Spremi izvor pa ga dohvati."
	case !ok:
		res.Err = "Odaberi vrstu izvora."
	case checkURL(settings["url"], needsURL(kind)) != "":
		res.Err = checkURL(settings["url"], needsURL(kind))
	default:
		ctx, cancel := context.WithTimeout(r.Context(), 30*time.Second)
		defer cancel()
		start := time.Now()
		items, err := f.Fetch(ctx, a.Client, settings)
		res.Took = time.Since(start)
		if err != nil {
			res.Err = err.Error()
		} else if len(items) == 0 {
			res.Err = "Feed je prazan: nema nijedne stavke."
		}
		res.Count = len(items)
		for _, it := range items {
			if it.PictureURL != "" {
				res.Pictures++
			}
		}
		res.Items = items[:min(8, len(items))]
	}
	a.render(w, "sources", "test", res)
}

var unsafeSVG = regexp.MustCompile(`(?i)<script|<foreignobject|\son[a-z]+\s*=|javascript:`)

// upload stores a picture sent in a form field. It gives the hash, or a message for the admin.
// No file in the field gives neither.
func (a *Admin) upload(r *http.Request, field string, width int) (string, string) {
	return a.uploadUpTo(r, field, width, maxUpload)
}

func (a *Admin) uploadUpTo(r *http.Request, field string, width, limit int) (string, string) {
	file, head, err := r.FormFile(field)
	if errors.Is(err, http.ErrMissingFile) || errors.Is(err, http.ErrNotMultipart) {
		return "", ""
	}
	if err != nil {
		return "", "Slika nije stigla: " + err.Error()
	}
	defer file.Close()
	body, err := io.ReadAll(io.LimitReader(file, int64(limit)+1))
	if err != nil {
		return "", "Slika nije stigla: " + err.Error()
	}
	if len(body) > limit {
		return "", fmt.Sprintf("Slika je prevelika: najviše %d MB.", limit>>20)
	}
	if bytes.Contains(bytes.ToLower(body[:min(512, len(body))]), []byte("<svg")) && unsafeSVG.Match(body) {
		return "", "Ovaj SVG ima skriptu ili poveznicu koja pokreće kod, pa ga ne mogu prihvatiti."
	}
	hash, err := a.Media.SaveBytes(r.Context(), body, "upload:"+head.Filename, width)
	if err != nil {
		return "", "To nije slika koju znam pročitati. Pošalji PNG, JPEG, GIF, WebP ili SVG."
	}
	return hash, ""
}

// count writes a number with its Croatian noun: 1 stavka, 3 stavke, 5 stavki.
func count(n int, one, few, many string) string {
	return fmt.Sprintf("%d %s", n, press.Plural(strconv.Itoa(n), one, few, many))
}
