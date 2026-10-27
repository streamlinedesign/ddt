package fetch

import (
	"context"
	"sort"
	"time"

	"tisak/internal/store"
)

// A Keeper is a kind that fills the side column rather than a category: the weather, NASA's picture.
// It keeps its own records for the day and says how many it kept.
type Keeper interface {
	Keep(ctx context.Context, k Kit, settings map[string]string) (int, error)
}

// Kit is what a keeper works with.
type Kit struct {
	Store    *store.Store
	Client   *Client
	Pictures PictureSaver
	Now      time.Time // local time of the fetch
}

// Day is the fetch's date, as the tables store it.
func (k Kit) Day() time.Time {
	return time.Date(k.Now.Year(), k.Now.Month(), k.Now.Day(), 0, 0, 0, 0, time.UTC)
}

var keepers = map[string]Keeper{}

// RegisterKeeper makes a side column kind available. Call it from an init function.
func RegisterKeeper(kind string, k Keeper) { keepers[kind] = k }

// Keeps tells whether a kind fills the side column, so its source has no category.
func Keeps(kind string) bool { _, ok := keepers[kind]; return ok }

// Known tells whether any fetcher or keeper has the kind.
func Known(kind string) bool { _, ok := kinds[kind]; return ok || Keeps(kind) }

// SideKinds lists the side column kinds.
func SideKinds() []string {
	out := make([]string, 0, len(keepers))
	for k := range keepers {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
