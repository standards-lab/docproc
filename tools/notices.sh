#!/usr/bin/env bash
# notices writes the third-party notices for one system tool's release binary, for one runtime
# identifier, to stdout: every package the binary links, at the version the tool's lock file pins,
# with its license, and for a .NET tool the .NET runtime NativeAOT compiles into the binary, with
# the runtime's license, followed once each by the third-party notices those packages bundle.
# pack.sh writes it into each release archive, and docproc's root check runs it for every tool so
# a package it can't give a notice for fails the check, not the release.
#
# It carries one license text, MIT's, and refuses a package under any other license: a new license
# is a reviewed change to this script. It reads the tool's restored package graph and the NuGet
# packages folder, so it runs after the tool's check or publish has restored; it reads no network
# and writes nothing.
#
# usage: tools/notices.sh <format> <rid>
#   format  the tool's directory under tools/, such as ooxml
#   rid     linux-x64 or linux-arm64
set -euo pipefail
cd "$(dirname "$0")/.."

if [ $# -ne 2 ]; then
	echo "usage: tools/notices.sh <format> <rid>" >&2
	exit 2
fi
format=$1
rid=$2
tool=tools/$format
case "$rid" in
	linux-x64 | linux-arm64) ;;
	*)
		echo "notices: no release for runtime identifier $rid" >&2
		exit 2
		;;
esac
if ! compgen -G "$tool/*.slnx" >/dev/null; then
	echo "notices: $tool is not a .NET tool, the only kind this script reads" >&2
	exit 2
fi

fail() {
	echo "notices: $1" >&2
	exit 1
}

# The release binary is built from the projects under src/; the tests' packages are never linked.
locks=("$tool"/src/*/packages.lock.json)
assets=("$tool"/src/*/obj/project.assets.json)
[ -e "${locks[0]}" ] || fail "$tool/src has no packages.lock.json"
[ -e "${assets[0]}" ] || fail "$tool is not restored: run its check or publish first"

# Each linked package as "<id> <version>", from the lock file's framework-wide section and its
# section for this runtime identifier. The NativeAOT toolchain's packages are left out: the
# compiler and the trimmer run at build time and link nothing of their own, and what the binary
# links of .NET is the NativeAOT runtime pack's, given below as the runtime's entry.
packages=$(jq -r --arg rid "$rid" --arg ilc "runtime.$rid.Microsoft.DotNet.ILCompiler" '
	["Microsoft.DotNet.ILCompiler", "Microsoft.NET.ILLink.Tasks", $ilc] as $toolchain |
	.dependencies | to_entries[] |
	select((.key | test("^net[0-9.]+$")) or (.key | endswith("/" + $rid))) | .value |
	to_entries[] | select(.key as $id | $toolchain | index($id) | not) |
	"\(.key) \(.value.resolved)"' "${locks[@]}" | sort -u)

# The NativeAOT runtime pack the restore downloaded for this runtime identifier, one version.
runtime_id=Microsoft.NETCore.App.Runtime.NativeAOT.$rid
runtime_version=$(jq -r --arg id "$runtime_id" '.project.frameworks[].downloadDependencies[] |
	select(.name == $id) | .version | ltrimstr("[") | split(",")[0]' "${assets[@]}" | sort -u)
[ -n "$runtime_version" ] || fail "$tool's restore downloaded no $runtime_id"
[ "$(wc -l <<<"$runtime_version")" -eq 1 ] || fail "$tool's projects restore more than one $runtime_id"

folder=$(jq -r '.packageFolders | keys[0]' "${assets[0]}")

# package_dir prints a restored package's directory in the packages folder.
package_dir() {
	local dir
	dir="$folder/${1,,}/${2,,}"
	[ -d "$dir" ] || fail "$1 $2 is not in $folder: run $tool's check or publish first"
	echo "$dir"
}

# nuspec prints one element's text from a package's nuspec, its XML entities decoded.
nuspec() {
	local dir=$1 id=$2 element=$3
	sed -n "s|.*<${element}[^>]*>\\([^<]*\\)</${element}>.*|\\1|p" "$dir/${id,,}.nuspec" | head -n 1 |
		sed -e 's/&lt;/</g' -e 's/&gt;/>/g' -e 's/&quot;/"/g' -e "s/&apos;/'/g" -e 's/&amp;/\&/g'
}

rule() {
	printf '%80s\n' '' | tr ' ' '='
}

mit() {
	cat <<'EOF'
Permission is hereby granted, free of charge, to any person obtaining a copy
of this software and associated documentation files (the "Software"), to deal
in the Software without restriction, including without limitation the rights
to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
copies of the Software, and to permit persons to whom the Software is
furnished to do so, subject to the following conditions:

The above copyright notice and this permission notice shall be included in all
copies or substantial portions of the Software.

THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE
SOFTWARE.
EOF
}

# The bundled third-party notices, each distinct text once: its file, by SHA-256, and the packages
# that bundle it, in first-seen order.
declare -A bundled_file bundled_by
bundled_order=()

# entry prints one package's notice: its name, version, license, project, and copyright, then its
# own license file, or MIT's text under its copyright when the package ships none. It records the
# third-party notices the package bundles for the end.
entry() {
	local title=$1 id=$2 version=$3 dir license project copyright file sum
	dir=$(package_dir "$id" "$version")
	license=$(nuspec "$dir" "$id" license)
	[ "$license" = MIT ] || fail "$id $version is licensed '${license:-by a file}', not MIT"
	project=$(nuspec "$dir" "$id" projectUrl)
	copyright=$(nuspec "$dir" "$id" copyright)

	rule
	echo "$title"
	echo "License: MIT"
	[ -z "$project" ] || echo "Project: $project"
	[ -z "$copyright" ] || echo "Copyright: $copyright"
	echo
	file=$(find "$dir" -maxdepth 1 -type f -iname 'license*' | sort | head -n 1)
	if [ -n "$file" ]; then
		cat "$file"
	else
		echo "MIT License"
		echo
		echo "${copyright:-Copyright (c) the authors of $id}"
		echo
		mit
	fi
	echo

	while read -r file; do
		[ -n "$file" ] || continue
		sum=$(sha256sum "$file" | cut -d' ' -f1)
		if [ -z "${bundled_file[$sum]:-}" ]; then
			bundled_file[$sum]=$file
			bundled_by[$sum]="$id $version"
			bundled_order+=("$sum")
		else
			bundled_by[$sum]+=", $id $version"
		fi
	done < <(find "$dir" -maxdepth 1 -type f -iname 'third-party-notices*' | sort)
}

binary=docproc-$format
echo "Third-party notices for $binary, $rid"
echo
echo "$binary links the NuGet packages below, at the versions its lock file pins, and the .NET"
echo "runtime NativeAOT compiles into it. Each entry gives its license; the third-party notices"
echo "those packages bundle follow the entries."
echo
while read -r id version; do
	[ -n "$id" ] || continue
	entry "$id $version" "$id" "$version"
done <<<"$packages"
entry ".NET runtime $runtime_version (the NativeAOT runtime pack, $runtime_id)" \
	"$runtime_id" "$runtime_version"

for sum in "${bundled_order[@]}"; do
	rule
	echo "Third-party notices bundled with ${bundled_by[$sum]}"
	echo
	cat "${bundled_file[$sum]}"
	echo
done
