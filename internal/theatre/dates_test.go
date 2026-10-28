package theatre

import (
	"testing"
	"time"
)

func TestDay(t *testing.T) {
	today := time.Date(2026, 10, 25, 0, 0, 0, 0, time.UTC)
	for _, c := range []struct{ in, want string }{
		{"25.10.2026.", "2026-10-25"},
		{"Subota, 31. 10.", "2026-10-31"},
		{"pet 6.11. u 19:30", "2026-11-06"},
		{"15.1.", "2027-01-15"},
		{"2. studenoga 2026.", "2026-11-02"},
		{"ČETVRTAK 12. PROSINCA", "2026-12-12"},
		{"1. siječnja", "2027-01-01"},
		{"Nedjelja, 20. rujna", "2026-09-20"}, // lately past, not next year
		{"no date", ""},
		{"31.11.", ""},
	} {
		got, ok := Day(c.in, today)
		s := ""
		if ok {
			s = got.Format(time.DateOnly)
		}
		if s != c.want {
			t.Errorf("Day(%q) = %q, want %q", c.in, s, c.want)
		}
	}
}

func TestClock(t *testing.T) {
	for in, want := range map[string]string{
		"19:30": "19:30", "u 20.00 h": "20:00", "9:05": "09:05", "20h00": "20:00", "Cijena 12.50 €": "12:50", "": "",
		"25.10.": "", "pet 20.11. 19:30": "19:30", "26. 10. 2026. 12:00h": "12:00", "25.10.2026": "", "20:30 -23:00": "20:30",
	} {
		if got := Clock(in); got != want {
			t.Errorf("Clock(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestWindow(t *testing.T) {
	today := time.Date(2026, 10, 25, 0, 0, 0, 0, time.UTC)
	day := func(n int) time.Time { return today.AddDate(0, 0, n) }
	got := Window([]Show{
		{Date: day(-1), Time: "19:30", Title: "Jučer"},
		{Date: day(0), Time: "19:30", Title: "  Gospoda   Glembajevi "},
		{Date: day(0), Time: "19:30", Title: "GOSPODA GLEMBAJEVI"},
		{Date: day(0), Time: "", Title: ""},
		{Date: day(Ahead), Time: "20:00", Title: "Zadnji dan"},
		{Date: day(Ahead + 1), Time: "20:00", Title: "Predaleko"},
	}, "Gavella", today)
	if len(got) != 2 || got[0].Title != "Gospoda Glembajevi" || got[0].Theatre != "Gavella" || got[1].Title != "Zadnji dan" {
		t.Fatalf("Window = %+v", got)
	}
}
