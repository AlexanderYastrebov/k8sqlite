package main

import (
	"context"
	"database/sql"
	_ "embed"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	_ "modernc.org/sqlite"
)

func must[T any](v T, err error) T {
	if err != nil {
		panic(err.Error())
	}
	return v
}

//go:embed schema.sql
var schemaSql string

//go:embed testdata/coredns.json
var corednsJson string

func main() {
	ctx := context.Background()
	ctx, cancel := signal.NotifyContext(ctx, os.Interrupt, syscall.SIGTERM)
	defer cancel()

	db := must(sql.Open("sqlite", ":memory:"))
	defer db.Close()

	must(0, db.PingContext(ctx))
	must(db.ExecContext(ctx, schemaSql))

	insertObjectStmt := must(db.PrepareContext(ctx, `INSERT INTO resources(object) VALUES(?)`))
	defer insertObjectStmt.Close()

	must(insertObjectStmt.ExecContext(ctx, corednsJson))

	var object string
	rows := db.QueryRowContext(ctx, "select object from resources where kind = :kind limit 1;", sql.Named("kind", "Deployment"))
	must("query", rows.Scan(&object))

	fmt.Printf("object: %s\n", object)
}
