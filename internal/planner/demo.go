// Package planner holds the planners that drive the agent: Demo, a
// rule-based planner that needs no key, and OpenRouter, a real model with
// tool calling. Both see the same tools and the same permission checks.
package planner

import (
	"context"
	"fmt"
	"math"
	"regexp"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/kotiniiqq/ai-data-analyst/internal/agent"
	"github.com/kotiniiqq/ai-data-analyst/internal/catalog"
	"github.com/kotiniiqq/ai-data-analyst/internal/tools"
)

// Demo understands metric names and synonyms, periods relative to the last
// day of data, splits by plan or region, comparisons, and raw SQL. It writes
// the answer only from the numbers the tools returned.
type Demo struct {
	end time.Time
}

func NewDemo(dataEnd string) *Demo {
	end, err := time.Parse(time.DateOnly, dataEnd)
	if err != nil {
		panic("planner: bad data end " + dataEnd)
	}
	return &Demo{end: end}
}

func (d *Demo) Next(_ context.Context, st agent.State) (agent.Step, error) {
	if len(st.History) == 0 {
		return agent.Step{ToolCalls: d.plan(st.Question)}, nil
	}
	return agent.Step{Final: d.answer(st)}, nil
}

// --- understanding the question ---

var synonyms = []struct {
	phrase, metric string
}{
	{"gross margin", "gross_margin_pct"}, {"margin", "gross_margin_pct"},
	{"infrastructure cost", "infra_cost"}, {"infra cost", "infra_cost"}, {"server cost", "infra_cost"}, {"cloud cost", "infra_cost"},
	{"servers", "servers"},
	{"new mrr", "new_mrr"}, {"churned mrr", "churned_mrr"}, {"lost mrr", "churned_mrr"}, {"mrr churn", "churned_mrr"},
	{"recurring revenue", "mrr"}, {"mrr", "mrr"},
	{"new customers", "new_customers"}, {"signups", "new_customers"}, {"sign-ups", "new_customers"},
	{"churned customers", "churned_customers"}, {"customer churn", "churned_customers"}, {"lost customers", "churned_customers"}, {"churn", "churned_customers"},
	{"active customers", "active_customers"}, {"customers", "active_customers"},
	{"support tickets", "tickets"}, {"tickets", "tickets"},
	{"first response", "first_response_min"}, {"response time", "first_response_min"},
	{"csat", "csat"}, {"satisfaction", "csat"},
	{"revenue", "revenue"}, {"cost", "cost"},
}

var (
	pairPattern    = regexp.MustCompile(`\b(new|churned|active|lost)\s+and\s+(new|churned|active|lost)\s+(customers|mrr)\b`)
	lastNPattern   = regexp.MustCompile(`\b(?:last|past)\s+(\d{1,2})\s+months?\b`)
	quarterPattern = regexp.MustCompile(`\bq([1-4])(?:\s+(\d{4}))?\b`)
	sincePattern   = regexp.MustCompile(`\bsince\s+([a-z]+)\s+(\d{4})\b`)
	monthYearPat   = regexp.MustCompile(`\b([a-z]+)\s+(\d{4})\b`)
	yearPattern    = regexp.MustCompile(`\b(20\d\d)\b`)
	nonWordPattern = regexp.MustCompile(`[^a-z0-9\-]+`)
	monthNames     = []string{"january", "february", "march", "april", "may", "june", "july", "august", "september", "october", "november", "december"}
)

func monthIndex(word string) int {
	for i, m := range monthNames {
		if word == m || (len(word) >= 3 && strings.HasPrefix(m, word)) {
			return i + 1
		}
	}
	return 0
}

func (d *Demo) plan(question string) []agent.ToolCall {
	raw := strings.TrimSpace(question)
	low := strings.ToLower(raw)
	if strings.HasPrefix(low, "select ") || strings.HasPrefix(low, "with ") {
		return []agent.ToolCall{{ID: "call_1", Name: "run_sql", Args: map[string]any{"sql": raw}}}
	}
	text := " " + nonWordPattern.ReplaceAllString(pairPattern.ReplaceAllString(low, "$1 $3 and $2 $3"), " ") + " "
	if strings.Contains(text, " table") {
		return []agent.ToolCall{{ID: "call_1", Name: "list_tables", Args: map[string]any{}}}
	}
	metrics := findMetrics(text)
	if len(metrics) == 0 {
		return []agent.ToolCall{{ID: "call_1", Name: "list_metrics", Args: map[string]any{}}}
	}
	from, to := d.period(text)
	by := ""
	switch {
	case strings.Contains(text, " plan"):
		by = "plan"
	case strings.Contains(text, " region"):
		by = "region"
	}
	cat := catalog.Default()
	var calls []agent.ToolCall
	for i, name := range metrics {
		args := map[string]any{"metric": name, "from": from, "to": to}
		if m, _ := cat.Metric(name); by != "" && slices.Contains(m.Dims, by) {
			args["by"] = by
		}
		calls = append(calls, agent.ToolCall{ID: fmt.Sprintf("call_%d", i+1), Name: "metric_series", Args: args})
	}
	return calls
}

func findMetrics(text string) []string {
	type hit struct {
		pos    int
		metric string
	}
	var hits []hit
	for _, s := range synonyms {
		needle := " " + s.phrase
		for {
			i := strings.Index(text, needle)
			if i < 0 {
				break
			}
			end := i + len(needle)
			if end < len(text) && text[end] != ' ' && text[end] != 's' { // whole words (allow a plural s)
				text = text[:i] + strings.Repeat("_", len(needle)) + text[end:]
				continue
			}
			hits = append(hits, hit{i, s.metric})
			text = text[:i] + strings.Repeat("_", len(needle)) + text[end:]
		}
	}
	sort.SliceStable(hits, func(a, b int) bool { return hits[a].pos < hits[b].pos })
	var out []string
	for _, h := range hits {
		if !slices.Contains(out, h.metric) {
			out = append(out, h.metric)
		}
	}
	return out
}

func day(t time.Time) string { return t.Format(time.DateOnly) }

func monthStart(y, m int) time.Time { return time.Date(y, time.Month(m), 1, 0, 0, 0, 0, time.UTC) }

func (d *Demo) clip(to time.Time) time.Time {
	if to.After(d.end) {
		return d.end
	}
	return to
}

// period returns from and to (YYYY-MM-DD) for the question, relative to the data end.
func (d *Demo) period(text string) (string, string) {
	endMonth := monthStart(d.end.Year(), int(d.end.Month()))
	if m := lastNPattern.FindStringSubmatch(text); m != nil {
		n, _ := strconv.Atoi(m[1])
		return day(endMonth.AddDate(0, -(max(n, 1) - 1), 0)), day(d.end)
	}
	switch {
	case strings.Contains(text, " last month "):
		return day(endMonth), day(d.end)
	case strings.Contains(text, " this year ") || strings.Contains(text, " ytd "):
		return day(monthStart(d.end.Year(), 1)), day(d.end)
	case strings.Contains(text, " last year "):
		y := d.end.Year() - 1
		return day(monthStart(y, 1)), day(monthStart(y, 12).AddDate(0, 1, -1))
	}
	if m := quarterPattern.FindStringSubmatch(text); m != nil {
		q, _ := strconv.Atoi(m[1])
		y := d.end.Year()
		if m[2] != "" {
			y, _ = strconv.Atoi(m[2])
		}
		start := monthStart(y, (q-1)*3+1)
		return day(start), day(d.clip(start.AddDate(0, 3, -1)))
	}
	if m := sincePattern.FindStringSubmatch(text); m != nil && monthIndex(m[1]) > 0 {
		y, _ := strconv.Atoi(m[2])
		return day(monthStart(y, monthIndex(m[1]))), day(d.end)
	}
	for _, m := range monthYearPat.FindAllStringSubmatch(text, -1) {
		if mi := monthIndex(m[1]); mi > 0 && m[1] != "in" {
			y, _ := strconv.Atoi(m[2])
			start := monthStart(y, mi)
			return day(start), day(d.clip(start.AddDate(0, 1, -1)))
		}
	}
	if m := yearPattern.FindStringSubmatch(text); m != nil {
		y, _ := strconv.Atoi(m[1])
		return day(monthStart(y, 1)), day(d.clip(monthStart(y, 12).AddDate(0, 1, -1)))
	}
	return day(endMonth.AddDate(0, -11, 0)), day(d.end)
}

// --- writing the answer ---

var displayNames = map[string]string{
	"mrr": "MRR", "new_mrr": "New MRR", "churned_mrr": "Churned MRR",
	"active_customers": "Active customers", "new_customers": "New customers", "churned_customers": "Churned customers",
	"tickets": "Support tickets", "first_response_min": "First response time", "csat": "CSAT",
	"revenue": "Revenue", "cost": "Cost", "gross_margin_pct": "Gross margin",
	"infra_cost": "Infrastructure cost", "servers": "Servers",
}

// additive metrics have a meaningful total over months.
var additive = map[string]bool{
	"new_mrr": true, "churned_mrr": true, "new_customers": true, "churned_customers": true,
	"tickets": true, "revenue": true, "cost": true, "infra_cost": true,
}

func (d *Demo) answer(st agent.State) string {
	asked := strings.ToLower(st.Question)
	var parts []string
	for _, t := range st.History {
		if t.Err != nil {
			if t.Err.Code == "forbidden" {
				parts = append(parts, "I can't share that: "+t.Err.Msg+".")
			} else {
				parts = append(parts, "I couldn't get that: "+t.Err.Msg+".")
			}
			continue
		}
		switch t.Call.Name {
		case "list_metrics":
			var names []string
			if ms, ok := t.Output.Data.([]catalog.Metric); ok {
				for _, m := range ms {
					names = append(names, m.Name)
				}
			}
			lead := "I did not recognise a metric in the question. You can ask about: "
			if strings.Contains(asked, "ask") || strings.Contains(asked, "metric") || strings.Contains(asked, "help") {
				lead = "You can ask about: "
			}
			parts = append(parts, lead+strings.Join(names, ", ")+".")
		case "list_tables":
			var names []string
			if ts, ok := t.Output.Data.([]catalog.Table); ok {
				for _, tb := range ts {
					names = append(names, tb.Name)
				}
			}
			parts = append(parts, "Tables you can query: "+strings.Join(names, ", ")+".")
		case "run_sql":
			parts = append(parts, sqlSentence(t.Output))
		case "metric_series":
			if s, ok := t.Output.Data.(tools.SeriesSummary); ok {
				parts = append(parts, seriesSentence(s))
			}
		}
	}
	return strings.Join(parts, " ")
}

func sqlSentence(o tools.Output) string {
	if o.Rows == nil {
		return "The query returned nothing."
	}
	if len(o.Rows.Rows) == 1 && len(o.Rows.Columns) == 1 {
		return fmt.Sprintf("The query returned %v.", o.Rows.Rows[0][0])
	}
	if len(o.Rows.Rows) == 1 {
		return "The query returned 1 row."
	}
	return fmt.Sprintf("The query returned %d rows.", len(o.Rows.Rows))
}

func monthLabel(ym string) string {
	t, err := time.Parse("2006-01", ym)
	if err != nil {
		return ym
	}
	return t.Format("Jan 2006")
}

func seriesSentence(s tools.SeriesSummary) string {
	name := displayNames[s.Metric]
	if name == "" {
		name = s.Metric
	}
	f := func(v float64) string { return withUnit(v, s.Unit, false) }
	if s.By != "" {
		if len(s.Groups) == 0 {
			return fmt.Sprintf("No %s data between %s and %s.", name, s.From, s.To)
		}
		gs := slices.Clone(s.Groups)
		sort.SliceStable(gs, func(a, b int) bool { return gs[a].Last > gs[b].Last })
		var items []string
		for _, g := range gs {
			items = append(items, g.Key+" "+f(g.Last))
		}
		out := fmt.Sprintf("%s by %s, %s: %s. Highest: %s.", name, s.By, monthLabel(gs[0].LastMonth), strings.Join(items, ", "), gs[0].Key)
		if gs[0].Points > 1 {
			sort.SliceStable(gs, func(a, b int) bool { return gs[a].ChangePct > gs[b].ChangePct })
			out += fmt.Sprintf(" Fastest change since %s: %s (%s%%).", monthLabel(gs[0].FirstMonth), gs[0].Key, signed(gs[0].ChangePct))
		}
		return out
	}
	if s.Points == 0 {
		return fmt.Sprintf("No %s data between %s and %s.", name, s.From, s.To)
	}
	if s.Points == 1 {
		return fmt.Sprintf("%s in %s was %s.", name, monthLabel(s.FirstMonth), f(s.Last))
	}
	out := fmt.Sprintf("%s went from %s in %s to %s in %s (%s, %s%%). Lowest %s, highest %s",
		name, f(s.First), monthLabel(s.FirstMonth), f(s.Last), monthLabel(s.LastMonth),
		withUnit(s.Change, s.Unit, true), signed(s.ChangePct), f(s.Min), f(s.Max))
	if additive[s.Metric] {
		out += ", total " + f(s.Total)
	}
	return out + "."
}

func signed(v float64) string {
	if v >= 0 {
		return "+" + num(v)
	}
	return "-" + num(-v)
}

func withUnit(v float64, unit string, sign bool) string {
	prefix := ""
	if sign {
		prefix = "+"
		if v < 0 {
			prefix, v = "-", -v
		}
	} else if v < 0 {
		prefix, v = "-", -v
	}
	switch unit {
	case "USD":
		return prefix + "$" + cents(num(v))
	case "%":
		return prefix + num(v) + "%"
	case "minutes":
		return prefix + num(v) + " min"
	default:
		return prefix + num(v)
	}
}

// cents pads money with one decimal to two: 1,234.5 -> 1,234.50.
func cents(s string) string {
	if i := strings.IndexByte(s, '.'); i >= 0 && len(s)-i == 2 {
		return s + "0"
	}
	return s
}

// num formats with thousands separators and at most two decimals.
func num(v float64) string {
	v = math.Round(v*100) / 100
	s := strconv.FormatFloat(v, 'f', -1, 64)
	intPart, frac, _ := strings.Cut(s, ".")
	var b strings.Builder
	for i, c := range intPart {
		if i > 0 && (len(intPart)-i)%3 == 0 {
			b.WriteByte(',')
		}
		b.WriteRune(c)
	}
	if frac != "" {
		return b.String() + "." + frac
	}
	return b.String()
}
