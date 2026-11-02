-- uniTech ilk şema
CREATE TABLE users (
    id            serial PRIMARY KEY,
    name          text        NOT NULL,
    email         text        NOT NULL UNIQUE,
    pass_hash     text        NOT NULL,
    role          text        NOT NULL DEFAULT 'student' CHECK (role IN ('student', 'staff', 'admin')),
    verified      boolean     NOT NULL DEFAULT false,
    token_version integer     NOT NULL DEFAULT 0,
    created_at    timestamptz NOT NULL DEFAULT now()
);

CREATE TABLE tickets (
    id          serial PRIMARY KEY,
    code        text        NOT NULL UNIQUE,
    category    text        NOT NULL,
    building    text        NOT NULL,
    room        text        NOT NULL DEFAULT '',
    title       text        NOT NULL,
    description text        NOT NULL,
    priority    text        NOT NULL CHECK (priority IN ('dusuk', 'orta', 'acil')),
    status      text        NOT NULL DEFAULT 'yeni' CHECK (status IN ('yeni', 'islemde', 'cozuldu')),
    reporter_id integer     NOT NULL REFERENCES users (id),
    assignee_id integer     REFERENCES users (id) ON DELETE SET NULL,
    created_at  timestamptz NOT NULL DEFAULT now(),
    updated_at  timestamptz NOT NULL DEFAULT now(),
    resolved_at timestamptz
);
CREATE INDEX tickets_status_idx   ON tickets (status);
CREATE INDEX tickets_reporter_idx ON tickets (reporter_id);
CREATE INDEX tickets_assignee_idx ON tickets (assignee_id);
CREATE INDEX tickets_created_idx  ON tickets (created_at DESC);

CREATE TABLE ticket_events (
    id        bigserial PRIMARY KEY,
    ticket_id integer     NOT NULL REFERENCES tickets (id) ON DELETE CASCADE,
    at        timestamptz NOT NULL DEFAULT now(),
    by_id     integer     NOT NULL REFERENCES users (id),
    status    text        NOT NULL DEFAULT '',
    note      text        NOT NULL DEFAULT '',
    action    text        NOT NULL DEFAULT ''
);
CREATE INDEX ticket_events_ticket_idx ON ticket_events (ticket_id, at);

CREATE TABLE photos (
    id           serial PRIMARY KEY,
    ticket_id    integer     NOT NULL REFERENCES tickets (id) ON DELETE CASCADE,
    uploader_id  integer     NOT NULL REFERENCES users (id),
    content_type text        NOT NULL,
    width        integer     NOT NULL,
    height       integer     NOT NULL,
    data         bytea       NOT NULL,
    created_at   timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX photos_ticket_idx ON photos (ticket_id);

CREATE TABLE auth_codes (
    user_id    integer     NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    purpose    text        NOT NULL CHECK (purpose IN ('verify', 'reset')),
    code_hash  text        NOT NULL,
    expires_at timestamptz NOT NULL,
    attempts   integer     NOT NULL DEFAULT 0,
    sent_at    timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (user_id, purpose)
);
