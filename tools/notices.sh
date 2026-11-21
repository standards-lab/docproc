#!/usr/bin/env bash
# notices writes the third-party notices for one system tool's release binary, for one runtime
# identifier, to stdout: every package the binary is built from, at the version the tool's lock
# file pins, with its license, then the tool's committed supplement, tools/<format>/notices.txt,
# when it has one. It fails on a package it can't give a notice for. pack.sh writes it into each
# release archive, and docproc's root check runs it for every tool. It reads no network and writes
# nothing; it runs after the tool's check or publish has restored its packages.
#
# usage: tools/notices.sh <format> <rid>
#   format  the tool's directory under tools/, such as ooxml
#   rid     linux-x64 or linux-arm64
# See docs/tool-builds.md
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

supplement() {
	if [ -f "$tool/notices.txt" ]; then
		echo
		cat "$tool/notices.txt"
	fi
}

musl_target() {
	case "$rid" in
		linux-x64) echo x86_64-unknown-linux-musl ;;
		linux-arm64) echo aarch64-unknown-linux-musl ;;
	esac
}

cargo_about_report() {
	# shellcheck disable=SC2016 # $CARGO_HOME expands in mise's environment, not this one
	mise -C "$tool" exec -- bash -c '
		export PATH="$CARGO_HOME/bin:$PATH"
		cargo fetch --locked --quiet >&2 &&
			cargo-about --color never generate --format json --frozen --fail --target "$1"
	' bash "$1"
}

pinned_rust_version() {
	local version
	# shellcheck disable=SC2016 # $CARGO_HOME expands in mise's environment, not this one
	version=$(mise -C "$tool" exec -- bash -c '
		export PATH="$CARGO_HOME/bin:$PATH"
		rustc --version
	') || return
	cut -d' ' -f2 <<<"$version"
}

crates_and_licenses() {
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
	'
}

rust_notices() {
	local target about rust
	target=$(musl_target)
	about=$(cargo_about_report "$target") || fail "cargo-about refused $tool's crates for $target"
	rust=$(pinned_rust_version) || fail "$tool's pinned rustc did not run"

	echo "Third-party notices for $binary, $rid"
	echo
	echo "$binary is built from the crates below, at the versions its Cargo.lock pins: the normal"
	echo "dependency graph for $target, proc macros and their dependencies included,"
	echo "though they run only at build time. Each entry gives the licenses its notice is given"
	echo "under, chosen from the crate's license expression; the license texts follow, each with"
	echo "the crates it covers. The notices of the Rust standard library and musl libc, which the"
	echo "binary also links, follow them."
	echo
	crates_and_licenses <<<"$about" || fail "cargo-about's report for $tool could not be read"
	echo
	sed -e "s/@RUST_VERSION@/$rust/g" -e "s/@TARGET@/$target/g" tools/notices-rust.txt
	supplement
}

linked_packages() {
	jq -r --arg rid "$rid" --arg ilc "runtime.$rid.Microsoft.DotNet.ILCompiler" '
		["Microsoft.DotNet.ILCompiler", "Microsoft.NET.ILLink.Tasks", $ilc] as $toolchain |
		.dependencies | to_entries[] |
		select((.key | test("^net[0-9.]+$")) or (.key | endswith("/" + $rid))) | .value |
		to_entries[] | select(.key as $id | $toolchain | index($id) | not) |
		"\(.key) \(.value.resolved)"' "${locks[@]}" | sort -u
}

runtime_pack_version() {
	jq -r --arg id "$runtime_id" '.project.frameworks[].downloadDependencies[] |
		select(.name == $id) | .version | ltrimstr("[") | split(",")[0]' "${assets[@]}" | sort -u
}

package_dir() {
	local dir
	dir="$folder/${1,,}/${2,,}"
	[ -d "$dir" ] || fail "$1 $2 is not in $folder: run $tool's check or publish first"
	echo "$dir"
}

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

declare -A bundled_file bundled_by
bundled_order=()

record_bundled_notices() {
	local dir=$1 id=$2 version=$3 file sum
	while read -r file; do
		if [ -z "$file" ]; then
			continue
		fi
		sum=$(sha256sum "$file" | cut -d' ' -f1) || return
		if [ -z "${bundled_file[$sum]:-}" ]; then
			bundled_file[$sum]=$file
			bundled_by[$sum]="$id $version"
			bundled_order+=("$sum")
		else
			bundled_by[$sum]+=", $id $version"
		fi
	done < <(find "$dir" -maxdepth 1 -type f -iname 'third-party-notices*' | sort)
}

entry() {
	local title=$1 id=$2 version=$3 dir license project copyright file
	dir=$(package_dir "$id" "$version") || return
	license=$(nuspec "$dir" "$id" license) || return
	[ "$license" = MIT ] || fail "$id $version is licensed '${license:-by a file}', not MIT"
	project=$(nuspec "$dir" "$id" projectUrl) || return
	copyright=$(nuspec "$dir" "$id" copyright) || return

	rule
	echo "$title"
	echo "License: MIT"
	[ -z "$project" ] || echo "Project: $project"
	[ -z "$copyright" ] || echo "Copyright: $copyright"
	echo
	file=$(find "$dir" -maxdepth 1 -type f -iname 'license*' | sort | head -n 1) || return
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

	record_bundled_notices "$dir" "$id" "$version"
}

dotnet_notices() {
	local packages runtime_version id version sum
	locks=("$tool"/src/*/packages.lock.json)
	assets=("$tool"/src/*/obj/project.assets.json)
	[ -e "${locks[0]}" ] || fail "$tool/src has no packages.lock.json"
	[ -e "${assets[0]}" ] || fail "$tool is not restored: run its check or publish first"

	packages=$(linked_packages) || return
	runtime_id=Microsoft.NETCore.App.Runtime.NativeAOT.$rid
	runtime_version=$(runtime_pack_version) || return
	[ -n "$runtime_version" ] || fail "$tool's restore downloaded no $runtime_id"
	[ "$(wc -l <<<"$runtime_version")" -eq 1 ] || fail "$tool's projects restore more than one $runtime_id"
	folder=$(jq -r '.packageFolders | keys[0]' "${assets[0]}") || return

	echo "Third-party notices for $binary, $rid"
	echo
	echo "$binary links the NuGet packages below, at the versions its lock file pins, and the .NET"
	echo "runtime NativeAOT compiles into it. Each entry gives its license; the third-party notices"
	echo "those packages bundle follow the entries."
	echo
	while read -r id version; do
		if [ -n "$id" ]; then entry "$id $version" "$id" "$version"; fi
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
	supplement
}

if [ "$kind" = cargo ]; then
	rust_notices
else
	dotnet_notices
fi
