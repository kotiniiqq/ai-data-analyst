package planner

import (
	"context"
	"fmt"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/kotiniiqq/ai-data-analyst/internal/agent"
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
	owner   = rbac.NewUser(3, "owner@demo.local", "owner")
)

func first(t *testing.T, q string) []agent.ToolCall {
	t.Helper()
	st, err := NewDemo("2026-09-30").Next(context.Background(), agent.State{User: owner, Question: q})
	if err != nil {
		t.Fatal(err)
	}
	return st.ToolCalls
}

func series(metric, from, to, by string) agent.ToolCall {
	args := map[string]any{"metric": metric, "from": from, "to": to}
	if by != "" {
		args["by"] = by
	}
	return agent.ToolCall{Name: "metric_series", Args: args}
}

func TestGoldenQuestions(t *testing.T) {
	cases := []struct {
		q    string
		want []agent.ToolCall
	}{
		{"What was MRR last month?", []agent.ToolCall{series("mrr", "2026-09-01", "2026-09-30", "")}},
		{"Show MRR by plan for the last 6 months", []agent.ToolCall{series("mrr", "2026-04-01", "2026-09-30", "plan")}},
		{"How did support tickets change in 2025?", []agent.ToolCall{series("tickets", "2025-01-01", "2025-12-31", "")}},
		{"Gross margin in Q2 2026", []agent.ToolCall{series("gross_margin_pct", "2026-04-01", "2026-06-30", "")}},
		{"Compare new and churned customers this year", []agent.ToolCall{
			series("new_customers", "2026-01-01", "2026-09-30", ""), series("churned_customers", "2026-01-01", "2026-09-30", "")}},
		{"Which plan has the highest MRR?", []agent.ToolCall{series("mrr", "2025-10-01", "2026-09-30", "plan")}},
		{"infra cost per region since March 2026", []agent.ToolCall{series("infra_cost", "2026-03-01", "2026-09-30", "region")}},
		{"first response time last year", []agent.ToolCall{series("first_response_min", "2025-01-01", "2025-12-31", "")}},
		{"CSAT in September 2026", []agent.ToolCall{series("csat", "2026-09-01", "2026-09-30", "")}},
		{"churned MRR over the past 3 months", []agent.ToolCall{series("churned_mrr", "2026-07-01", "2026-09-30", "")}},
		{"What can I ask?", []agent.ToolCall{{Name: "list_metrics", Args: map[string]any{}}}},
		{"what's the weather", []agent.ToolCall{{Name: "list_metrics", Args: map[string]any{}}}},
		{"Which tables are there?", []agent.ToolCall{{Name: "list_tables", Args: map[string]any{}}}},
		{"  SELECT plan, count(*) FROM gold.revenue_daily GROUP BY plan LIMIT 5", []agent.ToolCall{
			{Name: "run_sql", Args: map[string]any{"sql": "SELECT plan, count(*) FROM gold.revenue_daily GROUP BY plan LIMIT 5"}}}},
	}
	for _, c := range cases {
		got := first(t, c.q)
		for i := range got {
			got[i].ID = ""
		}
		if !reflect.DeepEqual(got, c.want) {
			t.Errorf("%q:\n got %+v\nwant %+v", c.q, got, c.want)
		}
	}
}

type fakeWH struct{ rows warehouse.Rows }

func (f fakeWH) Dialect() string                                       { return "postgres" }
func (f fakeWH) Query(context.Context, string) (warehouse.Rows, error) { return f.rows, nil }

type fakeEst struct{}

func (fakeEst) Estimate(context.Context, string) (cost.Estimate, error) {
	return cost.FromBytes(1000, 100<<20), nil
}

func run(t *testing.T, u rbac.User, q string, rows warehouse.Rows) agent.Answer {
	t.Helper()
	reg := tools.NewRegistry(&audit.Memory{}, &tools.Builtins{
		Catalog: catalog.Default(), Warehouse: fakeWH{rows}, Estimator: fakeEst{}, Approvals: approval.NewStore(),
		Now: time.Now, DataStart: "2024-01-01", DataEnd: "2026-09-30",
	})
	return agent.Run(context.Background(), NewDemo("2026-09-30"), reg, u, q)
}

func TestFinalAnswerForASingleMonth(t *testing.T) {
	a := run(t, viewer, "What was MRR last month?", warehouse.Rows{Columns: []string{"month", "mrr"}, Rows: [][]any{{"2026-09", 284830.27}}})
	if a.Text != "MRR in Sep 2026 was $284,830.27." || len(a.Warnings) != 0 {
		t.Fatalf("%q %v", a.Text, a.Warnings)
	}
}

func TestFinalAnswerForATrend(t *testing.T) {
	a := run(t, viewer, "tickets in the last 3 months", warehouse.Rows{Columns: []string{"month", "tickets"},
		Rows: [][]any{{"2026-07", int64(2900)}, {"2026-08", int64(3100)}, {"2026-09", int64(2850)}}})
	want := "Support tickets went from 2,900 in Jul 2026 to 2,850 in Sep 2026 (-50, -1.72%). Lowest 2,850, highest 3,100, total 8,850."
	if a.Text != want || len(a.Warnings) != 0 {
		t.Fatalf("got  %q\nwant %q %v", a.Text, want, a.Warnings)
	}
}

func TestFinalAnswerRanksGroups(t *testing.T) {
	a := run(t, viewer, "Which plan has the highest MRR?", warehouse.Rows{Columns: []string{"month", "plan", "mrr"},
		Rows: [][]any{{"2026-08", "starter", 23000.0}, {"2026-08", "team", 74000.0}, {"2026-08", "enterprise", 185000.0},
			{"2026-09", "starter", 23821.78}, {"2026-09", "team", 75250.82}, {"2026-09", "enterprise", 186581.69}}})
	if !strings.HasPrefix(a.Text, "MRR by plan, Sep 2026: enterprise $186,581.69, team $75,250.82, starter $23,821.78. Highest: enterprise.") || len(a.Warnings) != 0 {
		t.Fatalf("%q %v", a.Text, a.Warnings)
	}
}

func TestViewerAskingForMarginGetsARefusal(t *testing.T) {
	a := run(t, viewer, "What is our gross margin?", warehouse.Rows{})
	if !strings.HasPrefix(a.Text, "I can't share that: ") || !strings.Contains(a.Text, "sensitive") {
		t.Fatalf("%q", a.Text)
	}
	if a.Trace[0].Allowed || a.Trace[0].Code != "forbidden" {
		t.Fatalf("trace %+v", a.Trace)
	}
}

func TestRawSQLFromAnAnalyst(t *testing.T) {
	a := run(t, analyst, "select plan, count(*) as days from gold.revenue_daily group by plan limit 5",
		warehouse.Rows{Columns: []string{"plan", "days"}, Rows: [][]any{{"starter", int64(1004)}, {"team", int64(1004)}, {"enterprise", int64(1004)}}})
	if a.Text != "The query returned 3 rows." || a.Table == nil || len(a.Warnings) != 0 {
		t.Fatalf("%q %v", a.Text, a.Warnings)
	}
}

func TestUnrecognisedQuestionListsMetrics(t *testing.T) {
	a := run(t, viewer, "what's the weather", warehouse.Rows{})
	if !strings.HasPrefix(a.Text, "I did not recognise a metric in the question. You can ask about:") || !strings.Contains(a.Text, "mrr") {
		t.Fatalf("%q", a.Text)
	}
}

func TestRowCountIsAGroundedNumber(t *testing.T) {
	var rows [][]any
	for i := range 25 {
		rows = append(rows, []any{fmt.Sprintf("d%d", i)})
	}
	a := run(t, analyst, "select date from gold.support_daily limit 25", warehouse.Rows{Columns: []string{"date"}, Rows: rows})
	if a.Text != "The query returned 25 rows." || len(a.Warnings) != 0 {
		t.Fatalf("%q %v", a.Text, a.Warnings)
	}
}

func TestPercentagesKeepTheirOwnDecimals(t *testing.T) {
	a := run(t, owner, "Gross margin in September 2026", warehouse.Rows{Columns: []string{"month", "gross_margin_pct"}, Rows: [][]any{{"2026-09", 73.6}}})
	if a.Text != "Gross margin in Sep 2026 was 73.6%." {
		t.Fatalf("%q", a.Text)
	}
}
