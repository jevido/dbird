//go:build devdb

// Runs grid editing against the sample databases of `wails3 task dev:db:up`:
//
//	go test -tags devdb -run DevDB ./internal/dbx/
package dbx

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"dbird/internal/store"
)

type devDB struct {
	t  *testing.T
	m  *Manager
	id string
}

func openDevDB(t *testing.T, c store.Connection) *devDB {
	m := NewManager()
	t.Cleanup(m.Close)
	if err := m.Connect(context.Background(), c); err != nil {
		t.Skipf("sample database not running (wails3 task dev:db:up): %v", err)
	}
	return &devDB{t, m, c.ID}
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

func (d *devDB) save(req EditRequest) (*SaveResult, error) {
	db, driver, _ := d.m.DB(d.id)
	return SaveEdits(context.Background(), db, driver, req)
}

// request builds an EditRequest for the result of query.
func (d *devDB) request(query string) (EditRequest, Result) {
	d.t.Helper()
	r := d.run(query)[0]
	ed := r.Editable
	if ed == nil {
		d.t.Fatalf("%s: read-only: %s", query, r.ReadOnly)
	}
	req := EditRequest{Schema: ed.Schema, Table: ed.Table}
	for _, k := range ed.Key {
		req.KeyColumns = append(req.KeyColumns, ed.Columns[k].Name)
	}
	for _, c := range ed.Columns {
		req.Columns = append(req.Columns, c.Source)
	}
	return req, r
}

// editAndCheck edits the first row of query to values, saves, and checks the
// saved row as returned and as queried again.
func (d *devDB) editAndCheck(query string, values map[string]*string) {
	d.t.Helper()
	req, r := d.request(query)
	u := RowUpdate{Changes: values, Original: map[string]*string{}}
	for _, k := range r.Editable.Key {
		u.Key = append(u.Key, r.Rows[0][k])
	}
	for i, c := range r.Editable.Columns {
		if _, ok := values[c.Name]; ok {
			u.Original[c.Name] = r.Rows[0][i]
		}
	}
	req.Updates = []RowUpdate{u}
	res, err := d.save(req)
	if err != nil {
		d.t.Fatalf("%s: save: %v", query, err)
	}
	after := d.run(query)[0]
	for i, c := range after.Columns {
		want, ok := values[c.Name]
		if !ok {
			continue
		}
		for _, got := range []*string{after.Rows[0][i], res.Updated[0][i]} {
			switch {
			case want == nil && got != nil:
				d.t.Errorf("%s: %s = %q, want NULL", query, c.Name, *got)
			case want != nil && (got == nil || *got != *want):
				d.t.Errorf("%s: %s = %v, want %q", query, c.Name, deref([]*string{got}), *want)
			}
		}
	}
}

func TestDevDBPostgres(t *testing.T) {
	d := openDevDB(t, store.Connection{ID: "pg", Driver: Postgres, Host: "127.0.0.1", Port: 54320,
		User: "dbird", Password: "dbird", Database: "shop", SSLMode: "disable"})
	d.run(`drop schema if exists dbird_edit cascade`, `create schema dbird_edit`,
		`create type dbird_edit.mood as enum ('sad', 'ok', 'happy')`,
		`create table dbird_edit.team (id serial primary key, name text)`,
		`insert into dbird_edit.team (name) values ('red')`,
		`create table dbird_edit."Mixed Case" (
			id bigserial primary key, name text not null default 'x', n int, price numeric(10,2), ok boolean,
			at timestamptz, day date, doc jsonb, plain json, feel dbird_edit.mood, uid uuid,
			team_id int references dbird_edit.team(id), tags text[], raw bytea,
			twice int generated always as (n * 2) stored)`,
		`insert into dbird_edit."Mixed Case" (name, n, tags, raw, plain) values ('ann', 1, '{a}', '\x00', '{"k": 1}')`,
		`create table dbird_edit.pair (a int, b text, v text, primary key (a, b))`,
		`insert into dbird_edit.pair values (1, 'x', 'old')`,
		`create table dbird_edit.uniq (code text not null unique, v text)`,
		`insert into dbird_edit.uniq values ('k1', 'one')`)
	t.Cleanup(func() { d.run(`drop schema dbird_edit cascade`) })

	_, r := d.request(`select * from dbird_edit."Mixed Case"`)
	cols := map[string]EditColumn{}
	for i, c := range r.Editable.Columns {
		cols[r.Columns[i].Name] = c
	}
	if cols["tags"].Name != "" || cols["raw"].Name != "" || cols["twice"].Name != "" {
		t.Errorf("arrays, bytea and generated columns should be read-only: %+v %+v %+v", cols["tags"], cols["raw"], cols["twice"])
	}
	if c := cols["feel"]; c.Kind != "enum" || strings.Join(c.Enum, ",") != "sad,ok,happy" {
		t.Errorf("enum column = %+v", c)
	}
	if cols["ok"].Kind != "bool" || cols["at"].Kind != "datetime" || cols["doc"].Kind != "json" || cols["day"].Kind != "date" {
		t.Errorf("kinds: ok=%s at=%s doc=%s day=%s", cols["ok"].Kind, cols["at"].Kind, cols["doc"].Kind, cols["day"].Kind)
	}
	if ref := cols["team_id"].Ref; ref == nil || ref.Table != "team" || ref.Column != "id" || ref.Schema != "dbird_edit" {
		t.Errorf("team_id ref = %+v", ref)
	}
	if !cols["id"].HasDefault || !cols["name"].HasDefault || cols["n"].HasDefault {
		t.Error("defaults wrong")
	}

	d.editAndCheck(`select * from dbird_edit."Mixed Case"`, map[string]*string{
		"name": ptr("it's “quoted”"), "n": ptr("42"), "price": ptr("12.50"), "ok": ptr("true"),
		"day": ptr("2024-05-06"), "doc": ptr(`{"a": 1}`), "feel": ptr("happy"),
		"uid": ptr("6f1c6f3e-4d2b-4c1a-9a3e-2b7f3c9d8e01"), "team_id": ptr("1"),
	})
	// json has no equality operator; the conflict check compares its text.
	d.editAndCheck(`select id, plain from dbird_edit."Mixed Case"`, map[string]*string{"plain": ptr(`{"k": 2}`)})
	d.editAndCheck(`select id, name, n from dbird_edit."Mixed Case"`, map[string]*string{"n": nil})

	// timestamptz is shown in the local time zone; saving that text keeps the instant.
	local := time.Date(2024, 5, 6, 7, 8, 9, 0, time.UTC).Local().Format("2006-01-02 15:04:05.999999999Z07:00")
	d.editAndCheck(`select id, at from dbird_edit."Mixed Case"`, map[string]*string{"at": &local})
	shown := *d.run(`select at from dbird_edit."Mixed Case"`)[0].Rows[0][0]
	if got, err := time.Parse("2006-01-02 15:04:05.999999999Z07:00", shown); err != nil || !got.Equal(time.Date(2024, 5, 6, 7, 8, 9, 0, time.UTC)) {
		t.Errorf("timestamptz shown as %q", shown)
	}
	d.editAndCheck(`select id, at from dbird_edit."Mixed Case"`, map[string]*string{"at": &shown})

	// Insert with defaults and RETURNING, delete, in one save.
	req, _ := d.request(`select * from dbird_edit."Mixed Case"`)
	req.Inserts = []RowInsert{{Values: map[string]*string{"n": ptr("7")}}}
	res, err := d.save(req)
	if err != nil {
		t.Fatal(err)
	}
	got := deref(res.Inserted[0])
	if got[0] != "2" || got[1] != "x" || got[2] != "7" || got[len(got)-1] != "14" {
		t.Errorf("inserted row = %v (want id 2, default name, generated twice=14)", got)
	}
	req.Inserts = nil
	req.Deletes = []RowDelete{{Key: []*string{ptr("2")}}}
	if res, err := d.save(req); err != nil || res.Deleted != 1 {
		t.Fatalf("delete: %+v, %v", res, err)
	}

	// Concurrent change.
	req, r = d.request(`select id, name from dbird_edit."Mixed Case"`)
	d.run(`update dbird_edit."Mixed Case" set name = 'someone else'`)
	req.Updates = []RowUpdate{{Key: []*string{r.Rows[0][0]}, Changes: map[string]*string{"name": ptr("mine")},
		Original: map[string]*string{"name": r.Rows[0][1]}}}
	if _, err := d.save(req); !errors.Is(err, ErrConflict) {
		t.Errorf("conflict: err = %v", err)
	}

	// Unqualified names resolve through the session's search_path.
	d.run(`set search_path to dbird_edit`)
	d.editAndCheck(`select * from pair where a = 1`, map[string]*string{"v": ptr("new")})
	d.editAndCheck(`select * from uniq`, map[string]*string{"v": ptr("uno")})
	if _, r := d.request(`select * from uniq`); r.Editable.KeyName != "unique key uniq_code_key" {
		t.Errorf("uniq key = %s", r.Editable.KeyName)
	}

	// A value the column can't take fails the save without changing anything.
	req, r = d.request(`select id, n from "Mixed Case"`)
	req.Updates = []RowUpdate{{Key: []*string{r.Rows[0][0]}, Changes: map[string]*string{"n": ptr("not a number")}}}
	if _, err := d.save(req); err == nil {
		t.Error("saving text into an int column succeeded")
	}

	db, driver, _ := d.m.DB(d.id)
	vals, err := ReferencedValues(context.Background(), db, driver, ColumnRef{Schema: "dbird_edit", Table: "team", Column: "id"}, "")
	if err != nil || strings.Join(vals, ",") != "1" {
		t.Errorf("ReferencedValues = %v, %v", vals, err)
	}
	q, _ := BrowseSQL(`select * from team limit 10`, driver, BrowseOptions{Filter: "name = 'red'", OrderBy: 1, Desc: true}, nil)
	if r := d.run(q)[0]; len(r.Rows) != 1 || r.Editable == nil {
		t.Errorf("browse query %q: %d rows, editable %v", q, len(r.Rows), r.Editable != nil)
	}
}

func TestDevDBPostgresFilter(t *testing.T) {
	d := openDevDB(t, store.Connection{ID: "pg", Driver: Postgres, Host: "127.0.0.1", Port: 54320,
		User: "dbird", Password: "dbird", Database: "shop", SSLMode: "disable"})
	d.run(`drop table if exists dbird_account cascade`,
		`create table dbird_account (id serial primary key, "accountId" text, active boolean)`,
		`insert into dbird_account ("accountId", active) values ('jeff', true), ('ann', false), ('jeff', false)`,
		`create view dbird_account_v as select * from dbird_account`)
	t.Cleanup(func() { d.run(`drop table dbird_account cascade`) })
	db, driver, _ := d.m.DB(d.id)
	for _, base := range []string{`SELECT * FROM dbird_account`, `SELECT * FROM dbird_account_v`} {
		cols := FilterColumns(context.Background(), db, driver, base)
		for filter, want := range map[string]int{
			`accountId = "jeff"`:                 2,
			`1=false`:                            0,
			`1=true`:                             3,
			`accountid = 'ann' or active = true`: 2,
			`active = 1`:                         1,
		} {
			q, err := BrowseSQL(base, driver, BrowseOptions{Filter: filter}, cols)
			if err != nil {
				t.Fatal(err)
			}
			res, _ := d.m.Run(context.Background(), "tab", d.id, []string{q}, 100, false)
			if r := res[0]; r.Error != "" || len(r.Rows) != want {
				t.Errorf("%s WHERE %s: %d rows, error %q (sql %q)", base, filter, len(r.Rows), r.Error, q)
			}
		}
	}
}

func TestDevDBMySQL(t *testing.T) {
	d := openDevDB(t, store.Connection{ID: "my", Driver: MySQL, Host: "127.0.0.1", Port: 33061,
		User: "dbird", Password: "dbird", Database: "shop"})
	d.run(`drop table if exists dbird_edit`, `drop table if exists dbird_pair`, `drop table if exists dbird_uniq`, `drop table if exists dbird_team`,
		"create table dbird_team (id int primary key)", "insert into dbird_team values (1), (2)",
		"create table dbird_edit (id int auto_increment primary key, name varchar(50) not null default 'x', price decimal(10,2), "+
			"ratio float, at datetime, day date, doc json, feel enum('sad','ok','it''s'), active tinyint(1), raw blob, "+
			"team_id int, foreign key (team_id) references dbird_team(id))",
		`insert into dbird_edit (name, raw, ratio) values ('ann', x'00', 0.1)`,
		"create table dbird_pair (a int, b varchar(10), v text, primary key (a, b))",
		`insert into dbird_pair values (1, 'x', 'old')`,
		"create table dbird_uniq (code varchar(10) not null, v text, unique key by_code (code))",
		`insert into dbird_uniq values ('k1', 'one')`)
	t.Cleanup(func() {
		d.run(`drop table dbird_edit`, `drop table dbird_pair`, `drop table dbird_uniq`, `drop table dbird_team`)
	})

	_, r := d.request(`select * from dbird_edit`)
	cols := map[string]EditColumn{}
	for i, c := range r.Editable.Columns {
		cols[r.Columns[i].Name] = c
	}
	if c := cols["feel"]; c.Kind != "enum" || strings.Join(c.Enum, "|") != "sad|ok|it's" {
		t.Errorf("enum = %+v", c)
	}
	if cols["active"].Kind != "bool" || cols["raw"].Name != "" || cols["team_id"].Ref == nil {
		t.Errorf("active=%+v raw=%+v team=%+v", cols["active"], cols["raw"], cols["team_id"])
	}

	d.editAndCheck(`select * from dbird_edit`, map[string]*string{
		"name": ptr(`it's "quoted" \ back`), "price": ptr("12.50"), "at": ptr("2024-05-06 07:08:09"),
		"day": ptr("2024-05-06"), "doc": ptr(`{"a": 1}`), "feel": ptr("it's"), "active": ptr("1"), "team_id": ptr("2"),
	})
	// Unchanged value: MySQL reports 0 rows changed. FLOAT is left out of the
	// conflict check since 0.1 doesn't compare exactly.
	d.editAndCheck(`select * from dbird_edit`, map[string]*string{"feel": ptr("it's"), "ratio": ptr("0.5")})
	d.editAndCheck("select `a`, `b`, `v` from dbird_pair", map[string]*string{"v": ptr("new")})
	d.editAndCheck(`select * from dbird_uniq`, map[string]*string{"v": ptr("uno")})

	req, _ := d.request(`select * from dbird_edit`)
	req.Inserts = []RowInsert{{Values: map[string]*string{"price": ptr("1.00")}}}
	res, err := d.save(req)
	if err != nil {
		t.Fatal(err)
	}
	if got := deref(res.Inserted[0]); got[0] != "2" || got[1] != "x" || got[2] != "1.00" {
		t.Errorf("inserted = %v", got)
	}

	req, r = d.request(`select id, name from dbird_edit where id = 1`)
	d.run(`update dbird_edit set name = 'someone else' where id = 1`)
	req.Updates = []RowUpdate{{Key: []*string{r.Rows[0][0]}, Changes: map[string]*string{"name": ptr("mine")},
		Original: map[string]*string{"name": r.Rows[0][1]}}}
	if _, err := d.save(req); !errors.Is(err, ErrConflict) {
		t.Errorf("conflict: err = %v", err)
	}
	req.Updates = nil
	req.Deletes = []RowDelete{{Key: []*string{ptr("2")}}}
	if res, err := d.save(req); err != nil || res.Deleted != 1 {
		t.Errorf("delete: %+v, %v", res, err)
	}
}
