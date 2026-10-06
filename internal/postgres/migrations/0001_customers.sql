CREATE TABLE customers (
    id         uuid        PRIMARY KEY,
    name       text        NOT NULL,
    email      text        NOT NULL,
    document   text        NOT NULL,
    phone      text        NOT NULL DEFAULT '',
    street     text        NOT NULL DEFAULT '',
    number     text        NOT NULL DEFAULT '',
    city       text        NOT NULL DEFAULT '',
    state      text        NOT NULL DEFAULT '',
    zip_code   text        NOT NULL DEFAULT '',
    status     text        NOT NULL CHECK (status IN ('active', 'inactive')),
    created_at timestamptz NOT NULL,
    updated_at timestamptz NOT NULL,
    CONSTRAINT customers_email_key    UNIQUE (email),
    CONSTRAINT customers_document_key UNIQUE (document)
);

-- Matches the list order (newest first, id as tie-breaker) so pages are stable.
CREATE INDEX customers_created_idx ON customers (created_at DESC, id DESC);
