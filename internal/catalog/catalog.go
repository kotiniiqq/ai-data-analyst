// Package catalog describes what the analyst may talk about: the warehouse
// tables, their sensitivity, and metrics defined on top of them. Metric SQL
// is built from these definitions, never from free text.
package catalog

import (
	"slices"
	"strings"

	"github.com/kotiniiqq/ai-data-analyst/internal/rbac"
)

type Column struct {
	Name        string `json:"name"`
	Type        string `json:"type"`
	Description string `json:"description"`
}

type Table struct {
	Name        string   `json:"name"`
	Description string   `json:"description"`
	Columns     []Column `json:"columns"`
	Sensitive   bool     `json:"sensitive"`
}

func (t Table) HasColumn(name string) bool {
	return slices.ContainsFunc(t.Columns, func(c Column) bool { return strings.EqualFold(c.Name, name) })
}

// Metric is aggregated per month: Expr is an aggregate over Table, DateCol
// selects the period, Dims are the columns a series may be split by.
type Metric struct {
	Name        string   `json:"name"`
	Description string   `json:"description"`
	Unit        string   `json:"unit"`
	Table       string   `json:"table"`
	DateCol     string   `json:"-"`
	Expr        string   `json:"-"`
	Dims        []string `json:"dims,omitempty"`
	Sensitive   bool     `json:"sensitive"`
}

type Catalog struct {
	tables  []Table
	metrics []Metric
}

func visible(u rbac.User, sensitive bool) bool {
	return !sensitive || u.Can(rbac.DataSensitive)
}

// Metrics returns the metrics u may see.
func (c *Catalog) Metrics(u rbac.User) []Metric {
	var out []Metric
	for _, m := range c.metrics {
		if visible(u, m.Sensitive) {
			out = append(out, m)
		}
	}
	return out
}

// Tables returns the tables u may see.
func (c *Catalog) Tables(u rbac.User) []Table {
	var out []Table
	for _, t := range c.tables {
		if visible(u, t.Sensitive) {
			out = append(out, t)
		}
	}
	return out
}

// Metric finds a metric by name regardless of visibility; callers check access.
func (c *Catalog) Metric(name string) (Metric, bool) {
	i := slices.IndexFunc(c.metrics, func(m Metric) bool { return strings.EqualFold(m.Name, name) })
	if i < 0 {
		return Metric{}, false
	}
	return c.metrics[i], true
}

// Table finds a table by qualified name regardless of visibility.
func (c *Catalog) Table(name string) (Table, bool) {
	i := slices.IndexFunc(c.tables, func(t Table) bool { return strings.EqualFold(t.Name, name) })
	if i < 0 {
		return Table{}, false
	}
	return c.tables[i], true
}

func (c *Catalog) CanSee(u rbac.User, m Metric) bool { return visible(u, m.Sensitive) }

func col(name, typ, desc string) Column { return Column{Name: name, Type: typ, Description: desc} }

// Default is the catalog of the synthetic Northwind Cloud warehouse.
func Default() *Catalog {
	tables := []Table{
		{Name: "gold.revenue_daily", Description: "Monthly recurring revenue per day and plan",
			Columns: []Column{col("date", "date", "day"), col("plan", "text", "starter, team or enterprise"),
				col("mrr_usd", "numeric", "MRR at the end of the day, USD"),
				col("new_mrr_usd", "numeric", "MRR added that day, USD"),
				col("churned_mrr_usd", "numeric", "MRR lost that day, USD")}},
		{Name: "gold.customers_monthly", Description: "Customers per month and plan",
			Columns: []Column{col("month", "date", "first day of the month"), col("plan", "text", "starter, team or enterprise"),
				col("active", "integer", "active customers"), col("new", "integer", "customers who joined"),
				col("churned", "integer", "customers who left")}},
		{Name: "gold.support_daily", Description: "Support desk per day",
			Columns: []Column{col("date", "date", "day"), col("tickets", "integer", "tickets opened"),
				col("first_response_min", "numeric", "median first response, minutes"),
				col("csat", "numeric", "satisfaction score, 1 to 5")}},
		{Name: "gold.finance_monthly", Description: "Revenue, cost and gross margin per month", Sensitive: true,
			Columns: []Column{col("month", "date", "first day of the month"), col("revenue_usd", "numeric", "revenue, USD"),
				col("cost_usd", "numeric", "cost of revenue, USD"), col("gross_margin_pct", "numeric", "gross margin, %")}},
		{Name: "gold.infra_daily", Description: "Servers and infrastructure cost per day and region", Sensitive: true,
			Columns: []Column{col("date", "date", "day"), col("region", "text", "eu-west, us-east or ap-south"),
				col("servers", "integer", "servers running"), col("cost_usd", "numeric", "infrastructure cost, USD")}},
	}
	plan := []string{"plan"}
	metrics := []Metric{
		{Name: "mrr", Description: "Monthly recurring revenue (average over the month's days)", Unit: "USD",
			Table: "gold.revenue_daily", DateCol: "date", Expr: "round(sum(mrr_usd) / count(DISTINCT date), 2)", Dims: plan},
		{Name: "new_mrr", Description: "MRR added during the month", Unit: "USD",
			Table: "gold.revenue_daily", DateCol: "date", Expr: "round(sum(new_mrr_usd), 2)", Dims: plan},
		{Name: "churned_mrr", Description: "MRR lost during the month", Unit: "USD",
			Table: "gold.revenue_daily", DateCol: "date", Expr: "round(sum(churned_mrr_usd), 2)", Dims: plan},
		{Name: "active_customers", Description: "Active customers", Unit: "customers",
			Table: "gold.customers_monthly", DateCol: "month", Expr: "sum(active)", Dims: plan},
		{Name: "new_customers", Description: "Customers who joined", Unit: "customers",
			Table: "gold.customers_monthly", DateCol: "month", Expr: "sum(new)", Dims: plan},
		{Name: "churned_customers", Description: "Customers who left", Unit: "customers",
			Table: "gold.customers_monthly", DateCol: "month", Expr: "sum(churned)", Dims: plan},
		{Name: "tickets", Description: "Support tickets opened", Unit: "tickets",
			Table: "gold.support_daily", DateCol: "date", Expr: "sum(tickets)"},
		{Name: "first_response_min", Description: "Average first response time", Unit: "minutes",
			Table: "gold.support_daily", DateCol: "date", Expr: "round(avg(first_response_min), 1)"},
		{Name: "csat", Description: "Average customer satisfaction (1 to 5)", Unit: "score",
			Table: "gold.support_daily", DateCol: "date", Expr: "round(avg(csat), 2)"},
		{Name: "revenue", Description: "Revenue", Unit: "USD", Sensitive: true,
			Table: "gold.finance_monthly", DateCol: "month", Expr: "round(sum(revenue_usd), 2)"},
		{Name: "cost", Description: "Cost of revenue", Unit: "USD", Sensitive: true,
			Table: "gold.finance_monthly", DateCol: "month", Expr: "round(sum(cost_usd), 2)"},
		{Name: "gross_margin_pct", Description: "Gross margin", Unit: "%", Sensitive: true,
			Table: "gold.finance_monthly", DateCol: "month", Expr: "round((sum(revenue_usd) - sum(cost_usd)) / sum(revenue_usd) * 100, 1)"},
		{Name: "infra_cost", Description: "Infrastructure cost", Unit: "USD", Sensitive: true,
			Table: "gold.infra_daily", DateCol: "date", Expr: "round(sum(cost_usd), 2)", Dims: []string{"region"}},
		{Name: "servers", Description: "Servers running (average over the month's days)", Unit: "servers", Sensitive: true,
			Table: "gold.infra_daily", DateCol: "date", Expr: "round(sum(servers) * 1.0 / count(DISTINCT date), 1)", Dims: []string{"region"}},
	}
	return &Catalog{tables: tables, metrics: metrics}
}
