#!/usr/bin/env bash
# generate regenerates the document fixtures from clean.typ: clean.pdf and its ground truth, then
# degraded.pdf and its ground truth, byte-reproducibly, and prints their SHA-256 sums. It needs
# typst 0.15.1 and ImageMagick 7.1.2, laptop prerequisites that the check never runs, and jq.
# See README.md
set -euo pipefail
cd "$(dirname "$0")"

work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT

write_clean_pages_and_truth() {
	local truth i
	typst compile --ignore-system-fonts clean.typ clean.pdf
	truth=$(typst eval --ignore-system-fonts 'query(<truth>).map(it => it.value)' --in clean.typ) || return
	pages=$(jq length <<<"$truth") || return
	mkdir -p clean
	for ((i = 0; i < pages; i++)); do
		jq -r ".[$i]" <<<"$truth" >"clean/page-$((i + 1)).txt"
	done
}

render_gray_pages() {
	local p
	typst compile --ignore-system-fonts --format png --ppi 150 clean.typ "$work/clean-{p}.png"
	for ((p = 1; p <= pages; p++)); do
		magick "$work/clean-$p.png" -background white -alpha remove -alpha off -colorspace Gray \
			"$work/gray-$p.png"
	done
}

degrade() {
	local page=$1 source=$2
	shift 2
	magick "$work/gray-$source.png" "$@" -depth 8 "$work/degraded-$page.png"
	cp "clean/page-$source.txt" "degraded/page-$page.txt"
}

write_degraded_pages() {
	mkdir -p degraded
	degrade 1 1 -background white -rotate 2.5 -gravity center -extent 1275x1650
	degrade 2 3 -background white -rotate 7 -gravity center -extent 1275x1650
	degrade 3 3 +level 55%,100%
	degrade 4 5 +level 78%,96%
	degrade 5 5 -seed 7 -attenuate 0.6 +noise Impulse
	degrade 6 6 -seed 11 -attenuate 1.2 +noise Impulse
	degrade 7 2 -blur 0x1.8
	degrade 8 1 -blur 0x3
	degrade 9 2 -background white -rotate 4 -gravity center -extent 1275x1650 \
		-blur 0x1.2 +level 40%,100% -seed 13 -attenuate 0.5 +noise Impulse
}

wrap_degraded_pdf() {
	magick -define pdf:create-epoch=1767225600 -define pdf:modify-epoch=1767225600 \
		-define pdf:no-identifier=true \
		"$work"/degraded-{1..9}.png -alpha off -depth 8 -units PixelsPerInch -density 150 \
		-compress Zip degraded.pdf
}

write_clean_pages_and_truth
render_gray_pages
write_degraded_pages
wrap_degraded_pdf
sha256sum clean.pdf clean/*.txt degraded.pdf degraded/*.txt
