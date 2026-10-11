// Package auth issues and checks the session token and the demo users' passwords.
package auth

import (
	"errors"
	"strconv"
	"time"

	"github.com/golang-jwt/jwt/v5"

	"github.com/kotiniiqq/ai-data-analyst/internal/rbac"
)

// TTL is how long a session token is valid.
const TTL = 8 * time.Hour

var ErrInvalidToken = errors.New("invalid or expired token")

type claims struct {
	Email string `json:"email"`
	Role  string `json:"role"`
	jwt.RegisteredClaims
}

// Issue signs a token for u. Only identity and role go into it; permissions
// are always derived from the role on the server.
func Issue(secret []byte, u rbac.User, now time.Time) (string, error) {
	if len(secret) == 0 {
		return "", errors.New("empty JWT secret")
	}
	c := claims{
		Email: u.Email,
		Role:  u.Role,
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:   strconv.FormatInt(u.ID, 10),
			IssuedAt:  jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(now.Add(TTL)),
		},
	}
	return jwt.NewWithClaims(jwt.SigningMethodHS256, c).SignedString(secret)
}

// Parse verifies the signature (HS256 only) and expiry at now.
func Parse(secret []byte, token string, now time.Time) (rbac.User, error) {
	if len(secret) == 0 {
		return rbac.User{}, ErrInvalidToken
	}
	var c claims
	_, err := jwt.ParseWithClaims(token, &c, func(*jwt.Token) (any, error) { return secret, nil },
		jwt.WithValidMethods([]string{jwt.SigningMethodHS256.Alg()}),
		jwt.WithTimeFunc(func() time.Time { return now }),
		jwt.WithExpirationRequired(),
	)
	if err != nil {
		return rbac.User{}, ErrInvalidToken
	}
	id, err := strconv.ParseInt(c.Subject, 10, 64)
	if err != nil {
		return rbac.User{}, ErrInvalidToken
	}
	return rbac.NewUser(id, c.Email, c.Role), nil
}
