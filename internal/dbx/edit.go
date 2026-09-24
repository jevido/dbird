package dbx

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
)

// Editable describes how a result set maps back to a table, so its cells can
// be edited, rows added and deleted, and saved.
type Editable struct {
	Schema string `json:"schema"`
	Table  string `json:"table"`
	// Columns describes each result column; Name is "" when it shows an
	// expression or a type dbird can't edit (binary, arrays, generated).
	Columns []EditColumn `json:"columns"`
	// Key lists the result columns that identify a row: the primary key, or
	// else a unique key without NULLs.
	Key []int `json:"key"`
	// KeyName says which key that is ("primary key" or "unique key <name>").
	KeyName string `json:"keyName"`
}

// EditColumn is what the grid needs to edit one column.
type EditColumn struct {
	// Name is the table column when it can be edited, else "".
	Name string `json:"name"`
	// Source is the table column shown, also when it can't be edited.
	Source string `json:"source"`
	Type   string `json:"type"`
	// Kind picks the editor: text, number, bool, date, datetime, time, json
	// or enum.
	Kind     string `json:"kind"`
	Nullable bool   `json:"nullable"`
	// HasDefault means a new row may leave the column out.
	HasDefault bool     `json:"hasDefault"`
	Enum       []string `json:"enum"`
	// Ref is the column a single-column foreign key points to.
	Ref *ColumnRef `json:"ref"`
}

// ColumnRef names a table column.
type ColumnRef struct {
	Schema string `json:"schema"`
	Table  string `json:"table"`
	Column string `json:"column"`
}

// colMeta is a table column as needed for editing.
type colMeta struct {
	name, typ             string
	nullable, pk          bool
	hasDefault, generated bool
	enum                  []string
	kind                  string
}

// tableColumns reads the columns of schema.table for editing.
func tableColumns(ctx context.Context, q Querier, driver, schema, table string) ([]colMeta, error) {
	var query string
	var args []any
	switch driver {
	case Postgres:
		query = `SELECT a.attname, pg_catalog.format_type(a.atttypid, a.atttypmod), NOT a.attnotnull,
				COALESCE((SELECT true FROM pg_catalog.pg_index i
					WHERE i.indrelid = c.oid AND i.indisprimary AND a.attnum = ANY(i.indkey)), false),
				a.atthasdef OR a.attidentity <> '', a.attgenerated <> '',
				COALESCE((SELECT string_agg(e.enumlabel, E'\n' ORDER BY e.enumsortorder)
					FROM pg_catalog.pg_enum e WHERE e.enumtypid = a.atttypid), '')
			FROM pg_catalog.pg_attribute a
			JOIN pg_catalog.pg_class c ON c.oid = a.attrelid
			JOIN pg_catalog.pg_namespace n ON n.oid = c.relnamespace
			WHERE n.nspname = $1 AND c.relname = $2 AND a.attnum > 0 AND NOT a.attisdropped
			ORDER BY a.attnum`
		args = []any{schema, table}
	case MySQL:
		query = `SELECT column_name, column_type, is_nullable = 'YES', column_key = 'PRI',
				column_default IS NOT NULL OR extra LIKE '%auto_increment%',
				extra LIKE '%GENERATED%' AND extra NOT LIKE '%DEFAULT_GENERATED%', ''
			FROM information_schema.columns WHERE table_schema = ? AND table_name = ?
			ORDER BY ordinal_position`
		args = []any{schema, table}
	case SQLite:
		query = `SELECT name, type, "notnull" = 0, pk > 0,
				dflt_value IS NOT NULL OR (pk = 1 AND upper(type) = 'INTEGER'
					AND (SELECT count(*) FROM pragma_table_info(?, ?) WHERE pk > 0) = 1),
				hidden IN (2, 3), ''
			FROM pragma_table_xinfo(?, ?) WHERE hidden <> 1 ORDER BY cid`
		args = []any{table, schema, table, schema}
	default:
		return nil, fmt.Errorf("unsupported driver %q", driver)
	}
	rows, err := q.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []colMeta
	for rows.Next() {
		var m colMeta
		var enum string
		if err := rows.Scan(&m.name, &m.typ, &m.nullable, &m.pk, &m.hasDefault, &m.generated, &enum); err != nil {
			return nil, err
		}
		if enum != "" {
			m.enum = strings.Split(enum, "\n")
		} else if driver == MySQL {
			m.enum = mysqlEnum(m.typ)
		}
		m.kind = columnKind(driver, m.typ, m.enum != nil)
		out = append(out, m)
	}
	return out, rows.Err()
}

// mysqlEnum returns the values of a MySQL column type enum('a','b').
func mysqlEnum(t string) []string {
	if !strings.HasPrefix(strings.ToLower(t), "enum(") || !strings.HasSuffix(t, ")") {
		return nil
	}
	toks, ok := tokenize(t[5:len(t)-1], MySQL)
	if !ok {
		return nil
	}
	var out []string
	for _, tk := range toks {
		if tk.kind == 's' {
			out = append(out, tk.text)
		}
	}
	return out
}

// columnKind picks the grid editor for a column type.
func columnKind(driver, t string, isEnum bool) string {
	t = strings.ToLower(strings.TrimSpace(t))
	switch {
	case isEnum:
		return "enum"
	case t == "boolean" || t == "bool" || driver == MySQL && strings.HasPrefix(t, "tinyint(1)"):
		return "bool"
	case t == "date":
		return "date"
	case strings.HasPrefix(t, "timestamp") || strings.HasPrefix(t, "datetime"):
		return "datetime"
	case strings.HasPrefix(t, "time"):
		return "time"
	case t == "json" || t == "jsonb":
		return "json"
	}
	for _, n := range []string{"int", "numeric", "decimal", "real", "double", "float", "serial", "money"} {
		if strings.Contains(t, n) {
			return "number"
		}
	}
	return "text"
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

// foreignKeys returns, per column of schema.table, the column a
// single-column foreign key points to.
func foreignKeys(ctx context.Context, q Querier, driver, schema, table string) (map[string]ColumnRef, error) {
	var query string
	var args []any
	switch driver {
	case Postgres:
		query = `SELECT a.attname, fn.nspname, fc.relname, fa.attname
			FROM pg_catalog.pg_constraint k
			JOIN pg_catalog.pg_class c ON c.oid = k.conrelid
			JOIN pg_catalog.pg_namespace n ON n.oid = c.relnamespace
			JOIN pg_catalog.pg_attribute a ON a.attrelid = k.conrelid AND a.attnum = k.conkey[1]
			JOIN pg_catalog.pg_class fc ON fc.oid = k.confrelid
			JOIN pg_catalog.pg_namespace fn ON fn.oid = fc.relnamespace
			JOIN pg_catalog.pg_attribute fa ON fa.attrelid = k.confrelid AND fa.attnum = k.confkey[1]
			WHERE k.contype = 'f' AND cardinality(k.conkey) = 1 AND n.nspname = $1 AND c.relname = $2`
		args = []any{schema, table}
	case MySQL:
		query = `SELECT k.column_name, k.referenced_table_schema, k.referenced_table_name, k.referenced_column_name
			FROM information_schema.key_column_usage k
			WHERE k.table_schema = ? AND k.table_name = ? AND k.referenced_table_name IS NOT NULL
			AND (SELECT count(*) FROM information_schema.key_column_usage k2
				WHERE k2.constraint_schema = k.constraint_schema AND k2.constraint_name = k.constraint_name
				AND k2.table_name = k.table_name) = 1`
		args = []any{schema, table}
	case SQLite:
		query = `SELECT "from", ?, "table", "to" FROM pragma_foreign_key_list(?, ?)
			WHERE id IN (SELECT id FROM pragma_foreign_key_list(?, ?) GROUP BY id HAVING count(*) = 1)
			AND "to" IS NOT NULL`
		args = []any{schema, table, schema, table, schema}
	default:
		return nil, nil
	}
	rows, err := q.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]ColumnRef{}
	for rows.Next() {
		var col string
		var ref ColumnRef
		if err := rows.Scan(&col, &ref.Schema, &ref.Table, &ref.Column); err != nil {
			return nil, err
		}
		out[col] = ref
	}
	return out, rows.Err()
}

// tableKey returns the columns identifying a row of schema.table: its primary
// key, or else the smallest unique index whose columns can't be NULL. name is
// "primary key" or "unique key <index>"; cols is empty when there is no key.
func tableKey(ctx context.Context, q Querier, driver, schema, table string, meta []colMeta) (name string, cols []string, err error) {
	for _, m := range meta {
		if m.pk {
			cols = append(cols, m.name)
		}
	}
	if len(cols) > 0 {
		return "primary key", cols, nil
	}
	notNull := map[string]bool{}
	for _, m := range meta {
		notNull[m.name] = !m.nullable
	}
	var query string
	var args []any
	switch driver {
	case Postgres:
		query = `SELECT ic.relname, string_agg(a.attname, E'\n' ORDER BY k.ord)
			FROM pg_catalog.pg_index i
			JOIN pg_catalog.pg_class c ON c.oid = i.indrelid
			JOIN pg_catalog.pg_namespace n ON n.oid = c.relnamespace
			JOIN pg_catalog.pg_class ic ON ic.oid = i.indexrelid
			CROSS JOIN LATERAL unnest(i.indkey) WITH ORDINALITY AS k(attnum, ord)
			JOIN pg_catalog.pg_attribute a ON a.attrelid = c.oid AND a.attnum = k.attnum
			WHERE n.nspname = $1 AND c.relname = $2 AND i.indisunique
				AND i.indpred IS NULL AND i.indexprs IS NULL
			GROUP BY ic.relname ORDER BY count(*), ic.relname`
		args = []any{schema, table}
	case MySQL:
		query = `SELECT index_name, GROUP_CONCAT(column_name ORDER BY seq_in_index SEPARATOR '\n')
			FROM information_schema.statistics
			WHERE table_schema = ? AND table_name = ? AND non_unique = 0
			GROUP BY index_name HAVING SUM(sub_part IS NOT NULL OR column_name IS NULL) = 0
			ORDER BY count(*), index_name`
		args = []any{schema, table}
	case SQLite:
		query = `SELECT l.name, group_concat(i.name, char(10))
			FROM pragma_index_list(?, ?) l, pragma_index_info(l.name, ?) i
			WHERE l."unique" AND NOT l.partial
			GROUP BY l.name HAVING min(i.cid) >= 0 ORDER BY count(*), l.name`
		args = []any{table, schema, schema}
	default:
		return "", nil, nil
	}
	rows, err := q.QueryContext(ctx, query, args...)
	if err != nil {
		return "", nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var idx, list string
		if err := rows.Scan(&idx, &list); err != nil {
			return "", nil, err
		}
		cols := strings.Split(list, "\n")
		usable := true
		for _, c := range cols {
			usable = usable && notNull[c]
		}
		if usable {
			return "unique key " + idx, cols, nil
		}
	}
	return "", nil, rows.Err()
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
	meta, err := tableColumns(ctx, q, driver, schema, table)
	if err != nil || len(meta) == 0 {
		return nil, roNotSimple
	}

	// Expand the select list to the table columns it shows, in order.
	type want struct {
		col  *colMeta // nil for an expression
		name string   // expected result name, "" = any
	}
	var wants []want
	for _, it := range sel.items {
		if it.star {
			for i := range meta {
				wants = append(wants, want{&meta[i], meta[i].name})
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
	for i, w := range wants {
		if w.name != "" && !strings.EqualFold(w.name, cols[i].Name) {
			return nil, roNotSimple
		}
	}
	keyName, keyCols, err := tableKey(ctx, q, driver, schema, table, meta)
	if err != nil {
		return nil, roNotSimple
	}
	if len(keyCols) == 0 {
		return nil, roNoKey
	}
	fks, _ := foreignKeys(ctx, q, driver, schema, table)

	ed := &Editable{Schema: schema, Table: table, KeyName: keyName, Columns: make([]EditColumn, len(cols))}
	for i, w := range wants {
		if w.col == nil {
			continue
		}
		c := EditColumn{Source: w.col.name, Type: w.col.typ, Kind: w.col.kind, Nullable: w.col.nullable,
			HasDefault: w.col.hasDefault || w.col.generated, Enum: w.col.enum}
		if editableType(w.col.typ) && !w.col.generated {
			c.Name = w.col.name
		}
		if ref, ok := fks[w.col.name]; ok {
			c.Ref = &ref
		}
		ed.Columns[i] = c
	}
	for _, k := range keyCols {
		found := -1
		for i, w := range wants {
			if w.col != nil && w.col.name == k {
				found = i
				break
			}
		}
		if found < 0 {
			return nil, fmt.Sprintf("the %s column %s isn't in the result", keyName, k)
		}
		ed.Key = append(ed.Key, found)
	}
	return ed, ""
}

func findColumn(meta []colMeta, name string, quoted bool, driver string) *colMeta {
	if driver == Postgres && !quoted {
		name = strings.ToLower(name)
	}
	for i := range meta {
		if meta[i].name == name {
			return &meta[i]
		}
	}
	if driver == Postgres {
		return nil // identifiers are case-sensitive once folded
	}
	for i := range meta {
		if strings.EqualFold(meta[i].name, name) {
			return &meta[i]
		}
	}
	return nil
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

// RowUpdate changes one row: the values of its key as queried (in KeyColumns
// order), the new value per column (nil for NULL), and the values those
// columns had when queried, to detect changes made by someone else since.
type RowUpdate struct {
	Key      []*string          `json:"key"`
	Changes  map[string]*string `json:"changes"`
	Original map[string]*string `json:"original"`
}

// RowInsert is a new row. Columns left out get their default.
type RowInsert struct {
	Values map[string]*string `json:"values"`
}

// RowDelete identifies a row to delete by its key as queried.
type RowDelete struct {
	Key []*string `json:"key"`
}

// EditRequest asks for rows of a table to be updated, inserted and deleted.
type EditRequest struct {
	Schema string `json:"schema"`
	Table  string `json:"table"`
	// KeyColumns names the key columns, matching the Key of each row.
	KeyColumns []string `json:"keyColumns"`
	// Columns lists the table columns to return for each saved row, in grid
	// order ("" for a result column that isn't a table column).
	Columns []string    `json:"columns"`
	Updates []RowUpdate `json:"updates"`
	Inserts []RowInsert `json:"inserts"`
	Deletes []RowDelete `json:"deletes"`
}

// SaveResult reports a save. Updated and Inserted hold the rows as stored
// afterwards (in EditRequest.Columns order), so defaults and trigger changes
// show up.
type SaveResult struct {
	Updated  [][]*string `json:"updated"`
	Inserted [][]*string `json:"inserted"`
	Deleted  int         `json:"deleted"`
}

var (
	// ErrRowChanged is returned when a row to save no longer exists.
	ErrRowChanged = errors.New("the row no longer exists; run the query again to see its current state")
	// ErrConflict is returned when a row was changed by someone else since it
	// was queried.
	ErrConflict = errors.New("someone else changed this row since it was loaded; run the query again to see the new values")
)

// editPlan holds what SaveEdits and PreviewEdits need about the table.
type editPlan struct {
	driver string
	target string
	types  map[string]string
	key    []string
}

func (p *editPlan) quote(s string) string { return QuoteIdent(p.driver, s) }

func newEditPlan(ctx context.Context, q Querier, driver string, req EditRequest) (*editPlan, error) {
	meta, err := tableColumns(ctx, q, driver, req.Schema, req.Table)
	if err != nil {
		return nil, err
	}
	if len(meta) == 0 {
		return nil, fmt.Errorf("table %s.%s not found", req.Schema, req.Table)
	}
	_, key, err := tableKey(ctx, q, driver, req.Schema, req.Table, meta)
	if err != nil {
		return nil, err
	}
	if len(key) == 0 || strings.Join(key, "\x00") != strings.Join(req.KeyColumns, "\x00") {
		return nil, errors.New("the table's key has changed; run the query again")
	}
	p := &editPlan{driver: driver, types: map[string]string{}, key: key}
	for _, m := range meta {
		p.types[m.name] = m.typ
	}
	p.target = p.quote(req.Schema) + "." + p.quote(req.Table)
	return p, nil
}

// sqlStmt is a statement with its arguments.
type sqlStmt struct {
	sql  string
	args []any
}

// builder assembles one statement, numbering PostgreSQL placeholders.
type builder struct {
	p    *editPlan
	args []any
}

// value returns the placeholder for v as a value of column col.
func (b *builder) value(col string, v *string) string {
	if v == nil {
		return "NULL"
	}
	b.args = append(b.args, *v)
	if b.p.driver == Postgres {
		// Send text and let the server convert it to the column type.
		return fmt.Sprintf("CAST($%d::text AS %s)", len(b.args), b.p.types[col])
	}
	return "?"
}

// equal returns a NULL-safe comparison of column col with v.
func (b *builder) equal(col string, v *string) string {
	q := b.p.quote(col)
	switch b.p.driver {
	case Postgres:
		if v == nil {
			return q + " IS NULL"
		}
		if t := strings.ToLower(b.p.types[col]); t == "json" || t == "xml" {
			// These types have no equality operator; compare the text.
			b.args = append(b.args, *v)
			return fmt.Sprintf("%s::text = $%d::text", q, len(b.args))
		}
		return q + " IS NOT DISTINCT FROM " + b.value(col, v)
	case MySQL:
		return q + " <=> " + b.value(col, v)
	default:
		return q + " IS " + b.value(col, v)
	}
}

func (b *builder) keyWhere(key []*string) (string, error) {
	var parts []string
	for i, col := range b.p.key {
		if i >= len(key) || key[i] == nil {
			return "", fmt.Errorf("key column %s is missing", col)
		}
		parts = append(parts, b.p.quote(col)+" = "+b.value(col, key[i]))
	}
	return strings.Join(parts, " AND "), nil
}

// unchanged adds conditions that the columns u changes still hold the values
// they had when queried. MySQL FLOAT/DOUBLE and JSON values don't compare
// exactly with their text, so those are skipped.
func (b *builder) unchanged(u RowUpdate) string {
	var s string
	for _, col := range sortedKeys(u.Changes) {
		orig, ok := u.Original[col]
		if !ok {
			continue
		}
		if b.p.driver == MySQL {
			t := strings.ToLower(b.p.types[col])
			if strings.Contains(t, "float") || strings.Contains(t, "double") || strings.Contains(t, "real") || t == "json" {
				continue
			}
		}
		s += " AND " + b.equal(col, orig)
	}
	return s
}

func sortedKeys(m map[string]*string) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// update returns the UPDATE for u, a count of the rows it should match (for
// MySQL, which reports changed rather than matched rows), and a count of rows
// with u's key (to tell a conflict from a deleted row).
func (p *editPlan) update(u RowUpdate) (stmt, check, keyOnly sqlStmt, err error) {
	b := &builder{p: p}
	var sets []string
	for _, col := range sortedKeys(u.Changes) {
		if _, ok := p.types[col]; !ok {
			return stmt, check, keyOnly, fmt.Errorf("the table has no column %s", col)
		}
		sets = append(sets, p.quote(col)+" = "+b.value(col, u.Changes[col]))
	}
	where, err := b.keyWhere(u.Key)
	if err != nil {
		return stmt, check, keyOnly, err
	}
	where += b.unchanged(u)
	stmt = sqlStmt{"UPDATE " + p.target + " SET " + strings.Join(sets, ", ") + " WHERE " + where, b.args}

	cb := &builder{p: p}
	cw, _ := cb.keyWhere(u.Key)
	cw += cb.unchanged(u)
	check = sqlStmt{"SELECT count(*) FROM " + p.target + " WHERE " + cw, cb.args}

	kb := &builder{p: p}
	kw, _ := kb.keyWhere(u.Key)
	keyOnly = sqlStmt{"SELECT count(*) FROM " + p.target + " WHERE " + kw, kb.args}
	return stmt, check, keyOnly, nil
}

func (p *editPlan) insert(ins RowInsert) (sqlStmt, error) {
	b := &builder{p: p}
	cols := sortedKeys(ins.Values)
	var names, vals []string
	for _, col := range cols {
		if _, ok := p.types[col]; !ok {
			return sqlStmt{}, fmt.Errorf("the table has no column %s", col)
		}
		names = append(names, p.quote(col))
		vals = append(vals, b.value(col, ins.Values[col]))
	}
	var s string
	switch {
	case len(cols) > 0:
		s = "INSERT INTO " + p.target + " (" + strings.Join(names, ", ") + ") VALUES (" + strings.Join(vals, ", ") + ")"
	case p.driver == MySQL:
		s = "INSERT INTO " + p.target + " () VALUES ()"
	default:
		s = "INSERT INTO " + p.target + " DEFAULT VALUES"
	}
	if p.driver != MySQL {
		var keys []string
		for _, k := range p.key {
			keys = append(keys, p.quote(k))
		}
		s += " RETURNING " + strings.Join(keys, ", ")
	}
	return sqlStmt{s, b.args}, nil
}

func (p *editPlan) delete(d RowDelete) (sqlStmt, error) {
	b := &builder{p: p}
	where, err := b.keyWhere(d.Key)
	if err != nil {
		return sqlStmt{}, err
	}
	return sqlStmt{"DELETE FROM " + p.target + " WHERE " + where, b.args}, nil
}

// PreviewEdits returns the statements SaveEdits would run for req, with the
// values written in, for showing to the user.
func PreviewEdits(ctx context.Context, db Querier, driver string, req EditRequest) ([]string, error) {
	p, err := newEditPlan(ctx, db, driver, req)
	if err != nil {
		return nil, err
	}
	out := []string{}
	for _, d := range req.Deletes {
		s, err := p.delete(d)
		if err != nil {
			return nil, err
		}
		out = append(out, inline(driver, s))
	}
	for _, u := range req.Updates {
		if len(u.Changes) == 0 {
			continue
		}
		s, _, _, err := p.update(u)
		if err != nil {
			return nil, err
		}
		out = append(out, inline(driver, s))
	}
	for _, ins := range req.Inserts {
		s, err := p.insert(ins)
		if err != nil {
			return nil, err
		}
		out = append(out, inline(driver, s))
	}
	return out, nil
}

// inline writes a statement's arguments into it as literals, for display.
func inline(driver string, s sqlStmt) string {
	lit := func(a any) string {
		v := strings.ReplaceAll(fmt.Sprint(a), "'", "''")
		if driver == MySQL {
			v = strings.ReplaceAll(v, `\`, `\\`)
		}
		return "'" + v + "'"
	}
	out := s.sql
	if driver == Postgres {
		for i := len(s.args); i >= 1; i-- {
			out = strings.ReplaceAll(out, "$"+strconv.Itoa(i)+"::text", lit(s.args[i-1]))
		}
		return out + ";"
	}
	var b strings.Builder
	n := 0
	for _, r := range out {
		if r == '?' && n < len(s.args) {
			b.WriteString(lit(s.args[n]))
			n++
			continue
		}
		b.WriteRune(r)
	}
	return b.String() + ";"
}

// SaveEdits applies req in one transaction: all changes are saved or none.
// Updates and deletes must match exactly one row, and an update fails when
// the columns it changes no longer hold the values they had when queried.
func SaveEdits(ctx context.Context, db *sql.DB, driver string, req EditRequest) (*SaveResult, error) {
	p, err := newEditPlan(ctx, db, driver, req)
	if err != nil {
		return nil, err
	}
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	res := &SaveResult{Updated: [][]*string{}, Inserted: [][]*string{}}
	for n, d := range req.Deletes {
		s, err := p.delete(d)
		if err != nil {
			return nil, fmt.Errorf("deleted row %d: %w", n+1, err)
		}
		r, err := tx.ExecContext(ctx, s.sql, s.args...)
		if err != nil {
			return nil, fmt.Errorf("deleted row %d: %w", n+1, err)
		}
		switch affected, _ := r.RowsAffected(); {
		case affected == 0:
			return nil, fmt.Errorf("deleted row %d: %w", n+1, ErrRowChanged)
		case affected > 1:
			return nil, fmt.Errorf("deleted row %d: the key matched %d rows, nothing was saved", n+1, affected)
		}
		res.Deleted++
	}

	var refetch [][]*string // key of each saved row: updates, then inserts
	for n, u := range req.Updates {
		if len(u.Changes) == 0 {
			refetch = append(refetch, u.Key)
			continue
		}
		s, check, keyOnly, err := p.update(u)
		if err != nil {
			return nil, fmt.Errorf("row %d: %w", n+1, err)
		}
		r, err := tx.ExecContext(ctx, s.sql, s.args...)
		if err != nil {
			return nil, fmt.Errorf("row %d: %w", n+1, err)
		}
		affected, err := r.RowsAffected()
		if err != nil {
			return nil, err
		}
		if affected == 0 && driver == MySQL {
			// MySQL counts changed rows, so 0 can mean "already had these values".
			if err := tx.QueryRowContext(ctx, check.sql, check.args...).Scan(&affected); err != nil {
				return nil, err
			}
		}
		switch {
		case affected == 0:
			var exists int64
			if err := tx.QueryRowContext(ctx, keyOnly.sql, keyOnly.args...).Scan(&exists); err != nil {
				return nil, err
			}
			if exists > 0 {
				return nil, fmt.Errorf("row %d: %w", n+1, ErrConflict)
			}
			return nil, fmt.Errorf("row %d: %w", n+1, ErrRowChanged)
		case affected > 1:
			return nil, fmt.Errorf("row %d: the key matched %d rows, nothing was saved", n+1, affected)
		}
		// Find the row by its new key if the key was edited.
		key := append([]*string(nil), u.Key...)
		for i, k := range p.key {
			if v, ok := u.Changes[k]; ok {
				key[i] = v
			}
		}
		refetch = append(refetch, key)
	}

	for n, ins := range req.Inserts {
		s, err := p.insert(ins)
		if err != nil {
			return nil, fmt.Errorf("new row %d: %w", n+1, err)
		}
		key := make([]*string, len(p.key))
		if driver == MySQL {
			r, err := tx.ExecContext(ctx, s.sql, s.args...)
			if err != nil {
				return nil, fmt.Errorf("new row %d: %w", n+1, err)
			}
			for i, k := range p.key {
				key[i] = ins.Values[k]
			}
			if len(p.key) == 1 && key[0] == nil {
				if id, err := r.LastInsertId(); err == nil && id != 0 {
					v := strconv.FormatInt(id, 10)
					key[0] = &v
				}
			}
		} else {
			rows, err := tx.QueryContext(ctx, s.sql, s.args...)
			if err != nil {
				return nil, fmt.Errorf("new row %d: %w", n+1, err)
			}
			_, vals, _, err := readRows(rows, 1)
			rows.Close()
			if err != nil {
				return nil, fmt.Errorf("new row %d: %w", n+1, err)
			}
			if len(vals) == 1 {
				key = vals[0]
			}
		}
		for i, k := range p.key {
			if key[i] == nil {
				return nil, fmt.Errorf("new row %d: fill in %s", n+1, k)
			}
		}
		refetch = append(refetch, key)
	}

	// Read the saved rows back, so defaults and triggers show up.
	var cols []string
	for _, c := range req.Columns {
		if c == "" {
			cols = append(cols, "NULL")
		} else {
			cols = append(cols, p.quote(c))
		}
	}
	for i, key := range refetch {
		var row []*string
		if len(cols) > 0 {
			b := &builder{p: p}
			where, err := b.keyWhere(key)
			if err != nil {
				return nil, err
			}
			rows, err := tx.QueryContext(ctx, "SELECT "+strings.Join(cols, ", ")+" FROM "+p.target+" WHERE "+where, b.args...)
			if err != nil {
				return nil, err
			}
			_, vals, _, err := readRows(rows, 1)
			rows.Close()
			if err != nil {
				return nil, err
			}
			if len(vals) == 1 {
				row = vals[0]
			}
		}
		if i < len(req.Updates) {
			res.Updated = append(res.Updated, row)
		} else {
			res.Inserted = append(res.Inserted, row)
		}
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	return res, nil
}

// ReferencedValues returns up to 50 values of ref's column starting with
// prefix, for picking a foreign key value.
func ReferencedValues(ctx context.Context, db Querier, driver string, ref ColumnRef, prefix string) ([]string, error) {
	col := QuoteIdent(driver, ref.Column)
	target := QuoteIdent(driver, ref.Schema) + "." + QuoteIdent(driver, ref.Table)
	var q string
	switch driver {
	case Postgres:
		q = "SELECT DISTINCT " + col + "::text FROM " + target + " WHERE " + col + "::text LIKE $1 ORDER BY 1 LIMIT 50"
	case MySQL:
		q = "SELECT DISTINCT CAST(" + col + " AS CHAR) FROM " + target + " WHERE CAST(" + col + " AS CHAR) LIKE ? ORDER BY 1 LIMIT 50"
	default:
		q = "SELECT DISTINCT CAST(" + col + " AS TEXT) FROM " + target + " WHERE CAST(" + col + " AS TEXT) LIKE ? ESCAPE '\\' ORDER BY 1 LIMIT 50"
	}
	like := strings.NewReplacer(`\`, `\\`, "%", `\%`, "_", `\_`).Replace(prefix) + "%"
	return queryStrings(ctx, db, q, like)
}
