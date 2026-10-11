// Package rbac maps roles to permissions. The role always comes from the
// signed token; nothing a user types can change it.
package rbac

import "slices"

type Perm string

const (
	AIChat        Perm = "ai.chat"
	MetricsView   Perm = "metrics.view"
	SQLGenerate   Perm = "sql.generate"
	SQLView       Perm = "sql.view"
	SQLRun        Perm = "sql.run"
	DataSensitive Perm = "data.sensitive"
)

// All lists every permission, in display order.
var All = []Perm{AIChat, MetricsView, SQLGenerate, SQLView, SQLRun, DataSensitive}

var roles = map[string][]Perm{
	"viewer":  {AIChat, MetricsView},
	"analyst": {AIChat, MetricsView, SQLGenerate, SQLView, SQLRun},
	"owner":   {AIChat, MetricsView, SQLGenerate, SQLView, SQLRun, DataSensitive},
}

// PermsFor returns the permissions of a role; an unknown role gets none.
func PermsFor(role string) []Perm {
	return slices.Clone(roles[role])
}

type User struct {
	ID    int64  `json:"id"`
	Email string `json:"email"`
	Role  string `json:"role"`
	Perms []Perm `json:"perms"`
}

func NewUser(id int64, email, role string) User {
	return User{ID: id, Email: email, Role: role, Perms: PermsFor(role)}
}

func (u User) Can(p Perm) bool {
	return slices.Contains(u.Perms, p)
}
