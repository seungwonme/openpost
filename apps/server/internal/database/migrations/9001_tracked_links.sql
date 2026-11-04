CREATE TABLE IF NOT EXISTS tracked_links (
  id TEXT PRIMARY KEY,
  workspace_id TEXT NOT NULL REFERENCES workspaces(id) ON DELETE CASCADE,
  publication_id TEXT NOT NULL REFERENCES publications(id) ON DELETE CASCADE,
  rendition_id TEXT REFERENCES renditions(id) ON DELETE SET NULL,
  slug TEXT NOT NULL UNIQUE,
  destination_url TEXT NOT NULL,
  created_by_id TEXT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
  is_active BOOLEAN NOT NULL DEFAULT TRUE,
  created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
  updated_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX IF NOT EXISTS tracked_links_workspace_publication_idx
  ON tracked_links (workspace_id, publication_id);

CREATE TABLE IF NOT EXISTS tracked_link_clicks (
  id TEXT PRIMARY KEY,
  tracked_link_id TEXT NOT NULL REFERENCES tracked_links(id) ON DELETE CASCADE,
  occurred_at TIMESTAMP NOT NULL
);

CREATE INDEX IF NOT EXISTS tracked_link_clicks_link_occurred_idx
  ON tracked_link_clicks (tracked_link_id, occurred_at);
