//go:build ignore
// +build ignore

package main

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
)

func main() {
	pass := os.Getenv("POSTGRES_PASSWORD")
	candidates := []string{
		fmt.Sprintf("postgres://postgres.your-tenant-id:%s@127.0.0.1:5432/postgres?sslmode=disable", pass),
		fmt.Sprintf("postgres://supabase_admin:%s@127.0.0.1:5432/postgres?sslmode=disable", pass),
		fmt.Sprintf("postgres://postgres.your-tenant-id:%s@127.0.0.1:5432/full_puno?sslmode=disable", pass),
		fmt.Sprintf("postgres://supabase_admin:%s@127.0.0.1:5432/full_puno?sslmode=disable", pass),
		fmt.Sprintf("postgres://postgres:%s@127.0.0.1:15432/postgres?sslmode=disable", pass),
		fmt.Sprintf("postgres://supabase_admin:%s@127.0.0.1:15432/postgres?sslmode=disable", pass),
		fmt.Sprintf("postgres://postgres:%s@127.0.0.1:15432/full_puno?sslmode=disable", pass),
		fmt.Sprintf("postgres://supabase_admin:%s@127.0.0.1:15432/full_puno?sslmode=disable", pass),
	}
	for _, dsn := range candidates {
		db, err := sql.Open("pgx", dsn)
		if err != nil {
			fmt.Println("OPEN_ERR", dsn, err)
			continue
		}
		db.SetConnMaxLifetime(1 * time.Minute)
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		err = db.PingContext(ctx)
		cancel()
		if err != nil {
			fmt.Println("PING_ERR", dsn, err)
			_ = db.Close()
			continue
		}
		fmt.Println("OK", dsn)
		_ = db.Close()
		return
	}
	fmt.Println("no candidate worked")
}
