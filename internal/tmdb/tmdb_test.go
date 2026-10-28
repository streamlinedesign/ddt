package tmdb

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// A made-up TMDB: one film with a Japanese title, its English one and its Croatian release dates.
func fakeTMDB(t *testing.T) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		if q.Get("api_key") != "tajni-kljuc" {
			w.WriteHeader(http.StatusUnauthorized)
			w.Write([]byte(`{"status_message":"Invalid API key"}`))
			return
		}
		switch {
		case r.URL.Path == "/movie/upcoming":
			if q.Get("region") != "HR" || q.Get("language") != "hr-HR" {
				t.Errorf("upcoming asked with %s", r.URL.RawQuery)
			}
			w.Write([]byte(`{"page":1,"total_pages":1,"results":[{"id":7,"title":"ゴジラ","release_date":"2026-10-01"}]}`))
		case r.URL.Path == "/movie/7" && q.Get("language") == "en-US":
			w.Write([]byte(`{"id":7,"title":"Monster Film"}`))
		case r.URL.Path == "/movie/7":
			w.Write([]byte(`{"id":7,"title":"ゴジラ","original_title":"ゴジラ","release_date":"2026-10-01","runtime":125,
				"poster_path":"/p.jpg","genres":[{"id":878,"name":"Science Fiction"},{"id":53,"name":"Thriller"}],
				"release_dates":{"results":[
					{"iso_3166_1":"US","release_dates":[{"release_date":"2026-10-01T00:00:00.000Z","type":3}]},
					{"iso_3166_1":"HR","release_dates":[{"release_date":"2026-10-20T00:00:00.000Z","type":2},
						{"release_date":"2026-11-05T00:00:00.000Z","type":3},{"release_date":"2026-12-01T00:00:00.000Z","type":4}]}]}}`))
		default:
			http.NotFound(w, r)
		}
	}))
}

func TestMovie(t *testing.T) {
	srv := fakeTMDB(t)
	defer srv.Close()
	c := &Client{Key: "tajni-kljuc", Endpoint: srv.URL, HTTP: srv.Client()}
	list, pages, err := c.Upcoming(context.Background(), "HR", 1)
	if err != nil || pages != 1 || len(list) != 1 || list[0].Year() != 2026 {
		t.Fatalf("upcoming = %+v, %d, %v", list, pages, err)
	}
	m, err := c.Movie(context.Background(), 7)
	if err != nil {
		t.Fatal(err)
	}
	if m.Title != "Monster Film" {
		t.Errorf("title %q: a title without Latin letters takes the English one", m.Title)
	}
	if g := m.GenreList(); g != "Znanstvena fantastika, Triler" {
		t.Errorf("genres %q", g)
	}
	if d := m.Release("HR"); d == nil || d.Format("2006-01-02") != "2026-11-05" {
		t.Errorf("release %v: the theatrical date wins over the limited one", d)
	}
	if m.Release("DE") != nil {
		t.Error("no release in Germany")
	}
	if PosterURL(m.PosterPath, "w500") != "https://image.tmdb.org/t/p/w500/p.jpg" || PosterURL("", "w500") != "" {
		t.Error("poster URL")
	}
}

func TestKeyStaysSecret(t *testing.T) {
	if _, err := (&Client{}).Search(context.Background(), "x"); err != ErrNoKey {
		t.Errorf("no key: %v", err)
	}
	srv := fakeTMDB(t)
	defer srv.Close()
	c := &Client{Key: "pogresan-kljuc", Endpoint: srv.URL, HTTP: srv.Client()}
	_, err := c.Search(context.Background(), "x")
	if err == nil || strings.Contains(err.Error(), "pogresan-kljuc") || !strings.Contains(err.Error(), "Invalid API key") {
		t.Errorf("error %v", err)
	}
	// A failed connection names the address, key and all; the error must not.
	c = &Client{Key: "pogresan-kljuc", Endpoint: "http://127.0.0.1:1", HTTP: srv.Client()}
	if _, err := c.Search(context.Background(), "x"); err == nil || strings.Contains(err.Error(), "pogresan-kljuc") {
		t.Errorf("error %v", err)
	}
}
