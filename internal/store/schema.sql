-- teha proof of concept schema. One account per file.
PRAGMA journal_mode = WAL;
PRAGMA foreign_keys = ON;
PRAGMA busy_timeout = 5000;

CREATE TABLE IF NOT EXISTS change_log (
  version   INTEGER PRIMARY KEY AUTOINCREMENT,
  tbl       TEXT NOT NULL,
  row_id    TEXT NOT NULL,
  op        TEXT NOT NULL,
  at        TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS applied_command (
  uuid    TEXT PRIMARY KEY,
  at      TEXT NOT NULL,
  version INTEGER NOT NULL
);

CREATE TABLE IF NOT EXISTS project (
  id         TEXT PRIMARY KEY,
  name       TEXT NOT NULL,
  color      TEXT NOT NULL DEFAULT 'grey',
  parent_id  TEXT REFERENCES project(id),
  order_key  TEXT NOT NULL,
  is_inbox   INTEGER NOT NULL DEFAULT 0,
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL,
  deleted_at TEXT,
  version    INTEGER NOT NULL
);

CREATE TABLE IF NOT EXISTS label (
  id         TEXT PRIMARY KEY,
  name       TEXT NOT NULL,
  color      TEXT NOT NULL DEFAULT 'grey',
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL,
  deleted_at TEXT,
  version    INTEGER NOT NULL
);

CREATE TABLE IF NOT EXISTS task (
  id                    TEXT PRIMARY KEY,
  project_id            TEXT NOT NULL REFERENCES project(id),
  parent_id             TEXT REFERENCES task(id),
  order_key             TEXT NOT NULL,
  title                 TEXT NOT NULL,
  description           TEXT NOT NULL DEFAULT '',
  priority              INTEGER NOT NULL DEFAULT 4,
  due_date              TEXT,
  due_time              TEXT,
  due_tz                TEXT,
  rrule                 TEXT,
  rrule_from_completion INTEGER NOT NULL DEFAULT 0,
  start_date            TEXT,
  deadline              TEXT,
  duration_min          INTEGER,
  assignee_id           TEXT,
  state                 TEXT NOT NULL DEFAULT 'open',
  completed_at          TEXT,
  deleted_at            TEXT,
  source_ref            TEXT,
  created_at            TEXT NOT NULL,
  updated_at            TEXT NOT NULL,
  version               INTEGER NOT NULL
);

CREATE TABLE IF NOT EXISTS task_label (
  task_id  TEXT NOT NULL REFERENCES task(id) ON DELETE CASCADE,
  label_id TEXT NOT NULL REFERENCES label(id) ON DELETE CASCADE,
  PRIMARY KEY (task_id, label_id)
);

CREATE INDEX IF NOT EXISTS task_by_version    ON task(version);
CREATE INDEX IF NOT EXISTS task_by_project    ON task(project_id, state);
CREATE INDEX IF NOT EXISTS task_by_due        ON task(due_date) WHERE state = 'open';
CREATE INDEX IF NOT EXISTS project_by_version ON project(version);
CREATE INDEX IF NOT EXISTS label_by_version   ON label(version);

-- A plain FTS index, written by the store on every task change. An external
-- content table needs triggers, and the store already owns every write.
CREATE VIRTUAL TABLE IF NOT EXISTS task_fts USING fts5(task_id UNINDEXED, title, description);
