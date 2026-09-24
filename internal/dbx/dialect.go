// Package dbx opens database connections for the supported drivers and runs
// queries and metadata lookups against them. It has no Wails dependencies.
package dbx

import (
	"database/sql"
	"fmt"
	"net"
	"net/url"
	"strconv"
	"strings"

	"dbird/internal/store"

	"github.com/go-sql-driver/mysql"
	_ "github.com/jackc/pgx/v5/stdlib"
	_ "modernc.org/sqlite"
)

// Driver names as stored in store.Connection.Driver.
const (
	Postgres = "postgres"
	MySQL    = "mysql"
	SQLite   = "sqlite"
)

// dialect captures per-driver differences.
type dialect struct {
	sqlDriver string
	quote     func(string) string
}

func doubleQuote(s string) string { return `"` + strings.ReplaceAll(s, `"`, `""`) + `"` }
func backQuote(s string) string   { return "`" + strings.ReplaceAll(s, "`", "``") + "`" }

var dialects = map[string]dialect{
	Postgres: {sqlDriver: "pgx", quote: doubleQuote},
	MySQL:    {sqlDriver: "mysql", quote: backQuote},
	SQLite:   {sqlDriver: "sqlite", quote: doubleQuote},
}

func dialectFor(driver string) (dialect, error) {
	d, ok := dialects[driver]
	if !ok {
		return dialect{}, fmt.Errorf("unsupported driver %q", driver)
	}
	return d, nil
}

// DSN builds the driver-specific data source name for c.
func DSN(c store.Connection) (string, error) {
	if c.URL != "" {
		if c.Password != "" {
			return withPassword(c.Driver, c.URL, c.Password), nil
		}
		return c.URL, nil
	}
	switch c.Driver {
	case Postgres:
		host := c.Host
		if host == "" {
			host = "localhost"
		}
		port := c.Port
		if port == 0 {
			port = 5432
		}
		u := url.URL{
			Scheme: "postgres",
			Host:   net.JoinHostPort(host, strconv.Itoa(port)),
			Path:   "/" + c.Database,
		}
		if c.User != "" {
			if c.Password != "" {
				u.User = url.UserPassword(c.User, c.Password)
			} else {
				u.User = url.User(c.User)
			}
		}
		q := url.Values{}
		sslMode := c.SSLMode
		if sslMode == "" {
			sslMode = "prefer"
		}
		q.Set("sslmode", sslMode)
		q.Set("application_name", "dbird")
		u.RawQuery = q.Encode()
		return u.String(), nil
	case MySQL:
		host := c.Host
		if host == "" {
			host = "localhost"
		}
		port := c.Port
		if port == 0 {
			port = 3306
		}
		cfg := mysql.NewConfig()
		cfg.User = c.User
		cfg.Passwd = c.Password
		cfg.Net = "tcp"
		cfg.Addr = net.JoinHostPort(host, strconv.Itoa(port))
		cfg.DBName = c.Database
		cfg.AllowNativePasswords = true
		return cfg.FormatDSN(), nil
	case SQLite:
		if c.Database == "" {
			return "", fmt.Errorf("sqlite connection needs a database file path")
		}
		// SQLite decodes %HH in file: URIs, so escape the characters that
		// would otherwise start the query string or fragment.
		path := strings.NewReplacer("%", "%25", "?", "%3f", "#", "%23").Replace(c.Database)
		return "file:" + path + "?_pragma=foreign_keys(1)&_pragma=busy_timeout(5000)", nil
	default:
		return "", fmt.Errorf("unsupported driver %q", c.Driver)
	}
}

// Open opens (but does not ping) a connection pool for c.
func Open(c store.Connection) (*sql.DB, error) {
	d, err := dialectFor(c.Driver)
	if err != nil {
		return nil, err
	}
	dsn, err := DSN(c)
	if err != nil {
		return nil, err
	}
	db, err := sql.Open(d.sqlDriver, dsn)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(16)
	db.SetMaxIdleConns(4)
	return db, nil
}

// QuoteIdent quotes an identifier for the given driver.
func QuoteIdent(driver, name string) string {
	d, err := dialectFor(driver)
	if err != nil {
		return doubleQuote(name)
	}
	return d.quote(name)
}
