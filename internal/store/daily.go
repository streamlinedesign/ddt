package store

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
)

// ---------- articles ----------

// Block is one piece of an article's body: a paragraph, a subheading, a quote or a picture.
type Block struct {
	Type    string `json:"type"` // p, h, quote, img
	Text    string `json:"text,omitempty"`
	Picture string `json:"picture,omitempty"` // media hash, for img
	Caption string `json:"caption,omitempty"` // for img: what is in it and whose it is
}

type Article struct {
	ID            int64
	Title         string
	Description   string
	Author        string
	Picture       string // media hash, may be empty
	PictureAlt    string
	PictureCredit string
	Blocks        []Block
	Day           *time.Time // the day it is the main article
	Pinned        bool       // the main article of every day without its own
	CreatedAt     time.Time
	UpdatedAt     time.Time
}

const articleCols = `id, title, description, author, coalesce(picture, ''), picture_alt, picture_credit, blocks, day, pinned, created_at, updated_at`

func scanArticle(r pgx.Row) (Article, error) {
	var a Article
	var blocks []byte
	err := r.Scan(&a.ID, &a.Title, &a.Description, &a.Author, &a.Picture, &a.PictureAlt, &a.PictureCredit, &blocks, &a.Day,
		&a.Pinned, &a.CreatedAt, &a.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return a, ErrNotFound
	}
	if err == nil {
		err = json.Unmarshal(blocks, &a.Blocks)
	}
	return a, err
}

// Articles lists every article, those with a day first (latest day first), then the rest, newest first.
func (s *Store) Articles(ctx context.Context) ([]Article, error) {
	rows, err := s.db.Query(ctx, `SELECT `+articleCols+` FROM articles ORDER BY day DESC NULLS LAST, pinned DESC, updated_at DESC`)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(r pgx.CollectableRow) (Article, error) { return scanArticle(r) })
}

func (s *Store) Article(ctx context.Context, id int64) (Article, error) {
	return scanArticle(s.db.QueryRow(ctx, `SELECT `+articleCols+` FROM articles WHERE id = $1`, id))
}

// MainArticle is the article chosen for a day, or the pinned one when the day has none.
func (s *Store) MainArticle(ctx context.Context, day time.Time) (Article, error) {
	return scanArticle(s.db.QueryRow(ctx, `SELECT `+articleCols+` FROM articles WHERE day = $1 OR pinned
		ORDER BY day = $1 DESC NULLS LAST LIMIT 1`, day))
}

func (s *Store) NewArticle(ctx context.Context, a Article) (int64, error) {
	blocks, _ := json.Marshal(nonNil(a.Blocks))
	var id int64
	err := s.db.QueryRow(ctx, `INSERT INTO articles (title, description, author, picture, picture_alt, picture_credit, blocks)
		VALUES ($1, $2, $3, nullif($4, ''), $5, $6, $7) RETURNING id`,
		a.Title, a.Description, a.Author, a.Picture, a.PictureAlt, a.PictureCredit, blocks).Scan(&id)
	return id, err
}

// UpdateArticle saves an article's text and pictures. Its day is set apart, with SetArticleDay.
func (s *Store) UpdateArticle(ctx context.Context, a Article) error {
	blocks, _ := json.Marshal(nonNil(a.Blocks))
	tag, err := s.db.Exec(ctx, `UPDATE articles SET title = $2, description = $3, author = $4, picture = nullif($5, ''),
		picture_alt = $6, picture_credit = $7, blocks = $8, updated_at = now() WHERE id = $1`,
		a.ID, a.Title, a.Description, a.Author, a.Picture, a.PictureAlt, a.PictureCredit, blocks)
	if err == nil && tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return err
}

// SetArticleDay makes an article the main article of a day, taking the day from any other
// article that had it. A nil day takes the article off the front page.
func (s *Store) SetArticleDay(ctx context.Context, id int64, day *time.Time) error {
	return pgx.BeginFunc(ctx, s.db, func(tx pgx.Tx) error {
		if day != nil {
			if _, err := tx.Exec(ctx, `UPDATE articles SET day = NULL WHERE day = $1 AND id <> $2`, *day, id); err != nil {
				return err
			}
		}
		tag, err := tx.Exec(ctx, `UPDATE articles SET day = $2 WHERE id = $1`, id, day)
		if err == nil && tag.RowsAffected() == 0 {
			return ErrNotFound
		}
		return err
	})
}

// PinArticle makes an article the main article of every day without its own, in place of any other.
// Unpinning leaves no article pinned.
func (s *Store) PinArticle(ctx context.Context, id int64, pin bool) error {
	return pgx.BeginFunc(ctx, s.db, func(tx pgx.Tx) error {
		if pin {
			if _, err := tx.Exec(ctx, `UPDATE articles SET pinned = false WHERE pinned AND id <> $1`, id); err != nil {
				return err
			}
		}
		tag, err := tx.Exec(ctx, `UPDATE articles SET pinned = $2 WHERE id = $1`, id, pin)
		if err == nil && tag.RowsAffected() == 0 {
			return ErrNotFound
		}
		return err
	})
}

func (s *Store) DeleteArticle(ctx context.Context, id int64) error {
	_, err := s.db.Exec(ctx, `DELETE FROM articles WHERE id = $1`, id)
	return err
}

func nonNil(b []Block) []Block {
	if b == nil {
		return []Block{}
	}
	return b
}

// ---------- the word of the day ----------

type Word struct {
	ID          int64
	Day         time.Time
	Word        string
	Link        string
	Description string
}

const wordCols = `id, day, word, link, description`

func scanWord(r pgx.Row) (Word, error) {
	var w Word
	err := r.Scan(&w.ID, &w.Day, &w.Word, &w.Link, &w.Description)
	if errors.Is(err, pgx.ErrNoRows) {
		return w, ErrNotFound
	}
	return w, err
}

// Words lists the words from a day on, earliest first, then the latest few before it.
func (s *Store) Words(ctx context.Context, from time.Time) ([]Word, error) {
	rows, err := s.db.Query(ctx, `(SELECT `+wordCols+` FROM words WHERE day >= $1 ORDER BY day)
		UNION ALL (SELECT `+wordCols+` FROM words WHERE day < $1 ORDER BY day DESC LIMIT 14)`, from)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(r pgx.CollectableRow) (Word, error) { return scanWord(r) })
}

func (s *Store) Word(ctx context.Context, id int64) (Word, error) {
	return scanWord(s.db.QueryRow(ctx, `SELECT `+wordCols+` FROM words WHERE id = $1`, id))
}

func (s *Store) WordFor(ctx context.Context, day time.Time) (Word, error) {
	return scanWord(s.db.QueryRow(ctx, `SELECT `+wordCols+` FROM words WHERE day = $1`, day))
}

// SaveWord adds a word, or changes it when it has an id. A day can have only one word.
func (s *Store) SaveWord(ctx context.Context, w Word) (int64, error) {
	if w.ID == 0 {
		err := s.db.QueryRow(ctx, `INSERT INTO words (day, word, link, description) VALUES ($1, $2, $3, $4) RETURNING id`,
			w.Day, w.Word, w.Link, w.Description).Scan(&w.ID)
		return w.ID, taken(err)
	}
	tag, err := s.db.Exec(ctx, `UPDATE words SET day = $2, word = $3, link = $4, description = $5 WHERE id = $1`,
		w.ID, w.Day, w.Word, w.Link, w.Description)
	if err == nil && tag.RowsAffected() == 0 {
		return w.ID, ErrNotFound
	}
	return w.ID, taken(err)
}

func (s *Store) DeleteWord(ctx context.Context, id int64) error {
	_, err := s.db.Exec(ctx, `DELETE FROM words WHERE id = $1`, id)
	return err
}

// ---------- the album of the day ----------

type Album struct {
	ID      int64
	Day     time.Time
	Artist  string
	Title   string
	Year    int // 0 when not known
	Genre   string
	Minutes int
	Tracks  int
	Cover   string // media hash
	Link    string
}

const albumCols = `id, day, artist, title, coalesce(year, 0), genre, coalesce(minutes, 0), coalesce(tracks, 0), coalesce(cover, ''), link`

func scanAlbum(r pgx.Row) (Album, error) {
	var a Album
	err := r.Scan(&a.ID, &a.Day, &a.Artist, &a.Title, &a.Year, &a.Genre, &a.Minutes, &a.Tracks, &a.Cover, &a.Link)
	if errors.Is(err, pgx.ErrNoRows) {
		return a, ErrNotFound
	}
	return a, err
}

// Albums lists the albums from a day on, earliest first, then the latest few before it.
func (s *Store) Albums(ctx context.Context, from time.Time) ([]Album, error) {
	rows, err := s.db.Query(ctx, `(SELECT `+albumCols+` FROM albums WHERE day >= $1 ORDER BY day)
		UNION ALL (SELECT `+albumCols+` FROM albums WHERE day < $1 ORDER BY day DESC LIMIT 14)`, from)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(r pgx.CollectableRow) (Album, error) { return scanAlbum(r) })
}

func (s *Store) Album(ctx context.Context, id int64) (Album, error) {
	return scanAlbum(s.db.QueryRow(ctx, `SELECT `+albumCols+` FROM albums WHERE id = $1`, id))
}

func (s *Store) AlbumFor(ctx context.Context, day time.Time) (Album, error) {
	return scanAlbum(s.db.QueryRow(ctx, `SELECT `+albumCols+` FROM albums WHERE day = $1`, day))
}

// SaveAlbum adds an album, or changes it when it has an id. A day can have only one album.
func (s *Store) SaveAlbum(ctx context.Context, a Album) (int64, error) {
	if a.ID == 0 {
		err := s.db.QueryRow(ctx, `INSERT INTO albums (day, artist, title, year, genre, minutes, tracks, cover, link)
			VALUES ($1, $2, $3, nullif($4, 0), $5, nullif($6, 0), nullif($7, 0), nullif($8, ''), $9) RETURNING id`,
			a.Day, a.Artist, a.Title, a.Year, a.Genre, a.Minutes, a.Tracks, a.Cover, a.Link).Scan(&a.ID)
		return a.ID, taken(err)
	}
	tag, err := s.db.Exec(ctx, `UPDATE albums SET day = $2, artist = $3, title = $4, year = nullif($5, 0), genre = $6,
		minutes = nullif($7, 0), tracks = nullif($8, 0), cover = nullif($9, ''), link = $10 WHERE id = $1`,
		a.ID, a.Day, a.Artist, a.Title, a.Year, a.Genre, a.Minutes, a.Tracks, a.Cover, a.Link)
	if err == nil && tag.RowsAffected() == 0 {
		return a.ID, ErrNotFound
	}
	return a.ID, taken(err)
}

func (s *Store) DeleteAlbum(ctx context.Context, id int64) error {
	_, err := s.db.Exec(ctx, `DELETE FROM albums WHERE id = $1`, id)
	return err
}

// ---------- weather ----------

// Forecast is one city's weather for one day, as the press prints it.
type Forecast struct {
	City  string         `json:"city"`
	Order int            `json:"order"` // the city's place in the picker
	Link  string         `json:"link"`  // the forecast's page, for the panel's source link
	Min   *int           `json:"min,omitempty"`
	Max   *int           `json:"max,omitempty"`
	Parts []ForecastPart `json:"parts"`
}

// ForecastPart is the sky and the temperature at one time of day.
type ForecastPart struct {
	Label string `json:"label"` // Ujutro, Poslijepodne, Noć
	Hour  int    `json:"hour"`
	Temp  int    `json:"temp"`
	Icon  string `json:"icon"` // a Meteocons name: clear-day, rain...
	Sky   string `json:"sky"`  // the forecaster's words: Sunčano
}

// SaveWeather keeps one city's forecast for one day, replacing an older fetch.
func (s *Store) SaveWeather(ctx context.Context, day time.Time, f Forecast) error {
	js, err := json.Marshal(f)
	if err != nil {
		return err
	}
	_, err = s.db.Exec(ctx, `INSERT INTO weather (city, day, data) VALUES ($1, $2, $3)
		ON CONFLICT (city, day) DO UPDATE SET data = excluded.data, fetched_at = now()`, f.City, day, js)
	return err
}

// WeatherFor gives every city's forecast for a day, in the picker's order.
func (s *Store) WeatherFor(ctx context.Context, day time.Time) ([]Forecast, error) {
	rows, err := s.db.Query(ctx, `SELECT data FROM weather WHERE day = $1 ORDER BY (data->>'order')::int, city`, day)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Forecast
	for rows.Next() {
		var data []byte
		if err := rows.Scan(&data); err != nil {
			return nil, err
		}
		var f Forecast
		if err := json.Unmarshal(data, &f); err != nil {
			return nil, err
		}
		out = append(out, f)
	}
	return out, rows.Err()
}

// ---------- NASA's picture of the day ----------

type APOD struct {
	Day         time.Time
	Title       string
	Explanation string
	Credit      string
	Link        string
	MediaType   string
	PictureURL  string
	Picture     string // media hash once saved
	Alt         string // NASA's description of the picture, for screen readers
}

// SaveAPOD keeps a day's picture. A picture already saved stays unless a new one is given.
func (s *Store) SaveAPOD(ctx context.Context, a APOD) error {
	_, err := s.db.Exec(ctx, `INSERT INTO apod (day, title, explanation, credit, link, media_type, picture_url, picture, alt)
		VALUES ($1, $2, $3, $4, $5, $6, $7, nullif($8, ''), $9)
		ON CONFLICT (day) DO UPDATE SET title = excluded.title, explanation = excluded.explanation, credit = excluded.credit,
			link = excluded.link, media_type = excluded.media_type, picture_url = excluded.picture_url,
			picture = coalesce(excluded.picture, apod.picture), alt = excluded.alt, fetched_at = now()`,
		a.Day, a.Title, a.Explanation, a.Credit, a.Link, a.MediaType, a.PictureURL, a.Picture, a.Alt)
	return err
}

// LatestAPOD is the newest picture on or before a day: NASA posts after the morning fetch,
// so the morning edition carries the day before's.
func (s *Store) LatestAPOD(ctx context.Context, day time.Time) (APOD, error) {
	var a APOD
	err := s.db.QueryRow(ctx, `SELECT day, title, explanation, credit, link, media_type, picture_url, coalesce(picture, ''), alt
		FROM apod WHERE day <= $1 ORDER BY day DESC LIMIT 1`, day).
		Scan(&a.Day, &a.Title, &a.Explanation, &a.Credit, &a.Link, &a.MediaType, &a.PictureURL, &a.Picture, &a.Alt)
	if errors.Is(err, pgx.ErrNoRows) {
		return a, ErrNotFound
	}
	return a, err
}

// APODPicture tells whether a day's picture is already saved, so it is not downloaded twice.
func (s *Store) APODPicture(ctx context.Context, day time.Time) (string, error) {
	var hash string
	err := s.db.QueryRow(ctx, `SELECT coalesce(picture, '') FROM apod WHERE day = $1`, day).Scan(&hash)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", nil
	}
	return hash, err
}
