#!/usr/bin/env bash
# Creates its own PostgreSQL cluster; never accepts a business DSN or PGDATA.
set -euo pipefail
cd "$(dirname "$0")/.."
recovery_pg_bin=${RECOVERY_PG_BIN:-/www/server/pgsql/bin}
recovery_out=$(mktemp -d /tmp/gobbs-recovery-results.XXXXXX)
recovery_tmp=$(mktemp -d /tmp/gobbs-recovery-cluster.XXXXXX)
recovery_pid=
recovery_smtp_pid=
recovery_pg_user=$(id -un)
recovery_as_pg=()
if [[ $(id -u) == 0 ]]; then
  recovery_pg_user=postgres
  recovery_as_pg=(runuser -u postgres --)
  chown postgres "$recovery_tmp"
fi
cleanup() {
  if [[ -n $recovery_pid ]]; then kill -KILL "$recovery_pid" 2>/dev/null || true; wait "$recovery_pid" 2>/dev/null || true; fi
  if [[ -n $recovery_smtp_pid ]]; then kill -KILL "$recovery_smtp_pid" 2>/dev/null || true; wait "$recovery_smtp_pid" 2>/dev/null || true; fi
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
export FORUM_REQUIRE_TEST_DB=1
# The harness owns all endpoints, regardless of the invoking shell's deployment env.
unset FORUM_SMTP_HOST FORUM_SMTP_USER FORUM_SMTP_PASS FORUM_SMTP_FROM
# Race reports remain fatal; skip the default one-second exit delay for each probe.
export GORACE="halt_on_error=1 atexit_sleep_ms=0"
go test -race ./internal/db -count=1 -v > "$recovery_out/migrations.log" 2>&1
go test -race ./internal/store -run '^TestSearchIndexFailureRetryAndRecovery$' -count=1 -v > "$recovery_out/retry.log" 2>&1
go test -race ./internal/api -run '^(TestEmail|TestSettingsEmail|TestRegistrationResetAndEmailVerification)' -count=1 -v > "$recovery_out/email-api.log" 2>&1
if rg -q -- '--- SKIP|--- FAIL' "$recovery_out/migrations.log" "$recovery_out/retry.log" "$recovery_out/email-api.log"; then echo 'Unexpected skipped or failed test'; exit 1; fi
recovery_sql=("$recovery_pg_bin/psql" "$FORUM_TEST_DSN" -v ON_ERROR_STOP=1)
go test -race ./scripts/recovery-probe ./internal/mailtest ./internal/mail -count=1 -v > "$recovery_out/probe-tests.log" 2>&1
go build -race -o "$recovery_tmp/probe" ./scripts/recovery-probe
go version > "$recovery_out/toolchain.log"
"$recovery_pg_bin/psql" --version >> "$recovery_out/toolchain.log"

start_worker() {
  "$recovery_tmp/probe" worker "$recovery_target" > "$recovery_case/$1.log" 2>&1 &
  recovery_pid=$!
}
stop_worker() {
  kill -TERM "$recovery_pid"
  for ((i=0; i<150; i++)); do
    if ! kill -0 "$recovery_pid" 2>/dev/null; then break; fi
    sleep 0.1
  done
  if kill -0 "$recovery_pid" 2>/dev/null; then echo 'Worker did not stop within 15 seconds'; return 1; fi
  wait "$recovery_pid"
  recovery_pid=
}
probe() {
  "$recovery_tmp/probe" "$1" "$recovery_target" 2>&1 | tee -a "$recovery_case/checks.log"
}

# Each target gets both faults in its own fresh schema and loopback SMTP process.
for recovery_target in search forum growth titles subscriptions email; do
  for recovery_fault in kill disconnect; do
    recovery_case="$recovery_out/$recovery_target-$recovery_fault"
    mkdir "$recovery_case"
    export RECOVERY_SMTP_DIR="$recovery_case"
    echo "CASE $recovery_target / $recovery_fault"
    "${recovery_sql[@]}" -c 'DROP SCHEMA public CASCADE; CREATE SCHEMA public' > "$recovery_case/reset.log" 2>&1
    "$recovery_tmp/probe" smtp "$recovery_case" > "$recovery_case/smtp.log" 2>&1 &
    recovery_smtp_pid=$!
    for ((i=0; i<100; i++)); do
      [[ -s "$recovery_case/smtp-port" ]] && break
      kill -0 "$recovery_smtp_pid"
      sleep 0.1
    done
    [[ -s "$recovery_case/smtp-port" ]]
    "$recovery_tmp/probe" seed "$recovery_target" > "$recovery_case/seed.log" 2>&1
    probe inject
    start_worker worker-before-fault
    probe wait-locked
    if [[ $recovery_fault == kill ]]; then
      kill -KILL "$recovery_pid"
      wait "$recovery_pid" 2>/dev/null || true
      recovery_pid=
      # Only this script's private cluster: release abandoned transaction locks.
      "${recovery_admin[@]}" -c "SELECT pg_terminate_backend(pid) FROM pg_stat_activity WHERE datname='gobbs_test_worker'" > "$recovery_case/terminate.log"
      probe rollback
      probe release
      start_worker worker-restarted
    else
      "${recovery_as_pg[@]}" "$recovery_pg_bin/pg_ctl" -D "$recovery_tmp/data" -m immediate -w stop > "$recovery_case/db-stop.log"
      sleep 2
      kill -0 "$recovery_pid"
      "${recovery_as_pg[@]}" "$recovery_pg_bin/pg_ctl" -D "$recovery_tmp/data" -l "$recovery_tmp/postgres.log" -o "-k $recovery_tmp -h '' -p 55439" -w start > "$recovery_case/db-restart.log"
      probe rollback
      probe release
      # Same PID must reconnect; do not replace or restart the worker.
      kill -0 "$recovery_pid"
    fi
    probe verify
    stop_worker
    probe replay
    start_worker worker-replay
    probe verify
    stop_worker
    kill -TERM "$recovery_smtp_pid"
    wait "$recovery_smtp_pid"
    recovery_smtp_pid=
    # Verify final persistent state after all producer and worker processes stop.
    probe verify
    if rg -q 'WARNING: DATA RACE' "$recovery_case"/*.log; then echo 'Data race detected'; exit 1; fi
    echo "PASS $recovery_target / $recovery_fault" | tee -a "$recovery_out/summary.log"
  done
done
echo 'PASS: 12 process/database fault cases; rollback, recovery, award/fanout redelivery, real loopback SMTP and stable Message-ID'
