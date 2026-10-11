// Package bq calls the BigQuery REST API (jobs.query). The http.Client
// passed in is expected to add Google credentials.
package bq

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
)

// BaseURL is the BigQuery REST API root.
const BaseURL = "https://bigquery.googleapis.com/bigquery/v2"

// Query posts body to projects/{project}/queries and decodes the answer into out.
func Query(ctx context.Context, c *http.Client, base, project string, body map[string]any, out any) error {
	if base == "" {
		base = BaseURL
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
