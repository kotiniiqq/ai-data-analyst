package config

import (
	"strings"
	"testing"
)

func env(m map[string]string) func(string) string { return func(k string) string { return m[k] } }

func base() map[string]string {
	return map[string]string{
		"JWT_SECRET":    "0123456789abcdef0123456789abcdef",
		"DATABASE_URL":  "postgres://app@db/analyst",
		"WAREHOUSE_URL": "postgres://analyst_ro@db/analyst",
	}
}

func TestDefaults(t *testing.T) {
	c, err := Load(env(base()))
	if err != nil {
		t.Fatal(err)
	}
	if c.Planner != "demo" || c.Warehouse != "postgres" || c.ApprovalBytes != 100<<20 || c.Addr != ":8080" ||
		c.DataStart != "2024-01-01" || c.DataEnd != "2026-09-30" || c.OpenRouterModel != "anthropic/claude-sonnet-5" {
		t.Fatalf("%+v", c)
	}
}

func TestErrors(t *testing.T) {
	cases := map[string]map[string]string{
		"JWT_SECRET":         {"JWT_SECRET": ""},
		"at least 32":        {"JWT_SECRET": "short"},
		"DATABASE_URL":       {"DATABASE_URL": ""},
		"WAREHOUSE_URL":      {"WAREHOUSE_URL": ""},
		"OPENROUTER_API_KEY": {"PLANNER": "openrouter"},
		"PLANNER":            {"PLANNER": "magic"},
		"BIGQUERY_PROJECT":   {"WAREHOUSE": "bigquery"},
		"WAREHOUSE must":     {"WAREHOUSE": "snowflake"},
		"APPROVAL_BYTES":     {"APPROVAL_BYTES": "lots"},
		"DATA_END":           {"DATA_END": "yesterday"},
	}
	for want, override := range cases {
		m := base()
		for k, v := range override {
			m[k] = v
		}
		if _, err := Load(env(m)); err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("%v: got %v, want error mentioning %q", override, err, want)
		}
	}
}

func TestBigQueryDoesNotNeedAWarehouseURL(t *testing.T) {
	m := base()
	m["WAREHOUSE"], m["WAREHOUSE_URL"], m["BIGQUERY_PROJECT"] = "bigquery", "", "demo-proj"
	if _, err := Load(env(m)); err != nil {
		t.Fatal(err)
	}
}
