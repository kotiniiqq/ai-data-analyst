package audit

import (
	"context"
	"testing"

	"github.com/kotiniiqq/ai-data-analyst/internal/testdb"
)

func TestPGLoggerWritesAnEntry(t *testing.T) {
	pool := testdb.Pool(t)
	ctx := context.Background()
	var uid int64
	if err := pool.QueryRow(ctx, `INSERT INTO users (email, password_hash, role) VALUES ('audit-test@demo.local', 'x', 'viewer') RETURNING id`).Scan(&uid); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		pool.Exec(ctx, `DELETE FROM audit_log WHERE user_id = $1`, uid)
		pool.Exec(ctx, `DELETE FROM users WHERE id = $1`, uid)
	})
	NewPG(pool).Log(ctx, Entry{UserID: uid, Tool: "run_sql", Allowed: false, Code: "forbidden", InputKeys: []string{"sql"}, DurationMS: 3})
	var tool, code string
	var allowed bool
	var keys []string
	err := pool.QueryRow(ctx, `SELECT tool, allowed, code, input_keys FROM audit_log WHERE user_id = $1`, uid).Scan(&tool, &allowed, &code, &keys)
	if err != nil {
		t.Fatal(err)
	}
	if tool != "run_sql" || allowed || code != "forbidden" || len(keys) != 1 || keys[0] != "sql" {
		t.Fatalf("got %s %v %s %v", tool, allowed, code, keys)
	}
}

func TestMemoryLoggerKeepsEntries(t *testing.T) {
	m := &Memory{}
	m.Log(context.Background(), Entry{Tool: "a"})
	if len(m.Entries()) != 1 || m.Entries()[0].Tool != "a" {
		t.Fatal("entry not kept")
	}
}
