-- The task manager (internal/tasks): workspaces, their
-- boards, the work items on them, the items' updates, and the activity log.
--
-- Each row keeps the whole record as JSON (body), with beside it only the
-- columns something is looked up or ordered by: which board, which parent,
-- whose, when due, where in its column, and the version a save must match.
-- The model grows -- a field, a link kind -- without a migration, and the
-- rules about what is valid are internal/tasks's, not the table's.
--
-- A workspace's boards and a board's items go with it (ON DELETE CASCADE),
-- and an item's subtasks and updates with the item. The activity log does
-- not: what was done stays on record after the board it was done on is gone.

CREATE TABLE IF NOT EXISTS tm_workspaces (
    id         TEXT        PRIMARY KEY,
    body       JSONB       NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE IF NOT EXISTS tm_boards (
    id           TEXT        PRIMARY KEY,
    workspace_id TEXT        NOT NULL REFERENCES tm_workspaces (id) ON DELETE CASCADE,
    body         JSONB       NOT NULL,
    updated_at   TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS tm_boards_workspace ON tm_boards (workspace_id);

CREATE TABLE IF NOT EXISTS tm_items (
    id         TEXT             PRIMARY KEY,
    board_id   TEXT             NOT NULL REFERENCES tm_boards (id) ON DELETE CASCADE,
    parent_id  TEXT             REFERENCES tm_items (id) ON DELETE CASCADE,
    title      TEXT             NOT NULL,
    assignees  TEXT[]           NOT NULL DEFAULT '{}',
    due        TEXT             NOT NULL DEFAULT '',
    position   DOUBLE PRECISION NOT NULL DEFAULT 0,
    version    INT              NOT NULL,
    created_at TIMESTAMPTZ      NOT NULL,
    body       JSONB            NOT NULL
);
CREATE INDEX IF NOT EXISTS tm_items_board     ON tm_items (board_id, position);
CREATE INDEX IF NOT EXISTS tm_items_parent    ON tm_items (parent_id);
CREATE INDEX IF NOT EXISTS tm_items_assignees ON tm_items USING GIN (assignees);
CREATE INDEX IF NOT EXISTS tm_items_due       ON tm_items (due);

CREATE TABLE IF NOT EXISTS tm_comments (
    id         TEXT        PRIMARY KEY,
    item_id    TEXT        NOT NULL REFERENCES tm_items (id) ON DELETE CASCADE,
    created_at TIMESTAMPTZ NOT NULL,
    body       JSONB       NOT NULL
);
CREATE INDEX IF NOT EXISTS tm_comments_item ON tm_comments (item_id, created_at);

CREATE TABLE IF NOT EXISTS tm_activity (
    id       TEXT        PRIMARY KEY,
    board_id TEXT        NOT NULL DEFAULT '',
    item_id  TEXT        NOT NULL DEFAULT '',
    actor    TEXT        NOT NULL,
    action   TEXT        NOT NULL,
    detail   TEXT        NOT NULL DEFAULT '',
    at       TIMESTAMPTZ NOT NULL
);
CREATE INDEX IF NOT EXISTS tm_activity_board ON tm_activity (board_id, at DESC);
CREATE INDEX IF NOT EXISTS tm_activity_item  ON tm_activity (item_id, at DESC);
