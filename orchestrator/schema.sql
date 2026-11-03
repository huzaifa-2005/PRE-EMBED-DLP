CREATE EXTENSION IF NOT EXISTS vector;

CREATE TABLE IF NOT EXISTS documents (
    id      SERIAL PRIMARY KEY,
    name    TEXT NOT NULL,
    version INT  NOT NULL DEFAULT 1,
    status  TEXT NOT NULL DEFAULT 'ingested'
);

CREATE TABLE IF NOT EXISTS entities (
    id       SERIAL PRIMARY KEY,
    doc_id   INT  NOT NULL REFERENCES documents(id) ON DELETE CASCADE,
    type     TEXT NOT NULL,
    value    TEXT NOT NULL,
    decision TEXT NOT NULL DEFAULT 'redact'
);

CREATE TABLE IF NOT EXISTS document_versions (
    id             SERIAL PRIMARY KEY,
    doc_id         INT  NOT NULL REFERENCES documents(id) ON DELETE CASCADE,
    version        INT  NOT NULL,
    sanitized_text TEXT NOT NULL,
    created_at     TIMESTAMP NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS audit_log (
    id     SERIAL PRIMARY KEY,
    action TEXT NOT NULL,
    doc_id INT,
    time   TIMESTAMP NOT NULL DEFAULT NOW()
);

-- pgvector store (second vector DB). 1024 dims; see note in section B.
CREATE TABLE IF NOT EXISTS pg_chunks (
    id         SERIAL PRIMARY KEY,
    doc_id     TEXT NOT NULL,
    collection TEXT NOT NULL,
    content    TEXT NOT NULL,
    embedding  vector(1024)
);