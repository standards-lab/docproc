# testdata

The sevenzip package's fixtures, which the dispatch package's archive stage reads too: archives
7-Zip reads and the standard library doesn't. They are committed and never fetched or generated
in the check, which holds each to its SHA-256 below (`TestFixtureSums`).

## commons-compress

Five fixtures from [Apache Commons Compress](https://github.com/apache/commons-compress)'s test
resources, `src/test/resources/`, at commit `ab9953f5341dbd6c4d7eee7f6ec7696987de7696`
(2026-11-06, master, the first to hold the two lorem-ipsum ZIPs; release 1.28.0 has neither), under
the Apache License 2.0, its `LICENSE.txt` and `NOTICE.txt` beside them. `test-method-xz.zip` is
under `org/apache/commons/compress/zip/` there. Each was fetched from raw.githubusercontent.com at
that commit and tested whole with 7zz 26.05 (`7zz t`) before it was copied.

| File | Source | License | Bytes | SHA-256 |
| --- | --- | --- | --- | --- |
| `commons-compress/lorem-ipsum-deflate64.zip` | `src/test/resources/lorem-ipsum-deflate64.zip` | Apache-2.0 | 34821 | `aa5542a9408d62f28e9032b98cb4f614b5c29eefe006532d0cd1d6a4e5c62491` |
| `commons-compress/lorem-ipsum-implode.zip` | `src/test/resources/lorem-ipsum-implode.zip` | Apache-2.0 | 42925 | `671863618d4ea2be776d018c2def9e3696eaf96b8dbe7b4953fdb2cc7851813f` |
| `commons-compress/bzip2-zip.zip` | `src/test/resources/bzip2-zip.zip` | Apache-2.0 | 209 | `650a1ad2c88b0ad2f7686dc4502138848cb36016da535692b1a683675c2f5ca5` |
| `commons-compress/test-method-xz.zip` | `src/test/resources/org/apache/commons/compress/zip/test-method-xz.zip` | Apache-2.0 | 4044 | `3634ae8418575f4556b7ad7f7b737a92d2a123f03e51b4ada569225ab80bc0be` |
| `commons-compress/bla.deflate64.7z` | `src/test/resources/bla.deflate64.7z` | Apache-2.0 | 503 | `1b8b0e7cef719c9957abd980fff29579024fb4ef51ffed1e6feb29813d460566` |
| `commons-compress/LICENSE.txt` | `LICENSE.txt` | Apache-2.0 | 11358 | `51b88fd3e9e24edcdce1dd145a4fca2dcd12911407f00b06dfa6ff600f9733c1` |
| `commons-compress/NOTICE.txt` | `NOTICE.txt` | Apache-2.0 | 178 | `5318998af3591f72e0e3e80667d32ba334c080399011a767f93e611d907036ca` |

What each holds:

- `lorem-ipsum-deflate64.zip`: `lorem-ipsum.txt`, 144,060 bytes, Deflate64 (method 9).
- `lorem-ipsum-implode.zip`: `LOREM.TXT`, the same 144,060 bytes, imploded (method 6).
- `bzip2-zip.zip`: `lots-of-as`, 42 bytes, bzip2 (method 12), which the archive stage reads
  through `compress/bzip2`, without 7-Zip.
- `test-method-xz.zip`: `LICENSE.txt`, 11,357 bytes, xz (method 95).
- `bla.deflate64.7z`: a 7z of `test1.xml` (610 bytes) and `test2.xml` (82 bytes), Deflate64.

## libarchive

Three ZIPs and nine RARs from [libarchive](https://github.com/libarchive/libarchive)'s test suite,
`libarchive/test/`, at its release `v3.8.11` (commit `03036ffa409a2aa1c5bbad35be2202641a040fda`),
under libarchive's 2-clause BSD license, its `COPYING` beside them. libarchive keeps them
uuencoded; each `.uu` was fetched from raw.githubusercontent.com at that commit and decoded, with
Python's `binascii.a2b_uu`, to the file below, which 7zz tested whole before it was copied: the
ZIPs with 26.05, the RARs with 26.06, an encrypted one with its password from libarchive's test,
and a split set's first volume with the set's other volumes beside it, which aren't copied.

| File | Source | License | Bytes | SHA-256 |
| --- | --- | --- | --- | --- |
| `libarchive/test_read_format_zip_lzma.zipx` | `test_read_format_zip_lzma.zipx.uu`, SHA-256 `1d2b2126df4a27d777c1128efe9a8fba7c922dc0195f4c934aba65d24916d739`; LZMA (method 14) | BSD-2-Clause | 713 | `a8fd1714eff610dae52e434a759818829bf8040b5016aa771b9a33965d2dbc89` |
| `libarchive/test_read_format_zip_ppmd8.zipx` | `test_read_format_zip_ppmd8.zipx.uu`, SHA-256 `edd1a418baf069f41f79b93ec1977a447217c684041fc030305d18dfc354bfbc`; PPMd (method 98) | BSD-2-Clause | 616 | `3524501a47b1947a162cb2b33ccabc8d2bd8c2311b86c0a6af6f7ba28eedbf6e` |
| `libarchive/test_read_format_zip_zstd.zipx` | `test_read_format_zip_zstd.zipx.uu`, SHA-256 `cd0e2b6a53461d137341f8ad306e9a62ac93cdfc45b62e2e1545cf4e9a90ee31`; zstd (method 93) | BSD-2-Clause | 647 | `ca15c7eabde8b918f1c91ff3a173012175b0599664f007fc3cb0bdf00c767377` |
| `libarchive/test_read_format_rar.rar` | `test_read_format_rar.rar.uu`, SHA-256 `d1e75b4120995bce82fc4e72ebf8b80d18ea69fa34ac62de383b8393f92afa09`; RAR4 | BSD-2-Clause | 336 | `d421b86f6290aefad61b2a36737253b2b30fe27c156bd95abfc230f24fe0307e` |
| `libarchive/test_read_format_rar_encryption_partially.rar` | `test_read_format_rar_encryption_partially.rar.uu`, SHA-256 `efc09869294e03f6a363fd8e41cc5838e4a2d08e32a18773a95161a90965f3a3`; RAR4 | BSD-2-Clause | 173 | `144ab2270d4f28a61a857d5ae6248e90ab3be5ed46dc2fc59f9c0a98c7f49407` |
| `libarchive/test_read_format_rar_encryption_header.rar` | `test_read_format_rar_encryption_header.rar.uu`, SHA-256 `2e56bf61f98eb519e41ef19303d563d1331cc5fd126212e4aba0d0a0e8167057`; RAR4 | BSD-2-Clause | 220 | `67fa99d6f1d07d6dce56da85d22cd6271c16fbc7aad5efc6046b30425cc799b0` |
| `libarchive/test_read_format_rar_multivolume.part0001.rar` | `test_read_format_rar_multivolume.part0001.rar.uu`, SHA-256 `7e9d0e03f1335a812e09a03425dfdf8e4f763a8c536904b88ff9400bd2e44d68`; RAR4 | BSD-2-Clause | 60000 | `6c4d7f55cbf0a44aa0cafb283b0fd954ef625c9afc2fbe1cce4f201fc2508c63` |
| `libarchive/test_read_format_rar5_compressed.rar` | `test_read_format_rar5_compressed.rar.uu`, SHA-256 `7a87af5035430d2307f9a183b2da8a71c95b3c0a5b8e8d1f172ef3343d4b8620`; RAR5 | BSD-2-Clause | 436 | `68f9d7e2662cd5bc5607ba9c6bf1edbf121cf3b08011a7d8643091791750a7c8` |
| `libarchive/test_read_format_rar5_encrypted.rar` | `test_read_format_rar5_encrypted.rar.uu`, SHA-256 `5cc262d34226c3c9298df7bda065eee1523f8ff6ebaced486f48117095661a0b`; RAR5 | BSD-2-Clause | 391 | `72b69a1d5f5757511e683a9207166c60e91ce0846ae446b8b332b085f9e0ba48` |
| `libarchive/test_read_format_rar5_encrypted_filenames.rar` | `test_read_format_rar5_encrypted_filenames.rar.uu`, SHA-256 `f113860fd2f06aa13b2f933150a1f80988038cdeef393fbb6a1a0d302c86148e`; RAR5 | BSD-2-Clause | 718 | `b2b98004c9cbf4f2a4000380ad47a16dea99f2d50835b1602c40d724eb82c300` |
| `libarchive/test_read_format_rar5_symlink.rar` | `test_read_format_rar5_symlink.rar.uu`, SHA-256 `7c159d88ca351a8324e2d85c9455d26e86e7df4c79f46ee3db9bfb5a89ffdd44`; RAR5 | BSD-2-Clause | 190 | `a7336968564786ea5ff106fff2f6ef8dfd8ad5ecca86fbd1db3d63ba42bcd29a` |
| `libarchive/test_read_format_rar5_multiarchive.part01.rar` | `test_read_format_rar5_multiarchive.part01.rar.uu`, SHA-256 `93481eb23735afd0b5c527a04585b6d79f0f821ce14d33e0c788c243a54e3e49`; RAR5 | BSD-2-Clause | 15360 | `2f3894dc4b8a1bba90830ef1f89f457b0295b6f9030c70febd57283721bc2ce3` |
| `libarchive/COPYING` | `COPYING` | BSD-2-Clause | 3089 | `30e556b3959e3985d66efefec5eaac51d4995053caa1d3cffe6eb916f146f229` |

What each holds:

- The three ZIPs: `vimrc`, 912 bytes, CRC-32 `BA8E3BAA`, under one method each.
- `test_read_format_rar.rar`: a RAR4 of `test.txt` and `testdir/test.txt`, 20 bytes each,
  `testlink`, a symbolic link, its Unix mode `lrwxrwxrwx`, and two directories.
- `test_read_format_rar_encryption_partially.rar`: a RAR4 of `foo.txt`, encrypted (password
  `12345678`), and `bar.txt`, not, its headers clear.
- `test_read_format_rar_encryption_header.rar`: a RAR4 whose headers are encrypted (password
  `12345678`), so nothing lists without it.
- `test_read_format_rar_multivolume.part0001.rar`: the first of a RAR4 split set's four volumes,
  its main header's volume flag set.
- `test_read_format_rar5_compressed.rar`: a RAR5 of `test.bin`, 1,200 bytes.
- `test_read_format_rar5_encrypted.rar`: a RAR5 of `a.txt` and `c.txt`, not encrypted, and
  `b.txt` and `d.txt`, encrypted (passwords `password` and `password2`), its headers clear.
- `test_read_format_rar5_encrypted_filenames.rar`: a RAR5 whose headers are encrypted (password
  `password`): its encryption header comes first, so its volume flag, clear, can't be read either.
- `test_read_format_rar5_symlink.rar`: a RAR5 of `file.txt`, `symlink.txt` and `dirlink`,
  symbolic links, and the directory `dir`.
- `test_read_format_rar5_multiarchive.part01.rar`: the first of a RAR5 split set's eight volumes,
  its archive flags' volume bit set.

## generated

This repository's, Apache-2.0, written by `generate.go`: run `mise exec -- go run generate.go` in
this directory. It writes the zstd streams and cabinets by hand, every byte fixed, and the 7z
archives and xz streams through the 7zz mise pins, from files whose content, mode, and order are
fixed, with no times stored and one thread, so a rerun writes the same bytes, the 7z archives with
7zz 26.05 and 26.06 alike and the xz streams with 26.06, but for the two encrypted 7z archives,
whose salt and IV 7zz draws at random: a rerun writes them anew, and their sums below then change.
Their password is `infected`, which no test gives. `~/docs`' archives aren't redistributable, so
none is copied; the integration tier reads `~/docs` itself when the machine has it.

| File | Source | License | Bytes | SHA-256 |
| --- | --- | --- | --- | --- |
| `generated/clean.7z` | `generate.go`, 7zz | Apache-2.0 | 230 | `1f2399592d26b4caef028ed4bf250352704ebb5ec7eab946ee5358b5867b0136` |
| `generated/traversal.7z` | `generate.go`, 7zz | Apache-2.0 | 224 | `e76b303be0fe53ca1c6c4c28cff15f5c8b0956c84a03ba657e148be37813896b` |
| `generated/absolute.7z` | `generate.go`, 7zz | Apache-2.0 | 191 | `275a07f391372fd640d4f1ab71601e89220d9e59cc6900f3177a3eef77677f95` |
| `generated/symlink.7z` | `generate.go`, 7zz | Apache-2.0 | 188 | `0328adf055bd5fec49bd2b1909fd5d175ad7a5f5d8b4275d405b873c65784552` |
| `generated/device.7z` | `generate.go`, 7zz | Apache-2.0 | 169 | `682478771d5acc98ad424d1a6946a8ec456a7d47c9c553383f8ecee35576e2d1` |
| `generated/bomb.7z` | `generate.go`, 7zz | Apache-2.0 | 1408 | `f8618734e9ad7848eece0a0fdd89d173f8714df1a31267351b4a98d8dbe47fa0` |
| `generated/encrypted.7z` | `generate.go`, 7zz | Apache-2.0 | 254 | `77d4e80363d4b4892eb82c85f3e83123dfad3c7efd33090f402e58a2e6d87a00` |
| `generated/encrypted-headers.7z` | `generate.go`, 7zz | Apache-2.0 | 238 | `00b37c48141bc578d4d73893256a6a7794a3d99d89d84a889a877f4be57024da` |
| `generated/multiblock.7z` | `generate.go`, 7zz | Apache-2.0 | 1366 | `be7f9902e99963de26363a984c807739ea50dd61d094354a47ae73a09d8338ce` |
| `generated/mixed.7z` | `generate.go`, 7zz | Apache-2.0 | 5316 | `da1be1183d80c3f4afa9a4e86cef3f3cca8620771c4dfa08ed401d887906763a` |
| `generated/clean.tar.zst` | `generate.go` | Apache-2.0 | 3593 | `d6f55782a58c67086abb62835831a0c42cf6e875ea9bcf659002be3d5c986338` |
| `generated/bomb.zst` | `generate.go` | Apache-2.0 | 262 | `aceb8add8a2dd576901a2ec13d208aabf9d7779baf18dc625a4a253b637e223c` |
| `generated/declared-bomb.zst` | `generate.go` | Apache-2.0 | 18 | `b00abcbad0abe6830e32e14db8c8ec466b0305329373c65c783d56b2b9d4a1e5` |
| `generated/notebook.cab` | `generate.go`, onenote.rs's sections | MPL-2.0 (the sections), Apache-2.0 (the cabinet) | 3659 | `a6659c05b4679bab61045b4218034733e27db3296fcfc2fc205e5358b9f23707` |
| `generated/traversal.cab` | `generate.go` | Apache-2.0 | 177 | `cf758349ae47bb0fa8959cfad9e54a09e60f7f25b924d8ecbb9f815de93d7ad3` |
| `generated/absolute.cab` | `generate.go` | Apache-2.0 | 147 | `1b658724750d55e997e5f36cd133338dc1b54cd6388c1f8861db9d22f4814c2f` |
| `generated/bomb.cab` | `generate.go` | Apache-2.0 | 14662 | `b70d13ed29cb6ba2317828613e5b6566445ded7d27e667fbf705306936a9787b` |
| `generated/linebreak.cab` | `generate.go` | Apache-2.0 | 232 | `b2bbd0a9af45328e3d44196cbd687c9758ea9365b0e1c71188fb25e1430c11ea` |
| `generated/notes.txt.xz` | `generate.go`, 7zz | Apache-2.0 | 72 | `7a4308af2664a08b673a34e3469cefde312b6c462dc8e52921c110d730764192` |
| `generated/clean.txz` | `generate.go`, 7zz | Apache-2.0 | 232 | `84c171f622cb9a10bac27c35ac58644447ae20bdacc07ae8ba361ae216bcafe2` |
| `generated/bomb.xz` | `generate.go`, 7zz | Apache-2.0 | 1352 | `bd4d2095a33dcdc1b2f18bf5576f15dee5c27050a0afca22ed5af0989abca140` |

What each holds:

- `clean.7z`: a directory, `docs`, and a text and a 3x1 PNG's signature under it,
  `docs/notes.txt` and `docs/pixel.png`: no hazard.
- `traversal.7z`: an ordinary entry, `notes.txt`, and `docs/../../evil.txt`, ".." elements, a
  file renamed in the archive with `7zz rn`.
- `absolute.7z`: `/tmp/evil.txt`, an absolute path, and `C:\evil.txt`, a drive letter, each
  renamed in the archive.
- `symlink.7z`: an ordinary entry and `passwd`, a symbolic link to `/etc/passwd`, stored as a
  link (`-snl`), its Unix mode `lrwxrwxrwx`.
- `device.7z`: an ordinary entry and `pipe`, a FIFO stored as one, its Unix mode `prw-r--r--`. A
  character or block device needs root to make, so none is stored; a 7z can't carry a hard link,
  which 7-Zip stores as a copy.
- `bomb.7z`: 8 MiB of zeros in one entry, about 6,000:1 against the archive: past the default
  ratio, under the default 1 GiB, so a test lowers the size to see the rule fire.
- `encrypted.7z`: an ordinary entry, `notes.txt`, and `secret.txt`, added encrypted, its headers
  clear.
- `encrypted-headers.7z`: `secret.txt` with its headers encrypted too (`-mhe=on`), so nothing
  lists without the password.
- `multiblock.7z`: twelve text files, `blocks/f01.txt` to `blocks/f12.txt`, each its own lines
  and length, three to a solid block (`-ms=3f`): four LZMA2 blocks, so a whole read crosses a
  block three times.
- `mixed.7z`: a block of each of five methods, LZMA2, PPMd, BZip2, Deflate, and Copy (two
  blocks, Copy being unsolid), each method's two files added to the archive in turn with
  `-m0=<method>`, so six blocks of mixed methods; beside the LZMA2 files, `empty.txt`, an empty
  file, and `passwd`, a symbolic link stored as one (`-snl`), which a whole read excludes.
- `clean.tar.zst`: inspect's `clean.tar` (`inspect/testdata/`) as one zstd frame of raw blocks,
  declaring no content size.
- `bomb.zst`: 8 MiB of zeros in RLE blocks, about 32,000:1, declaring no content size: past the
  default ratio, under the default size, as written.
- `declared-bomb.zst`: a frame whose header declares 4 GiB, past both defaults by its header
  alone, which 7-Zip's listing reports in its Method; its content is 1 KiB of zeros, so it fails
  to decompress whole.
- `notebook.cab`: a OneNote package's shape, an MSZIP cabinet of `Open Notebook.onetoc2` and
  `New Section 1.one` from onenote.rs's `New Section Group` (`tools/onenote/tests/fixtures/
  onenote-rs/`), named .cab, so it routes to the archive stage rather than onenote. No .onepkg is
  committed anywhere to copy: onenote's tests build theirs at run time, with the `cab` crate, as
  this does.
- `traversal.cab`: an ordinary entry and `docs\..\..\evil.txt`, which 7-Zip lists with slashes.
- `absolute.cab`: `\tmp\evil.txt` and `C:\evil.txt`, listed as `/tmp/evil.txt` and
  `C:/evil.txt`.
- `bomb.cab`: 8 MiB of zeros in one entry, MSZIP, about 570:1: under both default thresholds, so
  a test lowers both to see the rule fire.
- `linebreak.cab`: `before.txt`, `line` and `break.txt` joined by a line break, `return` ending
  in a carriage return, and `after.txt`, written by hand since 7zz replaces a line break with
  `_` when it adds a file or renames one. 7zz lists the line break as `_`, `line_break.txt`, and
  the carriage return as stored.
- `notes.txt.xz`: `notes.txt`, 18 bytes, as one xz stream, LZMA2, which declares its size in its
  index.
- `clean.txz`: inspect's `clean.tar` as one xz stream, named `.txz`, so its content is named
  `clean.tar`.
- `bomb.xz`: 8 MiB of zeros, about 6,200:1, its size declared: past the default ratio, under the
  default size, as `bomb.7z` is.
