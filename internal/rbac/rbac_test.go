package rbac

import "testing"

func TestViewerCanChatButNotRunSQL(t *testing.T) {
	u := NewUser(1, "viewer@demo.local", "viewer")
	if !u.Can(AIChat) || !u.Can(MetricsView) {
		t.Fatal("viewer must be able to chat and view metrics")
	}
	if u.Can(SQLRun) || u.Can(SQLView) || u.Can(DataSensitive) {
		t.Fatal("viewer must not see or run SQL, nor see sensitive data")
	}
}

func TestAnalystRunsSQLButNoSensitiveData(t *testing.T) {
	u := NewUser(2, "analyst@demo.local", "analyst")
	for _, p := range []Perm{AIChat, MetricsView, SQLGenerate, SQLView, SQLRun} {
		if !u.Can(p) {
			t.Fatalf("analyst lacks %s", p)
		}
	}
	if u.Can(DataSensitive) {
		t.Fatal("analyst must not see sensitive data")
	}
}

func TestOwnerHasEveryPermission(t *testing.T) {
	u := NewUser(3, "owner@demo.local", "owner")
	for _, p := range All {
		if !u.Can(p) {
			t.Fatalf("owner lacks %s", p)
		}
	}
}

func TestUnknownRoleHasNoPermissions(t *testing.T) {
	u := NewUser(4, "x@demo.local", "admin")
	if len(u.Perms) != 0 || u.Can(AIChat) {
		t.Fatalf("unknown role got permissions: %v", u.Perms)
	}
}

func TestPermsForReturnsACopy(t *testing.T) {
	p := PermsFor("viewer")
	p[0] = DataSensitive
	if NewUser(1, "v", "viewer").Can(DataSensitive) {
		t.Fatal("mutating the returned slice changed the role table")
	}
}
