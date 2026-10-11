package agent

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/kotiniiqq/ai-data-analyst/internal/approval"
	"github.com/kotiniiqq/ai-data-analyst/internal/audit"
	"github.com/kotiniiqq/ai-data-analyst/internal/catalog"
	"github.com/kotiniiqq/ai-data-analyst/internal/cost"
	"github.com/kotiniiqq/ai-data-analyst/internal/rbac"
	"github.com/kotiniiqq/ai-data-analyst/internal/tools"
	"github.com/kotiniiqq/ai-data-analyst/internal/warehouse"
)

var (
	viewer  = rbac.NewUser(1, "viewer@demo.local", "viewer")
	analyst = rbac.NewUser(2, "analyst@demo.local", "analyst")
)

type fakeWH struct{ rows warehouse.Rows }

func (f fakeWH) Dialect() string { return "postgres" }
func (f fakeWH) Query(context.Context, string) (warehouse.Rows, error) {
	return f.rows, nil
}

type fakeEst struct{ bytes int64 }

func (f fakeEst) Estimate(context.Context, string) (cost.Estimate, error) {
	return cost.FromBytes(f.bytes, 100<<20), nil
}

func registry(bytes int64) *tools.Registry {
	return tools.NewRegistry(&audit.Memory{}, &tools.Builtins{
		Catalog: catalog.Default(),
		Warehouse: fakeWH{warehouse.Rows{Columns: []string{"month", "mrr"},
			Rows: [][]any{{"2026-08", 281694.59}, {"2026-09", 284830.27}}}},
		Estimator: fakeEst{bytes}, Approvals: approval.NewStore(),
		Now: func() time.Time { return time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC) },
		DataStart: "2024-01-01", DataEnd: "2026-09-30",
	})
}

// script plays back steps; each call sees the state so far.
type script struct {
	steps []Step
	seen  []State
	err   error
}

func (s *script) Next(_ context.Context, st State) (Step, error) {
	s.seen = append(s.seen, st)
	if s.err != nil {
		return Step{}, s.err
	}
	if len(s.steps) == 0 {
		return Step{ToolCalls: []ToolCall{{Name: "list_metrics"}}}, nil
	}
	st0 := s.steps[0]
	s.steps = s.steps[1:]
	return st0, nil
}

var series = ToolCall{ID: "1", Name: "metric_series", Args: map[string]any{"metric": "mrr", "from": "2026-08-01", "to": "2026-09-30"}}

func TestToolThenGroundedFinalAnswer(t *testing.T) {
	p := &script{steps: []Step{
		{ToolCalls: []ToolCall{series}},
		{Final: "MRR rose from $281,694.59 to $284,830.27 (+1.11%)."},
	}}
	a := Run(context.Background(), p, registry(1000), viewer, "How did MRR change?")
	if a.Text != "MRR rose from $281,694.59 to $284,830.27 (+1.11%)." || len(a.Warnings) != 0 {
		t.Fatalf("answer %+v", a)
	}
	if len(a.Trace) != 1 || a.Trace[0].Tool != "metric_series" || !a.Trace[0].Allowed {
		t.Fatalf("trace %+v", a.Trace)
	}
	if a.Table == nil || len(a.Table.Rows) != 2 || a.SQL == "" {
		t.Fatalf("table/sql missing: %+v", a)
	}
	if a.Chart == nil || !slices.Equal(a.Chart.X, []string{"2026-08", "2026-09"}) || a.Chart.Series[0].Values[1] != 284830.27 {
		t.Fatalf("chart %+v", a.Chart)
	}
	if len(p.seen[1].History) != 1 || p.seen[1].History[0].Output.Text == "" {
		t.Fatal("planner did not see the tool output")
	}
}

func TestInventedNumberIsReplacedByTemplate(t *testing.T) {
	p := &script{steps: []Step{
		{ToolCalls: []ToolCall{series}},
		{Final: "MRR grew 25% to $300k."},
	}}
	a := Run(context.Background(), p, registry(1000), viewer, "MRR?")
	if !slices.Contains(a.Warnings, "number_not_grounded") {
		t.Fatalf("warnings %v", a.Warnings)
	}
	if strings.Contains(a.Text, "25%") || !strings.Contains(a.Text, "284830.27") {
		t.Fatalf("text %q", a.Text)
	}
}

func TestStepLimit(t *testing.T) {
	p := &script{} // asks for list_metrics forever
	a := Run(context.Background(), p, registry(1000), viewer, "loop")
	if !slices.Contains(a.Warnings, "step_limit") || len(p.seen) != MaxSteps || len(a.Trace) != MaxSteps {
		t.Fatalf("warnings %v, planner calls %d, trace %d", a.Warnings, len(p.seen), len(a.Trace))
	}
}

func TestPlannerErrorGivesAWarningNotAPanic(t *testing.T) {
	a := Run(context.Background(), &script{err: errors.New("model unavailable")}, registry(1000), viewer, "x")
	if !slices.Contains(a.Warnings, "planner_error") || a.Text == "" {
		t.Fatalf("answer %+v", a)
	}
}

func TestDeniedToolIsTracedAndSeenByThePlanner(t *testing.T) {
	p := &script{steps: []Step{
		{ToolCalls: []ToolCall{{ID: "1", Name: "run_sql", Args: map[string]any{"sql": "select 1 from gold.support_daily limit 1"}}}},
		{Final: "Your role cannot run SQL."},
	}}
	a := Run(context.Background(), p, registry(1000), viewer, "run sql")
	if len(a.Trace) != 1 || a.Trace[0].Allowed || a.Trace[0].Code != "forbidden" {
		t.Fatalf("trace %+v", a.Trace)
	}
	if h := p.seen[1].History; len(h) != 1 || h[0].Err == nil || h[0].Err.Code != "forbidden" {
		t.Fatalf("history %+v", h)
	}
	if a.Text != "Your role cannot run SQL." {
		t.Fatalf("text %q", a.Text)
	}
}

func TestApprovalStopsTheLoop(t *testing.T) {
	p := &script{steps: []Step{
		{ToolCalls: []ToolCall{{ID: "1", Name: "run_sql", Args: map[string]any{"sql": "select date from gold.revenue_daily limit 10"}}}},
		{Final: "should not be asked"},
	}}
	a := Run(context.Background(), p, registry(500<<20), analyst, "big query")
	if a.Approval == nil || a.Approval.Token == "" || len(p.seen) != 1 {
		t.Fatalf("answer %+v, planner calls %d", a, len(p.seen))
	}
}

func TestPlannerOnlyGetsPermittedTools(t *testing.T) {
	p := &script{steps: []Step{{Final: "ok"}}}
	Run(context.Background(), p, registry(1000), viewer, "x")
	for _, tl := range p.seen[0].Tools {
		if tl.Name == "run_sql" {
			t.Fatal("viewer planner was offered run_sql")
		}
	}
}
