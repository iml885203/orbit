#!/usr/bin/env bash
# Integration tests for example.db persistence guarantees.
#
# Verifies that:
#   1. User modifications survive container recreation (docker rm + orbit up).
#   2. User DB files live in the volume, not the container overlay layer.
#
# (Single-DB clean restore is now `orbit db publish <db> --clean`, which needs
# the host dotnet/sqlpackage toolchain + a snapshot baseline — out of scope for
# this pure docker/sqlcmd persistence script; it is exercised by the publish
# acceptance flow instead.)
#
# These tests mutate state and share a single fresh_start to keep total
# runtime under ~1 minute. They run in order and stop on the first failure.

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ORBIT_ROOT="$(cd "$SCRIPT_DIR/.." && pwd)"
ORBIT_BIN="${ORBIT_BIN:-$ORBIT_ROOT/bin/orbit}"
CONTAINER="orbit-sql-server"
VOLUME="orbit_sql_server"
SA_PASSWORD="${SA_PASSWORD:-example@2024}"
MARKER_SP="Orbit_Persistence_Test_Marker"

red() { printf '\033[31m%s\033[0m\n' "$*"; }
green() { printf '\033[32m%s\033[0m\n' "$*"; }
bold() { printf '\033[1m%s\033[0m\n' "$*"; }

fresh_start() {
  "$ORBIT_BIN" down >/dev/null 2>&1 || true
  docker volume rm "$VOLUME" >/dev/null 2>&1 || true
  "$ORBIT_BIN" up sql-server >/dev/null
  wait_for_dbs_attached 16
}

wait_for_dbs_attached() {
  local expected="$1"
  for _ in $(seq 1 60); do
    local count
    count=$(docker exec "$CONTAINER" /opt/mssql-tools18/bin/sqlcmd \
      -S localhost -U sa -P "$SA_PASSWORD" -C -h -1 \
      -Q "SET NOCOUNT ON; SELECT COUNT(*) FROM sys.databases WHERE database_id > 4" \
      2>/dev/null | tr -d ' \r\n' || true)
    if [[ "$count" == "$expected" ]]; then
      return 0
    fi
    sleep 1
  done
  red "  timed out waiting for $expected DBs"
  return 1
}

create_marker_sp() {
  local db="$1"
  # CREATE PROCEDURE must be the first statement in its batch, so target the
  # DB via -d rather than prepending "USE [db];" in the same -Q.
  docker exec "$CONTAINER" /opt/mssql-tools18/bin/sqlcmd \
    -S localhost -U sa -P "$SA_PASSWORD" -C -d "$db" \
    -Q "CREATE OR ALTER PROCEDURE [dbo].[$MARKER_SP] AS SELECT 1" >/dev/null
}

assert_marker_present() {
  local db="$1"
  local got
  got=$(docker exec "$CONTAINER" /opt/mssql-tools18/bin/sqlcmd \
    -S localhost -U sa -P "$SA_PASSWORD" -C -h -1 \
    -Q "SET NOCOUNT ON; SELECT COUNT(*) FROM [$db].sys.procedures WHERE name = '$MARKER_SP'" \
    | tr -d ' \r\n')
  if [[ "$got" != "1" ]]; then
    red "  assert_marker_present($db): expected 1, got $got"
    return 1
  fi
}

assert_db_files_in_volume() {
  local db="$1"
  local path
  path=$(docker exec "$CONTAINER" /opt/mssql-tools18/bin/sqlcmd \
    -S localhost -U sa -P "$SA_PASSWORD" -C -h -1 \
    -Q "SET NOCOUNT ON; SELECT physical_name FROM sys.master_files WHERE database_id = DB_ID('$db') AND type = 0" \
    | tr -d ' \r')
  if [[ "$path" != /var/opt/mssql/* ]]; then
    red "  assert_db_files_in_volume($db): $db attached at $path (not in /var/opt/mssql/)"
    return 1
  fi
}

# ---------------------------------------------------------------
# Tests
# ---------------------------------------------------------------

test_db_files_in_volume() {
  bold "test_db_files_in_volume"
  assert_db_files_in_volume AppDB
  green "  ok"
}

test_modifications_survive_container_recreate() {
  bold "test_modifications_survive_container_recreate"
  create_marker_sp AppDB
  create_marker_sp AccountingDB
  assert_marker_present AppDB
  assert_marker_present AccountingDB

  # Matches the real failure mode the user reported: orbit restart
  # recreates the container (docker stop + new docker run), so any state
  # living in /app/mssql/data would be lost.
  "$ORBIT_BIN" restart sql-server >/dev/null
  wait_for_dbs_attached 16

  assert_marker_present AppDB
  assert_marker_present AccountingDB
  green "  ok"
}

main() {
  fresh_start
  test_db_files_in_volume
  test_modifications_survive_container_recreate
  echo
  green "All persistence tests passed."
}

main "$@"
