// Package warehouse runs approved queries. Values come back as plain JSON
// types: numbers as float64 or int64, dates as "YYYY-MM-DD", text as string.
package warehouse

import "context"

type Rows struct {
	Columns []string `json:"columns"`
	Rows    [][]any  `json:"rows"`
}

type Provider interface {
	Query(ctx context.Context, sql string) (Rows, error)
	Dialect() string // "postgres" or "bigquery"
}

// Error is what the user may see: a code and a short message, never a stack.
type Error struct {
	Code string // timeout, query, unavailable
	Msg  string
}

func (e *Error) Error() string { return e.Code + ": " + e.Msg }
