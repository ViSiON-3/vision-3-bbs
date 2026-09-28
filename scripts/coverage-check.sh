#!/usr/bin/env bash
# coverage-check.sh enforces the test-coverage floor in scripts/coverage-floor.
#
# Usage: scripts/coverage-check.sh <coverprofile>
#
# The gated number is statement coverage of internal/ only. cmd/ is mostly
# main() wiring and flag parsing, and third_party/ is vendored upstream code
# in its own modules, so neither is a useful target. Packages with no tests
# still count: since Go 1.22, go test -coverprofile writes zero-count blocks
# for packages with no _test.go files (internal/jsutil is one), so adding code
# without tests lowers the number. go.mod requires a newer Go than that. Both totals are printed so the whole-repo figure stays visible.
#
# The floor is a ratchet. When coverage rises, raise the floor in the same PR;
# the script says so once it is a full point ahead. A small tolerance absorbs
# the jitter from goroutine timing deciding which branches a test run takes.
set -euo pipefail

profile=${1:?usage: coverage-check.sh <coverprofile>}
here=$(cd "$(dirname "$0")" && pwd)
floor=$(grep -v '^#' "$here/coverage-floor" | tr -d '[:space:]')
tolerance=0.2
module=github.com/ViSiON-3/vision-3-bbs

# A block can appear once per test binary that compiles its package, so
# dedupe by position and treat it as covered if any run hit it.
read -r all internal < <(awk -v mod="$module/" '
	NR == 1 { next }
	{
		key = $1
		if (!(key in stmts)) { stmts[key] = $2 }
		if ($3 > 0) { hit[key] = 1 }
	}
	END {
		for (k in stmts) {
			path = k; sub(mod, "", path)
			t += stmts[k]; if (k in hit) c += stmts[k]
			if (path ~ /^internal\//) { it += stmts[k]; if (k in hit) ic += stmts[k] }
		}
		printf "%.1f %.1f\n", (t ? 100 * c / t : 0), (it ? 100 * ic / it : 0)
	}' "$profile")

echo "whole module coverage: ${all}%"
echo "internal/ coverage:    ${internal}% (floor ${floor}%)"

if awk -v c="$internal" -v f="$floor" -v tol="$tolerance" 'BEGIN { exit !(c + tol < f) }'; then
	echo "::error::internal/ coverage ${internal}% is below the ${floor}% floor. Add tests for the code this change adds or touches."
	exit 1
fi
if awk -v c="$internal" -v f="$floor" 'BEGIN { exit !(c >= f + 1) }'; then
	echo "::notice::internal/ coverage ${internal}% is a point or more above the floor. Raise scripts/coverage-floor to lock it in."
fi
