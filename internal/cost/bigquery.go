package cost

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
)

// BigQueryBaseURL is the BigQuery REST API root.
const BigQueryBaseURL = "https://bigquery.googleapis.com/bigquery/v2"

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
	if err := bqQuery(ctx, b.Client, b.BaseURL, b.Project, map[string]any{
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

func bqQuery(ctx context.Context, c *http.Client, base, project string, body map[string]any, out any) error {
	if base == "" {
		base = BigQueryBaseURL
	}
	buf, _ := json.Marshal(body)
	req, err := http.NewRequestWithContext(ctx, "POST", base+"/projects/"+project+"/queries", bytes.NewReader(buf))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	res, err := c.Do(req)
	if err != nil {
		return fmt.Errorf("bigquery: %w", err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		var e struct {
			Error struct {
				Message string `json:"message"`
			} `json:"error"`
		}
		json.NewDecoder(res.Body).Decode(&e)
		if e.Error.Message == "" {
			e.Error.Message = res.Status
		}
		return errors.New("bigquery: " + e.Error.Message)
	}
	return json.NewDecoder(res.Body).Decode(out)
}
