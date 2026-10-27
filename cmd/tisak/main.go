// Command tisak fetches at 04:30, prints at 05:00 and serves the site. One binary, one database.
//
//	tisak -migrate              create or update the tables (and fill in the first categories)
//	tisak -fetch-now            run every fetcher once
//	tisak -print-now            print today's next edition (a reprint fetches first)
//	tisak -check                test the database and try every source without saving
//	tisak -admin uriel          set the admin's name and password (the password is read from the terminal or stdin)
//	tisak                       serve the site and the dashboard at /admin/, with the daily fetch and print built in
package main

import (
	"bufio"
	"context"
	"errors"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"text/tabwriter"
	"time"

	_ "time/tzdata" // Europe/Zagreb even on a server without zone files

	"golang.org/x/term"

	"tisak/internal/admin"
	"tisak/internal/auth"
	"tisak/internal/fetch"
	"tisak/internal/jobs"
	"tisak/internal/media"
	"tisak/internal/press"
	"tisak/internal/site"
	"tisak/internal/store"
	"tisak/web"
)

func main() {
	var (
		migrate  = flag.Bool("migrate", false, "create or update the tables, then stop")
		fetchNow = flag.Bool("fetch-now", false, "run every fetcher once, then stop")
		printNow = flag.Bool("print-now", false, "print today's next edition, then stop")
		check    = flag.Bool("check", false, "test the database and every source without saving, then stop")
		setAdmin = flag.String("admin", "", "set the admin's `name` and password, then stop")
		dev      = flag.Bool("dev", false, "read templates and styles from ./web on every print, so edits show at once")
		reason   = flag.String("reason", "", "why this edition is printed, shown in the archive (for a reprint)")
		addr     = flag.String("addr", env("TISAK_ADDR", ":8080"), "address to serve on")
		dbURL    = flag.String("db", env("DATABASE_URL", "postgres://tisak:tisak@localhost:5432/tisak"), "Postgres connection URL")
		data     = flag.String("data", env("TISAK_DATA", "data"), "folder for pictures and editions")
		siteURL  = flag.String("site", env("TISAK_SITE", "/"), "the site's root, used in links from editions back to the archive")
		fetchAt  = flag.String("fetch-at", "04:30", "daily fetch time, Zagreb time")
		printAt  = flag.String("print-at", "05:00", "daily print time, Zagreb time")
		proxy    = flag.Bool("trust-proxy", env("TISAK_TRUST_PROXY", "") != "", "behind a proxy: read the client's address and https from X-Forwarded-*")
	)
	flag.Parse()
	logger := log.New(os.Stderr, "", log.LstdFlags)

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	db, err := store.Open(ctx, *dbURL)
	if err != nil {
		logger.Fatal(err)
	}
	defer db.Close()

	loc, err := time.LoadLocation("Europe/Zagreb")
	if err != nil {
		logger.Fatal(err)
	}
	webDir := ""
	if *dev {
		webDir = "web"
	}
	client := fetch.NewClient()
	pics := &media.Store{Dir: filepath.Join(*data, "media"), DB: db, Client: client}
	pr := &press.Press{Store: db, Media: pics, Web: web.FS(webDir), Out: filepath.Join(*data, "editions"), Site: *siteURL, Loc: loc}
	runner := &fetch.Runner{Store: db, Client: client, Pictures: pics, PictureWidth: 1000}
	app := &App{DB: db, Press: pr, Runner: runner, Jobs: &jobs.Jobs{Runner: runner, Press: pr}, Media: pics, Log: logger}
	app.Admin = &admin.Admin{Store: db, Auth: &auth.Manager{Store: db, TrustProxy: *proxy}, Jobs: app.Jobs, Press: pr,
		Media: pics, Client: client, Web: pr.Web, Loc: loc, Log: logger, FetchAt: *fetchAt, PrintAt: *printAt}

	switch {
	case *migrate:
		err = app.Migrate(ctx)
	case *check:
		err = app.Check(ctx)
	case *setAdmin != "":
		err = app.SetAdmin(ctx, *setAdmin)
	case *fetchNow:
		err = app.Fetch(ctx)
	case *printNow:
		err = app.PrintNow(ctx, *reason)
	default:
		err = app.Serve(ctx, *addr, *fetchAt, *printAt)
	}
	if err != nil && !errors.Is(err, context.Canceled) {
		logger.Fatal(err)
	}
}

func env(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

type App struct {
	DB     *store.Store
	Press  *press.Press
	Runner *fetch.Runner
	Jobs   *jobs.Jobs // every fetch and print goes through here, one at a time
	Media  *media.Store
	Admin  *admin.Admin
	Log    *log.Logger
}

func (a *App) Migrate(ctx context.Context) error {
	applied, err := a.DB.Migrate(ctx)
	for _, m := range applied {
		a.Log.Printf("migrated %s", m)
	}
	if err != nil {
		return err
	}
	if len(applied) == 0 {
		a.Log.Print("tables are up to date")
	}
	added, err := Seed(ctx, a.DB, a.Media, a.Press.Web)
	if added > 0 {
		a.Log.Printf("added the first %d categories and their sources", added)
	}
	return err
}

// Fetch runs every fetcher and prints a short report.
func (a *App) Fetch(ctx context.Context) error {
	start := time.Now()
	results, err := a.Jobs.Fetch(ctx, nil, true)
	if err != nil {
		return err
	}
	tw := tabwriter.NewWriter(os.Stdout, 0, 4, 2, ' ', 0)
	fmt.Fprintln(tw, "SOURCE\tITEMS\tPICTURES\tTIME\tRESULT")
	failed := 0
	for _, r := range results {
		res := "ok"
		if r.Err != nil {
			res, failed = r.Err.Error(), failed+1
		}
		fmt.Fprintf(tw, "%s\t%d\t%d\t%s\t%s\n", r.Source.Name, r.Items, r.Pictures, r.Took.Round(10*time.Millisecond), res)
	}
	tw.Flush()
	a.Log.Printf("fetched %d sources in %s, %d failed", len(results), time.Since(start).Round(time.Second), failed)
	return nil
}

// PrintNow prints today's next edition. A reprint (Verzija 2 and on) fetches first, as does a print
// with nothing fetched in the last two hours.
func (a *App) PrintNow(ctx context.Context, reason string) error {
	now := time.Now().In(a.Press.Loc)
	day := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)
	printed, err := a.DB.HasEdition(ctx, day)
	if err != nil {
		return err
	}
	last, err := a.DB.LastFetch(ctx)
	if err != nil {
		return err
	}
	if printed || time.Since(last) > 2*time.Hour {
		a.Log.Print("fetching first")
		if err := a.Fetch(ctx); err != nil {
			return err
		}
	}
	return a.print(ctx, reason)
}

func (a *App) print(ctx context.Context, reason string) error {
	start := time.Now()
	e, dir, err := a.Jobs.Print(ctx, reason, true)
	if err != nil {
		return err
	}
	a.Log.Printf("printed Br. %d, Verzija %d: %s (%d KB) in %s", e.Number, e.Version, filepath.Join(dir, "index.html"),
		(e.Bytes+512)/1024, time.Since(start).Round(time.Millisecond))
	return nil
}

// Check tests the database and tries every source without saving anything.
func (a *App) Check(ctx context.Context) error {
	sources, err := a.DB.Sources(ctx)
	if err != nil {
		return fmt.Errorf("database works, but the tables are missing (run -migrate): %w", err)
	}
	a.Log.Printf("database ok, %d sources", len(sources))
	tw := tabwriter.NewWriter(os.Stdout, 0, 4, 2, ' ', 0)
	fmt.Fprintln(tw, "SOURCE\tKIND\tITEMS\tNEWEST\tRESULT")
	for _, s := range sources {
		f, ok := fetch.Lookup(s.Kind)
		if !ok {
			fmt.Fprintf(tw, "%s\t%s\t-\t-\tunknown kind\n", s.Name, s.Kind)
			continue
		}
		items, err := f.Fetch(ctx, a.Runner.Client, s.Settings)
		newest, res := "-", "ok"
		if err != nil {
			res = err.Error()
		} else if len(items) > 0 && items[0].PublishedAt != nil {
			newest = items[0].PublishedAt.In(a.Press.Loc).Format("02.01. 15:04")
		}
		if !s.Enabled {
			res += " (off)"
		}
		fmt.Fprintf(tw, "%s\t%s\t%d\t%s\t%s\n", s.Name, s.Kind, len(items), newest, res)
	}
	return tw.Flush()
}

// Serve runs the site, and the fetch and the print at their times every day.
func (a *App) Serve(ctx context.Context, addr, fetchAt, printAt string) error {
	fh, fm, err := clock(fetchAt)
	if err != nil {
		return err
	}
	ph, pm, err := clock(printAt)
	if err != nil {
		return err
	}
	go a.daily(ctx, fh, fm, ph, pm)

	s := &site.Site{Store: a.DB, Press: a.Press, Log: a.Log}
	mux := http.NewServeMux()
	mux.Handle("/admin/", a.Admin.Handler())
	mux.Handle("/", s.Handler())
	srv := &http.Server{Addr: addr, Handler: mux, ReadHeaderTimeout: 10 * time.Second, IdleTimeout: 2 * time.Minute}
	go func() {
		<-ctx.Done()
		shut, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		srv.Shutdown(shut)
	}()
	if has, err := a.DB.HasAdmin(ctx); err == nil && !has {
		a.Log.Print("no admin yet: run tisak -admin <name> to set one, then sign in at /admin/")
	}
	a.Log.Printf("serving on %s; fetch at %s, print at %s (Zagreb)", addr, fetchAt, printAt)
	if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}

// daily wakes for the fetch and the print. If the program starts after print time and today
// is not printed yet, it fetches and prints straight away, so a restart never skips a day.
func (a *App) daily(ctx context.Context, fh, fm, ph, pm int) {
	for {
		now := time.Now().In(a.Press.Loc)
		day := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)
		fetchT := time.Date(now.Year(), now.Month(), now.Day(), fh, fm, 0, 0, a.Press.Loc)
		printT := time.Date(now.Year(), now.Month(), now.Day(), ph, pm, 0, 0, a.Press.Loc)

		printed, err := a.DB.HasEdition(ctx, day)
		if err != nil {
			a.Log.Printf("daily: %v", err)
		}
		switch {
		case err == nil && !printed && !now.Before(printT):
			if last, _ := a.DB.LastFetch(ctx); now.Sub(last) > 2*time.Hour {
				a.run(ctx, "fetch", a.Fetch)
			}
			a.run(ctx, "print", func(ctx context.Context) error { return a.print(ctx, "") })
			continue
		case now.Before(fetchT):
			if !sleep(ctx, fetchT.Sub(now)) {
				return
			}
			a.run(ctx, "fetch", a.Fetch)
			continue
		case now.Before(printT):
			if !sleep(ctx, printT.Sub(now)) {
				return
			}
			continue // the first case prints
		}
		tomorrow := time.Date(now.Year(), now.Month(), now.Day()+1, fh, fm, 0, 0, a.Press.Loc)
		if !sleep(ctx, tomorrow.Sub(now)) {
			return
		}
		a.run(ctx, "fetch", a.Fetch)
	}
}

// SetAdmin sets the one admin's name and password. The password is asked twice on a terminal,
// or read from the first line of stdin, so it never shows in the shell's history.
func (a *App) SetAdmin(ctx context.Context, name string) error {
	name = strings.TrimSpace(name)
	if name == "" || strings.ContainsAny(name, " \t") || len([]rune(name)) > 40 {
		return errors.New("admin name: up to 40 letters, no spaces")
	}
	var pw string
	if term.IsTerminal(int(os.Stdin.Fd())) {
		fmt.Fprintf(os.Stderr, "Password for %s (at least %d letters): ", name, auth.MinPassword)
		first, err := term.ReadPassword(int(os.Stdin.Fd()))
		fmt.Fprintln(os.Stderr)
		if err != nil {
			return err
		}
		fmt.Fprint(os.Stderr, "Again: ")
		again, err := term.ReadPassword(int(os.Stdin.Fd()))
		fmt.Fprintln(os.Stderr)
		if err != nil {
			return err
		}
		if string(first) != string(again) {
			return errors.New("the passwords differ")
		}
		pw = string(first)
	} else {
		line, err := bufio.NewReader(os.Stdin).ReadString('\n')
		if err != nil && line == "" {
			return fmt.Errorf("read the password from stdin: %w", err)
		}
		pw = strings.TrimRight(line, "\r\n")
	}
	hash, err := auth.HashPassword(pw)
	if err != nil {
		return fmt.Errorf("password: %w (at least %d letters)", err, auth.MinPassword)
	}
	created, err := a.DB.SetAdmin(ctx, name, hash)
	if err != nil {
		return err
	}
	if created {
		a.Log.Printf("admin %s created; sign in at /admin/", name)
	} else {
		a.Log.Printf("admin is now %s, with the new password", name)
	}
	return nil
}

func (a *App) run(ctx context.Context, what string, f func(context.Context) error) {
	if err := f(ctx); err != nil {
		a.Log.Printf("daily %s: %v", what, err)
		sleep(ctx, 5*time.Minute) // try again, but never spin on a broken database or feed
	}
}

func sleep(ctx context.Context, d time.Duration) bool {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-t.C:
		return true
	}
}

func clock(s string) (int, int, error) {
	t, err := time.Parse("15:04", strings.TrimSpace(s))
	if err != nil {
		return 0, 0, fmt.Errorf("time %q: use HH:MM", s)
	}
	return t.Hour(), t.Minute(), nil
}
