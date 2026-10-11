// Package httpapi exposes the analyst over HTTP: login, chat, approvals and
// the catalog. Every route except login and health needs a session token.
package httpapi

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/gofiber/fiber/v2"

	"github.com/kotiniiqq/ai-data-analyst/internal/agent"
	"github.com/kotiniiqq/ai-data-analyst/internal/auth"
	"github.com/kotiniiqq/ai-data-analyst/internal/catalog"
	"github.com/kotiniiqq/ai-data-analyst/internal/rbac"
	"github.com/kotiniiqq/ai-data-analyst/internal/tools"
)

// MaxMessage is the longest question accepted, in characters.
const MaxMessage = 2000

type Deps struct {
	Secret   []byte
	Users    auth.Store
	Registry *tools.Registry
	Planner  agent.Planner
	Catalog  *catalog.Catalog
	Now      func() time.Time
	Health   func(ctx context.Context) error
	// ChatTimeout bounds one question, planner and tools included.
	ChatTimeout time.Duration
}

func New(d Deps) *fiber.App {
	if d.ChatTimeout == 0 {
		d.ChatTimeout = 90 * time.Second
	}
	app := fiber.New(fiber.Config{
		BodyLimit:             64 << 10,
		DisableStartupMessage: true,
		ErrorHandler: func(c *fiber.Ctx, err error) error {
			var fe *fiber.Error
			if errors.As(err, &fe) {
				return fail(c, fe.Code, "http_error", fe.Message)
			}
			slog.Error("request failed", "path", c.Path(), "err", err)
			return fail(c, fiber.StatusInternalServerError, "internal", "something went wrong")
		},
	})
	h := &handlers{d: d, dummyHash: mustHash("not-a-real-password")}

	app.Get("/health", h.health)
	app.Post("/api/login", h.login)
	api := app.Group("/api", auth.Middleware(d.Secret, d.Now))
	api.Get("/me", h.me)
	api.Get("/catalog", h.catalog)
	api.Post("/chat", h.chat)
	api.Post("/approvals/:token", h.approve)
	return app
}

type handlers struct {
	d         Deps
	dummyHash string
}

func mustHash(pw string) string {
	h, err := auth.HashPassword(pw)
	if err != nil {
		panic(err)
	}
	return h
}

func fail(c *fiber.Ctx, status int, code, msg string) error {
	return c.Status(status).JSON(fiber.Map{"error": fiber.Map{"code": code, "msg": msg}})
}

func (h *handlers) health(c *fiber.Ctx) error {
	if err := h.d.Health(c.UserContext()); err != nil {
		return fail(c, fiber.StatusServiceUnavailable, "unhealthy", "database not reachable")
	}
	return c.JSON(fiber.Map{"status": "ok"})
}

func (h *handlers) login(c *fiber.Ctx) error {
	var in struct {
		Email    string `json:"email"`
		Password string `json:"password"`
	}
	if err := c.BodyParser(&in); err != nil {
		return fail(c, fiber.StatusBadRequest, "bad_request", "send JSON with email and password")
	}
	u, hash, err := h.d.Users.ByEmail(c.UserContext(), in.Email)
	if err != nil && !errors.Is(err, auth.ErrNoUser) {
		return err
	}
	if errors.Is(err, auth.ErrNoUser) {
		hash = h.dummyHash // same work whether or not the user exists
	}
	if !auth.CheckPassword(hash, in.Password) || err != nil {
		return fail(c, fiber.StatusUnauthorized, "bad_credentials", "wrong email or password")
	}
	tok, err := auth.Issue(h.d.Secret, u, h.d.Now())
	if err != nil {
		return err
	}
	return c.JSON(fiber.Map{"token": tok, "user": u})
}

func (h *handlers) me(c *fiber.Ctx) error {
	u, _ := auth.CurrentUser(c)
	return c.JSON(u)
}

func (h *handlers) catalog(c *fiber.Ctx) error {
	u, _ := auth.CurrentUser(c)
	out := fiber.Map{"metrics": h.d.Catalog.Metrics(u)}
	if u.Can(rbac.SQLView) {
		out["tables"] = h.d.Catalog.Tables(u)
	}
	return c.JSON(out)
}

func (h *handlers) chat(c *fiber.Ctx) error {
	u, _ := auth.CurrentUser(c)
	if !u.Can(rbac.AIChat) {
		return fail(c, fiber.StatusForbidden, "forbidden", "your role may not use the assistant")
	}
	var in struct {
		Message string `json:"message"`
	}
	if err := c.BodyParser(&in); err != nil {
		return fail(c, fiber.StatusBadRequest, "bad_request", "send JSON with a message")
	}
	msg := strings.TrimSpace(in.Message)
	if msg == "" || len([]rune(msg)) > MaxMessage {
		return fail(c, fiber.StatusBadRequest, "bad_request", fmt.Sprintf("the message must be 1 to %d characters", MaxMessage))
	}
	ctx, cancel := context.WithTimeout(c.UserContext(), h.d.ChatTimeout)
	defer cancel()
	a := agent.Run(ctx, h.d.Planner, h.d.Registry, u, msg)
	return c.JSON(redact(a, u))
}

func (h *handlers) approve(c *fiber.Ctx) error {
	u, _ := auth.CurrentUser(c)
	out, err := h.d.Registry.RunApproved(c.UserContext(), u, c.Params("token"))
	if err != nil {
		var te *tools.ToolError
		if !errors.As(err, &te) {
			return err
		}
		status := map[string]int{"forbidden": 403, "not_found": 404, "expired": 410}[te.Code]
		if status == 0 {
			status = fiber.StatusBadGateway
		}
		return fail(c, status, te.Code, te.Msg)
	}
	n := 0
	if out.Rows != nil {
		n = len(out.Rows.Rows)
	}
	a := agent.Answer{
		Text:     fmt.Sprintf("The approved query returned %d rows.", n),
		Trace:    []agent.TraceItem{{Tool: "run_sql_approved", Allowed: true, Summary: out.Text}},
		SQL:      out.SQL,
		Table:    out.Rows,
		Warnings: []string{},
	}
	if out.Rows != nil {
		a.Chart = agent.ChartOf(out.Rows)
	}
	return c.JSON(redact(a, u))
}

// redact removes SQL from the answer for users without sql.view.
func redact(a agent.Answer, u rbac.User) agent.Answer {
	if u.Can(rbac.SQLView) {
		return a
	}
	a.SQL = ""
	if a.Approval != nil {
		ap := *a.Approval
		ap.SQL = ""
		a.Approval = &ap
	}
	for i, t := range a.Trace {
		if _, ok := t.Args["sql"]; ok {
			args := map[string]any{}
			for k, v := range t.Args {
				args[k] = v
			}
			args["sql"] = "(hidden)"
			a.Trace[i].Args = args
		}
	}
	return a
}
