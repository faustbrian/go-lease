// Command postgres demonstrates one PostgreSQL lease acquire/release using a
// caller-owned pool from POSTGRES_URL. The operator must first install the schema
// and choose connection/durability policy. It adds no protected effect or complete
// shutdown/failover qualification; this example requires a configured live server.
package main

import (
	"context"
	"log"
	"os"
	"time"

	lease "github.com/faustbrian/go-lease/v3"
	leasepostgres "github.com/faustbrian/go-lease/v3/postgres"
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
