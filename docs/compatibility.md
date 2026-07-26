# Compatibility and rolling versions

The minimum Go version is 1.26.5. The v1 API baseline is checked by
`make api-compat`. PostgreSQL versions 14 through 18 and Valkey 9 are exercised
by the integration matrix.

Valkey script response shapes are versioned implicitly by the client release;
rolling clients must retain the four-field record and one-field release
outcomes. `TestRollingScriptResponseVersionsFailClosed` accepts the current
contract and rejects added, removed, or changed fields. PostgreSQL migration 1
is additive for the first release. The live operational fault suite proves an
old client continues through an additive column and fails ownership closed when
a required fence column is incompatible. Future schema changes must be backward
compatible for at least one rolling deployment window before old fields are
removed.

An incompatible key derivation, prefix, schema, or script response must use a
new coordinated namespace/version. Never silently split owners across formats.
