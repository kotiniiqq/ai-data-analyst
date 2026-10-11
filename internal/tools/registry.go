package tools

import (
	"context"
	"errors"
	"slices"
	"sort"
	"time"

	"github.com/kotiniiqq/ai-data-analyst/internal/approval"
	"github.com/kotiniiqq/ai-data-analyst/internal/audit"
	"github.com/kotiniiqq/ai-data-analyst/internal/rbac"
)

type Registry struct {
	tools    []Tool
	log      audit.Logger
	builtins *Builtins
}

func NewRegistry(log audit.Logger, b *Builtins) *Registry {
	return &Registry{tools: b.Tools(), log: log, builtins: b}
}

// Schemas lists the tools u is allowed to call.
func (r *Registry) Schemas(u rbac.User) []Tool {
	var out []Tool
	for _, t := range r.tools {
		if permitted(u, t.RequiredPerms) {
			out = append(out, t)
		}
	}
	return out
}

func permitted(u rbac.User, perms []rbac.Perm) bool {
	for _, p := range perms {
		if !u.Can(p) {
			return false
		}
	}
	return true
}

// Execute checks permissions and input, runs the tool and audits the call.
func (r *Registry) Execute(ctx context.Context, u rbac.User, name string, args map[string]any) (Output, error) {
	start := time.Now()
	keys := make([]string, 0, len(args))
	for k := range args {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	i := slices.IndexFunc(r.tools, func(t Tool) bool { return t.Name == name })
	if i < 0 {
		err := toolErr("unknown_tool", "there is no tool called %q", name)
		r.audit(ctx, u, name, false, err, keys, start)
		return Output{}, err
	}
	t := r.tools[i]
	if !permitted(u, t.RequiredPerms) {
		err := toolErr("forbidden", "your role (%s) may not use %s", u.Role, name)
		r.audit(ctx, u, name, false, err, keys, start)
		return Output{}, err
	}
	strArgs, err := validate(t, args)
	if err != nil {
		r.audit(ctx, u, name, true, err, keys, start)
		return Output{}, err
	}
	out, err := t.Execute(ctx, Call{User: u, Args: strArgs})
	if err == nil && out.Numbers == nil {
		out.Numbers = numbersIn(out)
	}
	var te *ToolError
	denied := errors.As(err, &te) && te.Code == "forbidden" // e.g. a sensitive metric
	r.audit(ctx, u, name, !denied, err, keys, start)
	return out, err
}

// RunApproved runs a query the same user confirmed after an approval prompt.
// Only the HTTP layer calls this; no planner can approve a query.
func (r *Registry) RunApproved(ctx context.Context, u rbac.User, token string) (Output, error) {
	start := time.Now()
	p, err := r.builtins.Approvals.Take(u.ID, token, r.builtins.Now())
	switch {
	case errors.Is(err, approval.ErrWrongUser):
		err = toolErr("forbidden", "this approval belongs to another user")
	case errors.Is(err, approval.ErrExpired):
		err = toolErr("expired", "the approval expired, ask again")
	case err != nil:
		err = toolErr("not_found", "unknown or already used approval")
	}
	if err != nil {
		r.audit(ctx, u, "run_sql_approved", false, err, []string{"token"}, start)
		return Output{}, err
	}
	out, err := r.builtins.runChecked(ctx, u, p.SQL, true)
	if err == nil {
		out.Numbers = numbersIn(out)
	}
	r.audit(ctx, u, "run_sql_approved", true, err, []string{"token"}, start)
	return out, err
}

func (r *Registry) audit(ctx context.Context, u rbac.User, tool string, allowed bool, err error, keys []string, start time.Time) {
	code := ""
	if err != nil {
		var te *ToolError
		if errors.As(err, &te) {
			code = te.Code
		} else {
			code = "error"
		}
	}
	r.log.Log(ctx, audit.Entry{UserID: u.ID, Tool: tool, Allowed: allowed, Code: code,
		InputKeys: keys, DurationMS: time.Since(start).Milliseconds()})
}

// MalformedArgsKey carries tool arguments a model sent as invalid JSON.
const MalformedArgsKey = "_malformed_arguments"

// validate accepts only declared parameters, all of them strings.
func validate(t Tool, args map[string]any) (map[string]string, error) {
	if _, ok := args[MalformedArgsKey]; ok {
		return nil, toolErr("bad_input", "the arguments for %s were not valid JSON", t.Name)
	}
	out := map[string]string{}
	for k, v := range args {
		if !slices.ContainsFunc(t.Params, func(p Param) bool { return p.Name == k }) {
			return nil, toolErr("bad_input", "%s has no parameter %q", t.Name, k)
		}
		s, ok := v.(string)
		if !ok {
			return nil, toolErr("bad_input", "parameter %q must be a string", k)
		}
		out[k] = s
	}
	for _, p := range t.Params {
		if p.Required && out[p.Name] == "" {
			return nil, toolErr("bad_input", "parameter %q is required", p.Name)
		}
	}
	return out, nil
}

func numbersIn(o Output) []float64 {
	var nums []float64
	if o.Rows != nil {
		nums = append(nums, float64(len(o.Rows.Rows)))
		for _, row := range o.Rows.Rows {
			for _, v := range row {
				switch x := v.(type) {
				case float64:
					nums = append(nums, x)
				case int64:
					nums = append(nums, float64(x))
				}
			}
		}
	}
	if s, ok := o.Data.(SeriesSummary); ok {
		nums = append(nums, s.numbers()...)
	}
	return nums
}
