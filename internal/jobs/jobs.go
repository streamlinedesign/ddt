// Package jobs runs the fetch and the print one at a time, whether the daily clock or the
// dashboard asks for them, and remembers how each last went.
package jobs

import (
	"context"
	"errors"
	"sync"
	"time"

	"tisak/internal/fetch"
	"tisak/internal/press"
	"tisak/internal/store"
)

// ErrBusy means another fetch or print is running; the dashboard says so instead of waiting.
var ErrBusy = errors.New("busy")

type Jobs struct {
	Runner *fetch.Runner
	Press  *press.Press

	run   sync.Mutex // held while a job runs
	mu    sync.Mutex // guards the fields below
	what  string     // the running job, empty when idle
	since time.Time
	fetch Last
	print Last
}

// Last is how a job last went.
type Last struct {
	At     time.Time
	Took   time.Duration
	Err    error
	Failed int // fetch: sources that failed
	Total  int // fetch: sources tried
}

// Fetch runs the fetchers, all of them when only is nil. With wait false it gives ErrBusy
// instead of queueing behind another job.
func (j *Jobs) Fetch(ctx context.Context, only func(store.Source) bool, wait bool) ([]fetch.Result, error) {
	what := "fetch"
	if only != nil {
		what = "fetch-some" // a single source's run is not the day's fetch
	}
	var results []fetch.Result
	err := j.do(what, wait, func(last *Last) error {
		var err error
		results, err = j.Runner.Run(ctx, only)
		last.Total = len(results)
		for _, r := range results {
			if r.Err != nil {
				last.Failed++
			}
		}
		return err
	})
	return results, err
}

// Print prints today's next edition.
func (j *Jobs) Print(ctx context.Context, reason string, wait bool) (store.Edition, string, error) {
	var e store.Edition
	var dir string
	err := j.do("print", wait, func(*Last) error {
		var err error
		e, dir, err = j.Press.Print(ctx, reason)
		return err
	})
	return e, dir, err
}

func (j *Jobs) do(what string, wait bool, f func(*Last) error) error {
	if wait {
		j.run.Lock()
	} else if !j.run.TryLock() {
		return ErrBusy
	}
	defer j.run.Unlock()
	start := time.Now()
	j.mu.Lock()
	j.what, j.since = what, start
	j.mu.Unlock()

	last := Last{At: start}
	last.Err = f(&last)
	last.Took = time.Since(start)

	j.mu.Lock()
	defer j.mu.Unlock()
	switch what {
	case "fetch":
		j.fetch = last
	case "print":
		j.print = last
	}
	j.what = ""
	return last.Err
}

// Status is what the dashboard shows: the running job, if any, and how the last ones went.
type Status struct {
	Running      string
	RunningSince time.Time
	Fetch, Print Last
}

func (j *Jobs) Status() Status {
	j.mu.Lock()
	defer j.mu.Unlock()
	return Status{Running: j.what, RunningSince: j.since, Fetch: j.fetch, Print: j.print}
}
