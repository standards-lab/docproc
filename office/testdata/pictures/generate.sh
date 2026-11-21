#!/usr/bin/env bash
# generate regenerates tiff.docx, the fixture that holds a TIFF picture: Apache POI's testtiff.tif,
# checked against its SHA-256, packed byte for byte into the smallest WordprocessingML package that
# shows it, so a regeneration writes the same bytes. It needs curl and zip, laptop prerequisites
# that the check never runs.
# See office/testdata/README.md
set -euo pipefail
cd "$(dirname "$0")"

commit=6327f056ba3cfdae326a0261a236460592b80f5c
tiff_sha256=9006e4b5615beb93d5ff99a79ed62551a523ec8b0e2f881b074589d28c47b119

work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT
mkdir -p "$work/pkg/_rels" "$work/pkg/word/_rels" "$work/pkg/word/media"

fetch_tiff() {
	curl -fsSL -o "$work/pkg/word/media/image1.tiff" \
		"https://raw.githubusercontent.com/apache/poi/$commit/test-data/slideshow/testtiff.tif"
	echo "$tiff_sha256  $work/pkg/word/media/image1.tiff" | sha256sum --check --quiet
}

write_package_parts() {
	cat >"$work/pkg/[Content_Types].xml" <<'XML'
<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types"><Default Extension="rels" ContentType="application/vnd.openxmlformats-package.relationships+xml"/><Default Extension="xml" ContentType="application/xml"/><Default Extension="tiff" ContentType="image/tiff"/><Override PartName="/word/document.xml" ContentType="application/vnd.openxmlformats-officedocument.wordprocessingml.document.main+xml"/></Types>
XML
	cat >"$work/pkg/_rels/.rels" <<'XML'
<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships"><Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/officeDocument" Target="word/document.xml"/></Relationships>
XML
	cat >"$work/pkg/word/_rels/document.xml.rels" <<'XML'
<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships"><Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/image" Target="media/image1.tiff"/></Relationships>
XML
	cat >"$work/pkg/word/document.xml" <<'XML'
<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<w:document xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main" xmlns:r="http://schemas.openxmlformats.org/officeDocument/2006/relationships" xmlns:wp="http://schemas.openxmlformats.org/drawingml/2006/wordprocessingDrawing" xmlns:a="http://schemas.openxmlformats.org/drawingml/2006/main" xmlns:pic="http://schemas.openxmlformats.org/drawingml/2006/picture"><w:body><w:p><w:r><w:t>A TIFF picture follows.</w:t></w:r></w:p><w:p><w:r><w:drawing><wp:inline distT="0" distB="0" distL="0" distR="0"><wp:extent cx="6096000" cy="3810000"/><wp:docPr id="1" name="Picture 1" descr="testtiff"/><a:graphic><a:graphicData uri="http://schemas.openxmlformats.org/drawingml/2006/picture"><pic:pic><pic:nvPicPr><pic:cNvPr id="1" name="image1.tiff" descr="testtiff"/><pic:cNvPicPr/></pic:nvPicPr><pic:blipFill><a:blip r:embed="rId1"/><a:stretch><a:fillRect/></a:stretch></pic:blipFill><pic:spPr><a:xfrm><a:off x="0" y="0"/><a:ext cx="6096000" cy="3810000"/></a:xfrm><a:prstGeom prst="rect"><a:avLst/></a:prstGeom></pic:spPr></pic:pic></a:graphicData></a:graphic></wp:inline></w:drawing></w:r></w:p><w:sectPr/></w:body></w:document>
XML
}

zip_package_at_fixed_times() {
	find "$work/pkg" -exec touch -h -d '1980-01-01 00:00:00 UTC' {} +
	rm -f tiff.docx
	(cd "$work/pkg" && TZ=UTC zip -X -D -q "$OLDPWD/tiff.docx" \
		'[Content_Types].xml' _rels/.rels word/document.xml word/_rels/document.xml.rels \
		word/media/image1.tiff)
}

fetch_tiff
write_package_parts
zip_package_at_fixed_times
sha256sum tiff.docx
