package fetch

import (
	"os"
	"strings"
	"testing"
	"time"
)

// Every feed sample saved in Stage 0 must parse into items with a title, a link and a date.
func TestFeedSamples(t *testing.T) {
	cases := []struct {
		file     string
		min      int
		pictures bool // most items should carry a picture
	}{
		{"hrt-hrvatska.xml", 20, true},
		{"hrt-kultura.xml", 20, true},
		{"bug.xml", 10, true},
		{"loudwire.xml", 10, true},
		{"glazba.xml", 5, false},
		{"muzika.xml", 5, false}, // no pictures in the feed
		{"kinofilm.xml", 5, true},
		{"germanijak-0.xml", 20, false},
	}
	for _, c := range cases {
		t.Run(c.file, func(t *testing.T) {
			body, err := os.ReadFile("testdata/" + c.file)
			if err != nil {
				t.Fatal(err)
			}
			items, err := ParseFeed(body)
			if err != nil {
				t.Fatal(err)
			}
			if len(items) < c.min {
				t.Fatalf("got %d items, want at least %d", len(items), c.min)
			}
			withPic := 0
			for _, it := range items {
				if it.Title == "" || !strings.HasPrefix(it.Link, "http") || it.GUID == "" {
					t.Errorf("incomplete item: %+v", it)
				}
				if it.PublishedAt == nil || it.PublishedAt.Year() < 2020 {
					t.Errorf("%q: no date", it.Title)
				}
				if strings.ContainsAny(it.Title+it.Summary, "<>") || strings.Contains(it.Summary, "&#") {
					t.Errorf("%q: markup left in text: %q", it.Title, it.Summary)
				}
				if len([]rune(it.Summary)) > 321 {
					t.Errorf("%q: summary too long", it.Title)
				}
				if it.PictureURL != "" {
					withPic++
				}
			}
			if c.pictures && withPic < len(items)/2 {
				t.Errorf("only %d of %d items have a picture", withPic, len(items))
			}
		})
	}
}

func TestEmptyFeed(t *testing.T) {
	body, _ := os.ReadFile("testdata/germanijak-3.xml")
	items, err := ParseFeed(body)
	if err != nil || len(items) != 0 {
		t.Fatalf("got %d items, %v; want an empty, valid feed", len(items), err)
	}
}

func TestHRTDetails(t *testing.T) {
	body, _ := os.ReadFile("testdata/hrt-hrvatska.xml")
	items, _ := ParseFeed(body)
	it := items[0]
	if !strings.Contains(it.PictureURL, "740x438") {
		t.Errorf("HRT should use its 740 px thumbnail, got %q", it.PictureURL)
	}
	if !strings.HasPrefix(it.Title, "Puljak") {
		t.Errorf("title %q: the byte order mark or order is off", it.Title)
	}
}

func TestHN(t *testing.T) {
	body, _ := os.ReadFile("testdata/hn-frontpage.xml")
	items, err := ParseHN(body)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) < 12 {
		t.Fatalf("got %d items, Hacker News needs 12", len(items))
	}
	it := items[0]
	if it.Extra["points"] != "4" || it.Extra["comment_count"] != "0" || it.Author != "jakevoytko" {
		t.Errorf("first item: %+v", it)
	}
	if !strings.Contains(it.Extra["comments"], "news.ycombinator.com/item?id=") {
		t.Errorf("comments link: %q", it.Extra["comments"])
	}
	if it.Summary != "" {
		t.Errorf("summary should be empty, got %q", it.Summary)
	}
}

func TestSummary(t *testing.T) {
	cases := map[string]string{
		`<p>Here&apos;s it. <a href="x">Continue reading&hellip;</a></p>`: "Here's it.",
		`Šokre, prvi si [&#8230;]`:                                        "Šokre, prvi si…",
		`a &amp;amp; b`:                                                   "a & b",
	}
	for in, want := range cases {
		if got := Summary(in, 320); got != want {
			t.Errorf("Summary(%q) = %q, want %q", in, got, want)
		}
	}
	long := strings.Repeat("riječ ", 100)
	if got := Summary(long, 50); len([]rune(got)) > 51 || !strings.HasSuffix(got, "…") {
		t.Errorf("long summary not cut at a word: %q", got)
	}
}

func TestParseDate(t *testing.T) {
	for _, s := range []string{
		"Tue, 27 Oct 2026 11:30:24 +0000", "Tue, 27 Oct 2026 11:00:00 Z", "Mon, 26 Oct 2026 20:03:30 GMT", "2026-10-27T11:30:24Z",
	} {
		got, ok := ParseDate(s)
		if !ok || got.Year() != 2026 || got.Month() != time.October {
			t.Errorf("ParseDate(%q) = %v, %v", s, got, ok)
		}
	}
}

func TestWordPressFooters(t *testing.T) {
	cases := map[string]string{
		`"Monstrumi iz mitova su besmrtni." The post Aldo Tardozzi: “Hrvatskoj nedostaju izleti” appeared first on kinofilm .`:         `"Monstrumi iz mitova su besmrtni."`,
		`Bruce Dickinson je stajao na pozornici. Te je [&#8230;] Objava Bruce Dickinson se vraća pojavila se prvi puta na muzika.hr .`: "Bruce Dickinson je stajao na pozornici. Te je…",
	}
	for in, want := range cases {
		if got := Summary(in, 320); got != want {
			t.Errorf("Summary(%q)\n got %q\nwant %q", in, got, want)
		}
	}
}

// Loudwire puts the photo credit in media:description; it must not replace the real description.
func TestMediaDescriptionIgnored(t *testing.T) {
	feed := `<rss xmlns:media="http://search.yahoo.com/mrss/"><channel><item><title>T</title><link>https://x/1</link>
<description>The real text.</description><media:description>Photo: Getty Images</media:description></item></channel></rss>`
	items, err := ParseFeed([]byte(feed))
	if err != nil || len(items) != 1 || items[0].Summary != "The real text." {
		t.Fatalf("got %+v, %v", items, err)
	}
}
