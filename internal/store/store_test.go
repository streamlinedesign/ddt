package store

import (
	"context"
	"os"
	"testing"
	"time"
)

func TestDaysBetween(t *testing.T) {
	a := time.Date(2026, 10, 27, 23, 0, 0, 0, time.UTC)
	b := time.Date(2026, 10, 29, 1, 0, 0, 0, time.UTC)
	if got := DaysBetween(a, b); got != 2 {
		t.Errorf("DaysBetween = %d, want 2", got)
	}
}

// TestNumbers runs against a real database only when TISAK_TEST_DB points at an empty one, e.g.
// TISAK_TEST_DB=postgres://tisak:tisak@localhost/tisak_test go test ./internal/store
func TestNumbers(t *testing.T) {
	url := os.Getenv("TISAK_TEST_DB")
	if url == "" {
		t.Skip("TISAK_TEST_DB not set")
	}
	ctx := context.Background()
	s, err := Open(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	for _, q := range []string{"DROP SCHEMA public CASCADE", "CREATE SCHEMA public"} {
		if _, err := s.db.Exec(ctx, q); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := s.Migrate(ctx); err != nil {
		t.Fatal(err)
	}
	if again, err := s.Migrate(ctx); err != nil || len(again) != 0 {
		t.Fatalf("second migrate applied %v, %v", again, err)
	}
	d1 := time.Date(2026, 10, 27, 0, 0, 0, 0, time.UTC)
	d3 := d1.AddDate(0, 0, 2)
	print := func(day time.Time, wantN, wantV int) {
		t.Helper()
		n, v, err := s.NextNumbers(ctx, day)
		if err != nil || n != wantN || v != wantV {
			t.Fatalf("%s: Br. %d Verzija %d (%v), want Br. %d Verzija %d", day.Format("02.01."), n, v, err, wantN, wantV)
		}
		if _, err := s.SaveEdition(ctx, Edition{Day: day, Number: n, Version: v, Snapshot: []byte("{}"), HTML: "x", Bytes: 1}); err != nil {
			t.Fatal(err)
		}
	}
	print(d1, 1, 1)
	print(d1, 1, 2) // a reprint keeps the day's number
	print(d3, 3, 1) // a day without a print still counts
	if _, err := s.SaveEdition(ctx, Edition{Day: d3, Number: 3, Version: 1, Snapshot: []byte("{}"), HTML: "x"}); err == nil {
		t.Error("two editions with the same day and version were saved")
	}
}
