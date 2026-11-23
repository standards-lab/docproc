# acceptance

This page records what docproc delivers at the close of v1.ai.processing, a section for each
measurement. Two of the goal's tasks measured it, on the same laptop, an Intel Core Ultra 9 185H,
16 cores and 22 threads, with gs 10.08.0, and each section names the task that measured it and
when:

- **acceptance, 2026-11-19**, measured the builds of its own branch, which released v0.4.0 and
  cmd/docproc/v0.2.0, and the tools at docproc-ooxml 0.1.1, docproc-mail 0.1.2, and
  docproc-onenote 0.1.0. Its sections are the sweep and adoption of shared gs processes, the
  defects they found, and gs's temporary files ("PDF pages in shared gs processes"); the office
  and OneNote benches ("Tool bench reruns"); ["Peak memory"](#peak-memory);
  ["Bombs"](#bombs); and ["Word error rate"](#word-error-rate). Its ~/docs run, the peak-memory
  run, and the bombs ran on 96a9cdb, its release candidate.
- **final, 2026-11-23**, reran four measurements against the releases the hardening and
  mail-hardening tasks cut: ["Identity"](#identity), on a3d861a; the mail bench
  (["docproc-mail: the findings"](#docproc-mail-the-findings)), against tools/mail/v0.1.4's
  release archive; and ["The ~/docs run"](#the-docs-run) and
  ["Failures by class"](#failures-by-class), on `b48ee124f6c8`. The two commits hold the same
  code and differ only in docs and context. That code is the release candidate, tagged v0.4.2,
  `storage/v0.1.1`, `postgres/v0.1.1`, and `tools/onenote/v0.1.2`, then cmd/docproc/v0.2.2. The
  storage and postgres modules were not measured. Each rerun section keeps acceptance's figures
  beside its own.

**The kept run.** The `final` task's ~/docs run is kept at `~/runs/docproc/final` for
v1.ai.inference's acceptance to read; ["The ~/docs run"](#the-docs-run) gives its run id.

**What `final` didn't rerun.** Word error rate, the bombs, peak memory, and the office and
OneNote benches were outside the `final` task's scope, which reran the measurements the hardened
releases bear on. Their sections hold acceptance's figures for its own builds. The CHANGELOGs and
the code between those builds and the releases `final` measured show what has changed since:

- **Word error rate**: `audio`, which cut the tracker windows at e14389c, has changed since only
  in comments and its unexported names, and in `audio.FFmpegImage`, the container default, which
  the measurement didn't run. Nothing it measures changed.
- **Bombs**: `inspect`, `dispatch`, and the archive stage have changed since 96a9cdb only in
  comments and layout. Since v0.4.1, route's ZIP rule also looks for an XPS package's sequence
  part in the central directory, which the rule already read for a ZIP holding no
  `[Content_Types].xml`, as both bombs are. No tool runs for either bomb. Nothing they measure
  changed.
- **The office bench**: docproc-ooxml v0.1.2 changed only the refusal reason for an input that is
  missing, a directory, or unreadable, and the bench's 72 files each exit 0. Nothing it measures
  changed.
- **The OneNote bench**: docproc-onenote v0.1.1 changed only the refusal reason for an input that
  is missing or unreadable, and patch releases in its lock file. v0.1.2's CHANGELOG states its
  output unchanged and its goldens identical, while the shared envelope crate runs the content
  rule, which docproc-onenote runs, with about half its former work per step. The fields the
  bench scores are therefore stated unchanged; its time and peak-memory rows measured v0.1.0, and
  v0.1.2's are unmeasured.
- **Peak memory**: several of its rows measure programs that have changed since. docproc-mail is
  v0.1.4, which reads with less copying and no content rule (its peaks on the bench's stores are
  in "docproc-mail: the findings"); docproc-onenote is v0.1.2, as above; soffice runs in
  Gotenberg 9.9.3's image, with the same LibreOffice, 26.8.2.2; docproc-ooxml no longer runs on
  LargeData.one's XPS package; and gs renders a PDF portfolio's page as the portfolio's own
  (v0.4.1). Its figures are acceptance's, of 96a9cdb and those tools' earlier versions, and
  weren't measured again.

## PDF pages in shared gs processes

`document.Preparer.Images` runs a PDF's pages through shared gs processes: at concurrency C, lane
k takes pages k, k+C, k+2C, and so on, up to `ImagesRequest.PagesPerProcess` pages a process, with
one txtwrite process before each render process when the request reads text. The per-page path,
as released on origin/main (v0.3.0), runs two gs processes for each page, its text and its render.
Every measurement here runs the request cmd/docproc starts with: 150 DPI, `Text`, `MaxPixels`
64,000,000, and the gs Tool's Timeout at 10 minutes. Acceptance measured the sweep, adoption, and
gs's temporary files on 2026-11-19, against the shared path as its branch built it before v0.4.0;
the `final` task reran Identity alone.

### The sweep and the default

Acceptance's sweep timed `Images` on the 915-page CFR volume (cfr-2024-title32-vol1.pdf) at
concurrency 1, three runs at each `PagesPerProcess`, the counts interleaved within each run.

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

Acceptance's adoption run timed both paths over all 68 PDFs in `~/docs`, at concurrency 1 and 4,
three runs each way, the per-page and shared runs interleaved. A PDF is faster or slower beyond the
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
that is tagged v0.4.2 and then cmd/docproc/v0.2.2. The harness is the one [the Recipe](#recipe)
describes, built twice: once requiring docproc v0.3.0, the per-page path, and once replacing it with the checkout,
the shared path at the default `PagesPerProcess`, 8. Each ran once over a scratch copy of the 68
PDFs, the shared path at concurrency 1 and 4 and the per-page path at 1, with every `DOCPROC_`
variable unset and the host's tools as they are: gs 10.08.0 (Arch's ghostscript 10.08.0-1) and Go
1.27.4.

Over the 1,387 pages and five failed counts, the shared path's lines are identical at
concurrency 1 and 4. Every line (each page's number, DPI, PNG SHA-256, text SHA-256, file name,
and error) is identical to the per-page path's, apart from two PDFs, each changed by a release:

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

The table gives each run's stream wall time, summed over the 68 PDFs and for the CFR volume
alone, beside the adoption run's medians, summed the same way:

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
on all 68 PDFs, every page line at concurrency 1 and 4 was identical to the per-page path's at
concurrency 1. Six PDFs failed as on the per-page path: the five gs
can't open failed their count with the same error, word for word, and compacted-syntax.pdf's one
page failed its render with the same error.

### Defects the measurement found and fixed

Acceptance's measurement found these in its branch's shared path, which v0.4.0 released with them
fixed.

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

Acceptance measured these on 2026-11-19, when the exec runner set gs's `TMPDIR` to a run's working
directory, the PDF's directory. gs keeps temporary files in its `TMPDIR`: `gs_` and six characters
for a render's band list, which a render creates and removes page by page, and `gpdf-collection-`
and six characters for each of a portfolio's embedded documents, which any run that opens a
portfolio writes, the count run among them. gs removes them as it exits. A gs killed first, by
SIGTERM or SIGKILL, leaves them; the tool package sends both when a run's context ends, as when the
consumer stops the stream, and when its bound passes.

On scratch copies of the 68 PDFs, a run to the end of the stream left nothing beside the PDF on
either path. Stopping the stream after 1, 2, 3, 5, or 9 pages, four times each at concurrency 4,
left an empty `gs_` file in 5 of 40 shared runs over stella-maberly-1896-ia-scan.pdf and
dodm-5200-01.pdf, none of 20 over the CFR volume, and none of 60 per-page runs over the three: a
shared process is ended mid-run, between its pages, where a page's own process has usually
finished. A per-page render of digitally-signed-3d-portfolio.pdf killed after one second, by
SIGTERM or SIGKILL, left five `gpdf-collection-` files. The adoption run's leftovers, `gs_` files
in several directories and `gpdf-collection-` files beside the portfolio, came from runs ended
early, since a run to the end leaves none.

Acceptance's fix, released in v0.4.0, put the count run and the shared processes in a scratch
directory of the stream's own through the exec runner, their `TMPDIR`, which is removed once every
page's work has ended. The same 40 stopped runs, and 20 over the portfolio, then left nothing
beside the PDF. A page's own processes still worked in the PDF's directory, as the per-page path's
did: `Render`, `Text`, and `Count`, and the fallback for a shared process that fails. One ended
early could leave gs's files beside the input, and changing `TMPDIR` for every tool's runs was
left to the tool package.

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

1. Build the harness twice: the per-page build, on docproc v0.3.0, and the shared build, its
   go.mod's `replace` naming the checkout. Acceptance's per-page build named a worktree of
   origin/main, then v0.3.0, in its `replace` (`git worktree add --detach <dir> origin/main`),
   removed once the runs were done; the `final` task's required the released module, `go get
   github.com/standards-lab/docproc@v0.3.0`, with no `replace`.
2. Copy the PDFs to a scratch directory and run there, never in `~/docs`: the per-page build's gs
   writes temporary files in a run's working directory, the PDF's, as a page's own processes on
   the shared build did before v0.4.1.
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

Acceptance measured all three on 2026-11-19, from 17:31 to 17:38 UTC, on the same laptop as the
spikes' runs: Intel Core Ultra 9 185H, 22 logical CPUs, 32 GB, Linux 7.2.15-1-omarchy. Each spike's
own bench ran against the tool's published linux-x64 release, downloaded with `gh release download
<tag> -R standards-lab/docproc` and checked against the release's `SHA256SUMS`:

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
the load average was between 0.4 and 2.8 throughout. The `final` task reran the mail bench on
2026-11-23 against tools/mail/v0.1.4; "docproc-mail: the findings" records both runs.

A spike figure is the one its findings page records in `docs/findings/`: office's run `office`
(2026-10-31, office 0.1.0+6ed0d0e), mail's run `v0.3.0` (2026-11-05, 0.3.0, the run that repeats
every table), and onenote's run `v0.1.0` (2026-11-04). Each difference is judged equal, better,
worse, or not comparable. Times within a run's own spread, or within the 12% the mail finding
records between two days' runs on this laptop, are equal.

### docproc-ooxml: the baseline

Acceptance's run, against tools/ooxml/v0.1.1. The bench is `mise run baseline`
(`scripts/baseline.sh`, `bench/SpikeAiPipeline.Office.Baseline`):
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

| Kind | Spike, whole pool | v0.1.1, whole pool | Spike, real-world | v0.1.1, real-world |
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

| Measure | Spike | v0.1.1 | Verdict |
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

The Markdown of 8 files differed from the spike's: a hyperlink's text is written as a Markdown
link (delins.docx, shapes.pptx, word-with-attachments.docx, unit-test-formatting.docx among them),
and an xlsx cell's `->` loses the spike's backslash. The scoring reads a link as its text, so no
score moves.

### docproc-mail: the findings

The bench is `mise run findings` (`scripts/findings.sh`, `bench/findings.rs`): the fields the
tool recovers per kind against the fixtures' truth, in nine tables, and its time and peak memory
on every committed .pst, plaso's 59 MB store, and the 2.1 GB scale store as a .pst and as a
2.7 GB mboxrd, each without and with its media directory, and the scale store resumed with
`--after`.

Acceptance ran it against tools/mail/v0.1.2 on 2026-11-19. The `final` task reran it against
tools/mail/v0.1.4 on 2026-11-23, from 14:45 to 14:48 UTC, on the same laptop: Intel Core Ultra 9
185H, 22 logical CPUs, 32 GB, Linux 7.2.15-1-omarchy. The release's linux-x64 archive,
downloaded with `gh release download tools/mail/v0.1.4 -R standards-lab/docproc`, matched the
release's `SHA256SUMS`:

| Release | Archive SHA-256 | Binary SHA-256 | `--version` |
| --- | --- | --- | --- |
| tools/mail/v0.1.4 | `f89828a1b10923ffec5d5437dac0b82776928006ae020b2cc50ab7f9db94804d` | `ba7d06e5ee463d30682679a6f9fcb5ee3a95ba67f920b4230bdaf0001fcfea47` | 0.1.4, schema 2 |

Adaptations, in `bench/findings.rs`, the same for both runs:

- `time_store`: the media flag passed is `--media <dir>`, where it was `--attachments <dir>`.
- `Args` and `run`: `--glibc-bin` is optional, and without it the three glibc rows are not run.
  The release has no glibc build.
- `score_generated`: the `body (plain text)` and `body (HTML)` kinds, read from `body.text` and
  `body.html`, are removed.
- `tika_msg` and `poi_msg`: a truth pointer into `/body` is skipped (a new function, `cut`), in
  the fields and in the Java hashes.
- `time_store`: once a row's runs are counted, its media directory is removed, so the large
  stores' 1.9 GB each don't accumulate on the tmpfs.

v0.1.4 needed no further adaptation.

The scorer was built with `CARGO_TARGET_DIR=<scratch>/target-mail mise exec -- cargo build
--release --locked --example findings`, and run from the spike's tree as the script runs it, with
`--bin <release>/docproc-mail --rusage <state>/rusage --plaso <plaso's store>
--plaso-commit f5bf35fcc102585eed58b930392da56a318690f6 --scale <scale store> --state <state>
--out <scratch>/results.json --commit <describe> --runs 3`, no `--glibc-bin`. Acceptance's tree
was a detached `git worktree` of the spike checkout at 9cbbdd3, with its large inputs symlinked
into its `.state`; the `final` task's was a `git clone` of that checkout into a scratch directory,
detached at 9cbbdd3, with `--plaso` and `--scale` pointed at the checkout's own
`.state/findings/plaso` and `.state/scale`, read in place, and `--state` in the scratch
directory. plaso's store matched its pinned SHA-256, and scale.mboxrd's SHA-256,
`79f5ad34bd4962a1bd84974fd1b4e06bd06bbba7718e5db1321ea64dbb22fe1a`, is the one the spike's run
v0.3.0 records. Outputs went to tmpfs in both runs. The `final` task's ran with every `DOCPROC_`
variable unset. Other sessions' builds shared the host, and the load average was between 1.6 and
5.8; the first of the three runs of scale.pst and of scale.mboxrd, without media, were their
rows' slowest, 12.267 s and 5.124 s, and the median sets the figure.

Cut fields left unscored: `body.text` and `body.html` (the generated tables, the MIME group's
.eml, and POI's two body pointers and their two Java hashes); `body.rtf` and `body.derived.html`,
which no truth names; and folder `display_name`, which no truth names. The two mbox tables
compare each message with the tool's read of its source, field by field, so their `body derived`,
`body text`, and `body html` kinds become one `markdown` kind on both sides.

Each count is expected, recovered, gap, mismatched; the releases' are with the cut kinds removed.

| Table | Spike | v0.1.2, acceptance | v0.1.4, `final` | Against v0.1.2 | Against the spike |
| --- | --- | --- | --- | --- | --- |
| Generated .pst | 94, 80, 14, 0 | 84, 70, 14, 0 | 84, 70, 14, 0 | equal | equal: the 10 removed are the body kinds the spike recovered |
| Generated .msg | 86, 67, 19, 0 | 76, 57, 19, 0 | 76, 57, 19, 0 | equal | equal |
| Generated .eml | 86, 86, 0, 0 | 76, 76, 0, 0 | 76, 76, 0, 0 | equal | equal |
| MIME .eml | 15, 15, 0, 0 | 14, 14, 0, 0 | 14, 14, 0, 0 | equal | equal |
| nine.mboxrd against its sources | 116, 116, 0, 0 | 105, 105, 0, 0 | 105, 105, 0, 0 | equal | equal |
| mbox.rlug against its splits | 59, 59, 0, 0 | 54, 54, 0, 0 | 54, 54, 0, 0 | equal | equal |
| Tika's stores, threading | 47, 47, 0, 0; 8 of 8 absences | 47, 47, 0, 0; 8 of 8 absences | 47, 47, 0, 0; 8 of 8 absences | equal | equal |
| Tika's .msg | 22, 21, 0, 1 | 22, 21, 0, 1 | 22, 21, 0, 1 | equal | equal; the miss is better read |
| POI's .msg | 31, 31, 0, 0 | 27, 27, 0, 0 | 27, 27, 0, 0 | equal | equal |

The generator gaps are the same 33 in all three, kind for kind. Tika's one mismatch is still
testMSG_chinese.msg's `/to/0/address`, an X.500 DN where Tika asserts the SMTP address; both
releases write that address, `tests.chang@fengttt.com`, in the recipient's `smtp_address`, which
the truth's pointer doesn't read. v0.1.4's HTML-only Markdown moves no score: the generated and
MIME tables don't score the bodies, and the mbox tables hold the tool's read of each source to
the same rule.

Each row gives the median time of 3 runs and the highest peak memory of the 3. Times within a
row's own spread, or within the 12% the mail finding records between two days' runs, are equal;
so is a peak on plaso's or the scale store within about 3 MiB, as acceptance judged
scale.mboxrd's:

| Store | Media | Spike | v0.1.2, acceptance | v0.1.4, `final` | Against v0.1.2 | Against the spike |
| --- | --- | --- | --- | --- | --- | --- |
| plaso's, 540 messages | no | 1.609 s, 41.7 MiB | 3.338 s, 33.1 MiB | 1.507 s, 32.8 MiB | time better, 2.22 times faster; memory equal | time equal; memory better |
| plaso's | yes | 1.607 s, 41.7 MiB | 3.364 s, 33.0 MiB | 1.509 s, 32.8 MiB | time better; memory equal | time equal; memory better |
| scale.pst, 50,000 messages | no | 14.322 s, 65.1 MiB | 14.244 s, 50.2 MiB | 10.751 s, 50.3 MiB | time better, 0.75 times; memory equal | time better; memory better |
| scale.pst | yes | 15.783 s, 66.6 MiB | 15.751 s, 52.6 MiB | 11.379 s, 52.6 MiB | time better, 0.72 times; memory equal | time better; memory better |
| scale.mboxrd | no | 6.727 s, 41.0 MiB | 8.516 s, 43.3 MiB | 4.345 s, 40.7 MiB | time better, 1.96 times faster; memory equal | time better, 1.55 times faster; memory equal |
| scale.mboxrd | yes | 7.408 s, 42.1 MiB | 8.418 s, 44.7 MiB | 4.929 s, 42.3 MiB | time better; memory equal | time better; memory equal |
| scale.pst after the 25,000th | no | 6.848 s, 58.6 MiB | 7.229 s, 46.7 MiB | 5.430 s, 46.6 MiB | time better; memory equal | time better; memory better |
| scale.pst after the 25,000th | yes | 8.405 s, 59.5 MiB | 7.562 s, 47.9 MiB | 5.777 s, 47.8 MiB | time better; memory equal | time better; memory better |
| scale.pst after the 50,000th | no | 0.105 s, 15.8 MiB | 0.097 s, 16.5 MiB | 0.099 s, 16.0 MiB | equal | equal |
| scale.mboxrd after the 25,000th | no | 4.226 s, 41.4 MiB | 5.043 s, 42.7 MiB | 2.791 s, 40.8 MiB | time better; memory equal | time better; memory equal |
| scale.mboxrd after the 25,000th | yes | 4.552 s, 41.5 MiB | 5.115 s, 42.3 MiB | 3.086 s, 41.2 MiB | time better; memory equal | time better; memory equal |
| scale.mboxrd after the 50,000th | no | 1.925 s, 24.0 MiB | 1.833 s, 24.8 MiB | 1.316 s, 24.1 MiB | time better; memory equal | time better; memory equal |
| Committed .pst, 6 stores | both | 2 to 8 ms, 2.1 to 3.0 MiB | 2 to 8 ms, 2.9 to 3.9 MiB | 1.6 to 6.7 ms, 2.4 to 3.5 MiB | time equal; memory better by about 0.5 MiB | time equal; memory worse by about 0.4 MiB, the process's start |
| glibc build, 3 stores | no | 0.733, 7.004, 4.021 s | not run | not run | not comparable: no glibc release | not comparable |

What else the runs show:

- Acceptance found v0.1.2 slower than the spike on plaso's store, 2.07 times, and on
  scale.mboxrd, 1.12 to 1.27 times, and the difference was CPU time: plaso's store took 3.02 to
  3.07 s of user time a run against the spike's 1.31 to 1.36 s, and scale.mboxrd 5.48 to 5.54 s
  against 3.90 to 4.00 s. v0.1.2 made every message's Markdown by the content rule, on htmd 0.6
  with its `bare` pass. v0.1.4 runs no content rule and repeats and copies less
  ([`mail-performance.md`](mail-performance.md) records both changes): plaso's store took 1.25 to
  1.31 s of user time a run, and scale.mboxrd 2.70 to 2.81 s.
- Every stdout was byte-identical across its runs and with and without `--media`, in both
  releases' runs. Every resumed stdout was the whole read's tail, byte for byte. `--media` wrote
  the same files as `--attachments` did: 21,202 files of 1,877,202,474 B from each scale store,
  10,489 and 10,584 after the middle message, and 5 files of 14,533,959 B from plaso's store.
- stdout is smaller than the spike's, the bodies cut, and v0.1.4's is the size v0.1.2's was:
  scale.pst's 77,903,482 B against the spike's 141,220,403 B, and scale.mboxrd's 100,620,613 B
  against 164,389,002 B.
- Warnings were 1,530 on plaso's store and 8 on Tika's testPST.pst in both releases' runs, where
  the spike's were 3 and 1. Each new one is an `http:` or `https:` image source that names no
  attachment, kept as written, as the README states for a source that names nothing; plaso's 3
  skipped contacts keep theirs.

### docproc-onenote: the findings

Acceptance's run, against tools/onenote/v0.1.0. The bench is `mise run findings`
(`scripts/findings.sh`, `bench/findings.rs`): every field of the committed sections and notebooks
against `sections.json` and `notebooks.json`, the content fields against `content.json`, and the
time and peak memory of every committed input and three large sections, each without and with
`--media`, 3 runs each.

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

| Table | Spike: expected, recovered; absences | v0.1.0, cut kinds removed | Verdict |
| --- | --- | --- | --- |
| Sections | 236, 236; 34 of 34 | 192, 191; 34 of 34 | equal, but the refusal reason, not comparable |
| Notebooks | 138, 138; 26 of 26 | 128, 126; 21 of 26 | equal, but 7 not comparable |
| Content | 129, 129; 6 of 6 | none left | not comparable: all cut |

Every kept kind was recovered as the spike's was, but these, each changed by a settled decision:

- Encryption.one was refused, exit 3 in both, with the reason `encrypted section (…); remove its
  password in OneNote first`, where the expectation holds the parser's `Unexpected end of file`.
  Inside interop/SUT's notebook its `skipped` line carries that reason and `encrypted: true`, and
  its warning that reason. Layers' docproc-onenote section settles this.
- Four notebooks that read no section (AlternativePackaging, NoSection, interop's Resources Open
  Notebook, and onenote-rs's object-revision) warned `/: no section or section group is read`,
  where the expectations hold no warning. Layers' docproc-onenote section and the tool's
  CHANGELOG settle this.

| Input | Media | Spike, median wall and median peak | v0.1.0 | Verdict |
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
| docproc-ooxml v0.1.1, acceptance | equal: every kind 1.000, as the spike's | equal | better, slightly |
| docproc-mail v0.1.2, acceptance, and v0.1.4, `final` | equal on every kept kind; Tika's SMTP address now written, in `smtp_address` | v0.1.4: equal on plaso's store, better on the scale store, as a .pst and as an mboxrd; v0.1.2 was equal on the scale .pst and worse on plaso's store (2.07 times) and the mboxrd (1.12 to 1.27 times) | better on plaso's and the scale .pst; equal on the mboxrd; about 0.4 MiB more on the small stores, where v0.1.2 took about 1 MiB more |
| docproc-onenote v0.1.0, acceptance | equal on every kept kind; the refusal and empty-notebook warnings changed by settled decisions | better on the large sections; equal on the committed inputs | better: the large twins 135 MiB to under 9 MiB |

No finding. No shipped tool falls short of its README, its CHANGELOG, or a settled decision on
anything the benches measure. docproc-mail v0.1.2's slower rows were not a regression in that
sense: its README stated no time of the tool's own, and the figures under "Limits" were cited as
the spike's. The slowdown was CPU time spent making the Markdown, which schema "2" added to every
message. v0.1.4, which the `final` task measured, has no slower row against the spike or against
v0.1.2. docproc-mail's memory stays within the README's bound, the largest attachment's small
multiple: 52.6 MiB at most on the scale store in both releases, whose largest is 16 MiB.

Not run: the three glibc rows of the mail bench, since no glibc build is released, and the
onenote content table, all of whose fields are cut.

## The ~/docs run

Acceptance ran it on 2026-11-19, from 19:11:55 to 19:14:27 UTC, on docproc at
`96a9cdbe35d179e636e27ff7f85844489f48f8cf`, the HEAD of `acceptance`, whose manifest read the
command as `v0.1.1-0.20261119190539-96a9cdbe35d1+dirty`; that run directory no longer exists. The
`final` task ran it again on 2026-11-23, from 14:53:33 to 14:56:04 UTC, on the same laptop: Intel
Core Ultra 9 185H, 22 logical CPUs, 32 GB, Linux 7.2.15-1-omarchy, the run's output on btrfs. It
measured docproc at `b48ee124f6c8176a0d37833b45d5852479a6e2dc`, the release candidate that is
tagged v0.4.2 and then cmd/docproc/v0.2.2; the manifest reads the command as
`v0.2.2-0.20261123145216-b48ee124f6c8` and the library as `(devel)`, cmd/docproc reaching the
base module through its temporary replace.

**The run is kept** at `~/runs/docproc/final`, outside any repository, for v1.ai.inference's
acceptance to read. Its run id is `01a208e5-e581-7b95-b43a-0f91f8d3b40e`: the manifest and record
are at `~/runs/docproc/final/runs/01a208e5-e581-7b95-b43a-0f91f8d3b40e/`, and the report at
`report/index.html` beside them.

cmd/docproc was built with `GOWORK=off go build` in `cmd/docproc`, without `-race`, as
acceptance's was. Acceptance built the three tools from its checkout with `mise -C tools/<tool>
run publish`; this run took the two released tools from their releases and built
docproc-onenote by its release recipe:

- **docproc-ooxml and docproc-mail**: each release's linux-x64 archive, downloaded with
  `gh release download tools/<tool>/v<version> -R standards-lab/docproc`, matched the release's
  `SHA256SUMS`.
- **docproc-onenote**: `tools/pack.sh onenote 0.1.2` in the tool's mise environment, the step the
  release workflow runs on each runner, which builds with the tool's `publish` task and refuses a
  binary whose `--version` isn't 0.1.2.

| Tool | Archive SHA-256 | Binary SHA-256 | `--version` |
| --- | --- | --- | --- |
| docproc-ooxml | `c318000d8bacaf6a32786d439c67f9ccf56befed8afbf58fd7f91b729160be0d` | `f5853e86f0da277fc1ca6947a3ef19fed8e4bd011f6b3bb0858ab4d08c2abed2` | 0.1.2, schema 2 |
| docproc-mail | `f89828a1b10923ffec5d5437dac0b82776928006ae020b2cc50ab7f9db94804d` | `ba7d06e5ee463d30682679a6f9fcb5ee3a95ba67f920b4230bdaf0001fcfea47` | 0.1.4, schema 2 |
| docproc-onenote | `e9ff3d85823e5abd416c0fdcee2625c5ae63f6d6b99054ceee620ce5bd4fdb9d`, built | `bb044b1f2d1608eb157fa952f07373bc04282cd6f266006130a7ee813eccf9f8` | 0.1.2, schema 2 |

The three binaries and cmd/docproc were put first on PATH, as acceptance put its builds. Every
`DOCPROC_` variable was unset, mise's DSN and S3 ones among them, so every bound was its starting
value, and the workers were the default, 22. The command was `docproc run ~/docs --out
~/runs/docproc/final`, then `docproc report ~/runs/docproc/final`.

The corpus is `~/docs`: 592 files of 1,740,329,401 bytes, MANIFEST.json dated 2026-11-06. Its
listing, every file's and directory's path, size, and modification time, was taken before and
after the run and compared with `cmp`: identical, so nothing was added, removed, or modified. No
directory's own modification time changed either, where acceptance's run changed five, those
where a PDF's page processes worked: since v0.4.1 every exec-runner run works in a `TMPDIR` of its
own under the host's (["gs's temporary files"](#gss-temporary-files)).

| Tool | Acceptance (2026-11-19) | This run | From |
| --- | --- | --- | --- |
| gs | 10.08.0 | 10.08.0 | /usr/bin/gs |
| qpdf | 12.4.2 | 12.4.2 | /usr/bin/qpdf |
| pdfdetach | 26.10.0 | 26.10.0 | /usr/bin/pdfdetach |
| magick | 7.1.2-34 Q16-HDRI | 7.1.2-34 Q16-HDRI | /usr/bin/magick |
| soffice | LibreOffice 26.8.2.2, in `gotenberg/gotenberg:9.1.0-libreoffice@sha256:4ff340bb7b0753e7d037ceb75a08b09c3a9aebd0e6484df9516ec29ec31074ff` | LibreOffice 26.8.2.2 | `gotenberg/gotenberg:9.9.3-libreoffice@sha256:d7c238a1b1f5726c0e82e5838b2b5acbbead95a4f4a367c6ffa05c2dc20485f6`, in its container |
| docproc-ooxml | 0.1.1, tools/ooxml/.state/publish | 0.1.2 | the tools/ooxml/v0.1.2 release |
| docproc-mail | 0.1.2, tools/mail/.state/publish | 0.1.4 | the tools/mail/v0.1.4 release |
| docproc-onenote | 0.1.0, tools/onenote/.state/publish | 0.1.2 | `tools/pack.sh onenote 0.1.2` |
| 7zz | 26.06 | 26.06 | mise, `github:ip7z/7zip` |
| ffmpeg, ffprobe | n9.0.2 | n9.0.2 | /usr/bin |

The stage table runs soffice alone in a container; ffmpeg and ffprobe run on the host, so the new
default `audio.FFmpegImage`, `linuxserver/ffmpeg:9.0.2-cli-ls90`, ran in neither run.

The run exited 0 with nothing on stderr, in **150.9 s** at 22 workers, against acceptance's
152.7 s. The stage table wired all ten routes. The counts:

| Outcome | Acceptance (2026-11-19) | This run |
| --- | --- | --- |
| prepared | 508 | 507 |
| seen | 17 | 18 |
| no processor yet | 51 | 51 |
| encrypted | 6 | 6 |
| failed | 1 | 1 |
| read elsewhere | 9 | 9 |
| total | 592 | 592 |
| Wall time | 152.7 s | 150.9 s |

By route: document 61 prepared, 2 seen, 3 encrypted, 1 failed; office 104 prepared, 2 seen, 10 no
processor yet, 1 encrypted; vision-converted 20; vision 9 and 1 seen; nested 71 and 1 seen;
onenote 16 prepared, 9 read elsewhere, 1 encrypted; archive 7 prepared, 1 seen, 1 encrypted,
where acceptance's was 8 and 1 encrypted; audio 41; video 9; text 169 and 11 seen; unrouted 41 no
processor yet. The record holds 971 part events, 379 of them below an input, 11,188 output lines,
and 29 warnings, as acceptance's did. Its "failed parts 3" is the 3 failures in "Failures by
class", where acceptance's 138 counted LargeData.one's docx and its 134 seen replays.

One input moved:

- **mixed-bundle.zip** (`archive/`) is seen, where acceptance's run prepared it. Its bytes are
  also zip-in-zip.zip's entry `inner.zip`, and the first of the two to reach the index is
  prepared, the other seen; at 22 workers which comes first varies. In this run zip-in-zip.zip
  prepared `inner.zip` with its 6 children, and mixed-bundle.zip replayed them. Run alone at one
  worker, mixed-bundle.zip then zip-in-zip.zip prepared both inputs; the reverse order left
  mixed-bundle.zip seen. No release changed it: dispatch's indexing is unchanged since acceptance.

The releases since acceptance moved no other input:

- **LargeData.one's docx** (`onenote/large/`), "testing - large.docx", an XPS package, is unrouted
  with "no processor yet" at all 135 of its occurrences, where docproc-ooxml refused it, exit 3,
  in acceptance's run, and its 134 other occurrences replayed the failure. Since v0.4.1 route
  sends an XPS package to `Unrouted` whatever its name (eec6cb5). The section itself is prepared
  in both runs.
- **digitally-signed-3d-portfolio.pdf** (`pdf/edge/signed/`) is prepared in both runs; since
  v0.4.1 its page is the portfolio's own cover, not its first embedded document's
  (["Identity"](#identity)).
- **compacted-syntax.pdf** (`pdf/edge/malformed/`) is prepared, with one image and one text, as in
  acceptance's run, which first carried 831f865.
- **docproc-mail v0.1.3 and v0.1.4** move no outcome: the nested route's counts and its 2 skipped
  items are acceptance's.
- **The soffice image** moves no outcome: LibreOffice is 26.8.2.2 in both images.

**The slowest inputs** were the large OneNote sections, 143.6 s and 142.8 s, against acceptance's
145.5 s and 145.3 s; the CFR volume, 136.6 s against 137.0 s; and the Stella Maberly scan, 113.9 s
against 116.2 s.

**The CFR volume**, `pdf/large/cfr-2024-title32-vol1.pdf`, 915 pages: prepared in 136.6 s with
1,824 outputs, 915 page images, 908 texts, and 1 forms record, as in acceptance's run, in 137.0 s.

**The audiobook**, `audio/long/librivox-stella-maberly.m4b`, 126,864,276 bytes: prepared in
31.7 s with 607 outputs, 606 audio windows cut at silence and 1 record, as in acceptance's run, in
37.7 s.

`docproc report` exited 0 and wrote the report to
`~/runs/docproc/final/runs/01a208e5-e581-7b95-b43a-0f91f8d3b40e/report/index.html`, with one page
for each entry beside it, named by a 64-hex-digit hash. Acceptance's run id was
`01a1f538-fc4f-7ecd-8573-17a54f77dada`.

## Failures by class

This table classes each failure in the `final` task's run's record by what stopped it, counting
each part that failed once, beside acceptance's run:

| Class | Acceptance (2026-11-19) | This run |
| --- | --- | --- |
| The tool's own exit | 1, with 134 seen replays | 0 |
| The tool's own signal | 0 | 0 |
| The tool's time bound | 0 | 0 |
| Output that doesn't parse | 1 | 1 |
| The tool's in-band refusal of one item, a `skipped` line | 2 | 2 |
| The 2 GiB memory bound | 0 | 0 |
| The run's own classes, retried and unrecorded | 0 ("error inputs 0") | 0 ("error inputs 0") |

"memory bound" appears in neither run's record, so no part failed by the 2 GiB memory bound.

| Input | Part | Tool | Reason | Verdict |
| --- | --- | --- | --- | --- |
| `pdf/edge/malformed/no-document-catalog.pdf` | `/` | gs, its page sizes | gs printed "Couldn't initialise file" and a count of 0 | no fix: a correct failure |
| `email/pst/java-libpst/passworded.pst` | `/message 2097188`, in Contacts | docproc-mail | skipped: Missing Sub-Node Tree on message | backlog |
| `email/pst/java-libpst/dist-list.pst` | `/message 2097188`, in Contacts | docproc-mail | skipped: Missing Sub-Node Tree on message | backlog |

- **no-document-catalog.pdf: no fix, a correct failure.** The file has no document catalog, and
  gs can't open it. qpdf's form read (exit 2, no trailer dictionary) and pdfdetach's attachment
  list (exit 1, no catalog) are warnings on the same part.
- **passworded.pst and dist-list.pst: backlog.** tools/mail/README.md's Limits documents it: an
  item outlook-pst 1.2.0 can't open, such as a contact stored without a sub-node tree, is skipped.
- **LargeData.one's docx left the table.** Acceptance's run counted it under the tool's own exit:
  docproc-ooxml refused the XPS package named `.docx`. Since v0.4.1 it is unrouted, "no processor
  yet", and XPS has no processor.

No failure is new. The other 25 warnings are informational: 11 from the text stage, text decoded
as windows-1252 and its replacement characters; 11 from docproc-mail, 1 attachment decoded
lossily, 7 image sources that name no attachment, and 3 embedded messages outlook-pst doesn't
open; and docproc-ooxml's 3 "text boxes not read". Acceptance's record held 29 warnings too, 24 of
them informational by its count; that record is gone, so the difference of one isn't traced.

## Peak memory

Acceptance measured it on 2026-11-19, on 96a9cdb, with its ~/docs run's tools: docproc-ooxml
0.1.1, docproc-mail 0.1.2, and docproc-onenote 0.1.0, built from its checkout, and soffice in
Gotenberg 9.1.0's image. The `final` task didn't measure it again; the page's opening says what
has changed since in the programs it measures.

It took the peak resident memory of each tool's calls, through a stand-in on PATH for every program
docproc runs on the host, which ran the real program with docproc's exact arguments, under its 2 GiB
RLIMIT_AS, and logged the call's ru_maxrss. The run was a second full ~/docs run of acceptance's,
`docproc run` with every tool wrapped: 154.6 s, the counts of acceptance's ~/docs run, 1,347 calls,
none ended by a signal. soffice runs in its container under docker's `--memory=2147483648`, a cgroup
bound, so it was measured apart: docproc's lockdown flags and soffice arguments, with a static
rusage as the container's entrypoint, on the five largest legacy office inputs, the largest .xls,
and the largest EMF and WMF.

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

**The 2 GiB bound stood.** No part failed by it, and every peak was under it: the highest,
magick's 344.3 MiB, is 16.8% of 2 GiB, and no peak came within 50% of the bound, 1 GiB. The bound
is on address space, which resident memory understates; acceptance's two runs' zero memory-bound
failures are the evidence for that side, and the `final` task's ~/docs run had none either
(["Failures by class"](#failures-by-class)).

One gap: docproc-ooxml's arm64 floor is unmeasured, because this host is x86_64. On x64, about 1
run in 20 of the .NET tool aborted at a 256 MiB address-space bound, and all 20 passed
at 512 MiB.

## Bombs

Acceptance ran both bombs on 2026-11-19, on 96a9cdb; the `final` task didn't run them again. Both
follow PI-1's recipe (`git show 8b03243:context/evaluations/processing-inspect.md`),
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

| Bomb | Before the write-time cap (cb82087) | Acceptance's run (96a9cdb) |
| --- | --- | --- |
| 2 TiB ZIP: outcome | quarantined, `archive-bomb`, unsafe | quarantined, `archive-bomb`, unsafe |
| 2 TiB ZIP: time | 0.70 s | 0.66 s |
| 2 TiB ZIP: disk | no entry written | no entry written |
| 999:1 entry: outcome | `limited`, "past the bytes cap, 34359738368", size 107,374,182,400 | `limited`, "past the bytes cap, 34359738368", no size or SHA-256 |
| 999:1 entry: time | 168.5 s | 31.4 s |
| 999:1 entry: peak apparent | 100.0 GiB | 30.9 GiB at the last sample, under the 31.9 GiB the cap leaves |
| 999:1 entry: peak physical | 3.71 GiB | 1.02 GiB |

The 2 TiB ZIP was quarantined at inspection, before any entry was written: "declared compressed
sizes sum to 2097152000 bytes, past the 185802-byte ZIP, and 1 more". The "and 1 more" is the
summed-entry bomb rule: `inspect/zip.go` flags both the declared compressed sum and the declared
expanded sum, and the reason quotes the first. The run exited 0.

The 999:1 entry was stopped while it was written. The archive part itself was prepared, with one
child; the child `/zip entry payload.bin` was recorded `limited`, with size 0 and no SHA-256, and
the run exited 0. The entry was written at about 1.13 GiB a second; the last sample before its
removal, at 28.3 s, read 30.92 GiB apparent, and the next, at 29.3 s, found the partial file
removed. The cap leaves the entry 32 GiB less the bytes already admitted, the ZIP's own
107,417,762, so the file stopped at no more than 34,252,320,606 bytes, 31.9 GiB. Its physical
size is 1.02 GiB only because btrfs compresses the near-zero payload; on an uncompressed
filesystem it is the apparent size. Before the cap bounded writes, the same entry was written
whole, 100 GiB, and hashed before dispatch refused it, in 168.5 s.

**No starting value moved.** A bound's starting value moves when a bomb uses up the run's scratch
or runs longer than the unit timeout, 10 minutes. Neither bomb did: the 2 TiB ZIP wrote
nothing, and the 999:1 entry was stopped by `DOCPROC_MAX_BYTES` at 32 GiB in 31.4 s, with 1.7 TB
free on the run's filesystem. No tool runs for either, since ZIP is read in Go, so neither
`DOCPROC_UNIT_TIMEOUT` nor `DOCPROC_STREAM_IDLE` applies. `DOCPROC_MAX_BYTES` at 32 GiB,
`DOCPROC_BOMB_RATIO` at 1000, `DOCPROC_BOMB_SIZE` at 1 GiB, and `DOCPROC_ARCHIVE_ENTRIES` at
100,000 stand.

## Word error rate

Acceptance measured it on 2026-11-19; the `final` task didn't measure it again. It ran on the
Framework row, `http://framework:8001/v1` direct, model `audio`:
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

**Every excerpt passed.** No miss went to v1.ai.inference. LibriVox was nearest its line, 0.0077
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
