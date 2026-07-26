# Formal state machines

The model separates an acquisition attempt, a local handle, and the
authoritative backend record. This prevents a local observation from being
mistaken for a backend ownership transition.

## Acquisition attempt

| From | Event | To | Result |
|---|---|---|---|
| idle | capacity and owner generation succeed | attempting | dispatch allowed |
| idle | capacity or owner generation fails | failed | no dispatch |
| attempting | matching successful response | acquired | create `active` handle |
| attempting | contended, retry budget remains | waiting | no handle |
| waiting | bounded delay completes | attempting | next dispatch allowed |
| attempting/waiting | wait or attempt budget ends | timed-out | `ErrTimeout` |
| attempting/waiting | caller cancellation is definite | canceled | `ErrCanceled` |
| attempting | mutation outcome or success identity is uncertain | ambiguous | `ErrAmbiguousOutcome` |
| attempting | definite non-contention backend failure | failed | classified error |

`acquired`, `timed-out`, `canceled`, `ambiguous`, and `failed` are terminal for
that attempt. A retry reuses the same owner identity and remains bounded by
wait duration, attempt count, operation timeout, and jitter limits. An
ambiguous attempt creates no locally admissible handle even if the backend may
have committed the acquisition.

## Local handle

Local handle states are `active`, `expired`, `lost`, `uncertain`, and
`released`.

| From | Event | To | Admission |
|---|---|---|---|
| acquired | matching acquisition response | active | before safe deadline |
| active | matching renew response | active | before new safe deadline |
| active | matching validation response | active | before existing safe deadline |
| active | safety deadline reached before or during an operation | expired | denied |
| active | stale owner or proven loss response | lost | denied |
| active | ambiguous, canceled, unavailable, or corrupt remote response | uncertain | denied |
| active | successful compare-release | released | denied |
| expired | successful compare-release | released | denied |
| expired | failed compare-release | expired | denied |
| lost/uncertain | release requested | unchanged | denied with `ErrInvalidState` |
| released | release requested again | released | denied; success is idempotent |
| any | concurrent remote transition requested | unchanged | denied with `ErrInvalidState` |

`expired`, `lost`, `uncertain`, and `released` are terminal for admission by
that handle. Only the explicit `expired` to `released` cleanup transition may
change a terminal handle. Cancellation never creates a `released` transition.
A managed renewer sends at most one loss observation and then exits; stopping
it never implies release.

## Authoritative backend and successor

For one canonical key, backend state is `vacant(F)` or
`owned(F, owner, token, expiry)`, where `F` is retained fence history.

| From | Event and atomic predicate | To | Fence effect |
|---|---|---|---|
| vacant(F) | acquisition | owned(F+1, new owner, F+1, expiry) | increment once |
| owned(F, O, T, E) | acquisition before E | unchanged | none; contended |
| owned(F, O, T, E) | renew/validate with O and T before E | owned with same O and T | none |
| owned(F, O, T, E) | compare-release with O and T | vacant(F) | retain F |
| owned(F, O, T, E) | expiry then successor acquisition | owned(F+1, successor, F+1, expiry) | increment once |
| any | renew/validate/release with stale O or T | unchanged | none; stale rejected |
| any | definite failed mutation | unchanged | none |
| any | ambiguous mutation response | backend state unknown to caller | local admission denied |

Within one documented continuity epoch, every successful successor token is
strictly greater than every earlier successful token for the same canonical
key. Every renew, validation, and release compares both owner and token in the
same atomic backend operation. Therefore an earlier handle cannot renew,
validate, or delete a successor, including after its own expiry, cancellation,
pause, late response, or ambiguous operation.

Each handle reserves at most one remote transition at a time. Concurrent
renew, validation, release, or managed-start attempts fail closed with
`ErrInvalidState`; they never wait behind user observation code. Remote calls
run outside the state mutex, so observers may safely inspect the handle. If the
safe deadline passes during a successful remote operation, the local handle
remains terminally expired and returns `ErrLost`.

The local deadline starts before the backend acquisition or renewal call. It
is bounded twice: once by the injected scheduling clock and once by an
independent process-monotonic clock. Backend wall-clock timestamps are retained
for inspection but never compared with the client wall clock for admission.
