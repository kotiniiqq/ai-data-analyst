package auth

import (
	"io"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gofiber/fiber/v2"

	"github.com/kotiniiqq/ai-data-analyst/internal/rbac"
)

func app(t *testing.T) *fiber.App {
	t.Helper()
	a := fiber.New()
	a.Use(Middleware(secret, func() time.Time { return now }))
	a.Get("/me", func(c *fiber.Ctx) error {
		u, ok := CurrentUser(c)
		if !ok {
			return c.SendStatus(500)
		}
		return c.SendString(u.Role)
	})
	return a
}

func status(t *testing.T, a *fiber.App, header string) (int, string) {
	t.Helper()
	req := httptest.NewRequest("GET", "/me", nil)
	if header != "" {
		req.Header.Set("Authorization", header)
	}
	resp, err := a.Test(req)
	if err != nil {
		t.Fatal(err)
	}
	b, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, string(b)
}

func TestMiddlewareRejectsMissingAndBadTokens(t *testing.T) {
	a := app(t)
	for _, h := range []string{"", "Bearer", "Bearer nope", "Basic abc"} {
		if code, _ := status(t, a, h); code != 401 {
			t.Fatalf("header %q: got %d, want 401", h, code)
		}
	}
}

func TestMiddlewarePutsUserIntoContext(t *testing.T) {
	tok, _ := Issue(secret, rbac.NewUser(3, "owner@demo.local", "owner"), now)
	code, body := status(t, app(t), "Bearer "+tok)
	if code != 200 || body != "owner" {
		t.Fatalf("got %d %q", code, body)
	}
}
