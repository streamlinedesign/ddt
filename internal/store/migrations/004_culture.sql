-- Stage 4: films, theatres, the events wall and Billboard. Films and charts come from their
-- fetchers, theatres from each theatre's page; the admin picks the films of the week and
-- puts up the events.

-- A film as TMDB has it, in Croatian where TMDB has a translation.
CREATE TABLE films (
    tmdb_id         int PRIMARY KEY,
    title           text NOT NULL,
    original_title  text NOT NULL DEFAULT '',
    year            int,
    runtime         int,                              -- minutes
    genres          text NOT NULL DEFAULT '',         -- "Drama, Triler"
    overview        text NOT NULL DEFAULT '',
    poster          text REFERENCES media(hash),
    release_hr      date,                             -- first in Croatian cinemas
    updated_at      timestamptz NOT NULL DEFAULT now()
);

-- Films coming to Croatian cinemas, from TMDB's list; the admin can hide one.
CREATE TABLE releases (
    tmdb_id   int PRIMARY KEY REFERENCES films(tmdb_id) ON DELETE CASCADE,
    day       date NOT NULL,
    hidden    boolean NOT NULL DEFAULT false
);
CREATE INDEX releases_day ON releases (day);

-- The admin's films of the week. A cinema week starts on Thursday.
CREATE TABLE film_picks (
    week      date NOT NULL,
    tmdb_id   int NOT NULL REFERENCES films(tmdb_id) ON DELETE CASCADE,
    position  int NOT NULL DEFAULT 0,
    PRIMARY KEY (week, tmdb_id)
);

-- A show on a theatre's schedule. Each fetch replaces its theatre's shows ahead; hidden ones stay hidden.
CREATE TABLE shows (
    id        bigserial PRIMARY KEY,
    source    text NOT NULL,                          -- the theatre's site: komedija, hnk...
    theatre   text NOT NULL,
    day       date NOT NULL,
    time      text NOT NULL DEFAULT '',               -- 19:30
    title     text NOT NULL,
    link      text NOT NULL DEFAULT '',
    hidden    boolean NOT NULL DEFAULT false,
    UNIQUE (source, day, time, title)
);
CREATE INDEX shows_day ON shows (day);

-- A poster on the events wall, typed in by the admin.
CREATE TABLE events (
    id           bigserial PRIMARY KEY,
    title        text NOT NULL,
    venue        text NOT NULL DEFAULT '',
    description  text NOT NULL DEFAULT '',
    price        text NOT NULL DEFAULT '',
    link         text NOT NULL DEFAULT '',
    start_day    date NOT NULL,
    end_day      date NOT NULL,
    time         text NOT NULL DEFAULT '',
    poster       text REFERENCES media(hash),
    created_at   timestamptz NOT NULL DEFAULT now(),
    updated_at   timestamptz NOT NULL DEFAULT now(),
    CHECK (end_day >= start_day)
);
CREATE INDEX events_days ON events (end_day, start_day);

-- A week's top of a Billboard chart: hot100 or billboard200.
CREATE TABLE charts (
    chart   text NOT NULL,
    week    date NOT NULL,
    rank    int NOT NULL,
    title   text NOT NULL,
    artist  text NOT NULL DEFAULT '',
    last    int NOT NULL DEFAULT 0,                   -- last week's place, 0 when new
    peak    int NOT NULL DEFAULT 0,
    weeks   int NOT NULL DEFAULT 0,
    PRIMARY KEY (chart, week, rank)
);

INSERT INTO sources (name, kind, settings, per_edition, position, enabled) VALUES
    ('TMDB filmovi', 'tmdb', '{}', 0, 30, true),
    ('Billboard ljestvice', 'billboard', '{}', 0, 40, true),
    ('Kazalište Komedija', 'kazaliste', '{"site": "komedija"}', 0, 50, true),
    ('Kazalište Kerempuh', 'kazaliste', '{"site": "kerempuh"}', 0, 51, true),
    ('HNK Zagreb', 'kazaliste', '{"site": "hnk"}', 0, 52, true),
    ('Scenoteka', 'kazaliste', '{"site": "scenoteka"}', 0, 53, true),
    ('Luda kuća', 'kazaliste', '{"site": "ludakuca"}', 0, 54, true),
    ('Aplauz teatar', 'kazaliste', '{"site": "aplauz"}', 0, 55, true),
    ('Teatar EXIT', 'kazaliste', '{"site": "exit"}', 0, 56, true),
    ('Gavella', 'kazaliste', '{"site": "gavella"}', 0, 57, true),
    ('KNAP', 'kazaliste', '{"site": "knap"}', 0, 58, true)
ON CONFLICT (name) DO NOTHING;

-- Glazba's layout: four stories beside Billboard's top ten, then a row of three.
ALTER TABLE categories DROP CONSTRAINT categories_layout_check;
ALTER TABLE categories ADD CONSTRAINT categories_layout_check
    CHECK (layout IN ('row3', 'rows3', 'lead2', 'lead2row3', 'list', 'six', 'music'));
UPDATE categories SET layout = 'music' WHERE slug = 'glazba';
