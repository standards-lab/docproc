#!/usr/bin/env bash
# notices writes the third-party notices for one system tool's release binary, for one runtime
# identifier, to stdout: every package the binary is built from, at the version the tool's lock
# file pins, with its license. pack.sh writes it into each release archive, and docproc's root
# check runs it for every tool so a package it can't give a notice for fails the check, not the
# release. It reads no network and writes nothing.
#
# A .NET tool (a .slnx) gets every NuGet package the binary links and the .NET runtime NativeAOT
# compiles into it, with the runtime's license, followed once each by the third-party notices
# those packages bundle. This script carries one license text for it, MIT's, and refuses a package
# under any other license: a new license is a reviewed change to this script. It reads the tool's
# restored package graph and the NuGet packages folder, so it runs after the tool's check or
# publish has restored.
#
# A Rust tool (a Cargo.toml) gets every crate cargo-about, pinned in the tool's mise.toml, walks
# in the tool's locked graph for the runtime identifier's musl target, followed by each license
# text once with the crates it covers. cargo-about checks each crate's license against the
# accepted list in the tool's about.toml, and a license outside it fails the script: a new license
# is a reviewed change to that file. It reads the crates from Cargo's registry cache, offline, so
# it runs after the tool's check or publish has fetched them.
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
if [ -f "$tool/Cargo.toml" ]; then
	kind=cargo
elif compgen -G "$tool/*.slnx" >/dev/null; then
	kind=dotnet
else
	echo "notices: $tool is neither a Rust tool (Cargo.toml) nor a .NET tool (.slnx)" >&2
	exit 2
fi
binary=docproc-$format

fail() {
	echo "notices: $1" >&2
	exit 1
}

rule() {
	printf '%80s\n' '' | tr ' ' '='
}

if [ "$kind" = cargo ]; then
	case "$rid" in
		linux-x64) target=x86_64-unknown-linux-musl ;;
		linux-arm64) target=aarch64-unknown-linux-musl ;;
	esac
	# cargo-about runs in the tool's mise environment, its pinned Rust's cargo ahead of a host
	# cargo, as the tool's own tasks run it. Its `cargo metadata` reads every crate in Cargo.lock,
	# on every platform, so `cargo fetch --locked` first downloads any the tool's build didn't (a
	# no-op once they're in the cache, as cargo test is). --frozen then keeps Cargo.lock as it is
	# and reads no network, and --fail fails on a license it can't read or the accepted list can't
	# satisfy. The inner script is single-quoted so that $CARGO_HOME expands in mise's environment,
	# not this one.
	# shellcheck disable=SC2016
	about=$(mise -C "$tool" exec -- bash -c '
		export PATH="$CARGO_HOME/bin:$PATH"
		cargo fetch --locked --quiet >&2 &&
			cargo-about --color never generate --format json --frozen --fail --target "$1"
	' bash "$target") || fail "cargo-about refused $tool's crates for $target"

	echo "Third-party notices for $binary, $rid"
	echo
	echo "$binary is built from the crates below, at the versions its Cargo.lock pins: the normal"
	echo "dependency graph for $target, proc macros and their dependencies included,"
	echo "though they run only at build time. Each entry gives the licenses its notice is given"
	echo "under, chosen from the crate's license expression; the license texts follow, each with"
	echo "the crates it covers."
	echo
	# Each crate's chosen licenses are the texts that name it. A license text cargo-about made up
	# from the license's template, for a crate that ships none, is given once per crate, under
	# its authors' copyright in place of the template's placeholder.
	jq -r '
		def crate: "\(.name) \(.version)";
		def rule: "=" * 80;
		[.licenses[] | .id as $id | .used_by[] | {crate: (.crate | crate), id: $id}] as $chosen |
		(.crates | map(.package) | sort_by(.name, .version)[] | crate as $c |
			($chosen | map(select(.crate == $c) | .id) | unique | join(" AND ")) as $ids |
			if $ids == "" then error("\($c) has no license text") else "\($c): \($ids)" end),
		"",
		(.licenses | sort_by(.id, .used_by[0].crate.name)[] |
			if .source_path then
				rule, "\(.id), for \(.used_by | map(.crate | crate) | join(", "))", "", .text
			else
				.id as $id | .text as $text | .used_by[].crate |
				(if (.authors | length) > 0 then .authors | join(", ")
					else "the authors of \(.name)" end) as $owner |
				rule, "\($id), for \(crate)", "",
				($text | sub("Copyright \\(c\\) <year> <copyright holders>";
					"Copyright (c) \($owner)"))
			end)
	' <<<"$about" || fail "cargo-about's report for $tool could not be read"
	exit 0
fi

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
