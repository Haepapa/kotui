-- Migration 002: Journal embeddings for agent memory

CREATE TABLE IF NOT EXISTS journal_embeddings (
    id          TEXT PRIMARY KEY,
    agent_id    TEXT NOT NULL,
    project_id  TEXT NOT NULL,
    content     TEXT NOT NULL,
    embedding   TEXT NOT NULL,  -- JSON array of float32
    is_feedback INTEGER NOT NULL DEFAULT 0,
    created_at  DATETIME NOT NULL DEFAULT (datetime('now'))
);

CREATE INDEX IF NOT EXISTS idx_journal_embeddings_agent ON journal_embeddings(agent_id, project_id);
