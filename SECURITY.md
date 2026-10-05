# Security policy

Report vulnerabilities privately through GitHub Security Advisories. Do not
include production keys, owner identities, connection strings, or customer data
in reports.

Published v1 releases use `github.com/faustbrian/go-lease`; v2 releases use
`github.com/faustbrian/go-lease/v2`. Security reports should identify the
affected module path and version, backend, continuity epoch, stale-owner
sequence, protected-resource fence behavior, and any ambiguous operation.

This package does not protect a resource that ignores fencing tokens. Backend
durability, ACL, TLS, restore, and failover configuration remain operator-owned.
Valkey guards expose redacted backend coordinates for atomic protected writes;
do not log them, send them over untrusted boundaries, or treat guard
construction as proof that ownership is still active.
