# Kubernetes

Use one lease key per logical singleton, not per pod. Include cluster and
environment in the namespace. Configure `terminationGracePeriodSeconds` above
the application drain and release budget, but rely on TTL for `SIGKILL`, node
loss, and partitions.

Readiness should fail when the service cannot safely acquire required leases;
liveness should not restart a healthy contender merely because another pod is
owner. During rollout, old and new versions must use compatible scripts/schema
and the same key derivation. Protected resources must compare fencing tokens
across both versions.
