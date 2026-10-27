package fetch

import (
	"context"
	"regexp"

	"tisak/internal/store"
)

func init() { Register("hn", HN{}) }

// HN reads the Hacker News front page through hnrss.org. Settings: url (defaults to the front page).
// The description only repeats the links and counts, so it becomes points and comments instead of a summary.
type HN struct{}

func (HN) Fetch(ctx context.Context, c *Client, settings map[string]string) ([]store.Item, error) {
	url := settings["url"]
	if url == "" {
		url = "https://hnrss.org/frontpage"
	}
	body, err := c.Get(ctx, url)
	if err != nil {
		return nil, err
	}
	return ParseHN(body)
}

var (
	hnPoints   = regexp.MustCompile(`Points:\s*(\d+)`)
	hnComments = regexp.MustCompile(`# Comments:\s*(\d+)`)
)

func ParseHN(body []byte) ([]store.Item, error) {
	items, descs, err := parseFeed(body)
	if err != nil {
		return nil, err
	}
	for i := range items {
		if m := hnPoints.FindStringSubmatch(descs[i]); m != nil {
			items[i].Extra["points"] = m[1]
		}
		if m := hnComments.FindStringSubmatch(descs[i]); m != nil {
			items[i].Extra["comment_count"] = m[1]
		}
		items[i].Summary = ""
		items[i].PictureURL = ""
	}
	return items, nil
}
