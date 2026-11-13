#!/usr/bin/env bash
# pack builds one system tool's release binary for this machine's runtime identifier and packs it
# as tools/<format>/.state/pack/docproc-<format>-v<version>-<rid>.tar.gz, holding the binary, the
# repository's LICENSE, and THIRD-PARTY-NOTICES.txt from notices.sh. It refuses to pack unless the
# first line of the binary's --version is the given version, and, when the binary links glibc,
# unless the newest glibc symbol version it requires is at most the GLIBC_FLOOR the tool's
# mise.toml declares; a binary that links glibc with no floor declared is refused too. The release
# workflow runs it on each runner, natively, with the version its tag carries; SHA256SUMS over
# both runners' archives is the workflow's. It writes only the tool's build output.
#
# usage: tools/pack.sh <format> <version>
#   format   the tool's directory under tools/, such as ooxml
#   version  X.Y.Z, the version a tools/<format>/vX.Y.Z tag carries
set -euo pipefail
cd "$(dirname "$0")/.."

if [ $# -ne 2 ]; then
	echo "usage: tools/pack.sh <format> <version>" >&2
	exit 2
fi
format=$1
version=$2
tool=tools/$format
binary=docproc-$format
if [ ! -f "$tool/mise.toml" ]; then
	echo "pack: $tool is not a tool: it has no mise.toml" >&2
	exit 2
fi
case "$(uname -m)" in
	x86_64) rid=linux-x64 ;;
	aarch64) rid=linux-arm64 ;;
	*)
		echo "pack: no runtime identifier for $(uname -m)" >&2
		exit 1
		;;
esac

# Each tool's publish task builds its release binary into its .state/publish/.
mise -C "$tool" run publish
built=$tool/.state/publish/$binary

# The binary's version is the one its project records, so the tag must match it.
actual=$("$built" --version | head -n 1)
if [ "$actual" != "$version" ]; then
	echo "pack: the binary's version is $actual, the tag's is $version" >&2
	exit 1
fi

# A dynamically linked binary runs only on a glibc at least as new as every symbol version it
# requires; the newest is checked against the floor the tool states.
required=$(objdump -T "$built" | grep -o 'GLIBC_[0-9][0-9.]*' | sed 's/^GLIBC_//' | sort -uV |
	tail -n 1 || true)
if [ -n "$required" ]; then
	floor=$(mise -C "$tool" env --json | jq -r '.GLIBC_FLOOR // empty')
	if [ -z "$floor" ]; then
		echo "pack: $binary requires glibc $required, and $tool/mise.toml declares no GLIBC_FLOOR" >&2
		exit 1
	fi
	if [ "$(printf '%s\n%s\n' "$required" "$floor" | sort -V | tail -n 1)" != "$floor" ]; then
		echo "pack: $binary requires glibc $required, above its floor of $floor" >&2
		exit 1
	fi
	echo "pack: $binary requires glibc $required, within its floor of $floor"
fi

pack=$tool/.state/pack
stage=$pack/$rid
archive=$pack/$binary-v$version-$rid.tar.gz
rm -rf "$stage" "$archive"
mkdir -p "$stage"
cp "$built" LICENSE "$stage/"
tools/notices.sh "$format" "$rid" >"$stage/THIRD-PARTY-NOTICES.txt"

# The archive's entries are sorted, owned by root, and dated by the commit, so the same binary
# packs to the same bytes.
epoch=$(git log -1 --format=%ct)
tar --sort=name --owner=0 --group=0 --numeric-owner --mtime="@$epoch" -C "$stage" -cf - \
	"$binary" LICENSE THIRD-PARTY-NOTICES.txt | gzip -n >"$archive"
echo "$archive"
