package warehouse

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
	"time"

	"github.com/kotiniiqq/ai-data-analyst/internal/testdb"
)

func pg(t *testing.T) *Postgres {
	t.Helper()
	testdb.WarehousePool(t) // skips without TEST_WAREHOUSE_URL
	p, err := NewPostgres(context.Background(), os.Getenv("TEST_WAREHOUSE_URL"), 5*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(p.Close)
	return p
}

func code(err error) string {
	var e *Error
	if errors.As(err, &e) {
		return e.Code
	}
	return ""
}

func TestPostgresNormalisesValues(t *testing.T) {
	r, err := pg(t).Query(context.Background(),
		"select date, plan, mrr_usd, 2::int as n from gold.revenue_daily where date = '2026-09-30' and plan = 'team' limit 1")
	if err != nil {
		t.Fatal(err)
	}
	if len(r.Columns) != 4 || r.Columns[2] != "mrr_usd" || len(r.Rows) != 1 {
		t.Fatalf("got %+v", r)
	}
	row := r.Rows[0]
	if row[0] != "2026-09-30" || row[1] != "team" || row[3] != int64(2) {
		t.Fatalf("row %#v", row)
	}
	if f, ok := row[2].(float64); !ok || f != 75250.82 {
		t.Fatalf("numeric became %#v", row[2])
	}
}

func TestPostgresNullsStayNull(t *testing.T) {
	r, err := pg(t).Query(context.Background(), "select null::numeric as x from gold.support_daily limit 1")
	if err != nil {
		t.Fatal(err)
	}
	if r.Rows[0][0] != nil {
		t.Fatalf("got %#v", r.Rows[0][0])
	}
}

func TestPostgresPermissionErrorIsAQueryError(t *testing.T) {
	_, err := pg(t).Query(context.Background(), "select email from public.users limit 1")
	if code(err) != "query" {
		t.Fatalf("got %v", err)
	}
}

func TestPostgresTimeout(t *testing.T) {
	p := pg(t)
	p.timeout = 300 * time.Millisecond
	start := time.Now()
	_, err := p.Query(context.Background(), "select pg_sleep(3)")
	if code(err) != "timeout" || time.Since(start) > 2*time.Second {
		t.Fatalf("got %v after %v", err, time.Since(start))
	}
}

func TestPostgresUnavailable(t *testing.T) {
	p, err := NewPostgres(context.Background(), "postgres://x:y@127.0.0.1:1/none?connect_timeout=1", time.Second)
	if err != nil {
		t.Fatal(err) // the pool connects lazily
	}
	defer p.Close()
	if _, err := p.Query(context.Background(), "select 1"); code(err) != "unavailable" {
		t.Fatalf("got %v", err)
	}
}

func TestBigQueryConvertsTypedRows(t *testing.T) {
	var body map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		json.Unmarshal(b, &body)
		w.Write([]byte(`{"jobComplete":true,
		  "schema":{"fields":[{"name":"date","type":"DATE"},{"name":"plan","type":"STRING"},
		    {"name":"mrr","type":"NUMERIC"},{"name":"n","type":"INTEGER"},{"name":"x","type":"FLOAT"}]},
		  "rows":[{"f":[{"v":"2026-09-30"},{"v":"team"},{"v":"75250.82"},{"v":"2"},{"v":null}]}]}`))
	}))
	defer srv.Close()
	b := &BigQuery{Project: "p", BaseURL: srv.URL, Client: srv.Client()}
	r, err := b.Query(context.Background(), "select 1")
	if err != nil {
		t.Fatal(err)
	}
	if body["useLegacySql"] != false || body["dryRun"] != nil {
		t.Fatalf("request %v", body)
	}
	row := r.Rows[0]
	if row[0] != "2026-09-30" || row[1] != "team" || row[2] != 75250.82 || row[3] != int64(2) || row[4] != nil {
		t.Fatalf("row %#v", row)
	}
}

func TestBigQueryIncompleteJobIsATimeout(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"jobComplete":false}`))
	}))
	defer srv.Close()
	_, err := (&BigQuery{Project: "p", BaseURL: srv.URL, Client: srv.Client()}).Query(context.Background(), "select 1")
	if code(err) != "timeout" {
		t.Fatalf("got %v", err)
	}
}
