package warehouse

import (
	"context"
	"net/http"
	"strconv"
	"strings"

	"github.com/kotiniiqq/ai-data-analyst/internal/bq"
)

// BigQuery runs queries with jobs.query. Client must add Google credentials.
type BigQuery struct {
	Project string
	BaseURL string
	Client  *http.Client
}

func (b *BigQuery) Dialect() string { return "bigquery" }

func (b *BigQuery) Query(ctx context.Context, sql string) (Rows, error) {
	var resp struct {
		JobComplete bool `json:"jobComplete"`
		Schema      struct {
			Fields []struct {
				Name string `json:"name"`
				Type string `json:"type"`
			} `json:"fields"`
		} `json:"schema"`
		Rows []struct {
			F []struct {
				V *string `json:"v"`
			} `json:"f"`
		} `json:"rows"`
	}
	err := bq.Query(ctx, b.Client, b.BaseURL, b.Project, map[string]any{
		"query": sql, "useLegacySql": false, "timeoutMs": 10000, "maxResults": 1000,
	}, &resp)
	if err != nil {
		return Rows{}, &Error{Code: "query", Msg: strings.TrimPrefix(err.Error(), "bigquery: ")}
	}
	if !resp.JobComplete {
		return Rows{}, &Error{Code: "timeout", Msg: "the query took too long"}
	}
	var out Rows
	for _, f := range resp.Schema.Fields {
		out.Columns = append(out.Columns, f.Name)
	}
	for _, r := range resp.Rows {
		row := make([]any, len(r.F))
		for i, c := range r.F {
			if c.V == nil || i >= len(resp.Schema.Fields) {
				continue
			}
			row[i] = bqValue(*c.V, resp.Schema.Fields[i].Type)
		}
		out.Rows = append(out.Rows, row)
	}
	return out, nil
}

func bqValue(v, typ string) any {
	switch typ {
	case "INTEGER", "INT64":
		if n, err := strconv.ParseInt(v, 10, 64); err == nil {
			return n
		}
	case "FLOAT", "FLOAT64", "NUMERIC", "BIGNUMERIC":
		if f, err := strconv.ParseFloat(v, 64); err == nil {
			return f
		}
	case "BOOLEAN", "BOOL":
		return v == "true"
	}
	return v
}
