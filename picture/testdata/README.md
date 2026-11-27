# testdata

The picture package's fixtures, for the integration tier's conversions and EXIF reads on the
host's magick: its SVGs, its TIFF, HEIC, WebP, and AVIF pictures, and its EXIF pictures, each with
magick's recorded output of it, which the unit tier's stand-in serves. The tiers read these alone.
They are committed and never generated in the check; `TestFixtureSums` checks the table.

| File | Source | License | Bytes | SHA-256 |
| --- | --- | --- | --- | --- |
| `shapes.svg` | written for this package | Apache-2.0, this repository's | 681 | `e85f8bee8ba14bdd82b211d973e75d9d47a7a2b4f20dc825d0d787aa0e454d7e` |
| `ink.svg` | written for this package | Apache-2.0, this repository's | 1073 | `293e12c802db06a0ae032a401f335e394a4ff62f69fa90ab7242acd77b58b826` |
| `script-bearing.svg` | the retired `~/docs` pool's `image/vector/script-bearing.svg` | that pool's "Generated for this pool", redistributable | 648 | `a44d794e6708452ed62b56b8066b5c886d859feb395e7b97d300724eb7607950` |
| `pages.tiff` | generated for this package | Apache-2.0, this repository's | 840 | `5828cfd89abfcce70cc77a385da33b2ecbcae257e4cf0c9fc3b0da182e49eb3e` |
| `scanned-g4.tiff` | generated for this package | Apache-2.0, this repository's | 600 | `7b9d0f6ae864973668b5decb5fa8b969530980e896a573c4670b76ecb3089b7c` |
| `lzw-gray.tiff` | generated for this package | Apache-2.0, this repository's | 532 | `651a53fdff1472c9a25119d8f06319c75aff7deac7d43c2589d8b3bc2846dc10` |
| `two-images.heic` | generated for this package | Apache-2.0, this repository's | 1488 | `4029abf08a0f58f3c8f7e33f5d7f2eb9d1e4d8cb0aefe684768d341618c377cc` |
| `oriented.webp` | generated for this package | Apache-2.0, this repository's | 100 | `26af1e195bf5ab390081b5a21a9fd9eada865baf752afab8be7689142d098dfc` |
| `animated.webp` | generated for this package | Apache-2.0, this repository's | 188 | `7f00b22baa77f7b7b522bb63eda272112c5efc8b968392cd05017ae3c527e4b6` |
| `animated.avif` | generated for this package | Apache-2.0, this repository's | 1234 | `107fc1d31eeee15ba1b071bc626ffc436c062db296c1ee005ea90e0d9f8472ac` |
| `exif-gps.jpg` | generated for this package | Apache-2.0, this repository's | 677 | `2ae938f382aafee96e0d7e140081c0650af05eb44f4e0da102a269e24de21f35` |
| `exif-gps.magick.txt` | recorded from ImageMagick 7.1.2-34 | Apache-2.0, this repository's | 427 | `536c61013d4af99ca61db68bf85e6fdead301a0898b89a29a6a5136277f2bd72` |
| `exif-zoneless.heic` | generated for this package | Apache-2.0, this repository's | 739 | `e496457c4055149b7dcb69e8f805e40d196bd99904e6a4e9fa27b2133d43b396` |
| `exif-zoneless.magick.txt` | recorded from ImageMagick 7.1.2-34 | Apache-2.0, this repository's | 241 | `9bd82a559dc323e6b8ad2bd5af2d93199b313486298afd558e2ddd6ccb0efcac` |
| `exif-none.png` | generated for this package | Apache-2.0, this repository's | 127 | `56488a2881be4377efcc9c92312d106c0d65639111131601021ea23b020904a4` |
| `exif-none.magick.txt` | recorded from ImageMagick 7.1.2-34 | Apache-2.0, this repository's | 4 | `c74ca9fbdc8f2a3d035d2dabe6e476e33e61bb7741f94b8c4d947186c36d866f` |

- `shapes.svg`: a rectangle, a circle, a triangle, a rule, and a line of text, on no
  background, 320x160 CSS pixels, so 500x250 at 150 dpi.
- `ink.svg`: an ink-like drawing in the form spike-ai-pipeline-onenote writes a page's ink: a
  viewBox in ink units of 0.01 mm, a size in millimetres, no background, and each stroke an open
  path with round caps and joins. Four strokes write "hi", 40x20 mm, so 236x118 at 150 dpi.
- `script-bearing.svg`: the security fixture of the test corpus that was then at `~/docs`, copied
  byte for byte; its manifest marked it generated for the pool, its license status verified, and
  redistributable. It carries an `onload` handler, an inline `<script>`, and an `<image>` whose
  href is `http://example.invalid/tracker.png`, and its one line of text,
  `Visible text: SAFE-SVG-MARKER`, is all a safe conversion draws.
- `pages.tiff`: three pages, 40x30 white, 50x30 mid-gray, and 60x30 black, so each page's size
  tells its place.
- `scanned-g4.tiff`: three bilevel pages compressed with CCITT Group 4, as a fax or a scanner
  writes them: 64x48 with a black block at its left, 72x48 with one at its right, and 80x48
  black. It stood in for the retired `~/docs` pool's scans, which the tier never read.
- `lzw-gray.tiff`: two 8-bit grayscale pages compressed with LZW, 30x20 of gray 64 and a 40x20
  gradient.
- `two-images.heic`: two top-level images, each with a thumbnail. The primary, item 1, is stored
  48x32, its left half white and its right red, with an irot box turning it a quarter turn
  clockwise, so upright it is 32x48, white above red; the other, item 4, is 32x32 blue.
- `oriented.webp`: the same 48x32 picture, lossless, under EXIF orientation 6, so upright it is
  32x48, white above red.
- `animated.webp` and `animated.avif`: three lossless frames of 24x16, red, green, and blue, so
  the first frame is red.

The rasters were generated once on Arch Linux, with ImageMagick 7.1.2-34, libheif 1.23.6's
heif-enc, and libavif 1.4.2's avifenc, in an empty directory:

```sh
magick -size 40x30 xc:white -size 50x30 xc:gray50 -size 60x30 xc:black -compress zip pages.tiff
magick \( -size 64x48 xc:white -fill black -draw "rectangle 8,8 31,39" \) \
  \( -size 72x48 xc:white -fill black -draw "rectangle 40,8 63,39" \) -size 80x48 xc:black \
  -monochrome -compress group4 scanned-g4.tiff
magick -size 30x20 xc:gray25 -size 40x20 gradient:white-black -colorspace gray -depth 8 \
  -compress lzw lzw-gray.tiff
# primary.jpg: plain.jpg with an APP1 Exif segment, written by hand after its SOI marker, whose
# IFD holds one entry, Orientation (0x0112), SHORT, 6.
magick -size 48x32 xc:red -fill white -draw "rectangle 0,0 23,31" -quality 100 plain.jpg
magick -size 32x32 xc:blue second.png
heif-enc -q 90 -t 16 primary.jpg second.png -o two-images.heic
magick primary.jpg -define webp:lossless=true oriented.webp
magick -size 24x16 xc:red -size 24x16 xc:lime -size 24x16 xc:blue -set delay 10 -loop 0 \
  -define webp:lossless=true animated.webp
for c in red lime blue; do magick -size 24x16 xc:$c f-$c.png; done
avifenc -l f-red.png f-lime.png f-blue.png animated.avif
```

heif-enc turns the JPEG's EXIF orientation into the HEIC's irot box. Their conversions are
checked by size, page order, and pixel, and by two conversions giving the same bytes; no output
sum is recorded, since magick and its libraries are the host's, not pinned, and their PNG bytes
may differ from one build to another.

- `exif-gps.jpg`: 48x32, its left half white and its right red, whose EXIF holds Make `Docproc`,
  Model `Fixture One`, Orientation 6, an ImageDescription with a non-ASCII character,
  DateTimeOriginal `2024:05:06 07:08:09` at OffsetTimeOriginal `+02:00`, so 05:08:09 UTC,
  PixelXDimension and PixelYDimension, and a GPS position, 33° 51' 35.64" S, 151° 12' 40.2" E,
  at an altitude of 58.5 m above sea level.
- `exif-zoneless.heic`: a 32x32 blue picture whose EXIF holds Make `Docproc`, Model
  `Fixture Two`, DateTimeOriginal `2023:12:31 23:59:58` with no OffsetTimeOriginal, read as UTC,
  and a GPS position, 51° 30' 15.48" N, 0° 7' 39.6" W, carried from its JPEG by heif-enc.
- `exif-none.png`: a 4x4 blue PNG with no EXIF, and no date or time chunk.
- `*.magick.txt`: what `magick <kind>:-[0] -format '%w %h\n%[EXIF:*]' info:` prints of each, the
  picture on its standard input, as the EXIF reader runs it.

They were generated once on Arch Linux, with ImageMagick 7.1.2-34, exiv2 0.28.9, and libheif
1.23.6's heif-enc, in an empty directory:

```sh
magick -size 48x32 xc:red -fill white -draw "rectangle 0,0 23,31" -quality 90 exif-gps.jpg
exiv2 -M"set Exif.Image.Make Docproc" -M"set Exif.Image.Model Fixture One" \
  -M"set Exif.Image.Orientation 6" -M"set Exif.Image.ImageDescription Harbor, Zürich" \
  -M"set Exif.Photo.DateTimeOriginal 2024:05:06 07:08:09" \
  -M"set Exif.Photo.OffsetTimeOriginal +02:00" \
  -M"set Exif.Photo.PixelXDimension 48" -M"set Exif.Photo.PixelYDimension 32" \
  -M"set Exif.GPSInfo.GPSLatitudeRef S" -M"set Exif.GPSInfo.GPSLatitude 33/1 51/1 3564/100" \
  -M"set Exif.GPSInfo.GPSLongitudeRef E" -M"set Exif.GPSInfo.GPSLongitude 151/1 12/1 4020/100" \
  -M"set Exif.GPSInfo.GPSAltitudeRef 0" -M"set Exif.GPSInfo.GPSAltitude 585/10" exif-gps.jpg
magick -size 32x32 xc:blue -quality 90 zoneless.jpg
exiv2 -M"set Exif.Image.Make Docproc" -M"set Exif.Image.Model Fixture Two" \
  -M"set Exif.Photo.DateTimeOriginal 2023:12:31 23:59:58" \
  -M"set Exif.GPSInfo.GPSLatitudeRef N" -M"set Exif.GPSInfo.GPSLatitude 51/1 30/1 1548/100" \
  -M"set Exif.GPSInfo.GPSLongitudeRef W" -M"set Exif.GPSInfo.GPSLongitude 0/1 7/1 3960/100" \
  zoneless.jpg
heif-enc -q 90 zoneless.jpg -o exif-zoneless.heic
magick -size 4x4 xc:blue -define png:exclude-chunks=date,time exif-none.png
for f in exif-gps.jpg:jpeg exif-zoneless.heic:heic exif-none.png:png; do
  n=${f%%:*}; magick "${f##*:}:-[0]" -format '%w %h\n%[EXIF:*]' info: < "$n" > "${n%.*}.magick.txt"
done
```

The magick outputs are ImageMagick 7.1.2-34's, and another build may print other bytes, so the
integration tier checks the typed fields the reader takes from the host's magick, not these bytes.
