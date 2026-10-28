package fetch

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"

	"tisak/internal/store"
)

func init() { RegisterKeeper("sportsdb", SportsDB{}) }

// SportsDB keeps a team's last result, with its scorers, and its next match, from TheSportsDB.
// Settings: team, TheSportsDB's id of the team (133912 is Croatia).
//
// The free key, 123, is TheSportsDB's public one; a paid key goes in SPORTSDB_API_KEY and, like every
// key, stays out of the code, the logs and the errors.
type SportsDB struct{}

const sportsDBEndpoint = "https://www.thesportsdb.com/api/v1/json/"

// sportsDBBase is the API's address with the key; tests point it at their own server.
var sportsDBBase = func() (base, key string) {
	key = strings.TrimSpace(os.Getenv("SPORTSDB_API_KEY"))
	if key == "" {
		key = "123"
	}
	return sportsDBEndpoint + key + "/", key
}

var digits = regexp.MustCompile(`^\d+$`)

func (SportsDB) Keep(ctx context.Context, k Kit, settings map[string]string) (int, error) {
	team := strings.TrimSpace(settings["team"])
	if !digits.MatchString(team) {
		return 0, fmt.Errorf("the team setting must be TheSportsDB's id of a team, like 133912, not %q", team)
	}
	base, key := sportsDBBase()
	get := func(path string, v any) error {
		body, err := k.Client.Get(ctx, base+path)
		if err != nil {
			return errors.New(strings.ReplaceAll(err.Error(), key, "…"))
		}
		if err := json.Unmarshal(body, v); err != nil {
			return fmt.Errorf("%s: not TheSportsDB's answer: %w", strings.SplitN(path, "?", 2)[0], err)
		}
		return nil
	}
	var last struct {
		Results []sdbEvent `json:"results"`
	}
	var next struct {
		Events []sdbEvent `json:"events"`
	}
	if err := get("eventslast.php?id="+team, &last); err != nil {
		return 0, err
	}
	if err := get("eventsnext.php?id="+team, &next); err != nil {
		return 0, err
	}
	loc := k.Now.Location()
	kept := 0
	var failed []error
	for _, e := range append(last.Results, next.Events...) {
		m, ok := e.match(team, loc)
		if !ok {
			continue
		}
		m.Source = "sportsdb:" + team
		if m.Played() {
			var tl struct {
				Timeline []sdbMoment `json:"timeline"`
			}
			if err := get("lookuptimeline.php?id="+idPart(e.ID), &tl); err == nil && tl.Timeline != nil {
				m.Goals = Goals(tl.Timeline)
			} else if m.Goals, err = k.Store.MatchGoals(ctx, m.EventID); err != nil {
				return kept, err
			}
		}
		if err := k.Store.SaveMatch(ctx, m); err != nil {
			failed = append(failed, err)
			continue
		}
		kept++
	}
	if kept == 0 && len(failed) == 0 {
		return 0, errors.New("TheSportsDB has no matches for the team")
	}
	return kept, errors.Join(failed...)
}

// idPart keeps an id from the feed to digits, as it goes into an address.
func idPart(id string) string {
	if digits.MatchString(id) {
		return id
	}
	return "0"
}

type sdbEvent struct {
	ID        string  `json:"idEvent"`
	Timestamp string  `json:"strTimestamp"` // UTC: 2026-10-06T18:45:00
	Date      string  `json:"dateEvent"`
	Time      string  `json:"strTime"`
	League    string  `json:"strLeague"`
	Round     string  `json:"intRound"`
	Home      string  `json:"strHomeTeam"`
	Away      string  `json:"strAwayTeam"`
	HomeID    string  `json:"idHomeTeam"`
	AwayID    string  `json:"idAwayTeam"`
	HomeScore *string `json:"intHomeScore"`
	AwayScore *string `json:"intAwayScore"`
	Venue     string  `json:"strVenue"`
	Status    string  `json:"strStatus"`
	Postponed string  `json:"strPostponed"`
}

type sdbMoment struct {
	Kind   string `json:"strTimeline"`       // Goal, Card, subst
	Detail string `json:"strTimelineDetail"` // Normal Goal, Penalty, Own Goal
	Home   string `json:"strHome"`           // Yes or No
	Minute string `json:"intTime"`
	Player string `json:"strPlayer"`
}

// match turns an event into a match of the followed team, its time in the paper's zone.
func (e sdbEvent) match(team string, loc *time.Location) (store.Match, bool) {
	if e.ID == "" || e.Home == "" || e.Away == "" || strings.EqualFold(e.Postponed, "yes") {
		return store.Match{}, false
	}
	m := store.Match{EventID: e.ID, League: Clean(e.League), Round: strings.TrimSpace(e.Round), Home: Clean(e.Home),
		Away: Clean(e.Away), Venue: Clean(e.Venue)}
	m.Team = m.Home
	if e.AwayID == team {
		m.Team = m.Away
	}
	if t, err := time.Parse("2006-01-02T15:04:05", e.Timestamp); err == nil {
		t = t.In(loc)
		m.Day = time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, time.UTC)
		if e.Time != "" && e.Time != "00:00:00" {
			m.Time = t.Format("15:04")
		}
	} else if d, err := time.Parse(time.DateOnly, e.Date); err == nil {
		m.Day = d
	} else {
		return store.Match{}, false
	}
	m.HomeScore, m.AwayScore = score(e.HomeScore), score(e.AwayScore)
	if m.HomeScore == nil || m.AwayScore == nil || e.Status == "NS" {
		m.HomeScore, m.AwayScore = nil, nil
	}
	return m, true
}

func score(s *string) *int {
	if s == nil {
		return nil
	}
	n, err := strconv.Atoi(strings.TrimSpace(*s))
	if err != nil || n < 0 {
		return nil
	}
	return &n
}

// Goals reads a match's timeline for its goals, in order.
func Goals(timeline []sdbMoment) []store.Goal {
	var out []store.Goal
	for _, x := range timeline {
		if !strings.EqualFold(x.Kind, "Goal") || strings.TrimSpace(x.Player) == "" {
			continue
		}
		g := store.Goal{Player: Clean(x.Player), Home: strings.EqualFold(x.Home, "Yes")}
		g.Minute, _ = strconv.Atoi(strings.TrimSpace(x.Minute))
		switch d := strings.ToLower(x.Detail); {
		case strings.Contains(d, "own"):
			g.Kind = "own"
		case strings.Contains(d, "penalty"):
			if strings.Contains(d, "miss") {
				continue
			}
			g.Kind = "penalty"
		}
		out = append(out, g)
	}
	return out
}
