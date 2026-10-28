package fetch

import (
	"encoding/json"
	"testing"
	"time"
)

// A made-up result and fixture in TheSportsDB's shape.
func TestSportsDBMatch(t *testing.T) {
	loc, _ := time.LoadLocation("Europe/Zagreb")
	var x struct {
		Results []sdbEvent `json:"results"`
	}
	body := `{"results":[
		{"idEvent":"11","strTimestamp":"2026-10-06T18:45:00","dateEvent":"2026-10-06","strTime":"18:45:00","strLeague":"Probna liga",
		 "intRound":"4","strHomeTeam":"Croatia","strAwayTeam":"Spain","idHomeTeam":"133912","idAwayTeam":"5","intHomeScore":"1",
		 "intAwayScore":"2","strVenue":"Stadion","strStatus":"FT"},
		{"idEvent":"12","strTimestamp":"2026-11-12T23:30:00","dateEvent":"2026-11-12","strTime":"23:30:00","strHomeTeam":"England",
		 "strAwayTeam":"Croatia","idHomeTeam":"7","idAwayTeam":"133912","intHomeScore":null,"intAwayScore":null,"strStatus":"NS"},
		{"idEvent":"13","strTimestamp":"2026-11-15T19:45:00","strHomeTeam":"A","strAwayTeam":"B","strPostponed":"yes"}]}`
	if err := json.Unmarshal([]byte(body), &x); err != nil {
		t.Fatal(err)
	}
	played, ok := x.Results[0].match("133912", loc)
	if !ok || !played.Played() || *played.HomeScore != 1 || *played.AwayScore != 2 || played.Time != "20:45" ||
		played.Day.Format("2006-01-02") != "2026-10-06" || played.Team != "Croatia" {
		t.Errorf("played: %+v", played)
	}
	ahead, ok := x.Results[1].match("133912", loc)
	if !ok || ahead.Played() || ahead.Team != "Croatia" || ahead.Day.Format("2006-01-02") != "2026-11-13" || ahead.Time != "00:30" {
		t.Errorf("ahead: %+v (late kick-offs move to Zagreb's next day)", ahead)
	}
	if _, ok := x.Results[2].match("133912", loc); ok {
		t.Error("a postponed match is left out")
	}
}

func TestGoals(t *testing.T) {
	g := Goals([]sdbMoment{
		{Kind: "Goal", Detail: "Normal Goal", Home: "Yes", Minute: "17", Player: "Igrač Jedan"},
		{Kind: "Card", Detail: "Yellow Card", Home: "Yes", Minute: "20", Player: "Igrač Jedan"},
		{Kind: "Goal", Detail: "Penalty", Home: "No", Minute: "55", Player: "Igrač Dva"},
		{Kind: "Goal", Detail: "Missed Penalty", Home: "No", Minute: "60", Player: "Igrač Dva"},
		{Kind: "Goal", Detail: "Own Goal", Home: "No", Minute: "88", Player: "Igrač Tri"},
	})
	if len(g) != 3 || g[0].Minute != 17 || !g[0].Home || g[1].Kind != "penalty" || g[2].Kind != "own" || g[2].Home {
		t.Errorf("goals %+v", g)
	}
}
