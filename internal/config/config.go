// Package config reads the API settings from the environment.
package config

import (
	"errors"
	"fmt"
	"strconv"
	"time"
)

type Config struct {
	Addr            string
	JWTSecret       string
	DatabaseURL     string // app role: users and audit log
	Warehouse       string // postgres or bigquery
	WarehouseURL    string // read-only role, for Warehouse=postgres
	BigQueryProject string
	Planner         string // demo or openrouter
	OpenRouterKey   string
	OpenRouterModel string
	ApprovalBytes   int64
	DataStart       string
	DataEnd         string
}

func Load(getenv func(string) string) (Config, error) {
	get := func(k, def string) string {
		if v := getenv(k); v != "" {
			return v
		}
		return def
	}
	c := Config{
		Addr:            get("ADDR", ":8080"),
		JWTSecret:       getenv("JWT_SECRET"),
		DatabaseURL:     getenv("DATABASE_URL"),
		Warehouse:       get("WAREHOUSE", "postgres"),
		WarehouseURL:    getenv("WAREHOUSE_URL"),
		BigQueryProject: getenv("BIGQUERY_PROJECT"),
		Planner:         get("PLANNER", "demo"),
		OpenRouterKey:   getenv("OPENROUTER_API_KEY"),
		OpenRouterModel: get("OPENROUTER_MODEL", "anthropic/claude-sonnet-5"),
		DataStart:       get("DATA_START", "2024-01-01"),
		DataEnd:         get("DATA_END", "2026-09-30"),
	}
	var errs []error
	if c.JWTSecret == "" {
		errs = append(errs, errors.New("JWT_SECRET is required"))
	} else if len(c.JWTSecret) < 32 {
		errs = append(errs, errors.New("JWT_SECRET must be at least 32 characters"))
	}
	if c.DatabaseURL == "" {
		errs = append(errs, errors.New("DATABASE_URL is required"))
	}
	switch c.Warehouse {
	case "postgres":
		if c.WarehouseURL == "" {
			errs = append(errs, errors.New("WAREHOUSE_URL is required for WAREHOUSE=postgres"))
		}
	case "bigquery":
		if c.BigQueryProject == "" {
			errs = append(errs, errors.New("BIGQUERY_PROJECT is required for WAREHOUSE=bigquery"))
		}
	default:
		errs = append(errs, fmt.Errorf("WAREHOUSE must be postgres or bigquery, got %q", c.Warehouse))
	}
	switch c.Planner {
	case "demo":
	case "openrouter":
		if c.OpenRouterKey == "" {
			errs = append(errs, errors.New("OPENROUTER_API_KEY is required for PLANNER=openrouter"))
		}
	default:
		errs = append(errs, fmt.Errorf("PLANNER must be demo or openrouter, got %q", c.Planner))
	}
	n, err := strconv.ParseInt(get("APPROVAL_BYTES", "104857600"), 10, 64)
	if err != nil || n <= 0 {
		errs = append(errs, errors.New("APPROVAL_BYTES must be a positive number of bytes"))
	}
	c.ApprovalBytes = n
	for k, v := range map[string]string{"DATA_START": c.DataStart, "DATA_END": c.DataEnd} {
		if _, err := time.Parse(time.DateOnly, v); err != nil {
			errs = append(errs, fmt.Errorf("%s must be YYYY-MM-DD, got %q", k, v))
		}
	}
	return c, errors.Join(errs...)
}
