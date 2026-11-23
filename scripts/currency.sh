#!/usr/bin/env bash
# currency reports, or upgrades, docproc's root pins: each Go module's direct requirements, tool
# modules, and go directive, the root mise tools, the workflows' action pins, the compose image
# pins, and the image pins in the Go source, tag and digest; then each tool's own, through its
# currency or upgrade task. A pin the root HOLDS list names is reported as held and never moved.
# report exits 1 when it reports any pin that isn't held.
# Run it through `mise run currency` or `mise run upgrade`.
#
# usage: scripts/currency.sh <report|upgrade>
# See docs/development.md
# See architecture/principles/rolling-currency.md
set -euo pipefail
cd "$MISE_PROJECT_ROOT"
export GOWORK=off

if [ $# -ne 1 ] || { [ "$1" != report ] && [ "$1" != upgrade ]; }; then
	echo "usage: scripts/currency.sh <report|upgrade>" >&2
	exit 2
fi
action=$1
holds=${HOLDS:-}

held() {
	grep -m1 "^$1 $2: " <<<"$holds" | cut -d: -f2- | sed 's/^ //' || true
}

go() {
	mise exec -- go "$@"
}

escape_regex() {
	# shellcheck disable=SC2001 # a bracket expression, which a ${//} pattern has no escape for
	sed 's/[][\\.*^$|+?(){}]/\\&/g' <<<"$1"
}

outdated_requirements() {
	go list -m -u -f \
		'{{if and (not .Main) (not .Indirect) .Update}}{{.Path}} {{.Version}} {{.Update.Version}}{{end}}' all
}

outdated_tool_modules() {
	local out err
	err=$(mktemp) || return
	if ! out=$(go list -f '{{.Module.Path}}' tool 2>"$err"); then
		cat "$err" >&2
		rm -f "$err"
		return 1
	fi
	rm -f "$err"
	if [ -z "$out" ]; then
		return 0
	fi
	# shellcheck disable=SC2046 # one argument per tool module
	go list -m -u -f \
		'{{if and (not .Main) .Indirect .Update}}{{.Path}} {{.Version}} {{.Update.Version}}{{end}}' \
		$(sort -u <<<"$out")
}

trailing_go_directive() {
	local minor
	minor=$(go mod edit -json | jq -r .Go | cut -d. -f1,2) || return
	if [ "$minor" != "$go_minor" ]; then
		echo "go $minor $go_minor"
	fi
}

outdated_mise_tools() {
	mise outdated --bump --no-global --json |
		jq -r 'to_entries[] | select(.value.bump != null) | "\(.key) \(.value.requested) \(.value.bump)"'
}

outdated_actions() {
	local file uses pinned repo latest
	local files=()
	for file in .github/workflows/*.yml; do
		if [ -e "$file" ]; then files+=("$file"); fi
	done
	if [ ${#files[@]} -eq 0 ]; then
		return 0
	fi
	uses=$(grep -ho 'uses: *[^ ]*@[^ ]*' "${files[@]}" | sed 's/uses: *//' | sort -u || [ $? -eq 1 ]) || return
	for uses in $uses; do
		pinned=${uses#*@}
		uses=${uses%@*}
		repo=$(echo "$uses" | cut -d/ -f1,2) || return
		latest=$(gh api "repos/$repo/releases/latest" --jq .tag_name) || return
		if [ "$pinned" != "$latest" ]; then
			echo "$uses $pinned $latest"
		fi
	done
}

latest_image_tag() {
	local image=$1 pin=$2 version suffix build='()' pattern tags latest
	version=${pin%%[!0-9.]*}
	suffix=${pin#"$version"}
	if [[ $suffix =~ -ls[0-9]+$ ]]; then
		build='-ls([0-9]+)'
		suffix=${suffix%-ls*}
	fi
	pattern="^([0-9]+(\.[0-9]+)+)${suffix//./\\.}${build}\$"
	tags=$(crane ls "$image") || return
	latest=$(grep -E "$pattern" <<<"$tags" | sed -E "s/$pattern/\\1 \\3 &/" |
		sort -t' ' -k1,1V -k2,2n | tail -n1 | cut -d' ' -f3) || return
	echo "$latest"
}

outdated_images() {
	local dockerfile ref image pin latest
	for dockerfile in compose/*/Dockerfile; do
		if [ ! -e "$dockerfile" ]; then
			continue
		fi
		ref=$(sed -n 's/^FROM  *\([^ ]*\).*/\1/p' "$dockerfile" | head -n1) || return
		image=${ref%:*}
		pin=${ref##*:}
		latest=$(latest_image_tag "$image" "$pin") || return
		if [ "$pin" != "$latest" ]; then
			echo "$image $pin $latest $dockerfile"
		fi
	done
}

go_source_image_pins() {
	local file
	for file in $(git ls-files '*.go'); do
		tr '\n' ' ' <"$file" | sed -E 's/"[[:space:]]*\+[[:space:]]*"//g' |
			grep -oE '"[a-z0-9][^"[:space:]@]*:[A-Za-z0-9_][A-Za-z0-9_.-]*@sha256:[0-9a-f]{64}"' |
			tr -d '"' | grep -vE '^[^/]*\.(example|invalid|test)(:[0-9]+)?/' |
			sed "s#^#$file #" || [ $? -eq 1 ] || return
	done
}

outdated_go_source_images() {
	local pins file ref image pin digest latest
	pins=$(go_source_image_pins) || return
	while read -r file ref; do
		if [ -z "$file" ]; then
			continue
		fi
		digest=${ref##*@}
		ref=${ref%@*}
		image=${ref%:*}
		pin=${ref##*:}
		latest=$(latest_image_tag "$image" "$pin") || return
		if [ "$pin" != "$latest" ]; then
			echo "$image $pin $latest $file $digest"
		fi
	done <<<"$pins"
}

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

report_go_modules() {
	local mod scanned name pinned latest
	for mod in $GO_MODULES; do
		scanned=$(cd "$mod" && outdated_requirements && outdated_tool_modules && trailing_go_directive) || return
		while read -r name pinned latest; do
			if [ -n "$name" ]; then trailing "$mod/go.mod" "$name" "$pinned" "$latest"; fi
		done <<<"$scanned"
	done
}

report_mise_tools() {
	local scanned name pinned latest
	scanned=$(outdated_mise_tools) || return
	while read -r name pinned latest; do
		if [ -n "$name" ]; then trailing mise.toml "$name" "$pinned" "$latest"; fi
	done <<<"$scanned"
}

report_each_tool() {
	local tool
	for tool in $TOOLS; do
		mise -C "tools/$tool" run currency || stale=1
	done
}

report_actions() {
	local scanned name pinned latest
	scanned=$(outdated_actions) || return
	while read -r name pinned latest; do
		if [ -n "$name" ]; then trailing .github/workflows "$name" "$pinned" "$latest"; fi
	done <<<"$scanned"
}

report_images() {
	local scanned name pinned latest dockerfile
	scanned=$(outdated_images) || return
	while read -r name pinned latest dockerfile; do
		if [ -n "$name" ]; then trailing "$dockerfile" "$name" "$pinned" "$latest"; fi
	done <<<"$scanned"
}

report_go_source_images() {
	local scanned name pinned latest file
	scanned=$(outdated_go_source_images) || return
	while read -r name pinned latest file _; do
		if [ -n "$name" ]; then trailing "$file" "$name" "$pinned" "$latest"; fi
	done <<<"$scanned"
}

upgrade_mise_tools() {
	local scanned name pinned latest
	local exclude=()
	scanned=$(outdated_mise_tools) || return
	while read -r name pinned latest; do
		if [ -n "$name" ] && [ -n "$(held "$name" "$pinned")" ]; then exclude+=(--exclude "$name"); fi
	done <<<"$scanned"
	if [ -n "$scanned" ]; then
		echo "upgrade: mise.toml"
		mise upgrade --bump --no-global "${exclude[@]}"
	fi
}

upgrade_go_module() {
	local scanned name pinned
	local latest=()
	scanned=$(trailing_go_directive) || return
	if [ -n "$scanned" ] && [ -z "$(held go "$(cut -d' ' -f2 <<<"$scanned")")" ]; then
		go mod edit -go="$go_minor" -toolchain=none
	fi
	scanned=$(outdated_requirements && outdated_tool_modules) || return
	while read -r name pinned _; do
		if [ -n "$name" ] && [ -z "$(held "$name" "$pinned")" ]; then latest+=("$name@latest"); fi
	done <<<"$scanned"
	if [ ${#latest[@]} -gt 0 ]; then
		go get "${latest[@]}"
	fi
	go mod tidy
}

upgrade_go_modules() {
	local mod
	for mod in $GO_MODULES; do
		echo "upgrade: $mod"
		(
			cd "$mod"
			upgrade_go_module
		)
	done
}

upgrade_actions() {
	local scanned name pinned latest key
	scanned=$(outdated_actions) || return
	while read -r name pinned latest; do
		if [ -z "$name" ] || [ -n "$(held "$name" "$pinned")" ]; then
			continue
		fi
		echo "upgrade: .github/workflows: $name $pinned -> $latest"
		key=$(escape_regex "$name@$pinned") || return
		sed -i -E "s#(uses: *)$key( |\$)#\\1$name@$latest\\2#" .github/workflows/*.yml
	done <<<"$scanned"
}

upgrade_images() {
	local scanned name pinned latest dockerfile key
	scanned=$(outdated_images) || return
	while read -r name pinned latest dockerfile; do
		if [ -z "$name" ] || [ -n "$(held "$name" "$pinned")" ]; then
			continue
		fi
		echo "upgrade: $dockerfile: $name $pinned -> $latest"
		key=$(escape_regex "$name:$pinned") || return
		sed -i -E "s#^(FROM +)$key( |\$)#\\1$name:$latest\\2#" "$dockerfile"
	done <<<"$scanned"
}

upgrade_go_source_images() {
	local scanned name pinned latest file digest index key
	scanned=$(outdated_go_source_images) || return
	while read -r name pinned latest file digest; do
		if [ -z "$name" ] || [ -n "$(held "$name" "$pinned")" ]; then
			continue
		fi
		index=$(crane digest "$name:$latest") || return
		echo "upgrade: $file: $name $pinned -> $latest"
		key=$(escape_regex "$name:$pinned@sha256:") || return
		sed -i -E -e "s#\"$key#\"$name:$latest@sha256:#" -e "s#${digest#sha256:}\"#${index#sha256:}\"#" "$file"
	done <<<"$scanned"
}

upgrade_each_tool() {
	local tool
	for tool in $TOOLS; do
		echo "upgrade: tools/$tool"
		mise -C "tools/$tool" run upgrade
	done
}

go_minor=$(mise latest go | cut -d. -f1,2)
if [ "$action" = report ]; then
	report_go_modules
	report_mise_tools
	report_each_tool
	report_actions
	report_images
	report_go_source_images
	exit "$stale"
fi
upgrade_mise_tools
upgrade_go_modules
upgrade_actions
upgrade_images
upgrade_go_source_images
upgrade_each_tool
