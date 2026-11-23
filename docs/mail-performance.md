# docproc-mail performance

Acceptance found docproc-mail slower than the spike on plaso's store and on scale.mboxrd, and the
difference was CPU time ([`acceptance.md`](acceptance.md), "docproc-mail: the findings"). This
page breaks down where a schema "2" read spends that time, stage by stage, from samply profiles of
the musl release binary. It names each waste the profiles show, with its code location: a
redundant pass, a needless allocation or copy, or work done for output that is never written. The
aim is that each stage does its work and nothing more, so there is no time target. The figures
are shares of samples, with wall times as context.

The "before" profiles were taken on 2026-11-22 at 1660dcb, before any fix, and
[After the fixes](#after-the-fixes) records the fixes and the "after" profile. docproc-mail 0.1.4
then stopped running the content rule ([The HTML-only Markdown](#the-html-only-markdown)), so the
content rule's rows record the read before that change.

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

Stages shared with the spike take about the same time; htmd 0.6's conversion costs what 0.5.6's
did. The difference is the content rule, which is new in schema "2": 1.58 s of plaso's 1.84 s and
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
   allocation and zeroing. The hash itself, 10.2%, is the work the names need, and the spike's
   took the same. Fix: take the bytes as a slice, hash them in place, and, with `--media`, write
   them straight to the temporary file.
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

- **The content rule's alignment on plaso's store (40.4%, 1.37 s).** Many of plaso's messages carry
  a text alternative written apart from their HTML: 513 of the 538 compared gain appended sentences.
  Across the 538 comparisons, the alignment kept about half of the text body's 286,259 tokens
  against the Markdown's 357,606. The Markdown's many image and link destinations are tokens the
  text body never holds. So each comparison runs to about 4,500 edits, and Myers' algorithm costs
  O((n + m)·D). The instrumented copy counted 268 million diagonal steps, 98.5% of them inside the
  edit graph, and no comparison reached `EDITS_MAX`. This is the rule's defined work, not a
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

Each waste was removed in a commit of its own, each with the check passing and the goldens
unchanged. The "after" profiles were taken on 2026-11-22 at 62e8d24, the same way and on the same
inputs as the "before" ones, with the stage rules following the renamed functions (`image_tags`,
`blocks_and_kept`, `collapse`).

### How each waste was removed

1. **Word tokens** (6a5b695). The interning map borrows its word keys, `&str` slices of the two
   texts, for the length of `content::missing`; URL keys, which `url::normalized` already
   allocates, are a map of their own, sharing one numbering. A word token allocates nothing.
2. **htmd's converter** (e7c4132). The converter is a `thread_local`, built at its first use. htmd
   resets its own per-document state at each conversion, so a reused converter writes what a
   fresh one did. Passing it in was weighed, but it would thread a value that never changes
   through every reader (pst, msg, mime, mbox, eml) and `derive`'s tests.
3. **The tag scans** (ed7becd). One scan, `image_tags`, counts the nesting and collects the
   `<img>` tags' ranges, and it returns None past the cap. `rewritten` rewrites the sources of the
   tags it's given, so images are still resolved only for a body within the cap. `Pieces` yields
   each tag name as a borrowed slice, compared ASCII case-insensitively against the static lists,
   and a raw element's end is found without building its `</name`. A tag allocates nothing.
4. **The payload buffer** (2447ea6). `Media::store_bytes` hashes a slice in place. With
   `--media`, it writes the slice whole into the temporary file, and only when its name isn't
   already written this run. Every docproc-mail caller passes a slice, so every one uses it.
   `Media::store` keeps its reader for docproc-onenote, whose file data comes as a reader. The
   `data:` image's double hash went in the same commit: `Payload` names the bytes once, when
   `Images::data` reads them, and `make` stores that `Payload` by its name.
5. **The `bare` pass** (93cf6e2). One offset parse, `blocks_and_kept`, yields both the top-level
   block ranges and the kept ranges. A block with no escape to try is appended as it stands, and
   the trials reuse one buffer. `bare` takes htmd's `String` by value, so a Markdown with no
   backslash is returned uncopied.
6. **`from_text`** (bf0157f). Lines are split at any line ending as the pass goes. A line that is
   already single-spaced is taken as it stands, and any other is collapsed into one reused buffer.
   Each line is escaped straight into the output through the envelope's `escape_into`, which
   `escape` now wraps.

The content rule's alignment (62e8d24) was tightened without changing what it finds.
`middle_snake` takes the same steps in the same order with less work in each. The two ranges are
sliced once. The diagonals are walked by a plain countdown. Diagonal k's start, which is one past
diagonal k − 1's point or on diagonal k + 1's, whichever reaches further, is taken as their
maximum, which it equals, so no branch depends on the data. Diagonal k − 1's point, read for
diagonal k, is carried to diagonal k − 2 as its k + 1, so each diagonal reads one point, not two.
The tokens were already interned to integers, and the common prefix and suffix were already
trimmed. Two other changes were measured and not kept: reversed copies of the sequences for the
backward snake, and diagonals indexed without the offset. Neither ran faster. On the 538 token
pairs dumped from plaso's store, the old and new `kept` agree exactly, bounded, unbounded, and at
a 1,000-edit bound, and so do they on 1.2 million random pairs with many ties. A unit test pins
the rule's choice among tied subsequences.

### Byte identity

Each binary's stdout and stderr were hashed (SHA-256) on the three bench inputs, at eec6906 before
any fix and at 62e8d24. Each commit between them was checked the same way: on plaso's store for
the first two, and on all three inputs from ed7becd on. Every hash matched:

| Input | stdout SHA-256 | stderr |
| --- | --- | --- |
| plaso's store | `094a78a1…b6b149` | identical |
| scale.mboxrd | `3c13feeb…5d9f8` | empty |
| scale.pst | `7a474845…b3e75` | empty |

With `--media`, plaso's store and scale.mboxrd write the same files byte for byte: 5 and 21,202
files.

### Wall time

Medians of 10 runs, the three binaries timed in one sitting. "Before" is eec6906's build.

| Input | Binary | Wall | Range | User | System | Instructions |
| --- | --- | --- | --- | --- | --- | --- |
| plaso's store | spike 0.3.1 | 1.576 s | 1.531–1.652 s | 1.354 s | 0.215 s | 7.84 G |
| plaso's store | before | 3.408 s | 3.377–3.574 s | 3.136 s | 0.271 s | 44.47 G |
| plaso's store | after | 2.495 s | 2.474–2.520 s | 2.246 s | 0.242 s | 28.42 G |
| scale.mboxrd | spike 0.3.1 | 5.595 s | 5.524–5.688 s | 3.676 s | 1.875 s | 25.92 G |
| scale.mboxrd | before | 7.695 s | 7.638–7.966 s | 5.397 s | 2.277 s | 41.37 G |
| scale.mboxrd | after | 5.463 s | 5.418–5.483 s | 3.883 s | 1.544 s | 31.95 G |

After the fixes, the tool ran slightly faster than the spike on scale.mboxrd while still applying
the content rule, which the spike lacks. On plaso's store the difference left was almost all the
content rule.

### Where the time goes now

Seconds are samples over the rate, as in the "before" table, shown here beside it:

| Stage | plaso's: before | plaso's: after | after share | scale.mboxrd: before | scale.mboxrd: after | after share |
| --- | --- | --- | --- | --- | --- | --- |
| Reading the input | 0.26 s | 0.31 s | 10.0% | 2.74 s | 2.01 s | 33.6% |
| Body selection and decoding | 0.01 s | 0.01 s | 0.2% | 0.00 s | 0.00 s | 0.0% |
| htmd: the converter built | 0.01 s | 0.00 s | 0.0% | 0.44 s | 0.00 s | 0.0% |
| htmd: the conversion | 0.91 s | 1.07 s | 35.4% | 0.49 s | 0.39 s | 6.5% |
| htmd: the `bare` pass | 0.07 s | 0.05 s | 1.8% | 0.01 s | 0.00 s | 0.1% |
| The tag scan (`image_tags`) | 0.04 s | 0.05 s | 1.5% | 0.03 s | 0.03 s | 0.5% |
| The image-source rewrite | 0.04 s | 0.00 s | 0.0% | 0.03 s | 0.01 s | 0.1% |
| Content rule: tokens | 0.15 s | 0.15 s | 4.8% | 1.13 s | 0.81 s | 13.5% |
| Content rule: alignment | 1.37 s | 0.78 s | 25.7% | 0.01 s | 0.00 s | 0.1% |
| Content rule: sentence split | 0.01 s | 0.01 s | 0.4% | 0.11 s | 0.10 s | 1.6% |
| Content rule: reading the Markdown | 0.04 s | 0.05 s | 1.7% | 0.10 s | 0.10 s | 1.6% |
| Content rule: the rest | 0.00 s | 0.00 s | 0.1% | 0.05 s | 0.05 s | 0.8% |
| Escaping | 0.03 s | 0.04 s | 1.3% | 0.17 s | 0.07 s | 1.1% |
| The JSON line | 0.01 s | 0.02 s | 0.6% | 0.25 s | 0.23 s | 3.8% |
| Media: hashing each payload for its name | 0.01 s | 0.01 s | 0.3% | 1.14 s | 0.99 s | 16.5% |
| Allocator, caller not unwound | 0.44 s | 0.48 s | 16.0% | 2.58 s | 1.20 s | 20.1% |
| Other | 0.00 s | 0.00 s | 0.1% | 0.01 s | 0.01 s | 0.1% |
| Samples, rate | 13,588 at 4 kHz | 12,150 at 4 kHz | 3.04 s | 18,561 at 2 kHz | 11,987 at 2 kHz | 5.99 s |

The before table's "nesting count" row is now the tag scan, which also finds the images. The
image-source rewrite now only rewrites the tags the scan found.

The allocation counts, from the instrumented copy ported onto 62e8d24:

| Stage | plaso's: before | plaso's: after | scale.mboxrd: before | scale.mboxrd: after |
| --- | --- | --- | --- | --- |
| Whole run | 5,199,022 | 3,287,718 | 19,092,087 | 4,890,911 |
| Content rule: `content::missing` | 1,153,819 | 556,382 | 7,627,754 | 1,006,769 |
| htmd: the converter built | 95,226 | 177, once | 6,175,353 | 177, once |
| htmd: the conversion | 2,124,209 | 2,124,209 | 1,038,269 | 1,038,269 |
| The tag scan and the image-source rewrite | 1,159,566 | 10,851 | 981,019 | 55,133 |
| Escaping (`from_text`) | 7,508 | 4,293 | 403,260 | 15,111 |
| Content rule: reading the Markdown | 145,142 | 145,142 | 338,025 | 338,025 |
| The `bare` pass | 121,224 | 54,880 | 34,889 | 0 |

On scale.mboxrd, reading the input and the allocator row fell too, though their code didn't
change. The likeliest cause is that musl's allocator now serves fourteen million fewer
allocations. The payload copy is gone from the media row; the hash is what's left there, the work
the names need. On plaso's store, the conversion's samples read higher after: 1.07 s against
0.91 s. The code it runs is html5ever's and is unchanged, and the instrumented copy timed the
conversion at 1.08 s against 1.05 s. The higher reading is most likely the allocator's state and
sampling, not new work.

### What is left

- **The content rule's alignment on plaso's store: 0.78 s, 25.7% of samples.** Alone, on the
  538 dumped token pairs, it takes 0.69 s in place of 1.39 s. That time is now the diagonal steps
  themselves, about 268 million of them, each two reads, one write, and one comparison of tokens.
  The number of steps is set by the rule: Myers' algorithm takes O((n + m)·D) steps, and plaso's
  comparisons run to about 4,500 edits. Of the Markdown's 357,606 tokens, 152,355 never occur in
  their message's text body: the image and link destinations, and the text the HTML holds beyond
  the text body. Fewer steps would need a change to which tokens are aligned, such as dropping
  those that occur on one side only. That changes which sentences are missing ("What is not
  waste"), so it is the architect's decision.
- **The content rule's tokens on scale.mboxrd: 0.81 s, 13.5%.** This is ICU4X's word segmenter
  (5.2%), the interning map's SipHash lookups (3.8%), and the URL scan (3.1%). Each is a step the
  rule needs, and none copies or repeats work. A faster hash would trade away SipHash's
  resistance to inputs crafted to collide, and mail input is untrusted.
- **htmd's conversion and reading the input** remain as "What is not waste" describes.
- **musl's allocator.** It is kept. mimalloc 0.1.52, measured in its place, read 13 to 35%
  faster with about twice the peak memory. But its C build needs zig or a musl C toolchain, which
  the static musl release would take on for the allocator alone.

## The HTML-only Markdown

docproc-mail no longer runs the content rule. On plaso's store the rule appended 2.1 M characters
to 513 of the 538 messages it compared, and the median message's Markdown grew 27%. Most of it
was sentences whose words the Markdown already held, flagged because the alignment depends on
order and one unmatched token flags a whole sentence, and tracking URLs that differ between the
text and HTML parts. The architect chose the HTML alone: a message's Markdown is made from its HTML
body when it has one, else from its text body, else from its RTF body's own text, and nothing of
one body is appended to another's, as the parts of a `multipart/alternative` are each an
alternative version of the same information (RFC 2046 §5.1.4). The alignment's cost in "What is
left" no longer applies to mail: the content rule is docproc-onenote's alone.

Medians of 10 runs, both binaries timed in one sitting, by the method's `hyperfine` line, the
tool's plain static musl release build against the spike's 0.3.1:

| Input | Binary | Wall | Range | User | System | Instructions |
| --- | --- | --- | --- | --- | --- | --- |
| plaso's store | spike 0.3.1 | 1.484 s | 1.473–1.494 s | 1.265 s | 0.206 s | 7.86 G |
| plaso's store | HTML-only | 1.495 s | 1.486–1.501 s | 1.288 s | 0.198 s | 8.36 G |
| scale.mboxrd | spike 0.3.1 | 5.473 s | 5.465–5.516 s | 3.597 s | 1.839 s | 25.90 G |
| scale.mboxrd | HTML-only | 4.066 s | 4.050–4.137 s | 2.687 s | 1.353 s | 18.78 G |

On plaso's store the tool now runs as fast as the spike, against 2.495 s after the fixes; on
scale.mboxrd it runs 26% faster than the spike, against 5.463 s. No profile was taken of this
build.
