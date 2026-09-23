package dbx

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"dbird/internal/store"
)

func TestFirstKeyword(t *testing.T) {
	cases := map[string]string{
		"select 1":                       "select",
		"  -- hi\n  /* x */ (SELECT 1)":  "SELECT",
		"with x as (select 1) select *":  "with",
		"# mysql comment\ninsert into t": "insert",
		"":                               "",
	}
	for in, want := range cases {
		if got := firstKeyword(in); got != want {
			t.Errorf("firstKeyword(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestSQLiteRoundTrip(t *testing.T) {
	ctx := context.Background()
	c := store.Connection{ID: "c1", Driver: SQLite, Database: filepath.Join(t.TempDir(), "t.db")}
	if _, err := Test(ctx, c); err != nil {
		t.Fatal(err)
	}
	m := NewManager()
	defer m.Close()
	if err := m.Connect(ctx, c); err != nil {
		t.Fatal(err)
	}

	res, err := m.Run(ctx, "tab1", "c1", []string{
		"create table people (id integer primary key, name text not null, avatar blob)",
		"insert into people (name, avatar) values ('ann', x'00ff'), ('bob', null)",
		"select id, name, avatar from people order by id",
	}, 1, false)
	if err != nil {
		t.Fatal(err)
	}
	if len(res) != 3 {
		t.Fatalf("got %d results", len(res))
	}
	for _, r := range res {
		if r.Error != "" {
			t.Fatalf("%s: %s", r.SQL, r.Error)
		}
	}
	if res[1].RowsAffected != 2 {
		t.Errorf("rows affected = %d", res[1].RowsAffected)
	}
	sel := res[2]
	if !sel.HasResultSet || len(sel.Columns) != 3 || len(sel.Rows) != 1 || !sel.Truncated {
		t.Fatalf("unexpected select result: %+v", sel)
	}
	if got := *sel.Rows[0][1]; got != "ann" {
		t.Errorf("name = %q", got)
	}
	if got := *sel.Rows[0][2]; got != "0x00ff" {
		t.Errorf("avatar = %q", got)
	}

	// Errors stop the script unless continueOnError is set.
	res, _ = m.Run(ctx, "tab1", "c1", []string{"select * from nope", "select 1"}, 10, false)
	if len(res) != 1 || res[0].Error == "" {
		t.Fatalf("expected single failed result, got %+v", res)
	}
	res, _ = m.Run(ctx, "tab1", "c1", []string{"select * from nope", "select 1"}, 10, true)
	if len(res) != 2 || res[1].Error != "" {
		t.Fatalf("expected script to continue, got %+v", res)
	}

	db, driver, err := m.DB("c1")
	if err != nil {
		t.Fatal(err)
	}
	schemas, err := Schemas(ctx, db, driver)
	if err != nil || len(schemas) == 0 || schemas[0] != "main" {
		t.Fatalf("schemas = %v, %v", schemas, err)
	}
	tables, err := Tables(ctx, db, driver, "main")
	if err != nil || len(tables) != 1 || tables[0].Name != "people" {
		t.Fatalf("tables = %v, %v", tables, err)
	}
	cols, err := Columns(ctx, db, driver, "main", "people")
	if err != nil || len(cols) != 3 || !cols[0].PrimaryKey || cols[1].Nullable {
		t.Fatalf("columns = %+v, %v", cols, err)
	}
	sc, err := SchemaColumns(ctx, db, driver, "main")
	if err != nil || len(sc["people"]) != 3 {
		t.Fatalf("schema columns = %v, %v", sc, err)
	}

	m.Disconnect("c1")
	if _, err := m.Run(ctx, "tab1", "c1", []string{"select 1"}, 10, false); err != ErrNotConnected {
		t.Fatalf("expected ErrNotConnected, got %v", err)
	}
}

func TestDSN(t *testing.T) {
	pg, err := DSN(store.Connection{Driver: Postgres, Host: "db", User: "u", Password: "p@ss", Database: "app"})
	if err != nil {
		t.Fatal(err)
	}
	if want := "postgres://u:p%40ss@db:5432/app?application_name=dbird&sslmode=prefer"; pg != want {
		t.Errorf("pg dsn = %q, want %q", pg, want)
	}
	my, err := DSN(store.Connection{Driver: MySQL, User: "root", Password: "x", Database: "app"})
	if err != nil {
		t.Fatal(err)
	}
	if want := "root:x@tcp(localhost:3306)/app"; my != want {
		t.Errorf("mysql dsn = %q, want %q", my, want)
	}
}

func TestSQLiteOddPath(t *testing.T) {
	ctx := context.Background()
	dir := filepath.Join(t.TempDir(), "my dir#1")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "odd?name%20&x.db")
	c := store.Connection{ID: "odd", Driver: SQLite, Database: path}
	m := NewManager()
	defer m.Close()
	if err := m.Connect(ctx, c); err != nil {
		t.Fatal(err)
	}
	res, err := m.Run(ctx, "t", "odd", []string{"create table x (a int)", "pragma foreign_keys"}, 10, false)
	if err != nil || res[0].Error != "" || res[1].Error != "" {
		t.Fatalf("%v %+v", err, res)
	}
	if got := *res[1].Rows[0][0]; got != "1" {
		t.Errorf("foreign_keys pragma not applied: %s", got)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("database not created at exact path: %v", err)
	}
}

func TestCompletionLookups(t *testing.T) {
	ctx := context.Background()
	c := store.Connection{ID: "cmp", Driver: SQLite, Database: filepath.Join(t.TempDir(), "c.db")}
	m := NewManager()
	defer m.Close()
	if err := m.Connect(ctx, c); err != nil {
		t.Fatal(err)
	}
	if _, err := m.Run(ctx, "t", "cmp", []string{
		"create table customers (id int)", "create table customer_notes (id int)",
		"create table orders (id int)", "create table cust_x (id int)",
	}, 10, false); err != nil {
		t.Fatal(err)
	}
	db, driver, _ := m.DB("cmp")
	n, err := TableCount(ctx, db, driver, "main")
	if err != nil || n != 4 {
		t.Fatalf("count = %d, %v", n, err)
	}
	got, err := TablesByPrefix(ctx, db, driver, "main", "cust", 10)
	if err != nil || strings.Join(got, ",") != "cust_x,customer_notes,customers" {
		t.Fatalf("prefix = %v, %v", got, err)
	}
	// "_" is a LIKE wildcard; it must match literally.
	got, _ = TablesByPrefix(ctx, db, driver, "main", "cust_", 10)
	if strings.Join(got, ",") != "cust_x" {
		t.Fatalf("escaped prefix = %v", got)
	}
	got, _ = TablesByPrefix(ctx, db, driver, "main", "", 2)
	if len(got) != 2 {
		t.Fatalf("limit = %v", got)
	}
}

func TestTablesPage(t *testing.T) {
	ctx := context.Background()
	c := store.Connection{ID: "pg", Driver: SQLite, Database: filepath.Join(t.TempDir(), "p.db")}
	m := NewManager()
	defer m.Close()
	if err := m.Connect(ctx, c); err != nil {
		t.Fatal(err)
	}
	var stmts []string
	for i := 1; i <= 25; i++ {
		stmts = append(stmts, fmt.Sprintf("create table t%02d (id int)", i))
	}
	stmts = append(stmts, "create view v_t1 as select 1", "create table under_score (id int)")
	if _, err := m.Run(ctx, "t", "pg", stmts, 10, false); err != nil {
		t.Fatal(err)
	}
	db, driver, _ := m.DB("pg")
	var all []string
	after := ""
	for {
		page, err := TablesPage(ctx, db, driver, "main", "", after, 10)
		if err != nil {
			t.Fatal(err)
		}
		for _, tb := range page {
			all = append(all, tb.Name)
		}
		if len(page) < 10 {
			break
		}
		after = page[len(page)-1].Name
	}
	if len(all) != 27 || all[0] != "t01" || all[26] != "v_t1" {
		t.Fatalf("paged = %d %v", len(all), all)
	}
	got, _ := TablesPage(ctx, db, driver, "main", "T1", "", 100)
	if len(got) != 11 { // t10..t19, v_t1 (case-insensitive contains)
		t.Fatalf("filter = %v", got)
	}
	n, _ := TableCountMatching(ctx, db, driver, "main", "_")
	if n != 2 { // "_" is literal: under_score, v_t1
		t.Fatalf("count(_) = %d", n)
	}
}
