package store

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
)

// ---------- matches ----------

// Goal is one goal of a match, as the card lists the scorers.
type Goal struct {
	Minute int    `json:"minute"`
	Player string `json:"player"`
	Home   bool   `json:"home"`           // scored for the home side
	Kind   string `json:"kind,omitempty"` // "", "penalty" or "own"
}

// Match is a followed team's match: played when both scores are set.
type Match struct {
	EventID   string
	Source    string
	Team      string
	League    string
	Round     string
	Home      string
	Away      string
	HomeScore *int
	AwayScore *int
	Day       time.Time
	Time      string
	Venue     string
	Goals     []Goal
}

// Played tells whether the match has its score.
func (m Match) Played() bool { return m.HomeScore != nil && m.AwayScore != nil }

// SaveMatch adds a match or brings it up to date: a fixture becomes a result.
func (s *Store) SaveMatch(ctx context.Context, m Match) error {
	if m.Goals == nil {
		m.Goals = []Goal{}
	}
	_, err := s.db.Exec(ctx, `INSERT INTO matches (event_id, source, team, league, round, home, away, home_score, away_score,
			day, time, venue, goals)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13)
		ON CONFLICT (event_id) DO UPDATE SET source = excluded.source, team = excluded.team, league = excluded.league,
			round = excluded.round, home = excluded.home, away = excluded.away, home_score = excluded.home_score,
			away_score = excluded.away_score, day = excluded.day, time = excluded.time, venue = excluded.venue,
			goals = excluded.goals, updated_at = now()`,
		m.EventID, m.Source, m.Team, m.League, m.Round, m.Home, m.Away, m.HomeScore, m.AwayScore, m.Day, m.Time, m.Venue, m.Goals)
	return err
}

// MatchGoals gives a match's goals as kept, so a fetch that cannot read the timeline keeps them.
func (s *Store) MatchGoals(ctx context.Context, eventID string) ([]Goal, error) {
	var g []Goal
	err := s.db.QueryRow(ctx, `SELECT goals FROM matches WHERE event_id = $1`, eventID).Scan(&g)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	return g, err
}

const matchCols = `event_id, source, team, league, round, home, away, home_score, away_score, day, time, venue, goals`

func scanMatch(r pgx.Row) (Match, error) {
	var m Match
	err := r.Scan(&m.EventID, &m.Source, &m.Team, &m.League, &m.Round, &m.Home, &m.Away, &m.HomeScore, &m.AwayScore,
		&m.Day, &m.Time, &m.Venue, &m.Goals)
	if errors.Is(err, pgx.ErrNoRows) {
		return m, ErrNotFound
	}
	return m, err
}

// LastResult is the newest played match between two days.
func (s *Store) LastResult(ctx context.Context, from, to time.Time) (Match, error) {
	return scanMatch(s.db.QueryRow(ctx, `SELECT `+matchCols+` FROM matches
		WHERE home_score IS NOT NULL AND away_score IS NOT NULL AND day BETWEEN $1 AND $2
		ORDER BY day DESC, time DESC LIMIT 1`, from, to))
}

// NextFixture is the first match still to be played between two days.
func (s *Store) NextFixture(ctx context.Context, from, to time.Time) (Match, error) {
	return scanMatch(s.db.QueryRow(ctx, `SELECT `+matchCols+` FROM matches
		WHERE (home_score IS NULL OR away_score IS NULL) AND day BETWEEN $1 AND $2
		ORDER BY day, time LIMIT 1`, from, to))
}

// TeamResults are a team's last few played matches before a day, the newest first.
func (s *Store) TeamResults(ctx context.Context, team string, before time.Time, n int) ([]Match, error) {
	rows, err := s.db.Query(ctx, `SELECT `+matchCols+` FROM matches
		WHERE team = $1 AND home_score IS NOT NULL AND away_score IS NOT NULL AND day < $2
		ORDER BY day DESC, time DESC LIMIT $3`, team, before, n)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(r pgx.CollectableRow) (Match, error) { return scanMatch(r) })
}

// ---------- Riječililica ----------

// GameWord is the day's word and the game's number.
type GameWord struct {
	Day    time.Time
	Word   string
	Number int
}

// DrawGameWord gives the day's word: the one drawn already, so a reprint keeps it, or a new one that
// pick chooses, given the words of the last year.
func (s *Store) DrawGameWord(ctx context.Context, day time.Time, pick func(used map[string]bool) (string, error)) (GameWord, error) {
	g := GameWord{Day: day}
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return g, err
	}
	defer tx.Rollback(ctx)
	if _, err := tx.Exec(ctx, `LOCK TABLE game_words IN SHARE ROW EXCLUSIVE MODE`); err != nil {
		return g, err
	}
	err = tx.QueryRow(ctx, `SELECT word, number FROM game_words WHERE day = $1`, day).Scan(&g.Word, &g.Number)
	if err == nil {
		return g, tx.Commit(ctx)
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return g, err
	}
	rows, err := tx.Query(ctx, `SELECT word FROM game_words WHERE day > $1::date - 366`, day)
	if err != nil {
		return g, err
	}
	words, err := pgx.CollectRows(rows, pgx.RowTo[string])
	if err != nil {
		return g, err
	}
	used := map[string]bool{}
	for _, w := range words {
		used[w] = true
	}
	if g.Word, err = pick(used); err != nil {
		return g, err
	}
	err = tx.QueryRow(ctx, `INSERT INTO game_words (day, word, number)
		VALUES ($1, $2, (SELECT coalesce(max(number), 0) + 1 FROM game_words)) RETURNING number`, day, g.Word).Scan(&g.Number)
	if err != nil {
		return g, err
	}
	return g, tx.Commit(ctx)
}

// ---------- Večera ----------

// Course is one recipe of a dinner.
type Course struct {
	Kind        string   `json:"kind"` // starter, main, dessert, cocktail
	Title       string   `json:"title"`
	Intro       string   `json:"intro,omitempty"`
	Meta        string   `json:"meta,omitempty"` // "4 osobe · 20 min"
	Ingredients []string `json:"ingredients,omitempty"`
	Method      []string `json:"method,omitempty"`
}

// Dinner is the menu from a day until the next one.
type Dinner struct {
	ID      int64
	Day     time.Time
	Courses []Course
}

const dinnerCols = `id, day, courses`

func scanDinner(r pgx.Row) (Dinner, error) {
	var d Dinner
	err := r.Scan(&d.ID, &d.Day, &d.Courses)
	if errors.Is(err, pgx.ErrNoRows) {
		return d, ErrNotFound
	}
	return d, err
}

// Dinners lists the dinners from a day on, and the last few before it.
func (s *Store) Dinners(ctx context.Context, from time.Time) ([]Dinner, error) {
	rows, err := s.db.Query(ctx, `(SELECT `+dinnerCols+` FROM dinners WHERE day >= $1 ORDER BY day)
		UNION ALL (SELECT `+dinnerCols+` FROM dinners WHERE day < $1 ORDER BY day DESC LIMIT 8)`, from)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(r pgx.CollectableRow) (Dinner, error) { return scanDinner(r) })
}

func (s *Store) Dinner(ctx context.Context, id int64) (Dinner, error) {
	return scanDinner(s.db.QueryRow(ctx, `SELECT `+dinnerCols+` FROM dinners WHERE id = $1`, id))
}

// DinnerOn is the dinner that holds on a day: the newest from that day or before, no older than a week.
func (s *Store) DinnerOn(ctx context.Context, day time.Time) (Dinner, error) {
	return scanDinner(s.db.QueryRow(ctx, `SELECT `+dinnerCols+` FROM dinners WHERE day BETWEEN $1::date - 6 AND $1
		ORDER BY day DESC LIMIT 1`, day))
}

// SaveDinner adds a dinner, or changes it when it has an id. A day holds one dinner.
func (s *Store) SaveDinner(ctx context.Context, d Dinner) (int64, error) {
	if d.Courses == nil {
		d.Courses = []Course{}
	}
	if d.ID == 0 {
		err := s.db.QueryRow(ctx, `INSERT INTO dinners (day, courses) VALUES ($1, $2) RETURNING id`, d.Day, d.Courses).Scan(&d.ID)
		return d.ID, taken(err)
	}
	tag, err := s.db.Exec(ctx, `UPDATE dinners SET day = $2, courses = $3, updated_at = now() WHERE id = $1`, d.ID, d.Day, d.Courses)
	if err == nil && tag.RowsAffected() == 0 {
		return d.ID, ErrNotFound
	}
	return d.ID, taken(err)
}

func (s *Store) DeleteDinner(ctx context.Context, id int64) error {
	_, err := s.db.Exec(ctx, `DELETE FROM dinners WHERE id = $1`, id)
	return err
}

// ---------- Kontakt ----------

// Message is what a reader sent through Kontakt.
type Message struct {
	ID       int64
	Name     string
	Email    string
	Subject  string
	Body     string
	IP       string
	Created  time.Time
	Read     *time.Time
	Archived bool
}

const messageCols = `id, name, email, subject, body, ip, created_at, read_at, archived`

func scanMessage(r pgx.Row) (Message, error) {
	var m Message
	err := r.Scan(&m.ID, &m.Name, &m.Email, &m.Subject, &m.Body, &m.IP, &m.Created, &m.Read, &m.Archived)
	if errors.Is(err, pgx.ErrNoRows) {
		return m, ErrNotFound
	}
	return m, err
}

func (s *Store) SaveMessage(ctx context.Context, m Message) (int64, error) {
	err := s.db.QueryRow(ctx, `INSERT INTO messages (name, email, subject, body, ip) VALUES ($1, $2, $3, $4, $5) RETURNING id`,
		m.Name, m.Email, m.Subject, m.Body, m.IP).Scan(&m.ID)
	return m.ID, err
}

// MessagesFrom counts the messages sent from an address since a time, to slow down a flood.
func (s *Store) MessagesFrom(ctx context.Context, ip string, since time.Time) (int, error) {
	var n int
	err := s.db.QueryRow(ctx, `SELECT count(*) FROM messages WHERE ip = $1 AND created_at >= $2`, ip, since).Scan(&n)
	return n, err
}

// Messages lists the inbox, or the archive, the newest first.
func (s *Store) Messages(ctx context.Context, archived bool, limit int) ([]Message, error) {
	rows, err := s.db.Query(ctx, `SELECT `+messageCols+` FROM messages WHERE archived = $1 ORDER BY created_at DESC LIMIT $2`,
		archived, limit)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(r pgx.CollectableRow) (Message, error) { return scanMessage(r) })
}

// UnreadMessages counts the inbox's unread messages.
func (s *Store) UnreadMessages(ctx context.Context) (int, error) {
	var n int
	err := s.db.QueryRow(ctx, `SELECT count(*) FROM messages WHERE read_at IS NULL AND NOT archived`).Scan(&n)
	return n, err
}

// OpenMessage gives a message and marks it read.
func (s *Store) OpenMessage(ctx context.Context, id int64) (Message, error) {
	return scanMessage(s.db.QueryRow(ctx, `UPDATE messages SET read_at = coalesce(read_at, now()) WHERE id = $1
		RETURNING `+messageCols, id))
}

func (s *Store) ArchiveMessage(ctx context.Context, id int64, archived bool) error {
	tag, err := s.db.Exec(ctx, `UPDATE messages SET archived = $2 WHERE id = $1`, id, archived)
	if err == nil && tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return err
}

func (s *Store) DeleteMessage(ctx context.Context, id int64) error {
	_, err := s.db.Exec(ctx, `DELETE FROM messages WHERE id = $1`, id)
	return err
}

// ---------- settings ----------

// Settings are the admin's small texts, by key.
func (s *Store) Settings(ctx context.Context) (map[string]string, error) {
	rows, err := s.db.Query(ctx, `SELECT key, value FROM settings`)
	if err != nil {
		return nil, err
	}
	out := map[string]string{}
	var k, v string
	_, err = pgx.ForEachRow(rows, []any{&k, &v}, func() error { out[k] = v; return nil })
	return out, err
}

// SetSettings keeps the given texts; an empty one is removed.
func (s *Store) SetSettings(ctx context.Context, kv map[string]string) error {
	tx, err := s.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	for k, v := range kv {
		if v == "" {
			_, err = tx.Exec(ctx, `DELETE FROM settings WHERE key = $1`, k)
		} else {
			_, err = tx.Exec(ctx, `INSERT INTO settings (key, value) VALUES ($1, $2)
				ON CONFLICT (key) DO UPDATE SET value = excluded.value`, k, v)
		}
		if err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}
