// Command api serves the AI analyst.
package main

import (
	"context"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"golang.org/x/oauth2/google"

	"github.com/kotiniiqq/ai-data-analyst/internal/agent"
	"github.com/kotiniiqq/ai-data-analyst/internal/approval"
	"github.com/kotiniiqq/ai-data-analyst/internal/audit"
	"github.com/kotiniiqq/ai-data-analyst/internal/auth"
	"github.com/kotiniiqq/ai-data-analyst/internal/catalog"
	"github.com/kotiniiqq/ai-data-analyst/internal/config"
	"github.com/kotiniiqq/ai-data-analyst/internal/cost"
	"github.com/kotiniiqq/ai-data-analyst/internal/httpapi"
	"github.com/kotiniiqq/ai-data-analyst/internal/planner"
	"github.com/kotiniiqq/ai-data-analyst/internal/tools"
	"github.com/kotiniiqq/ai-data-analyst/internal/warehouse"
)

func main() {
	if err := run(); err != nil {
		slog.Error("api stopped", "err", err)
		os.Exit(1)
	}
}

func run() error {
	cfg, err := config.Load(os.Getenv)
	if err != nil {
		return err
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	appDB, err := pgxpool.New(ctx, cfg.DatabaseURL)
	if err != nil {
		return err
	}
	defer appDB.Close()

	var wh warehouse.Provider
	var est cost.Estimator
	switch cfg.Warehouse {
	case "bigquery":
		client, err := google.DefaultClient(ctx, "https://www.googleapis.com/auth/bigquery")
		if err != nil {
			return err
		}
		wh = &warehouse.BigQuery{Project: cfg.BigQueryProject, Client: client}
		est = &cost.BigQuery{Project: cfg.BigQueryProject, Client: client, ApprovalBytes: cfg.ApprovalBytes}
	default:
		pg, err := warehouse.NewPostgres(ctx, cfg.WarehouseURL, 15*time.Second)
		if err != nil {
			return err
		}
		defer pg.Close()
		wh = pg
		est = &cost.Postgres{Pool: pg.Pool(), ApprovalBytes: cfg.ApprovalBytes}
	}

	var p agent.Planner = planner.NewDemo(cfg.DataEnd)
	if cfg.Planner == "openrouter" {
		p = &planner.OpenRouter{APIKey: cfg.OpenRouterKey, Model: cfg.OpenRouterModel, DataEnd: cfg.DataEnd,
			Client: &http.Client{Timeout: 60 * time.Second}}
	}

	cat := catalog.Default()
	reg := tools.NewRegistry(audit.NewPG(appDB), &tools.Builtins{
		Catalog: cat, Warehouse: wh, Estimator: est, Approvals: approval.NewStore(), Now: time.Now,
		DataStart: cfg.DataStart, DataEnd: cfg.DataEnd,
	})
	app := httpapi.New(httpapi.Deps{
		Secret: []byte(cfg.JWTSecret), Users: auth.NewPGStore(appDB), Registry: reg, Planner: p,
		Catalog: cat, Now: time.Now, Health: appDB.Ping,
	})

	go func() {
		<-ctx.Done()
		app.ShutdownWithTimeout(10 * time.Second)
	}()
	slog.Info("api listening", "addr", cfg.Addr, "planner", cfg.Planner, "warehouse", cfg.Warehouse)
	return app.Listen(cfg.Addr)
}
