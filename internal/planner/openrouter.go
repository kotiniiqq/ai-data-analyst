package planner

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/kotiniiqq/ai-data-analyst/internal/agent"
	"github.com/kotiniiqq/ai-data-analyst/internal/tools"
)

// OpenRouterBaseURL is the OpenAI-compatible API root.
const OpenRouterBaseURL = "https://openrouter.ai/api/v1"

// MalformedArgsKey is set on a tool call whose arguments were not valid JSON;
// the registry turns it into a bad_input error the model can correct.
const MalformedArgsKey = tools.MalformedArgsKey

// maxRowsToModel limits how many rows of a result go back to the model.
const maxRowsToModel = 50

// OpenRouter drives the same tools with a real model via tool calling.
type OpenRouter struct {
	BaseURL string
	APIKey  string
	Model   string
	DataEnd string
	Client  *http.Client
}

func (o *OpenRouter) systemPrompt(st agent.State) string {
	return strings.Join([]string{
		"You are the data analyst assistant of Northwind Cloud, a SaaS company with three plans (starter, team, enterprise).",
		"Answer only from tool results. The latest data is from " + o.DataEnd + "; read 'last month' and 'this year' relative to that date.",
		"Never calculate numbers yourself: quote values, changes and percentages exactly as the tools return them (metric_series already returns change and change %).",
		"Prefer metric_series over run_sql. If a tool refuses because of permissions, tell the user plainly which role is needed; do not try to work around it.",
		"If a query waits for approval, say so and stop. Keep answers short. The user's role is " + st.User.Role + ".",
	}, " ")
}

type orMessage struct {
	Role       string       `json:"role"`
	Content    *string      `json:"content"`
	ToolCalls  []orToolCall `json:"tool_calls,omitempty"`
	ToolCallID string       `json:"tool_call_id,omitempty"`
}

type orToolCall struct {
	ID       string `json:"id"`
	Type     string `json:"type"`
	Function struct {
		Name      string `json:"name"`
		Arguments string `json:"arguments"`
	} `json:"function"`
}

func text(s string) *string { return &s }

func (o *OpenRouter) Next(ctx context.Context, st agent.State) (agent.Step, error) {
	msgs := []orMessage{
		{Role: "system", Content: text(o.systemPrompt(st))},
		{Role: "user", Content: text(st.Question)},
	}
	for _, t := range st.History {
		args, _ := json.Marshal(t.Call.Args)
		if raw, ok := t.Call.Args[MalformedArgsKey].(string); ok {
			args = []byte(raw)
		}
		call := orToolCall{ID: t.Call.ID, Type: "function"}
		call.Function.Name, call.Function.Arguments = t.Call.Name, string(args)
		msgs = append(msgs,
			orMessage{Role: "assistant", Content: nil, ToolCalls: []orToolCall{call}},
			orMessage{Role: "tool", ToolCallID: t.Call.ID, Content: text(toolResult(t))})
	}
	var defs []map[string]any
	for _, t := range st.Tools {
		defs = append(defs, map[string]any{"type": "function", "function": map[string]any{
			"name": t.Name, "description": t.Description, "parameters": t.InputSchema(),
		}})
	}
	body := map[string]any{"model": o.Model, "messages": msgs}
	if len(defs) > 0 {
		body["tools"] = defs
	}

	var resp struct {
		Choices []struct {
			Message struct {
				Content   *string      `json:"content"`
				ToolCalls []orToolCall `json:"tool_calls"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err := o.post(ctx, body, &resp); err != nil {
		return agent.Step{}, err
	}
	if len(resp.Choices) == 0 {
		return agent.Step{}, errors.New("the model returned no answer")
	}
	m := resp.Choices[0].Message
	if len(m.ToolCalls) == 0 {
		if m.Content == nil || strings.TrimSpace(*m.Content) == "" {
			return agent.Step{}, errors.New("the model returned an empty answer")
		}
		return agent.Step{Final: strings.TrimSpace(*m.Content)}, nil
	}
	var step agent.Step
	for _, c := range m.ToolCalls {
		args := map[string]any{}
		if strings.TrimSpace(c.Function.Arguments) != "" {
			if err := json.Unmarshal([]byte(c.Function.Arguments), &args); err != nil {
				args = map[string]any{MalformedArgsKey: c.Function.Arguments}
			}
		}
		step.ToolCalls = append(step.ToolCalls, agent.ToolCall{ID: c.ID, Name: c.Function.Name, Args: args})
	}
	return step, nil
}

// toolResult is what the model sees: summary, rows (capped), SQL, or the error.
func toolResult(t agent.Turn) string {
	if t.Err != nil {
		b, _ := json.Marshal(map[string]any{"error": t.Err})
		return string(b)
	}
	r := map[string]any{"text": t.Output.Text}
	if t.Output.Rows != nil {
		rows := t.Output.Rows.Rows
		if len(rows) > maxRowsToModel {
			r["truncated"] = fmt.Sprintf("first %d of %d rows", maxRowsToModel, len(rows))
			rows = rows[:maxRowsToModel]
		}
		r["columns"], r["rows"] = t.Output.Rows.Columns, rows
	}
	if t.Output.Data != nil {
		r["data"] = t.Output.Data
	}
	if t.Output.SQL != "" {
		r["sql"] = t.Output.SQL
	}
	b, _ := json.Marshal(r)
	return string(b)
}

func (o *OpenRouter) post(ctx context.Context, body any, out any) error {
	base := o.BaseURL
	if base == "" {
		base = OpenRouterBaseURL
	}
	buf, _ := json.Marshal(body)
	req, err := http.NewRequestWithContext(ctx, "POST", base+"/chat/completions", bytes.NewReader(buf))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+o.APIKey)
	res, err := o.Client.Do(req)
	if err != nil {
		return fmt.Errorf("model request failed: %w", err)
	}
	defer res.Body.Close()
	if res.StatusCode != http.StatusOK {
		var e struct {
			Error struct {
				Message string `json:"message"`
			} `json:"error"`
		}
		json.NewDecoder(res.Body).Decode(&e)
		return fmt.Errorf("model API returned %d: %s", res.StatusCode, e.Error.Message)
	}
	return json.NewDecoder(res.Body).Decode(out)
}
