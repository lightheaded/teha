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

-- An account, and the passkeys that unlock it. The proof of concept holds one
-- account, the owner. A second account belongs to milestone M6, so this table
-- exists to give a credential an owner and not to model sharing.
CREATE TABLE IF NOT EXISTS account (
  id           TEXT PRIMARY KEY,
  user_handle  BLOB NOT NULL,
  name         TEXT NOT NULL,
  display_name TEXT NOT NULL,
  created_at   TEXT NOT NULL
);

-- One row per passkey. The id is the raw credential id in base64url, so a
-- browser and this table name the same credential with the same string.
--
-- A credential carries no version and never enters change_log. A client syncs
-- tasks, not the keys that unlock the account.
CREATE TABLE IF NOT EXISTS credential (
  id                 TEXT PRIMARY KEY,
  account_id         TEXT NOT NULL REFERENCES account(id) ON DELETE CASCADE,
  public_key         BLOB NOT NULL,
  sign_count         INTEGER NOT NULL DEFAULT 0,
  transports         TEXT NOT NULL DEFAULT '',
  aaguid             TEXT NOT NULL DEFAULT '',
  name               TEXT NOT NULL,
  -- flags holds the authenticator flag byte from registration. The backup
  -- eligibility bit must not change over the life of a credential, and a
  -- login cannot check that rule without the value from the first ceremony.
  flags              INTEGER NOT NULL DEFAULT 0,
  attestation_type   TEXT NOT NULL DEFAULT '',
  attestation_format TEXT NOT NULL DEFAULT '',
  created_at         TEXT NOT NULL,
  last_used_at       TEXT
);

CREATE INDEX IF NOT EXISTS credential_by_account ON credential(account_id);

-- A reminder fires one notification at one moment. It is account data, so it
-- carries a version and takes part in the change log and in sync, exactly like
-- a task.
--
-- Once only. The claim predicate is
--
--   fire_at <= now AND (sent_at IS NULL OR sent_at < fire_at)
--
-- and the claim sets sent_at = now inside the same transaction that reads the
-- row. now >= fire_at at that moment, so the predicate is false for ever after
-- for a one-shot reminder. A daily digest also moves fire_at forward by one
-- day in that same transaction, so it becomes claimable again tomorrow and
-- never twice for the same day. See docs/DECISIONS.md D-010.
CREATE TABLE IF NOT EXISTS reminder (
  id         TEXT PRIMARY KEY,
  task_id    TEXT REFERENCES task(id),  -- null for a daily digest
  kind       TEXT NOT NULL DEFAULT 'at_due',  -- at_due, before_due, daily_digest
  fire_at    TEXT NOT NULL,  -- RFC 3339 in UTC, the moment it fires
  offset_min INTEGER,        -- before_due: how many minutes before the due time
  sent_at    TEXT,           -- the once-only marker. See above
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL,
  deleted_at TEXT,
  version    INTEGER NOT NULL
);

CREATE INDEX IF NOT EXISTS reminder_by_version ON reminder(version);
CREATE INDEX IF NOT EXISTS reminder_by_task    ON reminder(task_id);
CREATE INDEX IF NOT EXISTS reminder_due        ON reminder(fire_at) WHERE deleted_at IS NULL;

-- One Web Push subscription per browser or installed web app. This table is
-- deliberately OUTSIDE sync and outside the change log: the endpoint plus the
-- two keys are enough to push to that device, they are per-device plumbing
-- rather than account data, and no client has a use for another device's
-- subscription. Nothing here is worth putting into every client's local copy.
CREATE TABLE IF NOT EXISTS push_subscription (
  endpoint     TEXT PRIMARY KEY,
  p256dh       TEXT NOT NULL,
  auth         TEXT NOT NULL,
  user_agent   TEXT NOT NULL DEFAULT '',
  created_at   TEXT NOT NULL,
  last_used_at TEXT,
  fail_count   INTEGER NOT NULL DEFAULT 0,
  -- retry_until holds the back-off deadline a 429 asked for. It is stored, not
  -- kept in memory, so a restart still honours Retry-After.
  retry_until  TEXT
);
