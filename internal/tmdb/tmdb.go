// Package tmdb is a small client of The Movie Database's API (v3): search, a film's details with its
// release dates, and the films coming to Croatian cinemas.
//
// The key is TMDB_API_KEY in the server's environment and nowhere else: it is never stored, printed
// or put in an error. TMDB's terms ask for its credit wherever its data shows; the edition prints it
// under Filmovi.
package tmdb

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

const (
	Endpoint  = "https://api.themoviedb.org/3"
	ImageBase = "https://image.tmdb.org/t/p/"
	// KeyVar is the environment variable that holds the key.
	KeyVar = "TMDB_API_KEY"
)

// ErrNoKey is returned when there is no key.
var ErrNoKey = errors.New(KeyVar + " is not set in the server's environment")

// Client calls TMDB in Croatian. Titles and overviews fall back to the original where TMDB has no translation.
type Client struct {
	Key      string
	HTTP     *http.Client
	Endpoint string // Endpoint when empty; tests point it at their own server
}

// FromEnv is a client with the key from the environment; Key is empty when it is not set.
func FromEnv() *Client {
	return &Client{Key: strings.TrimSpace(os.Getenv(KeyVar)), HTTP: &http.Client{Timeout: 20 * time.Second}}
}

// Result is a film in a list: search, upcoming.
type Result struct {
	ID            int    `json:"id"`
	Title         string `json:"title"`
	OriginalTitle string `json:"original_title"`
	ReleaseDate   string `json:"release_date"` // the first release anywhere
	PosterPath    string `json:"poster_path"`
	Overview      string `json:"overview"`
}

// Year is the year of the first release, or 0.
func (r Result) Year() int {
	y, _ := strconv.Atoi(strings.SplitN(r.ReleaseDate, "-", 2)[0])
	return y
}

// Movie is a film's details with its release dates.
type Movie struct {
	Result
	Runtime int `json:"runtime"`
	Genres  []struct {
		ID   int    `json:"id"`
		Name string `json:"name"`
	} `json:"genres"`
	ReleaseDates struct {
		Results []struct {
			Country string `json:"iso_3166_1"`
			Dates   []struct {
				Date string `json:"release_date"` // 2026-10-30T00:00:00.000Z
				Type int    `json:"type"`         // 2 limited, 3 theatrical
			} `json:"release_dates"`
		} `json:"results"`
	} `json:"release_dates"`
}

// genresHR are TMDB's genres in Croatian: TMDB answers hr-HR requests with English genre names.
var genresHR = map[int]string{
	28: "Akcija", 12: "Avantura", 16: "Animirani", 35: "Komedija", 80: "Kriminalistički", 99: "Dokumentarni",
	18: "Drama", 10751: "Obiteljski", 14: "Fantastika", 36: "Povijesni", 27: "Horor", 10402: "Glazbeni",
	9648: "Misterij", 10749: "Romantični", 878: "Znanstvena fantastika", 10770: "TV film", 53: "Triler",
	10752: "Ratni", 37: "Vestern",
}

// GenreList is the film's genres in Croatian: "Drama, Triler".
func (m *Movie) GenreList() string {
	var g []string
	for _, x := range m.Genres {
		name := genresHR[x.ID]
		if name == "" {
			name = x.Name
		}
		if r, n := utf8.DecodeRuneInString(name); n > 0 {
			g = append(g, string(unicode.ToUpper(r))+name[n:])
		}
	}
	return strings.Join(g, ", ")
}

// Release is the film's first theatrical (else limited) release in the country, or nil.
func (m *Movie) Release(country string) *time.Time {
	var best *time.Time
	bestType := 0
	for _, c := range m.ReleaseDates.Results {
		if c.Country != country {
			continue
		}
		for _, d := range c.Dates {
			if d.Type != 2 && d.Type != 3 {
				continue
			}
			t, err := time.Parse(time.DateOnly, d.Date[:min(10, len(d.Date))])
			if err != nil {
				continue
			}
			if best == nil || d.Type > bestType || (d.Type == bestType && t.Before(*best)) {
				best, bestType = &t, d.Type
			}
		}
	}
	return best
}

// latin tells whether s has a Latin letter: a reader of the paper cannot read "ゴジラ", so such a title
// is replaced by the English one.
func latin(s string) bool {
	for _, r := range s {
		if unicode.Is(unicode.Latin, r) {
			return true
		}
	}
	return false
}

// PosterURL is the address of a poster at a width TMDB serves: w185, w342, w500.
func PosterURL(path, size string) string {
	if path == "" {
		return ""
	}
	return ImageBase + size + path
}

// Link is the film's page on TMDB.
func Link(id int) string { return "https://www.themoviedb.org/movie/" + strconv.Itoa(id) }

func (c *Client) get(ctx context.Context, path string, q url.Values, v any) error {
	if c == nil || c.Key == "" {
		return ErrNoKey
	}
	endpoint := c.Endpoint
	if endpoint == "" {
		endpoint = Endpoint
	}
	q.Set("api_key", c.Key)
	if q.Get("language") == "" {
		q.Set("language", "hr-HR")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint+path+"?"+q.Encode(), nil)
	if err != nil {
		return c.strip(err)
	}
	req.Header.Set("Accept", "application/json")
	hc := c.HTTP
	if hc == nil {
		hc = http.DefaultClient
	}
	resp, err := hc.Do(req)
	if err != nil {
		return c.strip(err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return c.strip(err)
	}
	if resp.StatusCode != http.StatusOK {
		var e struct {
			Message string `json:"status_message"`
		}
		json.Unmarshal(body, &e)
		return c.strip(fmt.Errorf("TMDB %s: %s %s", path, resp.Status, e.Message))
	}
	if err := json.Unmarshal(body, v); err != nil {
		return fmt.Errorf("TMDB %s: %w", path, err)
	}
	return nil
}

// strip keeps the key out of an error: a url.Error carries the address.
func (c *Client) strip(err error) error {
	return errors.New(strings.ReplaceAll(err.Error(), c.Key, "…"))
}

// Search finds films by title.
func (c *Client) Search(ctx context.Context, query string) ([]Result, error) {
	var x struct {
		Results []Result `json:"results"`
	}
	err := c.get(ctx, "/search/movie", url.Values{"query": {query}, "include_adult": {"false"}}, &x)
	return x.Results, err
}

// Movie is a film's details with its release dates.
func (c *Client) Movie(ctx context.Context, id int) (*Movie, error) {
	var m Movie
	if err := c.get(ctx, "/movie/"+strconv.Itoa(id), url.Values{"append_to_response": {"release_dates"}}, &m); err != nil {
		return nil, err
	}
	if !latin(m.Title) {
		var en Result
		if err := c.get(ctx, "/movie/"+strconv.Itoa(id), url.Values{"language": {"en-US"}}, &en); err == nil && latin(en.Title) {
			m.Title = en.Title
		}
	}
	if m.Title == "" {
		m.Title = m.OriginalTitle
	}
	return &m, nil
}

// Upcoming is a page of the films coming to cinemas in a region ("HR"), and the number of pages.
func (c *Client) Upcoming(ctx context.Context, region string, page int) ([]Result, int, error) {
	var x struct {
		Results    []Result `json:"results"`
		TotalPages int      `json:"total_pages"`
	}
	err := c.get(ctx, "/movie/upcoming", url.Values{"region": {region}, "page": {strconv.Itoa(page)}}, &x)
	return x.Results, x.TotalPages, err
}
