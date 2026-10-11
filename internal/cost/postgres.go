package cost

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/jackc/pgx/v5/pgxpool"
)

// Postgres estimates with EXPLAIN. Postgres reports rows and row width, not
// bytes scanned, so the estimate is the largest rows x width of any plan
// node below the LIMIT: how much data the query has to produce on the way.
type Postgres struct {
	Pool          *pgxpool.Pool
	ApprovalBytes int64
}

func (p *Postgres) Estimate(ctx context.Context, sql string) (Estimate, error) {
	var raw []byte
	if err := p.Pool.QueryRow(ctx, "EXPLAIN (FORMAT JSON) "+sql).Scan(&raw); err != nil {
		return Estimate{}, err
	}
	b, err := planBytes(raw)
	if err != nil {
		return Estimate{}, err
	}
	e := FromBytes(b, p.ApprovalBytes)
	e.Source = "postgres explain"
	return e, nil
}

type planNode struct {
	NodeType string     `json:"Node Type"`
	Rows     float64    `json:"Plan Rows"`
	Width    float64    `json:"Plan Width"`
	Plans    []planNode `json:"Plans"`
}

func planBytes(raw []byte) (int64, error) {
	var doc []struct {
		Plan *planNode `json:"Plan"`
	}
	if err := json.Unmarshal(raw, &doc); err != nil || len(doc) == 0 || doc[0].Plan == nil {
		return 0, errors.New("unexpected EXPLAIN output")
	}
	return int64(widest(*doc[0].Plan)), nil
}

func widest(n planNode) float64 {
	best := 0.0
	if n.NodeType != "Limit" {
		best = n.Rows * n.Width
	}
	for _, c := range n.Plans {
		best = max(best, widest(c))
	}
	return best
}
