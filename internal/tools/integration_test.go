package tools

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/kotiniiqq/ai-data-analyst/internal/approval"
	"github.com/kotiniiqq/ai-data-analyst/internal/audit"
	"github.com/kotiniiqq/ai-data-analyst/internal/catalog"
	"github.com/kotiniiqq/ai-data-analyst/internal/cost"
	"github.com/kotiniiqq/ai-data-analyst/internal/testdb"
	"github.com/kotiniiqq/ai-data-analyst/internal/warehouse"
)

func TestEveryMetricRunsOnTheRealWarehouse(t *testing.T) {
	testdb.WarehousePool(t)
	ctx := context.Background()
	wh, err := warehouse.NewPostgres(ctx, os.Getenv("TEST_WAREHOUSE_URL"), 10*time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer wh.Close()
	cat := catalog.Default()
	reg := NewRegistry(&audit.Memory{}, &Builtins{
		Catalog: cat, Warehouse: wh, Estimator: &cost.Postgres{Pool: wh.Pool(), ApprovalBytes: cost.DefaultApprovalBytes},
		Approvals: approval.NewStore(), Now: time.Now, DataStart: "2024-01-01", DataEnd: "2026-09-30",
	})
	for _, m := range cat.Metrics(owner) {
		args := map[string]any{"metric": m.Name, "from": "2025-10-01", "to": "2026-09-30"}
		out, err := reg.Execute(ctx, owner, "metric_series", args)
		if err != nil {
			t.Fatalf("%s: %v", m.Name, err)
		}
		if s := out.Data.(SeriesSummary); s.Points != 12 {
			t.Fatalf("%s: %d points, want 12 (%s)", m.Name, s.Points, out.Text)
		}
		for _, d := range m.Dims {
			args["by"] = d
			out, err := reg.Execute(ctx, owner, "metric_series", args)
			if err != nil {
				t.Fatalf("%s by %s: %v", m.Name, d, err)
			}
			if s := out.Data.(SeriesSummary); len(s.Groups) != 3 {
				t.Fatalf("%s by %s: %d groups", m.Name, d, len(s.Groups))
			}
		}
	}
}
