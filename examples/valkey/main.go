// Command valkey demonstrates one Valkey lease acquire/release on a local
// caller-owned client. New does not perform Open's version/noeviction inspection;
// deployment, ACL/TLS and counter durability remain operator responsibilities.
// This live-server example adds no protected effect or failover qualification.
package main

import (
	"context"
	"log"
	"time"

	lease "github.com/faustbrian/go-lease/v3"
	leasevalkey "github.com/faustbrian/go-lease/v3/valkey"
	valkeygo "github.com/valkey-io/valkey-go"
)

func main() {
	ctx := context.Background()
	client, err := valkeygo.NewClient(valkeygo.ClientOption{
		InitAddress: []string{"127.0.0.1:6379"},
	})
	if err != nil {
		log.Fatal(err)
	}
	defer client.Close()
	backend, err := leasevalkey.New(client, "example-leases")
	if err != nil {
		log.Fatal(err)
	}
	leases, _ := lease.NewClient(backend, lease.ClientOptions{})
	key, _ := lease.NewKey("examples", "valkey")
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
	log.Printf("acquired fence %d", handle.Token())
}
