#!/usr/bin/env bash
# Disposable cluster wrapper for the same gate used by CI, plus HTTP benchmark.
set -euo pipefail
cd "$(dirname "$0")/.."
local_mode=${1:-test}
case "$local_mode" in test|load) ;; *) echo 'usage: verify-local-backend.sh test|load'; exit 1;; esac
local_pg_bin=${RECOVERY_PG_BIN:-/www/server/pgsql/bin}
local_results=$(mktemp -d /tmp/gobbs-backend-results.XXXXXX)
local_cluster=$(mktemp -d /tmp/gobbs-backend-cluster.XXXXXX)
local_pg_user=$(id -un)
local_as_pg=()
if [[ $(id -u) == 0 ]]; then local_pg_user=postgres; local_as_pg=(runuser -u postgres --); chown postgres "$local_cluster"; fi
cleanup() {
  "${local_as_pg[@]}" "$local_pg_bin/pg_ctl" -D "$local_cluster/data" -m immediate -w stop >/dev/null 2>&1 || true
  if [[ -f $local_cluster/postgres.log ]]; then cp "$local_cluster/postgres.log" "$local_results/"; fi
  rm -rf -- "$local_cluster"
  echo "Evidence: $local_results"
}
trap cleanup EXIT
echo "Evidence: $local_results"
"${local_as_pg[@]}" "$local_pg_bin/initdb" -D "$local_cluster/data" -A trust --no-locale -E UTF8 > "$local_results/initdb.log"
"${local_as_pg[@]}" "$local_pg_bin/pg_ctl" -D "$local_cluster/data" -l "$local_cluster/postgres.log" -o "-k $local_cluster -h '' -p 55440" -w start > "$local_results/start.log"
"$local_pg_bin/psql" -h "$local_cluster" -p 55440 -U "$local_pg_user" -d postgres -v ON_ERROR_STOP=1 <<'SQL' > "$local_results/create.log"
CREATE ROLE gobbs_test LOGIN NOSUPERUSER NOCREATEDB NOCREATEROLE;
CREATE DATABASE gobbs_test_store OWNER gobbs_test;
CREATE DATABASE gobbs_test_api OWNER gobbs_test;
CREATE DATABASE gobbs_test_migrations OWNER gobbs_test;
SQL
export FORUM_STORE_TEST_DSN="host=$local_cluster port=55440 user=gobbs_test dbname=gobbs_test_store sslmode=disable"
export FORUM_API_TEST_DSN="host=$local_cluster port=55440 user=gobbs_test dbname=gobbs_test_api sslmode=disable"
export FORUM_MIGRATION_TEST_DSN="host=$local_cluster port=55440 user=gobbs_test dbname=gobbs_test_migrations sslmode=disable"
export FORUM_TEST_RESULTS_DIR=$local_results
unset FORUM_SMTP_HOST FORUM_SMTP_USER FORUM_SMTP_PASS FORUM_SMTP_FROM
if [[ $local_mode == test ]]; then
  bash scripts/test-backend.sh
else
  FORUM_REQUIRE_TEST_DB=1 FORUM_TEST_ASYNC=1 FORUM_TEST_DSN="$FORUM_API_TEST_DSN" \
    go test ./internal/api -run '^$' -bench '^BenchmarkForumTraffic$' -benchtime=1x -count=1 -timeout=8m -v > "$local_results/load.log" 2>&1
  rg 'LOAD|PASS|FAIL' "$local_results/load.log"
fi
