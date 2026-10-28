// Package admin is the dashboard: one admin signs in, watches the day's fetch and print,
// and manages categories and sources. Pages are plain HTML forms; htmx (hx-boost) makes them
// feel instant and swaps the few parts that change in place.
package admin

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"html/template"
	"io/fs"
	"log"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	"tisak/internal/auth"
	"tisak/internal/fetch"
	"tisak/internal/jobs"
	"tisak/internal/media"
	"tisak/internal/press"
	"tisak/internal/store"
	"tisak/internal/tmdb"
)

type Admin struct {
	Store  *store.Store
	Auth   *auth.Manager
	Jobs   *jobs.Jobs
	Press  *press.Press
	Media  *media.Store
	Client *fetch.Client
	TMDB   *tmdb.Client // for picking films; its key comes from the environment
	Web    fs.FS        // admin/ templates and static/ files
	Loc    *time.Location
	Log    *log.Logger
	// FetchAt and PrintAt are the daily times, shown on the Today page.
	FetchAt, PrintAt string
}

const signIn = "/admin/prijava"

func (a *Admin) Handler() http.Handler {
	mux := http.NewServeMux()
	static, _ := fs.Sub(a.Web, "static")
	mux.Handle("GET /admin/static/", http.StripPrefix("/admin/static/", http.FileServerFS(static)))
	mux.HandleFunc("GET /admin/prijava", a.signInPage)
	mux.HandleFunc("POST /admin/prijava", a.signIn)

	in := http.NewServeMux() // everything here needs the admin signed in
	in.HandleFunc("GET /admin/{$}", a.today)
	in.HandleFunc("GET /admin/pregled", a.preview)
	in.HandleFunc("POST /admin/dohvati", a.fetchAll)
	in.HandleFunc("POST /admin/otisni", a.print)
	in.HandleFunc("POST /admin/stavke/{id}/skrij", a.hide)
	in.HandleFunc("POST /admin/stavke/{id}/vrati", a.unhide)

	in.HandleFunc("GET /admin/kategorije", a.categories)
	in.HandleFunc("POST /admin/kategorije", a.newCategory)
	in.HandleFunc("GET /admin/kategorije/{id}", a.category)
	in.HandleFunc("POST /admin/kategorije/{id}", a.saveCategory)
	in.HandleFunc("POST /admin/kategorije/{id}/pomakni", a.moveCategory)
	in.HandleFunc("POST /admin/kategorije/{id}/obrisi", a.deleteCategory)

	in.HandleFunc("GET /admin/izvori", a.sources)
	in.HandleFunc("GET /admin/izvori/novi", a.newSourcePage)
	in.HandleFunc("POST /admin/izvori", a.newSource)
	in.HandleFunc("POST /admin/izvori/provjeri", a.testSource)
	in.HandleFunc("GET /admin/izvori/{id}", a.source)
	in.HandleFunc("POST /admin/izvori/{id}", a.saveSource)
	in.HandleFunc("POST /admin/izvori/{id}/pomakni", a.moveSource)
	in.HandleFunc("POST /admin/izvori/{id}/dohvati", a.fetchSource)
	in.HandleFunc("POST /admin/izvori/{id}/obrisi", a.deleteSource)

	in.HandleFunc("GET /admin/clanci", a.articles)
	in.HandleFunc("POST /admin/clanci", a.newArticle)
	in.HandleFunc("GET /admin/clanci/{id}", a.article)
	in.HandleFunc("POST /admin/clanci/{id}", a.saveArticle)
	in.HandleFunc("POST /admin/clanci/{id}/dan", a.setArticleDay)
	in.HandleFunc("POST /admin/clanci/{id}/prikvaci", a.pinArticle)
	in.HandleFunc("POST /admin/clanci/{id}/obrisi", a.deleteArticle)

	in.HandleFunc("GET /admin/rijeci", a.words)
	in.HandleFunc("POST /admin/rijeci", a.newWord)
	in.HandleFunc("GET /admin/rijeci/{id}", a.word)
	in.HandleFunc("POST /admin/rijeci/{id}", a.saveWord)
	in.HandleFunc("POST /admin/rijeci/{id}/obrisi", a.deleteWord)

	in.HandleFunc("GET /admin/albumi", a.albums)
	in.HandleFunc("POST /admin/albumi", a.newAlbum)
	in.HandleFunc("GET /admin/albumi/{id}", a.album)
	in.HandleFunc("POST /admin/albumi/{id}", a.saveAlbum)
	in.HandleFunc("POST /admin/albumi/{id}/obrisi", a.deleteAlbum)

	in.HandleFunc("GET /admin/dogadanja", a.events)
	in.HandleFunc("POST /admin/dogadanja", a.newEvent)
	in.HandleFunc("GET /admin/dogadanja/{id}", a.event)
	in.HandleFunc("POST /admin/dogadanja/{id}", a.saveEvent)
	in.HandleFunc("POST /admin/dogadanja/{id}/obrisi", a.deleteEvent)

	in.HandleFunc("GET /admin/filmovi", a.films)
	in.HandleFunc("POST /admin/filmovi/preporuke", a.addPick)
	in.HandleFunc("POST /admin/filmovi/preporuke/makni", a.removePick)
	in.HandleFunc("POST /admin/filmovi/{id}/skrij", a.hideRelease)

	in.HandleFunc("GET /admin/kazalista", a.theatres)
	in.HandleFunc("POST /admin/kazalista/{id}/skrij", a.hideShow)

	in.HandleFunc("GET /admin/vecera", a.dinners)
	in.HandleFunc("POST /admin/vecera", a.newDinner)
	in.HandleFunc("GET /admin/vecera/{id}", a.dinner)
	in.HandleFunc("POST /admin/vecera/{id}", a.saveDinner)
	in.HandleFunc("POST /admin/vecera/{id}/obrisi", a.deleteDinner)

	in.HandleFunc("GET /admin/kontakt", a.contact)
	in.HandleFunc("POST /admin/kontakt", a.saveContact)
	in.HandleFunc("GET /admin/kontakt/{id}", a.message)
	in.HandleFunc("POST /admin/kontakt/{id}/arhiva", a.archiveMessage)
	in.HandleFunc("POST /admin/kontakt/{id}/obrisi", a.deleteMessage)

	in.HandleFunc("GET /admin/racun", a.account)
	in.HandleFunc("POST /admin/racun", a.saveAccount)
	in.HandleFunc("POST /admin/racun/odjavi-ostale", a.signOutOthers)
	in.HandleFunc("POST /admin/odjava", a.signOut)

	in.HandleFunc("GET /admin/slike/{file}", a.picture)
	mux.Handle("/admin/", a.Auth.Require(signIn, in))
	return secure(mux)
}

// secure sets the headers every dashboard response carries: nothing from elsewhere runs here,
// nothing frames it, and nothing is cached.
func secure(h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hd := w.Header()
		hd.Set("Content-Security-Policy", "default-src 'self'; img-src 'self' data:; style-src 'self'; script-src 'self'; "+
			"frame-ancestors 'none'; base-uri 'none'; form-action 'self'")
		hd.Set("X-Content-Type-Options", "nosniff")
		hd.Set("Referrer-Policy", "same-origin")
		hd.Set("X-Frame-Options", "DENY")
		r.Body = http.MaxBytesReader(w, r.Body, maxBody) // a few pictures and the form around them, no more
		if !strings.HasPrefix(r.URL.Path, "/admin/static/") {
			hd.Set("Cache-Control", "no-store")
		}
		h.ServeHTTP(w, r)
	})
}

// ---------- rendering ----------

// view is what every page gets.
type view struct {
	Title  string
	Nav    string // which menu entry is lit
	Who    auth.Who
	Flash  *flash
	Unread int // Kontakt's unread messages, beside its menu entry
}

func (v view) CSRF() string { return v.Who.Session.CSRF }

func (a *Admin) base(r *http.Request, w http.ResponseWriter, title, nav string) view {
	who, _ := auth.From(r.Context())
	unread, _ := a.Store.UnreadMessages(r.Context()) // a count in the menu is not worth failing a page over
	return view{Title: title, Nav: nav, Who: who, Flash: takeFlash(w, r), Unread: unread}
}

// render executes one block of a page. The page's file is parsed with the layout each time,
// which is quick, and means -dev shows template edits at once.
func (a *Admin) render(w http.ResponseWriter, page, block string, data any) {
	t, err := template.New("").Funcs(a.funcs()).ParseFS(a.Web, "admin/layout.html", "admin/icons.html", "admin/"+page+".html")
	if err != nil {
		a.fail(w, err)
		return
	}
	var buf bytes.Buffer
	if err := t.ExecuteTemplate(&buf, block, data); err != nil {
		a.fail(w, err)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	buf.WriteTo(w)
}

func (a *Admin) funcs() template.FuncMap {
	return template.FuncMap{
		"clock": func(t time.Time) string {
			if t.IsZero() {
				return "—"
			}
			return t.In(a.Loc).Format("15:04")
		},
		"when": a.when,
		"whenp": func(t *time.Time) string {
			if t == nil {
				return "—"
			}
			return a.when(*t)
		},
		"took": func(d time.Duration) string {
			if d < time.Second {
				return d.Round(time.Millisecond).String()
			}
			return d.Round(100 * time.Millisecond).String()
		},
		"kb":       func(n int) string { return fmt.Sprintf("%d KB", (n+512)/1024) },
		"media":    a.mediaURL,
		"cardpic":  func(p *press.Pic) string { return "/admin/slike/" + p.File() },
		"day":      func(t time.Time) string { return t.Format("2006/01/02") },
		"daylong":  press.DayLong,
		"plural":   func(n int, one, few, many string) string { return press.Plural(strconv.Itoa(n), one, few, many) },
		"short":    func(s string, n int) string { return short(s, n) },
		"browser":  browser,
		"deref":    func(b *bool) bool { return b != nil && *b },
		"isnil":    func(b *bool) bool { return b == nil },
		"add":      func(a, b int) int { return a + b },
		"hostname": hostname,
		"date":     func(t time.Time) string { return t.Format("2006-01-02") },
		"sday":     shortDay,
		"deday": func(t *time.Time) time.Time {
			if t == nil {
				return time.Time{}
			}
			return *t
		},
		"plain": press.Plain,
	}
}

// when writes a time the short way: 07:30 today, otherwise 26.10. 07:30.
func (a *Admin) when(t time.Time) string {
	if t.IsZero() {
		return "—"
	}
	t = t.In(a.Loc)
	now := time.Now().In(a.Loc)
	if t.Year() == now.Year() && t.YearDay() == now.YearDay() {
		return t.Format("15:04")
	}
	return t.Format("02.01. 15:04")
}

func (a *Admin) mediaURL(hash string) string {
	if hash == "" {
		return ""
	}
	m, err := a.Store.MediaByHash(context.Background(), hash)
	if err != nil {
		return ""
	}
	return "/admin/slike/" + m.Hash + "." + m.Ext
}

var mediaFile = regexp.MustCompile(`^([0-9a-f]{64})\.(jpg|png|svg)$`)

// picture shows a stored picture: logos, flags and the preview's photos.
func (a *Admin) picture(w http.ResponseWriter, r *http.Request) {
	m := mediaFile.FindStringSubmatch(r.PathValue("file"))
	if m == nil {
		http.NotFound(w, r)
		return
	}
	// an SVG opened on its own must not run anything
	w.Header().Set("Content-Security-Policy", "default-src 'none'; style-src 'unsafe-inline'; sandbox")
	w.Header().Set("Cache-Control", "private, max-age=86400, immutable")
	http.ServeFile(w, r, a.Media.Path(m[1], m[2]))
}

// fail logs an error and answers with a short message. htmx shows it in the error bar.
func (a *Admin) fail(w http.ResponseWriter, err error) {
	a.Log.Printf("admin: %v", err)
	http.Error(w, "Greška na poslužitelju: "+err.Error(), http.StatusInternalServerError)
}

func (a *Admin) notFound(w http.ResponseWriter, r *http.Request, err error) bool {
	if errors.Is(err, store.ErrNotFound) {
		http.NotFound(w, r)
		return true
	}
	if err != nil {
		a.fail(w, err)
		return true
	}
	return false
}

// ---------- flash messages ----------

// flash is a one-line message shown on the next page, after a form's redirect.
type flash struct {
	Kind string // ok, bad, warn
	Text string
	Link string
}

const flashCookie = "tisak_flash"

func setFlash(w http.ResponseWriter, kind, text, link string) {
	v := url.Values{"k": {kind}, "t": {text}, "l": {link}}
	http.SetCookie(w, &http.Cookie{Name: flashCookie, Value: v.Encode(), Path: "/admin", MaxAge: 60,
		HttpOnly: true, SameSite: http.SameSiteLaxMode})
}

func takeFlash(w http.ResponseWriter, r *http.Request) *flash {
	c, err := r.Cookie(flashCookie)
	if err != nil || c.Value == "" {
		return nil
	}
	http.SetCookie(w, &http.Cookie{Name: flashCookie, Value: "", Path: "/admin", MaxAge: -1, HttpOnly: true})
	v, err := url.ParseQuery(c.Value)
	if err != nil {
		return nil
	}
	return &flash{Kind: v.Get("k"), Text: v.Get("t"), Link: v.Get("l")}
}

// back sends the browser on after a form, with a message to show there.
func back(w http.ResponseWriter, r *http.Request, to, kind, text string) {
	if text != "" {
		setFlash(w, kind, text, "")
	}
	http.Redirect(w, r, to, http.StatusSeeOther)
}

// ---------- small helpers ----------

func id(r *http.Request) (int64, bool) {
	n, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	return n, err == nil && n > 0
}

func short(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return strings.TrimSpace(string(r[:n-1])) + "…"
}

func hostname(link string) string {
	u, err := url.Parse(link)
	if err != nil {
		return link
	}
	return strings.TrimPrefix(u.Hostname(), "www.")
}

// browser names the browser and system from a user agent, roughly, for the sessions list.
func browser(ua string) string {
	name := "Preglednik"
	switch {
	case strings.Contains(ua, "Edg/"):
		name = "Edge"
	case strings.Contains(ua, "Firefox/"):
		name = "Firefox"
	case strings.Contains(ua, "Chrome/"):
		name = "Chrome"
	case strings.Contains(ua, "Safari/"):
		name = "Safari"
	case strings.Contains(ua, "curl/"):
		name = "curl"
	}
	system := ""
	switch {
	case strings.Contains(ua, "iPhone"), strings.Contains(ua, "iPad"):
		system = "iOS"
	case strings.Contains(ua, "Android"):
		system = "Android"
	case strings.Contains(ua, "Mac OS X"):
		system = "macOS"
	case strings.Contains(ua, "Windows"):
		system = "Windows"
	case strings.Contains(ua, "Linux"):
		system = "Linux"
	}
	if system == "" {
		return name
	}
	return name + ", " + system
}

// detached keeps a job running if the browser goes away halfway, up to a limit.
func detached(r *http.Request, limit time.Duration) (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.WithoutCancel(r.Context()), limit)
}
