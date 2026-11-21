#!/usr/bin/env bash
# generate regenerates the generated legacy fixtures: each of office's generated OOXML fixtures in
# ../ooxml (the *.libreoffice.* ones) saved by LibreOffice as .doc, .xls, or .ppt into generated/,
# named as its original with the legacy extension, with LibreOffice run as the office package runs
# it. It writes generated/MANIFEST: the image, soffice's --version line, and each file's SHA-256.
# It needs docker, a laptop prerequisite that the check never runs.
# See ../README.md
set -euo pipefail
cd "$(dirname "$0")"

picture_libreoffice_image() {
	tr '\n' ' ' <../../../picture/spec.go | sed -E 's/"[[:space:]]*\+[[:space:]]*"//g' |
		grep -oE 'gotenberg/gotenberg:[^"@]+@sha256:[0-9a-f]{64}'
}

image=$(picture_libreoffice_image)
work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT

soffice() {
	docker run --rm --init --network=none --user="$(id -u):$(id -g)" --cap-drop=ALL \
		--pull=missing --entrypoint=soffice --volume="$work:$work" --workdir="$work" \
		"$image" "$@"
}

save_each_as_legacy() {
	local original name legacy filter
	rm -rf generated
	mkdir generated
	for original in ../ooxml/*.libreoffice.docx ../ooxml/*.libreoffice.xlsx ../ooxml/*.libreoffice.pptx; do
		name=$(basename "$original")
		case $name in
		*.docx) legacy=doc filter='MS Word 97' ;;
		*.xlsx) legacy=xls filter='MS Excel 97' ;;
		*.pptx) legacy=ppt filter='MS PowerPoint 97' ;;
		esac
		cp "$original" "$work/$name"
		soffice "-env:UserInstallation=file://$work/profile-$name" --headless --norestore \
			--convert-to "$legacy:$filter" --outdir "$work/out" "$work/$name" >/dev/null 2>&1
		cp "$work/out/${name%.*}.$legacy" generated/
	done
}

write_manifest() {
	{
		echo "image: $image"
		echo "soffice: $version"
		echo
		(cd generated && sha256sum -- *.doc *.xls *.ppt)
	} >generated/MANIFEST
}

version=$(soffice --version 2>/dev/null | grep -m1 '^LibreOffice ')
save_each_as_legacy
write_manifest
cat generated/MANIFEST
