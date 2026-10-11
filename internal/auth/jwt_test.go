package auth

import (
	"encoding/base64"
	"strings"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"

	"github.com/kotiniiqq/ai-data-analyst/internal/rbac"
)

var secret = []byte("test-secret-32-bytes-long-enough!")
var now = time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)

func TestIssueAndParseRoundTrip(t *testing.T) {
	tok, err := Issue(secret, rbac.NewUser(7, "analyst@demo.local", "analyst"), now)
	if err != nil {
		t.Fatal(err)
	}
	u, err := Parse(secret, tok, now.Add(time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if u.ID != 7 || u.Email != "analyst@demo.local" || u.Role != "analyst" || !u.Can(rbac.SQLRun) {
		t.Fatalf("unexpected user %+v", u)
	}
}

func TestExpiredTokenRejected(t *testing.T) {
	tok, _ := Issue(secret, rbac.NewUser(1, "v", "viewer"), now)
	if _, err := Parse(secret, tok, now.Add(8*time.Hour+time.Second)); err == nil {
		t.Fatal("token older than 8 h accepted")
	}
}

func TestWrongSecretRejected(t *testing.T) {
	tok, _ := Issue(secret, rbac.NewUser(1, "v", "viewer"), now)
	if _, err := Parse([]byte("another-secret-another-secret!!!"), tok, now); err == nil {
		t.Fatal("token signed with another secret accepted")
	}
}

func TestAlgNoneRejected(t *testing.T) {
	claims := jwt.MapClaims{"sub": "1", "email": "o", "role": "owner", "exp": now.Add(time.Hour).Unix()}
	tok, _ := jwt.NewWithClaims(jwt.SigningMethodNone, claims).SignedString(jwt.UnsafeAllowNoneSignatureType)
	if _, err := Parse(secret, tok, now); err == nil {
		t.Fatal("alg=none token accepted")
	}
}

func TestTamperedRoleRejected(t *testing.T) {
	tok, _ := Issue(secret, rbac.NewUser(1, "viewer@demo.local", "viewer"), now)
	parts := strings.Split(tok, ".")
	payload, _ := base64.RawURLEncoding.DecodeString(parts[1])
	forged := strings.Replace(string(payload), `"viewer"`, `"owner"`, 1)
	parts[1] = base64.RawURLEncoding.EncodeToString([]byte(forged))
	if _, err := Parse(secret, strings.Join(parts, "."), now); err == nil {
		t.Fatal("token with an edited role accepted")
	}
}

func TestPermissionsComeFromRoleNotToken(t *testing.T) {
	claims := jwt.MapClaims{"sub": "1", "email": "v", "role": "viewer", "perms": []string{"data.sensitive"},
		"iat": now.Unix(), "exp": now.Add(time.Hour).Unix()}
	tok, _ := jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString(secret)
	u, err := Parse(secret, tok, now)
	if err != nil {
		t.Fatal(err)
	}
	if u.Can(rbac.DataSensitive) {
		t.Fatal("a perms claim in the token granted a permission")
	}
}

func TestEmptySecretRefused(t *testing.T) {
	if _, err := Issue(nil, rbac.NewUser(1, "v", "viewer"), now); err == nil {
		t.Fatal("issued a token with an empty secret")
	}
}

func TestPasswordHashAndCheck(t *testing.T) {
	h, err := HashPassword("analyst-demo")
	if err != nil {
		t.Fatal(err)
	}
	if !CheckPassword(h, "analyst-demo") || CheckPassword(h, "analyst-demo ") || CheckPassword(h, "") {
		t.Fatal("password check is wrong")
	}
}
