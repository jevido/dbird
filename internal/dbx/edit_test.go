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
	}
	for _, q := range bad {
		if sel, _ := parseSimpleSelect(q, Postgres); sel != nil {
			t.Errorf("%q: parsed as editable %+v", q, sel)
		}
	}
	// # starts a comment in MySQL only.
	if sel, _ := parseSimpleSelect("select id # the key\nfrom account", MySQL); sel == nil {
		t.Error("mysql # comment not skipped")
	}
}

func TestEditSQLite(t *testing.T) {
	ctx := context.Background()
	c := store.Connection{ID: "c", Driver: SQLite, Database: filepath.Join(t.TempDir(), "t.db")}
	m := NewManager()
	defer m.Close()
	if err := m.Connect(ctx, c); err != nil {
		t.Fatal(err)
	}
	run := func(stmts ...string) []Result {
		t.Helper()
		res, err := m.Run(ctx, "tab", "c", stmts, 100, false)
		if err != nil {
			t.Fatal(err)
		}
		for _, r := range res {
			if r.Error != "" {
				t.Fatalf("%s: %s", r.SQL, r.Error)
			}
		}
		return res
	}
	run(`create table account (id integer primary key, name text, score real, raw blob)`,
		`insert into account values (1, 'ann', 1.5, x'00'), (2, 'bob', null, null)`,
		`create table nokey (x int)`,
		`create view v as select * from account`)

	r := run(`select * from account order by id`)[0]
	ed := r.Editable
	if ed == nil || ed.Table != "account" || ed.Schema != "main" {
		t.Fatalf("not editable: %s", r.ReadOnly)
	}
	if strings.Join(ed.Columns, ",") != "id,name,score," || len(ed.Key) != 1 || ed.Key[0] != 0 {
		t.Fatalf("editable = %+v", ed)
	}

	for q, want := range map[string]string{
		`select name from account`:     "primary key",
		`select * from nokey`:          roNoKey,
		`select * from v`:              "views",
		`select count(*) from account`: "", // expression without FROM mapping is fine but no key
		`select a.id, b.id from account a join account b on a.id = b.id`: roNotSimple,
	} {
		r := run(q)[0]
		if r.Editable != nil && want != "" {
			t.Errorf("%s: editable, want read-only (%s)", q, want)
		}
		if !strings.Contains(r.ReadOnly, want) {
			t.Errorf("%s: reason %q, want it to mention %q", q, r.ReadOnly, want)
		}
	}

	name, null := "annie", (*string)(nil)
	db, driver, _ := m.DB("c")
	n, err := SaveEdits(ctx, db, driver, EditRequest{
		Schema: "main", Table: "account", KeyColumns: []string{"id"},
		Rows: []RowEdit{
			{Key: []*string{ptr("1")}, Changes: map[string]*string{"name": &name, "score": null}},
			{Key: []*string{ptr("2")}, Changes: map[string]*string{"score": ptr("7.25")}},
		},
	})
	if err != nil || n != 2 {
		t.Fatalf("SaveEdits = %d, %v", n, err)
	}
	got := run(`select id, name, score from account order by id`)[0].Rows
	if *got[0][1] != "annie" || got[0][2] != nil || *got[1][2] != "7.25" {
		t.Errorf("after save: %v %v", deref(got[0]), deref(got[1]))
	}

	// A missing row fails the whole save.
	_, err = SaveEdits(ctx, db, driver, EditRequest{
		Schema: "main", Table: "account", KeyColumns: []string{"id"},
		Rows: []RowEdit{
			{Key: []*string{ptr("2")}, Changes: map[string]*string{"name": ptr("changed")}},
			{Key: []*string{ptr("99")}, Changes: map[string]*string{"name": ptr("ghost")}},
		},
	})
	if !errors.Is(err, ErrRowChanged) {
		t.Fatalf("missing row: err = %v", err)
	}
	if got := run(`select name from account where id = 2`)[0].Rows[0][0]; *got != "bob" {
		t.Errorf("failed save still changed row 2 to %q", *got)
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
