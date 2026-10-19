package main

import (
	"context"
	"path/filepath"
	"strings"

	"github.com/Schayan-nk/docfind-tasks/internal/auth"
	"github.com/Schayan-nk/docfind-tasks/internal/find"
	"github.com/Schayan-nk/docfind-tasks/internal/pgstore"
)

// useAccounts hands the accounts to the server, with everything that lives
// beside them: sessions, passkeys, pictures, notifications, the task manager
// and the audit trail. The returned function saves the sessions on the way out.
func useAccounts(app *find.Server, acc accountSetup, o options) (cleanup func()) {
	users := acc.users
	signer := auth.NewSigner(acc.sessionKey, find.TokenMaxAge)
	app.UseAccounts(users, signer, o.trustProxy)

	// Passkeys, when a host name is configured (passkeycfg.go). A bad setting
	// is logged and leaves them off: a typo there must not stop everybody
	// signing in with a password.
	if rp, err := passkeyConfig(); err != nil {
		logf("passkeys are off: %v", err)
	} else if rp != nil {
		app.UsePasskeys(rp)
		logf("passkeys enabled for %s (%s)", rp.ID, strings.Join(rp.Origins, ", "))
	}

	// Where people are signed in, and their profile pictures, beside the
	// accounts file: this application's private state.
	stateDir := filepath.Dir(o.usersFile)
	sessions := auth.OpenSessions(filepath.Join(stateDir, "sessions.json"))
	app.UseSessions(sessions, filepath.Join(stateDir, "avatars"))

	// Notifications on people's own devices, delivered with the page closed.
	startNotifications(app, acc.pgDB, stateDir)
	// The task manager itself: PostgreSQL holds every board, so it is off
	// without it, with the reason logged.
	startTasks(app, acc.pgDB, stateDir)
	// Codes for the second sign-in step by email, where a mailbox is given
	// (smtpmail.go); by SMS the panel is read from the environment as well.
	if m := smtpFromEnv(); m != nil {
		app.UseCodeMail(m)
		logf("sign-in codes by email from %s", m.from)
	}
	// The audit trail: every change, searchable at /api/audit.
	app.UseAudit(pgstore.NewAudit(acc.pgDB))
	if acc.pgDB != nil {
		// What the audit trail cannot take while the database is down waits on
		// this disk and goes in when it is back.
		app.UseAuditSpool(context.Background(), filepath.Join(stateDir, "audit-spool.jsonl"))
		// The sign-in policy: the lockout, what a password must contain, two
		// steps for everybody (securitypolicy.go).
		app.UseSecurityPolicy(pgstore.NewAppSettings(acc.pgDB))
		// What each role gets by default, as edited in Settings.
		app.UseRoleTemplates(pgstore.NewAppSettings(acc.pgDB))
	}

	if users.Count() == 0 {
		logf("no accounts yet: run with -add-user to create the first administrator")
	}
	return func() {
		if err := sessions.Flush(); err != nil {
			logf("saving sessions: %v", err)
		}
	}
}

// startTasks turns the task manager on when PostgreSQL is configured: the
// workspaces, boards and items (migration 031) and the departments a process
// step can go to (038 and 040), applied with the rest at start-up. The files
// items and answers take are kept beside the accounts.
func startTasks(app *find.Server, db *pgstore.DB, stateDir string) {
	if db == nil {
		logf("the task manager is off: it needs PostgreSQL (DOCFIND_PG_*), which holds every board")
		return
	}
	app.UseDepartments(pgstore.NewDepartments(db))
	app.UseTasks(pgstore.NewTasks(db))
	if err := app.UseTaskFiles(filepath.Join(stateDir, "task-files")); err != nil {
		logf("task files are off: %v", err)
	}
	logf("task manager on: /tasks")
}
