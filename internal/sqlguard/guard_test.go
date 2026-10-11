package sqlguard

import (
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/kotiniiqq/ai-data-analyst/internal/catalog"
	"github.com/kotiniiqq/ai-data-analyst/internal/rbac"
)

var (
	analyst = rbac.NewUser(2, "analyst@demo.local", "analyst")
	owner   = rbac.NewUser(3, "owner@demo.local", "owner")
	cat     = catalog.Default()
)

func code(err error) string {
	var v *Violation
	if errors.As(err, &v) {
		return v.Code
	}
	if err != nil {
		return "other:" + err.Error()
	}
	return ""
}

func TestRules(t *testing.T) {
	cases := []struct {
		name string
		user rbac.User
		sql  string
		want string // violation code, "" = allowed
	}{
		{"plain select", analyst, "select date, mrr_usd from gold.revenue_daily limit 10", ""},
		{"trailing semicolons", analyst, "select 1 from gold.revenue_daily limit 1;;", ""},
		{"keyword inside a string", analyst, "select 'delete; drop table x' as s from gold.revenue_daily limit 1", ""},
		{"keyword as a quoted alias", analyst, `select plan as "delete" from gold.revenue_daily limit 1`, ""},
		{"two statements", analyst, "select 1 from gold.revenue_daily limit 1; select 2 from gold.revenue_daily limit 1", "multiple_statements"},
		{"line comment hides nothing", analyst, "select 1 from gold.revenue_daily limit 1 -- x\n; drop table gold.revenue_daily", "multiple_statements"},
		{"block comment hides a keyword", analyst, "select 1 /* ; drop */ from gold.revenue_daily limit 1", ""},
		{"nested block comment", analyst, "select 1 /* a /* b */ ; c */ from gold.revenue_daily limit 1", ""},
		{"unterminated comment", analyst, "select 1 from gold.revenue_daily limit 1 /* ", "parse"},
		{"unterminated string", analyst, "select 'abc from gold.revenue_daily limit 1", "parse"},
		{"E-string escaped quote", analyst, `select E'\''; delete from gold.revenue_daily --' from gold.revenue_daily limit 1`, "multiple_statements"},
		{"dollar quoted string", analyst, "select $q$ ; drop $q$ from gold.revenue_daily limit 1", ""},
		{"empty", analyst, "  -- nothing\n ", "empty"},
		{"delete", analyst, "delete from gold.revenue_daily", "not_select"},
		{"table statement", analyst, "table gold.revenue_daily", "not_select"},
		{"cte select", analyst, "with m as (select plan, mrr_usd from gold.revenue_daily) select plan from m limit 5", ""},
		{"data-modifying cte", analyst, "with d as (delete from gold.revenue_daily returning *) select * from d limit 1", "forbidden_keyword"},
		{"select into", analyst, "select * into gold.copy from gold.revenue_daily limit 1", "forbidden_keyword"},
		{"for update", analyst, "select * from gold.revenue_daily limit 1 for update", "forbidden_keyword"},
		{"for share", analyst, "select * from gold.revenue_daily limit 1 for share", "forbidden_keyword"},
		{"pg_sleep", analyst, "select pg_sleep(5) from gold.revenue_daily limit 1", "forbidden_function"},
		{"PG_SLEEP upper", analyst, "select PG_SLEEP(5) from gold.revenue_daily limit 1", "forbidden_function"},
		{"qualified pg_catalog function", analyst, "select pg_catalog.pg_read_file('x') from gold.revenue_daily limit 1", "forbidden_function"},
		{"query_to_xml runs a string", analyst, "select query_to_xml('select * from users', true, true, '') from gold.revenue_daily limit 1", "forbidden_function"},
		{"set_config", analyst, "select set_config('statement_timeout', '0', false) from gold.revenue_daily limit 1", "forbidden_function"},
		{"dblink", analyst, "select dblink_connect('x') from gold.revenue_daily limit 1", "forbidden_function"},
		{"lo_import", analyst, "select lo_import('/etc/passwd') from gold.revenue_daily limit 1", "forbidden_function"},
		{"public table", analyst, "select * from public.users limit 1", "unknown_table"},
		{"unqualified table", analyst, "select * from revenue_daily limit 1", "unknown_table"},
		{"catalog view", analyst, "select * from pg_catalog.pg_user limit 1", "unknown_table"},
		{"information schema", analyst, "select * from information_schema.tables limit 1", "unknown_table"},
		{"joined unknown table", analyst, "select 1 from gold.revenue_daily r join public.users u on true limit 1", "unknown_table"},
		{"comma join unknown table", analyst, "select 1 from gold.revenue_daily r, public.users u limit 1", "unknown_table"},
		{"subquery unknown table", analyst, "select 1 from (select * from public.users) s limit 1", "unknown_table"},
		{"exists subquery unknown table", analyst, "select 1 from gold.revenue_daily where exists (select 1 from public.users) limit 1", "unknown_table"},
		{"extract from is not a table", analyst, "select extract(year from date) as y from gold.revenue_daily limit 1", ""},
		{"substring from is not a table", analyst, "select substring(plan from 1 for 2) from gold.revenue_daily limit 1", ""},
		{"sensitive table for analyst", analyst, "select month, gross_margin_pct from gold.finance_monthly limit 5", "sensitive_table"},
		{"sensitive table for owner", owner, "select month, gross_margin_pct from gold.finance_monthly limit 5", ""},
		{"select star on sensitive", owner, "select * from gold.infra_daily limit 5", "select_star_sensitive"},
		{"qualified star on sensitive", owner, "select i.* from gold.infra_daily i limit 5", "select_star_sensitive"},
		{"count star on sensitive is fine", owner, "select count(*) from gold.infra_daily limit 5", ""},
		{"select star on internal", analyst, "select * from gold.support_daily limit 5", ""},
		{"missing limit", analyst, "select * from gold.support_daily", "missing_limit"},
		{"limit all", analyst, "select * from gold.support_daily limit all", "missing_limit"},
		{"limit only in subquery", analyst, "select * from (select * from gold.support_daily limit 5) s", "missing_limit"},
		{"unicode identifier", analyst, "select tickets as Тикеты from gold.support_daily limit 3", ""},
		{"mixed case keywords", analyst, "SeLeCt Date FrOm GOLD.SUPPORT_DAILY LiMiT 3", ""},
		{"union with unknown table", analyst, "select email from public.users union select plan from gold.revenue_daily limit 3", "unknown_table"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, err := Check(c.sql, c.user, cat)
			if got := code(err); got != c.want {
				t.Fatalf("got %q, want %q (err: %v)", got, c.want, err)
			}
		})
	}
}

func TestLimitIsClampedTo1000(t *testing.T) {
	r, err := Check("select * from gold.support_daily limit 5000", analyst, cat)
	if err != nil {
		t.Fatal(err)
	}
	if r.Limit != 1000 || !strings.HasSuffix(r.SQL, "limit 1000") {
		t.Fatalf("got limit %d, sql %q", r.Limit, r.SQL)
	}
}

func TestCommentsAreStrippedFromTheOutput(t *testing.T) {
	r, err := Check("select tickets -- the count\n from gold.support_daily /* x */ limit 3", analyst, cat)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(r.SQL, "--") || strings.Contains(r.SQL, "/*") || strings.Contains(r.SQL, "count") {
		t.Fatalf("comments left in %q", r.SQL)
	}
}

func TestTablesAreReported(t *testing.T) {
	r, err := Check("select 1 from gold.revenue_daily r join gold.customers_monthly c on c.plan = r.plan limit 1", analyst, cat)
	if err != nil {
		t.Fatal(err)
	}
	slices.Sort(r.Tables)
	if !slices.Equal(r.Tables, []string{"gold.customers_monthly", "gold.revenue_daily"}) {
		t.Fatalf("got %v", r.Tables)
	}
}

func TestStringContentIsKeptAsIs(t *testing.T) {
	r, err := Check("select 'a  --  b' as s from gold.support_daily limit 1", analyst, cat)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(r.SQL, "'a  --  b'") {
		t.Fatalf("string literal changed: %q", r.SQL)
	}
}

func TestIsDistinctFromIsNotATable(t *testing.T) {
	if _, err := Check("select 1 from gold.revenue_daily where plan is not distinct from 'team' limit 1", analyst, cat); err != nil {
		t.Fatal(err)
	}
}
