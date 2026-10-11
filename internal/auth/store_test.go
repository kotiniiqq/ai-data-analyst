package auth

import (
	"context"
	"testing"

	"github.com/kotiniiqq/ai-data-analyst/internal/testdb"
)

func TestPGStoreFindsUserByEmail(t *testing.T) {
	pool := testdb.Pool(t)
	ctx := context.Background()
	s := NewPGStore(pool)
	t.Cleanup(func() { pool.Exec(ctx, `DELETE FROM users WHERE email = $1`, "store-test@demo.local") })
	if err := s.Upsert(ctx, "store-test@demo.local", "pw-1", "analyst"); err != nil {
		t.Fatal(err)
	}
	if err := s.Upsert(ctx, "store-test@demo.local", "pw-2", "analyst"); err != nil {
		t.Fatal("upsert must be repeatable:", err)
	}
	u, hash, err := s.ByEmail(ctx, "Store-Test@demo.local")
	if err != nil {
		t.Fatal(err)
	}
	if u.Role != "analyst" || !CheckPassword(hash, "pw-2") {
		t.Fatalf("got %+v", u)
	}
	if _, _, err := s.ByEmail(ctx, "nobody@demo.local"); err != ErrNoUser {
		t.Fatalf("want ErrNoUser, got %v", err)
	}
}
