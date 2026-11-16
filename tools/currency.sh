#!/usr/bin/env bash
# currency reports, or upgrades, one Rust system tool's pins: its crates, direct and indirect,
# within their requirements and beyond them; the crates of its locked graph with a RustSec
# advisory; and the mise tools its directory's mise files pin, Rust among them. A pin the tool's
# HOLDS list names (its mise.toml's [env]) is reported as held and never moved. Each Rust tool's
# currency and upgrade tasks run it by the tool's name; a tool's pins of its own beyond these,
# such as docproc-mail's fixture generator's, stay in its tasks. The workflows' action pins are
# docproc's root currency's. It reads the network, so CI never runs it.
#
# report prints one line for each pin behind its latest, prefixed with the tool's directory, a
# held one with its hold's reason, and exits 1 when any it reports is not held. It writes only
# cargo-deny's advisory database.
#
# upgrade moves every pin report shows trailing to its latest, but a held one, so a report after
# it shows none of them but the held, and an advisory only a newer release of a crate can clear.
# It rewrites the tool's mise files in place, then, in a new mise process with the toolchain just
# pinned, its crates' requirements and its Cargo.lock. The requirements it raises include those
# of every path crate in the tool's graph: the shared envelope crate, tools/envelope, has no lock
# of its own, so each Rust tool's upgrade raises its requirements by the same rules as the tool's
# and then re-locks the tool's own graph. Run in turn, the second tool finds the envelope's
# requirements already at their latest and only re-locks to them, so no tool's locked check
# trails the crate. A bump of the envelope's ICU4X crates that moves the Unicode version fails
# its unit test asserting it, so it stays a reviewed change. Run it, review the diff, and run each
# Rust tool's check.
#
# usage: tools/currency.sh <format> <report|upgrade>
#   format  the Rust tool's directory under tools/, such as mail
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

# The hold list: one line each, "<item> <pin>: <reason>", the item named as this script names it.
holds=$(mise -C "$tool" env --json | jq -r '.HOLDS // empty')
# held prints the reason a pin is held, or nothing.
held() {
	grep -m1 "^$1 $2: " <<<"$holds" | cut -d: -f2- | sed 's/^ //' || true
}

# cargo runs the tool's pinned cargo, in its directory, rustup's proxies in mise's CARGO_HOME
# ahead of a host cargo on PATH; mise's RUSTUP_TOOLCHAIN picks the toolchain. Each call is a new
# mise process, so after upgrade has rewritten the mise files it runs the toolchain just pinned.
# The inner script is single-quoted so that $CARGO_HOME expands in mise's environment.
cargo() {
	# shellcheck disable=SC2016
	mise -C "$tool" exec -- bash -c 'export PATH="$CARGO_HOME/bin:$PATH"; exec cargo "$@"' bash "$@"
}

# outdated prints "<file> <name> <pin> <latest>" for each mise tool a mise file in the tool's
# directory pins below its latest. Every environment's file is loaded, so mise.<env>.toml's tools,
# such as docproc-mail's fixture generator's SDK, are read with mise.toml's. The root's are its own.
outdated() {
	local envs file
	envs=$(for file in "$tool"/mise.*.toml; do
		[ -e "$file" ] || continue
		file=${file##*/mise.}
		file=${file%.toml}
		[ "$file" = local ] || echo "$file"
	done | paste -sd, -)
	MISE_ENV=$envs mise -C "$tool" outdated --bump --json |
		jq -r --arg dir "$dir" 'to_entries[] |
			select(.value.bump != null and (.value.source.path | startswith($dir + "/mise."))) |
			"\(.value.source.path | split("/") | last) \(.key) \(.value.requested) \(.value.bump)"'
}

# Crates, direct and indirect, as "<name> <pin> <latest>": updates within each requirement
# ("Updating"), and newer releases beyond it ("Unchanged ... (available: ...)"). --dry-run leaves
# Cargo.lock as it is. Each scan is captured before it is read, so a command that fails fails
# this script instead of reporting nothing.
crates() {
	cargo update --dry-run --verbose 2>&1 |
		sed -nE 's/^ *(Updating|Unchanged) +([^ ]+) +v([^ ]+)( -> v([^ ]+)| \(available: v([^)]+)\))$/\2 \3 \5\6/p'
}

if [ "$action" = report ]; then
	stale=0
	report() {
		echo "$tool: $1"
		stale=1
	}
	trailing() {
		local reason
		reason=$(held "$2" "$3")
		if [ -n "$reason" ]; then
			echo "$tool: $1: $2 $3 -> $4 held: $reason"
		else
			report "$1: $2 $3 -> $4"
		fi
	}

	scanned=$(crates)
	while read -r name pinned latest; do
		[ -n "$name" ] && trailing Cargo.lock "$name" "$pinned" "$latest"
	done <<<"$scanned"

	# Crates in the locked graph with a RustSec advisory: a vulnerability, an unmaintained or
	# unsound crate, or a yanked release. cargo-deny fetches the advisory database first, and reads
	# it here, never in the check, so a newly published advisory fails no build.
	# shellcheck disable=SC2016
	if ! advisories=$(mise -C "$tool" exec -- bash -c \
		'export PATH="$CARGO_HOME/bin:$PATH"; cargo deny --color never check advisories' 2>&1); then
		while read -r line; do
			[ -n "$line" ] && report "Cargo.lock: $line"
		done < <(grep -E '^(error|warning)\[' <<<"$advisories")
	fi

	scanned=$(outdated)
	while read -r file name pinned latest; do
		[ -n "$file" ] && trailing "$file" "$name" "$pinned" "$latest"
	done <<<"$scanned"

	exit "$stale"
fi

# Each mise tool is rewritten in place, its version alone, as `mise use` would write rust's table
# as a bare version. A backend-qualified name, such as "github:EmbarkStudios/cargo-about", is a
# quoted key holding a "/", so the name is matched with its quotes optional and its regex
# characters escaped, under a "|" delimiter.
scanned=$(outdated)
while read -r file name pinned latest; do
	[ -n "$file" ] || continue
	[ -z "$(held "$name" "$pinned")" ] || continue
	# shellcheck disable=SC2001 # a bracket expression, which a ${//} pattern has no escape for
	key=$(sed 's/[][\\.*^$|+?(){}]/\\&/g' <<<"$name")
	sed -i -E "s|^(\"?$key\"? = (\\{ version = )?\")$pinned\"|\\1$latest\"|" "$tool/$file"
done <<<"$scanned"

# A direct dependency with a release beyond its requirement gets that release as its requirement,
# an exact pin ("=") kept exact, in each manifest that requires it: the tool's own and that of
# each crate it requires by path, the shared envelope crate's among them, a crate both require
# (serde) in both, a dependency listed again under [dev-dependencies] in both its lines. An
# indirect one beyond its requirement moves only when the crate that requires it does. Each line
# of direct is a crate's name, then the manifest requiring it; a path crate has no release to
# move to. The
# manifests are read without resolving the graph (--no-deps), so a lock another tool's upgrade
# left trailing the envelope's requirements is read as it stands and re-locked below.
# shellcheck disable=SC2016 # jq's variable
requires='.packages[] | .manifest_path as $manifest | .dependencies[] | select(.path == null) |
	"\(.name) \($manifest)"'
paths=$(cargo metadata --format-version 1 --no-deps |
	jq -r '.packages[].dependencies[] | select(.path != null) | "\(.path)/Cargo.toml"')
direct=$(cargo metadata --format-version 1 --no-deps | jq -r "$requires")
while read -r manifest; do
	[ -n "$manifest" ] || continue
	direct+=$'\n'$(cargo metadata --format-version 1 --no-deps --manifest-path "$manifest" |
		jq -r "$requires")
done <<<"$paths"
beyond=$(cargo update --dry-run --verbose 2>&1 |
	sed -nE 's/^ *Unchanged +([^ ]+) +v([^ ]+) \(available: v([^)]+)\)$/\1 \2 \3/p')
while read -r name pinned latest; do
	[ -n "$name" ] || continue
	[ -z "$(held "$name" "$pinned")" ] || continue
	while read -r manifest; do
		sed -i -E "s/^($name = (\\{ version = )?\"=?)[^\"]+\"/\\1$latest\"/" "$manifest"
	done < <(sed -n "s/^$name //p" <<<"$direct" | sort -u)
done <<<"$beyond"

# Then every crate within its requirement to its latest, held ones left at their pins; a
# requirement raised above, the tool's or the envelope's by another tool's upgrade, is within
# now, so this re-locks the tool's graph to it.
within=$(cargo update --dry-run --verbose 2>&1 |
	sed -nE 's/^ *Updating +([^ ]+) +v([^ ]+) -> v[^ ]+$/\1 \2/p')
while read -r name pinned; do
	[ -n "$name" ] || continue
	[ -z "$(held "$name" "$pinned")" ] || continue
	cargo update --quiet "$name@$pinned"
done <<<"$within"
