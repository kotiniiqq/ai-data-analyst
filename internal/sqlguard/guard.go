// Package sqlguard decides whether a query may run. It works on tokens, not
// on regular expressions over raw text, so keywords inside strings, quoted
// identifiers and comments are seen for what they are. The database role is
// the second line of defence: it can only read schema gold.
package sqlguard

import (
	"fmt"
	"slices"
	"strconv"
	"strings"

	"github.com/kotiniiqq/ai-data-analyst/internal/catalog"
	"github.com/kotiniiqq/ai-data-analyst/internal/rbac"
)

// MaxLimit is the most rows a query may return.
const MaxLimit = 1000

type Violation struct {
	Code string `json:"code"`
	Msg  string `json:"msg"`
}

func (v *Violation) Error() string { return v.Code + ": " + v.Msg }

func violation(code, format string, args ...any) *Violation {
	return &Violation{Code: code, Msg: fmt.Sprintf(format, args...)}
}

// Result is the query that may run: comments removed, LIMIT clamped.
type Result struct {
	SQL    string
	Tables []string
	Limit  int
}

var forbiddenKeywords = setOf(
	"insert", "update", "delete", "merge", "upsert", "drop", "create", "alter", "truncate",
	"grant", "revoke", "copy", "call", "do", "execute", "prepare", "deallocate", "vacuum",
	"analyze", "cluster", "reindex", "lock", "listen", "notify", "unlisten", "set", "reset",
	"refresh", "comment", "security", "begin", "commit", "rollback", "savepoint", "release",
	"into", "import", "load", "discard", "checkpoint",
)

var forbiddenFunctions = setOf(
	"set_config", "current_setting", "query_to_xml", "query_to_xml_and_xmlschema",
	"query_to_xmlschema", "table_to_xml", "table_to_xml_and_xmlschema", "table_to_xmlschema",
	"cursor_to_xml", "cursor_to_xmlschema", "schema_to_xml", "schema_to_xml_and_xmlschema",
	"schema_to_xmlschema", "database_to_xml", "database_to_xml_and_xmlschema",
	"database_to_xmlschema", "txid_current", "nextval", "setval", "currval", "lastval",
	"version", "inet_server_addr", "inet_server_port",
)

var forbiddenFunctionPrefixes = []string{"pg_", "lo_", "dblink"}

// Words that may stand before "(" without making it a function call.
var nonFunctionWords = setOf(
	"select", "from", "join", "where", "and", "or", "not", "in", "exists", "as", "on", "using",
	"any", "all", "some", "lateral", "values", "over", "filter", "within", "group", "by",
	"order", "having", "union", "intersect", "except", "with", "recursive", "materialized",
	"when", "then", "else", "case", "is", "between", "like", "ilike", "limit", "offset",
	"distinct", "return", "returning", "window", "partition", "rows", "range", "groups", "array",
)

// Words that end a FROM list at the same nesting depth.
var clauseWords = setOf(
	"where", "group", "having", "window", "order", "limit", "offset", "union", "intersect",
	"except", "fetch", "for", "returning", "select",
)

func setOf(words ...string) map[string]bool {
	m := make(map[string]bool, len(words))
	for _, w := range words {
		m[w] = true
	}
	return m
}

func isPunct(t token, p string) bool { return t.kind == kPunct && t.text == p }
func isWord(t token, w string) bool  { return t.kind == kWord && t.text == w }
func isIdent(t token) bool           { return t.kind == kWord || t.kind == kQIdent }

// Check validates sql for user u against the catalog.
func Check(sql string, u rbac.User, cat *catalog.Catalog) (Result, error) {
	all, err := lex(sql)
	if err != nil {
		return Result{}, violation("parse", "%v", err)
	}
	var toks []token
	for _, t := range all {
		if t.kind != kComment {
			toks = append(toks, t)
		}
	}
	for len(toks) > 0 && isPunct(toks[len(toks)-1], ";") {
		toks = toks[:len(toks)-1]
	}
	if len(toks) == 0 {
		return Result{}, violation("empty", "the query is empty")
	}
	if slices.ContainsFunc(toks, func(t token) bool { return isPunct(t, ";") }) {
		return Result{}, violation("multiple_statements", "only one statement is allowed")
	}
	if !isWord(toks[0], "select") && !isWord(toks[0], "with") {
		return Result{}, violation("not_select", "only SELECT queries are allowed")
	}
	for i, t := range toks {
		if t.kind == kWord && forbiddenKeywords[t.text] {
			return Result{}, violation("forbidden_keyword", "%q is not allowed", strings.ToUpper(t.text))
		}
		if isWord(t, "for") && i+1 < len(toks) && (isWord(toks[i+1], "share") || isWord(toks[i+1], "no") || isWord(toks[i+1], "key")) {
			return Result{}, violation("forbidden_keyword", "row locks are not allowed")
		}
	}
	if err := checkFunctions(toks); err != nil {
		return Result{}, err
	}
	tables, err := checkTables(toks, u, cat)
	if err != nil {
		return Result{}, err
	}
	if err := checkStar(toks, tables, cat); err != nil {
		return Result{}, err
	}
	limitTok, limit, err := findLimit(toks)
	if err != nil {
		return Result{}, err
	}
	return Result{SQL: render(sql, all, toks[len(toks)-1].end, limitTok, limit), Tables: tables, Limit: limit}, nil
}

func checkFunctions(toks []token) error {
	for i := 0; i+1 < len(toks); i++ {
		if !isIdent(toks[i]) || !isPunct(toks[i+1], "(") {
			continue
		}
		name := strings.ToLower(toks[i].text)
		if toks[i].kind == kWord && nonFunctionWords[name] {
			continue
		}
		if forbiddenFunctions[name] || slices.ContainsFunc(forbiddenFunctionPrefixes, func(p string) bool { return strings.HasPrefix(name, p) }) {
			return violation("forbidden_function", "function %s() is not allowed", name)
		}
	}
	return nil
}

// cteNames finds names defined by WITH: `name [(cols)] AS [NOT] [MATERIALIZED] (`.
func cteNames(toks []token) map[string]bool {
	names := map[string]bool{}
	for i := 1; i < len(toks); i++ {
		prev := toks[i-1]
		if !(isWord(prev, "with") || isWord(prev, "recursive") || isPunct(prev, ",")) || !isIdent(toks[i]) {
			continue
		}
		j := i + 1
		if j < len(toks) && isPunct(toks[j], "(") {
			j = matchParen(toks, j) + 1
		}
		if j < len(toks) && isWord(toks[j], "as") {
			names[strings.ToLower(toks[i].text)] = true
		}
	}
	return names
}

func matchParen(toks []token, open int) int {
	depth := 0
	for i := open; i < len(toks); i++ {
		if isPunct(toks[i], "(") {
			depth++
		} else if isPunct(toks[i], ")") {
			depth--
			if depth == 0 {
				return i
			}
		}
	}
	return len(toks) - 1
}

// checkTables walks the query and checks every relation named in a FROM
// list or a JOIN, at any nesting depth.
func checkTables(toks []token, u rbac.User, cat *catalog.Catalog) ([]string, error) {
	ctes := cteNames(toks)
	var tables []string
	type level struct{ funcCall, inFrom bool }
	stack := []level{{}}

	relationAt := func(i int) error {
		for i < len(toks) && (isWord(toks[i], "only") || isWord(toks[i], "lateral")) {
			i++
		}
		if i >= len(toks) || !isIdent(toks[i]) {
			return nil // a subquery or VALUES list; walked separately
		}
		parts := []string{strings.ToLower(toks[i].text)}
		for i+2 < len(toks) && isPunct(toks[i+1], ".") && isIdent(toks[i+2]) {
			parts = append(parts, strings.ToLower(toks[i+2].text))
			i += 2
		}
		if i+1 < len(toks) && isPunct(toks[i+1], "(") {
			return nil // a set-returning function; function names are checked elsewhere
		}
		name := strings.Join(parts, ".")
		if len(parts) == 1 && ctes[parts[0]] {
			return nil
		}
		t, ok := cat.Table(name)
		if len(parts) != 2 || parts[0] != "gold" || !ok {
			return violation("unknown_table", "table %s is not in the catalog (use gold.<table>)", name)
		}
		if t.Sensitive && !u.Can(rbac.DataSensitive) {
			return violation("sensitive_table", "table %s needs the data.sensitive permission", t.Name)
		}
		if !slices.Contains(tables, t.Name) {
			tables = append(tables, t.Name)
		}
		return nil
	}

	for i, t := range toks {
		top := &stack[len(stack)-1]
		switch {
		case isPunct(t, "("):
			fn := i > 0 && isIdent(toks[i-1]) && !(toks[i-1].kind == kWord && nonFunctionWords[toks[i-1].text])
			stack = append(stack, level{funcCall: fn})
		case isPunct(t, ")"):
			if len(stack) > 1 {
				stack = stack[:len(stack)-1]
			}
		case top.funcCall:
			// FROM inside extract(), substring(), trim() is an argument keyword.
		case isWord(t, "from"):
			if i >= 2 && isWord(toks[i-1], "distinct") && (isWord(toks[i-2], "is") || isWord(toks[i-2], "not")) {
				continue // IS [NOT] DISTINCT FROM
			}
			top.inFrom = true
			if err := relationAt(i + 1); err != nil {
				return nil, err
			}
		case isWord(t, "join"):
			if err := relationAt(i + 1); err != nil {
				return nil, err
			}
		case isPunct(t, ",") && top.inFrom:
			if err := relationAt(i + 1); err != nil {
				return nil, err
			}
		case t.kind == kWord && clauseWords[t.text]:
			top.inFrom = false
		}
	}
	return tables, nil
}

// checkStar refuses SELECT * (or t.*) when a sensitive table is involved:
// sensitive data must be asked for column by column.
func checkStar(toks []token, tables []string, cat *catalog.Catalog) error {
	sensitive := slices.ContainsFunc(tables, func(name string) bool {
		t, _ := cat.Table(name)
		return t.Sensitive
	})
	if !sensitive {
		return nil
	}
	for i := 1; i < len(toks); i++ {
		if !isPunct(toks[i], "*") {
			continue
		}
		p := toks[i-1]
		if isWord(p, "select") || isWord(p, "distinct") || isWord(p, "all") || isPunct(p, ",") || isPunct(p, ".") {
			return violation("select_star_sensitive", "list the columns you need from sensitive tables instead of *")
		}
	}
	return nil
}

// findLimit returns the index of the top-level LIMIT number and its value (clamped).
func findLimit(toks []token) (int, int, error) {
	depth, at := 0, -1
	for i, t := range toks {
		switch {
		case isPunct(t, "("):
			depth++
		case isPunct(t, ")"):
			depth--
		case depth == 0 && isWord(t, "limit"):
			at = i
		}
	}
	if at < 0 || at+1 >= len(toks) || toks[at+1].kind != kNumber {
		return 0, 0, violation("missing_limit", "add LIMIT (at most %d rows)", MaxLimit)
	}
	n, err := strconv.Atoi(toks[at+1].text)
	if err != nil || n < 0 {
		return 0, 0, violation("missing_limit", "LIMIT must be a whole number")
	}
	return at + 1, min(n, MaxLimit), nil
}

// render rebuilds the query from the source without comments, up to the last
// code token, with the LIMIT value replaced by the clamped one.
func render(src string, all []token, end int, limitTok, limit int) string {
	var b strings.Builder
	pos, code := 0, 0
	for _, t := range all {
		if t.start >= end {
			break
		}
		b.WriteString(src[pos:t.start])
		switch {
		case t.kind == kComment:
			b.WriteString(" ")
		case code == limitTok:
			b.WriteString(strconv.Itoa(limit))
		default:
			b.WriteString(src[t.start:t.end])
		}
		if t.kind != kComment {
			code++
		}
		pos = t.end
	}
	return strings.TrimSpace(b.String())
}
