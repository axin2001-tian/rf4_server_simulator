//go:build ignore

package main

import (
	"database/sql"
	"fmt"
	"os"
	"strings"

	_ "modernc.org/sqlite"
)

func main() {
	db, err := sql.Open("sqlite", os.Args[1])
	if err != nil {
		panic(err)
	}
	defer db.Close()
	rows, err := db.Query(`SELECT k, v FROM kv WHERE k LIKE 'catch-settlement:%' ORDER BY rowid DESC LIMIT 5`)
	if err != nil {
		panic(err)
	}
	defer rows.Close()
	for rows.Next() {
		var k, v string
		if err := rows.Scan(&k, &v); err != nil {
			panic(err)
		}
		fmt.Println("KEY:", k)
		if len(v) > 900 {
			v = v[:900]
		}
		fmt.Println(strings.TrimSpace(v))
		fmt.Println(strings.Repeat("-", 60))
	}
}
