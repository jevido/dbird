//go:build integration

package dbx_test

import (
	"context"
	"testing"
	"time"

	"dbird/internal/dbx"
	"dbird/internal/store"

	embeddedpostgres "github.com/fergusstrange/embedded-postgres"
)

func TestPostgres(t *testing.T) {
	pg := embeddedpostgres.NewDatabase(embeddedpostgres.DefaultConfig().Port(54329).RuntimePath(t.TempDir()))
	if err := pg.Start(); err != nil {
		t.Fatal(err)
	}
	defer pg.Stop()
	c := store.Connection{ID: "pg", Driver: dbx.Postgres, Host: "localhost", Port: 54329, User: "postgres", Password: "postgres", Database: "postgres", SSLMode: "disable"}
	ctx := context.Background()
	v, err := dbx.Test(ctx, c)
	if err != nil {
		t.Fatal(err)
	}
	t.Log("version", v)
	m := dbx.NewManager()
	defer m.Close()
	if err := m.Ensure(ctx, c); err != nil {
		t.Fatal(err)
	}
	res, err := m.Run(ctx, "t1", "pg", []string{
		`create schema app`,
		`create table app.people (id serial primary key, name text not null, data jsonb, born date, at timestamptz default now(), score numeric(10,2), uid uuid default gen_random_uuid(), tags text[], raw bytea)`,
		`insert into app.people (name, data, born, score, tags, raw) values ('ann', '{"a":1}', '2001-02-03', 12.5, '{x,y}', '\xdead') returning id`,
		`insert into app.people (name) values ('bob'), ('cy')`,
		`select * from app.people order by id`,
		`set search_path to app`,
		`select count(*) from people`,
		`create or replace function app.f() returns int as $$ begin return 1; end $$ language plpgsql`,
		`explain select 1`,
	}, 100, false)
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range res {
		if r.Error != "" {
			t.Fatalf("%s: %s", r.SQL, r.Error)
		}
	}
	if res[3].RowsAffected != 2 {
		t.Errorf("affected=%d", res[3].RowsAffected)
	}
	sel := res[4]
	for i, col := range sel.Columns {
		v := "NULL"
		if sel.Rows[0][i] != nil {
			v = *sel.Rows[0][i]
		}
		t.Logf("%s (%s) = %s", col.Name, col.Type, v)
	}
	if *res[6].Rows[0][0] != "3" {
		t.Errorf("search_path session state lost: %v", *res[6].Rows[0][0])
	}

	db, driver, _ := m.DB("pg")
	schemas, err := dbx.Schemas(ctx, db, driver)
	t.Log("schemas", schemas, err)
	tables, err := dbx.Tables(ctx, db, driver, "app")
	t.Log("tables", tables, err)
	cols, err := dbx.Columns(ctx, db, driver, "app", "people")
	t.Logf("cols %+v %v", cols, err)
	if err != nil || len(cols) != 9 || !cols[0].PrimaryKey {
		t.Fatal("bad columns")
	}
	ds, err := dbx.DefaultSchema(ctx, db, driver)
	t.Log("default schema", ds, err)
	sc, err := dbx.SchemaColumns(ctx, db, driver, "app")
	t.Log("completion", sc, err)

	// Cancellation of a long query.
	done := make(chan []dbx.Result)
	go func() {
		r, _ := m.Run(ctx, "t2", "pg", []string{"select pg_sleep(10)"}, 10, false)
		done <- r
	}()
	time.Sleep(500 * time.Millisecond)
	if _, err := m.Run(ctx, "t2", "pg", []string{"select 1"}, 10, false); err != dbx.ErrBusy {
		t.Errorf("expected busy, got %v", err)
	}
	start := time.Now()
	if err := m.Cancel("t2"); err != nil {
		t.Fatal(err)
	}
	r := <-done
	t.Logf("cancelled after %s: %s", time.Since(start), r[0].Error)
	if r[0].Error == "" || time.Since(start) > 3*time.Second {
		t.Fatal("cancel did not work")
	}
	// Session is usable again afterwards.
	r2, err := m.Run(ctx, "t2", "pg", []string{"select 42"}, 10, false)
	if err != nil || r2[0].Error != "" {
		t.Fatalf("after cancel: %v %+v", err, r2)
	}

	// Truncation with big result.
	r3, _ := m.Run(ctx, "t3", "pg", []string{"select g from generate_series(1, 100000) g"}, 1000, false)
	if !r3[0].Truncated || len(r3[0].Rows) != 1000 {
		t.Fatalf("truncation: %v %d", r3[0].Truncated, len(r3[0].Rows))
	}
	t.Logf("truncated big select in %.1f ms", r3[0].DurationMs)
}
