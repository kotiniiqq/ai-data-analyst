package tools

import (
	"context"
	"errors"
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"

	"github.com/kotiniiqq/ai-data-analyst/internal/approval"
	"github.com/kotiniiqq/ai-data-analyst/internal/catalog"
	"github.com/kotiniiqq/ai-data-analyst/internal/cost"
	"github.com/kotiniiqq/ai-data-analyst/internal/rbac"
	"github.com/kotiniiqq/ai-data-analyst/internal/sqlguard"
	"github.com/kotiniiqq/ai-data-analyst/internal/warehouse"
)

// Builtins holds what the built-in tools need.
type Builtins struct {
	Catalog   *catalog.Catalog
	Warehouse warehouse.Provider
	Estimator cost.Estimator
	Approvals *approval.Store
	Now       func() time.Time
	DataStart string // first day with data, YYYY-MM-DD
	DataEnd   string // last day with data, YYYY-MM-DD
}

func (b *Builtins) Tools() []Tool {
	view := []rbac.Perm{rbac.MetricsView}
	return []Tool{
		{Name: "list_metrics", Description: "List the business metrics you can ask about, with units and the ways each can be split.",
			RequiredPerms: view, Execute: b.listMetrics},
		{Name: "describe_metric", Description: "Describe one metric: meaning, unit, source table, possible splits.",
			Params:        []Param{{Name: "metric", Type: "string", Description: "metric name from list_metrics", Required: true}},
			RequiredPerms: view, Execute: b.describeMetric},
		{Name: "metric_series", Description: "Monthly values of a metric for a period, optionally split by a dimension. " +
			"Returns the rows and a summary (first, last, change, change %, min, max, average, total) computed by code.",
			Params: []Param{
				{Name: "metric", Type: "string", Description: "metric name from list_metrics", Required: true},
				{Name: "from", Type: "string", Description: "first day, YYYY-MM-DD (default: 12 months before the latest data)"},
				{Name: "to", Type: "string", Description: "last day, YYYY-MM-DD (default: the latest data, " + b.DataEnd + ")"},
				{Name: "by", Type: "string", Description: "optional split, one of the metric's dimensions (plan or region)"},
			},
			RequiredPerms: view, Execute: b.metricSeries},
		{Name: "list_tables", Description: "List the warehouse tables you may query.",
			RequiredPerms: []rbac.Perm{rbac.SQLView}, Execute: b.listTables},
		{Name: "describe_table", Description: "Columns of one warehouse table.",
			Params:        []Param{{Name: "table", Type: "string", Description: "qualified name, e.g. gold.revenue_daily", Required: true}},
			RequiredPerms: []rbac.Perm{rbac.SQLView}, Execute: b.describeTable},
		{Name: "run_sql", Description: "Run one read-only SELECT on schema gold (PostgreSQL syntax, LIMIT required, at most 1000 rows). " +
			"Expensive queries are held until the user approves them.",
			Params:        []Param{{Name: "sql", Type: "string", Description: "the SELECT statement", Required: true}},
			RequiredPerms: []rbac.Perm{rbac.SQLRun}, Execute: b.runSQL},
	}
}

func (b *Builtins) listMetrics(_ context.Context, c Call) (Output, error) {
	ms := b.Catalog.Metrics(c.User)
	var lines []string
	for _, m := range ms {
		line := fmt.Sprintf("%s: %s (%s)", m.Name, m.Description, m.Unit)
		if len(m.Dims) > 0 {
			line += ", by " + strings.Join(m.Dims, ", ")
		}
		lines = append(lines, line)
	}
	return Output{Text: strings.Join(lines, "\n"), Data: ms, Numbers: []float64{}}, nil
}

func (b *Builtins) metric(c Call) (catalog.Metric, error) {
	m, ok := b.Catalog.Metric(strings.TrimSpace(c.Args["metric"]))
	if !ok {
		return m, toolErr("unknown_metric", "no metric %q; call list_metrics", c.Args["metric"])
	}
	if !b.Catalog.CanSee(c.User, m) {
		return m, toolErr("forbidden", "%s is sensitive; your role (%s) may not see it", m.Name, c.User.Role)
	}
	return m, nil
}

func (b *Builtins) describeMetric(_ context.Context, c Call) (Output, error) {
	m, err := b.metric(c)
	if err != nil {
		return Output{}, err
	}
	text := fmt.Sprintf("%s: %s. Unit: %s. Monthly, from %s. Data from %s to %s.", m.Name, m.Description, m.Unit, m.Table, b.DataStart, b.DataEnd)
	if len(m.Dims) > 0 {
		text += " Can be split by " + strings.Join(m.Dims, ", ") + "."
	}
	return Output{Text: text, Data: m, Numbers: []float64{}}, nil
}

func (b *Builtins) listTables(_ context.Context, c Call) (Output, error) {
	ts := b.Catalog.Tables(c.User)
	var lines []string
	for _, t := range ts {
		lines = append(lines, t.Name+": "+t.Description)
	}
	return Output{Text: strings.Join(lines, "\n"), Data: ts, Numbers: []float64{}}, nil
}

func (b *Builtins) describeTable(_ context.Context, c Call) (Output, error) {
	t, ok := b.Catalog.Table(strings.TrimSpace(c.Args["table"]))
	if !ok {
		return Output{}, toolErr("bad_input", "no table %q; call list_tables", c.Args["table"])
	}
	if t.Sensitive && !c.User.Can(rbac.DataSensitive) {
		return Output{}, toolErr("forbidden", "%s is sensitive; your role (%s) may not see it", t.Name, c.User.Role)
	}
	var cols []string
	for _, col := range t.Columns {
		cols = append(cols, fmt.Sprintf("%s %s: %s", col.Name, col.Type, col.Description))
	}
	return Output{Text: t.Name + "\n" + strings.Join(cols, "\n"), Data: t, Numbers: []float64{}}, nil
}

func parseDay(s string) (time.Time, error) {
	return time.Parse("2006-01-02", strings.TrimSpace(s))
}

func (b *Builtins) metricSeries(ctx context.Context, c Call) (Output, error) {
	m, err := b.metric(c)
	if err != nil {
		return Output{}, err
	}
	dataStart, _ := parseDay(b.DataStart)
	dataEnd, _ := parseDay(b.DataEnd)
	from := dataEnd.AddDate(0, -11, 0)
	from = time.Date(from.Year(), from.Month(), 1, 0, 0, 0, 0, time.UTC)
	to := dataEnd
	if s := c.Args["from"]; s != "" {
		if from, err = parseDay(s); err != nil {
			return Output{}, toolErr("bad_input", "from must be a date YYYY-MM-DD, got %q", s)
		}
	}
	if s := c.Args["to"]; s != "" {
		if to, err = parseDay(s); err != nil {
			return Output{}, toolErr("bad_input", "to must be a date YYYY-MM-DD, got %q", s)
		}
	}
	if from.After(to) {
		return Output{}, toolErr("bad_input", "from (%s) is after to (%s)", from.Format(time.DateOnly), to.Format(time.DateOnly))
	}
	if to.Before(dataStart) || from.After(dataEnd) {
		return Output{}, toolErr("bad_input", "no data in that period; data covers %s to %s", b.DataStart, b.DataEnd)
	}
	by := strings.ToLower(strings.TrimSpace(c.Args["by"]))
	if by != "" && !containsFold(m.Dims, by) {
		if len(m.Dims) == 0 {
			return Output{}, toolErr("bad_input", "%s cannot be split", m.Name)
		}
		return Output{}, toolErr("bad_input", "%s can be split only by %s", m.Name, strings.Join(m.Dims, ", "))
	}

	sql := seriesSQL(b.Warehouse.Dialect(), m, from.Format(time.DateOnly), to.Format(time.DateOnly), by)
	out, err := b.runChecked(ctx, c.User, sql, false)
	if err != nil || out.Approval != nil {
		return out, err
	}
	s := summarise(m, by, out.Rows, from.Format(time.DateOnly), to.Format(time.DateOnly))
	out.Data = s
	out.Text = s.text()
	return out, nil
}

func containsFold(list []string, s string) bool {
	for _, x := range list {
		if strings.EqualFold(x, s) {
			return true
		}
	}
	return false
}

func seriesSQL(dialect string, m catalog.Metric, from, to, by string) string {
	month := fmt.Sprintf("to_char(date_trunc('month', %s), 'YYYY-MM')", m.DateCol)
	if dialect == "bigquery" {
		month = fmt.Sprintf("FORMAT_DATE('%%Y-%%m', DATE_TRUNC(%s, MONTH))", m.DateCol)
	}
	cols, group := month+" AS month", "1"
	if by != "" {
		cols += ", " + by
		group = "1, 2"
	}
	return fmt.Sprintf("SELECT %s, %s AS %s FROM %s WHERE %s BETWEEN DATE '%s' AND DATE '%s' GROUP BY %s ORDER BY %s LIMIT 1000",
		cols, m.Expr, m.Name, m.Table, m.DateCol, from, to, group, group)
}

func (b *Builtins) runSQL(ctx context.Context, c Call) (Output, error) {
	return b.runChecked(ctx, c.User, c.Args["sql"], false)
}

// runChecked is the single path to the warehouse: guard, estimate, then run
// or hold for approval. approved skips the hold for a query the user confirmed;
// the guard still runs.
func (b *Builtins) runChecked(ctx context.Context, u rbac.User, sql string, approved bool) (Output, error) {
	res, err := sqlguard.Check(sql, u, b.Catalog)
	if err != nil {
		return Output{}, toolErr("sql_rejected", "%v", err)
	}
	if !approved {
		est, err := b.Estimator.Estimate(ctx, res.SQL)
		if err != nil {
			return Output{}, toolErr("warehouse", "could not plan the query: %v", err)
		}
		if est.NeedsApproval {
			tok := b.Approvals.Put(u.ID, res.SQL, b.Now())
			return Output{
				Text:     fmt.Sprintf("This query would process about %s (~$%.4f). It waits for the user's approval.", humanBytes(est.Bytes), est.USD),
				SQL:      res.SQL,
				Approval: &Approval{Token: tok, SQL: res.SQL, Estimate: est},
				Numbers:  []float64{},
			}, nil
		}
	}
	rows, err := b.Warehouse.Query(ctx, res.SQL)
	if err != nil {
		var we *warehouse.Error
		if errors.As(err, &we) {
			return Output{}, toolErr("warehouse", "%s", we.Msg)
		}
		return Output{}, toolErr("warehouse", "the query failed")
	}
	return Output{Text: fmt.Sprintf("%d rows", len(rows.Rows)), Rows: &rows, SQL: res.SQL}, nil
}

func humanBytes(n int64) string {
	switch {
	case n >= 1<<30:
		return fmt.Sprintf("%.1f GiB", float64(n)/(1<<30))
	case n >= 1<<20:
		return fmt.Sprintf("%.0f MiB", float64(n)/(1<<20))
	default:
		return fmt.Sprintf("%d KiB", n>>10)
	}
}

// SeriesSummary is computed by code so a planner never has to do arithmetic.
type SeriesSummary struct {
	Metric string       `json:"metric"`
	Unit   string       `json:"unit"`
	From   string       `json:"from"`
	To     string       `json:"to"`
	By     string       `json:"by,omitempty"`
	Stats               // whole series when not split
	Groups []GroupStats `json:"groups,omitempty"`
}

type Stats struct {
	Points     int     `json:"points"`
	FirstMonth string  `json:"first_month,omitempty"`
	LastMonth  string  `json:"last_month,omitempty"`
	First      float64 `json:"first"`
	Last       float64 `json:"last"`
	Change     float64 `json:"change"`
	ChangePct  float64 `json:"change_pct"`
	Min        float64 `json:"min"`
	Max        float64 `json:"max"`
	Avg        float64 `json:"avg"`
	Total      float64 `json:"total"`
}

type GroupStats struct {
	Key string `json:"key"`
	Stats
}

func round2(x float64) float64 { return math.Round(x*100) / 100 }

func statsOf(months []string, vals []float64) Stats {
	if len(vals) == 0 {
		return Stats{}
	}
	s := Stats{Points: len(vals), FirstMonth: months[0], LastMonth: months[len(months)-1],
		First: vals[0], Last: vals[len(vals)-1], Min: vals[0], Max: vals[0]}
	for _, v := range vals {
		s.Min, s.Max, s.Total = min(s.Min, v), max(s.Max, v), s.Total+v
	}
	s.Total = round2(s.Total)
	s.Avg = round2(s.Total / float64(len(vals)))
	s.Change = round2(s.Last - s.First)
	if s.First != 0 {
		s.ChangePct = round2(s.Change / s.First * 100)
	}
	return s
}

func toFloat(v any) (float64, bool) {
	switch x := v.(type) {
	case float64:
		return x, true
	case int64:
		return float64(x), true
	}
	return 0, false
}

func summarise(m catalog.Metric, by string, rows *warehouse.Rows, from, to string) SeriesSummary {
	s := SeriesSummary{Metric: m.Name, Unit: m.Unit, From: from, To: to, By: by}
	if rows == nil {
		return s
	}
	if by == "" {
		var months []string
		var vals []float64
		for _, r := range rows.Rows {
			if v, ok := toFloat(r[1]); ok {
				months, vals = append(months, fmt.Sprint(r[0])), append(vals, v)
			}
		}
		s.Stats = statsOf(months, vals)
		return s
	}
	type series struct {
		months []string
		vals   []float64
	}
	groups := map[string]*series{}
	var order []string
	for _, r := range rows.Rows {
		key := fmt.Sprint(r[1])
		v, ok := toFloat(r[2])
		if !ok {
			continue
		}
		if groups[key] == nil {
			groups[key] = &series{}
			order = append(order, key)
		}
		groups[key].months = append(groups[key].months, fmt.Sprint(r[0]))
		groups[key].vals = append(groups[key].vals, v)
	}
	for _, k := range order {
		s.Groups = append(s.Groups, GroupStats{Key: k, Stats: statsOf(groups[k].months, groups[k].vals)})
	}
	return s
}

func (s SeriesSummary) numbers() []float64 {
	all := []Stats{s.Stats}
	for _, g := range s.Groups {
		all = append(all, g.Stats)
	}
	var out []float64
	for _, st := range all {
		out = append(out, st.First, st.Last, st.Change, st.ChangePct, st.Min, st.Max, st.Avg, st.Total, float64(st.Points))
	}
	return out
}

func (s SeriesSummary) text() string {
	if s.By == "" {
		st := s.Stats
		if st.Points == 0 {
			return fmt.Sprintf("%s: no data between %s and %s", s.Metric, s.From, s.To)
		}
		return fmt.Sprintf("%s (%s), %d months %s..%s: first %s, last %s, change %s (%s%%), min %s, max %s, avg %s, total %s",
			s.Metric, s.Unit, st.Points, st.FirstMonth, st.LastMonth, plain(st.First), plain(st.Last), plain(st.Change), plain(st.ChangePct), plain(st.Min), plain(st.Max), plain(st.Avg), plain(st.Total))
	}
	var parts []string
	for _, g := range s.Groups {
		parts = append(parts, fmt.Sprintf("%s: first %s, last %s, change %s (%s%%), total %s", g.Key, plain(g.First), plain(g.Last), plain(g.Change), plain(g.ChangePct), plain(g.Total)))
	}
	return fmt.Sprintf("%s (%s) by %s, %s..%s. %s", s.Metric, s.Unit, s.By, s.From, s.To, strings.Join(parts, "; "))
}

// plain prints a number without exponent notation.
func plain(v float64) string { return strconv.FormatFloat(v, 'f', -1, 64) }
