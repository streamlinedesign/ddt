package fetch

import (
	"context"
	"errors"
	"fmt"
	"html"
	"math"
	"regexp"
	"strconv"
	"strings"
	"time"

	"tisak/internal/store"
)

func init() { RegisterKeeper("dhmz", DHMZ{}) }

// City is one place in the weather picker, with its code on meteo.hr.
type City struct{ Name, Code string }

// Cities are the picker's cities, Zagreb first: it is shown until a reader picks another.
var Cities = []City{
	{"Zagreb", "ZAGREB-GRI%C4%8C"}, {"Split", "SPLIT"}, {"Rijeka", "RIJEKA"}, {"Osijek", "OSIJEK"},
	{"Zadar", "ZADAR"}, {"Pula", "PULA"}, {"Dubrovnik", "DUBROVNIK"}, {"Varaždin", "VARA%C5%BDDIN"},
}

func (c City) Link() string {
	return "https://meteo.hr/prognoze.php?id=prognoza&section=prognoze_model&param=7d&Code=" + c.Code
}

// partHours are the times of day the panel shows; the nearest forecast hour stands in.
var partHours = []struct {
	Label string
	Hour  int
}{{"Ujutro", 8}, {"Poslijepodne", 14}, {"Noć", 22}}

// DHMZ reads the national weather service's 7-day forecast for every city in the picker and keeps today's.
// No settings.
type DHMZ struct{}

func (DHMZ) Keep(ctx context.Context, k Kit, _ map[string]string) (int, error) {
	day := k.Day()
	before, err := k.Store.WeatherFor(ctx, day)
	if err != nil {
		return 0, err
	}
	old := map[string]store.Forecast{}
	for _, f := range before {
		old[f.City] = f
	}
	kept := 0
	var failed []error
	for i, c := range Cities {
		body, err := k.Client.Get(ctx, c.Link())
		if err == nil {
			var f store.Forecast
			f, err = ParseDHMZ(body, day)
			if err == nil {
				f.City, f.Order, f.Link = c.Name, i, c.Link()
				f.Parts = keepParts(f.Parts, old[c.Name].Parts)
				err = k.Store.SaveWeather(ctx, day, f)
			}
		}
		if err != nil {
			failed = append(failed, fmt.Errorf("%s: %w", c.Name, err))
			continue
		}
		kept++
	}
	return kept, errors.Join(failed...)
}

// keepParts fills the parts of the day that have passed from an earlier fetch of the same day,
// so an afternoon reprint still has the morning.
func keepParts(now, earlier []store.ForecastPart) []store.ForecastPart {
	var out []store.ForecastPart
	for _, ph := range partHours {
		found := false
		for _, p := range now {
			if p.Label == ph.Label {
				out, found = append(out, p), true
				break
			}
		}
		if found {
			continue
		}
		for _, p := range earlier {
			if p.Label == ph.Label {
				out = append(out, p)
				break
			}
		}
	}
	return out
}

var (
	dhmzRow    = regexp.MustCompile(`(?s)<tr data-row="\d+">(.*?)</tr>`)
	dhmzCell   = regexp.MustCompile(`(?s)<td[^>]*>(.*?)</td>`)
	dhmzIcon   = regexp.MustCompile(`id="icon-(\w+)"`)
	dhmzTitle  = regexp.MustCompile(`(?s)<title>(.*?)</title>`)
	dhmzNum    = regexp.MustCompile(`-?\d+(?:[.,]\d+)?`)
	dhmzDay    = regexp.MustCompile(`(?s)<tr data-col="\d+"><th>[^<]*<br>\s*(\d{1,2}\.\d{1,2}\.\d{4})\.?</th>(.*?)</tr>`)
	dhmzMaxMin = regexp.MustCompile(`(?s)(-?\d+(?:\.\d+)?)\s*(?:&#176;|°)C\s*</span>\s*/\s*<span[^>]*>\s*(-?\d+(?:\.\d+)?)\s*(?:&#176;|°)C`)
)

type dhmzHour struct {
	Hour int
	Temp float64
	Code string // 1, 1n, 12...
	Sky  string
}

// ParseDHMZ reads one city's 7-day page and gives the day's lowest and highest temperature
// and the sky at the parts of the day that are still ahead.
func ParseDHMZ(body []byte, day time.Time) (store.Forecast, error) {
	page := string(body)
	var f store.Forecast
	dates, hours, temps, skies := dhmzTable(page, "tabl_datum"), dhmzTable(page, "tabl_sat"), dhmzTable(page, "tabl_temp"), dhmzTable(page, "tabl_simb")
	if len(dates) == 0 || len(dates) != len(hours) || len(dates) != len(temps) || len(dates) != len(skies) {
		return f, errors.New("the hourly tables are missing or do not line up: has the page changed?")
	}
	want := day.Format("2.1.2006")
	var today []dhmzHour
	for r := range dates {
		for c := range dates[r] {
			if c >= len(hours[r]) || c >= len(temps[r]) || c >= len(skies[r]) || dmy(dates[r][c]) != want {
				continue
			}
			h, err1 := strconv.Atoi(strings.TrimSpace(hours[r][c]))
			t, err2 := number(temps[r][c])
			if err1 != nil || err2 != nil {
				continue
			}
			var code, sky string
			if m := dhmzIcon.FindStringSubmatch(skies[r][c]); m != nil {
				code = m[1]
			}
			if m := dhmzTitle.FindStringSubmatch(skies[r][c]); m != nil {
				sky = strings.TrimSpace(html.UnescapeString(m[1]))
			}
			today = append(today, dhmzHour{h, t, code, sky})
		}
	}
	if len(today) == 0 {
		return f, fmt.Errorf("no forecast for %s", day.Format("02.01.2006."))
	}

	for _, ph := range partHours {
		best, gap := -1, 4 // within three hours, or the part is left out
		for i, h := range today {
			if d := abs(h.Hour - ph.Hour); d < gap {
				best, gap = i, d
			}
		}
		if best < 0 {
			continue
		}
		h := today[best]
		f.Parts = append(f.Parts, store.ForecastPart{Label: ph.Label, Hour: h.Hour, Temp: round(h.Temp),
			Icon: SkyIcon(h.Sky, strings.HasSuffix(h.Code, "n")), Sky: h.Sky})
	}

	// The short table has the day's own maximum and minimum; the hours that are left are the fallback.
	for _, m := range dhmzDay.FindAllStringSubmatch(page, -1) {
		if dmy(m[1]) != want {
			continue
		}
		if mm := dhmzMaxMin.FindStringSubmatch(m[2]); mm != nil {
			hi, _ := strconv.ParseFloat(mm[1], 64)
			lo, _ := strconv.ParseFloat(mm[2], 64)
			f.Max, f.Min = ptr(round(hi)), ptr(round(lo))
		}
		break
	}
	if f.Max == nil {
		lo, hi := today[0].Temp, today[0].Temp
		for _, h := range today {
			lo, hi = math.Min(lo, h.Temp), math.Max(hi, h.Temp)
		}
		f.Max, f.Min = ptr(round(hi)), ptr(round(lo))
	}
	return f, nil
}

// dhmzTable gives a table's cells by row.
func dhmzTable(page, id string) [][]string {
	start := strings.Index(page, `id="`+id+`"`)
	if start < 0 {
		return nil
	}
	end := strings.Index(page[start:], "</table>")
	if end < 0 {
		return nil
	}
	var out [][]string
	for _, row := range dhmzRow.FindAllStringSubmatch(page[start:start+end], -1) {
		var cells []string
		for _, c := range dhmzCell.FindAllStringSubmatch(row[1], -1) {
			cells = append(cells, c[1])
		}
		out = append(out, cells)
	}
	return out
}

// dmy turns 27.10.2026. and 7.1.2026 alike into 27.10.2026, for comparing.
func dmy(s string) string {
	parts := strings.Split(strings.Trim(strings.TrimSpace(s), "."), ".")
	if len(parts) != 3 {
		return ""
	}
	d, err1 := strconv.Atoi(parts[0])
	m, err2 := strconv.Atoi(parts[1])
	if err1 != nil || err2 != nil {
		return ""
	}
	return fmt.Sprintf("%d.%d.%s", d, m, parts[2])
}

func number(s string) (float64, error) {
	m := dhmzNum.FindString(s)
	if m == "" {
		return 0, errors.New("no number")
	}
	return strconv.ParseFloat(strings.ReplaceAll(m, ",", "."), 64)
}

// SkyIcon picks the Meteocons icon for the forecaster's words.
func SkyIcon(sky string, night bool) string {
	s := strings.ToLower(sky)
	has := func(words ...string) bool {
		for _, w := range words {
			if strings.Contains(s, w) {
				return true
			}
		}
		return false
	}
	dayNight := func(d, n string) string {
		if night {
			return n
		}
		return d
	}
	switch {
	case has("grmljavin", "nevrijem"):
		return "thunderstorms"
	case has("susnježic", "susnjež") || has("snijeg") && has("kiš"):
		return "sleet"
	case has("snijeg", "snjež"):
		return "snow"
	case has("magl"):
		return "fog"
	case has("kiš", "pljus"):
		switch {
		case has("promjenljiv", "malo oblačno", "umjereno", "sunčano"):
			return dayNight("partly-cloudy-day-rain", "drizzle")
		case has("mal", "slab", "rosulj"):
			return "drizzle"
		}
		return "rain"
	case has("potpuno"):
		return "overcast"
	case has("pretežno"):
		return dayNight("overcast-day", "cloudy")
	case has("umjereno", "malo oblačno", "promjenljiv", "djelomično"):
		return dayNight("partly-cloudy-day", "partly-cloudy-night")
	case has("sunčano", "vedro"):
		return dayNight("clear-day", "clear-night")
	case has("oblačno"):
		return "overcast"
	}
	return "cloudy"
}

func round(f float64) int { return int(math.Round(f)) }
func ptr(n int) *int      { return &n }
func abs(n int) int {
	if n < 0 {
		return -n
	}
	return n
}
