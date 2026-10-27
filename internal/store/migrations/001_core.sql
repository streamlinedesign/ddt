-- Stage 1: the core print. Categories hold sources, sources give items,
-- the press turns them into editions. Pictures live on disk by hash; tables keep the hash.

CREATE TABLE media (
    hash        text PRIMARY KEY,          -- sha256 of the stored file, hex
    ext         text NOT NULL,             -- jpg, png, svg
    width       int,
    height      int,
    bytes       int NOT NULL,
    source_url  text NOT NULL DEFAULT '',
    created_at  timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE categories (
    id            bigserial PRIMARY KEY,
    slug          text NOT NULL UNIQUE,
    title         text NOT NULL,
    flag          text REFERENCES media(hash),   -- title image, 32 px tall
    position      int NOT NULL DEFAULT 0,
    layout        text NOT NULL DEFAULT 'row3'
                  CHECK (layout IN ('row3', 'rows3', 'lead2', 'lead2row3', 'list', 'six')),
    lead_picture  boolean NOT NULL DEFAULT false,
    in_menu       boolean NOT NULL DEFAULT true,
    enabled       boolean NOT NULL DEFAULT true,
    created_at    timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE sources (
    id             bigserial PRIMARY KEY,
    category_id    bigint REFERENCES categories(id) ON DELETE SET NULL,
    name           text NOT NULL UNIQUE,
    logo           text REFERENCES media(hash),
    kind           text NOT NULL,                  -- fetcher kind: rss, hn
    settings       jsonb NOT NULL DEFAULT '{}',    -- {"url": "..."} and the like
    per_edition    int NOT NULL DEFAULT 3 CHECK (per_edition BETWEEN 0 AND 24),
    position       int NOT NULL DEFAULT 0,
    enabled        boolean NOT NULL DEFAULT true,
    last_fetch_at  timestamptz,
    last_ok        boolean,
    last_error     text NOT NULL DEFAULT '',
    last_count     int NOT NULL DEFAULT 0,
    created_at     timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE items (
    id            bigserial PRIMARY KEY,
    source_id     bigint NOT NULL REFERENCES sources(id) ON DELETE CASCADE,
    guid          text NOT NULL,
    title         text NOT NULL,
    summary       text NOT NULL DEFAULT '',
    link          text NOT NULL,
    author        text NOT NULL DEFAULT '',
    published_at  timestamptz,
    fetched_at    timestamptz NOT NULL DEFAULT now(),
    picture_url   text NOT NULL DEFAULT '',
    picture       text REFERENCES media(hash),
    extra         jsonb NOT NULL DEFAULT '{}',     -- per kind, e.g. Hacker News points
    hidden        boolean NOT NULL DEFAULT false,
    UNIQUE (source_id, guid)
);
CREATE INDEX items_source_published ON items (source_id, published_at DESC);
CREATE INDEX items_fetched ON items (fetched_at DESC);

CREATE TABLE editions (
    id          bigserial PRIMARY KEY,
    day         date NOT NULL,
    number      int NOT NULL,                 -- Br.: days since the first print, counting from 1
    version     int NOT NULL,                 -- Verzija: 1, then one more for each reprint of the day
    reason      text NOT NULL DEFAULT '',
    snapshot    jsonb NOT NULL,
    html        text NOT NULL,
    bytes       int NOT NULL,
    printed_at  timestamptz NOT NULL DEFAULT now(),
    UNIQUE (day, version)
);
