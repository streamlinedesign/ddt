-- Stage 5: sport and play. Matches come from TheSportsDB, the game's word is drawn at print time,
-- the admin writes the dinner, and readers write in through Kontakt.

-- A match of a followed team: played (with its score and scorers) or ahead.
CREATE TABLE matches (
    event_id    text PRIMARY KEY,                 -- TheSportsDB's idEvent
    source      text NOT NULL,                    -- the source's name, for clearing out
    team        text NOT NULL,                    -- the followed team, as TheSportsDB names it
    league      text NOT NULL DEFAULT '',
    round       text NOT NULL DEFAULT '',
    home        text NOT NULL,
    away        text NOT NULL,
    home_score  int,
    away_score  int,
    day         date NOT NULL,                    -- Zagreb's date of the kick-off
    time        text NOT NULL DEFAULT '',         -- Zagreb's time: "20:45"
    venue       text NOT NULL DEFAULT '',
    goals       jsonb NOT NULL DEFAULT '[]',      -- [{"minute": 17, "player": "...", "home": true, "kind": "penalty"}]
    updated_at  timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX matches_day ON matches (day);

-- Riječililica: one word a day, drawn by the press, never twice within a year.
CREATE TABLE game_words (
    day     date PRIMARY KEY,
    word    text NOT NULL,
    number  int NOT NULL UNIQUE
);

-- Večera: three courses and a cocktail, from a day until the next dinner.
CREATE TABLE dinners (
    id          bigserial PRIMARY KEY,
    day         date NOT NULL UNIQUE,
    courses     jsonb NOT NULL DEFAULT '[]',     -- [{"kind": "starter", "title": "...", "ingredients": [...], ...}]
    created_at  timestamptz NOT NULL DEFAULT now(),
    updated_at  timestamptz NOT NULL DEFAULT now()
);

-- Messages readers send through Kontakt.
CREATE TABLE messages (
    id          bigserial PRIMARY KEY,
    name        text NOT NULL,
    email       text NOT NULL,
    subject     text NOT NULL DEFAULT '',
    body        text NOT NULL,
    ip          text NOT NULL DEFAULT '',
    created_at  timestamptz NOT NULL DEFAULT now(),
    read_at     timestamptz,
    archived    boolean NOT NULL DEFAULT false
);
CREATE INDEX messages_new ON messages (created_at DESC) WHERE NOT archived;

-- Small texts the admin sets once: Kontakt's addresses and hours.
CREATE TABLE settings (
    key    text PRIMARY KEY,
    value  text NOT NULL
);

-- Croatia's national team, the first team followed; more are added under Izvori.
INSERT INTO sources (name, kind, settings, per_edition, position, enabled) VALUES
    ('TheSportsDB Hrvatska', 'sportsdb', '{"team": "133912"}', 0, 60, true)
ON CONFLICT (name) DO NOTHING;

-- Sport's layout: news around the match cards, then two rows of three.
ALTER TABLE categories DROP CONSTRAINT categories_layout_check;
ALTER TABLE categories ADD CONSTRAINT categories_layout_check
    CHECK (layout IN ('row3', 'rows3', 'lead2', 'lead2row3', 'list', 'six', 'music', 'sport'));
UPDATE categories SET layout = 'sport' WHERE slug = 'sport';
