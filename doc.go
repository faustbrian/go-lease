// Package lease acquires time-bounded ownership and per-key fencing tokens from
// a caller-owned Backend. Construct NewKey, NewPolicy and NewClient, then acquire
// a Handle and explicitly Release it. Managed renewal must be stopped separately.
// The core has no live-service or cgo requirement; memory is process-local, while
// postgres and valkey use caller-owned native clients and backend time.
//
// A Handle's injected-clock and real-time deadlines govern local admission, not
// proof that remote effects are absent after cancellation. Tokens are monotonic
// only while backend fence history survives its continuity epoch. Resource-side
// atomic fence comparison, authentication, effect idempotency, durable failover
// and callback cooperation remain application/deployment responsibilities.
// See docs/fencing.md, docs/backend-guarantees.md and docs/renewal-and-loss.md.
package lease
