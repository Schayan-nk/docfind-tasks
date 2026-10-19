# docfind-tasks

A self-hosted task manager and workflow engine written in Go, with a Persian
(right-to-left) interface. Teams keep their work on boards (Kanban, table,
calendar, timeline), take requests through forms, and run their own processes:
drawn in a BPMN designer in the browser, each step handed to a person, a
department or the requester's manager, and waiting in that person's inbox.

It began as the task manager inside DocFind, a document and workflow system,
and runs here on its own: one binary and a PostgreSQL database.

## What it does

**Boards and items**
- Workspaces with their members (owner, member, viewer), and boards in them,
  each with its columns, custom fields, labels and groups
- Seven views of a board: Kanban, table, list, a Jalali calendar, a timeline,
  the team's workload, and its activity
- Items with assignees and watchers, priority, start and due days, estimated
  and spent hours, a checklist, subtasks, dependencies (finish to start and
  the other three, blocks, related, duplicate) and links to what the item is
  about outside the board: a customer, an order, an email, a document, a
  contract, or a page of this site
- Comments that name people with `@`, an activity log on every item, "my
  work" across every board, and items found from the Ctrl+K palette
- Rules: when an item is created, moved, assigned, commented on, due soon or
  overdue, move it, set its priority, due day or group, assign people, add
  labels, watchers, a checklist line, a subtask or a comment, start or stop
  its timer, or notify somebody. Buttons on an item do the same when pressed
  (Start, Complete, Escalate, or a board's own)
- A timer per person and a timesheet of the last thirty days
- Approvals in steps, each signed by one of its approvers or by all of them
- Templates: a tender, a technical offer and a sales pipeline built in, and
  any board marked as a template
- A dashboard across every board the reader may see: open, overdue and done,
  by board, by person and by column
- Webhooks: a board's events posted to an address, signed with HMAC-SHA256

**Forms**
- Intake forms on a board, whose answers become items; a form can be open to
  everybody who holds the task manager, not only the board's members
- Questions: text, number, money, date, single and multiple choice, a
  checkbox, a person, a department, a table of rows, a heading, a calculated
  value, and files (up to 25 MB each)
- A question can be shown or required only when other answers say so, and a
  calculated one is worked out from the others as people type: `+ - * / %`,
  `round`, `floor`, `ceil`, `abs`, `min`, `max` and `if`
- The answers of a form as a CSV, with Jalali dates

**Processes**
- A board can run a process: steps, gateways (the first route whose condition
  holds, or all of them in parallel, and joins) and ends, drawn in the
  browser
- A step goes to named people, to the person a question names, to a
  department (anybody in it claims the step), or to the requester's manager:
  the head of their primary department, or of the one above it when they
  head their own
- Conditions are written as BPM tools write them, `@@amount > 5000 &&
  in_array("2", @@buy_via)`, or picked from a list; a step can set a value,
  notify people or call a webhook when it opens or closes
- Every open step waits in its assignee's inbox here, whether or not they are
  in the board's workspace; each case keeps the version of the map it started
  on, and its history
- BPMN 2.0 in and out: a map drawn in bpmn.io or Camunda Modeler can be
  brought in, and one drawn here taken out, with what BPMN has no words for
  kept in each element's `extensionElements`

**Departments**
- A tree of departments on the users page, each person in any number of
  them with one primary, and a head for each

**Notifications**
- Web Push to phones and desktops when an item is given to you, your name is
  in a comment, or an item you made or follow moves; reminders the day before
  and the day after a due day
- Quiet hours, a notification centre in the page, and a per-person switch

**Accounts and security**
- Roles (administrator, manager, viewer) with per-account allow and deny
  overrides
- Two-step sign-in with an authenticator app, a code by email or by SMS, and
  passkeys (WebAuthn)
- A sign-in policy (password rules, lockouts, two steps for everyone), the
  list of active sessions, and an audit trail of every change
- A Content-Security-Policy that runs scripts from this server only (the
  pages' own inline scripts included), CSRF and origin checks, rate limits,
  and webhooks that cannot be aimed at this machine or a link-local address

## Requirements

- Go 1.26 to build
- PostgreSQL for the boards, accounts, notifications and the audit trail. The
  schema is created and upgraded on start
- HTTPS in front of it for anything beyond this machine: passkeys and the
  installed app need a secure page. Use a reverse proxy, or give the server a
  certificate with `DOCFIND_TLS_CERT` and `DOCFIND_TLS_KEY`

## Quick start

```sh
go build -o docfind-tasks ./cmd/tasks

cp .env.example .env        # then set DOCFIND_PG_* at least

# The first administrator, typed at the terminal (the password is not echoed):
./docfind-tasks -users data/users.json -add-user admin -role admin

# On this machine only:
./docfind-tasks -users data/users.json

# For the network, once an account exists:
./docfind-tasks -users data/users.json -bind 0.0.0.0 -port 8330
```

Then open `http://127.0.0.1:8330`, sign in, and make a workspace. The server
refuses to listen beyond this machine while the accounts file is empty, so
the first administrator cannot be created over the network by whoever gets
there first. Without PostgreSQL it still starts, for accounts and settings,
and the task manager answers that it needs a database.

## Permissions

| Permission | Who has it by default |
|---|---|
| `page:tasks` (the task manager) | everyone |
| `page:settings`, `action:notifications` | everyone |
| `action:manage_users` (accounts, roles, departments), `action:manage_settings` | administrators |

Within the task manager, a workspace's members decide the rest: its owners
change its settings, members and boards, its members work on its boards, and
its viewers read them. A process step reaches the people it is assigned to
whether or not they hold `page:tasks`. Overrides per account are set in
Settings, or from the command line with the server stopped.

## Configuration

Everything is read from the environment, or from a `.env` file beside the
binary (`-env-file` to name another). Nothing secret belongs in the code or in
a config file under version control. [.env.example](.env.example) lists every
variable with what it does; the main ones:

| Variable | Purpose |
|---|---|
| `DOCFIND_PG_HOST`, `DOCFIND_PG_PORT`, `DOCFIND_PG_USER`, `DOCFIND_PG_PASSWORD`, `DOCFIND_PG_DATABASE`, `DOCFIND_PG_SSLMODE` | The database. |
| `DOCFIND_ALLOWED_HOSTS` | Hosts a board's webhooks may reach whatever they resolve to, such as a service on this same machine. Without it a webhook reaches the internet and the local network, but not this machine or a link-local address. |
| `DOCFIND_PUSH_SUBJECT` | The `mailto:` address push services may reach you at. Without it, device notifications stay off. |
| `DOCFIND_PASSKEY_RPID`, `DOCFIND_PASSKEY_ORIGINS` | The host name passkeys are bound to, and the origins they are used from. |
| `DOCFIND_SMTP_*`, `DOCFIND_SMS_*` | Where sign-in codes are sent from, by email and by SMS. |

Files the server keeps beside the accounts file (`-users`): the session
signing key, the active sessions, profile pictures, the key that seals
two-step secrets, the files items and answers carry (`task-files`), and by
default the logs. Back that folder up, with the database.

## Command line

| Flag | |
|---|---|
| `-users <file>` | The accounts file. Required. |
| `-bind <address>`, `-port <n>` | Where to listen. Default 127.0.0.1:8330. |
| `-trust-proxy-headers` | Take the client address from X-Forwarded-For. Only behind a proxy you control. |
| `-add-user <name> -role admin\|manager\|viewer` | Create an account, or reset its password, then exit. |
| `-set-grant <name> -perm <id>[,<id>...] -state allow\|deny\|inherit` | Set permission overrides, with the server stopped. |
| `-set-phone <name> -phone <number>` | Set an account's mobile number, with the server stopped. |
| `-migrate-down <n> -confirm-data-loss` | Run one migration's down script. Without the second flag it only shows what it would drop. |
| `-env-file <path>`, `-env-file-wins` | Which `.env` to read, and whether it overrides the real environment. |
| `-version` | Print the version. |

## Layout

| Path | |
|---|---|
| `cmd/tasks` | The binary: options, start-up, wiring |
| `internal/tasks` | The model and its rules: workspaces, boards, items, forms, automations, approvals, processes, conditions and formulas, BPMN |
| `internal/find` | The HTTP server and its pages (the task page, the process designer, the form maker, settings) |
| `internal/auth` | Accounts, sessions, roles, permissions, the sign-in policy |
| `internal/pgstore` | PostgreSQL access and the numbered migrations |
| `internal/notify`, `internal/webpush` | The notification outbox and Web Push |
| `internal/webauthn` | Passkeys |
| `internal/audit`, `internal/obs` | The audit trail, request logs and metrics |
| `internal/netguard` | Where a webhook may and may not go |
| `internal/jalali` | The Jalali calendar |

## Development

```sh
go vet ./...
go test ./...
```

The tests run the real router, and the model's tests an in-memory store. The
tests that need PostgreSQL run when `DOCFIND_PG_*` is set and skip
otherwise; use a database you can throw away, and run them one package at a
time, `go test -p 1 ./...`.

## License

MIT. See [LICENSE](LICENSE). The bundled third-party files keep their own
licences; see [THIRD-PARTY.md](THIRD-PARTY.md).
