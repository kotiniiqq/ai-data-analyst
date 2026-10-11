package auth

import (
	"strings"
	"time"

	"github.com/gofiber/fiber/v2"

	"github.com/kotiniiqq/ai-data-analyst/internal/rbac"
)

const userKey = "user"

// Middleware requires a valid "Authorization: Bearer <token>" header and
// stores the user in the request context.
func Middleware(secret []byte, now func() time.Time) fiber.Handler {
	return func(c *fiber.Ctx) error {
		h := c.Get(fiber.HeaderAuthorization)
		tok, ok := strings.CutPrefix(h, "Bearer ")
		if !ok || tok == "" {
			return unauthorized(c)
		}
		u, err := Parse(secret, tok, now())
		if err != nil {
			return unauthorized(c)
		}
		c.Locals(userKey, u)
		return c.Next()
	}
}

func CurrentUser(c *fiber.Ctx) (rbac.User, bool) {
	u, ok := c.Locals(userKey).(rbac.User)
	return u, ok
}

func unauthorized(c *fiber.Ctx) error {
	return c.Status(fiber.StatusUnauthorized).JSON(fiber.Map{
		"error": fiber.Map{"code": "unauthorized", "msg": "log in first"},
	})
}
