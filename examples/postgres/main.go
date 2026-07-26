// Command postgres demonstrates acquiring a PostgreSQL-backed fenced lease.
package main

import (
	"context"
	"log"
	"os"
	"time"

	lease "github.com/faustbrian/golib/pkg/lease"
	leasepostgres "github.com/faustbrian/golib/pkg/lease/postgres"
	"github.com/jackc/pgx/v5/pgxpool"
)

func main() {
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, os.Getenv("POSTGRES_URL"))
	if err != nil {
		log.Fatal(err)
	}
	defer pool.Close()
	backend, err := leasepostgres.New(pool)
	if err != nil {
		log.Fatal(err)
	}
	leases, _ := lease.NewClient(backend, lease.ClientOptions{})
	key, _ := lease.NewKey("examples", "postgres")
	policy, _ := lease.NewPolicy(lease.PolicyOptions{
		TTL: 30 * time.Second, Retry: 100 * time.Millisecond, MaxAttempts: 1,
	})
	handle, err := leases.TryAcquire(ctx, key, policy)
	if err != nil {
		log.Fatal(err)
	}
	defer func() {
		if err := handle.Release(context.WithoutCancel(ctx)); err != nil {
			log.Printf("release outcome: %v", err)
		}
	}()
	log.Print("acquired fenced lease")
}
