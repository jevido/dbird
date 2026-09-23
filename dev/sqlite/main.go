// Command sqlite creates a SQLite database from a SQL script, for machines
// without the sqlite3 CLI. Usage: go run ./dev/sqlite out.sqlite seed.sql
package main

import (
	"database/sql"
	"log"
	"os"

	_ "modernc.org/sqlite"
)

func main() {
	if len(os.Args) != 3 {
		log.Fatal("usage: sqlite <database> <script.sql>")
	}
	script, err := os.ReadFile(os.Args[2])
	if err != nil {
		log.Fatal(err)
	}
	db, err := sql.Open("sqlite", os.Args[1])
	if err != nil {
		log.Fatal(err)
	}
	defer db.Close()
	if _, err := db.Exec(string(script)); err != nil {
		log.Fatal(err)
	}
}
