// Package store holds every Postgres query and the migrations.
package store

import (
	"context"
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

//go:embed migrations/*.sql
var migrations embed.FS

type Store struct{ db *pgxpool.Pool }

func Open(ctx context.Context, url string) (*Store, error) {
	db, err := pgxpool.New(ctx, url)
	if err != nil {
		return nil, err
	}
	if err := db.Ping(ctx); err != nil {
		db.Close()
		return nil, fmt.Errorf("database: %w", err)
	}
	return &Store{db: db}, nil
}

func (s *Store) Close() { s.db.Close() }

// Migrate applies every migration that has not run yet, each in its own transaction.
func (s *Store) Migrate(ctx context.Context) ([]string, error) {
	if _, err := s.db.Exec(ctx, `CREATE TABLE IF NOT EXISTS schema_migrations (
		version int PRIMARY KEY, name text NOT NULL, applied_at timestamptz NOT NULL DEFAULT now())`); err != nil {
		return nil, err
	}
	names, err := fs.Glob(migrations, "migrations/*.sql")
	if err != nil {
		return nil, err
	}
	sort.Strings(names)
	var applied []string
	for _, name := range names {
		base := strings.TrimPrefix(name, "migrations/")
		v, err := strconv.Atoi(strings.SplitN(base, "_", 2)[0])
		if err != nil {
			return applied, fmt.Errorf("migration %s: name must start with a number", base)
		}
		var done bool
		if err := s.db.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM schema_migrations WHERE version = $1)`, v).Scan(&done); err != nil {
			return applied, err
		}
		if done {
			continue
		}
		sql, _ := migrations.ReadFile(name)
		err = pgx.BeginFunc(ctx, s.db, func(tx pgx.Tx) error {
			if _, err := tx.Exec(ctx, string(sql)); err != nil {
				return err
			}
			_, err := tx.Exec(ctx, `INSERT INTO schema_migrations (version, name) VALUES ($1, $2)`, v, base)
			return err
		})
		if err != nil {
			return applied, fmt.Errorf("migration %s: %w", base, err)
		}
		applied = append(applied, base)
	}
	return applied, nil
}

// ---------- categories and sources ----------

type Category struct {
	ID          int64
	Slug        string
	Title       string
	Flag        string // media hash, may be empty
	Position    int
	Layout      string
	LeadPicture bool
	InMenu      bool
	Enabled     bool
}

type Source struct {
	ID          int64
	CategoryID  int64
	Name        string
	Logo        string // media hash, may be empty
	Kind        string
	Settings    map[string]string
	PerEdition  int
	Position    int
	Enabled     bool
	LastFetchAt *time.Time
	LastOK      *bool
	LastError   string
	LastCount   int
}

func (s *Store) Categories(ctx context.Context) ([]Category, error) {
	rows, err := s.db.Query(ctx, `SELECT id, slug, title, coalesce(flag, ''), position, layout, lead_picture, in_menu, enabled
		FROM categories ORDER BY position, id`)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(r pgx.CollectableRow) (Category, error) {
		var c Category
		err := r.Scan(&c.ID, &c.Slug, &c.Title, &c.Flag, &c.Position, &c.Layout, &c.LeadPicture, &c.InMenu, &c.Enabled)
		return c, err
	})
}

func (s *Store) Sources(ctx context.Context) ([]Source, error) {
	rows, err := s.db.Query(ctx, `SELECT id, coalesce(category_id, 0), name, coalesce(logo, ''), kind, settings, per_edition, position,
		enabled, last_fetch_at, last_ok, last_error, last_count FROM sources ORDER BY position, id`)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(r pgx.CollectableRow) (Source, error) {
		var src Source
		var settings []byte
		err := r.Scan(&src.ID, &src.CategoryID, &src.Name, &src.Logo, &src.Kind, &settings, &src.PerEdition, &src.Position,
			&src.Enabled, &src.LastFetchAt, &src.LastOK, &src.LastError, &src.LastCount)
		if err == nil {
			err = json.Unmarshal(settings, &src.Settings)
		}
		return src, err
	})
}

func (s *Store) AddCategory(ctx context.Context, c Category) (int64, error) {
	var id int64
	err := s.db.QueryRow(ctx, `INSERT INTO categories (slug, title, flag, position, layout, lead_picture, in_menu, enabled)
		VALUES ($1, $2, nullif($3, ''), $4, $5, $6, $7, $8) RETURNING id`,
		c.Slug, c.Title, c.Flag, c.Position, c.Layout, c.LeadPicture, c.InMenu, c.Enabled).Scan(&id)
	return id, err
}

func (s *Store) AddSource(ctx context.Context, src Source) (int64, error) {
	settings, _ := json.Marshal(src.Settings)
	var id int64
	err := s.db.QueryRow(ctx, `INSERT INTO sources (category_id, name, logo, kind, settings, per_edition, position, enabled)
		VALUES (nullif($1, 0), $2, nullif($3, ''), $4, $5, $6, $7, $8) RETURNING id`,
		src.CategoryID, src.Name, src.Logo, src.Kind, settings, src.PerEdition, src.Position, src.Enabled).Scan(&id)
	return id, err
}

// FetchResult records how a source's last fetch went; it feeds the health board.
func (s *Store) FetchResult(ctx context.Context, sourceID int64, count int, fetchErr error) error {
	msg := ""
	if fetchErr != nil {
		msg = fetchErr.Error()
	}
	_, err := s.db.Exec(ctx, `UPDATE sources SET last_fetch_at = now(), last_ok = $2, last_error = $3, last_count = $4 WHERE id = $1`,
		sourceID, fetchErr == nil, msg, count)
	return err
}

// ---------- items ----------

type Item struct {
	ID          int64
	SourceID    int64
	GUID        string
	Title       string
	Summary     string
	Link        string
	Author      string
	PublishedAt *time.Time
	FetchedAt   time.Time
	PictureURL  string
	Picture     string // media hash once saved
	Extra       map[string]string
	Hidden      bool
}

// SaveItems inserts new items and refreshes known ones; a hand-hidden item stays hidden.
// It returns the ids in the order given.
func (s *Store) SaveItems(ctx context.Context, sourceID int64, items []Item) ([]int64, error) {
	ids := make([]int64, 0, len(items))
	err := pgx.BeginFunc(ctx, s.db, func(tx pgx.Tx) error {
		for _, it := range items {
			extra, _ := json.Marshal(it.Extra)
			var id int64
			err := tx.QueryRow(ctx, `INSERT INTO items (source_id, guid, title, summary, link, author, published_at, picture_url, extra)
				VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
				ON CONFLICT (source_id, guid) DO UPDATE SET title = excluded.title, summary = excluded.summary,
					link = excluded.link, author = excluded.author, picture_url = excluded.picture_url, extra = excluded.extra,
					published_at = coalesce(excluded.published_at, items.published_at), fetched_at = now()
				RETURNING id`,
				sourceID, it.GUID, it.Title, it.Summary, it.Link, it.Author, it.PublishedAt, it.PictureURL, extra).Scan(&id)
			if err != nil {
				return fmt.Errorf("item %q: %w", it.GUID, err)
			}
			ids = append(ids, id)
		}
		return nil
	})
	return ids, err
}

// ItemsNeedingPicture are the newest items of a source that have a picture URL but no saved picture yet.
func (s *Store) ItemsNeedingPicture(ctx context.Context, sourceID int64, limit int) ([]Item, error) {
	rows, err := s.db.Query(ctx, `SELECT id, picture_url FROM items
		WHERE source_id = $1 AND picture_url <> '' AND picture IS NULL AND NOT hidden
		ORDER BY published_at DESC NULLS LAST, id DESC LIMIT $2`, sourceID, limit)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(r pgx.CollectableRow) (Item, error) {
		var it Item
		err := r.Scan(&it.ID, &it.PictureURL)
		return it, err
	})
}

func (s *Store) SetItemPicture(ctx context.Context, itemID int64, hash string) error {
	_, err := s.db.Exec(ctx, `UPDATE items SET picture = $2 WHERE id = $1`, itemID, hash)
	return err
}

// RecentItems gives a source's newest visible items fetched since a time, newest first.
func (s *Store) RecentItems(ctx context.Context, sourceID int64, since time.Time, limit int) ([]Item, error) {
	rows, err := s.db.Query(ctx, `SELECT id, source_id, guid, title, summary, link, author, published_at, fetched_at,
			picture_url, coalesce(picture, ''), extra, hidden
		FROM items WHERE source_id = $1 AND NOT hidden AND fetched_at >= $2
		ORDER BY published_at DESC NULLS LAST, id DESC LIMIT $3`, sourceID, since, limit)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, scanItem)
}

func scanItem(r pgx.CollectableRow) (Item, error) {
	var it Item
	var extra []byte
	err := r.Scan(&it.ID, &it.SourceID, &it.GUID, &it.Title, &it.Summary, &it.Link, &it.Author, &it.PublishedAt, &it.FetchedAt,
		&it.PictureURL, &it.Picture, &extra, &it.Hidden)
	if err == nil {
		err = json.Unmarshal(extra, &it.Extra)
	}
	return it, err
}

// LastFetch is when any item was last fetched, zero if never.
func (s *Store) LastFetch(ctx context.Context) (time.Time, error) {
	var t *time.Time
	err := s.db.QueryRow(ctx, `SELECT max(fetched_at) FROM items`).Scan(&t)
	if err != nil || t == nil {
		return time.Time{}, err
	}
	return *t, nil
}

// ---------- media ----------

type Media struct {
	Hash      string
	Ext       string
	Width     int
	Height    int
	Bytes     int
	SourceURL string
}

func (s *Store) SaveMedia(ctx context.Context, m Media) error {
	_, err := s.db.Exec(ctx, `INSERT INTO media (hash, ext, width, height, bytes, source_url) VALUES ($1, $2, $3, $4, $5, $6)
		ON CONFLICT (hash) DO NOTHING`, m.Hash, m.Ext, m.Width, m.Height, m.Bytes, m.SourceURL)
	return err
}

func (s *Store) MediaByHash(ctx context.Context, hash string) (Media, error) {
	var m Media
	err := s.db.QueryRow(ctx, `SELECT hash, ext, coalesce(width, 0), coalesce(height, 0), bytes, source_url FROM media WHERE hash = $1`, hash).
		Scan(&m.Hash, &m.Ext, &m.Width, &m.Height, &m.Bytes, &m.SourceURL)
	if errors.Is(err, pgx.ErrNoRows) {
		return m, ErrNotFound
	}
	return m, err
}

// ---------- editions ----------

var ErrNotFound = errors.New("not found")

type Edition struct {
	ID        int64
	Day       time.Time
	Number    int
	Version   int
	Reason    string
	Snapshot  json.RawMessage
	HTML      string
	Bytes     int
	PrintedAt time.Time
}

// NextNumbers gives the Br. and Verzija the next print of a day would get.
// Br. counts days from the first printed day (that day is Br. 1), so a day keeps its number across reprints.
func (s *Store) NextNumbers(ctx context.Context, day time.Time) (number, version int, err error) {
	var first *time.Time
	var maxVersion int
	err = s.db.QueryRow(ctx, `SELECT (SELECT min(day) FROM editions), coalesce((SELECT max(version) FROM editions WHERE day = $1), 0)`, day).
		Scan(&first, &maxVersion)
	if err != nil {
		return 0, 0, err
	}
	number = 1
	if first != nil {
		number = DaysBetween(*first, day) + 1
	}
	return number, maxVersion + 1, nil
}

// DaysBetween counts calendar days from a to b, by date alone.
func DaysBetween(a, b time.Time) int {
	da := time.Date(a.Year(), a.Month(), a.Day(), 0, 0, 0, 0, time.UTC)
	db := time.Date(b.Year(), b.Month(), b.Day(), 0, 0, 0, 0, time.UTC)
	return int(db.Sub(da).Hours() / 24)
}

func (s *Store) SaveEdition(ctx context.Context, e Edition) (int64, error) {
	var id int64
	err := s.db.QueryRow(ctx, `INSERT INTO editions (day, number, version, reason, snapshot, html, bytes)
		VALUES ($1, $2, $3, $4, $5, $6, $7) RETURNING id`,
		e.Day, e.Number, e.Version, e.Reason, e.Snapshot, e.HTML, e.Bytes).Scan(&id)
	return id, err
}

// EditionInfo is an edition without its HTML and snapshot, for lists.
type EditionInfo struct {
	Day       time.Time
	Number    int
	Version   int
	Reason    string
	Bytes     int
	PrintedAt time.Time
}

func (s *Store) Editions(ctx context.Context) ([]EditionInfo, error) {
	rows, err := s.db.Query(ctx, `SELECT day, number, version, reason, bytes, printed_at FROM editions ORDER BY day DESC, version DESC`)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(r pgx.CollectableRow) (EditionInfo, error) {
		var e EditionInfo
		err := r.Scan(&e.Day, &e.Number, &e.Version, &e.Reason, &e.Bytes, &e.PrintedAt)
		return e, err
	})
}

// HasEdition tells whether a day has been printed at all.
func (s *Store) HasEdition(ctx context.Context, day time.Time) (bool, error) {
	var ok bool
	err := s.db.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM editions WHERE day = $1)`, day).Scan(&ok)
	return ok, err
}
