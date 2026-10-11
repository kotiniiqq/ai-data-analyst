// Package tools is the only way the analyst touches data. Each tool declares
// the permissions it needs; the registry checks them, validates the input
// and writes an audit entry for every call, allowed or not.
package tools

import (
	"context"
	"fmt"

	"github.com/kotiniiqq/ai-data-analyst/internal/cost"
	"github.com/kotiniiqq/ai-data-analyst/internal/rbac"
	"github.com/kotiniiqq/ai-data-analyst/internal/warehouse"
)

type Param struct {
	Name        string
	Type        string // "string" only for now
	Description string
	Required    bool
}

type Tool struct {
	Name          string
	Description   string
	Params        []Param
	RequiredPerms []rbac.Perm
	Execute       func(ctx context.Context, c Call) (Output, error)
}

// InputSchema is the JSON schema of the tool's arguments, as sent to a model.
func (t Tool) InputSchema() map[string]any {
	props := map[string]any{}
	required := []string{}
	for _, p := range t.Params {
		props[p.Name] = map[string]any{"type": p.Type, "description": p.Description}
		if p.Required {
			required = append(required, p.Name)
		}
	}
	return map[string]any{"type": "object", "properties": props, "required": required, "additionalProperties": false}
}

type Call struct {
	User rbac.User
	Args map[string]string
}

type Approval struct {
	Token    string        `json:"token"`
	SQL      string        `json:"sql"`
	Estimate cost.Estimate `json:"estimate"`
}

// Output is what a tool returns. Text is the short summary a planner reads;
// Numbers are every number the output contains, for the number guard.
type Output struct {
	Text     string          `json:"text"`
	Data     any             `json:"data,omitempty"`
	Rows     *warehouse.Rows `json:"rows,omitempty"`
	SQL      string          `json:"sql,omitempty"`
	Approval *Approval       `json:"approval,omitempty"`
	Numbers  []float64       `json:"-"`
}

// ToolError is an expected failure, returned to the planner and the user.
type ToolError struct {
	Code string `json:"code"` // forbidden, bad_input, unknown_tool, unknown_metric, sql_rejected, warehouse, not_found, expired
	Msg  string `json:"msg"`
}

func (e *ToolError) Error() string { return e.Code + ": " + e.Msg }

func toolErr(code, format string, args ...any) *ToolError {
	return &ToolError{Code: code, Msg: fmt.Sprintf(format, args...)}
}
