#!/usr/bin/env bash
# Creates its own PostgreSQL cluster; never accepts a business DSN or PGDATA.
set -euo pipefail
cd "$(dirname "$0")/.."
recovery_pg_bin=${RECOVERY_PG_BIN:-/www/server/pgsql/bin}
recovery_out=$(mktemp -d /tmp/gobbs-recovery-results.XXXXXX)
recovery_tmp=$(mktemp -d /tmp/gobbs-recovery-cluster.XXXXXX)
recovery_pid=
recovery_pg_user=$(id -un)
recovery_as_pg=()
if [[ $(id -u) == 0 ]]; then
  recovery_pg_user=postgres
  recovery_as_pg=(runuser -u postgres --)
  chown postgres "$recovery_tmp"
fi
cleanup() {
  if [[ -n $recovery_pid ]]; then kill -KILL "$recovery_pid" 2>/dev/null || true; wait "$recovery_pid" 2>/dev/null || true; fi
  "${recovery_as_pg[@]}" "$recovery_pg_bin/pg_ctl" -D "$recovery_tmp/data" -m immediate -w stop >/dev/null 2>&1 || true
  if [[ -f $recovery_tmp/postgres.log ]]; then cp "$recovery_tmp/postgres.log" "$recovery_out/"; fi
  rm -rf -- "$recovery_tmp"
  echo "Evidence: $recovery_out"
}
trap cleanup EXIT
echo "Evidence: $recovery_out"
"${recovery_as_pg[@]}" "$recovery_pg_bin/initdb" -D "$recovery_tmp/data" -A trust --no-locale -E UTF8 > "$recovery_out/initdb.log"
"${recovery_as_pg[@]}" "$recovery_pg_bin/pg_ctl" -D "$recovery_tmp/data" -l "$recovery_tmp/postgres.log" -o "-k $recovery_tmp -h '' -p 55439" -w start > "$recovery_out/start.log"
recovery_admin=("$recovery_pg_bin/psql" -h "$recovery_tmp" -p 55439 -U "$recovery_pg_user" -d postgres -v ON_ERROR_STOP=1)
"${recovery_admin[@]}" -c 'CREATE ROLE gobbs_test_worker LOGIN NOSUPERUSER NOCREATEDB NOCREATEROLE' -c 'CREATE DATABASE gobbs_test_worker OWNER gobbs_test_worker' > "$recovery_out/create.log"
export FORUM_TEST_DSN="host=$recovery_tmp port=55439 user=gobbs_test_worker dbname=gobbs_test_worker sslmode=disable"
export FORUM_MIGRATION_TEST_DSN="$FORUM_TEST_DSN"
go test -race ./internal/db -count=1 -v > "$recovery_out/migrations.log" 2>&1
go test -race ./internal/store -run '^TestSearchIndexFailureRetryAndRecovery$' -count=1 -v > "$recovery_out/retry.log" 2>&1
if rg -q -- '--- SKIP|--- FAIL' "$recovery_out/migrations.log" "$recovery_out/retry.log"; then echo 'Unexpected skipped or failed test'; exit 1; fi
recovery_sql=("$recovery_pg_bin/psql" "$FORUM_TEST_DSN" -v ON_ERROR_STOP=1)
"${recovery_sql[@]}" -c 'DROP SCHEMA public CASCADE; CREATE SCHEMA public' > "$recovery_out/reset.log" 2>&1
go build -o "$recovery_tmp/probe" ./scripts/recovery-probe
"$recovery_tmp/probe" seed > "$recovery_out/seed.log" 2>&1
"${recovery_sql[@]}" <<'SQL' > "$recovery_out/inject.log"
CREATE FUNCTION recovery_pause() RETURNS trigger LANGUAGE plpgsql AS $$ BEGIN PERFORM pg_sleep(30); RETURN NEW; END $$;
CREATE TRIGGER recovery_pause BEFORE UPDATE OF search_data ON posts FOR EACH ROW EXECUTE FUNCTION recovery_pause();
SQL
"$recovery_tmp/probe" worker > "$recovery_out/worker-killed.log" 2>&1 &
recovery_pid=$!
"$recovery_tmp/probe" wait-locked
kill -KILL "$recovery_pid"
wait "$recovery_pid" 2>/dev/null || true
recovery_pid=
# Terminate only this disposable cluster's abandoned in-flight query.
"${recovery_admin[@]}" -c "SELECT pg_terminate_backend(pid) FROM pg_stat_activity WHERE datname='gobbs_test_worker'" > "$recovery_out/terminate.log"
"${recovery_sql[@]}" <<'SQL' > "$recovery_out/restart-check.log"
DO $$ BEGIN IF (SELECT count(*) FROM search_index_events) <> 2 THEN RAISE EXCEPTION 'lost claimed events'; END IF; END $$;
DROP TRIGGER recovery_pause ON posts;
SQL
"$recovery_tmp/probe" worker > "$recovery_out/worker-restarted.log" 2>&1 &
recovery_pid=$!
"$recovery_tmp/probe" verify
kill -TERM "$recovery_pid"
wait "$recovery_pid"
recovery_pid=
"$recovery_tmp/probe" seed >> "$recovery_out/seed.log" 2>&1
"${recovery_sql[@]}" -c 'CREATE TRIGGER recovery_pause BEFORE UPDATE OF search_data ON posts FOR EACH ROW EXECUTE FUNCTION recovery_pause()' > /dev/null
"$recovery_tmp/probe" worker > "$recovery_out/worker-disconnect.log" 2>&1 &
recovery_pid=$!
"$recovery_tmp/probe" wait-locked
"${recovery_as_pg[@]}" "$recovery_pg_bin/pg_ctl" -D "$recovery_tmp/data" -m immediate -w stop > "$recovery_out/db-stop.log"
sleep 2
kill -0 "$recovery_pid"
"${recovery_as_pg[@]}" "$recovery_pg_bin/pg_ctl" -D "$recovery_tmp/data" -l "$recovery_tmp/postgres.log" -o "-k $recovery_tmp -h '' -p 55439" -w start > "$recovery_out/db-restart.log"
"${recovery_sql[@]}" -c 'DROP TRIGGER recovery_pause ON posts; DROP FUNCTION recovery_pause()' > /dev/null
"$recovery_tmp/probe" verify
kill -TERM "$recovery_pid"
wait "$recovery_pid"
recovery_pid=
echo 'PASS: migration replay, SQL retry, killed worker restart, live worker database crash/reconnect, final search and forum counts'
