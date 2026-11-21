#!/usr/bin/env bash
# release-tag-test holds scripts/release-tag.sh to its routing: each tag is routed over a tree made
# for the test, its output compared whole, or refused. The tree has the base module, a nested
# module with its changelog, one without, a directory with no go.mod, and tools/'s own go.mod, so
# the routing is shown against each case, never against the repository's tree as it happens to be.
# The lint runs it. It writes only its temporary tree.
#
# usage: scripts/release-tag-test.sh
set -euo pipefail
script=$(dirname "$0")/release-tag.sh

tree=$(mktemp -d)
trap 'rm -rf "$tree"' EXIT
failed=0

make_tree() {
	local module
	mkdir -p "$tree/storage" "$tree/nested/deep" "$tree/bare" "$tree/route" "$tree/tools/mail"
	for module in . storage nested/deep bare tools; do
		touch "$tree/$module/go.mod"
	done
	for module in . storage nested/deep; do
		touch "$tree/$module/CHANGELOG.md"
	done
}

routes() {
	local tag=$1 want got
	want=$(printf '%s\n' "${@:2}")
	if ! got=$("$script" "$tag" "$tree" 2>&1); then
		echo "FAIL $tag: refused: $got"
		failed=1
		return
	fi
	if [ "$got" != "$want" ]; then
		printf 'FAIL %s:\n got:\n%s\n want:\n%s\n' "$tag" "$got" "$want"
		failed=1
		return
	fi
	echo "ok   $tag"
}

refuses() {
	local tag=$1 got status=0
	got=$("$script" "$tag" "$tree" 2>&1) || status=$?
	if [ "$status" -ne 1 ] || [[ "$got" != "release: $tag is "* ]]; then
		echo "FAIL $tag: want a refusal, got exit $status: $got"
		failed=1
		return
	fi
	echo "ok   $tag refused"
}

test_tool_tags() {
	routes tools/mail/v0.1.1 kind=tool format=mail version=0.1.1
	routes tools/ooxml/v10.20.30 kind=tool format=ooxml version=10.20.30
}

test_module_tags() {
	routes v0.1.0 kind=module module=. changelog=CHANGELOG.md prefix= version=0.1.0
	routes storage/v0.1.0 kind=module module=storage changelog=storage/CHANGELOG.md \
		prefix=storage/ version=0.1.0
	routes nested/deep/v1.2.3 kind=module module=nested/deep changelog=nested/deep/CHANGELOG.md \
		prefix=nested/deep/ version=1.2.3
}

test_neither_tool_nor_module_tags() {
	refuses tools/v0.1.0
	refuses tools/mail/deep/v0.1.0
	refuses tools/Mail/v0.1.0
	refuses tools/mail/v0.1
	refuses tools/mail/0.1.0
	refuses v0.1
	refuses v0.1.0-rc.1
	refuses v0.1.0.1
	refuses V0.1.0
	refuses 0.1.0
	refuses latest
	refuses route/v0.1.0
	refuses missing/v0.1.0
	refuses bare/v0.1.0
	refuses ../storage/v0.1.0
	refuses ./storage/v0.1.0
	refuses storage//v0.1.0
	refuses /storage/v0.1.0
	refuses storage/v0.1.0/
}

test_usage_exits_2() {
	local status=0
	"$script" >/dev/null 2>&1 || status=$?
	if [ "$status" -ne 2 ]; then
		echo "FAIL no arguments: want exit 2, got $status"
		failed=1
	else
		echo "ok   no arguments refused"
	fi
}

make_tree
test_tool_tags
test_module_tags
test_neither_tool_nor_module_tags
test_usage_exits_2
if [ "$failed" -ne 0 ]; then
	echo "release-tag-test: failed"
	exit 1
fi
echo "release-tag-test: ok"
