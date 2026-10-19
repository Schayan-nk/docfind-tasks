-- Who heads each department (1405/07/14), for a process step that goes to
-- "the requester's manager" (internal/tasks/flow.go): the head of the
-- requester's primary department, or of the one above when they head it
-- themselves. A docfind username; '' is nobody set.
ALTER TABLE departments ADD COLUMN IF NOT EXISTS head TEXT NOT NULL DEFAULT '';
