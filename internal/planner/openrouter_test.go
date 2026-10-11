package planner

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/kotiniiqq/ai-data-analyst/internal/agent"
	"github.com/kotiniiqq/ai-data-analyst/internal/tools"
	"github.com/kotiniiqq/ai-data-analyst/internal/warehouse"
)

type request struct {
	Model    string `json:"model"`
	Messages []struct {
		Role       string `json:"role"`
		Content    string `json:"content"`
		ToolCallID string `json:"tool_call_id"`
		ToolCalls  []struct {
			ID       string `json:"id"`
			Function struct {
				Name      string `json:"name"`
				Arguments string `json:"arguments"`
			} `json:"function"`
		} `json:"tool_calls"`
	} `json:"messages"`
	Tools []struct {
		Type     string `json:"type"`
		Function struct {
			Name       string         `json:"name"`
			Parameters map[string]any `json:"parameters"`
		} `json:"function"`
	} `json:"tools"`
}

func server(t *testing.T, reply string, status int, got *request, auth *string) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/chat/completions" {
			t.Errorf("path %s", r.URL.Path)
		}
		if auth != nil {
			*auth = r.Header.Get("Authorization")
		}
		b, _ := io.ReadAll(r.Body)
		if got != nil {
			json.Unmarshal(b, got)
		}
		w.WriteHeader(status)
		w.Write([]byte(reply))
	}))
	t.Cleanup(srv.Close)
	return srv
}

var someTools = []tools.Tool{{Name: "metric_series", Description: "series",
	Params: []tools.Param{{Name: "metric", Type: "string", Required: true}}}}

func TestOpenRouterToolCallRoundTrip(t *testing.T) {
	var req request
	var auth string
	srv := server(t, `{"choices":[{"message":{"role":"assistant","content":null,
		"tool_calls":[{"id":"call_a","type":"function","function":{"name":"metric_series","arguments":"{\"metric\":\"mrr\"}"}}]}}]}`, 200, &req, &auth)
	p := &OpenRouter{BaseURL: srv.URL, APIKey: "k-test", Model: "some/model", DataEnd: "2026-09-30", Client: srv.Client()}
	step, err := p.Next(context.Background(), agent.State{User: viewer, Question: "MRR?", Tools: someTools})
	if err != nil {
		t.Fatal(err)
	}
	if auth != "Bearer k-test" || req.Model != "some/model" {
		t.Fatalf("auth %q model %q", auth, req.Model)
	}
	if len(req.Tools) != 1 || req.Tools[0].Type != "function" || req.Tools[0].Function.Name != "metric_series" ||
		req.Tools[0].Function.Parameters["type"] != "object" {
		t.Fatalf("tools %+v", req.Tools)
	}
	if len(req.Messages) != 2 || req.Messages[0].Role != "system" || !strings.Contains(req.Messages[0].Content, "2026-09-30") ||
		req.Messages[1].Role != "user" || req.Messages[1].Content != "MRR?" {
		t.Fatalf("messages %+v", req.Messages)
	}
	if len(step.ToolCalls) != 1 || step.ToolCalls[0].ID != "call_a" || step.ToolCalls[0].Args["metric"] != "mrr" {
		t.Fatalf("step %+v", step)
	}
}

func TestOpenRouterSendsToolResultsBack(t *testing.T) {
	var req request
	srv := server(t, `{"choices":[{"message":{"role":"assistant","content":"MRR was $284,830.27."}}]}`, 200, &req, nil)
	p := &OpenRouter{BaseURL: srv.URL, APIKey: "k", Model: "m", DataEnd: "2026-09-30", Client: srv.Client()}
	st := agent.State{User: viewer, Question: "MRR?", Tools: someTools, History: []agent.Turn{
		{Call: agent.ToolCall{ID: "call_a", Name: "metric_series", Args: map[string]any{"metric": "mrr"}},
			Output: tools.Output{Text: "mrr last 284830.27", Rows: &warehouse.Rows{Columns: []string{"month", "mrr"}, Rows: [][]any{{"2026-09", 284830.27}}}}},
		{Call: agent.ToolCall{ID: "call_b", Name: "run_sql", Args: map[string]any{"sql": "x"}},
			Err: &tools.ToolError{Code: "forbidden", Msg: "your role (viewer) may not use run_sql"}},
	}}
	step, err := p.Next(context.Background(), st)
	if err != nil {
		t.Fatal(err)
	}
	if step.Final != "MRR was $284,830.27." || len(step.ToolCalls) != 0 {
		t.Fatalf("step %+v", step)
	}
	m := req.Messages
	if len(m) != 6 || m[2].Role != "assistant" || m[2].ToolCalls[0].ID != "call_a" || m[3].Role != "tool" || m[3].ToolCallID != "call_a" ||
		!strings.Contains(m[3].Content, "284830.27") || m[5].ToolCallID != "call_b" || !strings.Contains(m[5].Content, "forbidden") {
		t.Fatalf("messages %+v", m)
	}
	if m[2].ToolCalls[0].Function.Arguments != `{"metric":"mrr"}` {
		t.Fatalf("arguments %q", m[2].ToolCalls[0].Function.Arguments)
	}
}

func TestOpenRouterMalformedArgumentsBecomeABadInputCall(t *testing.T) {
	srv := server(t, `{"choices":[{"message":{"role":"assistant",
		"tool_calls":[{"id":"c1","type":"function","function":{"name":"metric_series","arguments":"{metric: mrr"}}]}}]}`, 200, nil, nil)
	p := &OpenRouter{BaseURL: srv.URL, APIKey: "k", Model: "m", DataEnd: "2026-09-30", Client: srv.Client()}
	step, err := p.Next(context.Background(), agent.State{User: viewer, Question: "q", Tools: someTools})
	if err != nil {
		t.Fatal(err)
	}
	if len(step.ToolCalls) != 1 || step.ToolCalls[0].Args[MalformedArgsKey] != "{metric: mrr" {
		t.Fatalf("step %+v", step)
	}
}

func TestOpenRouterHTTPErrorIsAnError(t *testing.T) {
	srv := server(t, `{"error":{"message":"No auth credentials found"}}`, 401, nil, nil)
	p := &OpenRouter{BaseURL: srv.URL, APIKey: "bad", Model: "m", DataEnd: "2026-09-30", Client: srv.Client()}
	_, err := p.Next(context.Background(), agent.State{User: viewer, Question: "q"})
	if err == nil || !strings.Contains(err.Error(), "401") || !strings.Contains(err.Error(), "No auth credentials") {
		t.Fatalf("got %v", err)
	}
}

func TestOpenRouterEmptyChoiceIsAnError(t *testing.T) {
	srv := server(t, `{"choices":[]}`, 200, nil, nil)
	p := &OpenRouter{BaseURL: srv.URL, APIKey: "k", Model: "m", DataEnd: "2026-09-30", Client: srv.Client()}
	if _, err := p.Next(context.Background(), agent.State{User: viewer, Question: "q"}); err == nil {
		t.Fatal("empty response accepted")
	}
}
