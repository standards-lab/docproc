#!/usr/bin/env bash
# currency reports, or upgrades, docproc's root pins: each Go module's direct requirements, tool
# modules, and go directive, the mise tools the root mise.toml pins, the workflows' action pins,
# and the compose harness's image pins, each compose/<service>/Dockerfile's FROM line; then each
# tool's own, through the tool's currency or upgrade task. A pin the root HOLDS list names (the
# root mise.toml's [env]) is reported as held and never moved. It reads the network and writes
# nothing in a report but the tools' obj/. Run it through `mise run currency` or
# `mise run upgrade`.
#
# report prints one line for each pin behind its latest, a held one with its hold's reason, and
# exits 1 when any it reports is not held.
#
# upgrade moves every pin report shows trailing to its latest, but a held one, so a report after
# it shows none of them but the held. It bumps the root mise pins first, so the Go modules then
# move under the Go just pinned: a go directive raised to a Go minor the old toolchain predates
# would otherwise send the go command to fetch a toolchain. Then the Go modules, then the action
# pins and the image pins, then each tool's upgrade last. It rewrites mise.toml, each module's
# go.mod and go.sum, the workflows, the compose Dockerfiles, and the tools' pins. Run it, review
# the diff, and run the check.
#
# Each scan captures its command's output in a variable before reading it, so under set -e a
# scan command that fails (a go list missing a go.sum entry) fails currency instead of reporting
# nothing.
#
# usage: scripts/currency.sh <report|upgrade>
set -euo pipefail
cd "$MISE_PROJECT_ROOT"
export GOWORK=off

if [ $# -ne 1 ] || { [ "$1" != report ] && [ "$1" != upgrade ]; }; then
	echo "usage: scripts/currency.sh <report|upgrade>" >&2
	exit 2
fi
action=$1

# The hold list: one line each, "<item> <pin>: <reason>", the item named as the report names it:
# a Go module by its path, the go directive as go, a mise tool by its name, an action by its
# owner/repo[/path], a compose image by its image.
holds=${HOLDS:-}
# held prints the reason a pin is held, or nothing.
held() {
	grep -m1 "^$1 $2: " <<<"$holds" | cut -d: -f2- | sed 's/^ //' || true
}

# go runs the Go the root mise.toml pins, in a new mise process in the current directory, a module
# under the root, so after upgrade has bumped the pin it runs the Go just pinned rather than the
# one this task started with.
go() {
	mise exec -- go "$@"
}

go_minor=$(mise latest go | cut -d. -f1,2)

# requirements prints "<path> <pin> <latest>" for each direct requirement of the module in the
# current directory with a newer version within its major.
requirements() {
	go list -m -u -f \
		'{{if and (not .Main) (not .Indirect) .Update}}{{.Path}} {{.Version}} {{.Update.Version}}{{end}}' all
}

# tools prints "<path> <pin> <latest>" for each module a tool directive of the module in the
# current directory declares, with a newer version within its major. A tool's module is an
# indirect requirement, which requirements leaves out; one the module also requires directly is
# reported there, so it is skipped here. Only go list's standard output names modules: its
# standard error carries the warning when no tool directive matches and any download's progress,
# so it is shown only when go list fails.
tools() {
	local out err
	err=$(mktemp)
	if ! out=$(go list -f '{{.Module.Path}}' tool 2>"$err"); then
		cat "$err" >&2
		rm -f "$err"
		return 1
	fi
	rm -f "$err"
	[ -n "$out" ] || return 0
	# shellcheck disable=SC2046 # one argument per tool module
	go list -m -u -f \
		'{{if and (not .Main) .Indirect .Update}}{{.Path}} {{.Version}} {{.Update.Version}}{{end}}' \
		$(sort -u <<<"$out")
}

# directive prints "go <pin> <latest>" when the go directive's minor of the module in the current
# directory trails the current Go minor.
directive() {
	local minor
	minor=$(go mod edit -json | jq -r .Go | cut -d. -f1,2)
	[ "$minor" = "$go_minor" ] || echo "go $minor $go_minor"
}

# outdated prints "<name> <pin> <latest>" for each mise tool the root mise.toml pins below its
# latest. The tools' mise files are their own.
outdated() {
	mise outdated --bump --no-global --json |
		jq -r 'to_entries[] | select(.value.bump != null) | "\(.key) \(.value.requested) \(.value.bump)"'
}

# actions prints "<action> <pin> <latest>" for each action a workflow pins below its repository's
# latest release tag. With no workflows, the glob stays unexpanded and is skipped.
actions() {
	local file uses pinned repo latest
	local files=()
	for file in .github/workflows/*.yml; do
		if [ -e "$file" ]; then files+=("$file"); fi
	done
	[ ${#files[@]} -gt 0 ] || return 0
	uses=$(grep -ho 'uses: *[^ ]*@[^ ]*' "${files[@]}" | sed 's/uses: *//' | sort -u || [ $? -eq 1 ])
	for uses in $uses; do
		pinned=${uses#*@}
		uses=${uses%@*}
		repo=$(echo "$uses" | cut -d/ -f1,2)
		latest=$(gh api "repos/$repo/releases/latest" --jq .tag_name)
		[ "$pinned" = "$latest" ] || echo "$uses $pinned $latest"
	done
}

# latest_tag prints the highest semver tag of an image that carries the given pin's variant suffix
# (18.7-alpine -> -alpine). A tag needs at least one dot to count, so a bare build number never
# sorts above every release.
latest_tag() {
	local image=$1 pin=$2 suffix pattern tags latest
	suffix=${pin#"${pin%%[!0-9.]*}"}
	pattern="^[0-9]+(\.[0-9]+)+${suffix//./\\.}\$"
	tags=$(crane ls "$image")
	latest=$(grep -E "$pattern" <<<"$tags" | sed "s/${suffix}\$//" | sort -V | tail -n1)
	echo "$latest$suffix"
}

# images prints "<image> <pin> <latest> <dockerfile>" for each compose service whose Dockerfile's
# FROM line pins its image below the latest tag. A service that builds from
# compose/<service>/Dockerfile has its one pin there. With no Dockerfiles, the glob stays
# unexpanded and is skipped.
images() {
	local dockerfile ref image pin latest
	for dockerfile in compose/*/Dockerfile; do
		[ -e "$dockerfile" ] || continue
		ref=$(sed -n 's/^FROM  *\([^ ]*\).*/\1/p' "$dockerfile" | head -n1)
		image=${ref%:*}
		pin=${ref##*:}
		latest=$(latest_tag "$image" "$pin")
		[ "$pin" = "$latest" ] || echo "$image $pin $latest $dockerfile"
	done
}

if [ "$action" = report ]; then
	stale=0
	trailing() {
		local reason
		reason=$(held "$2" "$3")
		if [ -n "$reason" ]; then
			echo "$1: $2 $3 -> $4 held: $reason"
		else
			echo "$1: $2 $3 -> $4"
			stale=1
		fi
	}

	for mod in $GO_MODULES; do
		scanned=$(cd "$mod" && requirements && tools && directive)
		while read -r name pinned latest; do
			[ -n "$name" ] && trailing "$mod/go.mod" "$name" "$pinned" "$latest"
		done <<<"$scanned"
	done

	scanned=$(outdated)
	while read -r name pinned latest; do
		[ -n "$name" ] && trailing mise.toml "$name" "$pinned" "$latest"
	done <<<"$scanned"

	# Each tool's own currency: its packages, advisories, and toolchain, each line prefixed with
	# the tool's directory. A tool reporting anything exits non-zero, which marks the whole report
	# stale without stopping the other scans.
	for tool in $TOOLS; do
		mise -C "tools/$tool" run currency || stale=1
	done

	scanned=$(actions)
	while read -r name pinned latest; do
		[ -n "$name" ] && trailing .github/workflows "$name" "$pinned" "$latest"
	done <<<"$scanned"

	scanned=$(images)
	while read -r name pinned latest dockerfile; do
		[ -n "$name" ] && trailing "$dockerfile" "$name" "$pinned" "$latest"
	done <<<"$scanned"

	exit "$stale"
fi

# The root mise pins, every one trailing but a held one, which is excluded by name. --no-global
# writes the root mise.toml alone, never the global config.
scanned=$(outdated)
exclude=()
while read -r name pinned latest; do
	[ -n "$name" ] || continue
	if [ -n "$(held "$name" "$pinned")" ]; then exclude+=(--exclude "$name"); fi
done <<<"$scanned"
if [ -n "$scanned" ]; then
	echo "upgrade: mise.toml"
	mise upgrade --bump --no-global "${exclude[@]}"
fi

# Each Go module's go directive, when its minor trails, to the current Go minor with no toolchain
# line; then each direct requirement and tool module with a newer version, but a held one, to its
# latest; then tidied. A directive whose minor is current is left as written, patch and all.
for mod in $GO_MODULES; do
	echo "upgrade: $mod"
	(
		cd "$mod"
		scanned=$(directive)
		if [ -n "$scanned" ] && [ -z "$(held go "$(cut -d' ' -f2 <<<"$scanned")")" ]; then
			go mod edit -go="$go_minor" -toolchain=none
		fi
		scanned=$(requirements && tools)
		latest=()
		while read -r name pinned _; do
			[ -n "$name" ] || continue
			if [ -z "$(held "$name" "$pinned")" ]; then latest+=("$name@latest"); fi
		done <<<"$scanned"
		[ ${#latest[@]} -eq 0 ] || go get "${latest[@]}"
		go mod tidy
	)
done

# Each workflow's action pins, every use of a trailing action rewritten to its latest release
# tag, but a held one. The action and its pin are matched with their regex characters escaped,
# under a "#" delimiter, and only up to the line's end or a space before a comment, so v1 never
# matches inside v1.2.
scanned=$(actions)
while read -r name pinned latest; do
	[ -n "$name" ] || continue
	[ -z "$(held "$name" "$pinned")" ] || continue
	echo "upgrade: .github/workflows: $name $pinned -> $latest"
	# shellcheck disable=SC2001 # a bracket expression, which a ${//} pattern has no escape for
	key=$(sed 's/[][\\.*^$|+?(){}]/\\&/g' <<<"$name@$pinned")
	sed -i -E "s#(uses: *)$key( |\$)#\\1$name@$latest\\2#" .github/workflows/*.yml
done <<<"$scanned"

# Each compose image pin, the FROM line of its service's Dockerfile, to the latest tag, but a held
# one. The image and its pin are matched with their regex characters escaped, under a "#"
# delimiter, and only up to a space or the line's end, so 18.7 never matches inside 18.70.
scanned=$(images)
while read -r name pinned latest dockerfile; do
	[ -n "$name" ] || continue
	[ -z "$(held "$name" "$pinned")" ] || continue
	echo "upgrade: $dockerfile: $name $pinned -> $latest"
	# shellcheck disable=SC2001 # a bracket expression, which a ${//} pattern has no escape for
	key=$(sed 's/[][\\.*^$|+?(){}]/\\&/g' <<<"$name:$pinned")
	sed -i -E "s#^(FROM +)$key( |\$)#\\1$name:$latest\\2#" "$dockerfile"
done <<<"$scanned"

# Each tool's upgrade last, so a tool's pins move after the root's.
for tool in $TOOLS; do
	echo "upgrade: tools/$tool"
	mise -C "tools/$tool" run upgrade
done
