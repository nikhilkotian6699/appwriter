package main

import (
	"context"
	"io"
	"log/slog"
	"os"
	"strings"
	"testing"

	"writersguild/internal/auth"
	"writersguild/internal/config"
	"writersguild/internal/db/sqlcgen"
	"writersguild/internal/testutil"
)

// TestIntegrationBootstrap runs the first-start logic against a real
// database. It needs an empty users table, so it only runs when the test
// database has no users; other tests create and delete their own users.
func TestIntegrationBootstrap(t *testing.T) {
	pool := testutil.DB(t)
	q := sqlcgen.New(pool)
	ctx := context.Background()
	if n, _ := q.CountUsers(ctx); n != 0 {
		t.Skip("users table is not empty; bootstrap test needs a fresh database")
	}
	log := slog.New(slog.NewTextHandler(io.Discard, nil))
	cfg := config.Config{AppName: "writersguild", DefaultModelAlias: "lumos-chat", AppUsername: "firstadmin", AppPassword: "correct horse battery"}

	if err := bootstrap(ctx, q, config.Config{AppName: "writersguild"}, log); err == nil || !strings.Contains(err.Error(), "APP_USERNAME") {
		t.Fatalf("bootstrap without credentials must fail clearly, got %v", err)
	}
	if err := bootstrap(ctx, q, cfg, log); err != nil {
		t.Fatal(err)
	}
	u, err := q.GetUserByUsername(ctx, "firstadmin")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = q.DeleteUser(context.Background(), u.ID) })
	if u.Role != "admin" || !auth.CheckPassword(u.PasswordHash, "correct horse battery") {
		t.Fatalf("first user = %+v", u)
	}
	writers, err := q.ListWriters(ctx, u.ID)
	if err != nil || len(writers) != 8 {
		t.Fatalf("seeded writers = %d err=%v", len(writers), err)
	}
	// a second start is a no-op
	if err := bootstrap(ctx, q, config.Config{AppName: "writersguild"}, log); err != nil {
		t.Fatalf("second bootstrap: %v", err)
	}
	// an installation whose only user predates accounts: that user becomes the
	// admin. Other packages' tests may be creating users at the same moment,
	// in which case this part cannot be judged and is skipped.
	if err := q.SetUserRole(ctx, sqlcgen.SetUserRoleParams{ID: u.ID, Role: "author"}); err != nil {
		t.Fatal(err)
	}
	if err := bootstrap(ctx, q, config.Config{AppName: "writersguild"}, log); err != nil {
		t.Fatalf("promoting bootstrap: %v", err)
	}
	if n, _ := q.CountUsers(ctx); n == 1 {
		if promoted, _ := q.GetUserByID(ctx, u.ID); promoted.Role != "admin" {
			t.Fatalf("the only user should have become the admin, role %q", promoted.Role)
		}
	} else {
		t.Logf("skipping the promotion check: %d users exist while other tests run", n)
		_ = q.SetUserRole(ctx, sqlcgen.SetUserRoleParams{ID: u.ID, Role: "admin"})
	}
	// reset flag: new password, sessions invalidated
	cfg.AppPassword = "another password"
	if err := resetAdminPassword(ctx, q, cfg, log); err != nil {
		t.Fatal(err)
	}
	u2, _ := q.GetUserByID(ctx, u.ID)
	if !auth.CheckPassword(u2.PasswordHash, "another password") || u2.AuthVersion != u.AuthVersion+1 {
		t.Fatalf("reset did not apply: %+v", u2)
	}
	if err := resetAdminPassword(ctx, q, config.Config{AppUsername: "ghost", AppPassword: "long enough"}, log); err == nil {
		t.Fatal("reset for unknown user must fail")
	}
	_ = os.Getenv("TEST_DATABASE_URL")
}
