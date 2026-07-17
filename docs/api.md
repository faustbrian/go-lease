# API reference

`NewKey` validates a canonical namespace/name key of at most 256 bytes.
`NewPolicy` copies and validates TTL, wait, retry, jitter, renewal, safety
margin, and attempt bounds. `NewClient` injects a clock, owner source, sleeper,
and jitter source or uses cryptographic production defaults. Policy also fixes
an operation timeout and `FailureFailClosed` behavior. Client options bound
concurrent waiters and managed renewers.

`Client.TryAcquire` performs one backend attempt. `Client.Acquire` retries only
contention and stops at both the wait and attempt bounds. `Handle` exposes
`Owner`, `Token`, `AcquiredAt`, `Deadline`, `State`, `Renew`, `Validate`,
`Release`, and `StartManaged`. `AcquiredAt` is backend-clock inspection data;
`Deadline` is the conservative local admission bound and is safe across
backend/client clock skew.

Stable errors are `ErrContended`, `ErrTimeout`, `ErrCanceled`, `ErrLost`,
`ErrStaleOwner`, `ErrBackendUnavailable`, `ErrInvalidState`, and
`ErrAmbiguousOutcome`. Classify with `errors.Is`; do not parse text.

`NewObservedBackend` emits bounded operation, outcome, time, and hashed key. It
never emits owner or raw key. Observer panics are contained and callbacks run
after the backend operation without package locks held.

`valkey.New` trusts caller configuration; `valkey.Open` additionally verifies
Valkey 9 or newer and `noeviction` before returning a backend.

All exported declarations are also available through `go doc` and protected by
`make api-compat`.
