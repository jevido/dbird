package dbx

import (
	"context"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"dbird/internal/store"
)

func TestParseSimpleSelect(t *testing.T) {
	ok := []struct{ sql, schema, table string }{
		{"select * from account", "", "account"},
		{"SELECT id, name FROM public.account WHERE id > 3 ORDER BY name LIMIT 10;", "public", "account"},
		{"select a.* from account a where a.id in (select id from other) for update", "", "account"},
		{`select "Id", n.name as label from "My Schema"."Acc" n`, "My Schema", "Acc"},
		{"select id, lower(name) from account -- comment\n where id = 1", "", "account"},
		{"select id, data #> '{a}' as x from account", "", "account"},
		{"select * from account order by id limit 5 offset 10", "", "account"},
	}
	for _, c := range ok {
		sel, reason := parseSimpleSelect(c.sql, Postgres)
		if sel == nil || sel.schema != c.schema || sel.table != c.table {
			t.Errorf("%q: got %+v (%s), want %s.%s", c.sql, sel, reason, c.schema, c.table)
		}
	}
	bad := []string{
		"select * from a join b on a.id = b.id",
		"select * from a, b",
		"select count(*) from a group by x",
		"select distinct name from a",
		"select * from a union select * from b",
		"select 1",
		"with x as (select 1) select * from x",
		"update a set x = 1",
		"select * from generate_series(1, 3)",
		"select * from a; select * from b",
		"select * from a left join b using (id)",
		"select * from a limit 1 where x = 1",
	}
	for _, q := range bad {
		if sel, _ := parseSimpleSelect(q, Postgres); sel != nil {
			t.Errorf("%q: parsed as editable %+v", q, sel)
		}
	}
	if sel, _ := parseSimpleSelect("select id # the key\nfrom account", MySQL); sel == nil {
		t.Error("mysql # comment not skipped")
	}
}

func TestBrowseSQL(t *testing.T) {
	cases := []struct {
		in   string
		opt  BrowseOptions
		want string
	}{
		{"SELECT * FROM account\nLIMIT 200;", BrowseOptions{}, "SELECT * FROM account\nLIMIT 200"},
		{"SELECT * FROM account a WHERE a.id > 3 ORDER BY name LIMIT 200", BrowseOptions{Filter: "name LIKE 'A%';", OrderBy: 2, Desc: true},
			"SELECT * FROM account a\nWHERE (a.id > 3) AND (name LIKE 'A%')\nORDER BY 2 DESC\nLIMIT 200"},
		{"select * from account limit 200", BrowseOptions{Limit: 200, Offset: 400}, "select * from account\nLIMIT 200 OFFSET 400"},
		{"select * from account where x in (select 1) order by id for update", BrowseOptions{Filter: "id = 1"},
			"select * from account\nWHERE (x in (select 1)) AND (id = 1)\nORDER BY id\nFOR update"},
	}
	for _, c := range cases {
		got, err := BrowseSQL(c.in, Postgres, c.opt)
		if err != nil || got != c.want {
			t.Errorf("BrowseSQL(%q, %+v) =\n%s\n(%v), want\n%s", c.in, c.opt, got, err, c.want)
		}
	}
	if b := browsable("select * from account limit 200", Postgres); b == nil || b.Limit != 200 {
		t.Errorf("browsable = %+v", b)
	}
	if _, err := BrowseSQL("select * from a join b on true", Postgres, BrowseOptions{}); err == nil {
		t.Error("BrowseSQL accepted a join")
	}
}

type sqliteFixture struct {
	t *testing.T
	m *Manager
}

func newSQLite(t *testing.T) *sqliteFixture {
	ctx := context.Background()
	c := store.Connection{ID: "c", Driver: SQLite, Database: filepath.Join(t.TempDir(), "t.db")}
	m := NewManager()
	t.Cleanup(m.Close)
	if err := m.Connect(ctx, c); err != nil {
		t.Fatal(err)
	}
	return &sqliteFixture{t, m}
}

func (f *sqliteFixture) run(stmts ...string) []Result {
	f.t.Helper()
	res, err := f.m.Run(context.Background(), "tab", "c", stmts, 100, false)
	if err != nil {
		f.t.Fatal(err)
	}
	for _, r := range res {
		if r.Error != "" {
			f.t.Fatalf("%s: %s", r.SQL, r.Error)
		}
	}
	return res
}

func (f *sqliteFixture) save(req EditRequest) (*SaveResult, error) {
	db, driver, _ := f.m.DB("c")
	return SaveEdits(context.Background(), db, driver, req)
}

func (f *sqliteFixture) rows(q string) [][]string {
	f.t.Helper()
	var out [][]string
	for _, r := range f.run(q)[0].Rows {
		out = append(out, deref(r))
	}
	return out
}

func TestEditInfoSQLite(t *testing.T) {
	f := newSQLite(t)
	f.run(`create table team (id integer primary key, name text)`,
		`create table account (id integer primary key, name text not null, active boolean,
			born date, score real default 0, raw blob, team_id int references team(id))`,
		`create table nokey (x int)`,
		`create table uniq (code text not null unique, loose text unique, v text)`,
		`create view v as select * from account`)

	r := f.run(`select * from account`)[0]
	ed := r.Editable
	if ed == nil || ed.Table != "account" || ed.Schema != "main" || ed.KeyName != "primary key" {
		t.Fatalf("not editable: %s", r.ReadOnly)
	}
	var names, kinds []string
	for _, c := range ed.Columns {
		names = append(names, c.Name)
		kinds = append(kinds, c.Kind)
	}
	if strings.Join(names, ",") != "id,name,active,born,score,,team_id" {
		t.Errorf("names = %v", names)
	}
	if strings.Join(kinds, ",") != "number,text,bool,date,number,text,number" {
		t.Errorf("kinds = %v", kinds)
	}
	if c := ed.Columns[1]; c.Nullable || c.HasDefault {
		t.Errorf("name: %+v, want required", c)
	}
	if !ed.Columns[0].HasDefault || !ed.Columns[4].HasDefault {
		t.Error("rowid primary key and defaulted column should have defaults")
	}
	if ref := ed.Columns[6].Ref; ref == nil || ref.Table != "team" || ref.Column != "id" {
		t.Errorf("team_id ref = %+v", ref)
	}
	if r.Browse == nil {
		t.Error("simple select not browsable")
	}

	// Without a primary key, a unique key without NULLs is used.
	if r := f.run(`select * from uniq`)[0]; r.Editable == nil || r.Editable.KeyName != "unique key sqlite_autoindex_uniq_1" {
		t.Errorf("uniq: %+v %s", r.Editable, r.ReadOnly)
	}

	for q, want := range map[string]string{
		`select name from account`:     "primary key",
		`select * from nokey`:          "no primary key",
		`select * from v`:              "views",
		`select count(*) from account`: "",
		`select a.id, b.id from account a join account b on a.id = b.id`: roNotSimple,
	} {
		r := f.run(q)[0]
		if r.Editable != nil && want != "" {
			t.Errorf("%s: editable, want read-only (%s)", q, want)
		}
		if !strings.Contains(r.ReadOnly, want) {
			t.Errorf("%s: reason %q, want it to mention %q", q, r.ReadOnly, want)
		}
	}
}

func TestSaveEditsSQLite(t *testing.T) {
	f := newSQLite(t)
	f.run(`create table account (id integer primary key, name text not null, email text,
			score real default 5, updated text)`,
		`create trigger touch after update on account begin
			update account set updated = 'yes' where id = new.id; end`,
		`insert into account (id, name, email) values (1, 'ann', 'a@x'), (2, 'bob', null), (3, 'cy', 'c@x')`)
	cols := []string{"id", "name", "email", "score", "updated"}
	req := func() EditRequest {
		return EditRequest{Schema: "main", Table: "account", KeyColumns: []string{"id"}, Columns: cols}
	}

	r := req()
	r.Updates = []RowUpdate{
		{Key: []*string{ptr("1")}, Changes: map[string]*string{"name": ptr("annie"), "email": nil},
			Original: map[string]*string{"name": ptr("ann"), "email": ptr("a@x")}},
		// Editing the key itself: the row is found by its old key.
		{Key: []*string{ptr("2")}, Changes: map[string]*string{"id": ptr("20")}, Original: map[string]*string{"id": ptr("2")}},
	}
	r.Inserts = []RowInsert{{Values: map[string]*string{"name": ptr("dee")}}}
	r.Deletes = []RowDelete{{Key: []*string{ptr("3")}}}

	db, driver, _ := f.m.DB("c")
	preview, err := PreviewEdits(context.Background(), db, driver, r)
	if err != nil || len(preview) != 4 || !strings.HasPrefix(preview[0], "DELETE FROM") ||
		!strings.Contains(preview[1], `"name" = 'annie'`) || !strings.HasPrefix(preview[3], "INSERT INTO") {
		t.Errorf("preview = %q, %v", preview, err)
	}

	res, err := f.save(r)
	if err != nil {
		t.Fatal(err)
	}
	if res.Deleted != 1 || len(res.Updated) != 2 || len(res.Inserted) != 1 {
		t.Fatalf("result = %+v", res)
	}
	if got := strings.Join(deref(res.Updated[0]), ","); got != "1,annie,NULL,5,yes" {
		t.Errorf("refreshed update = %s (trigger should have set updated)", got)
	}
	if got := strings.Join(deref(res.Updated[1]), ","); got != "20,bob,NULL,5,yes" {
		t.Errorf("refreshed key edit = %s", got)
	}
	if got := strings.Join(deref(res.Inserted[0]), ","); got != "21,dee,NULL,5,NULL" {
		t.Errorf("inserted = %s (id and score should come from defaults)", got)
	}
	if got := f.rows(`select id from account order by id`); len(got) != 3 || got[0][0] != "1" || got[1][0] != "20" || got[2][0] != "21" {
		t.Errorf("ids after save = %v", got)
	}

	// Someone else changed the name since it was loaded: conflict, nothing saved.
	f.run(`update account set name = 'changed elsewhere' where id = 1`)
	r = req()
	r.Updates = []RowUpdate{{Key: []*string{ptr("1")}, Changes: map[string]*string{"name": ptr("mine")},
		Original: map[string]*string{"name": ptr("annie")}}}
	r.Inserts = []RowInsert{{Values: map[string]*string{"name": ptr("should not be inserted")}}}
	if _, err := f.save(r); !errors.Is(err, ErrConflict) {
		t.Fatalf("conflict: err = %v", err)
	}
	if got := f.rows(`select count(*) from account`)[0][0]; got != "3" {
		t.Errorf("failed save inserted a row: %s rows", got)
	}

	// A deleted row: ErrRowChanged.
	r = req()
	r.Updates = []RowUpdate{{Key: []*string{ptr("99")}, Changes: map[string]*string{"name": ptr("ghost")}}}
	if _, err := f.save(r); !errors.Is(err, ErrRowChanged) {
		t.Fatalf("missing row: err = %v", err)
	}
	// A new row missing a required value fails with the database's error.
	r = req()
	r.Inserts = []RowInsert{{Values: map[string]*string{"email": ptr("x")}}}
	if _, err := f.save(r); err == nil || !strings.Contains(err.Error(), "new row 1") {
		t.Errorf("insert without name: err = %v", err)
	}
}

func TestReferencedValuesSQLite(t *testing.T) {
	f := newSQLite(t)
	f.run(`create table team (code text primary key)`, `insert into team values ('alpha'), ('beta'), ('al_x')`)
	db, driver, _ := f.m.DB("c")
	got, err := ReferencedValues(context.Background(), db, driver, ColumnRef{Schema: "main", Table: "team", Column: "code"}, "al")
	if err != nil || strings.Join(got, ",") != "al_x,alpha" {
		t.Errorf("ReferencedValues = %v, %v", got, err)
	}
	if got, _ := ReferencedValues(context.Background(), db, driver, ColumnRef{Schema: "main", Table: "team", Column: "code"}, "al_"); strings.Join(got, ",") != "al_x" {
		t.Errorf("LIKE wildcards not escaped: %v", got)
	}
}

func ptr(s string) *string { return &s }

func deref(row []*string) []string {
	out := make([]string, len(row))
	for i, v := range row {
		if v == nil {
			out[i] = "NULL"
		} else {
			out[i] = *v
		}
	}
	return out
}
