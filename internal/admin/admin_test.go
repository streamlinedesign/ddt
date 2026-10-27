package admin

import (
	"bytes"
	"context"
	"io"
	"log"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"tisak/internal/auth"
	"tisak/internal/fetch"
	"tisak/internal/jobs"
	"tisak/internal/media"
	"tisak/internal/press"
	"tisak/internal/store"
	"tisak/web"
)

func TestSlugify(t *testing.T) {
	for in, want := range map[string]string{
		"Vijesti":             "vijesti",
		"Hrvatski nogomet":    "hrvatski-nogomet",
		"Šport i žene":        "sport-i-zene",
		"  Đir — po Čakovcu ": "dir-po-cakovcu",
		"Hacker News!":        "hacker-news",
	} {
		if got := Slugify(in); got != want {
			t.Errorf("Slugify(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestNext(t *testing.T) {
	for in, want := range map[string]string{
		"/admin/izvori":         "/admin/izvori",
		"//evil.example/admin/": "/admin/",
		"https://evil.example/": "/admin/",
		"/admin\\..\\x":         "/admin/",
		"/admin/prijava":        "/admin/",
		"":                      "/admin/",
	} {
		r := httptest.NewRequest("GET", "/admin/prijava?dalje="+url.QueryEscape(in), nil)
		if got := next(r); got != want {
			t.Errorf("next(%q) = %q, want %q", in, got, want)
		}
	}
}

// testAdmin builds the dashboard on a fresh database. It needs TISAK_TEST_DB, and uses a
// database of its own (the name plus _admin) so it can run beside the store's tests.
func testAdmin(t *testing.T) (*Admin, http.Handler) {
	t.Helper()
	base := os.Getenv("TISAK_TEST_DB")
	if base == "" {
		t.Skip("TISAK_TEST_DB not set")
	}
	u, err := url.Parse(base)
	if err != nil {
		t.Fatal(err)
	}
	u.Path += "_admin"
	ctx := context.Background()
	conn, err := pgx.Connect(ctx, u.String())
	if err != nil {
		t.Skipf("no database %s: %v", u.Path, err)
	}
	if _, err := conn.Exec(ctx, "DROP SCHEMA public CASCADE; CREATE SCHEMA public"); err != nil {
		t.Fatal(err)
	}
	conn.Close(ctx)

	db, err := store.Open(ctx, u.String())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(db.Close)
	if _, err := db.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	hash, err := auth.HashPassword("ispravna-lozinka")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.SetAdmin(ctx, "uriel", hash); err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	loc, _ := time.LoadLocation("Europe/Zagreb")
	client := fetch.NewClient()
	pics := &media.Store{Dir: dir + "/media", DB: db, Client: client}
	pr := &press.Press{Store: db, Media: pics, Web: web.FS(""), Out: dir + "/editions", Site: "/", Loc: loc}
	runner := &fetch.Runner{Store: db, Client: client, Pictures: pics}
	a := &Admin{Store: db, Auth: &auth.Manager{Store: db}, Jobs: &jobs.Jobs{Runner: runner, Press: pr}, Press: pr,
		Media: pics, Client: client, Web: pr.Web, Loc: loc, Log: log.New(io.Discard, "", 0), FetchAt: "05:15", PrintAt: "05:45"}
	return a, a.Handler()
}

// browser keeps the session cookie between requests, like a browser would.
type browserT struct {
	t      *testing.T
	h      http.Handler
	cookie *http.Cookie
	csrf   string
	ip     string
}

func (b *browserT) do(method, path string, form url.Values, headers map[string]string) *httptest.ResponseRecorder {
	b.t.Helper()
	var body io.Reader
	if form != nil {
		body = strings.NewReader(form.Encode())
	}
	r := httptest.NewRequest(method, path, body)
	r.RemoteAddr = b.ip + ":1234"
	if form != nil {
		r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	}
	if b.cookie != nil {
		r.AddCookie(b.cookie)
	}
	for k, v := range headers {
		r.Header.Set(k, v)
	}
	w := httptest.NewRecorder()
	b.h.ServeHTTP(w, r)
	for _, c := range w.Result().Cookies() {
		if c.Name == auth.Cookie {
			b.cookie = c
		}
	}
	if m := regexp.MustCompile(`name="csrf" value="([^"]+)"`).FindStringSubmatch(w.Body.String()); m != nil {
		b.csrf = m[1]
	}
	return w
}

func (b *browserT) signIn(password string) *httptest.ResponseRecorder {
	return b.do("POST", "/admin/prijava", url.Values{"username": {"uriel"}, "password": {password}, "dalje": {"/admin/izvori"}}, nil)
}

func TestSignInAndCSRF(t *testing.T) {
	a, h := testAdmin(t)
	b := &browserT{t: t, h: h, ip: "10.0.0.1"}

	w := b.do("GET", "/admin/", nil, nil)
	if w.Code != http.StatusSeeOther || w.Header().Get("Location") != "/admin/prijava?dalje=%2Fadmin%2F" {
		t.Fatalf("signed out: got %d to %q", w.Code, w.Header().Get("Location"))
	}
	w = b.do("GET", "/admin/", nil, map[string]string{"HX-Request": "true"})
	if w.Code != http.StatusUnauthorized || w.Header().Get("HX-Redirect") != "/admin/prijava" {
		t.Fatalf("signed out htmx: got %d, HX-Redirect %q", w.Code, w.Header().Get("HX-Redirect"))
	}
	if w := b.signIn("kriva-lozinka"); !strings.Contains(w.Body.String(), "Krivo ime ili lozinka") {
		t.Fatalf("wrong password: %d %s", w.Code, w.Body.String())
	}
	w = b.signIn("ispravna-lozinka")
	if w.Code != http.StatusSeeOther || w.Header().Get("Location") != "/admin/izvori" {
		t.Fatalf("sign in: got %d to %q", w.Code, w.Header().Get("Location"))
	}
	if b.cookie == nil || !b.cookie.HttpOnly || b.cookie.Path != "/admin" || b.cookie.SameSite != http.SameSiteLaxMode {
		t.Fatalf("session cookie: %+v", b.cookie)
	}

	// every page renders
	for _, p := range []string{"/admin/", "/admin/pregled", "/admin/kategorije", "/admin/izvori", "/admin/izvori/novi", "/admin/racun",
		"/admin/clanci", "/admin/rijeci", "/admin/albumi"} {
		if w := b.do("GET", p, nil, nil); w.Code != http.StatusOK {
			t.Errorf("GET %s: %d %s", p, w.Code, w.Body.String())
		}
	}
	if w := b.do("GET", "/admin/kategorije", nil, nil); !strings.Contains(w.Header().Get("Content-Security-Policy"), "script-src 'self'") {
		t.Errorf("no CSP on dashboard pages")
	}

	add := url.Values{"title": {"Šport i žene"}, "layout": {"row3"}, "enabled": {"1"}}
	if w := b.do("POST", "/admin/kategorije", add, nil); w.Code != http.StatusForbidden {
		t.Errorf("POST without CSRF token: %d, want 403", w.Code)
	}
	if w := b.do("POST", "/admin/kategorije", add, map[string]string{auth.CSRFHeader: b.csrf, "Origin": "https://evil.example"}); w.Code != http.StatusForbidden {
		t.Errorf("POST from another site: %d, want 403", w.Code)
	}
	if w := b.do("POST", "/admin/kategorije", add, map[string]string{auth.CSRFHeader: "nije-to"}); w.Code != http.StatusForbidden {
		t.Errorf("POST with a wrong token: %d, want 403", w.Code)
	}
	add.Set("csrf", b.csrf)
	if w := b.do("POST", "/admin/kategorije", add, nil); w.Code != http.StatusSeeOther {
		t.Fatalf("POST with the token in the form: %d %s", w.Code, w.Body.String())
	}
	cats, _ := a.Store.Categories(context.Background())
	if len(cats) != 1 || cats[0].Slug != "sport-i-zene" || cats[0].Title != "Šport i žene" {
		t.Fatalf("categories after adding: %+v", cats)
	}
	// the same slug again is refused with a message, not an error page
	if w := b.do("POST", "/admin/kategorije", add, nil); w.Code != http.StatusOK || !strings.Contains(w.Body.String(), "Ta oznaka već postoji") {
		t.Errorf("duplicate slug: %d", w.Code)
	}

	// a source needs a real category, a known kind and a sane share
	src := url.Values{"csrf": {b.csrf}, "name": {"Primjer"}, "kind": {"rss"}, "url": {"javascript:alert(1)"}, "per_edition": {"99"}, "category": {"0"}}
	w = b.do("POST", "/admin/izvori", src, nil)
	for _, want := range []string{"https://", "Broj od 0 do 24", "Odaberi kategoriju"} {
		if !strings.Contains(w.Body.String(), want) {
			t.Errorf("bad source form should say %q", want)
		}
	}

	// signing out ends the session for good
	if w := b.do("POST", "/admin/odjava", url.Values{"csrf": {b.csrf}}, nil); w.Code != http.StatusSeeOther {
		t.Fatalf("sign out: %d", w.Code)
	}
	old := b.cookie
	b.cookie = &http.Cookie{Name: auth.Cookie, Value: old.Value}
	if w := b.do("GET", "/admin/", nil, nil); w.Code != http.StatusSeeOther {
		t.Errorf("after sign out: %d, want a redirect to sign in", w.Code)
	}
}

func TestLockout(t *testing.T) {
	_, h := testAdmin(t)
	b := &browserT{t: t, h: h, ip: "10.0.0.2"}
	for i := 0; i < 5; i++ {
		b.signIn("kriva-lozinka")
	}
	if w := b.signIn("ispravna-lozinka"); !strings.Contains(w.Body.String(), "Previše pokušaja") {
		t.Errorf("sixth try should be refused even with the right password")
	}
	other := &browserT{t: t, h: h, ip: "10.0.0.3"}
	if w := other.signIn("ispravna-lozinka"); w.Code != http.StatusSeeOther {
		t.Errorf("another address should still sign in: %d", w.Code)
	}
}

func TestPasswordChangeEndsOtherSessions(t *testing.T) {
	a, h := testAdmin(t)
	phone := &browserT{t: t, h: h, ip: "10.0.0.4"}
	laptop := &browserT{t: t, h: h, ip: "10.0.0.5"}
	phone.signIn("ispravna-lozinka")
	laptop.signIn("ispravna-lozinka")
	laptop.do("GET", "/admin/racun", nil, nil)

	f := url.Values{"csrf": {laptop.csrf}, "username": {"uriel"}, "current": {"kriva"}, "password": {"nova-lozinka-1"}, "password2": {"nova-lozinka-1"}}
	if w := laptop.do("POST", "/admin/racun", f, nil); !strings.Contains(w.Body.String(), "Trenutna lozinka nije točna") {
		t.Fatalf("wrong current password was accepted")
	}
	f.Set("current", "ispravna-lozinka")
	if w := laptop.do("POST", "/admin/racun", f, nil); w.Code != http.StatusSeeOther {
		t.Fatalf("password change: %d %s", w.Code, w.Body.String())
	}
	if w := phone.do("GET", "/admin/", nil, nil); w.Code != http.StatusSeeOther {
		t.Errorf("the other session should be signed out, got %d", w.Code)
	}
	if w := laptop.do("GET", "/admin/", nil, nil); w.Code != http.StatusOK {
		t.Errorf("this session should stay, got %d", w.Code)
	}
	adm, _ := a.Store.AdminByUsername(context.Background(), "uriel")
	if !auth.CheckPassword(adm.PasswordHash, "nova-lozinka-1") {
		t.Errorf("new password not saved")
	}
}

func TestUploadRefusesScriptedSVG(t *testing.T) {
	a, _ := testAdmin(t)
	for name, body := range map[string]string{
		"script":  `<svg xmlns="http://www.w3.org/2000/svg"><script>alert(1)</script></svg>`,
		"onload":  `<svg xmlns="http://www.w3.org/2000/svg" onload="alert(1)"><rect width="10" height="10"/></svg>`,
		"foreign": `<svg xmlns="http://www.w3.org/2000/svg"><foreignObject><div>x</div></foreignObject></svg>`,
	} {
		var buf bytes.Buffer
		mw := multipart.NewWriter(&buf)
		fw, _ := mw.CreateFormFile("logo", "logo.svg")
		fw.Write([]byte(body))
		mw.Close()
		r := httptest.NewRequest("POST", "/", &buf)
		r.Header.Set("Content-Type", mw.FormDataContentType())
		if err := r.ParseMultipartForm(maxUpload); err != nil {
			t.Fatal(err)
		}
		if hash, msg := a.upload(r, "logo", 240); hash != "" || msg == "" {
			t.Errorf("%s: SVG was accepted", name)
		}
	}
}
