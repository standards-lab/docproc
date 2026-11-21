#!/usr/bin/env bash
# currency reports, or upgrades, one Rust system tool's pins: its crates, direct and indirect, the
# crates of its locked graph with a RustSec advisory, and the mise tools its directory's mise files
# pin. A pin the tool's HOLDS list names is reported as held and never moved. report prints each
# line prefixed with the tool's directory and exits 1 when it reports any pin that isn't held. Each
# Rust tool's currency and upgrade tasks run it by the tool's name.
#
# usage: tools/currency.sh <format> <report|upgrade>
#   format  the Rust tool's directory under tools/, such as mail
# See docs/tool-builds.md
# See architecture/principles/rolling-currency.md
set -euo pipefail
cd "$(dirname "$0")/.."

if [ $# -ne 2 ] || { [ "$2" != report ] && [ "$2" != upgrade ]; }; then
	echo "usage: tools/currency.sh <format> <report|upgrade>" >&2
	exit 2
fi
format=$1
action=$2
tool=tools/$format
dir=$PWD/$tool
if [ ! -f "$tool/mise.toml" ] || [ ! -f "$tool/Cargo.toml" ]; then
	echo "currency: $tool is not a Rust tool: it has no mise.toml or no Cargo.toml" >&2
	exit 2
fi
holds=$(mise -C "$tool" env --json | jq -r '.HOLDS // empty')

held() {
	grep -m1 "^$1 $2: " <<<"$holds" | cut -d: -f2- | sed 's/^ //' || true
}

cargo() {
	# shellcheck disable=SC2016 # $CARGO_HOME expands in mise's environment, not this one
	mise -C "$tool" exec -- bash -c 'export PATH="$CARGO_HOME/bin:$PATH"; exec cargo "$@"' bash "$@"
}

escape_regex() {
	# shellcheck disable=SC2001 # a bracket expression, which a ${//} pattern has no escape for
	sed 's/[][\\.*^$|+?(){}]/\\&/g' <<<"$1"
}

mise_envs() {
	local file
	for file in "$tool"/mise.*.toml; do
		if [ ! -e "$file" ]; then
			continue
		fi
		file=${file##*/mise.}
		file=${file%.toml}
		if [ "$file" != local ]; then
			echo "$file"
		fi
	done | paste -sd, -
}

outdated_mise_tools() {
	local envs
	envs=$(mise_envs) || return
	MISE_ENV=$envs mise -C "$tool" outdated --bump --json |
		jq -r --arg dir "$dir" 'to_entries[] |
			select(.value.bump != null and (.value.source.path | startswith($dir + "/mise."))) |
			"\(.value.source.path | split("/") | last) \(.key) \(.value.requested) \(.value.bump)"'
}

outdated_crates() {
	cargo update --dry-run --verbose 2>&1 |
		sed -nE 's/^ *(Updating|Unchanged) +([^ ]+) +v([^ ]+)( -> v([^ ]+)| \(available: v([^)]+)\))$/\2 \3 \5\6/p'
}

crates_beyond_requirements() {
	cargo update --dry-run --verbose 2>&1 |
		sed -nE 's/^ *Unchanged +([^ ]+) +v([^ ]+) \(available: v([^)]+)\)$/\1 \2 \3/p'
}

crates_within_requirements() {
	cargo update --dry-run --verbose 2>&1 |
		sed -nE 's/^ *Updating +([^ ]+) +v([^ ]+) -> v[^ ]+$/\1 \2/p'
}

direct_requirements() {
	# shellcheck disable=SC2016 # jq's variable
	local requires='.packages[] | .manifest_path as $manifest | .dependencies[] | select(.path == null) |
		"\(.name) \($manifest)"'
	local paths manifest
	paths=$(cargo metadata --format-version 1 --no-deps |
		jq -r '.packages[].dependencies[] | select(.path != null) | "\(.path)/Cargo.toml"') || return
	cargo metadata --format-version 1 --no-deps | jq -r "$requires" || return
	while read -r manifest; do
		if [ -n "$manifest" ]; then
			cargo metadata --format-version 1 --no-deps --manifest-path "$manifest" | jq -r "$requires" || return
		fi
	done <<<"$paths"
}

stale=0
report_line() {
	echo "$tool: $1"
	stale=1
}

trailing() {
	local reason
	reason=$(held "$2" "$3")
	if [ -n "$reason" ]; then
		echo "$tool: $1: $2 $3 -> $4 held: $reason"
	else
		report_line "$1: $2 $3 -> $4"
	fi
}

report_crates() {
	local scanned name pinned latest
	scanned=$(outdated_crates) || return
	while read -r name pinned latest; do
		if [ -n "$name" ]; then trailing Cargo.lock "$name" "$pinned" "$latest"; fi
	done <<<"$scanned"
}

report_advisories() {
	local advisories line
	# shellcheck disable=SC2016 # $CARGO_HOME expands in mise's environment, not this one
	if ! advisories=$(mise -C "$tool" exec -- bash -c \
		'export PATH="$CARGO_HOME/bin:$PATH"; cargo deny --color never check advisories' 2>&1); then
		while read -r line; do
			if [ -n "$line" ]; then report_line "Cargo.lock: $line"; fi
		done < <(grep -E '^(error|warning)\[' <<<"$advisories")
	fi
}

report_mise_tools() {
	local scanned file name pinned latest
	scanned=$(outdated_mise_tools) || return
	while read -r file name pinned latest; do
		if [ -n "$file" ]; then trailing "$file" "$name" "$pinned" "$latest"; fi
	done <<<"$scanned"
}

upgrade_mise_tools() {
	local scanned file name pinned latest key
	scanned=$(outdated_mise_tools) || return
	while read -r file name pinned latest; do
		if [ -z "$file" ] || [ -n "$(held "$name" "$pinned")" ]; then
			continue
		fi
		key=$(escape_regex "$name") || return
		sed -i -E "s|^(\"?$key\"? = (\\{ version = )?\")$pinned\"|\\1$latest\"|" "$tool/$file"
	done <<<"$scanned"
}

raise_requirements_beyond() {
	local direct beyond name pinned latest manifest
	direct=$(direct_requirements) || return
	beyond=$(crates_beyond_requirements) || return
	while read -r name pinned latest; do
		if [ -z "$name" ] || [ -n "$(held "$name" "$pinned")" ]; then
			continue
		fi
		while read -r manifest; do
			sed -i -E "s/^($name = (\\{ version = )?\"=?)[^\"]+\"/\\1$latest\"/" "$manifest"
		done < <(sed -n "s/^$name //p" <<<"$direct" | sort -u)
	done <<<"$beyond"
}

update_crates_within() {
	local within name pinned
	within=$(crates_within_requirements) || return
	while read -r name pinned; do
		if [ -z "$name" ] || [ -n "$(held "$name" "$pinned")" ]; then
			continue
		fi
		if ! cargo pkgid --quiet "$name@$pinned" >/dev/null 2>&1; then
			continue
		fi
		cargo update --quiet "$name@$pinned"
	done <<<"$within"
}

if [ "$action" = report ]; then
	report_crates
	report_advisories
	report_mise_tools
	exit "$stale"
fi
upgrade_mise_tools
raise_requirements_beyond
update_crates_within
