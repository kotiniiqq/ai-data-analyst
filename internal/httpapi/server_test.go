package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gofiber/fiber/v2"

	"github.com/kotiniiqq/ai-data-analyst/internal/approval"
	"github.com/kotiniiqq/ai-data-analyst/internal/audit"
	"github.com/kotiniiqq/ai-data-analyst/internal/auth"
	"github.com/kotiniiqq/ai-data-analyst/internal/catalog"
	"github.com/kotiniiqq/ai-data-analyst/internal/cost"
	"github.com/kotiniiqq/ai-data-analyst/internal/planner"
	"github.com/kotiniiqq/ai-data-analyst/internal/rbac"
	"github.com/kotiniiqq/ai-data-analyst/internal/tools"
	"github.com/kotiniiqq/ai-data-analyst/internal/warehouse"
)

type fakeUsers map[string]struct {
	user rbac.User
	hash string
}

func (f fakeUsers) ByEmail(_ context.Context, email string) (rbac.User, string, error) {
	u, ok := f[strings.ToLower(email)]
	if !ok {
		return rbac.User{}, "", auth.ErrNoUser
	}
	return u.user, u.hash, nil
}

type fakeWH struct{ rows warehouse.Rows }

func (f fakeWH) Dialect() string                                    { return "postgres" }
func (f fakeWH) Query(context.Context, string) (warehouse.Rows, error) { return f.rows, nil }

type fakeEst struct{ bytes int64 }

func (f fakeEst) Estimate(context.Context, string) (cost.Estimate, error) {
	return cost.FromBytes(f.bytes, 100<<20), nil
}

var secret = []byte("test-secret-test-secret-test-secret")

type testServer struct {
	app    *fiber.App
	health error
}

func newServer(t *testing.T, bytes int64) *testServer {
	t.Helper()
	users := fakeUsers{}
	for i, role := range []string{"viewer", "analyst", "owner"} {
		h, _ := auth.HashPassword(role + "-demo")
		users[role+"@demo.local"] = struct {
			user rbac.User
			hash string
		}{rbac.NewUser(int64(i+1), role+"@demo.local", role), h}
	}
	reg := tools.NewRegistry(&audit.Memory{}, &tools.Builtins{
		Catalog: catalog.Default(),
		Warehouse: fakeWH{warehouse.Rows{Columns: []string{"month", "mrr"},
			Rows: [][]any{{"2026-08", 281694.59}, {"2026-09", 284830.27}}}},
		Estimator: fakeEst{bytes}, Approvals: approval.NewStore(), Now: time.Now,
		DataStart: "2024-01-01", DataEnd: "2026-09-30",
	})
	ts := &testServer{}
	ts.app = New(Deps{
		Secret: secret, Users: users, Registry: reg, Planner: planner.NewDemo("2026-09-30"),
		Catalog: catalog.Default(), Now: time.Now,
		Health: func(context.Context) error { return ts.health },
	})
	return ts
}

func (s *testServer) do(t *testing.T, method, path, token string, body any) (int, map[string]any) {
	t.Helper()
	var r io.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		r = bytes.NewReader(b)
	}
	req := httptest.NewRequest(method, path, r)
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := s.app.Test(req, 10000)
	if err != nil {
		t.Fatal(err)
	}
	var out map[string]any
	json.NewDecoder(resp.Body).Decode(&out)
	return resp.StatusCode, out
}

func (s *testServer) login(t *testing.T, role string) string {
	t.Helper()
	code, out := s.do(t, "POST", "/api/login", "", map[string]string{"email": role + "@demo.local", "password": role + "-demo"})
	if code != 200 {
		t.Fatalf("login %s: %d %v", role, code, out)
	}
	return out["token"].(string)
}

func errCode(out map[string]any) string {
	e, _ := out["error"].(map[string]any)
	c, _ := e["code"].(string)
	return c
}

func TestLogin(t *testing.T) {
	s := newServer(t, 1000)
	code, out := s.do(t, "POST", "/api/login", "", map[string]string{"email": "Analyst@demo.local", "password": "analyst-demo"})
	if code != 200 || out["token"] == "" || out["user"].(map[string]any)["role"] != "analyst" {
		t.Fatalf("%d %v", code, out)
	}
	for _, body := range []map[string]string{
		{"email": "analyst@demo.local", "password": "wrong"},
		{"email": "nobody@demo.local", "password": "analyst-demo"},
		{},
	} {
		code, out := s.do(t, "POST", "/api/login", "", body)
		if code != 401 || errCode(out) != "bad_credentials" {
			t.Fatalf("%v: %d %v", body, code, out)
		}
	}
}

func TestMeNeedsAToken(t *testing.T) {
	s := newServer(t, 1000)
	if code, _ := s.do(t, "GET", "/api/me", "", nil); code != 401 {
		t.Fatalf("got %d", code)
	}
	code, out := s.do(t, "GET", "/api/me", s.login(t, "owner"), nil)
	if code != 200 || out["role"] != "owner" {
		t.Fatalf("%d %v", code, out)
	}
}

func TestViewerGetsARefusalForMargin(t *testing.T) {
	s := newServer(t, 1000)
	code, out := s.do(t, "POST", "/api/chat", s.login(t, "viewer"), map[string]string{"message": "What is our gross margin?"})
	if code != 200 || !strings.HasPrefix(out["answer"].(string), "I can't share that") {
		t.Fatalf("%d %v", code, out)
	}
	tr := out["trace"].([]any)[0].(map[string]any)
	if tr["allowed"] != false || tr["code"] != "forbidden" {
		t.Fatalf("trace %v", tr)
	}
}

func TestSQLIsHiddenFromViewersAndShownToAnalysts(t *testing.T) {
	s := newServer(t, 1000)
	_, out := s.do(t, "POST", "/api/chat", s.login(t, "viewer"), map[string]string{"message": "MRR last 2 months"})
	if _, has := out["sql"]; has || out["table"] == nil || out["chart"] == nil {
		t.Fatalf("viewer response %v", out)
	}
	_, out = s.do(t, "POST", "/api/chat", s.login(t, "analyst"), map[string]string{"message": "MRR last 2 months"})
	if sql, _ := out["sql"].(string); !strings.HasPrefix(sql, "SELECT") {
		t.Fatalf("analyst response %v", out)
	}
}

func TestChatInputLimits(t *testing.T) {
	s := newServer(t, 1000)
	tok := s.login(t, "viewer")
	for _, msg := range []string{"", "   ", strings.Repeat("a", 2001)} {
		if code, out := s.do(t, "POST", "/api/chat", tok, map[string]string{"message": msg}); code != 400 || errCode(out) != "bad_request" {
			t.Fatalf("len %d: %d %v", len(msg), code, out)
		}
	}
}

func TestApprovalFlow(t *testing.T) {
	s := newServer(t, 500<<20)
	analyst, viewer := s.login(t, "analyst"), s.login(t, "viewer")
	code, out := s.do(t, "POST", "/api/chat", analyst, map[string]string{"message": "select date from gold.revenue_daily limit 10"})
	ap, ok := out["approval"].(map[string]any)
	if code != 200 || !ok || ap["token"] == "" {
		t.Fatalf("%d %v", code, out)
	}
	tok := ap["token"].(string)
	if code, out := s.do(t, "POST", "/api/approvals/"+tok, viewer, nil); code != 403 || errCode(out) != "forbidden" {
		t.Fatalf("viewer: %d %v", code, out)
	}
	code, out = s.do(t, "POST", "/api/approvals/"+tok, analyst, nil)
	if code != 200 || out["table"] == nil || !strings.Contains(out["answer"].(string), "2 rows") {
		t.Fatalf("analyst: %d %v", code, out)
	}
	if code, out := s.do(t, "POST", "/api/approvals/"+tok, analyst, nil); code != 404 || errCode(out) != "not_found" {
		t.Fatalf("reuse: %d %v", code, out)
	}
}

func TestCatalogFollowsTheRole(t *testing.T) {
	s := newServer(t, 1000)
	_, out := s.do(t, "GET", "/api/catalog", s.login(t, "viewer"), nil)
	if out["tables"] != nil || strings.Contains(toJSON(out["metrics"]), "gross_margin_pct") {
		t.Fatalf("viewer catalog %v", out)
	}
	_, out = s.do(t, "GET", "/api/catalog", s.login(t, "owner"), nil)
	if len(out["tables"].([]any)) != 5 || !strings.Contains(toJSON(out["metrics"]), "gross_margin_pct") {
		t.Fatalf("owner catalog %v", out)
	}
}

func TestHealth(t *testing.T) {
	s := newServer(t, 1000)
	if code, _ := s.do(t, "GET", "/health", "", nil); code != 200 {
		t.Fatalf("healthy: %d", code)
	}
	s.health = errors.New("db down")
	if code, _ := s.do(t, "GET", "/health", "", nil); code != 503 {
		t.Fatalf("unhealthy: %d", code)
	}
}

func toJSON(v any) string {
	b, _ := json.Marshal(v)
	return string(b)
}
