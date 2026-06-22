package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"os"
	"strings"
	"time"

	"github.com/devpablocristo/companion/internal/agentreconcile"
	_ "github.com/jackc/pgx/v5/stdlib"
)

func main() {
	var apply bool
	var databaseURL string
	var timeout time.Duration
	var includeRows bool
	flag.BoolVar(&apply, "apply", false, "write inferred agents into companion_agents")
	flag.BoolVar(&includeRows, "include-rows", false, "include inferred rows in the JSON report")
	flag.StringVar(&databaseURL, "db", env("COMPANION_DATABASE_URL", env("DATABASE_URL", "")), "Companion Postgres URL")
	flag.DurationVar(&timeout, "timeout", 30*time.Second, "reconcile timeout")
	flag.Parse()

	if strings.TrimSpace(databaseURL) == "" {
		log.Fatal("COMPANION_DATABASE_URL, DATABASE_URL or --db is required")
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	db, err := sql.Open("pgx", databaseURL)
	if err != nil {
		log.Fatalf("open companion db: %v", err)
	}
	defer db.Close()
	if err := db.PingContext(ctx); err != nil {
		log.Fatalf("ping companion db: %v", err)
	}
	report, err := agentreconcile.Run(ctx, db, apply)
	if err != nil {
		log.Fatal(err)
	}
	if !includeRows {
		report.Rows = nil
	}
	encoder := json.NewEncoder(os.Stdout)
	encoder.SetIndent("", "  ")
	if err := encoder.Encode(report); err != nil {
		log.Fatal(err)
	}
	if !apply {
		fmt.Fprintln(os.Stderr, "dry-run only; pass --apply to write companion_agents")
	}
}

func env(key string, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(key)); value != "" {
		return value
	}
	return fallback
}
