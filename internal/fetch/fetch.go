// Package fetch turns sources into items. Every kind of source is one Fetcher;
// a new kind of site means one new file that registers itself here.
package fetch

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	neturl "net/url"
	"sort"
	"strings"
	"sync"
	"time"

	"tisak/internal/store"
)

// A Fetcher reads one source and gives its items, newest first. It must not touch the database.
type Fetcher interface {
	Fetch(ctx context.Context, c *Client, settings map[string]string) ([]store.Item, error)
}

var kinds = map[string]Fetcher{}

// Register makes a fetcher kind available to sources. Call it from an init function.
func Register(kind string, f Fetcher) { kinds[kind] = f }

// Kinds lists the registered kinds, side column ones too, for the dashboard's source form.
func Kinds() []string {
	out := make([]string, 0, len(kinds)+len(keepers))
	for k := range kinds {
		out = append(out, k)
	}
	for k := range keepers {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func Lookup(kind string) (Fetcher, bool) { f, ok := kinds[kind]; return f, ok }

// Client is the one HTTP client every fetcher uses: a timeout, a polite user agent, a size cap.
type Client struct {
	HTTP      *http.Client
	UserAgent string
	MaxBytes  int64
}

func NewClient() *Client {
	return &Client{
		HTTP:      &http.Client{Timeout: 25 * time.Second},
		UserAgent: "DigitalniDnevniTisak/1.0 (+https://github.com/)",
		MaxBytes:  20 << 20,
	}
}

// Get downloads a URL and returns its body, failing on anything but a 200.
func (c *Client) Get(ctx context.Context, url string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	return c.do(req, url)
}

// Post sends a form and returns the answer's body, as Get does.
func (c *Client) Post(ctx context.Context, url string, form neturl.Values) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, strings.NewReader(form.Encode()))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	return c.do(req, url)
}

// Open starts a download too large to hold, such as a data set of every week since 1958, for reading as it comes.
func (c *Client) Open(ctx context.Context, url string) (io.ReadCloser, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	long := *c.HTTP
	long.Timeout = 0 // a big file takes longer than a page; the caller's context sets the limit
	resp, err := c.send(&long, req, url)
	if err != nil {
		return nil, err
	}
	return resp.Body, nil
}

func (c *Client) send(hc *http.Client, req *http.Request, url string) (*http.Response, error) {
	req.Header.Set("User-Agent", c.UserAgent)
	req.Header.Set("Accept", "*/*")
	resp, err := hc.Do(req)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		resp.Body.Close()
		return nil, fmt.Errorf("%s: HTTP %d", url, resp.StatusCode)
	}
	return resp, nil
}

func (c *Client) do(req *http.Request, url string) ([]byte, error) {
	resp, err := c.send(c.HTTP, req, url)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, c.MaxBytes+1))
	if err != nil {
		return nil, err
	}
	if int64(len(body)) > c.MaxBytes {
		return nil, fmt.Errorf("%s: larger than %d MB", url, c.MaxBytes>>20)
	}
	return body, nil
}

// PictureSaver stores a picture and gives its media hash. The media package provides it.
type PictureSaver interface {
	SaveURL(ctx context.Context, url string, maxWidth int) (string, error)
}

// Result is one source's outcome, for the log and the health board.
type Result struct {
	Source   store.Source
	Items    int
	Pictures int
	Err      error
	Took     time.Duration
}

type Runner struct {
	Store    *store.Store
	Client   *Client
	Pictures PictureSaver
	Workers  int
	// Picture widths: lead cards print large, everything else small.
	PictureWidth int
	Loc          *time.Location // the paper's time zone, for the side column's day
}

// Run fetches every enabled source, saves the items, then saves the pictures the press may print.
// One broken source never stops the others.
func (r *Runner) Run(ctx context.Context, only func(store.Source) bool) ([]Result, error) {
	sources, err := r.Store.Sources(ctx)
	if err != nil {
		return nil, err
	}
	cats, err := r.Store.Categories(ctx)
	if err != nil {
		return nil, err
	}
	pictures := map[int64]bool{} // only categories that print a lead picture need pictures
	for _, c := range cats {
		pictures[c.ID] = c.LeadPicture
	}
	var todo []store.Source
	for _, s := range sources {
		if s.Enabled && (only == nil || only(s)) {
			todo = append(todo, s)
		}
	}
	workers := r.Workers
	if workers <= 0 {
		workers = 4
	}
	results := make([]Result, len(todo))
	var wg sync.WaitGroup
	next := make(chan int)
	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := range next {
				start := time.Now()
				results[i] = r.one(ctx, todo[i], pictures[todo[i].CategoryID])
				results[i].Took = time.Since(start)
			}
		}()
	}
	for i := range todo {
		next <- i
	}
	close(next)
	wg.Wait()
	return results, nil
}

func (r *Runner) one(ctx context.Context, src store.Source, wantPictures bool) Result {
	res := Result{Source: src}
	if k, ok := keepers[src.Kind]; ok {
		return r.keep(ctx, src, k)
	}

	f, ok := Lookup(src.Kind)
	if !ok {
		res.Err = fmt.Errorf("unknown fetcher kind %q", src.Kind)
		_ = r.Store.FetchResult(ctx, src.ID, 0, res.Err)
		return res
	}
	sctx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()
	items, err := f.Fetch(sctx, r.Client, src.Settings)
	if err == nil && len(items) == 0 {
		err = errors.New("no items")
	}
	if err != nil {
		res.Err = err
		_ = r.Store.FetchResult(ctx, src.ID, 0, err)
		return res
	}
	if _, err := r.Store.SaveItems(ctx, src.ID, items); err != nil {
		res.Err = err
		_ = r.Store.FetchResult(ctx, src.ID, 0, err)
		return res
	}
	res.Items = len(items)

	// Pictures are saved now, at fetch time, so the press never waits on the network.
	// Only the newest few per source can print, so only those are downloaded.
	if r.Pictures != nil && wantPictures {
		need, err := r.Store.ItemsNeedingPicture(ctx, src.ID, src.PerEdition+2)
		if err == nil {
			for _, it := range need {
				hash, err := r.Pictures.SaveURL(sctx, it.PictureURL, r.PictureWidth)
				if err != nil {
					continue // the card prints without its picture
				}
				if r.Store.SetItemPicture(ctx, it.ID, hash) == nil {
					res.Pictures++
				}
			}
		}
	}
	_ = r.Store.FetchResult(ctx, src.ID, res.Items, nil)
	return res
}

func (r *Runner) keep(ctx context.Context, src store.Source, k Keeper) Result {
	res := Result{Source: src}
	loc := r.Loc
	if loc == nil {
		loc = time.Local
	}
	sctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	kit := Kit{Store: r.Store, Client: r.Client, Pictures: r.Pictures, Now: time.Now().In(loc)}
	res.Items, res.Err = k.Keep(sctx, kit, src.Settings)
	if res.Err == nil && res.Items == 0 {
		res.Err = errors.New("nothing to keep")
	}
	_ = r.Store.FetchResult(ctx, src.ID, res.Items, res.Err)
	return res
}
