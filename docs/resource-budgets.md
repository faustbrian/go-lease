# Resource budgets

| Resource | Bound |
|---|---|
| canonical key | 256 bytes |
| owner identity | 128 bytes; default 32 URL-safe characters |
| TTL | 24 hours |
| acquisition wait | 1 hour |
| acquisition attempts | 10,000 |
| backend operation | 1 minute maximum |
| concurrent client waiters | 1,000,000 configurable maximum; default 1,024 |
| client managed renewers | 100,000 configurable maximum; default 1,024 |
| observers | 16 per decorator; one in-flight callback each |
| managed renewers per handle | one |
| service handles | caller-supplied hard maximum |
| PostgreSQL cleanup | 10,000 rows per call |
| conformance model operations | 128 per fuzz input |

The memory backend also requires a maximum distinct-key count because it
retains token history. Valkey and PostgreSQL backend clients must separately
bound pools, command timeouts, reconnects, TLS handshakes, and server resources.

Acquisition wait is enforced by both the injected scheduling clock and an
independent cancelable wall-clock deadline. A frozen or rolled-back injected
clock therefore cannot extend a caller's configured `Wait` budget.

Lease admission uses the same dual-clock principle. Its conservative local
deadline begins before the acquisition or renewal request, subtracts the
safety margin, and expires when either the injected clock or the independent
process-monotonic clock reaches the bound. Backend/client clock skew and
response latency therefore only shorten local admission; they cannot extend it.

Observation delivery is best effort. A blocked observer occupies only its own
single callback slot; later events for that observer are dropped, and lease
operations never wait for exporter code.
