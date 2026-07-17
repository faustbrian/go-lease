# FAQ

## Is this a mutex?

No. Ownership expires, and old work may continue. Use fencing at the resource.

## Is the memory backend distributed?

No. It is a deterministic reference and process-local test implementation.

## Is acquisition fair?

No ordering or starvation bound is claimed.

## Can I hold multiple keys atomically?

No. Multi-key leases and a deadlock policy are out of scope.

## Does shutdown release the lease?

Only a successful `Release` response proves it. Cancellation does not.

## Which clock decides expiry?

Valkey and PostgreSQL use server time for remote expiry. The handle starts a
conservative local deadline before each acquisition or renewal request and
expires when either its injected clock or an independent process-monotonic
clock reaches that bound. Backend timestamps are never compared with the
client wall clock for admission.

## What happens after restore?

Treat lost counter history as a new epoch and reconcile protected resources.
