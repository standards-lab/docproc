# acceptance

Measured 2026-11-19 on an Intel Core Ultra 9 185H, 16 cores and 22 threads, with gs 10.08.0.
This note holds the results of v1.ai.processing's acceptance task, a section for each
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

Every page's PNG and text, and its record line apart from times, is identical to the per-page
path's on all 68 PDFs: each page's number, DPI, PNG SHA-256, text SHA-256, file name, and error,
at concurrency 1 and 4, against the per-page path's at concurrency 1. Six PDFs fail as before:
the five gs can't open fail their count with the same error, word for word, and
compacted-syntax.pdf's one page fails its render with the same error.

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
