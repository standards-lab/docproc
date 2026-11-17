#!/usr/bin/env bash
# release-tag routes a pushed release tag to what it releases, and refuses any tag that names
# neither a system tool nor a Go module. The release workflow's first job runs it on the tag and
# appends its output, key=value lines, to $GITHUB_OUTPUT, so each later job runs only for its
# kind. It reads the tree and writes nothing.
#
# A tag takes one of three forms, each version X.Y.Z, digits alone:
#
#   tools/<format>/vX.Y.Z  a system tool under tools/, <format> lowercase letters and digits:
#                          kind=tool, format, and version. pack.sh refuses a format that isn't
#                          a tool.
#   vX.Y.Z                 the base module, whose go.mod is the root's: kind=module.
#   <module>/vX.Y.Z        a nested Go module whose go.mod is <module>/go.mod, outside tools/
#                          (tools/go.mod only keeps the tools' trees out of the base module):
#                          kind=module.
#
# A module's output also names its directory (module, . for the base), its changelog
# (<module>/CHANGELOG.md), the tag's prefix before its vX.Y.Z (prefix, empty for the base), and
# version. A module tag is refused when its directory holds no go.mod or no CHANGELOG.md, so a
# release is never made from another module's changelog.
#
# usage: scripts/release-tag.sh <tag> [root]
#   tag   the pushed tag's name, such as tools/mail/v0.1.1, v0.1.0, or storage/v0.1.0
#   root  the repository's tree, the script's own by default; the test passes its own
set -euo pipefail

if [ $# -lt 1 ] || [ $# -gt 2 ]; then
	echo "usage: scripts/release-tag.sh <tag> [root]" >&2
	exit 2
fi
tag=$1
root=${2:-$(dirname "$0")/..}

version_re='[0-9]+\.[0-9]+\.[0-9]+'
# A module directory is one or more path segments, each beginning with a letter or digit, so
# none is empty, . or .., and the directory is never absolute.
segment_re='[a-z0-9][a-z0-9._-]*'

refuse() {
	echo "release: $tag is $1" >&2
	exit 1
}

if [[ "$tag" == tools/* ]]; then
	if [[ ! "$tag" =~ ^tools/([a-z0-9]+)/v($version_re)$ ]]; then
		refuse "not a tools/<format>/vX.Y.Z tag"
	fi
	echo "kind=tool"
	echo "format=${BASH_REMATCH[1]}"
	echo "version=${BASH_REMATCH[2]}"
	exit 0
fi

if [[ "$tag" =~ ^v($version_re)$ ]]; then
	module=.
	prefix=
	version=${BASH_REMATCH[1]}
elif [[ "$tag" =~ ^($segment_re(/$segment_re)*)/v($version_re)$ ]]; then
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
