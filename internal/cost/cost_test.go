package cost

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/kotiniiqq/ai-data-analyst/internal/testdb"
)

func fixture(t *testing.T, name string) []byte {
	t.Helper()
	b, err := os.ReadFile("testdata/" + name)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func TestPlanBytesUsesTheWidestNodeBelowLimit(t *testing.T) {
	b, err := planBytes(fixture(t, "cross_join.json"))
	if err != nil {
		t.Fatal(err)
	}
	// Nested Loop: 3,024,048 rows x 12 bytes; the Limit node above it (1000 rows) is ignored.
	if b != 3024048*12 {
		t.Fatalf("got %d", b)
	}
	small, _ := planBytes(fixture(t, "simple.json"))
	if small <= 0 || small >= b {
		t.Fatalf("simple plan bytes %d", small)
	}
}

func TestPlanBytesRejectsGarbage(t *testing.T) {
	if _, err := planBytes([]byte(`{"no":"plan"}`)); err == nil {
		t.Fatal("garbage accepted")
	}
}

func TestFromBytes(t *testing.T) {
	e := FromBytes(1<<40, 100<<20)
	if e.USD != 6.25 || !e.NeedsApproval {
		t.Fatalf("1 TiB: %+v", e)
	}
	if e := FromBytes(100<<20-1, 100<<20); e.NeedsApproval {
		t.Fatal("below the threshold needs approval")
	}
	if e := FromBytes(100<<20, 100<<20); !e.NeedsApproval {
		t.Fatal("at the threshold does not need approval")
	}
}

func TestBigQueryDryRun(t *testing.T) {
	var got map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" || r.URL.Path != "/projects/demo-proj/queries" {
			t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
		}
		body, _ := io.ReadAll(r.Body)
		json.Unmarshal(body, &got)
		w.Write([]byte(`{"jobComplete":true,"totalBytesProcessed":"209715200"}`))
	}))
	defer srv.Close()
	e, err := (&BigQuery{Project: "demo-proj", BaseURL: srv.URL, Client: srv.Client(), ApprovalBytes: 100 << 20}).
		Estimate(context.Background(), "select 1")
	if err != nil {
		t.Fatal(err)
	}
	if got["dryRun"] != true || got["useLegacySql"] != false || got["query"] != "select 1" {
		t.Fatalf("request body %v", got)
	}
	if e.Bytes != 209715200 || !e.NeedsApproval || e.Source != "bigquery dry run" {
		t.Fatalf("got %+v", e)
	}
}

func TestBigQueryErrorIsReported(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(400)
		w.Write([]byte(`{"error":{"message":"Unrecognized name: foo"}}`))
	}))
	defer srv.Close()
	_, err := (&BigQuery{Project: "p", BaseURL: srv.URL, Client: srv.Client()}).Estimate(context.Background(), "select foo")
	if err == nil || err.Error() != "bigquery: Unrecognized name: foo" {
		t.Fatalf("got %v", err)
	}
}

func TestPostgresEstimateAgainstTheWarehouse(t *testing.T) {
	pool := testdb.WarehousePool(t)
	pg := &Postgres{Pool: pool, ApprovalBytes: 100 << 20}
	ctx := context.Background()
	small, err := pg.Estimate(ctx, "select date, mrr_usd from gold.revenue_daily limit 10")
	if err != nil {
		t.Fatal(err)
	}
	if small.NeedsApproval || small.Source != "postgres explain" {
		t.Fatalf("small query: %+v", small)
	}
	wide, err := pg.Estimate(ctx, "select a.date, b.date, a.mrr_usd, b.mrr_usd from gold.revenue_daily a cross join gold.revenue_daily b limit 1000")
	if err != nil {
		t.Fatal(err)
	}
	if !wide.NeedsApproval {
		t.Fatalf("self cross join does not need approval: %+v", wide)
	}
}
