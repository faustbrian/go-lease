# Mutation report

`make mutation` pins Gremlins v0.6.0, two workers, and timeout coefficient 10.
It targets the memory reference state machine, where mutations to owner/token
comparison, expiry boundaries, counter increments, active state, and release
conditions are exercised by conformance, model, and hardening tests. It then
runs `scripts/check-fence-mutations.sh` against disposable source copies.

The adapter mutation gate changes Valkey owner/token comparisons from unequal
to equal and PostgreSQL owner/token comparisons from equal to unequal. The
exact contract tests must fail for every mutated copy. This covers the Lua and
SQL predicates that Go mutation tools cannot rewrite directly.
Shared conformance independently forges only the owner and only the token for
renew, validate, and release, then revalidates the successor after every
rejection. This detects an adapter that accidentally compares only one field.

The current reference run generated 23 Go mutants: 23 killed, zero lived, zero
uncovered, 100.00% efficacy, and 100.00% mutant coverage. The adapter run
generated four comparison mutation classes: four killed and zero lived.
