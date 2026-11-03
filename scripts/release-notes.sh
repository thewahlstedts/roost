#!/bin/sh
# Print the CHANGELOG.md section for a version tag (e.g. v0.5.0).
# Fails if there is no "## v0.5.0" section with notes in it.
set -eu
tag="${1:?usage: release-notes.sh vX.Y.Z}"
file="${2:-CHANGELOG.md}"
notes=$(awk -v h="## $tag" '
	index($0, h) == 1 && (length($0) == length(h) || substr($0, length(h) + 1, 1) == " ") { on = 1; next }
	/^## / && on { exit }
	on { print }
' "$file")
if [ -z "$(printf '%s' "$notes" | tr -d '[:space:]')" ]; then
	echo "CHANGELOG.md has no notes under '## $tag' — rename 'Unreleased' to it before tagging." >&2
	exit 1
fi
printf '%s\n' "$notes"
