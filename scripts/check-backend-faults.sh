#!/usr/bin/env bash
set -euo pipefail

postgres_name=lease-postgres-faults
postgres_replica_name=lease-postgres-replica-faults
valkey_name=lease-valkey-faults
valkey_replica_name=lease-valkey-replica-faults
valkey_secure_name=lease-valkey-secure-faults
network_name=lease-backend-faults
postgres_replica_volume=lease-postgres-replica-data
valkey_secure_volume=lease-valkey-secure-data
postgres_port=55432
postgres_replica_port=55433
valkey_port=56379
valkey_replica_port=56380
valkey_secure_port=56381
valkey_test_pid=
coordination_dir=

cleanup() {
  if test -n "$valkey_test_pid"; then
    kill "$valkey_test_pid" >/dev/null 2>&1 || true
    wait "$valkey_test_pid" >/dev/null 2>&1 || true
  fi
  docker unpause "$postgres_name" "$postgres_replica_name" \
    "$valkey_name" "$valkey_replica_name" "$valkey_secure_name" \
    >/dev/null 2>&1 || true
  docker rm -f "$postgres_name" "$postgres_replica_name" \
    "$valkey_name" "$valkey_replica_name" "$valkey_secure_name" \
    >/dev/null 2>&1 || true
  docker volume rm "$postgres_replica_volume" >/dev/null 2>&1 || true
  docker volume rm "$valkey_secure_volume" >/dev/null 2>&1 || true
  docker network rm "$network_name" >/dev/null 2>&1 || true
  if test -n "$coordination_dir"; then
    rm -rf "$coordination_dir"
  fi
}
trap cleanup EXIT
cleanup

docker network create "$network_name" >/dev/null
docker run -d --name "$postgres_name" \
  --network "$network_name" \
  -e POSTGRES_PASSWORD=postgres -e POSTGRES_DB=lease \
  -p "$postgres_port:5432" postgres:18 >/dev/null
docker run -d --name "$valkey_name" \
  --network "$network_name" \
  -p "$valkey_port:6379" valkey/valkey:9.0-alpine >/dev/null
docker run -d --name "$valkey_replica_name" \
  --network "$network_name" \
  -p "$valkey_replica_port:6379" valkey/valkey:9.0-alpine \
  valkey-server --replicaof "$valkey_name" 6379 >/dev/null

for _ in {1..60}; do
  docker exec "$postgres_name" pg_isready -U postgres -d lease \
    >/dev/null 2>&1 && break
  sleep 1
done
docker exec "$postgres_name" pg_isready -U postgres -d lease >/dev/null

for _ in {1..60}; do
  docker exec "$valkey_name" valkey-cli ping >/dev/null 2>&1 && break
  sleep 1
done
docker exec "$valkey_name" valkey-cli ping >/dev/null
for _ in {1..60}; do
  docker exec "$valkey_replica_name" valkey-cli info replication 2>/dev/null |
    grep -q 'master_link_status:up' && break
  sleep 1
done
docker exec "$valkey_replica_name" valkey-cli info replication |
  grep -q 'master_link_status:up'

coordination_dir="$(mktemp -d)"

postgres_url="postgres://postgres:postgres@127.0.0.1:$postgres_port/lease?sslmode=disable"
POSTGRES_URL="$postgres_url" go test -race -count=1 ./postgres
VALKEY_ADDRESS="127.0.0.1:$valkey_port" go test -race -count=1 ./valkey
POSTGRES_FAULT_URL="$postgres_url" \
  go test -race -count=1 -run TestLiveOperationalFaults ./postgres

POSTGRES_CONTINUITY_URL="$postgres_url" POSTGRES_CONTINUITY_PHASE=seed \
  go test -race -count=1 -run TestLiveFenceContinuity ./postgres
VALKEY_CONTINUITY_ADDRESS="127.0.0.1:$valkey_port" \
VALKEY_CONTINUITY_PHASE=seed \
  go test -race -count=1 -run TestLiveFenceContinuity ./valkey

docker exec "$postgres_name" pg_dump -U postgres -d lease -Fc \
  >"$coordination_dir/postgres.dump"
docker exec "$valkey_name" valkey-cli save >/dev/null
docker cp "$valkey_name:/data/dump.rdb" \
  "$coordination_dir/valkey.rdb" >/dev/null

docker restart "$postgres_name" "$valkey_name" >/dev/null
for _ in {1..60}; do
  docker exec "$postgres_name" pg_isready -U postgres -d lease \
    >/dev/null 2>&1 && break
  sleep 1
done
docker exec "$postgres_name" pg_isready -U postgres -d lease >/dev/null
for _ in {1..60}; do
  docker exec "$valkey_name" valkey-cli ping >/dev/null 2>&1 && break
  sleep 1
done
docker exec "$valkey_name" valkey-cli ping >/dev/null

POSTGRES_CONTINUITY_URL="$postgres_url" POSTGRES_CONTINUITY_PHASE=verify \
  go test -race -count=1 -run TestLiveFenceContinuity ./postgres
VALKEY_CONTINUITY_ADDRESS="127.0.0.1:$valkey_port" \
VALKEY_CONTINUITY_PHASE=verify \
  go test -race -count=1 -run TestLiveFenceContinuity ./valkey

docker exec "$postgres_name" dropdb -U postgres lease
docker exec "$postgres_name" createdb -U postgres lease
docker exec -i "$postgres_name" pg_restore -U postgres -d lease \
  <"$coordination_dir/postgres.dump"
docker stop "$valkey_name" >/dev/null
docker cp "$coordination_dir/valkey.rdb" \
  "$valkey_name:/data/dump.rdb" >/dev/null
docker start "$valkey_name" >/dev/null
for _ in {1..60}; do
  docker exec "$valkey_name" valkey-cli ping >/dev/null 2>&1 && break
  sleep 1
done
docker exec "$valkey_name" valkey-cli ping >/dev/null

POSTGRES_CONTINUITY_URL="$postgres_url" POSTGRES_CONTINUITY_PHASE=rollback \
POSTGRES_CONTINUITY_MAX_TOKEN=2 \
  go test -race -count=1 -run TestLiveFenceContinuity ./postgres
VALKEY_CONTINUITY_ADDRESS="127.0.0.1:$valkey_port" \
VALKEY_CONTINUITY_PHASE=rollback VALKEY_CONTINUITY_MAX_TOKEN=2 \
  go test -race -count=1 -run TestLiveFenceContinuity ./valkey

POSTGRES_CONTINUITY_URL="$postgres_url" POSTGRES_CONTINUITY_PHASE=reset \
  go test -race -count=1 -run TestLiveFenceContinuity ./postgres
VALKEY_CONTINUITY_ADDRESS="127.0.0.1:$valkey_port" \
VALKEY_CONTINUITY_PHASE=reset \
  go test -race -count=1 -run TestLiveFenceContinuity ./valkey

POSTGRES_URL="$postgres_url" go test -race -count=1 ./postgres
VALKEY_ADDRESS="127.0.0.1:$valkey_port" go test -race -count=1 ./valkey

docker pause "$postgres_name" >/dev/null
POSTGRES_PARTITION_URL="$postgres_url" \
  go test -race -count=1 -run TestLivePartitionOutcome ./postgres
docker unpause "$postgres_name" >/dev/null

VALKEY_PARTITION_ADDRESS="127.0.0.1:$valkey_port" \
VALKEY_PARTITION_READY="$coordination_dir/ready" \
VALKEY_PARTITION_TRIGGER="$coordination_dir/trigger" \
  go test -race -count=1 -run TestLivePartitionOutcome ./valkey &
valkey_test_pid=$!
for _ in {1..100}; do
  test -f "$coordination_dir/ready" && break
  sleep 0.1
done
test -f "$coordination_dir/ready"
docker pause "$valkey_name" >/dev/null
touch "$coordination_dir/trigger"
wait "$valkey_test_pid"
valkey_test_pid=
docker unpause "$valkey_name" >/dev/null

POSTGRES_CONTINUITY_URL="$postgres_url" POSTGRES_CONTINUITY_PHASE=seed \
  go test -race -count=1 -run TestLiveFenceContinuity ./postgres
docker exec "$postgres_name" sh -c \
  'printf "%s\n" "host replication postgres samenet scram-sha-256" >> "$PGDATA/pg_hba.conf"'
docker exec "$postgres_name" psql -U postgres -d lease -v ON_ERROR_STOP=1 \
  -c 'SELECT pg_reload_conf()' >/dev/null
docker volume create "$postgres_replica_volume" >/dev/null
docker run --rm --user root \
  -v "$postgres_replica_volume:/var/lib/postgresql" postgres:18 \
  sh -c 'mkdir -p /var/lib/postgresql/18/docker &&
    chown -R postgres:postgres /var/lib/postgresql'
docker run --rm --user postgres --network "$network_name" \
  -v "$postgres_replica_volume:/var/lib/postgresql" postgres:18 \
  pg_basebackup \
    -d "host=$postgres_name user=postgres password=postgres" \
    -D /var/lib/postgresql/18/docker -Fp -Xs -P -R
docker run -d --name "$postgres_replica_name" \
  --network "$network_name" \
  -p "$postgres_replica_port:5432" \
  -v "$postgres_replica_volume:/var/lib/postgresql" postgres:18 >/dev/null
for _ in {1..60}; do
  docker exec "$postgres_replica_name" pg_isready -U postgres -d lease \
    >/dev/null 2>&1 && break
  sleep 1
done
docker exec "$postgres_replica_name" pg_isready -U postgres -d lease >/dev/null
for _ in {1..60}; do
  test "$(docker exec "$postgres_name" psql -U postgres -d lease -Atc \
    "SELECT count(*) FROM pg_stat_replication WHERE state = 'streaming'")" = 1 && break
  sleep 1
done
test "$(docker exec "$postgres_name" psql -U postgres -d lease -Atc \
  "SELECT count(*) FROM pg_stat_replication WHERE state = 'streaming'")" = 1
docker stop "$postgres_name" >/dev/null
docker exec --user postgres "$postgres_replica_name" \
  sh -c 'pg_ctl -D "$PGDATA" promote -w' >/dev/null
postgres_replica_url="postgres://postgres:postgres@127.0.0.1:$postgres_replica_port/lease?sslmode=disable"
POSTGRES_CONTINUITY_URL="$postgres_replica_url" \
POSTGRES_CONTINUITY_PHASE=verify \
  go test -race -count=1 -run TestLiveFenceContinuity ./postgres

VALKEY_CONTINUITY_ADDRESS="127.0.0.1:$valkey_port" \
VALKEY_CONTINUITY_PHASE=seed \
  go test -race -count=1 -run TestLiveFenceContinuity ./valkey
for _ in {1..100}; do
  replica_counter="$({
    docker exec "$valkey_replica_name" valkey-cli --scan \
      --pattern 'lease-continuity:*:counter' |
      head -n 1
  } || true)"
  if test -n "$replica_counter" && \
    test "$(docker exec "$valkey_replica_name" valkey-cli get "$replica_counter")" = 1; then
    break
  fi
  sleep 0.1
done
test -n "$replica_counter"
test "$(docker exec "$valkey_replica_name" valkey-cli get "$replica_counter")" = 1
docker stop "$valkey_name" >/dev/null
docker exec "$valkey_replica_name" valkey-cli replicaof no one >/dev/null
VALKEY_CONTINUITY_ADDRESS="127.0.0.1:$valkey_replica_port" \
VALKEY_CONTINUITY_PHASE=verify \
  go test -race -count=1 -run TestLiveFenceContinuity ./valkey

old_certificates="$coordination_dir/tls-old"
new_certificates="$coordination_dir/tls-new"
for certificate_dir in "$old_certificates" "$new_certificates"; do
  mkdir -p "$certificate_dir"
  openssl req -x509 -newkey rsa:2048 -nodes -days 1 \
    -keyout "$certificate_dir/ca.key" -out "$certificate_dir/ca.crt" \
    -subj "/CN=lease-fault-ca" >/dev/null 2>&1
  openssl req -newkey rsa:2048 -nodes \
    -keyout "$certificate_dir/server.key" \
    -out "$certificate_dir/server.csr" -subj "/CN=localhost" \
    >/dev/null 2>&1
  printf '%s\n' 'subjectAltName=DNS:localhost,IP:127.0.0.1' \
    'extendedKeyUsage=serverAuth' >"$certificate_dir/server.ext"
  openssl x509 -req -days 1 -in "$certificate_dir/server.csr" \
    -CA "$certificate_dir/ca.crt" -CAkey "$certificate_dir/ca.key" \
    -CAcreateserial -out "$certificate_dir/server.crt" \
    -extfile "$certificate_dir/server.ext" >/dev/null 2>&1
  chmod 0644 "$certificate_dir"/*
done

docker volume create "$valkey_secure_volume" >/dev/null
docker run -d --name "$valkey_secure_name" --network "$network_name" \
  -p "$valkey_secure_port:6379" -v "$old_certificates:/certs:ro" \
  -v "$valkey_secure_volume:/data" valkey/valkey:9.0-alpine \
  valkey-server --port 0 --tls-port 6379 \
    --tls-cert-file /certs/server.crt --tls-key-file /certs/server.key \
    --tls-ca-cert-file /certs/ca.crt --tls-auth-clients no \
    --user lease-old-user on '>lease-old' '~*' '+@all' \
    --user default off >/dev/null
for _ in {1..60}; do
  docker exec "$valkey_secure_name" valkey-cli --tls \
    --cacert /certs/ca.crt --user lease-old-user \
    -a lease-old ping >/dev/null 2>&1 && break
  sleep 1
done
docker exec "$valkey_secure_name" valkey-cli --tls \
  --cacert /certs/ca.crt --user lease-old-user \
  -a lease-old ping >/dev/null 2>&1
VALKEY_CONTINUITY_ADDRESS="127.0.0.1:$valkey_secure_port" \
VALKEY_CONTINUITY_PHASE=seed VALKEY_CONTINUITY_PASSWORD=lease-old \
VALKEY_CONTINUITY_USERNAME=lease-old-user \
VALKEY_CONTINUITY_CA_FILE="$old_certificates/ca.crt" \
VALKEY_CONTINUITY_SERVER_NAME=localhost \
  go test -race -count=1 -run TestLiveFenceContinuity ./valkey

docker stop "$valkey_secure_name" >/dev/null
docker rm "$valkey_secure_name" >/dev/null
docker run -d --name "$valkey_secure_name" --network "$network_name" \
  -p "$valkey_secure_port:6379" -v "$new_certificates:/certs:ro" \
  -v "$valkey_secure_volume:/data" valkey/valkey:9.0-alpine \
  valkey-server --port 0 --tls-port 6379 \
    --tls-cert-file /certs/server.crt --tls-key-file /certs/server.key \
    --tls-ca-cert-file /certs/ca.crt --tls-auth-clients no \
    --user lease-new-user on '>lease-new' '~*' '+@all' \
    --user default off >/dev/null
for _ in {1..60}; do
  docker exec "$valkey_secure_name" valkey-cli --tls \
    --cacert /certs/ca.crt --user lease-new-user \
    -a lease-new ping >/dev/null 2>&1 && break
  sleep 1
done
docker exec "$valkey_secure_name" valkey-cli --tls \
  --cacert /certs/ca.crt --user lease-new-user \
  -a lease-new ping >/dev/null 2>&1

VALKEY_CONTINUITY_ADDRESS="127.0.0.1:$valkey_secure_port" \
VALKEY_CONTINUITY_PHASE=reject VALKEY_CONTINUITY_PASSWORD=lease-new \
VALKEY_CONTINUITY_USERNAME=lease-new-user \
VALKEY_CONTINUITY_CA_FILE="$old_certificates/ca.crt" \
VALKEY_CONTINUITY_SERVER_NAME=localhost \
  go test -race -count=1 -run TestLiveFenceContinuity ./valkey
VALKEY_CONTINUITY_ADDRESS="127.0.0.1:$valkey_secure_port" \
VALKEY_CONTINUITY_PHASE=reject VALKEY_CONTINUITY_PASSWORD=lease-old \
VALKEY_CONTINUITY_USERNAME=lease-old-user \
VALKEY_CONTINUITY_CA_FILE="$new_certificates/ca.crt" \
VALKEY_CONTINUITY_SERVER_NAME=localhost \
  go test -race -count=1 -run TestLiveFenceContinuity ./valkey
VALKEY_CONTINUITY_ADDRESS="127.0.0.1:$valkey_secure_port" \
VALKEY_CONTINUITY_PHASE=verify VALKEY_CONTINUITY_PASSWORD=lease-new \
VALKEY_CONTINUITY_USERNAME=lease-new-user \
VALKEY_CONTINUITY_CA_FILE="$new_certificates/ca.crt" \
VALKEY_CONTINUITY_SERVER_NAME=localhost \
  go test -race -count=1 -run TestLiveFenceContinuity ./valkey
