#!/usr/bin/env bash
# release-tag routes a pushed release tag to what it releases, printing key=value lines, and
# refuses any tag that names neither a system tool nor a Go module. A tool's tag,
# tools/<format>/vX.Y.Z, prints kind=tool, format, and version; a Go module's, vX.Y.Z or
# <module>/vX.Y.Z, prints kind=module, module, changelog, prefix, and version, and is refused when
# the module's directory holds no go.mod or no CHANGELOG.md. The release workflow's first job runs
# it. It reads the tree and writes nothing.
#
# usage: scripts/release-tag.sh <tag> [root]
#   tag   the pushed tag's name, such as tools/mail/v0.1.1, v0.1.0, or storage/v0.1.0
#   root  the repository's tree, the script's own by default; the test passes its own
# See docs/ci.md
set -euo pipefail

if [ $# -lt 1 ] || [ $# -gt 2 ]; then
	echo "usage: scripts/release-tag.sh <tag> [root]" >&2
	exit 2
fi
tag=$1
root=${2:-$(dirname "$0")/..}

version_re='[0-9]+\.[0-9]+\.[0-9]+'
module_segment_re='[a-z0-9][a-z0-9._-]*'

refuse() {
	echo "release: $tag is $1" >&2
	exit 1
}

route_tool_tag() {
	if [[ ! "$tag" =~ ^tools/([a-z0-9]+)/v($version_re)$ ]]; then
		refuse "not a tools/<format>/vX.Y.Z tag"
	fi
	echo "kind=tool"
	echo "format=${BASH_REMATCH[1]}"
	echo "version=${BASH_REMATCH[2]}"
}

route_module_tag() {
	local module prefix version
	if [[ "$tag" =~ ^v($version_re)$ ]]; then
		module=.
		prefix=
		version=${BASH_REMATCH[1]}
	elif [[ "$tag" =~ ^($module_segment_re(/$module_segment_re)*)/v($version_re)$ ]]; then
		module=${BASH_REMATCH[1]}
		prefix=$module/
		version=${BASH_REMATCH[3]}
	else
		refuse "neither a tools/<format>/vX.Y.Z tag nor a Go module's vX.Y.Z or <module>/vX.Y.Z"
	fi
	if [ ! -f "$root/$module/go.mod" ]; then
		refuse "not a Go module's tag: $module has no go.mod"
	fi
	if [ ! -f "$root/$module/CHANGELOG.md" ]; then
		refuse "a Go module's tag, but $module has no CHANGELOG.md"
	fi
	echo "kind=module"
	echo "module=$module"
	if [ "$module" = . ]; then
		echo "changelog=CHANGELOG.md"
	else
		echo "changelog=$module/CHANGELOG.md"
	fi
	echo "prefix=$prefix"
	echo "version=$version"
}

if [[ "$tag" == tools/* ]]; then
	route_tool_tag
else
	route_module_tag
fi
