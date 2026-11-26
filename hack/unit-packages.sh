#!/usr/bin/env bash

set -euo pipefail

go_bin=${GO:-go}
all_packages=$("$go_bin" list ./...)
unit_packages=$(while IFS= read -r package; do
	case "$package" in
	""|*/e2e-tests/tests|*/e2e-tests/tests/*)
		;;
	*)
		printf '%s\n' "$package"
		;;
	esac
done <<< "$all_packages")

if [[ -z "$unit_packages" ]]; then
	echo "no cluster-independent Go packages were discovered" >&2
	exit 1
fi

printf '%s\n' "$unit_packages"
