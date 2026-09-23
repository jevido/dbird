package dbx

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
)

// Completion modes, stored per connection in store.Connection.Completion.
const (
	CompletionAuto    = ""        // preload small schemas, look up large ones
	CompletionPreload = "preload" // load every table and column up front
	CompletionLookup  = "lookup"  // query the database as the user types
	CompletionOff     = "off"     // keywords only
)

// PreloadTableLimit is the largest schema (in tables) that automatic mode
// loads up front.
const PreloadTableLimit = 5000

// TableCount counts the tables and views in schema without listing them.
func TableCount(ctx context.Context, db *sql.DB, driver, schema string) (int, error) {
	var (
		q    string
		args []any
	)
	switch driver {
	case Postgres:
		q = `SELECT count(*) FROM pg_catalog.pg_class c JOIN pg_catalog.pg_namespace n ON n.oid = c.relnamespace
			WHERE n.nspname = $1 AND c.relkind IN ('r','p','v','m','f')`
		args = []any{schema}
	case MySQL:
		q = `SELECT count(*) FROM information_schema.tables WHERE table_schema = ?`
		args = []any{schema}
	case SQLite:
		q = fmt.Sprintf(`SELECT count(*) FROM %s.sqlite_master WHERE type IN ('table','view') AND name NOT LIKE 'sqlite_%%'`, doubleQuote(schema))
	default:
		return 0, fmt.Errorf("unsupported driver %q", driver)
	}
	var n int
	err := db.QueryRowContext(ctx, q, args...).Scan(&n)
	return n, err
}

// likePrefix escapes s for use as a LIKE prefix pattern with '\' as escape character.
func likePrefix(s string) string {
	r := strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`)
	return r.Replace(s) + "%"
}

// TablesByPrefix returns up to limit table and view names in schema that
// start with prefix, sorted by name. It is meant to be cheap on schemas with
// hundreds of thousands of tables: PostgreSQL answers it from the index on
// pg_class names.
func TablesByPrefix(ctx context.Context, db *sql.DB, driver, schema, prefix string, limit int) ([]string, error) {
	var (
		q    string
		args []any
	)
	switch driver {
	case Postgres:
		// Unquoted identifiers are folded to lower case, and the name index
		// only helps a case-sensitive LIKE, so match on the lower-cased prefix.
		q = `SELECT c.relname FROM pg_catalog.pg_class c JOIN pg_catalog.pg_namespace n ON n.oid = c.relnamespace
			WHERE n.nspname = $1 AND c.relkind IN ('r','p','v','m','f') AND c.relname LIKE $2
			ORDER BY c.relname LIMIT $3`
		args = []any{schema, likePrefix(strings.ToLower(prefix)), limit}
	case MySQL:
		q = `SELECT table_name FROM information_schema.tables
			WHERE table_schema = ? AND table_name LIKE ? ORDER BY table_name LIMIT ?`
		args = []any{schema, likePrefix(prefix), limit}
	case SQLite:
		q = fmt.Sprintf(`SELECT name FROM %s.sqlite_master WHERE type IN ('table','view')
			AND name NOT LIKE 'sqlite_%%' AND name LIKE ? ESCAPE '\' ORDER BY name LIMIT ?`, doubleQuote(schema))
		args = []any{likePrefix(prefix), limit}
	default:
		return nil, fmt.Errorf("unsupported driver %q", driver)
	}
	return queryStrings(ctx, db, q, args...)
}

// likeContains escapes s for a LIKE "contains" pattern with '\' as escape.
func likeContains(s string) string {
	return "%" + likePrefix(s)
}

// TablesPage returns up to limit tables and views in schema whose name
// contains filter (case-insensitive; "" matches all), ordered by name and
// starting after the name after ("" for the first page). Paging on the name
// keeps every page as fast as the first, even with hundreds of thousands of
// tables.
func TablesPage(ctx context.Context, db *sql.DB, driver, schema, filter, after string, limit int) ([]TableInfo, error) {
	var (
		q    string
		args []any
	)
	switch driver {
	case Postgres:
		q = `SELECT c.relname, CASE WHEN c.relkind IN ('v','m') THEN 'view' ELSE 'table' END
			FROM pg_catalog.pg_class c JOIN pg_catalog.pg_namespace n ON n.oid = c.relnamespace
			WHERE n.nspname = $1 AND c.relkind IN ('r','p','v','m','f')
			  AND ($2 = '' OR c.relname ILIKE $3) AND c.relname > $4
			ORDER BY c.relname LIMIT $5`
		args = []any{schema, filter, likeContains(filter), after, limit}
	case MySQL:
		q = `SELECT table_name, CASE WHEN table_type = 'VIEW' THEN 'view' ELSE 'table' END
			FROM information_schema.tables
			WHERE table_schema = ? AND (? = '' OR table_name LIKE ?) AND table_name > ?
			ORDER BY table_name LIMIT ?`
		args = []any{schema, filter, likeContains(filter), after, limit}
	case SQLite:
		q = fmt.Sprintf(`SELECT name, type FROM %s.sqlite_master
			WHERE type IN ('table','view') AND name NOT LIKE 'sqlite_%%'
			  AND (? = '' OR name LIKE ? ESCAPE '\') AND name > ?
			ORDER BY name LIMIT ?`, doubleQuote(schema))
		args = []any{filter, likeContains(filter), after, limit}
	default:
		return nil, fmt.Errorf("unsupported driver %q", driver)
	}
	rows, err := db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []TableInfo{}
	for rows.Next() {
		var t TableInfo
		if err := rows.Scan(&t.Name, &t.Kind); err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

// TableCountMatching counts the tables and views in schema whose name
// contains filter ("" counts all).
func TableCountMatching(ctx context.Context, db *sql.DB, driver, schema, filter string) (int, error) {
	if filter == "" {
		return TableCount(ctx, db, driver, schema)
	}
	var (
		q    string
		args []any
	)
	switch driver {
	case Postgres:
		q = `SELECT count(*) FROM pg_catalog.pg_class c JOIN pg_catalog.pg_namespace n ON n.oid = c.relnamespace
			WHERE n.nspname = $1 AND c.relkind IN ('r','p','v','m','f') AND c.relname ILIKE $2`
		args = []any{schema, likeContains(filter)}
	case MySQL:
		q = `SELECT count(*) FROM information_schema.tables WHERE table_schema = ? AND table_name LIKE ?`
		args = []any{schema, likeContains(filter)}
	case SQLite:
		q = fmt.Sprintf(`SELECT count(*) FROM %s.sqlite_master WHERE type IN ('table','view')
			AND name NOT LIKE 'sqlite_%%' AND name LIKE ? ESCAPE '\'`, doubleQuote(schema))
		args = []any{likeContains(filter)}
	default:
		return 0, fmt.Errorf("unsupported driver %q", driver)
	}
	var n int
	err := db.QueryRowContext(ctx, q, args...).Scan(&n)
	return n, err
}
