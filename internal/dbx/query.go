package dbx

import (
	"context"
	"database/sql"
	"encoding/hex"
	"fmt"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

// Column describes one result column.
type Column struct {
	Name string `json:"name"`
	Type string `json:"type"`
}

// Result is the outcome of executing one statement.
type Result struct {
	SQL          string      `json:"sql"`
	Columns      []Column    `json:"columns"`
	Rows         [][]*string `json:"rows"`
	RowsAffected int64       `json:"rowsAffected"`
	// HasResultSet is true when the statement produced rows (even zero rows).
	HasResultSet bool    `json:"hasResultSet"`
	Truncated    bool    `json:"truncated"`
	DurationMs   float64 `json:"durationMs"`
	Error        string  `json:"error"`
	// Editable is set when the rows can be edited in the grid; otherwise
	// ReadOnly says why not.
	Editable *Editable `json:"editable"`
	ReadOnly string    `json:"readOnly"`
	// Browse is set for a SELECT from one table (or view), which can be
	// filtered, sorted and paged on the server (see BrowseSQL).
	Browse *Browsable `json:"browse"`
}

// Querier is satisfied by *sql.DB, *sql.Conn and *sql.Tx.
type Querier interface {
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
}

// returnsRows guesses whether a statement yields a result set. Statements that
// don't are run with Exec so the affected row count is available.
func returnsRows(stmt string) bool {
	kw := strings.ToUpper(firstKeyword(stmt))
	switch kw {
	case "SELECT", "WITH", "SHOW", "EXPLAIN", "DESCRIBE", "DESC", "PRAGMA",
		"VALUES", "TABLE", "CALL", "FETCH", "ANALYZE":
		return true
	}
	return strings.Contains(strings.ToUpper(stmt), "RETURNING")
}

// firstKeyword returns the first word of stmt, skipping whitespace, comments
// and opening parentheses.
func firstKeyword(stmt string) string {
	s := stmt
	for {
		s = strings.TrimLeft(s, " \t\r\n(")
		switch {
		case strings.HasPrefix(s, "--"), strings.HasPrefix(s, "#"):
			i := strings.IndexByte(s, '\n')
			if i < 0 {
				return ""
			}
			s = s[i+1:]
		case strings.HasPrefix(s, "/*"):
			i := strings.Index(s, "*/")
			if i < 0 {
				return ""
			}
			s = s[i+2:]
		default:
			end := strings.IndexFunc(s, func(r rune) bool {
				return !(r == '_' || r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z')
			})
			if end < 0 {
				return s
			}
			return s[:end]
		}
	}
}

// Execute runs one statement and collects at most maxRows rows. Errors are
// reported in Result.Error rather than returned so a script can keep going.
func Execute(ctx context.Context, q Querier, stmt string, maxRows int) Result {
	res := Result{SQL: stmt}
	start := time.Now()

	if !returnsRows(stmt) {
		r, err := q.ExecContext(ctx, stmt)
		if err != nil {
			res.Error = err.Error()
			res.DurationMs = ms(start)
			return res
		}
		if n, err := r.RowsAffected(); err == nil {
			res.RowsAffected = n
		}
		res.DurationMs = ms(start)
		return res
	}

	rows, err := q.QueryContext(ctx, stmt)
	if err != nil {
		res.Error = err.Error()
		res.DurationMs = ms(start)
		return res
	}
	defer rows.Close()

	res.Columns, res.Rows, res.Truncated, err = readRows(rows, maxRows)
	if err != nil {
		res.Error = err.Error()
	}
	res.HasResultSet = len(res.Columns) > 0
	res.DurationMs = ms(start)
	return res
}

// readRows reads at most maxRows rows (0 = all) as display text. truncated
// is set when more rows were available.
func readRows(rows *sql.Rows, maxRows int) (cols []Column, out [][]*string, truncated bool, err error) {
	types, err := rows.ColumnTypes()
	if err != nil {
		return nil, nil, false, err
	}
	cols = make([]Column, len(types))
	for i, c := range types {
		t := strings.ToLower(c.DatabaseTypeName())
		if strings.HasPrefix(t, "_") { // PostgreSQL array types, e.g. _int4
			t = t[1:] + "[]"
		}
		cols[i] = Column{Name: c.Name(), Type: t}
	}
	out = [][]*string{}
	vals := make([]any, len(cols))
	ptrs := make([]any, len(cols))
	for i := range vals {
		ptrs[i] = &vals[i]
	}
	for rows.Next() {
		if maxRows > 0 && len(out) >= maxRows {
			truncated = true
			break
		}
		if err := rows.Scan(ptrs...); err != nil {
			return cols, out, truncated, err
		}
		row := make([]*string, len(cols))
		for i, v := range vals {
			row[i] = format(v, cols[i].Type)
		}
		out = append(out, row)
	}
	return cols, out, truncated, rows.Err()
}

func ms(start time.Time) float64 {
	return float64(time.Since(start).Microseconds()) / 1000
}

// format renders a scanned value as display text. NULL becomes nil.
func format(v any, dbType string) *string {
	var s string
	switch x := v.(type) {
	case nil:
		return nil
	case []byte:
		if utf8.Valid(x) && !isBinaryType(dbType) {
			s = string(x)
		} else {
			s = "0x" + hex.EncodeToString(x)
		}
	case string:
		s = x
	case time.Time:
		switch dbType {
		case "date":
			s = x.Format("2006-01-02")
		default:
			s = x.Format("2006-01-02 15:04:05.999999999Z07:00")
		}
	case bool:
		s = strconv.FormatBool(x)
	case int64:
		s = strconv.FormatInt(x, 10)
	case float64:
		s = strconv.FormatFloat(x, 'g', -1, 64)
	case float32:
		s = strconv.FormatFloat(float64(x), 'g', -1, 32)
	case [16]byte: // pgx uuid
		s = fmt.Sprintf("%x-%x-%x-%x-%x", x[0:4], x[4:6], x[6:8], x[8:10], x[10:16])
	case fmt.Stringer:
		s = x.String()
	default:
		s = fmt.Sprint(x)
	}
	return &s
}

func isBinaryType(t string) bool {
	switch t {
	case "bytea", "blob", "binary", "varbinary", "tinyblob", "mediumblob", "longblob":
		return true
	}
	return false
}
