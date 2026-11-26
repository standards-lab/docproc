#!/usr/bin/env bash
# generate regenerates the ODF fixtures into generated/: three of office's OOXML fixtures in
# ../ooxml saved by LibreOffice as ODF documents and templates (tables.libreoffice.docx as .odt and
# .ott, cells.libreoffice.xlsx as .ods and .ots, slides.libreoffice.pptx as .odp and .otp),
# drawing.fodg, a flat ODF drawing written by hand, saved by Draw as .odg and .otg, with LibreOffice
# run in the picture package's default image as the office package runs it; and
# tables.libreoffice.docx saved as an .odt encrypted with the password "docproc", through encrypt.py
# and the host's soffice and Python UNO bridge, since soffice's --convert-to sets no password. It
# writes generated/MANIFEST: the image, each soffice's --version line, and each file's SHA-256. It
# needs docker, soffice, and python3 with uno, laptop prerequisites that the check never runs.
# See office/testdata/README.md
set -euo pipefail
cd "$(dirname "$0")"

picture_libreoffice_image() {
	tr '\n' ' ' <../../../picture/spec.go | sed -E 's/"[[:space:]]*\+[[:space:]]*"//g' |
		grep -oE 'gotenberg/gotenberg:[^"@]+@sha256:[0-9a-f]{64}'
}

image=$(picture_libreoffice_image)
work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT

soffice_image() {
	docker run --rm --init --network=none --user="$(id -u):$(id -g)" --cap-drop=ALL \
		--pull=missing --entrypoint=soffice --volume="$work:$work" --workdir="$work" \
		"$image" "$@"
}

save_as() {
	local source=$1 ext=$2 filter=$3 name
	name=$(basename "$source")
	cp "$source" "$work/$name"
	soffice_image "-env:UserInstallation=file://$work/profile-$name-$ext" --headless --norestore \
		--convert-to "$ext:$filter" --outdir "$work/out" "$work/$name" >/dev/null 2>&1
	cp "$work/out/${name%.*}.$ext" generated/
}

rm -rf generated
mkdir generated
save_as ../ooxml/tables.libreoffice.docx odt writer8
save_as ../ooxml/tables.libreoffice.docx ott writer8_template
save_as ../ooxml/cells.libreoffice.xlsx ods calc8
save_as ../ooxml/cells.libreoffice.xlsx ots calc8_template
save_as ../ooxml/slides.libreoffice.pptx odp impress8
save_as ../ooxml/slides.libreoffice.pptx otp impress8_template
save_as drawing.fodg odg draw8
save_as drawing.fodg otg draw8_template
python3 -I encrypt.py "$PWD/../ooxml/tables.libreoffice.docx" "$PWD/generated/encrypted.odt" "$work/profile-encrypted"

{
	echo "image: $image"
	echo "soffice: $(soffice_image --version 2>/dev/null | grep -m1 '^LibreOffice ')"
	echo "host soffice, encrypted.odt: $(soffice --version 2>/dev/null | grep -m1 '^LibreOffice ')"
	echo
	(cd generated && sha256sum -- *.od? *.ot?)
} >generated/MANIFEST
cat generated/MANIFEST
