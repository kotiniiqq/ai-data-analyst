// Package audit records every tool call: who, which tool, whether it was
// allowed, the outcome code and the names of the inputs. Input values are
// never stored, so queries and questions do not leak into the log.
package audit

import (
	"context"
	"log/slog"
	"sync"

	"github.com/jackc/pgx/v5/pgxpool"
)

type Entry struct {
	UserID     int64
	Tool       string
	Allowed    bool
	Code       string // "" on success, otherwise the tool error code
	InputKeys  []string
	DurationMS int64
}

type Logger interface {
	Log(ctx context.Context, e Entry)
}

type PG struct{ pool *pgxpool.Pool }

func NewPG(pool *pgxpool.Pool) *PG { return &PG{pool: pool} }

func (p *PG) Log(ctx context.Context, e Entry) {
	keys := e.InputKeys
	if keys == nil {
		keys = []string{}
	}
	_, err := p.pool.Exec(context.WithoutCancel(ctx),
		`INSERT INTO audit_log (user_id, tool, allowed, code, input_keys, duration_ms) VALUES ($1, $2, $3, $4, $5, $6)`,
		e.UserID, e.Tool, e.Allowed, e.Code, keys, e.DurationMS)
	if err != nil {
		slog.Error("audit write failed", "tool", e.Tool, "err", err)
	}
}

// Memory keeps entries in memory, for tests.
type Memory struct {
	mu      sync.Mutex
	entries []Entry
}

func (m *Memory) Log(_ context.Context, e Entry) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.entries = append(m.entries, e)
}

func (m *Memory) Entries() []Entry {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]Entry(nil), m.entries...)
}
