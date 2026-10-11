package cost

import (
	"context"
	"errors"
	"net/http"
	"strconv"

	"github.com/kotiniiqq/ai-data-analyst/internal/bq"
)

// BigQuery estimates with a dry run, which reports the bytes the query would
// bill without running it. Client must add Google credentials.
type BigQuery struct {
	Project       string
	BaseURL       string
	Client        *http.Client
	ApprovalBytes int64
}

func (b *BigQuery) Estimate(ctx context.Context, sql string) (Estimate, error) {
	var resp struct {
		TotalBytesProcessed string `json:"totalBytesProcessed"`
	}
	if err := bq.Query(ctx, b.Client, b.BaseURL, b.Project, map[string]any{
		"query": sql, "useLegacySql": false, "dryRun": true,
	}, &resp); err != nil {
		return Estimate{}, err
	}
	n, err := strconv.ParseInt(resp.TotalBytesProcessed, 10, 64)
	if err != nil {
		return Estimate{}, errors.New("bigquery: no byte estimate in the dry run")
	}
	e := FromBytes(n, b.ApprovalBytes)
	e.Source = "bigquery dry run"
	return e, nil
}
