//go:build devdb

// Runs grid editing against the sample databases of `wails3 task dev:db:up`:
//
//	go test -tags devdb -run DevDB ./internal/dbx/
package dbx

import (
	"context"
	"strings"
	"testing"
	"time"

	"dbird/internal/store"
)

type devDB struct {
	t   *testing.T
	m   *Manager
	id  string
	drv string
}

func openDevDB(t *testing.T, c store.Connection) *devDB {
	m := NewManager()
	t.Cleanup(m.Close)
	if err := m.Connect(context.Background(), c); err != nil {
		t.Skipf("sample database not running (wails3 task dev:db:up): %v", err)
	}
	return &devDB{t, m, c.ID, c.Driver}
}

func (d *devDB) run(stmts ...string) []Result {
	d.t.Helper()
	res, err := d.m.Run(context.Background(), "tab", d.id, stmts, 100, false)
	if err != nil {
		d.t.Fatal(err)
	}
	for _, r := range res {
		if r.Error != "" {
			d.t.Fatalf("%s: %s", r.SQL, r.Error)
		}
	}
	return res
}

func (d *devDB) save(req EditRequest) (int, error) {
	db, driver, _ := d.m.DB(d.id)
	return SaveEdits(context.Background(), db, driver, req)
}

// editAndCheck edits every editable non-key column of the only row of query
// to the given values, saves, and checks the query now returns them.
func (d *devDB) editAndCheck(query string, values map[string]*string) {
	d.t.Helper()
	r := d.run(query)[0]
	ed := r.Editable
	if ed == nil {
		d.t.Fatalf("%s: read-only: %s", query, r.ReadOnly)
	}
	req := EditRequest{Schema: ed.Schema, Table: ed.Table}
	row := RowEdit{Changes: map[string]*string{}}
	for _, k := range ed.Key {
		req.KeyColumns = append(req.KeyColumns, ed.Columns[k])
		row.Key = append(row.Key, r.Rows[0][k])
	}
	for col, v := range values {
		row.Changes[col] = v
	}
	req.Rows = []RowEdit{row}
	if n, err := d.save(req); err != nil || n != 1 {
		d.t.Fatalf("%s: save = %d, %v", query, n, err)
	}
	after := d.run(query)[0]
	for i, c := range after.Columns {
		want, ok := values[c.Name]
		if !ok {
			continue
		}
		got := after.Rows[0][i]
		switch {
		case want == nil && got != nil:
			d.t.Errorf("%s: %s = %q, want NULL", query, c.Name, *got)
		case want != nil && (got == nil || *got != *want):
			d.t.Errorf("%s: %s = %v, want %q", query, c.Name, deref([]*string{got}), *want)
		}
	}
}

func TestDevDBPostgres(t *testing.T) {
	d := openDevDB(t, store.Connection{ID: "pg", Driver: Postgres, Host: "127.0.0.1", Port: 54320,
		User: "dbird", Password: "dbird", Database: "shop", SSLMode: "disable"})
	d.run(`drop schema if exists dbird_edit cascade`, `create schema dbird_edit`,
		`create type dbird_edit.mood as enum ('sad', 'ok', 'happy')`,
		`create table dbird_edit."Mixed Case" (
			id bigserial primary key, name text, n int, price numeric(10,2), ok boolean,
			at timestamptz, day date, doc jsonb, feel dbird_edit.mood, uid uuid, tags text[], raw bytea)`,
		`insert into dbird_edit."Mixed Case" (name, n, tags, raw) values ('ann', 1, '{a}', '\x00')`,
		`create table dbird_edit.pair (a int, b text, v text, primary key (a, b))`,
		`insert into dbird_edit.pair values (1, 'x', 'old')`)
	t.Cleanup(func() { d.run(`drop schema dbird_edit cascade`) })

	r := d.run(`select * from dbird_edit."Mixed Case"`)[0]
	if r.Editable == nil {
		t.Fatalf("read-only: %s", r.ReadOnly)
	}
	if got := strings.Join(r.Editable.Columns, ","); !strings.HasSuffix(got, ",uid,,") {
		t.Errorf("columns = %s (arrays and bytea should be read-only)", got)
	}
	d.editAndCheck(`select * from dbird_edit."Mixed Case"`, map[string]*string{
		"name": ptr("it's “quoted”"), "n": ptr("42"), "price": ptr("12.50"), "ok": ptr("true"),
		"day": ptr("2024-05-06"), "doc": ptr(`{"a": 1}`),
		"feel": ptr("happy"), "uid": ptr("6f1c6f3e-4d2b-4c1a-9a3e-2b7f3c9d8e01"),
	})
	d.editAndCheck(`select id, name from dbird_edit."Mixed Case"`, map[string]*string{"name": nil})

	// timestamptz is displayed in the local time zone; the saved instant must match.
	d.run(`update dbird_edit."Mixed Case" set at = null`)
	if n, err := d.save(EditRequest{Schema: "dbird_edit", Table: "Mixed Case", KeyColumns: []string{"id"},
		Rows: []RowEdit{{Key: []*string{ptr("1")}, Changes: map[string]*string{"at": ptr("2024-05-06 07:08:09Z")}}}}); err != nil || n != 1 {
		t.Fatalf("save timestamptz: %d, %v", n, err)
	}
	shown := *d.run(`select at from dbird_edit."Mixed Case"`)[0].Rows[0][0]
	got, err := time.Parse("2006-01-02 15:04:05.999999999Z07:00", shown)
	if err != nil || !got.Equal(time.Date(2024, 5, 6, 7, 8, 9, 0, time.UTC)) {
		t.Errorf("timestamptz shown as %q (%v)", shown, err)
	}
	// Saving the value exactly as the grid shows it keeps the instant.
	d.editAndCheck(`select id, at from dbird_edit."Mixed Case"`, map[string]*string{"at": &shown})

	// Unqualified names resolve through the session's search_path.
	d.run(`set search_path to dbird_edit`)
	d.editAndCheck(`select * from pair where a = 1`, map[string]*string{"v": ptr("new")})

	// A value the column can't take fails the save without changing anything.
	if _, err := d.save(EditRequest{Schema: "dbird_edit", Table: "Mixed Case", KeyColumns: []string{"id"},
		Rows: []RowEdit{{Key: []*string{ptr("1")}, Changes: map[string]*string{"n": ptr("not a number")}}}}); err == nil {
		t.Error("saving text into an int column succeeded")
	}
}

func TestDevDBMySQL(t *testing.T) {
	d := openDevDB(t, store.Connection{ID: "my", Driver: MySQL, Host: "127.0.0.1", Port: 33061,
		User: "dbird", Password: "dbird", Database: "shop"})
	d.run(`drop table if exists dbird_edit`, `drop table if exists dbird_pair`,
		"create table dbird_edit (id int auto_increment primary key, name varchar(50), price decimal(10,2), "+
			"at datetime, day date, doc json, feel enum('sad','ok','happy'), raw blob)",
		`insert into dbird_edit (name, raw) values ('ann', x'00')`,
		"create table dbird_pair (a int, b varchar(10), v text, primary key (a, b))",
		`insert into dbird_pair values (1, 'x', 'old')`)
	t.Cleanup(func() { d.run(`drop table dbird_edit`, `drop table dbird_pair`) })

	d.editAndCheck(`select * from dbird_edit`, map[string]*string{
		"name": ptr(`it's "quoted" \ back`), "price": ptr("12.50"), "at": ptr("2024-05-06 07:08:09"),
		"day": ptr("2024-05-06"), "doc": ptr(`{"a": 1}`), "feel": ptr("happy"),
	})
	// Saving a value the row already has: MySQL reports 0 rows changed.
	d.editAndCheck(`select * from dbird_edit`, map[string]*string{"feel": ptr("happy")})
	d.editAndCheck("select `a`, `b`, `v` from dbird_pair", map[string]*string{"v": ptr("new")})
}
