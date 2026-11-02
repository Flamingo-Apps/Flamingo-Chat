#!/usr/bin/env bash
# Runs the tests for known defects (build tag "defect", see CONTRIBUTING.md,
# "Tests for known defects") in every service and says which defects are still
# open. A failing test here means the defect is still there. A test that
# PASSES means the defect has been fixed: remove its `//go:build defect` line
# so it joins the normal suite, and this script exits 1 until you do.
#
# Some tests need a scratch Postgres or Redis. They skip when these are unset,
# and this script refuses anything but a local address, so it can never be
# pointed at a development or production database:
#
#   SCRATCH_POSTGRES_URL=postgres://user:pass@localhost:55433/scratch?sslmode=disable
#   SCRATCH_REDIS_ADDR=localhost:56379
#
# usage: scripts/run-defect-tests.sh
set -uo pipefail
cd "$(dirname "$0")/.."

is_local() { case "$1" in *@localhost[:/]*|*@127.0.0.1[:/]*|localhost:*|127.0.0.1:*) return 0 ;; *) return 1 ;; esac; }
if [ -n "${SCRATCH_POSTGRES_URL:-}" ] && ! is_local "$SCRATCH_POSTGRES_URL"; then
	echo "Refusing: SCRATCH_POSTGRES_URL is not a local address." >&2; exit 2
fi
if [ -n "${SCRATCH_REDIS_ADDR:-}" ] && ! is_local "$SCRATCH_REDIS_ADDR"; then
	echo "Refusing: SCRATCH_REDIS_ADDR is not a local address." >&2; exit 2
fi

open=0; fixed=0; skipped=0; broken=0
while IFS= read -r dir; do
	mod=$(echo "$dir" | cut -d/ -f1-2)
	pkg="./${dir#"$mod"/}"
	out=$(cd "$mod" && GOTOOLCHAIN=local go test -tags defect -run TestDefect -count=1 -v "$pkg" 2>&1)
	if echo "$out" | grep -qE "build failed|\[setup failed\]"; then
		echo "BROKEN  $dir (does not compile)"; broken=$((broken + 1)); continue
	fi
	while IFS= read -r line; do
		name=$(echo "$line" | awk '{print $3}')
		case "$line" in
			*"--- FAIL"*) echo "OPEN    $dir  $name"; open=$((open + 1)) ;;
			*"--- PASS"*) echo "FIXED   $dir  $name   <- remove its //go:build defect line"; fixed=$((fixed + 1)) ;;
			*"--- SKIP"*) echo "SKIPPED $dir  $name   (needs SCRATCH_POSTGRES_URL or SCRATCH_REDIS_ADDR)"; skipped=$((skipped + 1)) ;;
		esac
	done < <(echo "$out" | grep -E "^--- (FAIL|PASS|SKIP): TestDefect")
done < <(grep -rl --include='defect*_test.go' '^//go:build defect' services | xargs -n1 dirname | sort -u)

echo
echo "defects still open: $open | fixed, tag to remove: $fixed | skipped for lack of a scratch database: $skipped | broken: $broken"
[ "$fixed" -eq 0 ] && [ "$broken" -eq 0 ]
