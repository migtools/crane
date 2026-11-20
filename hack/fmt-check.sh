#!/usr/bin/env bash

set -euo pipefail

# Keep git's exit status and handle filenames containing whitespace or newlines.
file_list=$(mktemp)
trap 'rm -f "$file_list"' EXIT
git ls-files -z -- '*.go' > "$file_list"
go_files=()
while IFS= read -r -d '' file; do
	# The index still lists unstaged deletions and the old paths of renames.
	if [[ -f "$file" ]]; then
		go_files+=("$file")
	fi
done < "$file_list"
if [[ ${#go_files[@]} -eq 0 ]]; then
	exit 0
fi

offending_files=$(gofmt -l -- "${go_files[@]}")
if [[ -n "$offending_files" ]]; then
	echo "gofmt would change the following tracked Go files:"
	printf '%s\n' "$offending_files"
	exit 1
fi
