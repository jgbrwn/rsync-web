-- Base schema
--
-- Migrations tracking table
CREATE TABLE IF NOT EXISTS migrations (
    migration_number INTEGER PRIMARY KEY,
    migration_name TEXT NOT NULL,
    executed_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
);

-- Rsync command history
CREATE TABLE IF NOT EXISTS rsync_history (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    source TEXT NOT NULL,
    destination TEXT NOT NULL,
    options TEXT NOT NULL,  -- JSON array of options
    full_command TEXT NOT NULL,
    status TEXT NOT NULL DEFAULT 'pending',  -- pending, running, completed, failed, cancelled
    exit_code INTEGER,
    output TEXT,
    started_at TIMESTAMP,
    completed_at TIMESTAMP,
    created_at TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX idx_rsync_history_created_at ON rsync_history(created_at DESC);
CREATE INDEX idx_rsync_history_status ON rsync_history(status);

-- Record execution of this migration
INSERT OR IGNORE INTO migrations (migration_number, migration_name)
VALUES (001, '001-base');
