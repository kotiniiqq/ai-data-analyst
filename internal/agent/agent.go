// Package agent runs the question loop: the planner proposes tool calls, the
// registry runs them (with permissions and audit), and the planner writes the
// final answer, which must not contain numbers the tools did not return.
package agent

import (
	"context"
	"errors"
	"fmt"
	"regexp"

	"github.com/kotiniiqq/ai-data-analyst/internal/rbac"
	"github.com/kotiniiqq/ai-data-analyst/internal/tools"
	"github.com/kotiniiqq/ai-data-analyst/internal/warehouse"
)

// MaxSteps caps planner rounds per question.
const MaxSteps = 6

type ToolCall struct {
	ID   string         `json:"id"`
	Name string         `json:"name"`
	Args map[string]any `json:"args"`
}

type Turn struct {
	Call   ToolCall
	Output tools.Output
	Err    *tools.ToolError
}

type State struct {
	User     rbac.User
	Question string
	Tools    []tools.Tool
	History  []Turn
}

// Step is either some tool calls or a final answer.
type Step struct {
	ToolCalls []ToolCall
	Final     string
}

type Planner interface {
	Next(ctx context.Context, s State) (Step, error)
}

type TraceItem struct {
	Tool    string         `json:"tool"`
	Args    map[string]any `json:"args"`
	Allowed bool           `json:"allowed"`
	Code    string         `json:"code,omitempty"`
	Summary string         `json:"summary"`
}

type ChartSeries struct {
	Name   string    `json:"name"`
	Values []float64 `json:"values"`
}

type Chart struct {
	X      []string      `json:"x"`
	Series []ChartSeries `json:"series"`
}

type Answer struct {
	Text     string          `json:"answer"`
	Trace    []TraceItem     `json:"trace"`
	SQL      string          `json:"sql,omitempty"`
	Table    *warehouse.Rows `json:"table,omitempty"`
	Chart    *Chart          `json:"chart,omitempty"`
	Approval *tools.Approval `json:"approval,omitempty"`
	Warnings []string        `json:"warnings"`
}

// Run answers question for u. It never returns an error: problems become
// warnings and a plain answer built from what the tools returned.
func Run(ctx context.Context, p Planner, reg *tools.Registry, u rbac.User, question string) Answer {
	st := State{User: u, Question: question, Tools: reg.Schemas(u)}
	a := Answer{Trace: []TraceItem{}, Warnings: []string{}}
	for range MaxSteps {
		step, err := p.Next(ctx, st)
		if err != nil {
			a.Warnings = append(a.Warnings, "planner_error")
			a.Text = fallback(st, "The assistant could not finish ("+err.Error()+").")
			return a
		}
		if len(step.ToolCalls) == 0 {
			a.Text = step.Final
			if ok, _ := Grounded(step.Final, numbers(st)); !ok {
				a.Warnings = append(a.Warnings, "number_not_grounded")
				a.Text = fallback(st, "The drafted answer quoted numbers the data did not contain, so here is the data itself.")
			}
			return a
		}
		for _, call := range step.ToolCalls {
			out, err := reg.Execute(ctx, u, call.Name, call.Args)
			turn := Turn{Call: call, Output: out}
			item := TraceItem{Tool: call.Name, Args: call.Args, Allowed: true, Summary: out.Text}
			if err != nil {
				var te *tools.ToolError
				if !errors.As(err, &te) {
					te = &tools.ToolError{Code: "error", Msg: err.Error()}
				}
				turn.Err = te
				item.Code, item.Summary = te.Code, te.Msg
				item.Allowed = te.Code != "forbidden" && te.Code != "unknown_tool"
			}
			st.History = append(st.History, turn)
			a.Trace = append(a.Trace, item)
			if err == nil {
				if out.Rows != nil {
					a.Table, a.SQL, a.Chart = out.Rows, out.SQL, ChartOf(out.Rows)
				}
				if out.Approval != nil {
					a.Approval, a.SQL = out.Approval, out.Approval.SQL
					a.Text = out.Text
					return a
				}
			}
		}
	}
	a.Warnings = append(a.Warnings, "step_limit")
	a.Text = fallback(st, "The question needed more steps than allowed.")
	return a
}

func numbers(st State) []float64 {
	var out []float64
	for _, t := range st.History {
		out = append(out, t.Output.Numbers...)
	}
	return out
}

// fallback answers with the last useful tool output, quoted as is.
func fallback(st State, lead string) string {
	for i := len(st.History) - 1; i >= 0; i-- {
		t := st.History[i]
		if t.Err != nil && i == len(st.History)-1 {
			return lead + " " + t.Err.Msg
		}
		if t.Err == nil && t.Output.Text != "" {
			return lead + " What the data shows: " + t.Output.Text
		}
	}
	return lead
}

var monthPattern = regexp.MustCompile(`^\d{4}-\d{2}$`)

// ChartOf draws a line chart when the first column is a month and the last is numeric.
func ChartOf(rows *warehouse.Rows) *Chart {
	if len(rows.Rows) < 2 || len(rows.Columns) < 2 || len(rows.Columns) > 3 {
		return nil
	}
	c := &Chart{}
	idx := map[string]int{}
	xs := map[string]bool{}
	for _, r := range rows.Rows {
		m, ok := r[0].(string)
		if !ok || !monthPattern.MatchString(m) {
			return nil
		}
		v, ok := toFloat(r[len(r)-1])
		if !ok {
			return nil
		}
		name := rows.Columns[len(rows.Columns)-1]
		if len(r) == 3 {
			name = fmt.Sprint(r[1])
		}
		if !xs[m] {
			xs[m] = true
			c.X = append(c.X, m)
		}
		i, seen := idx[name]
		if !seen {
			i = len(c.Series)
			idx[name] = i
			c.Series = append(c.Series, ChartSeries{Name: name})
		}
		c.Series[i].Values = append(c.Series[i].Values, v)
	}
	for _, s := range c.Series {
		if len(s.Values) != len(c.X) {
			return nil // ragged series; a table is clearer
		}
	}
	return c
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
