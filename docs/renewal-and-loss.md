# Renewal and loss handling

Choose `RenewEvery < TTL - SafetyMargin`. The margin covers request latency,
runtime pauses, scheduling delay, and response processing. Managed renewal owns
one goroutine, sends at most one `Loss`, and never releases implicitly.

On `ErrStaleOwner` or `ErrLost`, a successor or expiry is proven. On
`ErrAmbiguousOutcome`, `ErrBackendUnavailable`, or a canceled renewal, local
admission fails closed because the remote result is not reliable. Stop creating
side effects, cancel child work, and rely on resource fencing for work already
in flight.

`Managed.Stop` only stops the goroutine. Call `Handle.Release` separately and
inspect its result.
