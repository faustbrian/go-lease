package postgres

import migrations "github.com/faustbrian/go-migrations/v4"

// Migration is the value description of lease/fence schema DDL, not execution
// or permission authority. Dropping/recreating it loses the continuity epoch.
type Migration struct {
	// Version is the schema migration identifier, not a fencing token.
	Version uint
	// Name identifies the schema operation.
	Name string
	// Up creates the two tables and indexes in the caller-selected schema context.
	Up string
	// Down drops lease records and persistent fences; historical resource fences
	// require operator coordination before a recreated backend admits ownership.
	Down string
}

// SchemaMigration returns fresh DDL values without touching a database. The
// caller owns migration transactions/search_path/permissions; executing Down is
// structurally reversible DDL, not preservation of monotonic fence history.
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

// GoMigration validates the DDL into go-migrations/v4's immutable value with
// TransactionModeDefault. It performs no database work or continuity recovery;
// the caller owns migration execution and any adoption/rollback consequences.
func GoMigration() (migrations.Migration, error) {
	migration := SchemaMigration()
	return migrations.NewMigration(
		migrations.Version(migration.Version), migration.Name,
		migrations.TransactionModeDefault, migration.Up, migration.Down,
	)
}
