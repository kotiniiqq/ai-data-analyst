package catalog

import (
	"slices"
	"strings"
	"testing"

	"github.com/kotiniiqq/ai-data-analyst/internal/rbac"
)

var (
	viewer  = rbac.NewUser(1, "viewer@demo.local", "viewer")
	analyst = rbac.NewUser(2, "analyst@demo.local", "analyst")
	owner   = rbac.NewUser(3, "owner@demo.local", "owner")
)

func names(ms []Metric) []string {
	var out []string
	for _, m := range ms {
		out = append(out, m.Name)
	}
	return out
}

func TestSensitiveMetricsHiddenWithoutPermission(t *testing.T) {
	c := Default()
	for _, u := range []rbac.User{viewer, analyst} {
		got := names(c.Metrics(u))
		if slices.Contains(got, "gross_margin_pct") || slices.Contains(got, "infra_cost") {
			t.Fatalf("%s sees sensitive metrics: %v", u.Role, got)
		}
		if !slices.Contains(got, "mrr") {
			t.Fatalf("%s does not see mrr", u.Role)
		}
	}
	if !slices.Contains(names(c.Metrics(owner)), "gross_margin_pct") {
		t.Fatal("owner does not see gross_margin_pct")
	}
}

func TestSensitiveTablesHiddenWithoutPermission(t *testing.T) {
	c := Default()
	for _, tb := range c.Tables(analyst) {
		if tb.Sensitive {
			t.Fatalf("analyst sees %s", tb.Name)
		}
	}
	if len(c.Tables(owner)) != 5 {
		t.Fatalf("owner sees %d tables, want 5", len(c.Tables(owner)))
	}
}

func TestEveryMetricPointsAtARealTableAndColumns(t *testing.T) {
	c := Default()
	for _, m := range c.Metrics(owner) {
		tb, ok := c.Table(m.Table)
		if !ok {
			t.Fatalf("%s: unknown table %s", m.Name, m.Table)
		}
		if !strings.HasPrefix(m.Table, "gold.") {
			t.Fatalf("%s: table must be qualified", m.Name)
		}
		if !tb.HasColumn(m.DateCol) {
			t.Fatalf("%s: no date column %s", m.Name, m.DateCol)
		}
		for _, d := range m.Dims {
			if !tb.HasColumn(d) {
				t.Fatalf("%s: dim %s is not a column of %s", m.Name, d, m.Table)
			}
		}
		if m.Sensitive != tb.Sensitive {
			t.Fatalf("%s: sensitivity differs from its table", m.Name)
		}
		if m.Description == "" || m.Unit == "" || m.Expr == "" {
			t.Fatalf("%s: incomplete", m.Name)
		}
	}
}

func TestLookupIsCaseInsensitive(t *testing.T) {
	c := Default()
	if _, ok := c.Metric("MRR"); !ok {
		t.Fatal("MRR not found")
	}
	if _, ok := c.Table("GOLD.Revenue_Daily"); !ok {
		t.Fatal("table lookup is case sensitive")
	}
	if _, ok := c.Metric("nope"); ok {
		t.Fatal("unknown metric found")
	}
}
