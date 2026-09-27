#!/usr/bin/env bash
set -euo pipefail
cd "$(dirname "$0")/.."
test_results=${FORUM_TEST_RESULTS_DIR:-$(mktemp -d /tmp/gobbs-tests.XXXXXX)}
mkdir -p "$test_results"
go run ./scripts/api-contract -check
go run ./scripts/test-report validate-databases
export FORUM_REQUIRE_TEST_DB=1
unset FORUM_TEST_ASYNC
echo "Test evidence: $test_results"
for test_package in store api db; do
  if FORUM_TEST_DSN= FORUM_MIGRATION_TEST_DSN= go test "./internal/$test_package" -run '^$' -count=1 > "$test_results/missing-$test_package.log" 2>&1; then
    echo "Missing database incorrectly accepted by $test_package"; exit 1
  fi
  if ! rg -q 'required .* test database is missing' "$test_results/missing-$test_package.log"; then cat "$test_results/missing-$test_package.log"; exit 1; fi
done
run_tests() {
  local label=$1
  shift
  if ! go test -race -count=1 -timeout=10m -json "$@" > "$test_results/$label.json" 2>&1; then
    tail -40 "$test_results/$label.json"
    return 1
  fi
  local packages
  packages=$(go list "$@")
  go run ./scripts/test-report "$test_results/$label.json" $packages
}
FORUM_TEST_DSN=$FORUM_STORE_TEST_DSN run_tests store ./internal/store
FORUM_TEST_DSN=$FORUM_API_TEST_DSN run_tests api ./internal/api
run_tests migrations ./internal/db
mapfile -t test_packages < <(go list -f '{{if or .TestGoFiles .XTestGoFiles}}{{.ImportPath}}{{end}}' ./... | sed '/^$/d' | rg -v '^dzforum/internal/(store|api|db)$')
if ((${#test_packages[@]})); then run_tests unit "${test_packages[@]}"; fi
go vet ./...
go build ./...
unformatted=$(gofmt -l $(git ls-files '*.go') $(git ls-files --others --exclude-standard '*.go'))
if [[ -n $unformatted ]]; then echo "$unformatted"; exit 1; fi
git diff --check
echo 'PASS: backend test gate'
