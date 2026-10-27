-- Stage 2: the dashboard. One admin; sessions keep only a hash of their token.

CREATE TABLE admins (
    id             bigserial PRIMARY KEY,
    username       text NOT NULL UNIQUE,
    password_hash  text NOT NULL,
    created_at     timestamptz NOT NULL DEFAULT now(),
    updated_at     timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE sessions (
    token_hash  text PRIMARY KEY,            -- sha256 of the cookie value, hex
    admin_id    bigint NOT NULL REFERENCES admins(id) ON DELETE CASCADE,
    csrf        text NOT NULL,
    user_agent  text NOT NULL DEFAULT '',
    ip          text NOT NULL DEFAULT '',
    created_at  timestamptz NOT NULL DEFAULT now(),
    seen_at     timestamptz NOT NULL DEFAULT now(),
    expires_at  timestamptz NOT NULL
);
CREATE INDEX sessions_admin ON sessions (admin_id);
