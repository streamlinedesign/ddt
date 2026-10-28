package store

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

// ---------- films ----------

// Film is a film as TMDB has it.
type Film struct {
	TMDBID        int
	Title         string
	OriginalTitle string
	Year          int
	Runtime       int // minutes
	Genres        string
	Overview      string
	Poster        string // media hash
	ReleaseHR     *time.Time
}

// SaveFilm adds a film or brings it up to date. A poster already saved stays unless a new one is given.
func (s *Store) SaveFilm(ctx context.Context, f Film) error {
	_, err := s.db.Exec(ctx, `INSERT INTO films (tmdb_id, title, original_title, year, runtime, genres, overview, poster, release_hr)
		VALUES ($1, $2, $3, nullif($4, 0), nullif($5, 0), $6, $7, nullif($8, ''), $9)
		ON CONFLICT (tmdb_id) DO UPDATE SET title = excluded.title, original_title = excluded.original_title,
			year = excluded.year, runtime = excluded.runtime, genres = excluded.genres, overview = excluded.overview,
			poster = coalesce(excluded.poster, films.poster), release_hr = excluded.release_hr, updated_at = now()`,
		f.TMDBID, f.Title, f.OriginalTitle, f.Year, f.Runtime, f.Genres, f.Overview, f.Poster, f.ReleaseHR)
	return err
}

const filmCols = `f.tmdb_id, f.title, f.original_title, coalesce(f.year, 0), coalesce(f.runtime, 0), f.genres, f.overview,
	coalesce(f.poster, ''), f.release_hr`

func scanFilm(r pgx.Row, extra ...any) (Film, error) {
	var f Film
	err := r.Scan(append([]any{&f.TMDBID, &f.Title, &f.OriginalTitle, &f.Year, &f.Runtime, &f.Genres, &f.Overview, &f.Poster,
		&f.ReleaseHR}, extra...)...)
	if errors.Is(err, pgx.ErrNoRows) {
		return f, ErrNotFound
	}
	return f, err
}

func (s *Store) Film(ctx context.Context, tmdbID int) (Film, error) {
	return scanFilm(s.db.QueryRow(ctx, `SELECT `+filmCols+` FROM films f WHERE f.tmdb_id = $1`, tmdbID))
}

// FilmPoster tells whether a film's poster is already saved, so it is not downloaded twice.
func (s *Store) FilmPoster(ctx context.Context, tmdbID int) (string, error) {
	var hash string
	err := s.db.QueryRow(ctx, `SELECT coalesce(poster, '') FROM films WHERE tmdb_id = $1`, tmdbID).Scan(&hash)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", nil
	}
	return hash, err
}

// Release is a film coming to Croatian cinemas on a day.
type Release struct {
	Film
	Day    time.Time
	Hidden bool
}

// SaveRelease puts a film on the list of releases, keeping it hidden if the admin hid it.
func (s *Store) SaveRelease(ctx context.Context, tmdbID int, day time.Time) error {
	_, err := s.db.Exec(ctx, `INSERT INTO releases (tmdb_id, day) VALUES ($1, $2)
		ON CONFLICT (tmdb_id) DO UPDATE SET day = excluded.day`, tmdbID, day)
	return err
}

// Releases lists the films coming out between two days, hidden ones too, by day.
func (s *Store) Releases(ctx context.Context, from, to time.Time) ([]Release, error) {
	rows, err := s.db.Query(ctx, `SELECT `+filmCols+`, r.day, r.hidden FROM releases r JOIN films f USING (tmdb_id)
		WHERE r.day BETWEEN $1 AND $2 ORDER BY r.day, f.title`, from, to)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(r pgx.CollectableRow) (Release, error) {
		var x Release
		var err error
		x.Film, err = scanFilm(r, &x.Day, &x.Hidden)
		return x, err
	})
}

func (s *Store) SetReleaseHidden(ctx context.Context, tmdbID int, hidden bool) error {
	tag, err := s.db.Exec(ctx, `UPDATE releases SET hidden = $2 WHERE tmdb_id = $1`, tmdbID, hidden)
	if err == nil && tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return err
}

// FilmPicks are the admin's films of a cinema week, in order.
func (s *Store) FilmPicks(ctx context.Context, week time.Time) ([]Film, error) {
	rows, err := s.db.Query(ctx, `SELECT `+filmCols+` FROM film_picks p JOIN films f USING (tmdb_id)
		WHERE p.week = $1 ORDER BY p.position, f.title`, week)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(r pgx.CollectableRow) (Film, error) { return scanFilm(r) })
}

// AddFilmPick puts a film last among the week's picks; a film picked already stays where it is.
func (s *Store) AddFilmPick(ctx context.Context, week time.Time, tmdbID int) error {
	_, err := s.db.Exec(ctx, `INSERT INTO film_picks (week, tmdb_id, position)
		VALUES ($1, $2, (SELECT coalesce(max(position), 0) + 10 FROM film_picks WHERE week = $1))
		ON CONFLICT DO NOTHING`, week, tmdbID)
	return err
}

func (s *Store) RemoveFilmPick(ctx context.Context, week time.Time, tmdbID int) error {
	_, err := s.db.Exec(ctx, `DELETE FROM film_picks WHERE week = $1 AND tmdb_id = $2`, week, tmdbID)
	return err
}

// ---------- theatres ----------

// Show is one performance on a theatre's schedule.
type Show struct {
	ID      int64
	Source  string
	Theatre string
	Day     time.Time
	Time    string
	Title   string
	Link    string
	Hidden  bool
}

func showKey(day time.Time, at, title string) string {
	return day.Format("2006-01-02") + "|" + at + "|" + strings.ToLower(title)
}

// ReplaceShows puts a theatre's shows from a day on in place of what its last fetch found.
// A show the admin hid stays hidden. Days before from are left as they are.
func (s *Store) ReplaceShows(ctx context.Context, source string, from time.Time, shows []Show) error {
	return pgx.BeginFunc(ctx, s.db, func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx, `SELECT day, time, title FROM shows WHERE source = $1 AND day >= $2 AND hidden`, source, from)
		if err != nil {
			return err
		}
		hidden := map[string]bool{}
		for rows.Next() {
			var day time.Time
			var at, title string
			if err := rows.Scan(&day, &at, &title); err != nil {
				return err
			}
			hidden[showKey(day, at, title)] = true
		}
		if err := rows.Err(); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `DELETE FROM shows WHERE source = $1 AND day >= $2`, source, from); err != nil {
			return err
		}
		for _, x := range shows {
			if x.Day.Before(from) {
				continue
			}
			_, err := tx.Exec(ctx, `INSERT INTO shows (source, theatre, day, time, title, link, hidden)
				VALUES ($1, $2, $3, $4, $5, $6, $7) ON CONFLICT DO NOTHING`,
				source, x.Theatre, x.Day, x.Time, x.Title, x.Link, hidden[showKey(x.Day, x.Time, x.Title)])
			if err != nil {
				return err
			}
		}
		return nil
	})
}

// Shows lists every show between two days, hidden ones too, by day, time and theatre.
func (s *Store) Shows(ctx context.Context, from, to time.Time) ([]Show, error) {
	rows, err := s.db.Query(ctx, `SELECT id, source, theatre, day, time, title, link, hidden FROM shows
		WHERE day BETWEEN $1 AND $2 ORDER BY day, time = '', time, theatre, title`, from, to)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(r pgx.CollectableRow) (Show, error) {
		var x Show
		err := r.Scan(&x.ID, &x.Source, &x.Theatre, &x.Day, &x.Time, &x.Title, &x.Link, &x.Hidden)
		return x, err
	})
}

func (s *Store) SetShowHidden(ctx context.Context, id int64, hidden bool) error {
	tag, err := s.db.Exec(ctx, `UPDATE shows SET hidden = $2 WHERE id = $1`, id, hidden)
	if err == nil && tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return err
}

// ---------- the events wall ----------

// Event is a poster on the wall: something on from one day to another.
type Event struct {
	ID          int64
	Title       string
	Venue       string
	Description string
	Price       string
	Link        string
	Start, End  time.Time
	Time        string
	Poster      string // media hash
}

const eventCols = `id, title, venue, description, price, link, start_day, end_day, time, coalesce(poster, '')`

func scanEvent(r pgx.Row) (Event, error) {
	var e Event
	err := r.Scan(&e.ID, &e.Title, &e.Venue, &e.Description, &e.Price, &e.Link, &e.Start, &e.End, &e.Time, &e.Poster)
	if errors.Is(err, pgx.ErrNoRows) {
		return e, ErrNotFound
	}
	return e, err
}

// Events lists the events not yet over on a day, the soonest first.
func (s *Store) Events(ctx context.Context, from time.Time) ([]Event, error) {
	rows, err := s.db.Query(ctx, `SELECT `+eventCols+` FROM events WHERE end_day >= $1 ORDER BY start_day, end_day, title`, from)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(r pgx.CollectableRow) (Event, error) { return scanEvent(r) })
}

// PastEvents lists the latest few events that are over.
func (s *Store) PastEvents(ctx context.Context, before time.Time, limit int) ([]Event, error) {
	rows, err := s.db.Query(ctx, `SELECT `+eventCols+` FROM events WHERE end_day < $1 ORDER BY end_day DESC LIMIT $2`, before, limit)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(r pgx.CollectableRow) (Event, error) { return scanEvent(r) })
}

// EventsBetween lists the events on at any time between two days: on now first, then by start.
func (s *Store) EventsBetween(ctx context.Context, from, to time.Time) ([]Event, error) {
	rows, err := s.db.Query(ctx, `SELECT `+eventCols+` FROM events WHERE end_day >= $1 AND start_day <= $2
		ORDER BY greatest(start_day, $1), end_day, title`, from, to)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(r pgx.CollectableRow) (Event, error) { return scanEvent(r) })
}

func (s *Store) Event(ctx context.Context, id int64) (Event, error) {
	return scanEvent(s.db.QueryRow(ctx, `SELECT `+eventCols+` FROM events WHERE id = $1`, id))
}

// SaveEvent adds an event, or changes it when it has an id.
func (s *Store) SaveEvent(ctx context.Context, e Event) (int64, error) {
	if e.ID == 0 {
		err := s.db.QueryRow(ctx, `INSERT INTO events (title, venue, description, price, link, start_day, end_day, time, poster)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, nullif($9, '')) RETURNING id`,
			e.Title, e.Venue, e.Description, e.Price, e.Link, e.Start, e.End, e.Time, e.Poster).Scan(&e.ID)
		return e.ID, err
	}
	tag, err := s.db.Exec(ctx, `UPDATE events SET title = $2, venue = $3, description = $4, price = $5, link = $6,
		start_day = $7, end_day = $8, time = $9, poster = nullif($10, ''), updated_at = now() WHERE id = $1`,
		e.ID, e.Title, e.Venue, e.Description, e.Price, e.Link, e.Start, e.End, e.Time, e.Poster)
	if err == nil && tag.RowsAffected() == 0 {
		return e.ID, ErrNotFound
	}
	return e.ID, err
}

func (s *Store) DeleteEvent(ctx context.Context, id int64) error {
	_, err := s.db.Exec(ctx, `DELETE FROM events WHERE id = $1`, id)
	return err
}

// ---------- Billboard ----------

// ChartEntry is one place on a chart.
type ChartEntry struct {
	Rank   int
	Title  string
	Artist string
	Last   int // last week's place; 0 when new
	Peak   int
	Weeks  int
}

// SaveChart keeps a chart's week, in place of an earlier copy of the same week.
func (s *Store) SaveChart(ctx context.Context, chart string, week time.Time, top []ChartEntry) error {
	return pgx.BeginFunc(ctx, s.db, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `DELETE FROM charts WHERE chart = $1 AND week = $2`, chart, week); err != nil {
			return err
		}
		for _, e := range top {
			if _, err := tx.Exec(ctx, `INSERT INTO charts (chart, week, rank, title, artist, last, peak, weeks)
				VALUES ($1, $2, $3, $4, $5, $6, $7, $8)`, chart, week, e.Rank, e.Title, e.Artist, e.Last, e.Peak, e.Weeks); err != nil {
				return err
			}
		}
		return nil
	})
}

// LatestChart is a chart's newest week on or before a day. The week is zero when there is none.
func (s *Store) LatestChart(ctx context.Context, chart string, day time.Time) (time.Time, []ChartEntry, error) {
	var at *time.Time // max over no rows is NULL
	if err := s.db.QueryRow(ctx, `SELECT max(week) FROM charts WHERE chart = $1 AND week <= $2`, chart, day).Scan(&at); err != nil || at == nil {
		return time.Time{}, nil, err
	}
	week := *at
	rows, err := s.db.Query(ctx, `SELECT rank, title, artist, last, peak, weeks FROM charts WHERE chart = $1 AND week = $2 ORDER BY rank`,
		chart, week)
	if err != nil {
		return week, nil, err
	}
	top, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (ChartEntry, error) {
		var e ChartEntry
		err := r.Scan(&e.Rank, &e.Title, &e.Artist, &e.Last, &e.Peak, &e.Weeks)
		return e, err
	})
	return week, top, err
}
