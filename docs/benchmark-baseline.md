# Benchmark baseline

The release gate runs `go test ./... -run '^$' -bench . -benchmem
-benchtime=100ms`. Baselines are environment-specific; capture the full output
with CPU, operating system, Go 1.26.5, and backend versions before comparing a
change.

The mandatory local cases cover acquisition plus release, renewal, contention,
parallel renewal load, allocations, and PostgreSQL cleanup adapter overhead.
Live deployment qualification additionally measures p50/p95/p99 backend
latency, cleanup against populated tables, and pool saturation. No absolute
network latency budget is claimed from local in-process benchmarks.

Reference run on Apple M4 Max, Darwin arm64, Go 1.26.5:

| Benchmark | Time | Allocations |
|---|---:|---:|
| AcquireRelease | 361.6 ns/op | 513 B/op, 2 allocs/op |
| Renew | 42.24 ns/op | 0 B/op, 0 allocs/op |
| Contention | 108.8 ns/op | 96 B/op, 3 allocs/op |
| RenewalLoad | 122.0 ns/op | 0 B/op, 0 allocs/op |
| Cleanup | 61.66 ns/op | 104 B/op, 5 allocs/op |
