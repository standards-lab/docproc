#!/usr/bin/env bash
# mboxrd writes the MIME group's mboxrd: the eight generated .eml under
# tests/fixtures/generated/eml and the group's own under tests/fixtures/mime/eml,
# in name order, so their ordinals are 1 to 9. Each message is its source byte
# for byte, but that every line matching ^>*From  gains one ">", after one fixed
# separator line; a blank line ends it. The check rebuilds the file and compares
# it byte for byte with the committed one.
#
# usage: scripts/mboxrd.sh [output]
#   output  the file written, tests/fixtures/mime/nine.mboxrd by default
set -euo pipefail
export LC_ALL=C

if [ $# -gt 1 ]; then
	echo "usage: scripts/mboxrd.sh [output]" >&2
	exit 2
fi

absolute_output() {
	case $1 in
	/*) echo "$1" ;;
	*) echo "$PWD/$1" ;;
	esac
}

sources_by_file_name() {
	local source
	for source in tests/fixtures/generated/eml/*.eml tests/fixtures/mime/eml/*.eml; do
		printf '%s\t%s\n' "$(basename "$source")" "$source"
	done | sort | cut -f2
}

output=tests/fixtures/mime/nine.mboxrd
if [ $# -eq 1 ]; then
	output=$(absolute_output "$1")
fi
cd "$(dirname "$0")/.."
mapfile -t sources < <(sources_by_file_name)

{
	for source in "${sources[@]}"; do
		printf 'From MAILER-DAEMON Thu Jan  1 00:00:00 1970\n'
		sed -e '/^>*From /s/^/>/' "$source"
		printf '\n'
	done
} >"$output"
