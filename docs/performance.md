# Performance

The explicit full route, `make check-full`, retains the native `./...`
benchmark operation at `100ms` with a `20m` timeout for acquisition/release
and renewal latency and allocation baselines. Ordinary local gates do not run
this campaign. Run live backend benchmarks in the deployment region before
setting TTL and margins; local memory results are not network predictions.

Contention serializes per key by design. Shard unrelated work across distinct
keys, keep renewal well below backend capacity, and bound client pools. Valkey
uses one script round trip per operation. PostgreSQL uses one statement per
operation and keeps fence rows permanently.

Record environment, Go version, backend version, concurrency, latency
percentiles, and allocations when changing a budget. A faster acquisition does
not justify reducing the resource-side fencing requirement.
