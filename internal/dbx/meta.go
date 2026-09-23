package dbx

import (
	"context"
	"database/sql"
	"fmt"
)

// TableInfo is a table or view in a schema.
type TableInfo struct {
	Name string `json:"name"`
	Kind string `json:"kind"` // table or view
}

// ColumnInfo is a column of a table.
type ColumnInfo struct {
	Name       string `json:"name"`
	Type       string `json:"type"`
	Nullable   bool   `json:"nullable"`
	PrimaryKey bool   `json:"primaryKey"`
}

// Schemas lists the schemas (databases for MySQL) visible on the connection.
func Schemas(ctx context.Context, db *sql.DB, driver string) ([]string, error) {
	var q string
	switch driver {
	case Postgres:
		q = `SELECT nspname FROM pg_catalog.pg_namespace
			WHERE nspname NOT LIKE 'pg_toast%' AND nspname NOT LIKE 'pg_temp_%'
			ORDER BY CASE WHEN nspname IN ('pg_catalog','information_schema') THEN 1 ELSE 0 END, nspname`
	case MySQL:
		q = `SELECT schema_name FROM information_schema.schemata
			ORDER BY CASE WHEN schema_name IN ('mysql','information_schema','performance_schema','sys') THEN 1 ELSE 0 END, schema_name`
	case SQLite:
		q = `SELECT name FROM pragma_database_list ORDER BY seq`
	default:
		return nil, fmt.Errorf("unsupported driver %q", driver)
	}
	return queryStrings(ctx, db, q)
}

// DefaultSchema returns the schema unqualified names resolve to.
func DefaultSchema(ctx context.Context, db *sql.DB, driver string) (string, error) {
	var q string
	switch driver {
	case Postgres:
		q = `SELECT current_schema()`
	case MySQL:
		q = `SELECT COALESCE(DATABASE(), '')`
	case SQLite:
		return "main", nil
	default:
		return "", fmt.Errorf("unsupported driver %q", driver)
	}
	var s sql.NullString
	if err := db.QueryRowContext(ctx, q).Scan(&s); err != nil {
		return "", err
	}
	return s.String, nil
}

// Tables lists tables and views in schema.
func Tables(ctx context.Context, db *sql.DB, driver, schema string) ([]TableInfo, error) {
	var (
		q    string
		args []any
	)
	switch driver {
	case Postgres:
		q = `SELECT c.relname, CASE WHEN c.relkind IN ('v','m') THEN 'view' ELSE 'table' END
			FROM pg_catalog.pg_class c JOIN pg_catalog.pg_namespace n ON n.oid = c.relnamespace
			WHERE n.nspname = $1 AND c.relkind IN ('r','p','v','m','f')
			ORDER BY c.relname`
		args = []any{schema}
	case MySQL:
		q = `SELECT table_name, CASE WHEN table_type = 'VIEW' THEN 'view' ELSE 'table' END
			FROM information_schema.tables WHERE table_schema = ? ORDER BY table_name`
		args = []any{schema}
	case SQLite:
		q = fmt.Sprintf(`SELECT name, type FROM %s.sqlite_master
			WHERE type IN ('table','view') AND name NOT LIKE 'sqlite_%%' ORDER BY name`, doubleQuote(schema))
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

// Columns lists the columns of schema.table.
func Columns(ctx context.Context, db *sql.DB, driver, schema, table string) ([]ColumnInfo, error) {
	var (
		q    string
		args []any
	)
	switch driver {
	case Postgres:
		q = `SELECT a.attname,
				pg_catalog.format_type(a.atttypid, a.atttypmod),
				NOT a.attnotnull,
				COALESCE((SELECT true FROM pg_catalog.pg_index i
					WHERE i.indrelid = c.oid AND i.indisprimary AND a.attnum = ANY(i.indkey)), false)
			FROM pg_catalog.pg_attribute a
			JOIN pg_catalog.pg_class c ON c.oid = a.attrelid
			JOIN pg_catalog.pg_namespace n ON n.oid = c.relnamespace
			WHERE n.nspname = $1 AND c.relname = $2 AND a.attnum > 0 AND NOT a.attisdropped
			ORDER BY a.attnum`
		args = []any{schema, table}
	case MySQL:
		q = `SELECT column_name, column_type, is_nullable = 'YES', column_key = 'PRI'
			FROM information_schema.columns WHERE table_schema = ? AND table_name = ?
			ORDER BY ordinal_position`
		args = []any{schema, table}
	case SQLite:
		q = `SELECT name, type, "notnull" = 0, pk > 0 FROM pragma_table_info(?, ?) ORDER BY cid`
		args = []any{table, schema}
	default:
		return nil, fmt.Errorf("unsupported driver %q", driver)
	}
	rows, err := db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []ColumnInfo{}
	for rows.Next() {
		var c ColumnInfo
		if err := rows.Scan(&c.Name, &c.Type, &c.Nullable, &c.PrimaryKey); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}

// SchemaColumns returns every table in schema mapped to its column names, for
// editor autocompletion.
func SchemaColumns(ctx context.Context, db *sql.DB, driver, schema string) (map[string][]string, error) {
	out := map[string][]string{}
	switch driver {
	case Postgres, MySQL:
		q := `SELECT table_name, column_name FROM information_schema.columns
			WHERE table_schema = ? ORDER BY table_name, ordinal_position`
		if driver == Postgres {
			q = `SELECT table_name, column_name FROM information_schema.columns
				WHERE table_schema = $1 ORDER BY table_name, ordinal_position`
		}
		rows, err := db.QueryContext(ctx, q, schema)
		if err != nil {
			return nil, err
		}
		defer rows.Close()
		for rows.Next() {
			var t, c string
			if err := rows.Scan(&t, &c); err != nil {
				return nil, err
			}
			out[t] = append(out[t], c)
		}
		return out, rows.Err()
	case SQLite:
		tables, err := Tables(ctx, db, driver, schema)
		if err != nil {
			return nil, err
		}
		for _, t := range tables {
			cols, err := Columns(ctx, db, driver, schema, t.Name)
			if err != nil {
				return nil, err
			}
			names := make([]string, len(cols))
			for i, c := range cols {
				names[i] = c.Name
			}
			out[t.Name] = names
		}
		return out, nil
	default:
		return nil, fmt.Errorf("unsupported driver %q", driver)
	}
}

func queryStrings(ctx context.Context, db *sql.DB, q string, args ...any) ([]string, error) {
	rows, err := db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []string{}
	for rows.Next() {
		var s string
		if err := rows.Scan(&s); err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, rows.Err()
}
