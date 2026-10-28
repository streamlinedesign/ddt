# Digitalni Dnevni Tisak

A daily digital newspaper. One Go binary with Postgres fetches the sources at 05:15, prints the
day's edition at 05:45 (Zagreb time), and serves the site and a small dashboard. Every edition is a
folder whose `index.html` stands on its own: styles, fonts and scripts inlined, pictures beside it.

## Run it

Needs Go 1.24 and Postgres 16.

```sh
sudo -u postgres psql -c "CREATE ROLE tisak LOGIN PASSWORD 'tisak'"
sudo -u postgres psql -c "CREATE DATABASE tisak OWNER tisak"

go build -o tisak ./cmd/tisak
./tisak -migrate          # tables and the first categories and sources
./tisak -admin uriel      # the one admin; asks for the password
./tisak                   # site on :8080, dashboard at /admin/
```

Other commands: `-check` tries every source without saving, `-fetch-now` fetches once,
`-print-now` prints today's next edition (`-reason "…"` for a reprint). Settings come from flags or
`DATABASE_URL`, `TISAK_DATA`, `TISAK_ADDR`, `TISAK_SITE`, `TISAK_TRUST_PROXY`. API keys only ever
come from the environment.

## Tests

```sh
go test ./...
TISAK_TEST_DB=postgres://tisak:tisak@localhost/tisak_test go test ./...   # plus the database tests
```

The database tests also use `<name>_admin` (here `tisak_test_admin`), and wipe both.

## Layout

- `cmd/tisak`: the binary, the daily loop, the first categories and sources
- `internal/store`: Postgres and migrations
- `internal/fetch`: one fetcher per kind of source; `dhmz` (weather, 8 cities) and `apod` (NASA, no key)
  fill the side column and keep their own records
- `internal/media`: pictures, shrunk once and stored by hash
- `internal/press`: picks the day's main article, side column and cards, and writes the edition
- `internal/site`: serves editions and the archive
- `internal/auth`, `internal/admin`, `internal/jobs`: sign-in, the dashboard, fetch and print one at a time
- `web/`: the edition's templates and static files, the dashboard's templates
- `design/`: the frozen Stage 0 design the templates were cut from
