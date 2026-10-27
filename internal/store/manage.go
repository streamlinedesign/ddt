package store

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// ErrTaken means a slug or a name is already used by another row.
var ErrTaken = errors.New("already taken")

// ErrInUse means a row cannot go while others still point at it.
var ErrInUse = errors.New("still in use")

func taken(err error) error {
	var pg *pgconn.PgError
	if errors.As(err, &pg) && pg.Code == "23505" {
		return ErrTaken
	}
	return err
}

// ---------- categories ----------

func (s *Store) Category(ctx context.Context, id int64) (Category, error) {
	var c Category
	err := s.db.QueryRow(ctx, `SELECT id, slug, title, coalesce(flag, ''), position, layout, lead_picture, in_menu, enabled
		FROM categories WHERE id = $1`, id).
		Scan(&c.ID, &c.Slug, &c.Title, &c.Flag, &c.Position, &c.Layout, &c.LeadPicture, &c.InMenu, &c.Enabled)
	if errors.Is(err, pgx.ErrNoRows) {
		return c, ErrNotFound
	}
	return c, err
}

// NewCategory adds a category at the end of the list.
func (s *Store) NewCategory(ctx context.Context, c Category) (int64, error) {
	if err := s.db.QueryRow(ctx, `SELECT coalesce(max(position), 0) + 10 FROM categories`).Scan(&c.Position); err != nil {
		return 0, err
	}
	id, err := s.AddCategory(ctx, c)
	return id, taken(err)
}

func (s *Store) UpdateCategory(ctx context.Context, c Category) error {
	tag, err := s.db.Exec(ctx, `UPDATE categories SET slug = $2, title = $3, flag = nullif($4, ''), layout = $5,
		lead_picture = $6, in_menu = $7, enabled = $8 WHERE id = $1`,
		c.ID, c.Slug, c.Title, c.Flag, c.Layout, c.LeadPicture, c.InMenu, c.Enabled)
	if err == nil && tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return taken(err)
}

// DeleteCategory removes an empty category. One that still has sources is refused.
func (s *Store) DeleteCategory(ctx context.Context, id int64) error {
	var n int
	if err := s.db.QueryRow(ctx, `SELECT count(*) FROM sources WHERE category_id = $1`, id).Scan(&n); err != nil {
		return err
	}
	if n > 0 {
		return ErrInUse
	}
	_, err := s.db.Exec(ctx, `DELETE FROM categories WHERE id = $1`, id)
	return err
}

// MoveCategory moves a category one place up (by -1) or down (by 1).
func (s *Store) MoveCategory(ctx context.Context, id int64, by int) error {
	return s.move(ctx, `SELECT id FROM categories ORDER BY position, id`, nil, `UPDATE categories SET position = $2 WHERE id = $1`, id, by)
}

// move swaps a row with its neighbour and numbers the whole list again in tens.
func (s *Store) move(ctx context.Context, list string, args []any, set string, id int64, by int) error {
	return pgx.BeginFunc(ctx, s.db, func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx, list, args...)
		if err != nil {
			return err
		}
		ids, err := pgx.CollectRows(rows, pgx.RowTo[int64])
		if err != nil {
			return err
		}
		at := -1
		for i, v := range ids {
			if v == id {
				at = i
			}
		}
		if at < 0 {
			return ErrNotFound
		}
		to := at + by
		if to < 0 || to >= len(ids) {
			return nil // already at the end
		}
		ids[at], ids[to] = ids[to], ids[at]
		for i, v := range ids {
			if _, err := tx.Exec(ctx, set, v, (i+1)*10); err != nil {
				return err
			}
		}
		return nil
	})
}

// ---------- sources ----------

func (s *Store) Source(ctx context.Context, id int64) (Source, error) {
	all, err := s.Sources(ctx)
	if err != nil {
		return Source{}, err
	}
	for _, src := range all {
		if src.ID == id {
			return src, nil
		}
	}
	return Source{}, ErrNotFound
}

// NewSource adds a source at the end of its category.
func (s *Store) NewSource(ctx context.Context, src Source) (int64, error) {
	if err := s.db.QueryRow(ctx, `SELECT coalesce(max(position), 0) + 10 FROM sources WHERE category_id = $1`,
		src.CategoryID).Scan(&src.Position); err != nil {
		return 0, err
	}
	id, err := s.AddSource(ctx, src)
	return id, taken(err)
}

// UpdateSource saves a source's settings. A source moved to another category goes to its end.
func (s *Store) UpdateSource(ctx context.Context, src Source) error {
	settings, _ := json.Marshal(src.Settings)
	tag, err := s.db.Exec(ctx, `UPDATE sources SET
			position = CASE WHEN category_id IS DISTINCT FROM nullif($2, 0)
				THEN (SELECT coalesce(max(position), 0) + 10 FROM sources WHERE category_id = nullif($2, 0)) ELSE position END,
			category_id = nullif($2, 0), name = $3, logo = nullif($4, ''), kind = $5, settings = $6, per_edition = $7, enabled = $8
		WHERE id = $1`,
		src.ID, src.CategoryID, src.Name, src.Logo, src.Kind, settings, src.PerEdition, src.Enabled)
	if err == nil && tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return taken(err)
}

// DeleteSource removes a source and its items. Printed editions keep what they showed.
func (s *Store) DeleteSource(ctx context.Context, id int64) error {
	_, err := s.db.Exec(ctx, `DELETE FROM sources WHERE id = $1`, id)
	return err
}

// MoveSource moves a source one place up or down within its category.
func (s *Store) MoveSource(ctx context.Context, id int64, by int) error {
	src, err := s.Source(ctx, id)
	if err != nil {
		return err
	}
	return s.move(ctx, `SELECT id FROM sources WHERE category_id IS NOT DISTINCT FROM nullif($1, 0) ORDER BY position, id`,
		[]any{src.CategoryID}, `UPDATE sources SET position = $2 WHERE id = $1`, id, by)
}

// ---------- hiding items ----------

// SetItemHidden keeps an item out of every print from now on, or lets it back in.
func (s *Store) SetItemHidden(ctx context.Context, id int64, hidden bool) error {
	tag, err := s.db.Exec(ctx, `UPDATE items SET hidden = $2 WHERE id = $1`, id, hidden)
	if err == nil && tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return err
}

// HiddenItem is a hidden item with its source's name, for the list of what was taken out.
type HiddenItem struct {
	ID          int64
	Title       string
	Link        string
	Source      string
	PublishedAt *time.Time
}

// HiddenItems lists items hidden among those fetched since a time, newest first.
func (s *Store) HiddenItems(ctx context.Context, since time.Time) ([]HiddenItem, error) {
	rows, err := s.db.Query(ctx, `SELECT i.id, i.title, i.link, s.name, i.published_at
		FROM items i JOIN sources s ON s.id = i.source_id
		WHERE i.hidden AND i.fetched_at >= $1 ORDER BY i.published_at DESC NULLS LAST, i.id DESC`, since)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(r pgx.CollectableRow) (HiddenItem, error) {
		var h HiddenItem
		err := r.Scan(&h.ID, &h.Title, &h.Link, &h.Source, &h.PublishedAt)
		return h, err
	})
}
