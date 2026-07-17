# Scheduler integration

`leasescheduler.Coordinator.OnOneServer` and `WithoutOverlapping` use the same
fenced execution path. The callback receives a token and a context canceled on
managed-renewal loss.

Use a key that includes environment and schedule identity. Set TTL above the
normal backend round trip and scheduling pause, reserve a safety margin, and
renew well before that margin. Contention means another owner was admitted; it
does not prove that owner's work is still running.

For long jobs, every external write must compare the callback token. After a
loss, stop admitting new work and allow the callback to unwind.
