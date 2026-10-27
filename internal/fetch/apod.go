package fetch

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"strings"
	"time"

	"tisak/internal/store"
)

func init() { RegisterKeeper("apod", APOD{}) }

// APOD reads NASA's Astronomy Picture of the Day from its keyless feed and keeps the last few days,
// so the morning edition has yesterday's picture before today's is posted (at five past five, Zagreb time).
// Settings: url (defaults to the feed).
type APOD struct{}

const apodFeed = "https://science.nasa.gov/wp-json/wp/v2/apod-basic?per_page=3"

// apodWidth is how wide the picture is kept: the panel is narrow, but readers open it full screen.
const apodWidth = 1600

func (APOD) Keep(ctx context.Context, k Kit, settings map[string]string) (int, error) {
	link := settings["url"]
	if link == "" {
		link = apodFeed
	}
	body, err := k.Client.Get(ctx, link)
	if err != nil {
		return 0, err
	}
	days, err := ParseAPOD(body)
	if err != nil {
		return 0, err
	}
	kept := 0
	var failed []error
	for _, a := range days {
		if a.MediaType == "image" && a.PictureURL != "" && k.Pictures != nil {
			have, err := k.Store.APODPicture(ctx, a.Day)
			if err != nil {
				return kept, err
			}
			if have == "" {
				if a.Picture, err = k.Pictures.SaveURL(ctx, a.PictureURL, apodWidth); err != nil {
					failed = append(failed, fmt.Errorf("%s picture: %w", a.Day.Format("2006-01-02"), err))
				}
			}
		}
		if err := k.Store.SaveAPOD(ctx, a); err != nil {
			return kept, err
		}
		kept++
	}
	return kept, errors.Join(failed...)
}

type apodPost struct {
	Date        string `json:"date"`
	Title       string `json:"title"`
	Permalink   string `json:"permalink"`
	MediaType   string `json:"media_type"`
	Explanation string `json:"explanation"`
	Credit      string `json:"credit"`
	Copyright   string `json:"copyright"`
	Alt         string `json:"alt"`
	URL         string `json:"url"`
	HDURL       string `json:"hdurl"`
}

// ParseAPOD reads the feed's posts, newest first, as plain text.
func ParseAPOD(body []byte) ([]store.APOD, error) {
	var posts []apodPost
	if err := json.Unmarshal(body, &posts); err != nil {
		return nil, fmt.Errorf("not the APOD feed: %w", err)
	}
	var out []store.APOD
	for _, p := range posts {
		day, err := time.Parse("2006-01-02", p.Date)
		if err != nil || strings.TrimSpace(p.Title) == "" {
			continue
		}
		a := store.APOD{
			Day: day, Title: Clean(p.Title), Link: first(p.Permalink, p.URL), MediaType: first(p.MediaType, "image"),
			Explanation: strings.TrimSpace(strings.TrimPrefix(prose(p.Explanation), "Explanation:")),
			Credit:      prose(first(p.Credit, p.Copyright)), Alt: Clean(p.Alt),
		}
		if a.MediaType == "image" {
			a.PictureURL = apodPicture(first(p.HDURL, p.URL))
		}
		out = append(out, a)
	}
	if len(out) == 0 {
		return nil, errors.New("the feed has no pictures")
	}
	return out, nil
}

var (
	spaceBefore = regexp.MustCompile(`\s+([,.;:!?)])`)
	spaceAfter  = regexp.MustCompile(`\(\s+`)
)

// prose cleans text whose links became spaces: "Höfer , Večerník" reads "Höfer, Večerník".
func prose(s string) string {
	return spaceAfter.ReplaceAllString(spaceBefore.ReplaceAllString(Clean(s), "$1"), "(")
}

// apodPicture asks NASA's image server for a picture no wider than the one kept, instead of the original.
func apodPicture(link string) string {
	u, err := url.Parse(link)
	if err != nil || u.Host == "" {
		return ""
	}
	if strings.Contains(u.Path, "/dynamicimage/") || u.Query().Has("w") {
		q := u.Query()
		q.Set("w", fmt.Sprint(apodWidth))
		q.Del("h")
		u.RawQuery = q.Encode()
	}
	return u.String()
}
