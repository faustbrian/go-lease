# Valkey quickstart

Requirements: Valkey 9, `noeviction`, one authoritative deployment, and a
caller-configured `valkey-go` client with TLS, ACL, routing, and timeouts.

```go
client, err := valkeygo.NewClient(valkeygo.ClientOption{
    InitAddress: []string{"valkey:6379"},
})
if err != nil { return err }
defer client.Close()

backend, err := leasevalkey.Open(ctx, client, "my-service-leases")
if err != nil { return err }
leases, err := lease.NewClient(backend, lease.ClientOptions{})
```

Each logical key becomes a SHA-256-derived hash tag with a TTL lease hash and a
persistent counter in the same cluster slot. Scripts use Valkey `TIME`, compare
both owner and token, and transparently recover from `NOSCRIPT` through
`valkey-go`'s Lua helper.

Do not deploy this as a multi-independent-master algorithm. Counter continuity
ends after flush, restore without counter keys, or acknowledged data loss. See
[failover](failover.md).
