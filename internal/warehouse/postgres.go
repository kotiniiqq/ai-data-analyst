package warehouse

import (
	"context"
	"errors"
	"math/big"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Postgres connects as the read-only warehouse role. The role has its own
// statement_timeout; timeout here is a second, client-side limit.
type Postgres struct {
	pool    *pgxpool.Pool
	timeout time.Duration
}

func NewPostgres(ctx context.Context, dsn string, timeout time.Duration) (*Postgres, error) {
	pool, err := pgxpool.New(ctx, dsn)
	if err != nil {
		return nil, err
	}
	return &Postgres{pool: pool, timeout: timeout}, nil
}

func (p *Postgres) Close()          { p.pool.Close() }
func (p *Postgres) Dialect() string { return "postgres" }

// Pool exposes the connection pool for the EXPLAIN-based cost estimator.
func (p *Postgres) Pool() *pgxpool.Pool { return p.pool }

func (p *Postgres) Query(ctx context.Context, sql string) (Rows, error) {
	ctx, cancel := context.WithTimeout(ctx, p.timeout)
	defer cancel()
	rows, err := p.pool.Query(ctx, sql)
	if err != nil {
		return Rows{}, classify(ctx, err)
	}
	defer rows.Close()
	var out Rows
	for _, f := range rows.FieldDescriptions() {
		out.Columns = append(out.Columns, f.Name)
	}
	for rows.Next() {
		vals, err := rows.Values()
		if err != nil {
			return Rows{}, classify(ctx, err)
		}
		for i, v := range vals {
			vals[i] = normalise(v, rows.FieldDescriptions()[i].DataTypeOID)
		}
		out.Rows = append(out.Rows, vals)
	}
	if err := rows.Err(); err != nil {
		return Rows{}, classify(ctx, err)
	}
	return out, nil
}

func normalise(v any, oid uint32) any {
	switch x := v.(type) {
	case nil:
		return nil
	case pgtype.Numeric:
		if !x.Valid {
			return nil
		}
		f, _ := x.Float64Value()
		return f.Float64
	case time.Time:
		if oid == pgtype.DateOID {
			return x.Format("2006-01-02")
		}
		return x.UTC().Format(time.RFC3339)
	case int16:
		return int64(x)
	case int32:
		return int64(x)
	case float32:
		return float64(x)
	case *big.Int:
		return x.String()
	default:
		return v
	}
}

func classify(ctx context.Context, err error) error {
	var pgErr *pgconn.PgError
	switch {
	case errors.As(err, &pgErr) && pgErr.Code == "57014", errors.Is(ctx.Err(), context.DeadlineExceeded):
		return &Error{Code: "timeout", Msg: "the query took too long and was stopped"}
	case errors.As(err, &pgErr):
		return &Error{Code: "query", Msg: pgErr.Message}
	default:
		return &Error{Code: "unavailable", Msg: "the warehouse is not reachable"}
	}
}
