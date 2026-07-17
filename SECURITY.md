# Security policy

Report vulnerabilities privately through GitHub Security Advisories. Do not
include production keys, owner identities, connection strings, or customer data
in reports.

Supported releases begin with v1 after publication. Security reports should
describe the backend, continuity epoch, stale-owner sequence, protected-resource
fence behavior, and whether an ambiguous operation occurred.

This package does not protect a resource that ignores fencing tokens. Backend
durability, ACL, TLS, restore, and failover configuration remain operator-owned.
