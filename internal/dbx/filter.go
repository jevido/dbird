package dbx

import (
	"context"
	"strings"
)

// FilterColumn is a column a filter can refer to.
type FilterColumn struct {
	Name string
	Bool bool
}

// FilterColumns returns the columns of the table or view a simple SELECT
// reads, for normalizeFilter. It returns nil when they can't be found.
func FilterColumns(ctx context.Context, q Querier, driver, stmt string) []FilterColumn {
	sel, _ := parseSimpleSelect(stmt, driver)
	if sel == nil {
		return nil
	}
	schema, table := sel.schema, sel.table
	if driver == Postgres {
		if err := q.QueryRowContext(ctx, `SELECT n.nspname, c.relname FROM pg_catalog.pg_class c
			JOIN pg_catalog.pg_namespace n ON n.oid = c.relnamespace WHERE c.oid = to_regclass($1)`,
			sel.ref).Scan(&schema, &table); err != nil {
			return nil
		}
	} else if schema == "" {
		var err error
		if schema, err = DefaultSchema(ctx, q, driver); err != nil {
			return nil
		}
	}
	meta, err := tableColumns(ctx, q, driver, schema, table)
	if err != nil {
		return nil
	}
	cols := make([]FilterColumn, len(meta))
	for i, m := range meta {
		cols[i] = FilterColumn{Name: m.name, Bool: m.kind == "bool"}
	}
	return cols
}

// normalizeFilter makes a filter typed into the grid as forgiving on
// PostgreSQL as it is on MySQL and SQLite:
//
//   - a name that matches a column only when ignoring case gets the column's
//     exact, quoted name (accountId -> "accountId");
//   - a double-quoted word that isn't a column becomes a string ("jeff" ->
//     'jeff');
//   - 0 or 1 compared with true, false or a boolean column is read as a
//     boolean (1=false, active = 1).
//
// Everything else is left as typed. Other drivers get the filter unchanged.
func normalizeFilter(filter, driver string, cols []FilterColumn) string {
	if driver != Postgres {
		return filter
	}
	toks, ok := tokenize(filter, driver)
	if !ok {
		return filter
	}
	exact := map[string]bool{}
	boolCol := map[string]bool{}
	folded := map[string][]string{}
	for _, c := range cols {
		exact[c.Name] = true
		boolCol[c.Name] = c.Bool
		folded[strings.ToLower(c.Name)] = append(folded[strings.ToLower(c.Name)], c.Name)
	}
	// column returns the column name matches, if exactly one does.
	column := func(name string) (string, bool) {
		if exact[name] {
			return name, true
		}
		if m := folded[strings.ToLower(name)]; len(m) == 1 {
			return m[0], true
		}
		return "", false
	}
	// isBool reports whether t is true, false or a boolean column.
	isBool := func(t sqlToken) bool {
		if t.is("true") || t.is("false") {
			return true
		}
		name := t.text
		if t.kind == 'w' && exact[strings.ToLower(name)] {
			name = strings.ToLower(name)
		}
		c, ok := column(name)
		return (t.kind == 'w' || t.kind == 'q') && ok && boolCol[c]
	}
	isBit := func(t sqlToken) bool { return t.kind == 'n' && (t.text == "0" || t.text == "1") }
	// op returns the number of tokens of a comparison operator at i.
	op := func(i int) int {
		if i >= len(toks) || toks[i].kind != 'p' {
			return 0
		}
		switch toks[i].text {
		case "=":
			return 1
		case "<", "!":
			if i+1 < len(toks) && toks[i+1].punct(">") || i+1 < len(toks) && toks[i+1].punct("=") {
				return 2
			}
		}
		return 0
	}

	var b strings.Builder
	last := 0
	replace := func(t sqlToken, with string) {
		b.WriteString(filter[last:t.pos])
		b.WriteString(with)
		last = t.end
	}
	for i, t := range toks {
		prevDot := i > 0 && toks[i-1].punct(".")
		nextDot := i+1 < len(toks) && toks[i+1].punct(".")
		call := i+1 < len(toks) && toks[i+1].punct("(")
		switch {
		case t.kind == 'w' && !call && !nextDot:
			lower := strings.ToLower(t.text)
			if exact[lower] {
				continue // PostgreSQL folds it to this column already
			}
			if c, ok := column(t.text); ok && c != lower {
				replace(t, doubleQuote(c))
			}
		case t.kind == 'q' && filter[t.pos] == '"' && !call && !nextDot:
			if c, ok := column(t.text); ok {
				if c != t.text {
					replace(t, doubleQuote(c))
				}
			} else if !prevDot {
				replace(t, "'"+strings.ReplaceAll(t.text, "'", "''")+"'")
			}
		case isBit(t):
			if n := op(i + 1); n > 0 && i+1+n < len(toks) && isBool(toks[i+1+n]) ||
				i >= 2 && isBool(toks[i-2]) && op(i-1) == 1 ||
				i >= 3 && isBool(toks[i-3]) && op(i-2) == 2 {
				replace(t, map[string]string{"0": "false", "1": "true"}[t.text])
			}
		}
	}
	b.WriteString(filter[last:])
	return b.String()
}
