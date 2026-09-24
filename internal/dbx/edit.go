package dbx

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"sort"
	"strings"
)

// Editable describes how a result set maps back to a table, so its cells can
// be edited and saved with UPDATE statements.
type Editable struct {
	Schema string `json:"schema"`
	Table  string `json:"table"`
	// Columns has, per result column, the table column it shows, or "" when
	// it shows an expression or a type dbird can't edit (binary, arrays).
	Columns []string `json:"columns"`
	// Key lists the result columns holding the table's primary key.
	Key []int `json:"key"`
}

// sqlToken is one token of a statement. Keywords and unquoted identifiers
// have kind 'w' and their text as written; quoted identifiers kind 'q' with
// the quotes removed.
type sqlToken struct {
	kind byte // 'w' word, 'q' quoted identifier, 's' string, 'n' number, 'p' punctuation
	text string
}

func (t sqlToken) is(word string) bool { return t.kind == 'w' && strings.EqualFold(t.text, word) }

func (t sqlToken) ident() bool { return t.kind == 'w' || t.kind == 'q' }

// tokenize splits stmt into tokens, dropping comments (# only starts one in
// MySQL; in PostgreSQL it is an operator). ok is false for input it can't make
// sense of, such as an unterminated string.
func tokenize(stmt, driver string) (toks []sqlToken, ok bool) {
	s := stmt
	i := 0
	for i < len(s) {
		c := s[i]
		switch {
		case c == ' ' || c == '\t' || c == '\n' || c == '\r':
			i++
		case c == '-' && strings.HasPrefix(s[i:], "--"), c == '#' && driver == MySQL:
			for i < len(s) && s[i] != '\n' {
				i++
			}
		case c == '/' && strings.HasPrefix(s[i:], "/*"):
			end := strings.Index(s[i+2:], "*/")
			if end < 0 {
				return nil, false
			}
			i += end + 4
		case c == '\'':
			j := i + 1
			var b strings.Builder
			for {
				if j >= len(s) {
					return nil, false
				}
				if s[j] == '\\' && j+1 < len(s) {
					b.WriteByte(s[j+1])
					j += 2
					continue
				}
				if s[j] == '\'' {
					if j+1 < len(s) && s[j+1] == '\'' {
						b.WriteByte('\'')
						j += 2
						continue
					}
					break
				}
				b.WriteByte(s[j])
				j++
			}
			toks = append(toks, sqlToken{'s', b.String()})
			i = j + 1
		case c == '"' || c == '`' || c == '[':
			closer := c
			if c == '[' {
				closer = ']'
			}
			j := i + 1
			var b strings.Builder
			for {
				if j >= len(s) {
					return nil, false
				}
				if s[j] == closer {
					if closer != ']' && j+1 < len(s) && s[j+1] == closer {
						b.WriteByte(closer)
						j += 2
						continue
					}
					break
				}
				b.WriteByte(s[j])
				j++
			}
			toks = append(toks, sqlToken{'q', b.String()})
			i = j + 1
		case c == '$' && i+1 < len(s) && (s[i+1] == '$' || isWordByte(s[i+1]) && !isDigit(s[i+1])):
			// PostgreSQL dollar quoting: $$...$$ or $tag$...$tag$.
			end := strings.IndexByte(s[i+1:], '$')
			if end < 0 {
				return nil, false
			}
			tag := s[i : i+end+2]
			if strings.Trim(tag[1:len(tag)-1], "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789_") != "" {
				toks = append(toks, sqlToken{'p', "$"})
				i++
				continue
			}
			rest := s[i+len(tag):]
			close := strings.Index(rest, tag)
			if close < 0 {
				return nil, false
			}
			toks = append(toks, sqlToken{'s', rest[:close]})
			i += len(tag) + close + len(tag)
		case isDigit(c):
			j := i
			for j < len(s) && (isWordByte(s[j]) || s[j] == '.') {
				j++
			}
			toks = append(toks, sqlToken{'n', s[i:j]})
			i = j
		case isWordByte(c) || c >= 0x80:
			j := i
			for j < len(s) && (isWordByte(s[j]) || s[j] == '$' || s[j] >= 0x80) {
				j++
			}
			toks = append(toks, sqlToken{'w', s[i:j]})
			i = j
		default:
			toks = append(toks, sqlToken{'p', string(c)})
			i++
		}
	}
	return toks, true
}

func isDigit(c byte) bool { return c >= '0' && c <= '9' }
func isWordByte(c byte) bool {
	return c == '_' || isDigit(c) || c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z'
}

// selectItem is one entry of a select list.
type selectItem struct {
	star   bool   // * or t.*
	column string // plain column reference, "" for an expression
	quoted bool   // column was a quoted identifier
	name   string // result column name when predictable (alias or column)
}

// simpleSelect is a SELECT reading one table.
type simpleSelect struct {
	items  []selectItem
	schema string // "" when unqualified
	table  string
	// ref is the table reference as written, for PostgreSQL's to_regclass.
	ref string
}

// Reasons a result set is read-only, shown to the user.
const (
	roNotSimple = "only results of a SELECT from a single table can be edited"
	roNoKey     = "the table has no primary key"
)

// parseSimpleSelect recognizes SELECT <columns> FROM <table> [alias]
// [WHERE …] [ORDER BY …] [LIMIT …]: one table, no joins, grouping, DISTINCT
// or set operations. It returns a reason when stmt is anything else.
func parseSimpleSelect(stmt, driver string) (*simpleSelect, string) {
	toks, ok := tokenize(stmt, driver)
	if !ok {
		return nil, roNotSimple
	}
	for len(toks) > 0 && toks[len(toks)-1].kind == 'p' && toks[len(toks)-1].text == ";" {
		toks = toks[:len(toks)-1]
	}
	if len(toks) < 4 || !toks[0].is("select") {
		return nil, roNotSimple
	}
	i := 1
	if toks[i].is("distinct") {
		return nil, "results of SELECT DISTINCT can't be edited"
	}
	if toks[i].is("all") {
		i++
	}

	// Split the select list on top-level commas, up to FROM.
	var items [][]sqlToken
	var cur []sqlToken
	depth := 0
	for ; i < len(toks); i++ {
		t := toks[i]
		if depth == 0 && t.is("from") {
			break
		}
		if t.kind == 'p' {
			switch t.text {
			case "(":
				depth++
			case ")":
				depth--
			case ",":
				if depth == 0 {
					items = append(items, cur)
					cur = nil
					continue
				}
			}
		}
		cur = append(cur, t)
	}
	if i >= len(toks) || len(cur) == 0 {
		return nil, roNotSimple
	}
	items = append(items, cur)
	i++ // FROM

	sel := &simpleSelect{}
	for _, it := range items {
		sel.items = append(sel.items, parseSelectItem(it))
	}

	// Table reference: name or schema.name.
	if i >= len(toks) || !toks[i].ident() {
		return nil, roNotSimple
	}
	refStart := i
	sel.table = toks[i].text
	i++
	if i+1 < len(toks) && toks[i].kind == 'p' && toks[i].text == "." && toks[i+1].ident() {
		sel.schema, sel.table = sel.table, toks[i+1].text
		i += 2
	}
	sel.ref = rebuild(toks[refStart:i])
	if i < len(toks) && toks[i].kind == 'p' && toks[i].text == "(" {
		return nil, roNotSimple // table function
	}
	// Optional alias.
	if i < len(toks) && toks[i].is("as") {
		i++
	}
	if i < len(toks) && toks[i].ident() && !clauseStart(toks[i]) {
		i++
	}
	// Whatever follows may only be filtering, ordering and limiting.
	if i < len(toks) && !clauseStart(toks[i]) {
		return nil, roNotSimple
	}
	depth = 0
	for ; i < len(toks); i++ {
		t := toks[i]
		switch {
		case t.kind == 'p' && t.text == "(":
			depth++
		case t.kind == 'p' && t.text == ")":
			depth--
		case t.kind == 'p' && t.text == ";":
			return nil, roNotSimple // more than one statement
		case depth == 0:
			for _, w := range []string{"join", "group", "having", "union", "intersect", "except", "window", "from"} {
				if t.is(w) {
					return nil, roNotSimple
				}
			}
		}
	}
	return sel, ""
}

func clauseStart(t sqlToken) bool {
	for _, w := range []string{"where", "order", "limit", "offset", "fetch", "for"} {
		if t.is(w) {
			return true
		}
	}
	return false
}

func parseSelectItem(it []sqlToken) selectItem {
	isDot := func(t sqlToken) bool { return t.kind == 'p' && t.text == "." }
	isStar := func(t sqlToken) bool { return t.kind == 'p' && t.text == "*" }
	switch {
	case len(it) == 1 && isStar(it[0]):
		return selectItem{star: true}
	case len(it) == 3 && it[0].ident() && isDot(it[1]) && isStar(it[2]):
		return selectItem{star: true}
	}
	// col, t.col, s.t.col — optionally followed by [AS] alias.
	n := 1
	for n+1 < len(it) && isDot(it[n]) && it[n+1].ident() {
		n += 2
	}
	if !it[0].ident() || it[0].kind == 'w' && isKeywordish(it[0].text) {
		return selectItem{name: aliasOf(it)}
	}
	col := it[n-1]
	rest := it[n:]
	item := selectItem{column: col.text, quoted: col.kind == 'q', name: col.text}
	switch {
	case len(rest) == 0:
	case len(rest) == 1 && rest[0].ident():
		item.name = rest[0].text
	case len(rest) == 2 && rest[0].is("as") && rest[1].ident():
		item.name = rest[1].text
	default:
		return selectItem{name: aliasOf(it)}
	}
	return item
}

// aliasOf returns the alias of an expression item ("expr AS x" or "expr x"),
// or "" when its result name can't be predicted.
func aliasOf(it []sqlToken) string {
	n := len(it)
	if n >= 3 && it[n-2].is("as") && it[n-1].ident() {
		return it[n-1].text
	}
	return ""
}

func isKeywordish(w string) bool {
	switch strings.ToLower(w) {
	case "case", "cast", "not", "null", "true", "false", "exists", "distinct", "interval", "date", "time", "timestamp":
		return true
	}
	return false
}

// rebuild writes tokens back as SQL, for a table reference.
func rebuild(toks []sqlToken) string {
	var b strings.Builder
	for _, t := range toks {
		switch t.kind {
		case 'q':
			b.WriteString(doubleQuote(t.text))
		default:
			b.WriteString(t.text)
		}
	}
	return b.String()
}

// EditInfo reports whether the result of stmt, with result columns cols, can
// be edited, reading table metadata through q (the session that ran stmt, so
// USE and search_path apply). The reason is set when it can't.
func EditInfo(ctx context.Context, q Querier, driver, stmt string, cols []Column) (*Editable, string) {
	sel, reason := parseSimpleSelect(stmt, driver)
	if sel == nil {
		return nil, reason
	}
	schema, table, reason, err := resolveTable(ctx, q, driver, sel)
	if err != nil || reason != "" {
		if reason == "" {
			reason = roNotSimple
		}
		return nil, reason
	}
	meta, err := Columns(ctx, q, driver, schema, table)
	if err != nil || len(meta) == 0 {
		return nil, roNotSimple
	}

	// Expand the select list to the table columns it shows, in order.
	type want struct {
		col  *ColumnInfo // nil for an expression
		name string      // expected result name, "" = any
	}
	var wants []want
	for _, it := range sel.items {
		if it.star {
			for i := range meta {
				wants = append(wants, want{&meta[i], meta[i].Name})
			}
			continue
		}
		w := want{name: it.name}
		if it.column != "" {
			w.col = findColumn(meta, it.column, it.quoted, driver)
			if w.col == nil {
				return nil, roNotSimple
			}
		}
		wants = append(wants, w)
	}
	if len(wants) != len(cols) {
		return nil, roNotSimple
	}
	ed := &Editable{Schema: schema, Table: table, Columns: make([]string, len(cols))}
	for i, w := range wants {
		if w.name != "" && !strings.EqualFold(w.name, cols[i].Name) {
			return nil, roNotSimple
		}
		if w.col != nil && editableType(w.col.Type) {
			ed.Columns[i] = w.col.Name
		}
	}
	for _, m := range meta {
		if !m.PrimaryKey {
			continue
		}
		found := -1
		for i, w := range wants {
			if w.col != nil && w.col.Name == m.Name {
				found = i
				break
			}
		}
		if found < 0 {
			return nil, fmt.Sprintf("the primary key column %s isn't in the result", m.Name)
		}
		ed.Key = append(ed.Key, found)
	}
	if len(ed.Key) == 0 {
		return nil, roNoKey
	}
	return ed, ""
}

func findColumn(meta []ColumnInfo, name string, quoted bool, driver string) *ColumnInfo {
	if driver == Postgres && !quoted {
		name = strings.ToLower(name)
	}
	for i := range meta {
		if meta[i].Name == name {
			return &meta[i]
		}
	}
	if driver == Postgres {
		return nil // identifiers are case-sensitive once folded
	}
	for i := range meta {
		if strings.EqualFold(meta[i].Name, name) {
			return &meta[i]
		}
	}
	return nil
}

// editableType reports whether values of a column type survive the round
// trip through the grid's text: binary data and arrays don't.
func editableType(t string) bool {
	t = strings.ToLower(t)
	if strings.HasSuffix(t, "[]") {
		return false
	}
	for _, b := range []string{"bytea", "blob", "binary", "varbinary"} {
		if strings.Contains(t, b) {
			return false
		}
	}
	return true
}

// resolveTable finds the schema and exact name of the table sel reads. The
// reason is set when it exists but isn't a table (a view, for example).
func resolveTable(ctx context.Context, q Querier, driver string, sel *simpleSelect) (schema, table, reason string, err error) {
	switch driver {
	case Postgres:
		var kind string
		err = q.QueryRowContext(ctx, `SELECT n.nspname, c.relname, c.relkind::text
			FROM pg_catalog.pg_class c JOIN pg_catalog.pg_namespace n ON n.oid = c.relnamespace
			WHERE c.oid = to_regclass($1)`, sel.ref).Scan(&schema, &table, &kind)
		if err != nil {
			return "", "", "", err
		}
		if kind != "r" && kind != "p" {
			return "", "", "only tables can be edited, not views", nil
		}
		return schema, table, "", nil
	case MySQL:
		schema = sel.schema
		if schema == "" {
			if schema, err = DefaultSchema(ctx, q, driver); err != nil {
				return "", "", "", err
			}
		}
		var kind string
		err = q.QueryRowContext(ctx, `SELECT table_name, table_type FROM information_schema.tables
			WHERE table_schema = ? AND table_name = ?`, schema, sel.table).Scan(&table, &kind)
		if err != nil {
			return "", "", "", err
		}
		if kind != "BASE TABLE" {
			return "", "", "only tables can be edited, not views", nil
		}
		return schema, table, "", nil
	case SQLite:
		schema = sel.schema
		if schema == "" {
			schema = "main"
		}
		var kind string
		err = q.QueryRowContext(ctx, `SELECT name, type FROM `+doubleQuote(schema)+`.sqlite_master
			WHERE type IN ('table', 'view') AND name = ? COLLATE NOCASE`, sel.table).Scan(&table, &kind)
		if err != nil {
			return "", "", "", err
		}
		if kind != "table" {
			return "", "", "only tables can be edited, not views", nil
		}
		return schema, table, "", nil
	}
	return "", "", "", fmt.Errorf("unsupported driver %q", driver)
}

// RowEdit is the change to one row: the original values of its primary key
// (in Editable.Key order) and the new value per column (nil for NULL).
type RowEdit struct {
	Key     []*string          `json:"key"`
	Changes map[string]*string `json:"changes"`
}

// EditRequest asks for rows of a table to be updated.
type EditRequest struct {
	Schema string `json:"schema"`
	Table  string `json:"table"`
	// KeyColumns names the primary key columns, matching RowEdit.Key.
	KeyColumns []string  `json:"keyColumns"`
	Rows       []RowEdit `json:"rows"`
}

// ErrRowChanged is returned when a row to update no longer exists, for
// example because someone else deleted it or changed its key.
var ErrRowChanged = errors.New("the row no longer exists; run the query again to see its current state")

// SaveEdits applies req in one transaction: all rows are updated or none.
// Each UPDATE must match exactly one row. It returns the number of rows
// updated.
func SaveEdits(ctx context.Context, db *sql.DB, driver string, req EditRequest) (int, error) {
	meta, err := Columns(ctx, db, driver, req.Schema, req.Table)
	if err != nil {
		return 0, err
	}
	types := map[string]string{}
	var pk []string
	for _, m := range meta {
		types[m.Name] = m.Type
		if m.PrimaryKey {
			pk = append(pk, m.Name)
		}
	}
	if len(pk) == 0 || len(pk) != len(req.KeyColumns) {
		return 0, errors.New("the table's primary key has changed; run the query again")
	}
	for i, k := range req.KeyColumns {
		if k != pk[i] {
			return 0, errors.New("the table's primary key has changed; run the query again")
		}
	}

	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()

	quote := func(s string) string { return QuoteIdent(driver, s) }
	target := quote(req.Schema) + "." + quote(req.Table)
	for n, row := range req.Rows {
		if len(row.Changes) == 0 {
			continue
		}
		if len(row.Key) != len(pk) {
			return 0, fmt.Errorf("row %d: incomplete primary key", n+1)
		}
		var sets, where []string
		var args []any
		// param returns the placeholder for a new argument.
		param := func(col string, v string) string {
			args = append(args, v)
			switch driver {
			case Postgres:
				// Send text and let the server convert it to the column type.
				return fmt.Sprintf("CAST($%d::text AS %s)", len(args), types[col])
			default:
				return "?"
			}
		}
		cols := make([]string, 0, len(row.Changes))
		for col := range row.Changes {
			if _, ok := types[col]; !ok {
				return 0, fmt.Errorf("the table has no column %s", col)
			}
			cols = append(cols, col)
		}
		sort.Strings(cols)
		for _, col := range cols {
			v := row.Changes[col]
			if v == nil {
				sets = append(sets, quote(col)+" = NULL")
			} else {
				sets = append(sets, quote(col)+" = "+param(col, *v))
			}
		}
		for i, col := range pk {
			if row.Key[i] == nil {
				return 0, fmt.Errorf("row %d: primary key %s is NULL", n+1, col)
			}
			where = append(where, quote(col)+" = "+param(col, *row.Key[i]))
		}
		stmt := "UPDATE " + target + " SET " + strings.Join(sets, ", ") + " WHERE " + strings.Join(where, " AND ")
		res, err := tx.ExecContext(ctx, stmt, args...)
		if err != nil {
			return 0, err
		}
		affected, err := res.RowsAffected()
		if err != nil {
			return 0, err
		}
		if affected == 0 && driver == MySQL {
			// MySQL counts changed rows, so 0 can mean "already had these values".
			affected, err = countRows(ctx, tx, target, where, args[len(args)-len(pk):])
			if err != nil {
				return 0, err
			}
		}
		switch {
		case affected == 0:
			return 0, fmt.Errorf("row %d: %w", n+1, ErrRowChanged)
		case affected > 1:
			return 0, fmt.Errorf("row %d: the key matched %d rows, nothing was saved", n+1, affected)
		}
	}
	if err := tx.Commit(); err != nil {
		return 0, err
	}
	return len(req.Rows), nil
}

func countRows(ctx context.Context, tx *sql.Tx, target string, where []string, args []any) (int64, error) {
	var n int64
	err := tx.QueryRowContext(ctx, "SELECT COUNT(*) FROM "+target+" WHERE "+strings.Join(where, " AND "), args...).Scan(&n)
	return n, err
}
