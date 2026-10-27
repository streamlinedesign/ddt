package fetch

import (
	"strings"
	"testing"
)

func TestParseAPOD(t *testing.T) {
	// shaped like the APOD Basic feed; the posts are made up
	body := []byte(`[
 {"date": "2026-10-27", "title": "Clouds over a Field", "permalink": "https://science.nasa.gov/image-article/apod-2026-october-27-x/",
  "media_type": "image", "explanation": "<strong>Explanation:\u00a0</strong>Sometimes it's night on the ground but <a href=\"https://example.com/\">day</a> in the air.",
  "credit": "<a href=\"mailto:\">A. Author</a>, B. Second &amp; C. Third", "copyright": "", "alt": "A dark sky over a field.",
  "url": "https://science.nasa.gov/image-article/apod-2026-october-27-x/",
  "hdurl": "https://assets.science.nasa.gov/dynamicimage/assets/science/cds/apod/apod/2026/october/x.jpg?w=6048&h=4030&fit=clip"},
 {"date": "2026-10-26", "title": "A Nebula", "media_type": "image", "explanation": "<strong>Explanation:</strong> Gas.",
  "credit": "<a href=\"https://example.com\">D. Fourth</a> ( Observatory, Somewhere)", "url": "https://science.nasa.gov/b/", "hdurl": "https://example.com/b.jpg"},
 {"date": "2026-10-25", "title": "A Video", "media_type": "video", "explanation": "Moving.", "url": "https://science.nasa.gov/c/"}
]`)
	days, err := ParseAPOD(body)
	if err != nil {
		t.Fatal(err)
	}
	if len(days) != 3 {
		t.Fatalf("got %d days, want 3", len(days))
	}
	a := days[0]
	if a.Day.Format("2006-01-02") != "2026-10-27" || a.Title != "Clouds over a Field" {
		t.Errorf("first = %s %q", a.Day.Format("2006-01-02"), a.Title)
	}
	if strings.HasPrefix(a.Explanation, "Explanation") || strings.Contains(a.Explanation, "<") || !strings.HasPrefix(a.Explanation, "Sometimes it's night") {
		t.Errorf("explanation = %.60q", a.Explanation)
	}
	if a.Credit != "A. Author, B. Second & C. Third" {
		t.Errorf("credit = %q", a.Credit)
	}
	if !strings.Contains(a.PictureURL, "w=1600") || strings.Contains(a.PictureURL, "h=4030") {
		t.Errorf("picture = %s", a.PictureURL)
	}
	if a.Alt == "" || a.Link == "" {
		t.Errorf("alt %q link %q", a.Alt, a.Link)
	}
	if days[1].Credit != "D. Fourth (Observatory, Somewhere)" || days[1].PictureURL != "https://example.com/b.jpg" {
		t.Errorf("second: %q %q", days[1].Credit, days[1].PictureURL)
	}
	if days[2].MediaType != "video" || days[2].PictureURL != "" {
		t.Errorf("a video has no picture: %+v", days[2])
	}
	if _, err := ParseAPOD([]byte(`{"code":"rest_no_route"}`)); err == nil {
		t.Error("an error answer should fail")
	}
}
