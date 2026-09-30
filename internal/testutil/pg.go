// Package testutil helps integration tests talk to a real Postgres. Tests
// skip when TEST_DATABASE_URL is unset; each test creates its own account and
// deletes it afterwards, which cascades to everything the account owns.
package testutil

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"os"
	"sync"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"

	"writersguild/internal/db"
	"writersguild/internal/db/sqlcgen"
)

var (
	once sync.Once
	pool *pgxpool.Pool
	err  error
)

// DB returns a shared, migrated pool or skips the test.
func DB(t *testing.T) *pgxpool.Pool {
	t.Helper()
	url := os.Getenv("TEST_DATABASE_URL")
	if url == "" {
		t.Skip("TEST_DATABASE_URL not set; skipping integration test")
	}
	once.Do(func() {
		ctx := context.Background()
		pool, err = db.Connect(ctx, url)
		if err == nil {
			err = db.Migrate(ctx, pool)
		}
	})
	if err != nil {
		t.Fatalf("test database: %v", err)
	}
	return pool
}

// NewUser creates a throwaway account with a random username and removes it
// when the test ends.
func NewUser(t *testing.T, q *sqlcgen.Queries, role string) sqlcgen.User {
	t.Helper()
	var b [6]byte
	_, _ = rand.Read(b[:])
	u, err := q.CreateUser(context.Background(), sqlcgen.CreateUserParams{
		Username: "t" + hex.EncodeToString(b[:]), DisplayName: "Test " + role, PasswordHash: "$2a$10$invalidhashfortests000000000000000000000000000000000000", Role: role,
	})
	if err != nil {
		t.Fatalf("create test user: %v", err)
	}
	t.Cleanup(func() {
		if err := q.DeleteUser(context.Background(), u.ID); err != nil {
			t.Errorf("cleanup user: %v", err)
		}
	})
	return u
}
