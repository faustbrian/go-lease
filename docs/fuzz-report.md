# Fuzz report

`make fuzz` runs bounded smoke fuzzing for canonical key parsing, policy bounds,
and the memory lease state model. The model limits each input to 128 operations
and mixes acquire, renew, release, expiry jumps, and clock rollback.

Long-running qualification should extend `FUZZ_TIME` and preserve any generated
corpus. A crash, non-monotonic successful fence, accepted out-of-bound policy,
or unexpected state-machine error is a release blocker.
