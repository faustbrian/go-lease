# Race and lifecycle stress report

`make stress` repeats the ownership-critical concurrency matrix 25 times under
Go's race detector with shuffled test order. The gate covers contended
acquisition, cancellation, explicit renewal and release, concurrent handle
operations, late responses, managed renewal and loss notification, stale-owner
successor protection, queue and scheduler loss cancellation, and service
shutdown races.

The command is deterministic in scope and bounded in repetitions. Increase
`STRESS_COUNT` for soak runs; the blocking local and hosted gate keeps the
documented default so execution time remains bounded.

The release gate is:

```text
go test -race -shuffle=on -count=25 -run <lifecycle matrix> ./...
```

Passing this report does not replace backend fault tests. PostgreSQL and Valkey
failover, restore, partition, and credential phases remain in
`make backend-hardening`.
