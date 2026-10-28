// Package site serves the public pages: today's edition, every dated edition, the archive.
// It only ever serves finished editions from disk; it never renders the news itself.
package site

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"tisak/internal/press"
	"tisak/internal/store"
)

type Site struct {
	Store *store.Store
	Press *press.Press
	Log   *log.Logger
	// ClientIP is the address a request came from, through the proxy when there is one.
	ClientIP func(*http.Request) string
}

func (s *Site) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) { fmt.Fprintln(w, "ok") })
	mux.HandleFunc("GET /arhiva/{$}", s.archive)
	mux.HandleFunc("GET /sve/{$}", s.sve)
	mux.HandleFunc("POST /kontakt", s.contact)
	mux.HandleFunc("GET /{$}", s.today)
	mux.HandleFunc("GET /index.html", s.today)
	mux.HandleFunc("GET /sve.html", s.todayFile)
	mux.HandleFunc("GET /images/{file}", s.todayFile)
	mux.HandleFunc("GET /{y}/{m}/{d}/{rest...}", s.dated)
	return logged(s.Log, mux)
}

// latest is the newest edition of all, or of one day when day is set.
func (s *Site) latest(ctx context.Context, day *time.Time) (store.EditionInfo, error) {
	eds, err := s.Store.Editions(ctx)
	if err != nil {
		return store.EditionInfo{}, err
	}
	for _, e := range eds { // newest day first, highest version first
		if day == nil || e.Day.Equal(*day) {
			return e, nil
		}
	}
	return store.EditionInfo{}, store.ErrNotFound
}

func (s *Site) today(w http.ResponseWriter, r *http.Request) {
	e, err := s.latest(r.Context(), nil)
	if s.fail(w, err) {
		return
	}
	w.Header().Set("Cache-Control", "public, max-age=300")
	s.file(w, r, e, "index.html")
}

// todayFile serves sve.html and pictures for the edition shown at the root.
func (s *Site) todayFile(w http.ResponseWriter, r *http.Request) {
	e, err := s.latest(r.Context(), nil)
	if s.fail(w, err) {
		return
	}
	s.file(w, r, e, strings.TrimPrefix(r.URL.Path, "/"))
}

func (s *Site) sve(w http.ResponseWriter, r *http.Request) {
	e, err := s.latest(r.Context(), nil)
	if s.fail(w, err) {
		return
	}
	http.Redirect(w, r, fmt.Sprintf("/%s/v%d/sve.html", e.Day.Format("2006/01/02"), e.Version), http.StatusFound)
}

var version = regexp.MustCompile(`^v(\d+)$`)

// dated serves /2026/10/27/ (the day's newest version) and /2026/10/27/v2/ (that version), with their files.
func (s *Site) dated(w http.ResponseWriter, r *http.Request) {
	day, err := time.Parse("2006/01/02", r.PathValue("y")+"/"+r.PathValue("m")+"/"+r.PathValue("d"))
	if err != nil {
		http.NotFound(w, r)
		return
	}
	rest := r.PathValue("rest")
	parts := strings.SplitN(rest, "/", 2)
	if m := version.FindStringSubmatch(parts[0]); m != nil {
		v, _ := strconv.Atoi(m[1])
		if len(parts) == 1 { // /v2 without its slash: relative links need it
			http.Redirect(w, r, r.URL.Path+"/", http.StatusMovedPermanently)
			return
		}
		e := store.EditionInfo{Day: day, Version: v}
		if !s.exists(e) {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Cache-Control", "public, max-age=31536000, immutable") // an edition never changes
		s.file(w, r, e, parts[1])
		return
	}
	e, err := s.latest(r.Context(), &day)
	if s.fail(w, err) {
		return
	}
	w.Header().Set("Cache-Control", "public, max-age=300")
	s.file(w, r, e, rest)
}

func (s *Site) exists(e store.EditionInfo) bool {
	_, err := os.Stat(s.Press.Dir(e.Day, e.Version))
	return err == nil
}

// file serves one file from an edition's folder, never anything outside it.
func (s *Site) file(w http.ResponseWriter, r *http.Request, e store.EditionInfo, name string) {
	if name == "" {
		name = "index.html"
	}
	clean := filepath.Clean("/" + name)[1:]
	if clean != name && clean+"/" != name || strings.HasPrefix(clean, "..") {
		http.NotFound(w, r)
		return
	}
	http.ServeFile(w, r, filepath.Join(s.Press.Dir(e.Day, e.Version), clean))
}

func (s *Site) archive(w http.ResponseWriter, r *http.Request) {
	eds, err := s.Store.Editions(r.Context())
	if s.fail(w, err) {
		return
	}
	page, err := s.Press.RenderArchive(eds)
	if s.fail(w, err) {
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "public, max-age=300")
	w.Write(page)
}

func (s *Site) fail(w http.ResponseWriter, err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, store.ErrNotFound) {
		http.Error(w, "Tisak još nije otisnut.", http.StatusNotFound)
		return true
	}
	s.Log.Printf("site: %v", err)
	http.Error(w, "Greška na poslužitelju.", http.StatusInternalServerError)
	return true
}

type status struct {
	http.ResponseWriter
	code int
}

func (s *status) WriteHeader(c int) { s.code = c; s.ResponseWriter.WriteHeader(c) }

func logged(l *log.Logger, h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		st := &status{ResponseWriter: w, code: 200}
		start := time.Now()
		h.ServeHTTP(st, r)
		if st.code >= 400 || r.URL.Path == "/" {
			l.Printf("%d %s %s %s", st.code, r.Method, r.URL.Path, time.Since(start).Round(time.Millisecond))
		}
	})
}
