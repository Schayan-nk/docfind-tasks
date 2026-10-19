-- Departments: created, renamed, moved and deleted on the users page. A
-- person may belong to several, one marked primary. oa_id is a department's
-- id in a directory it was imported from, if any.
CREATE TABLE IF NOT EXISTS departments (
    id         SERIAL PRIMARY KEY,
    oa_id      BIGINT UNIQUE,
    parent_id  INTEGER REFERENCES departments(id) ON DELETE SET NULL,
    name       TEXT NOT NULL,
    code       TEXT NOT NULL DEFAULT '',
    -- 'unit' (a department) or 'position' (a post within one), as the
    -- directory has them; departments made here are units.
    kind       TEXT NOT NULL DEFAULT 'unit',
    active     BOOLEAN NOT NULL DEFAULT TRUE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS departments_parent ON departments (parent_id);

CREATE TABLE IF NOT EXISTS user_departments (
    username      TEXT NOT NULL,
    department_id INTEGER NOT NULL REFERENCES departments(id) ON DELETE CASCADE,
    is_primary    BOOLEAN NOT NULL DEFAULT FALSE,
    PRIMARY KEY (username, department_id)
);
CREATE INDEX IF NOT EXISTS user_departments_dept ON user_departments (department_id);
