# Renewal and loss handling

[Documentation map](index.md) · [Repository README](../README.md)
[Next: API reference](api.md)

Choose `RenewEvery < TTL - SafetyMargin`. The margin covers request latency,
runtime pauses, scheduling delay, and response processing. Managed renewal owns
one goroutine, sends at most one `Loss`, and never releases implicitly.

On `ErrStaleOwner`, the backend comparison refused ownership; `ErrLost` can
instead mean local admission expired or the adapter signaled renewal loss, not
proof of remote release. On
`ErrAmbiguousOutcome`, `ErrBackendUnavailable`, or a canceled renewal, local
admission fails closed because the remote result is not reliable. Stop creating
side effects, cancel child work, and rely on resource fencing for work already
in flight.

`Managed.Stop` only stops the goroutine. Call `Handle.Release` separately and
inspect its result.

Only a reported renewal-call failure produces `Loss`; context cancellation or a
sleeper error can close the stream without a value. Without managed renewal,
queue/scheduler callbacks have no separate automatic TTL cancellation watcher.
Contexts signal loss but do not stop arbitrary callback code or undo effects.
