# Unique jobs and non-overlapping handlers

`leasequeue.NewWorker` wraps a `queue/core.Worker`. Its key function should
derive stable job identity from bounded, non-secret fields. `Run` acquires the
lease, cancels the child context on renewal loss, and explicitly releases after
the handler returns.

Read the fence with `leasequeue.TokenFromContext` and pass it into protected
writes. A handler that ignores cancellation may continue after expiry, so the
resource-side fence remains mandatory for dangerous side effects. Queue
acknowledgement and lease release are separate operations with a crash window;
make handlers replay-safe where delivery semantics require it.
