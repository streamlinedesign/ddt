package fetch

import (
	"context"
	"encoding/csv"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"

	"tisak/internal/store"
	"tisak/internal/theatre"
	"tisak/internal/tmdb"
)

func init() {
	RegisterKeeper("kazaliste", Theatre{})
	RegisterKeeper("tmdb", Films{})
	RegisterKeeper("billboard", Charts{})
}

// ---------- theatres ----------

// Theatre reads one Zagreb theatre's schedule and keeps its shows for the next four weeks.
// Settings: site, the theatre's name in the theatre package (komedija, gavella…).
type Theatre struct{}

func (Theatre) Keep(ctx context.Context, k Kit, settings map[string]string) (int, error) {
	site, ok := theatre.Lookup(settings["site"])
	if !ok {
		return 0, fmt.Errorf("unknown theatre %q: the site setting names one of the theatre package's sites", settings["site"])
	}
	get := func(ctx context.Context, raw string, form url.Values) ([]byte, error) {
		if form == nil {
			return k.Client.Get(ctx, raw)
		}
		return k.Client.Post(ctx, raw, form)
	}
	day := k.Day()
	shows, err := theatre.Read(ctx, site, get, day)
	if err != nil {
		return 0, err
	}
	if err := k.Store.ReplaceShows(ctx, site.Source, day, shows); err != nil {
		return 0, err
	}
	return len(shows), nil
}

// ---------- films ----------

// Films reads the films coming to Croatian cinemas from TMDB: its upcoming list for HR, each film's own
// Croatian date (the list's date is the first release anywhere), its poster saved once.
// The key comes from the environment only. No settings.
type Films struct{}

// tmdbClient makes the client; tests point it at their own server.
var tmdbClient = tmdb.FromEnv

// The releases kept: a week back, still new in cinemas, to ten weeks ahead.
const (
	releasesBack  = 7
	releasesAhead = 70
	releasesPages = 3
)

func (Films) Keep(ctx context.Context, k Kit, _ map[string]string) (int, error) {
	c := tmdbClient()
	if c.Key == "" {
		return 0, tmdb.ErrNoKey
	}
	day := k.Day()
	from, to := day.AddDate(0, 0, -releasesBack), day.AddDate(0, 0, releasesAhead)
	kept := 0
	var failed []error
	done := map[int]bool{}
	for page, pages := 1, 1; page <= pages && page <= releasesPages; page++ {
		list, n, err := c.Upcoming(ctx, "HR", page)
		if err != nil {
			return kept, err
		}
		pages = n
		for _, r := range list {
			if done[r.ID] {
				continue // the list moved between pages
			}
			done[r.ID] = true
			m, err := c.Movie(ctx, r.ID)
			if err != nil {
				failed = append(failed, err)
				continue
			}
			date := m.Release("HR")
			if date == nil || date.Before(from) || date.After(to) {
				continue
			}
			if _, err := SaveMovie(ctx, k.Store, k.Pictures, m); err != nil {
				failed = append(failed, err)
				continue
			}
			if err := k.Store.SaveRelease(ctx, m.ID, *date); err != nil {
				return kept, err
			}
			kept++
		}
	}
	// A few films failing is a note, not a failed fetch.
	if kept > 0 && len(failed) < kept {
		return kept, nil
	}
	return kept, errors.Join(failed...)
}

// filmPoster is how wide a poster is kept: a card is narrow, a pick a little wider.
const filmPoster = 500

// SaveMovie keeps a film from its TMDB details, its poster downloaded the first time only.
func SaveMovie(ctx context.Context, st *store.Store, pics PictureSaver, m *tmdb.Movie) (store.Film, error) {
	f := store.Film{TMDBID: m.ID, Title: Clean(m.Title), OriginalTitle: Clean(m.OriginalTitle), Year: m.Year(),
		Runtime: m.Runtime, Genres: m.GenreList(), Overview: strings.TrimSpace(m.Overview), ReleaseHR: m.Release("HR")}
	if m.PosterPath != "" && pics != nil {
		have, err := st.FilmPoster(ctx, m.ID)
		if err != nil {
			return f, err
		}
		if have == "" {
			if hash, err := pics.SaveURL(ctx, tmdb.PosterURL(m.PosterPath, "w500"), filmPoster); err == nil {
				f.Poster = hash
			}
		}
	}
	return f, st.SaveFilm(ctx, f)
}

// ---------- charts ----------

// Charts keeps Billboard's top ten songs (Hot 100) and albums (Billboard 200). Billboard has no open feed,
// so they come from two public data sets on GitHub that copy the charts every week.
// Settings: hot100 and billboard200, the data sets' addresses, when they move.
type Charts struct{}

const (
	Hot100URL       = "https://raw.githubusercontent.com/mhollingshead/billboard-hot-100/main/recent.json"
	Billboard200URL = "https://raw.githubusercontent.com/utdata/rwd-billboard-data/main/data-out/billboard-200-current.csv"
	Hot100          = "hot100"
	Billboard200    = "billboard200"
	// ChartTop is how many places are kept.
	ChartTop = 10
)

func (Charts) Keep(ctx context.Context, k Kit, settings map[string]string) (int, error) {
	kept := 0
	var failed []error
	body, err := k.Client.Get(ctx, first(settings["hot100"], Hot100URL))
	if err == nil {
		var week time.Time
		var top []store.ChartEntry
		if week, top, err = ParseHot100(body); err == nil {
			err = k.Store.SaveChart(ctx, Hot100, week, top)
		}
	}
	if err != nil {
		failed = append(failed, fmt.Errorf("%s: %w", Hot100, err))
	} else {
		kept++
	}

	// The Billboard 200 comes as every week since 1967 in one file of some 35 MB, read as it downloads,
	// and only when a new week can be out: Billboard dates a chart by the Saturday after it comes out.
	week, _, err := k.Store.LatestChart(ctx, Billboard200, k.Day().AddDate(0, 0, 14))
	if err != nil {
		return kept, errors.Join(append(failed, err)...)
	}
	if !week.IsZero() && k.Day().Before(week.AddDate(0, 0, 3)) {
		return kept + 1, errors.Join(failed...) // this week's is kept already
	}
	if err := billboard200(ctx, k, first(settings["billboard200"], Billboard200URL)); err != nil {
		failed = append(failed, fmt.Errorf("%s: %w", Billboard200, err))
	} else {
		kept++
	}
	return kept, errors.Join(failed...)
}

func billboard200(ctx context.Context, k Kit, link string) error {
	body, err := k.Client.Open(ctx, link)
	if err != nil {
		return err
	}
	defer body.Close()
	week, top, err := ParseBillboard200(io.LimitReader(body, 256<<20))
	if err != nil {
		return err
	}
	return k.Store.SaveChart(ctx, Billboard200, week, top)
}

// ParseHot100 reads the Hot 100's latest week: its date and top ten.
func ParseHot100(body []byte) (time.Time, []store.ChartEntry, error) {
	var x struct {
		Date string `json:"date"`
		Data []struct {
			Song     string `json:"song"`
			Artist   string `json:"artist"`
			ThisWeek int    `json:"this_week"`
			LastWeek *int   `json:"last_week"`
			Peak     int    `json:"peak_position"`
			Weeks    int    `json:"weeks_on_chart"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &x); err != nil {
		return time.Time{}, nil, fmt.Errorf("not the Hot 100 data: %w", err)
	}
	week, err := time.Parse(time.DateOnly, x.Date)
	if err != nil {
		return time.Time{}, nil, fmt.Errorf("no week: %q", x.Date)
	}
	var top []store.ChartEntry
	for _, d := range x.Data {
		if d.ThisWeek < 1 || d.ThisWeek > ChartTop || strings.TrimSpace(d.Song) == "" {
			continue
		}
		e := store.ChartEntry{Rank: d.ThisWeek, Title: Clean(d.Song), Artist: Clean(d.Artist), Peak: d.Peak, Weeks: d.Weeks}
		if d.LastWeek != nil {
			e.Last = *d.LastWeek
		}
		top = append(top, e)
	}
	if len(top) == 0 {
		return time.Time{}, nil, errors.New("no songs")
	}
	return week, byRank(top), nil
}

// ParseBillboard200 reads the Billboard 200's CSV, every week in no set order, and keeps the newest week's top ten.
func ParseBillboard200(r io.Reader) (time.Time, []store.ChartEntry, error) {
	c := csv.NewReader(r)
	c.FieldsPerRecord = -1
	c.ReuseRecord = true
	head, err := c.Read()
	if err != nil {
		return time.Time{}, nil, err
	}
	col := map[string]int{}
	for i, h := range head {
		col[strings.TrimSpace(h)] = i
	}
	for _, k := range []string{"chart_week", "current_week", "title", "performer", "last_week", "peak_pos", "wks_on_chart"} {
		if _, ok := col[k]; !ok {
			return time.Time{}, nil, fmt.Errorf("no column %s: has the data set changed?", k)
		}
	}
	num := func(s string) int { n, _ := strconv.Atoi(strings.TrimSpace(s)); return n } // "NA" is 0
	var newest string
	var top []store.ChartEntry
	for {
		rec, err := c.Read()
		if err == io.EOF {
			break
		}
		if err != nil {
			return time.Time{}, nil, err
		}
		if len(rec) < len(head) {
			continue
		}
		week := rec[col["chart_week"]]
		rank := num(rec[col["current_week"]])
		if rank < 1 || rank > ChartTop || week < newest {
			continue
		}
		if week > newest {
			newest, top = week, nil
		}
		top = append(top, store.ChartEntry{Rank: rank, Title: Clean(rec[col["title"]]), Artist: Clean(rec[col["performer"]]),
			Last: num(rec[col["last_week"]]), Peak: num(rec[col["peak_pos"]]), Weeks: num(rec[col["wks_on_chart"]])})
	}
	week, err := time.Parse(time.DateOnly, newest)
	if err != nil || len(top) == 0 {
		return time.Time{}, nil, errors.New("no albums")
	}
	return week, byRank(top), nil
}

func byRank(top []store.ChartEntry) []store.ChartEntry {
	sort.Slice(top, func(i, j int) bool { return top[i].Rank < top[j].Rank })
	return top
}
