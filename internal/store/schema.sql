CREATE TABLE IF NOT EXISTS projects (
    id                 INTEGER PRIMARY KEY AUTOINCREMENT,
    name               TEXT NOT NULL,
    description        TEXT NOT NULL DEFAULT '',
    dev_mode           TEXT NOT NULL DEFAULT 'single_main',
    target_branch      TEXT NOT NULL DEFAULT 'main',
    auto_pr_on_confirm INTEGER NOT NULL DEFAULT 1,
    automation_rules   TEXT NOT NULL DEFAULT '[]',
    auto_develop       INTEGER NOT NULL DEFAULT 0,
    auto_develop_agent TEXT NOT NULL DEFAULT '',
    auto_develop_labels TEXT NOT NULL DEFAULT '["bug","fix","error","crash","regression","enhancement","feature","需求","缺陷","优化","功能","报错","崩溃"]',
    auto_develop_text  INTEGER NOT NULL DEFAULT 0,
    auto_develop_authors TEXT NOT NULL DEFAULT '[]',
    auto_develop_daily_limit INTEGER NOT NULL DEFAULT 5,
    local_seq          INTEGER NOT NULL DEFAULT 0,
    created_at         TEXT NOT NULL,
    updated_at         TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS repos (
    id             INTEGER PRIMARY KEY AUTOINCREMENT,
    project_id     INTEGER NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    name           TEXT NOT NULL,
    provider       TEXT NOT NULL,
    remote_url     TEXT NOT NULL,
    owner_repo     TEXT NOT NULL DEFAULT '',
    default_branch TEXT NOT NULL DEFAULT 'main',
    test_command   TEXT NOT NULL DEFAULT '',
    UNIQUE(project_id, name)
);

CREATE TABLE IF NOT EXISTS issues (
    id            INTEGER PRIMARY KEY AUTOINCREMENT,
    project_id    INTEGER NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    key           TEXT NOT NULL,
    source        TEXT NOT NULL,
    repo_id       INTEGER REFERENCES repos(id) ON DELETE SET NULL,
    parent_issue_id INTEGER REFERENCES issues(id) ON DELETE SET NULL,
    remote_number INTEGER,
    remote_url    TEXT NOT NULL DEFAULT '',
    title         TEXT NOT NULL,
    body          TEXT NOT NULL DEFAULT '',
    labels        TEXT NOT NULL DEFAULT '[]',
    priority      TEXT NOT NULL DEFAULT 'normal',
    status        TEXT NOT NULL,
    created_at    TEXT NOT NULL,
    updated_at    TEXT NOT NULL,
    archived_at   TEXT,
    UNIQUE(project_id, key)
);

CREATE TABLE IF NOT EXISTS issue_events (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    issue_id   INTEGER NOT NULL REFERENCES issues(id) ON DELETE CASCADE,
    kind       TEXT NOT NULL,
    actor      TEXT NOT NULL DEFAULT 'system',
    message    TEXT NOT NULL DEFAULT '',
    created_at TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS issue_pr_links (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    issue_id   INTEGER NOT NULL REFERENCES issues(id) ON DELETE CASCADE,
    url        TEXT NOT NULL,
    title      TEXT NOT NULL DEFAULT '',
    state      TEXT NOT NULL DEFAULT 'open',
    created_at TEXT NOT NULL,
    UNIQUE(issue_id, url)
);

CREATE TABLE IF NOT EXISTS issue_attachments (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    issue_id   INTEGER NOT NULL REFERENCES issues(id) ON DELETE CASCADE,
    name       TEXT NOT NULL,
    mime       TEXT NOT NULL,
    size       INTEGER NOT NULL,
    path       TEXT NOT NULL,
    created_at TEXT NOT NULL
);

CREATE INDEX IF NOT EXISTS idx_issue_events_issue ON issue_events(issue_id);
CREATE INDEX IF NOT EXISTS idx_issue_attachments_issue ON issue_attachments(issue_id);

CREATE TABLE IF NOT EXISTS comments (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    issue_id   INTEGER NOT NULL REFERENCES issues(id) ON DELETE CASCADE,
    author     TEXT NOT NULL,
    body       TEXT NOT NULL,
    remote_id  TEXT NOT NULL DEFAULT '',
    remote_url TEXT NOT NULL DEFAULT '',
    created_at TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS runs (
    id            INTEGER PRIMARY KEY AUTOINCREMENT,
    issue_id      INTEGER NOT NULL REFERENCES issues(id) ON DELETE CASCADE,
    agent         TEXT NOT NULL,
    model         TEXT NOT NULL DEFAULT '',
    status        TEXT NOT NULL,
    branch        TEXT NOT NULL DEFAULT '',
    target_branch TEXT NOT NULL DEFAULT '',
    workdir       TEXT NOT NULL DEFAULT '',
    session_id    TEXT NOT NULL DEFAULT '',
    summary       TEXT NOT NULL DEFAULT '',
    error         TEXT NOT NULL DEFAULT '',
    pr_url        TEXT NOT NULL DEFAULT '',
    pr_state      TEXT NOT NULL DEFAULT '',
    change_record TEXT NOT NULL DEFAULT '',
    auto_pr       INTEGER NOT NULL DEFAULT 0,
    runner_id     TEXT NOT NULL DEFAULT '',
    input_tokens  INTEGER NOT NULL DEFAULT 0,
    output_tokens INTEGER NOT NULL DEFAULT 0,
    total_tokens  INTEGER NOT NULL DEFAULT 0,
    context_used  INTEGER NOT NULL DEFAULT 0,
    context_size  INTEGER NOT NULL DEFAULT 0,
    cost_amount   REAL NOT NULL DEFAULT 0,
    cost_currency TEXT NOT NULL DEFAULT '',
    usage_cumulative_cost REAL NOT NULL DEFAULT 0,
    usage_json    TEXT NOT NULL DEFAULT '',
    usage_updated_at TEXT,
    created_at    TEXT NOT NULL,
    started_at    TEXT,
    ended_at      TEXT
);
CREATE INDEX IF NOT EXISTS idx_runs_issue ON runs(issue_id);
CREATE INDEX IF NOT EXISTS idx_runs_created ON runs(created_at);

CREATE TABLE IF NOT EXISTS run_events (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    run_id     INTEGER NOT NULL REFERENCES runs(id) ON DELETE CASCADE,
    seq        INTEGER NOT NULL,
    type       TEXT NOT NULL,
    data       TEXT NOT NULL,
    created_at TEXT NOT NULL,
    UNIQUE(run_id, seq)
);

CREATE INDEX IF NOT EXISTS idx_comments_issue ON comments(issue_id);
CREATE INDEX IF NOT EXISTS idx_issues_repo ON issues(repo_id);

CREATE TABLE IF NOT EXISTS branch_watches (
    id           INTEGER PRIMARY KEY AUTOINCREMENT,
    project_id   INTEGER NOT NULL REFERENCES projects(id) ON DELETE CASCADE,
    repo_id      INTEGER NOT NULL REFERENCES repos(id) ON DELETE CASCADE,
    branch       TEXT NOT NULL,
    last_sha     TEXT NOT NULL DEFAULT '',
    last_message TEXT NOT NULL DEFAULT '',
    last_author  TEXT NOT NULL DEFAULT '',
    updated_at   TEXT NOT NULL,
    UNIQUE(repo_id, branch)
);

CREATE TABLE IF NOT EXISTS notifications (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    kind       TEXT NOT NULL,
    title      TEXT NOT NULL,
    body       TEXT NOT NULL DEFAULT '',
    link       TEXT NOT NULL DEFAULT '',
    meta       TEXT NOT NULL DEFAULT '',
    read       INTEGER NOT NULL DEFAULT 0,
    created_at TEXT NOT NULL
);
CREATE INDEX IF NOT EXISTS idx_notifications_created ON notifications(id DESC);

CREATE TABLE IF NOT EXISTS pr_handled_events (
    id         INTEGER PRIMARY KEY AUTOINCREMENT,
    pr_url     TEXT NOT NULL,
    event_key  TEXT NOT NULL,
    action_log TEXT NOT NULL DEFAULT '',
    created_at TEXT NOT NULL,
    UNIQUE(pr_url, event_key)
);
CREATE INDEX IF NOT EXISTS idx_pr_handled_events ON pr_handled_events(pr_url, event_key);



CREATE TABLE IF NOT EXISTS jobs (
    id          INTEGER PRIMARY KEY AUTOINCREMENT,
    kind        TEXT NOT NULL,
    status      TEXT NOT NULL,
    repo_id     INTEGER NOT NULL DEFAULT 0,
    payload     TEXT NOT NULL DEFAULT '{}',
    error       TEXT NOT NULL DEFAULT '',
    created_at  TEXT NOT NULL,
    started_at  TEXT,
    finished_at TEXT
);

CREATE INDEX IF NOT EXISTS idx_jobs_status ON jobs(status);
