package fetch

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"tisak/internal/store"
)

// dhmzPage builds a page shaped like meteo.hr's 7-day forecast: hourly tables by day row, and the short
// table with each day's maximum and minimum. The forecast itself is made up.
func dhmzPage() []byte {
	type hour struct {
		day, h int
		temp   string
		code   string
		sky    string
	}
	var hours [][]hour
	// fetched at two in the afternoon: today's row starts at 14
	var today []hour
	for h := 14; h <= 23; h++ {
		code, sky := "1", "Sunčano"
		if h >= 19 {
			code, sky = "1n", "Vedro"
		}
		today = append(today, hour{27, h, fmt.Sprintf("%.1f °C", 14.5-float64(h-14)*0.8), code, sky})
	}
	hours = append(hours, today)
	var tomorrow []hour
	for h := 0; h <= 23; h++ {
		code, sky := "3", "Umjereno oblačno"
		switch {
		case h < 7:
			code, sky = "1n", "Vedro"
		case h < 12:
			code, sky = "1", "Sunčano"
		case h >= 20:
			code, sky = "4n", "Pretežno oblačno"
		}
		tomorrow = append(tomorrow, hour{28, h, fmt.Sprintf("%.1f °C", 5+float64(min(h, 14))/2), code, sky})
	}
	hours = append(hours, tomorrow)

	var b strings.Builder
	b.WriteString(`<table id="kratka_tablica" class="fd-c-table"><tbody><tr><th>Dan</th></tr>`)
	b.WriteString(`<tr data-col="0"><th>Utorak<br>27.10.2026.</th><td></td><td><span class="fd-u-color--secondary">14.5 &#176;C </span> / <span class="fd-u-color--primary">7.0 &#176;C</span></td></tr>`)
	b.WriteString(`<tr data-col="1"><th>Srijeda<br>28.10.2026.</th><td><span class="fd-u-color--secondary">12.8 &#176;C </span> / <span class="fd-u-color--primary">4.7 &#176;C</span></td></tr>`)
	b.WriteString("</tbody></table>\n")
	table := func(id string, cell func(hour) string) {
		fmt.Fprintf(&b, `<table class="fd-c-table" id="%s"><tbody>`, id)
		for r, row := range hours {
			fmt.Fprintf(&b, `<tr data-row="%d">`, r)
			for _, h := range row {
				fmt.Fprintf(&b, `<td class="fd-u-border-right--2">%s</td>`, cell(h))
			}
			b.WriteString("</tr>\n")
		}
		b.WriteString("</tbody></table>\n")
	}
	table("tabl_simb", func(h hour) string {
		return fmt.Sprintf(`<svg id="icon-%s" viewBox="10 10 90 90"><title>%s</title><path d="M1 1"/></svg>`, h.code, h.sky)
	})
	table("tabl_temp", func(h hour) string { return h.temp })
	table("tabl_datum", func(h hour) string { return fmt.Sprintf("%d.10.2026. ", h.day) })
	table("tabl_sat", func(h hour) string { return fmt.Sprintf("%02d ", h.h) })
	return []byte(b.String())
}

func TestParseDHMZ(t *testing.T) {
	body := dhmzPage()
	// Fetched at two in the afternoon: the morning has passed, so only the afternoon and night are left.
	f, err := ParseDHMZ(body, time.Date(2026, 10, 27, 0, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	if f.Max == nil || *f.Max != 15 || *f.Min != 7 {
		t.Errorf("max/min = %v/%v, want 15/7", deref(f.Max), deref(f.Min))
	}
	if len(f.Parts) != 2 || f.Parts[0].Label != "Poslijepodne" || f.Parts[0].Temp != 15 || f.Parts[1].Label != "Noć" ||
		f.Parts[1].Hour != 22 || f.Parts[1].Icon != "clear-night" {
		t.Errorf("parts = %+v", f.Parts)
	}

	// The next day has every hour.
	f, err = ParseDHMZ(body, time.Date(2026, 10, 28, 0, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatal(err)
	}
	if len(f.Parts) != 3 || f.Parts[0].Hour != 8 || f.Parts[1].Hour != 14 || f.Parts[2].Hour != 22 {
		t.Fatalf("parts = %+v", f.Parts)
	}
	if f.Parts[0].Icon != "clear-day" || f.Parts[1].Icon != "partly-cloudy-day" || f.Parts[2].Icon != "cloudy" || f.Parts[1].Temp != 12 {
		t.Errorf("parts = %+v", f.Parts)
	}
	if *f.Max != 13 || *f.Min != 5 {
		t.Errorf("max/min = %d/%d, want 13/5", *f.Max, *f.Min)
	}

	if _, err := ParseDHMZ(body, time.Date(2026, 11, 20, 0, 0, 0, 0, time.UTC)); err == nil {
		t.Error("a day past the forecast should fail")
	}
	if _, err := ParseDHMZ([]byte("<html></html>"), time.Now()); err == nil {
		t.Error("an empty page should fail")
	}
}

func TestSkyIcon(t *testing.T) {
	for _, c := range []struct {
		sky   string
		night bool
		want  string
	}{
		{"Sunčano", false, "clear-day"},
		{"Vedro", true, "clear-night"},
		{"Malo oblačno", false, "partly-cloudy-day"},
		{"Umjereno oblačno", true, "partly-cloudy-night"},
		{"Pretežno oblačno", false, "overcast-day"},
		{"Potpuno oblačno", false, "overcast"},
		{"Promjenljivo oblačno uz malu količinu kiše", false, "partly-cloudy-day-rain"},
		{"Oblačno uz malu količinu kiše", false, "drizzle"},
		{"Oblačno s kišom", false, "rain"},
		{"Kiša i snijeg", false, "sleet"},
		{"Oblačno sa snijegom", false, "snow"},
		{"Kiša s grmljavinom", false, "thunderstorms"},
		{"Magla", false, "fog"},
	} {
		if got := SkyIcon(c.sky, c.night); got != c.want {
			t.Errorf("SkyIcon(%q, %v) = %s, want %s", c.sky, c.night, got, c.want)
		}
	}
}

func deref(p *int) any {
	if p == nil {
		return nil
	}
	return *p
}

func TestKeepParts(t *testing.T) {
	morning := []store.ForecastPart{{Label: "Ujutro", Temp: 6}, {Label: "Poslijepodne", Temp: 12}, {Label: "Noć", Temp: 8}}
	afternoon := []store.ForecastPart{{Label: "Poslijepodne", Temp: 13}, {Label: "Noć", Temp: 9}}
	got := keepParts(afternoon, morning)
	if len(got) != 3 || got[0].Temp != 6 || got[1].Temp != 13 || got[2].Temp != 9 {
		t.Errorf("keepParts = %+v", got)
	}
	if got := keepParts(afternoon, nil); len(got) != 2 {
		t.Errorf("without an earlier fetch: %+v", got)
	}
}
