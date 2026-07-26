package postgres

import migrations "github.com/faustbrian/golib/pkg/migrations"

// Migration describes the reversible PostgreSQL schema contract.
type Migration struct {
	Version uint
	Name    string
	Up      string
	Down    string
}

// SchemaMigration returns the durable lease and persistent-fence schema.
func SchemaMigration() Migration {
	return Migration{
		Version: 1,
		Name:    "create_fenced_leases",
		Up: `CREATE TABLE lease_fences (
    key_digest bytea PRIMARY KEY,
    fencing_token bigint NOT NULL CHECK (fencing_token > 0)
);
CREATE TABLE lease_records (
    key_digest bytea PRIMARY KEY REFERENCES lease_fences (key_digest),
    owner text NOT NULL CHECK (octet_length(owner) BETWEEN 1 AND 128),
    fencing_token bigint NOT NULL CHECK (fencing_token > 0),
    acquired_at timestamptz NOT NULL,
    expires_at timestamptz NOT NULL,
    active boolean NOT NULL,
    updated_at timestamptz NOT NULL,
    CHECK (expires_at > acquired_at)
);
CREATE INDEX lease_records_expiry_idx
    ON lease_records (expires_at) WHERE active;
CREATE INDEX lease_records_cleanup_idx
    ON lease_records (updated_at) WHERE NOT active;`,
		Down: `DROP TABLE lease_records;
DROP TABLE lease_fences;`,
	}
}

// GoMigration returns the schema in migrations' immutable format.
func GoMigration() (migrations.Migration, error) {
	migration := SchemaMigration()
	return migrations.NewMigration(
		migrations.Version(migration.Version), migration.Name,
		migrations.TransactionModeDefault, migration.Up, migration.Down,
	)
}
