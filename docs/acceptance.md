# acceptance

Measured 2026-11-19 on an Intel Core Ultra 9 185H, 16 cores and 22 threads, with gs 10.08.0.
This page holds the results of v1.ai.processing's acceptance task, a section for each
measurement.

## PDF pages in shared gs processes

`document.Preparer.Images` runs a PDF's pages through shared gs processes: at concurrency C, lane
k takes pages k, k+C, k+2C, and so on, up to `ImagesRequest.PagesPerProcess` pages a process, with
one txtwrite process before each render process when the request reads text. The per-page path,
as released on origin/main (v0.3.0), runs two gs processes for each page, its text and its render.
Every measurement here runs the request cmd/docproc starts with: 150 DPI, `Text`, `MaxPixels`
64,000,000, and the gs Tool's Timeout at 10 minutes.

### The sweep and the default

The sweep timed `Images` on the 915-page CFR volume (cfr-2024-title32-vol1.pdf) at concurrency 1,
three runs at each `PagesPerProcess`, the counts interleaved within each run.

| PagesPerProcess | Median (s) | Range (s) | Against the fastest median |
| --- | --- | --- | --- |
| 1 | 140.8 | 139.3–148.3 | +49.5% |
| 4 | 104.1 | 103.0–105.1 | +10.5% |
| 8 | 97.6 | 97.4–98.6 | +3.6% |
| 16 | 95.3 | 94.4–96.9 | +1.2% |
| 32 | 94.6 | 93.4–97.3 | +0.4% |
| 64 | 94.2 | 92.2–95.2 | fastest |
| 128 | 97.2 | 93.7–97.4 | +3.1% |
| 256 | 96.1 | 95.7–100.6 | +2.0% |
| 915 | 140.8 | 138.9–146.5 | +49.5% |

The 915 row isn't one process for the whole PDF. The sweep's build wrote every page into gs's
page list, and gs refuses an argument past 2047 bytes, so that process failed and each page fell
back to processes of its own. A build that writes consecutive pages as a range ran the whole PDF
in one process in 99.3 s (99.2–100.5 s), and 64 in 95.1 s, in three interleaved runs.

The default is 8, by this rule: the smallest count whose median comes within 5% of the fastest
count's median. 8 took 97.6 s against 64's 94.2 s, +3.6%; 4 took 104.1 s, +10.5%. The rule was
first anchored to one process for the whole PDF, which chose 4, at 104.1 s against the whole PDF's
99.3 s, +4.8%. One whole-PDF process isn't the fastest setting: an interleaved rerun of the three,
three runs each, took 102.9 s at 4, 97.4 s at 8, and 99.6 s for the whole PDF.

### Adoption at 8

The adoption run timed both paths over all 68 PDFs in `~/docs`, at concurrency 1 and 4, three
runs each way, the per-page and shared runs interleaved. A PDF is faster or slower beyond the
spread when every run of one path took less time than every run of the other.

The 915-page CFR volume (cfr-2024-title32-vol1.pdf):

| Concurrency | Per-page median (s) | Shared median (s) | Speed-up |
| --- | --- | --- | --- |
| 1 | 135.79 (135.76–140.50) | 94.79 (94.75–95.62) | 1.43x |
| 4 | 40.60 (38.14–40.61) | 27.58 (27.56–27.73) | 1.47x |

All 68 PDFs, each PDF's median summed:

| Concurrency | Per-page (s) | Shared (s) | Faster beyond the spread | Slower beyond the spread |
| --- | --- | --- | --- | --- |
| 1 | 331.0 | 237.7 | 16 | 0 |
| 4 | 124.9 | 96.0 | 7 | 3 |

At concurrency 1 every PDF of more than one page is faster beyond the spread, from 1.17x on
scanned.pdf to 2.16x on dodm-5200-01.pdf, and two one-page PDFs are too, by 1% and 5%. At
concurrency 4 a PDF of four pages or fewer gives each lane one page, which is its own process, as
on the per-page path; the seven faster beyond the spread are the seven PDFs of six pages or more.
A one-page PDF does the same work either way, a count run and its page's own processes, and its
median ratio is between 0.95x and 1.14x at concurrency 1 and between 0.88x and 1.06x at 4.

The three PDFs slower beyond the spread at concurrency 4 are one-page PDFs, 2–10 ms slower in
three runs. Each was rerun 10 times a way at concurrency 4, interleaved, on a scratch copy,
against a per-page build of origin/main, with the build that closes the measurement;
tables-033c1efd…_pg5_pg1.pdf, slower beyond the spread in the first adoption run, at
`PagesPerProcess` 4, was rerun the same way at 8 before it. Times are in ms, and t is Welch's t
of the shared mean less the per-page mean.

| PDF | Way | Median | Mean | SD | Range | t |
| --- | --- | --- | --- | --- | --- | --- |
| simple-open-password.pdf | per-page | 22.0 | 22.8 | 2.1 | 20–27 | |
| | shared | 21.0 | 21.7 | 1.9 | 20–26 | −1.22 |
| corruption-one-byte-missing.pdf | per-page | 112.0 | 114.0 | 4.2 | 110–122 | |
| | shared | 113.5 | 115.3 | 4.4 | 111–124 | +0.67 |
| multi_column-0925342e…_page_6_pg1.pdf | per-page | 239.0 | 239.1 | 3.5 | 234–244 | |
| | shared | 237.0 | 237.9 | 4.1 | 233–245 | −0.71 |
| tables-033c1efd…_pg5_pg1.pdf | per-page | 197.0 | 197.0 | 2.5 | 192–201 | |
| | shared | 198.0 | 198.1 | 3.1 | 194–204 | +0.87 |

No difference is beyond noise: every |t| is below 1.3, and two of the four go the other way. The
one gs run that counts a one-page PDF also reads whether it is a portfolio, which is the only work
the shared path adds to it. That run, against the released sizes run on
corruption-one-byte-missing.pdf, 30 runs each, took 22.3 ± 1.3 ms against 22.7 ± 1.2 ms. The
extra work isn't a measurable cost.

The rows' full paths are `~/docs/pdf/edge/encrypted/simple-open-password.pdf`,
`~/docs/pdf/edge/malformed/corruption-one-byte-missing.pdf`, and
`~/docs/pdf/scanned/olmocr-bench/` for
`multi_column-0925342e1efabb7957efa34203f65d3cbc4d_page_6_pg1.pdf` and
`tables-033c1efd2c0e15b829f9e9164f0e7bc1fecf_pg5_pg1.pdf`.

### Identity

v1.ai.processing's final task measured it again on 2026-11-23, on a3d861a, the release candidate
that is tagged v0.4.2; cmd/docproc/v0.2.2 follows it at SHIP. The harness is the Recipe's, built
twice: once requiring docproc v0.3.0, the per-page path, and once replacing it with the checkout,
the shared path at the default `PagesPerProcess`, 8. Each ran once over a scratch copy of the 68
PDFs, the shared path at concurrency 1 and 4 and the per-page path at 1, with every `DOCPROC_`
variable unset and the host's tools as they are: gs 10.08.0 (Arch's ghostscript 10.08.0-1) and Go
1.27.4.

Over the 1,387 pages and five failed counts, every line, each page's number, DPI, PNG SHA-256,
text SHA-256, file name, and error, is identical to the per-page path's, and the shared path's
lines are identical at concurrency 1 and 4, apart from two PDFs, each a change a release made:

- **digitally-signed-3d-portfolio.pdf**, the stated exception. The per-page path renders, and
  reads the text of, the first embedded document's first page in place of the portfolio's one
  page. Since v0.4.1 a portfolio's page is its own, the cover that asks for Acrobat 9 or Adobe
  Reader 9, so its PNG and text SHA-256 differ; its number, DPI, and name agree.
- **compacted-syntax.pdf**. The per-page path fails its one page with "no PNG on stdout", gs's
  "Page drawing error occurred" ahead of the PNG on stdout. Since v0.4.0 (831f865) that message
  goes to stderr, and the page renders. Its PNG is byte for byte what follows the message on
  stdout when gs runs with v0.3.0's render arguments, and its text is what v0.3.0's text arguments
  give.

Five PDFs fail as before, their count failing with the same error, word for word:
encryption-openpassword.pdf, simple-open-password.pdf, and unicode-password-corrigendum5.pdf in
`edge/encrypted/`, and header-no-version-info.pdf and no-document-catalog.pdf in
`edge/malformed/`. The sixth, compacted-syntax.pdf, now prepares.

Each run's stream wall times, summed over the 68 PDFs, and the CFR volume's alone, beside the
adoption run's medians, summed for the 68:

| Path | Concurrency | 68 PDFs (s) | Adoption (s) | CFR volume (s) | Adoption (s) |
| --- | --- | --- | --- | --- | --- |
| per-page, v0.3.0 | 1 | 361.1 | 331.0 | 153.78 | 135.79 |
| shared, a3d861a | 1 | 233.6 | 237.7 | 94.38 | 94.79 |
| shared, a3d861a | 4 | 89.4 | 96.0 | 26.54 | 27.58 |

These are one run each against adoption's medians of three. The per-page run took 9% longer over
the 68 and 13% longer on the CFR volume; the shared runs took 2% less over the 68 at concurrency
1 and 7% less at 4, and up to 4% less on the CFR volume.

`~/docs` was only read: its listing, the path, size, and modification time of each of its 700
entries, was the same before the runs and after.

Acceptance measured it first, on 2026-11-19, against a build of the shared path before 831f865:
every page line was identical to the per-page path's on all 68 PDFs, at concurrency 1 and 4,
against the per-page path's at concurrency 1. Six PDFs failed as on the per-page path: the five gs
can't open failed their count with the same error, word for word, and compacted-syntax.pdf's one
page failed its render with the same error.

### Defects the measurement found and fixed

- **Portfolios.** gs renders, and reads the text of, a PDF portfolio's embedded documents in place
  of its own pages, the run's pages applied to each document in turn. A shared process over
  digitally-signed-3d-portfolio.pdf wrote five PNGs and five texts for its one page and kept the
  first of each. The run that counts the pages now reads whether the PDF is a portfolio, by gs's
  own test, its PDFInfo holding /Collection, and a portfolio's pages each take processes of their
  own.
- **The argument limit.** gs refuses an argument past 2047 bytes as "Command too long" and exits
  1, so a page list of many pages failed and each of its pages fell back to its own processes. A
  page list now writes each run of consecutive pages as a range, 1-915, and a batch ends where its
  list would pass the limit.
- **The one-page double cost.** A batch of one page ran in a shared process, and a page that
  failed there ran again alone, paying gs twice (compacted-syntax.pdf). A batch of one page is now
  its page's own process.
- **The error line.** On the five PDFs gs can't open (encryption-openpassword.pdf,
  simple-open-password.pdf, and unicode-password-corrigendum5.pdf in `edge/encrypted/`;
  header-no-version-info.pdf and no-document-catalog.pdf in `edge/malformed/`), the error named
  the run "survey" in place of "page count" or "page sizes", and quoted gs's output with an extra
  `false` line, the portfolio flag. The error names the count or the sizes again, and the count
  run prints the flag after the count, and only when the count is above zero. gs prints a count of
  0 for a PDF it can't open, so the output quoted is what the released run printed.

### gs's temporary files

gs keeps temporary files in its `TMPDIR`, which the exec runner sets to a run's working
directory, the PDF's directory: `gs_` and six characters for a render's band list, which a render
creates and removes page by page, and `gpdf-collection-` and six characters for each of a
portfolio's embedded documents, which any run that opens a portfolio writes, the count run among
them. gs removes them as it exits. A gs killed first, by SIGTERM or SIGKILL, leaves them; the tool
package sends both when a run's context ends, as when the consumer stops the stream, and when its
bound passes.

On scratch copies of the 68 PDFs, a run to the end of the stream left nothing beside the PDF on
either path. Stopping the stream after 1, 2, 3, 5, or 9 pages, four times each at concurrency 4,
left an empty `gs_` file in 5 of 40 shared runs over stella-maberly-1896-ia-scan.pdf and
dodm-5200-01.pdf, none of 20 over the CFR volume, and none of 60 per-page runs over the three: a
shared process is ended mid-run, between its pages, where a page's own process has usually
finished. A per-page render of digitally-signed-3d-portfolio.pdf killed after one second, by
SIGTERM or SIGKILL, left five `gpdf-collection-` files. The adoption run's leftovers, `gs_` files
in several directories and `gpdf-collection-` files beside the portfolio, came from runs ended
early, since a run to the end leaves none.

The count run and the shared processes now work in a scratch directory of the stream's own
through the exec runner, their `TMPDIR`, which is removed once every page's work has ended. The
same 40 stopped runs, and 20 over the portfolio, then left nothing beside the PDF. A page's own
processes still work in the PDF's directory, as the released per-page path's do: `Render`,
`Text`, and `Count`, and the fallback for a shared process that fails. One ended early can leave
gs's files beside the input, as the released path can. Changing `TMPDIR` for every tool's runs
belongs to the tool package and is on the backlog.

Since v0.4.1, every exec-runner run's `TMPDIR` is a directory of the run's own under the host's
`TMPDIR`, removed once gs is reaped, so a page's own process ended early leaves nothing beside the
PDF either ([`docs/api.md`](api.md), "Running the tools").

### Recipe

The harness is a scratch Go program, never committed, over `document`'s exported API. It builds
the gs Tool with `tool.NewExec(ctx, document.GhostscriptSpec())`, its Timeout set to 10 minutes,
then `document.NewGhostscript` and `document.NewPreparer`, and ranges over `Images` with
`ImagesRequest{Dir, DPI: 150, Text: true, MaxPixels: 64_000_000, Concurrency: c}`, setting
`PagesPerProcess` when the build has the field. It prints the stream's wall time, its page count,
and its error count, and, when asked, writes a line a page sorted by page number: the number, the
DPI, the SHA-256 of the PNG file and of the text, the file's name, and the error. It empties its
output directory before and after each run.

1. Build the harness twice: once with its go.mod's `replace` naming a worktree of origin/main
   (`git worktree add --detach <dir> origin/main`), the per-page build, and once naming the
   checkout, the shared build. Remove the worktree once the runs are done.
2. Copy the PDFs to a scratch directory and run there, never in `~/docs`: gs writes temporary
   files in a run's working directory, the PDF's.
3. The sweep: the shared build on the CFR volume at concurrency 1, each `PagesPerProcess` in
   turn, three times.
4. Adoption: for each PDF, at concurrency 1 and 4, three times, the per-page build then the shared
   build, each run's page lines written. Compare each PDF's medians and spreads.
5. Identity: compare every run's page lines with the per-page build's at concurrency 1, byte for
   byte; an error quotes the PDF's path, so both builds read the same copy.
6. Follow-ups: ten runs a way at concurrency 4, interleaved, and the median, mean, SD, range, and
   Welch's t of each way's times.
7. Temporary files: after each run, list the files beside the PDF that aren't PDFs, and delete
   them. Stop the stream after a few pages with a `break` to see what an early end leaves.
   `inotifywait -m -r -e create,delete` on the copies shows every file gs creates during a run.

## Tool bench reruns

Measured 2026-11-19, from 17:31 to 17:38 UTC, on the same laptop as the spikes' runs: Intel Core
Ultra 9 185H, 22 logical CPUs, 32 GB, Linux 7.2.15-1-omarchy. Each spike's own bench ran against
the tool's published linux-x64 release, downloaded with `gh release download <tag> -R
standards-lab/docproc` and checked against the release's `SHA256SUMS`:

| Release | Archive SHA-256 | Binary SHA-256 | `--version` |
| --- | --- | --- | --- |
| tools/ooxml/v0.1.1 | `4ea3de23330f8c637d7ee0dd2f817e6b0e9aac03d8a14fc0a4a34896b5d6de33` | `e2a0fe997d34bbbf5eeb79e40e2969bf555771ec48d5c653d556de4b06133671` | 0.1.1, schema 2 |
| tools/mail/v0.1.2 | `1846c2016932dda71fab9525505c5af04a11c20f3a63c13b20bf5d03d671b9e1` | `2fbf7e2dab0012c8f9a16fc4e0cf9ac7bd360bd67554f27f559e4cc03cf52dbd` | 0.1.2, schema 2 |
| tools/onenote/v0.1.0 | `28f82553162fbdd211620734566e65686eac3943c43fcfacd6e8952a258ead1a` | `a6b908801601270115c08bba344b3cf931ce05f580ad3fb3d826e5d363d88090` | 0.1.0, schema 2 |

Each bench ran from a detached `git worktree` of its spike checkout in `~/experiments`, at the
checkout's HEAD (office b9bb1a2, mail 9cbbdd3, onenote 866ffc6), with its run counts unchanged:
office 5 runs a file, mail and onenote 3 runs of each large input and 1 of each committed store
(mail) or 3 of every input (onenote). The large inputs were the spikes' own, read in place through
symlinks: mail's scale store (`.state/scale`, built 2026-11-05 for run v0.3.0) and plaso's store,
and onenote's three large sections, each checked against its pinned SHA-256. Outputs went to a
tmpfs scratch directory, where the spikes wrote to btrfs. The WER measurement was scheduled on the
host at the same time; no ffmpeg process was running at the start or the end of the mail run, and
the load average was between 0.4 and 2.8 throughout.

A spike figure is the one its findings page records in `docs/findings/`: office's run `office`
(2026-10-31, office 0.1.0+6ed0d0e), mail's run `v0.3.0` (2026-11-05, 0.3.0, the run that repeats
every table), and onenote's run `v0.1.0` (2026-11-04). Each difference is judged equal, better,
worse, or not comparable. Times within a run's own spread, or within the 12% the mail finding
records between two days' runs on this laptop, are equal.

### docproc-ooxml: the baseline

The bench is `mise run baseline` (`scripts/baseline.sh`, `bench/SpikeAiPipeline.Office.Baseline`):
each of the 72 pool files (42 generated, 30 real-world) run 5 times through `bench/rusage.c`, the
first run's Markdown scored against the file's ground truth.

Adaptations, in `bench/SpikeAiPipeline.Office.Baseline`:

- `Program.cs`: `Arguments(file)` is `[file]`, where it was `[file, "--format", "markdown"]`. A
  new local function, `MarkdownOf(stdout)`, parses each stdout line as JSON and joins the lines'
  `markdown` fields in order with nothing between them, the whole rendering by the contract; the
  scored text and `markdownBytes` are that join. The output identity (`sameOutputEveryRun`) still
  hashes the whole stdout.
- `Footprint.cs`: `Office()` returns the binary's size with `packages` null when the spike's
  `obj/project.assets.json` is absent; the release ships `THIRD-PARTY-NOTICES.txt` instead.

The bench was built and run without the script's publish step:
`mise exec -- dotnet build bench/SpikeAiPipeline.Office.Baseline -c Release -warnaserror -o
.state/baseline/bench`, `cc -O2 -Wall -Wextra -Werror -o .state/baseline/rusage bench/rusage.c`,
then `.state/baseline/bench/SpikeAiPipeline.Office.Baseline --root . --office
<release>/docproc-ooxml --rusage .state/baseline/rusage --runs 5 --timeout 60 --out
<scratch>/results.json`.

Cut fields: none scored. The bench reads the Markdown alone, and schema "2" moved it into the
JSON lines whole. The structured fields schema "2" cut (the block tree, the chart fields, the
picture `alt`) were never scored.

| Kind | Spike, whole pool | Release, whole pool | Spike, real-world | Release, real-world |
| --- | --- | --- | --- | --- |
| Heading | 62 / 62 | 62 / 62 | 18 / 18 | 18 / 18 |
| Paragraph | 179 / 179 | 179 / 179 | 89 / 89 | 89 / 89 |
| List item | 167 / 167 | 167 / 167 | 91 / 91 | 91 / 91 |
| Table cell | 825 / 825 | 825 / 825 | 577 / 577 | 577 / 577 |
| Sheet name | 39 / 39 | 39 / 39 | 17 / 17 | 17 / 17 |
| Slide order | 38 / 38 | 38 / 38 | 18 / 18 | 18 / 18 |
| Slide notes | 14 / 14 | 14 / 14 | 4 / 4 | 4 / 4 |
| Chart value, region rule | 331 / 331 | 331 / 331 | 294 / 294 | 294 / 294 |
| Chart value, table-only rule | 331 / 331 | 331 / 331 | 294 / 294 | 294 / 294 |
| Media reference | 50 / 50 | 50 / 50 | 18 / 18 | 18 / 18 |
| Header or footer | 32 / 32 | 32 / 32 | 11 / 11 | 11 / 11 |
| Footnote or endnote | 13 / 13 | 13 / 13 | 3 / 3 | 3 / 3 |
| Comment | 18 / 18 | 18 / 18 | 4 / 4 | 4 / 4 |

| Measure | Spike | Release | Verdict |
| --- | --- | --- | --- |
| Files exiting 0 | 72 of 72 | 72 of 72 | equal |
| Same output on every run | 72 of 72 | 72 of 72 | equal |
| Median file, median of 5 | 16.37 ms | 15.39 ms | equal, within the 3 ms median spread |
| Whole pool, sum of medians | 1,306.1 ms | 1,200.0 ms | equal |
| Fastest and slowest file's median | 12.08, 29.98 ms | 10.40, 26.23 ms | equal |
| First invocation | 24.76 ms | 20.93 ms | equal |
| Peak memory of the median file | 35.29 MiB | 33.94 MiB | better, slightly |
| Smallest and largest peak | 30.47, 44.25 MiB | 28.89, 42.93 MiB | better, slightly; the largest is sections.libreoffice.docx in both |
| Binary | 30,197,712 B | 29,363,352 B | equal |
| Markdown, all 72 files | 46,418 B | 47,484 B | not comparable: the release's Markdown adds text |

The Markdown of 8 files differs from the spike's: a hyperlink's text is written as a Markdown
link (delins.docx, shapes.pptx, word-with-attachments.docx, unit-test-formatting.docx among them),
and an xlsx cell's `->` loses the spike's backslash. The scoring reads a link as its text, so no
score moves.

### docproc-mail: the findings

The bench is `mise run findings` (`scripts/findings.sh`, `bench/findings.rs`): the fields the
tool recovers per kind against the fixtures' truth, in nine tables, and its time and peak memory
on every committed .pst, plaso's 59 MB store, and the 2.1 GB scale store as a .pst and as a
2.7 GB mboxrd, each without and with its media directory, and the scale store resumed with
`--after`.

Adaptations, in `bench/findings.rs`:

- `time_store`: the media flag passed is `--media <dir>`, where it was `--attachments <dir>`.
- `Args` and `run`: `--glibc-bin` is optional, and without it the three glibc rows are not run.
  The release has no glibc build.
- `score_generated`: the `body (plain text)` and `body (HTML)` kinds, read from `body.text` and
  `body.html`, are removed.
- `tika_msg` and `poi_msg`: a truth pointer into `/body` is skipped (a new function, `cut`), in
  the fields and in the Java hashes.
- `time_store`: once a row's runs are counted, its media directory is removed, so the large
  stores' 1.9 GB each don't accumulate on the tmpfs.

The scorer was built with `CARGO_TARGET_DIR=<scratch>/target-mail mise exec -- cargo build
--release --locked --example findings`, and run from the worktree as the script runs it, with
`--bin <release>/docproc-mail --rusage .state/findings/rusage --plaso <plaso's store>
--plaso-commit f5bf35fcc102585eed58b930392da56a318690f6 --scale .state/scale --state
.state/findings --out <scratch>/results.json --commit <describe> --runs 3`, no `--glibc-bin`.

Cut fields left unscored: `body.text` and `body.html` (the generated tables, the MIME group's
.eml, and POI's two body pointers and their two Java hashes); `body.rtf` and `body.derived.html`,
which no truth names; and folder `display_name`, which no truth names. The two mbox tables
compare each message with the tool's read of its source, field by field, so their `body derived`,
`body text`, and `body html` kinds become one `markdown` kind on both sides.

| Table | Spike: expected, recovered, gap, mismatched | Release, cut kinds removed | Verdict |
| --- | --- | --- | --- |
| Generated .pst | 94, 80, 14, 0 | 84, 70, 14, 0 | equal: the 10 removed are the body kinds the spike recovered |
| Generated .msg | 86, 67, 19, 0 | 76, 57, 19, 0 | equal |
| Generated .eml | 86, 86, 0, 0 | 76, 76, 0, 0 | equal |
| MIME .eml | 15, 15, 0, 0 | 14, 14, 0, 0 | equal |
| nine.mboxrd against its sources | 116, 116, 0, 0 | 105, 105, 0, 0 | equal |
| mbox.rlug against its splits | 59, 59, 0, 0 | 54, 54, 0, 0 | equal |
| Tika's stores, threading | 47, 47, 0, 0; 8 of 8 absences | 47, 47, 0, 0; 8 of 8 absences | equal |
| Tika's .msg | 22, 21, 0, 1 | 22, 21, 0, 1 | equal; the miss is better read |
| POI's .msg | 31, 31, 0, 0 | 27, 27, 0, 0 | equal |

The generator gaps are the same 33, kind for kind. Tika's one mismatch is still
testMSG_chinese.msg's `/to/0/address`, an X.500 DN where Tika asserts the SMTP address; the
release writes that address, `tests.chang@fengttt.com`, in the recipient's new `smtp_address`,
which the truth's pointer doesn't read.

Time and peak memory, the median of 3 runs and the most of 3:

| Store | Media | Spike | Release | Verdict |
| --- | --- | --- | --- | --- |
| plaso's, 540 messages | no | 1.609 s, 41.7 MiB | 3.338 s, 33.1 MiB | time worse, 2.07 times; memory better |
| plaso's | yes | 1.607 s, 41.7 MiB | 3.364 s, 33.0 MiB | time worse; memory better |
| scale.pst, 50,000 messages | no | 14.322 s, 65.1 MiB | 14.244 s, 50.2 MiB | time equal; memory better |
| scale.pst | yes | 15.783 s, 66.6 MiB | 15.751 s, 52.6 MiB | time equal; memory better |
| scale.mboxrd | no | 6.727 s, 41.0 MiB | 8.516 s, 43.3 MiB | time worse, 1.27 times; memory equal |
| scale.mboxrd | yes | 7.408 s, 42.1 MiB | 8.418 s, 44.7 MiB | time worse, 1.14 times; memory equal |
| scale.pst after the 25,000th | no | 6.848 s, 58.6 MiB | 7.229 s, 46.7 MiB | time equal; memory better |
| scale.pst after the 25,000th | yes | 8.405 s, 59.5 MiB | 7.562 s, 47.9 MiB | time equal; memory better |
| scale.pst after the 50,000th | no | 0.105 s, 15.8 MiB | 0.097 s, 16.5 MiB | equal |
| scale.mboxrd after the 25,000th | no | 4.226 s, 41.4 MiB | 5.043 s, 42.7 MiB | time worse, 1.19 times; memory equal |
| scale.mboxrd after the 25,000th | yes | 4.552 s, 41.5 MiB | 5.115 s, 42.3 MiB | time worse, 1.12 times; memory equal |
| scale.mboxrd after the 50,000th | no | 1.925 s, 24.0 MiB | 1.833 s, 24.8 MiB | equal |
| Committed .pst, 6 stores | both | 2 to 8 ms, 2.1 to 3.0 MiB | 2 to 8 ms, 2.9 to 3.9 MiB | time equal; memory worse by about 1 MiB, the process's start |
| glibc build, 3 stores | no | 0.733, 7.004, 4.021 s | not run | not comparable: no glibc release |

What else the run shows, against the spike's:

- The slower rows are CPU time. plaso's store took 3.02 to 3.07 s of user time a run against
  1.31 to 1.36 s, and scale.mboxrd 5.48 to 5.54 s against 3.90 to 4.00 s; system time barely
  moved. The run doesn't profile it; [`mail-performance.md`](mail-performance.md) does, stage
  by stage. Schema "2", as this run read it, made every message's Markdown by the content rule, on
  htmd 0.6 with its `bare` pass; docproc-mail 0.1.4 no longer runs the rule.
- Every stdout was byte-identical across its runs and with and without `--media`. Every resumed
  stdout was the whole read's tail, byte for byte. `--media` wrote the same files as
  `--attachments` did: 21,202 files of 1,877,202,474 B from each scale store, 10,489 and 10,584
  after the middle message, and 5 files of 14,533,959 B from plaso's store.
- stdout is smaller, the bodies cut: scale.pst's 77,903,482 B against 141,220,403 B, and
  scale.mboxrd's 100,620,613 B against 164,389,002 B.
- Warnings rose from 3 to 1,530 on plaso's store and from 1 to 8 on Tika's testPST.pst. Each new
  one is an `http:` or `https:` image source that names no attachment, kept as written, as the
  README states for a source that names nothing; plaso's 3 skipped contacts keep theirs.

### docproc-onenote: the findings

The bench is `mise run findings` (`scripts/findings.sh`, `bench/findings.rs`): every field of the
committed sections and notebooks against `sections.json` and `notebooks.json`, the content fields
against `content.json`, and the time and peak memory of every committed input and three large
sections, each without and with `--media`, 3 runs each.

Adaptations, in `bench/findings.rs`:

- `NAME`, the prefix of the tool's stderr lines, is `docproc-onenote`.
- `score_sections`: a refusal's reason is read after `docproc-onenote: error: `, since schema
  "2"'s error line never leads with the input path; the expected notebook line's `schema` is `"2"`;
  the `Section display name` and `Containers: <kind>` kinds are removed.
- `score_notebooks`: the expected `Notebook schema` is `"2"`; the `Section display name` kind is
  removed.
- `score_content`: a pointer into `/content`, `/ink`, or `/ink_text` is skipped (a new function,
  `cut`). Every pointer `content.json` holds is into one of them.

The CLI call is unchanged: the spike already passed `--media <dir>`. The scorer was built as
mail's was, into `<scratch>/target-onenote`, and run from the worktree with `--bin
<release>/docproc-onenote --rusage .state/findings/rusage --large .state/findings/large --state
.state/findings --out <scratch>/results.json --commit <describe> --runs 3`.

Cut fields left unscored: a section's `display_name` (23), the page's `content` counted by
container kind (31), and the whole of `content.json` (129 fields and 6 absences), which points
into `content`, `ink`, and `ink_text` alone: containers, paragraph styles, runs, links, note tags,
list markers, tables, math, images, embedded files, ink, and recognised words. Their text is in
the Markdown, which the bench doesn't score.

| Table | Spike: expected, recovered; absences | Release, cut kinds removed | Verdict |
| --- | --- | --- | --- |
| Sections | 236, 236; 34 of 34 | 192, 191; 34 of 34 | equal, but the refusal reason, not comparable |
| Notebooks | 138, 138; 26 of 26 | 128, 126; 21 of 26 | equal, but 7 not comparable |
| Content | 129, 129; 6 of 6 | none left | not comparable: all cut |

Every kept kind is recovered as the spike's was, but these, each changed by a settled decision:

- Encryption.one is refused, exit 3 in both, with the reason `encrypted section (…); remove its
  password in OneNote first`, where the expectation holds the parser's `Unexpected end of file`.
  Inside interop/SUT's notebook its `skipped` line carries that reason and `encrypted: true`, and
  its warning that reason. Layers' docproc-onenote section settles this.
- Four notebooks that read no section (AlternativePackaging, NoSection, interop's Resources Open
  Notebook, and onenote-rs's object-revision) warn `/: no section or section group is read`,
  where the expectations hold no warning. Layers' docproc-onenote section and the tool's
  CHANGELOG settle this.

| Input | Media | Spike, median wall and median peak | Release | Verdict |
| --- | --- | --- | --- | --- |
| LargeData.one, 11 MB | no | 1.296 s, 19.5 MiB | 0.953 s, 8.6 MiB | better |
| LargeData.one | yes | 1.307 s, 19.4 MiB | 1.183 s, 10.3 MiB | better |
| Large Desktop.one, 113 MB | no | 0.253 s, 135.0 MiB | 0.161 s, 8.8 MiB | better |
| Large Desktop.one | yes | 0.267 s, 134.8 MiB | 0.197 s, 8.6 MiB | better |
| Large OneDrive.one, 161 MB | no | 0.240 s, 134.9 MiB | 0.161 s, 6.9 MiB | better |
| Large OneDrive.one | yes | 0.253 s, 135.1 MiB | 0.198 s, 6.5 MiB | better |
| Committed inputs, 23, both ways | both | each under 8.0 ms, 137 ms summed; at most 4.4 MiB | each under 9.7 ms, 155 ms summed; at most 3.7 MiB | equal |

`--media` wrote the same files as the spike's from every input: 2 of 8,127,445 B from
LargeData.one, 11 of 112,889,106 B from each large twin, and 17 of 1,642,020 B over the
committed inputs. Every stdout was byte-identical across its runs. The committed inputs' times
are mostly the process's start, and the 18 ms summed over 46 rows is within the run's noise.

### Verdicts and findings

| Tool | Fields | Time | Memory |
| --- | --- | --- | --- |
| docproc-ooxml | equal: every kind 1.000, as the spike's | equal | better, slightly |
| docproc-mail | equal on every kept kind; Tika's SMTP address now written, in `smtp_address` | equal on the scale .pst; worse on plaso's store (2.07 times) and the mboxrd (1.12 to 1.27 times) | better on plaso's and the scale .pst; equal on the mboxrd; about 1 MiB more on the small stores |
| docproc-onenote | equal on every kept kind; the refusal and empty-notebook warnings changed by settled decisions | better on the large sections; equal on the committed inputs | better: the large twins 135 MiB to under 9 MiB |

No finding. No shipped tool falls short of its README, its CHANGELOG, or a settled decision on
anything the benches measure. docproc-mail's slower rows are not a regression in that sense: its
README states no time of the tool's own, and the figures under "Limits" are cited as the spike's.
The slowdown is CPU time spent making the Markdown, which schema "2" adds to every message, and
the run doesn't attribute it further. docproc-mail's memory stays within the README's bound, the
largest attachment's small multiple: 52.6 MiB at most on the scale store, whose largest is
16 MiB.

Not run: the three glibc rows of the mail bench, since no glibc build is released, and the
onenote content table, all of whose fields are cut.

## The ~/docs run

Measured 2026-11-19, from 19:11:55 to 19:14:27 UTC, on the same laptop: Intel Core Ultra 9 185H,
22 logical CPUs, Linux 7.2.15-1-omarchy, the run's output on btrfs. This run is the release
candidate's result: docproc at `96a9cdbe35d179e636e27ff7f85844489f48f8cf`, the HEAD of
`acceptance`, whose manifest reads the command as `v0.1.1-0.20261119190539-96a9cdbe35d1+dirty`.
The tree's one change is the uncommitted goal record, no code. It carries three fixes the first
measurement led to: gs's render runs send their messages to stderr (831f865), the bytes cap bounds
what a stage writes (c61f750), and the integration tier runs no tool in testdata (78380e3).

The three tools were built from the checkout with `mise -C tools/<tool> run publish` and put first
on PATH; cmd/docproc was built with `GOWORK=off go build` in `cmd/docproc`, without `-race`. Every
`DOCPROC_` variable was unset, mise's DSN and S3 ones among them, so every bound is its starting
value, and the workers are the default, 22. The command was `docproc run ~/docs --out <dir>`, then
`docproc report <dir>`.

The corpus is `~/docs`: 592 files of 1,740,329,401 bytes, MANIFEST.json dated 2026-11-06. Its
listing was taken before and after each run here, the two ~/docs runs, the soffice bench, and the
bombs. Every file's path, size, and modification time is unchanged. The five directories whose
own modification times changed are those where a PDF's page processes worked:
`pdf/edge/malformed`, `pdf/edge/pdfa`, `pdf/edge/signed`, `pdf/edge/tagged`, and
`pdf/scanned/olmocr-bench`. No `gs_*` or `gpdf-collection-*` file was left, and nothing was
removed.

| Tool | Version | From |
| --- | --- | --- |
| gs | 10.08.0 | /usr/bin/gs |
| qpdf | 12.4.2 | /usr/bin/qpdf |
| pdfdetach | 26.10.0 | /usr/bin/pdfdetach |
| magick | 7.1.2-34 Q16-HDRI | /usr/bin/magick |
| soffice | LibreOffice 26.8.2.2 | `gotenberg/gotenberg:9.1.0-libreoffice@sha256:4ff340bb7b0753e7d037ceb75a08b09c3a9aebd0e6484df9516ec29ec31074ff`, in its container |
| docproc-ooxml | 0.1.1 | tools/ooxml/.state/publish |
| docproc-mail | 0.1.2 | tools/mail/.state/publish |
| docproc-onenote | 0.1.0 | tools/onenote/.state/publish |
| 7zz | 26.06 | mise, `github:ip7z/7zip` |
| ffmpeg, ffprobe | n9.0.2 | /usr/bin |

The run exited 0 with nothing on stderr, in **152.7 s** at 22 workers. The stage table wires all
ten routes. The counts, beside the spike's run and the first measurement's:

| Outcome | Spike | First measurement | This run |
| --- | --- | --- | --- |
| prepared | 515 | 507 | 508 |
| seen | 17 | 17 | 17 |
| no processor yet | 51 | 51 | 51 |
| encrypted | 5 | 6 | 6 |
| failed | 4 | 2 | 1 |
| read elsewhere | not an outcome | 9 | 9 |
| total | 592 | 592 | 592 |
| Wall time | 1,614.67 s (26 min 55 s) | 142.7 s | 152.7 s |

By route: document 61 prepared, 2 seen, 3 encrypted, 1 failed; office 104 prepared, 2 seen, 10 no
processor yet, 1 encrypted; vision-converted 20; vision 9 and 1 seen; nested 71 and 1 seen;
onenote 16 prepared, 9 read elsewhere, 1 encrypted; archive 8 and 1 encrypted; audio 41; video 9;
text 169 and 11 seen; unrouted 41 no processor yet. The record holds 971 part events, 379 of them
below an input, 11,188 output lines, and 29 warnings. Its "failed parts 138" is the 4 failures
in "Failures by class" and 134 seen replays of one of them, LargeData.one's docx.

Against the spike, cfr-2024-title32-vol1.pdf and compacted-syntax.pdf now prepare, the 9
`.onetoc2` tables of contents are read elsewhere, and Encryption.one is refused as an encrypted
section. Against the first measurement, one input moved:

- **compacted-syntax.pdf** (`pdf/edge/malformed/`) is prepared, with one image and one text, where
  it failed with "no PNG on stdout". gs renders its page with "Page drawing error occurred", and
  that message now goes to stderr, so stdout holds the PNG alone (831f865).
- **The bytes cap** changes no outcome here: no input comes near 32 GiB, the largest being
  295,621,088 bytes.
- **The wall time** is 10.0 s longer, 152.7 s against 142.7 s; this measurement's wrapped run took
  154.6 s, with the same counts. The slowest inputs each took 4 to 12 s longer: the large OneNote
  sections 145.5 s and 145.3 s, the CFR volume 137.0 s, and the Stella Maberly scan 116.2 s. The
  run doesn't attribute the difference, and no outcome depends on it.

**The CFR volume**, `pdf/large/cfr-2024-title32-vol1.pdf`, 915 pages: prepared in 137.0 s with
1,824 outputs, 915 page images, 908 texts, and 1 forms record holding its one signed signature
field. No warning.

**The audiobook**, `audio/long/librivox-stella-maberly.m4b`, 126,864,276 bytes: prepared in
37.7 s with 607 outputs, 606 audio windows cut at silence and 1 record. The spike took 1,613.87 s
for 646 outputs.

`docproc report` exited 0 and wrote the report to `<out>/runs/<run id>/report/index.html`, with
one page for each entry beside it, named by a 64-hex-digit hash; this run's id is
`01a1f538-fc4f-7ecd-8573-17a54f77dada`.

## Failures by class

Each failure in the run's record, classed by what stopped it, with each part that failed counted
once:

| Class | Parts failed |
| --- | --- |
| The tool's own exit | 1, with 134 seen replays |
| The tool's own signal | 0 |
| The tool's time bound | 0 |
| Output that doesn't parse | 1 |
| The tool's in-band refusal of one item, a `skipped` line | 2 |
| The 2 GiB memory bound | 0 |
| The run's own classes, retried and unrecorded | 0 ("error inputs 0") |

"memory bound" appears in neither ~/docs run's record, so no part failed by the 2 GiB memory
bound.

| Input | Part | Tool | Reason | Verdict |
| --- | --- | --- | --- | --- |
| `pdf/edge/malformed/no-document-catalog.pdf` | `/` | gs, its page sizes | gs printed "Couldn't initialise file" and a count of 0 | no fix: a correct failure |
| `onenote/large/LargeData.one` | `/page 875519ac-…, media 1`, "testing - large.docx" | docproc-ooxml, exit 3 | refused: not a docx, xlsx, or pptx package | backlog |
| `email/pst/java-libpst/passworded.pst` | `/message 2097188`, in Contacts | docproc-mail | skipped: Missing Sub-Node Tree on message | backlog |
| `email/pst/java-libpst/dist-list.pst` | `/message 2097188`, in Contacts | docproc-mail | skipped: Missing Sub-Node Tree on message | backlog |

- **no-document-catalog.pdf: no fix, a correct failure.** The file has no document catalog, and
  gs can't open it. qpdf's form read (exit 2, no trailer dictionary) and pdfdetach's attachment
  list (exit 1, no catalog) are warnings on the same part.
- **LargeData.one's docx: backlog.** The bytes are an XPS package, `FixedDocumentSequence.fdseq`
  among its entries, named `.docx` and routed office by its extension, so docproc-ooxml refuses it
  as its README says. XPS has no processor, and routing by name against content is PI-10's ground.
  Since v0.4.1, route sends a ZIP holding `FixedDocumentSequence.fdseq`, whole or as its first
  piece, as this part stores it, to `Unrouted`, whose outcome is "no processor yet".
- **passworded.pst and dist-list.pst: backlog.** tools/mail/README.md's Limits documents it: an
  item outlook-pst 1.2.0 can't open, such as a contact stored without a sub-node tree, is skipped.

No failure is new. The other 24 warnings are informational: text decoded as windows-1252, mail
decoded lossily, embedded messages outlook-pst doesn't open, image sources that name no
attachment, and docproc-ooxml's "text boxes not read".

## Peak memory

The peak resident memory of each tool's calls, through a stand-in on PATH for every program
docproc runs on the host, which runs the real program with docproc's exact arguments, under its
2 GiB RLIMIT_AS, and logs the call's ru_maxrss. The run was a full ~/docs run, `docproc run`
again with every tool wrapped: 154.6 s, the same counts, 1,347 calls, none ended by a signal.
soffice runs in its container under docker's `--memory=2147483648`, a cgroup bound, so it was
measured apart: docproc's lockdown flags and soffice arguments, with a static rusage as the
container's entrypoint, on the five largest legacy office inputs, the largest .xls, and the
largest EMF and WMF.

| Tool | Calls | Peak RSS | The peak call's input | Longest call |
| --- | --- | --- | --- | --- |
| magick | 36 | 344.3 MiB | bbb-alpha-inverted.avif, 8,468 B | 1.97 s |
| soffice | 8, in its container | 224.2 MiB | lists.libreoffice.rtf, 30,836 B | 1.15 s |
| gs | 751 | 152.7 MiB | balloon-a1b-jp2k.pdf's page render, 698,427 B | 15.15 s |
| ffmpeg | 53 | 107.7 MiB | a video's frame extraction | 41.03 s |
| ffprobe | 53 | 70.0 MiB | librivox-stella-maberly.m4b, 126,864,276 B | 0.23 s |
| docproc-ooxml | 115 | 42.8 MiB | form-footnotes.docx, 27,824 B | 0.09 s |
| pdfdetach | 83 | 19.0 MiB | cfr-2024-title32-vol1.pdf, 7,415,782 B | 0.10 s |
| qpdf | 149 | 14.0 MiB | a PDF attachment's form read | 0.12 s |
| docproc-onenote | 27 | 10.6 MiB | LargeData.one, 11,288,928 B | 2.76 s |
| 7zz | 5 | 7.5 MiB | mixed-bundle.tar.zst, 44,020 B | 0.03 s |
| docproc-mail | 75 | 3.7 MiB | outlook.pst, 271,360 B | 3.52 s |

**The 2 GiB bound stands.** No part failed by it, and every peak is under it: the highest,
magick's 344.3 MiB, is 16.8% of 2 GiB, and no peak comes within 50% of the bound, 1 GiB. The bound
is on address space, which resident memory understates; the two runs' zero memory-bound failures
are the evidence for that side.

One gap: docproc-ooxml's arm64 floor is unmeasured, because this host is x86_64. On x64, about 1
run in 20 of the .NET tool aborts at a 256 MiB address-space bound, and all 20 pass at 512 MiB.

## Bombs

Both bombs follow PI-1's recipe (`git show 8b03243:context/evaluations/processing-inspect.md`),
built by the first measurement's script on disk:

- **2 TiB across 2,000 entries**, `many-entries.zip`, 185,802 bytes, SHA-256
  `aed45de8c2acb337d7cacd56c47d6e445fc83829e778aafc2415a5a8b80dde09`. Entries `0.bin` to
  `1999.bin` each declare 1,073,741,823 bytes, 1 GiB less one byte, expanded from 1,048,576
  compressed, and each one's data is an empty final Deflate block, so the declared compressed
  sizes overlap the file. Each entry alone passes the per-entry rule, under 1 GiB.
- **100 GiB in one entry at 999:1**, `one-entry.zip`, 107,417,762 bytes, SHA-256
  `f293a9c6e3ad26aa25a5655530c137b3cc021ed8aa9593a69ae41bf171598cc8`. Its one ZIP64 entry,
  `payload.bin`, is 102,400 copies of one full-flushed Deflate stream of a 1 MiB chunk, zeros with
  a random byte every 300,000, so the copies concatenate: 107,374,182,400 bytes expanded, 999.6:1,
  under the rule's 1,000:1.

Each went through `docproc run` at the starting bounds, with the tools wrapped, in its own run
directory on btrfs with zstd:3 compression. The apparent size of the run directory and the bytes
used on its filesystem were sampled every second; the run output was deleted afterwards and the
records kept.

| Bomb | Before the write-time cap (cb82087) | This run (96a9cdb) |
| --- | --- | --- |
| 2 TiB ZIP: outcome | quarantined, `archive-bomb`, unsafe | quarantined, `archive-bomb`, unsafe |
| 2 TiB ZIP: time | 0.70 s | 0.66 s |
| 2 TiB ZIP: disk | no entry written | no entry written |
| 999:1 entry: outcome | `limited`, "past the bytes cap, 34359738368", size 107,374,182,400 | `limited`, "past the bytes cap, 34359738368", no size or SHA-256 |
| 999:1 entry: time | 168.5 s | 31.4 s |
| 999:1 entry: peak apparent | 100.0 GiB | 30.9 GiB at the last sample, under the 31.9 GiB the cap leaves |
| 999:1 entry: peak physical | 3.71 GiB | 1.02 GiB |

The 2 TiB ZIP is quarantined at inspection, before any entry is written: "declared compressed
sizes sum to 2097152000 bytes, past the 185802-byte ZIP, and 1 more". The "and 1 more" is the
summed-entry bomb rule: `inspect/zip.go` flags both the declared compressed sum and the declared
expanded sum, and the reason quotes the first. The run exited 0.

The 999:1 entry is stopped while it is written. The archive part itself is prepared, with one
child; the child `/zip entry payload.bin` is recorded `limited`, with size 0 and no SHA-256, and
the run exited 0. The entry was written at about 1.13 GiB a second; the last sample before its
removal, at 28.3 s, read 30.92 GiB apparent, and the next, at 29.3 s, found the partial file
removed. The cap leaves the entry 32 GiB less the bytes already admitted, the ZIP's own
107,417,762, so the file stopped at no more than 34,252,320,606 bytes, 31.9 GiB. Its physical
size is 1.02 GiB only because btrfs compresses the near-zero payload; on an uncompressed
filesystem it is the apparent size. Before the cap bounded writes, the same entry was written
whole, 100 GiB, and hashed before dispatch refused it, in 168.5 s.

**No starting value moved.** A bound's starting value moves when a bomb uses up the run's scratch
or runs longer than the unit timeout, 10 minutes. Neither bomb does: the 2 TiB ZIP writes
nothing, and the 999:1 entry is stopped by `DOCPROC_MAX_BYTES` at 32 GiB in 31.4 s, with 1.7 TB
free on the run's filesystem. No tool runs for either, since ZIP is read in Go, so neither
`DOCPROC_UNIT_TIMEOUT` nor `DOCPROC_STREAM_IDLE` applies. `DOCPROC_MAX_BYTES` at 32 GiB,
`DOCPROC_BOMB_RATIO` at 1000, `DOCPROC_BOMB_SIZE` at 1 GiB, and `DOCPROC_ARCHIVE_ENTRIES` at
100,000 stand.

## Word error rate

Measured 2026-11-19 on the Framework row, `http://framework:8001/v1` direct, model `audio`:
google/gemma-4-E4B-it in BF16 on vLLM v0.34.0 ROCm, `max_model_len` 8192. Each window went
through infer v0.2.0 (`github.com/standards-lab/go-ai/tools/infer/cmd/infer@v0.2.0`),
`infer --model audio --retries 1 transcribe --mode chat`, with the row's own sampling and no
language named. The tracker windows came from docproc at e14389c, cut by `audio.Splitter.Windows`
with `audio.Request{Limit: 30 s}`, as cmd/docproc's audio stage cuts them. The silencedetect
windows came from the spike, `~/experiments/spike-ai-pipeline-processing` at v0.9.0, its
`Splitter.Plan` and `Windows` at their defaults.

The row's image sets `MIOPEN_FIND_MODE=FAST`; before it, every request with 10 s or more of audio
crashed vLLM's engine, and the first attempt at this measurement returned HTTP 500 on every audio
request.

### Excerpts and truths

Each excerpt is its source's first 10 minutes, its first audio stream, as 16 kHz mono 16-bit WAV:
`ffmpeg -nostdin -hide_banner -loglevel error -i <source> -map 0:a:0 -t 600 -ac 1 -ar 16000 -c:a pcm_s16le -y <excerpt>.wav`.
Both arms cut their windows from the same excerpt. Each truth is read by the spike's own readers,
every file checked against `~/docs/MANIFEST.json`, and each run is joined by the spike's
`audio.Join` at its defaults and scored by `textscore.NormalizedWordErrors`. Rescoring the spike's
recorded windowed transcripts reproduced its figures exactly: AMI 0.2967, 0.3003, and 0.2967;
LibriVox 0.0563, 0.0582, and 0.0575.

| Excerpt | Source | Truth | Truth words |
| --- | --- | --- | --- |
| AMI | `ami-es2002a-mix-headset.wav` | `ami-es2002a-truth/ES2002a.{A,B,C,D}.{words,segments}.xml`, every word whose middle is before 600 s | 1,129 |
| Earnings-21 | `earnings21-4330115.mp3` | `earnings21-4330115.truth.nlp`, the first 1,574 normalized words | 1,574 |
| Earnings-22 | `earnings22-4479944.mp3` | `earnings22-4479944.truth.nlp`, the first 1,588 normalized words | 1,588 |
| LibriVox | `librivox-stella-maberly.m4b` | `librivox-stella-maberly.truth-approx.txt`, cleaned by `cleanBookOCR`, the first 1,582 normalized words; approximate | 1,582 |

The three untimed cuts are the spike's recorded derivations, fixed, so both arms are scored
against the spike's exact truths.

### Results

Three runs of each arm, at most 4 requests in flight. All 570 requests completed on the first
attempt: no 500, no refused connection, no retry, and no window without a transcript. No window of
either arm was cut at its 30 s limit.

| Excerpt | Arm | Windows | Run 1 | Run 2 | Run 3 | Median | Spread |
| --- | --- | --- | --- | --- | --- | --- | --- |
| AMI | tracker | 23 | 0.3233 | 0.3206 | 0.3295 | 0.3233 | 0.0089 |
| AMI | silencedetect | 24 | 0.3463 | 0.3260 | 0.3268 | 0.3268 | 0.0204 |
| Earnings-21 | tracker | 24 | 0.0737 | 0.0731 | 0.0699 | 0.0731 | 0.0038 |
| Earnings-21 | silencedetect | 24 | 0.1201 | 0.1067 | 0.1061 | 0.1067 | 0.0140 |
| Earnings-22 | tracker | 24 | 0.1014 | 0.1045 | 0.1026 | 0.1026 | 0.0031 |
| Earnings-22 | silencedetect | 23 | 0.0938 | 0.1039 | 0.1020 | 0.1020 | 0.0101 |
| LibriVox | tracker | 24 | 0.0664 | 0.0721 | 0.0664 | 0.0664 | 0.0057 |
| LibriVox | silencedetect | 24 | 0.0512 | 0.0531 | 0.0588 | 0.0531 | 0.0076 |

An excerpt passes when its median tracker WER is no more than its median silencedetect WER plus
0.021.

| Excerpt | Pass line | Tracker median | Verdict |
| --- | --- | --- | --- |
| AMI | 0.3478 | 0.3233 | pass, 0.0035 under silencedetect |
| Earnings-21 | 0.1277 | 0.0731 | pass, 0.0337 under silencedetect |
| Earnings-22 | 0.1230 | 0.1026 | pass, 0.0006 over silencedetect |
| LibriVox | 0.0741 | 0.0664 | pass, 0.0133 over silencedetect |

**Every excerpt passes.** No miss goes to v1.ai.inference. LibriVox is nearest its line, 0.0077
under it.

### The spike's figures, for context only

The spike's excerpt WER, windowed against unsplit: AMI 0.298 and 0.258, Earnings-21 0.069 and
0.053, Earnings-22 0.101 and 0.101, LibriVox 0.057 and 0.061. That row was llama.cpp with Gemma 4
E4B in Q8_0, at temperature 0 with English named; this one is BF16 on vLLM with the row's own
sampling and no language named. The absolute figures aren't comparable, and the pass line compares
the two arms on this row only.

### Recipe

`S` is a scratch directory; `~/docs` holds the sources and truths.

```sh
GOBIN=$S/bin go install github.com/standards-lab/go-ai/tools/infer/cmd/infer@v0.2.0
git clone -q ~/experiments/spike-ai-pipeline-processing $S/spike && git -C $S/spike checkout -q v0.9.0

mkdir -p $S/excerpts
for p in "ami audio/long/ami-es2002a-mix-headset.wav" "earnings21 audio/long/earnings21-4330115.mp3" \
         "earnings22 audio/long/earnings22-4479944.mp3" "librivox audio/long/librivox-stella-maberly.m4b"; do
  set -- $p
  ffmpeg -nostdin -hide_banner -loglevel error -i ~/docs/$2 -map 0:a:0 -t 600 -ac 1 -ar 16000 -c:a pcm_s16le -y $S/excerpts/$1.wav
done
```

1. **Tracker windows.** A scratch module whose go.mod replaces `github.com/standards-lab/docproc`
   with this checkout runs `audio.NewSplitter` over `audio.FFmpegSpec` and `audio.FFprobeSpec`,
   then `Splitter.Windows` with `audio.Request{Limit: 30 * time.Second}`, and copies each window
   to `$S/windows/tracker/<excerpt>/NNN.wav` in plan order.
2. **Silencedetect windows.** A findings-tier test in the spike's `audio` package runs
   `Splitter.Plan` and `Windows` at their defaults into `$S/windows/silencedetect/<excerpt>/`.
3. **Transcribe.** For each run 1 to 3, arm, and excerpt, every window through
   `INFER_BASE_URL=http://framework:8001/v1 $S/bin/infer --model audio --retries 1 transcribe --mode chat <window.wav>`,
   at most 4 in flight (`xargs -P4`), each retried at most twice, into
   `$S/transcripts/<arm>/<excerpt>/run<N>/NNN.txt`.
4. **Score.** A findings-tier test in the spike's `audio` package reads each run's transcripts in
   plan order, a missing one joined as the empty string as `audiotest.Run` does, joins them with
   `audio.Join` at its defaults, and scores the join with `textscore.NormalizedWordErrors`
   against the truths above. The median is of the 3 runs' rates, and the spread their max less
   min.
