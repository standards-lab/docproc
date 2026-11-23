# docproc-mail performance

Acceptance found docproc-mail slower than the spike on plaso's store and on scale.mboxrd, and the
difference was CPU time ([`acceptance.md`](acceptance.md), "docproc-mail: the findings"). This
page breaks down where a schema "2" read spends that time, stage by stage, from samply profiles of
the musl release binary. It names each waste the profiles show, with its code location: a
redundant pass, a needless allocation or copy, or work done for output that is never written. The
aim is that each stage does its work and nothing more, so there is no time target. The figures
are shares of samples, with wall times as context.

The "before" profiles were taken on 2026-11-22 at 1660dcb, before any fix. The fixes and the
"after" profile are recorded in [After the fixes](#after-the-fixes).

## Method

The inputs are plaso's store (`nromanoff@stark-research-labs.com.pst`, 59 MB, 540 messages) and
scale.mboxrd (2.7 GB, 50,000 messages), both under spike-ai-pipeline-mail's `.state/`. Neither
run passes `--media`. Two binaries are profiled, each a static musl release build with debug
symbols, built outside the repository:

- HEAD's docproc-mail, built from `tools/mail` with the tool's own toolchain.
- The spike's 0.3.1, built from a scratch clone of spike-ai-pipeline-mail at 9cbbdd3, so that
  its stages read by name.

Timings use the plain release builds: HEAD's without debug symbols, and the spike's committed
0.3.1 binary.

```sh
S=<scratch dir>
(cd tools/mail && mise x -- bash -c 'export PATH="$CARGO_HOME/bin:$PATH"
  CARGO_TARGET_DIR='$S'/target-prof CARGO_PROFILE_RELEASE_DEBUG=true \
    cargo build --release --locked --target x86_64-unknown-linux-musl
  CARGO_TARGET_DIR='$S'/target-rel cargo build --release --locked --target x86_64-unknown-linux-musl')
git clone ~/experiments/spike-ai-pipeline-mail $S/spike && git -C $S/spike checkout 9cbbdd3
(cd $S/spike && mise trust && mise x -- bash -c 'export PATH="$CARGO_HOME/bin:$PATH"
  CARGO_PROFILE_RELEASE_DEBUG=true cargo build --release --locked --target x86_64-unknown-linux-musl')
rec() { mise x github:mstange/samply@samply-v0.13.1 -- taskset -c 0-3 \
  samply record --save-only --unstable-presymbolicate "$@" > /dev/null; }
rec --rate 4000 -o $S/head-plaso.json.gz -- $S/target-prof/x86_64-unknown-linux-musl/release/docproc-mail <plaso's store>
rec --rate 2000 -o $S/head-mbox.json.gz -- $S/target-prof/x86_64-unknown-linux-musl/release/docproc-mail <scale.mboxrd>
rec --rate 4000 -o $S/spike-plaso.json.gz -- $S/spike/target/x86_64-unknown-linux-musl/release/spike-ai-pipeline-mail <plaso's store>
rec --rate 2000 -o $S/spike-mbox.json.gz -- $S/spike/target/x86_64-unknown-linux-musl/release/spike-ai-pipeline-mail <scale.mboxrd>
hyperfine -N --warmup 1 --runs 10 -n spike "<spike 0.3.1> <input>" \
  -n head "$S/target-rel/x86_64-unknown-linux-musl/release/docproc-mail <input>" --output=null
```

- samply was fetched for the session only and is pinned nowhere. It needs
  `perf_event_paranoid` at 1 or lower; the machine's 2 was lowered to 1 for the session.
- `taskset -c 0-3` is needed because samply maps a perf buffer for each CPU the process may run
  on, and across all 22 CPUs that exceeds the unprivileged lock limit (`perf_event_mlock_kb` 516),
  so `samply record` fails with "mmap failed". The tool is single-threaded, so the pin doesn't
  change what it does.
- Each sample's stack was symbolized with `llvm-symbolizer --inlining`, inlined frames included.
  A caller's frame is looked up at its return address less one, so the frame names the call
  rather than the instruction after it. samply's sidecar gives only one inline chain per function,
  so it wasn't used for this.
- A sample counts toward the stage named by the innermost frame on its stack that belongs to a
  stage. For example, a pulldown-cmark frame under `content::markdown::plain` counts toward the
  content rule, and one under `markdown::bare` counts toward the bare pass.
- musl's allocator has no unwind tables, so a sample taken inside it usually loses its callers.
  Those samples are a row of their own. To attribute allocations, a scratch copy of the tool was
  instrumented: a counting `GlobalAlloc` wraps `System` (musl's allocator still serves every
  call) and counts allocations around each stage. The copy also recorded per-message token counts
  and edit outcomes for the content rule's alignment. Neither the copy nor the profiles are
  committed.

## Wall time, for context

Medians of 10 runs, taken in one sitting, with hyperfine's user-space instruction counts:

| Input | Binary | Wall | Range | User | System | Instructions |
| --- | --- | --- | --- | --- | --- | --- |
| plaso's store | spike 0.3.1 | 1.465 s | 1.460–1.492 s | 1.264 s | 0.193 s | 7.87 G |
| plaso's store | HEAD | 3.289 s | 3.273–3.326 s | 3.015 s | 0.258 s | 44.42 G |
| scale.mboxrd | spike 0.3.1 | 5.655 s | 5.469–5.735 s | 3.646 s | 1.954 s | 25.92 G |
| scale.mboxrd | HEAD | 7.826 s | 7.633–9.459 s | 5.420 s | 2.349 s | 41.27 G |

On plaso's store, HEAD runs 5.6 times the spike's instructions.

## Where the time goes

Shares of each profile's samples by stage. The seconds are the samples divided by the sampling
rate.

| Stage | plaso's store: share | plaso's store: s | scale.mboxrd: share | scale.mboxrd: s |
| --- | --- | --- | --- | --- |
| Reading the input: the .pst's nodes and properties; the mbox's split, MIME parse, and attachment decoding | 7.8% | 0.26 | 29.5% | 2.74 |
| Body selection and decoding: the MAPI bodies, code pages, RTF | 0.2% | 0.01 | 0.0% | 0.00 |
| htmd: the converter built | 0.2% | 0.01 | 4.8% | 0.44 |
| htmd: the conversion (html5ever's parse and htmd's walk) | 26.7% | 0.91 | 5.3% | 0.49 |
| htmd: the `bare` pass | 2.0% | 0.07 | 0.1% | 0.01 |
| The nesting count (`too_deep`) | 1.1% | 0.04 | 0.3% | 0.03 |
| The image-source rewrite | 1.0% | 0.04 | 0.3% | 0.03 |
| Content rule: tokens | 4.4% | 0.15 | 12.2% | 1.13 |
| Content rule: alignment | 40.4% | 1.37 | 0.1% | 0.01 |
| Content rule: sentence split | 0.4% | 0.01 | 1.1% | 0.11 |
| Content rule: reading the Markdown | 1.3% | 0.04 | 1.0% | 0.10 |
| Content rule: the rest | 0.1% | 0.00 | 0.6% | 0.05 |
| Escaping: `from_text` and the envelope's escapes | 0.9% | 0.03 | 1.9% | 0.17 |
| The JSON line | 0.4% | 0.01 | 2.6% | 0.25 |
| Media: hashing each payload for its name | 0.2% | 0.01 | 12.3% | 1.14 |
| Allocator, caller not unwound | 13.0% | 0.44 | 27.7% | 2.58 |
| Other | 0.1% | 0.00 | 0.2% | 0.01 |
| Samples, rate | 13,588 at 4 kHz | 3.40 | 18,561 at 2 kHz | 9.28 |

For an mbox, mail-parser decodes the bodies while it parses each message, so their decoding falls
under reading the input.

The allocation counts from the instrumented copy, as a share of the run's allocations:

| Stage | plaso's store | scale.mboxrd |
| --- | --- | --- |
| Whole run | 5,199,022 | 19,092,087 |
| Content rule: `content::missing` | 1,153,819 (22%) | 7,627,754 (40%) |
| htmd: the converter built | 95,226 (2%) | 6,175,353 (32%) |
| htmd: the conversion | 2,124,209 (41%) | 1,038,269 (5%) |
| The nesting count and the image-source rewrite | 1,159,566 (22%) | 981,019 (5%) |
| Escaping (`from_text`) | 7,508 | 403,260 (2%) |
| Content rule: reading the Markdown | 145,142 (3%) | 338,025 (2%) |
| The `bare` pass | 121,224 (2%) | 34,889 |
| The JSON line | 0 | 0 |

### Against the spike

The spike's profiles, grouped the same way where the spike has the same stage. Seconds are samples
over the rate, and a sample counts toward every group its stack holds:

| Group | Spike, plaso's | HEAD, plaso's | Spike, scale.mboxrd | HEAD, scale.mboxrd |
| --- | --- | --- | --- | --- |
| Total | 1.56 s | 3.40 s | 6.35 s | 9.28 s |
| The .pst's reading (`outlook_pst`) | 0.25 s | 0.26 s | — | — |
| The mbox split | — | — | 1.44 s | 1.60 s |
| Attachment decoding | — | — | 0.46 s | 0.50 s |
| Payload hashing (SHA-256) | 0.01 s | 0.01 s | 0.89 s | 0.95 s |
| htmd: the converter built | 0.00 s | 0.01 s | 0.36 s | 0.44 s |
| htmd: the conversion | 0.90 s | 0.92 s | 0.50 s | 0.48 s |
| The content rule | — | 1.58 s | — | 1.40 s |
| The JSON line | 0.06 s | 0.01 s | 0.28 s | 0.23 s |
| Allocator, caller not unwound | 0.33 s | 0.44 s | 1.76 s | 2.56 s |

Stages shared with the spike take about the same time; htmd 0.6's conversion costs what 0.5.6's did.
The difference is the content rule, which is new in schema "2": 1.58 s of plaso's 1.84 s and
1.40 s of scale.mboxrd's 2.93 s. Most of the rest of the mbox difference is allocator time that
lost its callers. The rule's per-token allocations (waste 1) are the likeliest source.

## The wastes

Ranked by what each costs in the profiles. Paths are relative to `tools/`, with line numbers at
1660dcb.

1. **A `String` for every word token.** `words`
   (`envelope/src/content.rs:180`) allocates each word segment as `Key::Word(segment.to_owned())`
   (line 197). `Ids::tokens` (line 147) then hashes it with SipHash into the run's interning map
   (line 163) and drops it whenever the word is already known, which is most of the time. On
   scale.mboxrd the tokens take 12.2% of samples (1.13 s): 2.5% copying segments, 3.4% hashing
   and inserting into the map, the rest in the segmenter and URL scan. `content::missing` makes
   7.6 million of the run's 19.1 million allocations, and 1.2 million of 5.2 million on plaso's
   store. Fix: intern borrowed slices, a map from `&str` that borrows the two texts for the
   call's length, with URL keys, which `url::normalized` already allocates, in a map of their
   own. Then a word token allocates nothing.
2. **htmd's converter built for every message.** `from_html` (`mail/src/markdown.rs:442`)
   calls `HtmlToMarkdown::builder()` and adds the skip list and three handlers (lines 443–448)
   on each call. htmd rebuilds its handler table on every `add_handler`, so each build costs about
   180 allocations. On scale.mboxrd that is 4.8% of samples (0.44 s), about as much as the
   conversion itself, and 6.2 million allocations, 32% of the run's. The spike did the same
   (0.36 s). Fix: build the converter once per run and pass it to `from_html`, or keep it in a
   `thread_local`; its handlers are plain functions.
3. **Two scans of the HTML's tags, each allocating every tag name.** `too_deep` (line 727) and
   `rewritten` (line 350) each run `Pieces` over the whole body. `Pieces::name` (line 869)
   returns each tag name as a lowercased `String` (line 873), and `too_deep` keeps its open
   elements as `Vec<String>` (line 728). Together they take 2.1% of plaso's samples (0.08 s) and
   1.16 million allocations, 22% of that run's; on scale.mboxrd they take 0.6% and 0.98 million.
   Fix: one scan that counts the nesting and collects the `<img>` source ranges, with names
   compared in place against the static lists (ASCII case-insensitively, or lowercased into a
   small stack buffer). Then a tag allocates nothing.
4. **Payloads copied through a fresh 64 KiB buffer before hashing.** `Media::store`
   (`envelope/src/media.rs:61`) takes `impl Read`. Every caller passes a slice already in memory,
   yet `copy` (line 130) allocates a zeroed 64 KiB buffer for each payload (line 132) and copies
   the slice through it (line 134) before `hasher.update` (line 140). On scale.mboxrd, with
   21,202 distinct payloads, the copy is 1.7% of samples (0.16 s), besides the buffer's
   allocation and zeroing. The hash itself, 10.2%, is the work the names need, and the spike's took the same.
   Fix: take the bytes as a slice, hash them in place, and, with `--media`, write them straight
   to the temporary file.
5. **The `bare` pass parses the whole Markdown twice, then again per block.** For any Markdown
   holding a backslash, `bare` (`mail/src/markdown.rs:463`) parses it once in `kept` (line 552)
   and again in `blocks` (line 480). `bare_block` (line 522) copies the block (line 523), and for
   each escape it might remove, it copies the block again (line 529) and parses it again. For a
   Markdown with no backslash, `bare` copies the whole text to return it (line 465). On plaso's
   store this is 2.0% of samples (0.07 s): `kept` 0.8%, `blocks` 0.7%, the trials 0.2%; 121,224
   allocations. Fix: one offset parse that yields both the top-level block ranges and the kept
   ranges, and `bare` taking the `String` by value, so a Markdown with no backslash is returned
   as it is.
6. **`from_text` copies each line three times.** `from_text` (line 658) copies the whole text
   twice to normalize line endings (line 659). Then, for each line, it collects the line's words
   into a `Vec` and joins them into a new `String` (line 667), which `escape` copies again into
   another. On scale.mboxrd escaping is 1.9% of samples (0.17 s) and 403,260 allocations. Fix:
   one pass over the text that splits lines at any line ending, collapses whitespace, and escapes
   straight into the output, with an `escape` that appends to a caller's `String`.

Looked for and not found to cost anything on these inputs:

- Writing a `data:` image's payload hashes it twice: once in `Images::data`, for its name
  (`mail/src/markdown.rs:252`), and again in `files.store` in `make`. Neither input has a
  `data:` image, and `Images::data` has no samples. The fix that removes waste 4 can hand the
  name through.
- The per-message flush of stdout in the mbox reader (`mail/src/mbox.rs:133`) is part of the
  documented resume behavior and takes 0.2% of samples.
- Building the content rule's segmenters and its category map for each call doesn't show in the
  profiles.

## What is not waste

- **The content rule's alignment on plaso's store (40.4%, 1.37 s).** Many of plaso's messages
  carry a text alternative written apart from their HTML: 513 of the 538 compared gain appended
  sentences. Across the 538 comparisons, the alignment kept about half of the text body's
  286,259 tokens against the Markdown's 357,606. The Markdown's many image and link destinations are tokens the text body
  never holds. So each comparison runs to about 4,500 edits, and Myers' algorithm costs
  O((n + m)·D). The instrumented copy counted 268 million diagonal steps, 98.5% of them inside
  the edit graph, and no comparison reached `EDITS_MAX`. This is the rule's defined work, not a
  redundant pass. Dropping the tokens that appear on only one side before aligning, as diff tools
  do, would cut it from 2.76 s to 0.56 s in the instrumented copy. But it changed which sentences
  are missing in 136 of plaso's 538 messages, and it would change what `EDITS_MAX` counts. That
  makes it a change to the rule (schema "2"), not a fix, and it is the architect's decision. On
  scale.mboxrd the generator's text and HTML bodies align closely, and the alignment is 0.1%.
- **html5ever's parse (23% of plaso's samples).** Its tokenizer appends most text and attribute
  values to tendrils one character at a time. That accounts for most of the conversion's memcpy,
  and the spike's htmd 0.5.6 spent the same time in it. The cost lies inside the dependency, and
  the HTML it parses has nothing to drop. Plaso's 20.9 MB of HTML holds 19 KB of `<style>` and
  38 KB of comments.
- **Reading the input.** The mbox split, mail-parser's parse, attachment decoding, and the .pst's
  reads take what the spike's did.

## After the fixes

Slice 5 records here how each numbered waste was removed, with the after profiles taken the same
way on the same inputs, and the stage table and allocation counts set beside the ones above.
