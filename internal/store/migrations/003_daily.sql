-- Stage 3: the main article and the side column. The admin writes articles, the word and the
-- album, each for a date; weather and NASA's picture are fetched each morning.

CREATE TABLE articles (
    id              bigserial PRIMARY KEY,
    title           text NOT NULL,
    description     text NOT NULL DEFAULT '',
    author          text NOT NULL DEFAULT '',
    picture         text REFERENCES media(hash),
    picture_alt     text NOT NULL DEFAULT '',
    picture_credit  text NOT NULL DEFAULT '',
    blocks          jsonb NOT NULL DEFAULT '[]',    -- [{"type": "p", "text": "..."}, ...]
    day             date UNIQUE,                     -- the day it is the main article, if any
    pinned          boolean NOT NULL DEFAULT false,  -- the main article of every day without its own
    created_at      timestamptz NOT NULL DEFAULT now(),
    updated_at      timestamptz NOT NULL DEFAULT now()
);
CREATE UNIQUE INDEX articles_one_pinned ON articles (pinned) WHERE pinned;

CREATE TABLE words (
    id           bigserial PRIMARY KEY,
    day          date NOT NULL UNIQUE,
    word         text NOT NULL,
    link         text NOT NULL DEFAULT '',
    description  text NOT NULL DEFAULT '',
    created_at   timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE albums (
    id          bigserial PRIMARY KEY,
    day         date NOT NULL UNIQUE,
    artist      text NOT NULL,
    title       text NOT NULL,
    year        int,
    genre       text NOT NULL DEFAULT '',
    minutes     int,
    tracks      int,
    cover       text REFERENCES media(hash),
    link        text NOT NULL DEFAULT '',
    created_at  timestamptz NOT NULL DEFAULT now()
);

-- One city's forecast for one day, as the press prints it.
CREATE TABLE weather (
    city        text NOT NULL,
    day         date NOT NULL,
    data        jsonb NOT NULL,
    fetched_at  timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (city, day)
);

-- NASA's Astronomy Picture of the Day.
CREATE TABLE apod (
    day          date PRIMARY KEY,
    title        text NOT NULL,
    explanation  text NOT NULL DEFAULT '',
    credit       text NOT NULL DEFAULT '',
    link         text NOT NULL DEFAULT '',
    media_type   text NOT NULL DEFAULT 'image',
    picture_url  text NOT NULL DEFAULT '',
    picture      text REFERENCES media(hash),
    alt          text NOT NULL DEFAULT '',
    fetched_at   timestamptz NOT NULL DEFAULT now()
);

-- The side column's fetchers are sources without a category, so the dashboard shows their health.
INSERT INTO sources (name, kind, settings, per_edition, position, enabled)
VALUES ('DHMZ prognoza', 'dhmz', '{}', 0, 10, true),
       ('NASA slika dana', 'apod', '{"url": "https://science.nasa.gov/wp-json/wp/v2/apod-basic?per_page=3"}', 0, 20, true)
ON CONFLICT (name) DO NOTHING;
