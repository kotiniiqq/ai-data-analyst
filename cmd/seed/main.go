// Command seed creates the three demo users. It is safe to run again: the
// users' passwords and roles are reset to the documented demo values.
package main

import (
	"context"
	"log/slog"
	"os"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/kotiniiqq/ai-data-analyst/internal/auth"
)

// Demo accounts for local use only; documented in the README.
var demoUsers = []struct{ email, password, role string }{
	{"viewer@demo.local", "viewer-demo", "viewer"},
	{"analyst@demo.local", "analyst-demo", "analyst"},
	{"owner@demo.local", "owner-demo", "owner"},
}

func main() {
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, os.Getenv("DATABASE_URL"))
	if err != nil {
		slog.Error("connect", "err", err)
		os.Exit(1)
	}
	defer pool.Close()
	store := auth.NewPGStore(pool)
	for _, u := range demoUsers {
		if err := store.Upsert(ctx, u.email, u.password, u.role); err != nil {
			slog.Error("seed user", "email", u.email, "err", err)
			os.Exit(1)
		}
	}
	slog.Info("demo users ready", "count", len(demoUsers))
}
