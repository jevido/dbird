package dbx

import (
	"testing"

	"dbird/internal/store"

	"github.com/jackc/pgx/v5/pgconn"
)

func TestSplitPassword(t *testing.T) {
	cases := []struct {
		driver, in, clean, pw string
	}{
		{Postgres, "postgres://me:s3cr%40t@db:5432/app?sslmode=disable", "postgres://me@db:5432/app?sslmode=disable", "s3cr@t"},
		{Postgres, "postgres://me@db/app", "postgres://me@db/app", ""},
		{Postgres, "host=db user=me password='it\\'s secret' dbname=app", "host=db user=me dbname=app", "it's secret"},
		{Postgres, "host=db password=plain", "host=db", "plain"},
		{Postgres, "host=db user=me", "host=db user=me", ""},
		{MySQL, "me:pw@tcp(db:3306)/app?parseTime=true", "me@tcp(db:3306)/app?parseTime=true", "pw"},
		{MySQL, "me@tcp(db:3306)/app", "me@tcp(db:3306)/app", ""},
		{SQLite, "file:/tmp/x.db", "file:/tmp/x.db", ""},
	}
	for _, c := range cases {
		clean, pw := SplitPassword(c.driver, c.in)
		if clean != c.clean || pw != c.pw {
			t.Errorf("SplitPassword(%q) = %q, %q; want %q, %q", c.in, clean, pw, c.clean, c.pw)
		}
	}
}

// A password split off and put back yields a DSN the driver reads the same
// password from.
func TestPasswordRoundTrip(t *testing.T) {
	for _, in := range []string{
		"postgres://me:p%20w@db:5432/app",
		"host=db user=me password='a b\\'c' dbname=app",
	} {
		clean, pw := SplitPassword(Postgres, in)
		cfg, err := pgconn.ParseConfig(withPassword(Postgres, clean, pw))
		if err != nil {
			t.Fatalf("%q: %v", in, err)
		}
		want, _ := pgconn.ParseConfig(in)
		if cfg.Password != want.Password || cfg.User != "me" || cfg.Database != "app" {
			t.Errorf("%q: got password %q user %q db %q", in, cfg.Password, cfg.User, cfg.Database)
		}
	}
	dsn, err := DSN(store.Connection{Driver: MySQL, URL: "me@tcp(db:3306)/app", Password: "p@ss:word"})
	if err != nil || dsn != "me:p@ss:word@tcp(db:3306)/app" {
		t.Errorf("mysql DSN = %q, %v", dsn, err)
	}
}
